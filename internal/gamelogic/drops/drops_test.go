package drops

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/util"
)

func dropTestCfg(t *testing.T) (*config.GameConfig, *config.Monster) {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	// Bali（150）作为测试怪。
	m, ok := cfg.Monster(150)
	if !ok {
		t.Fatal("怪物 150 不存在")
	}
	return cfg, m
}

// TestMoneyDropFormula 锁定金钱掉落公式：money = gainedExperience + BaseMoneyDrop(7)。
func TestMoneyDropFormula(t *testing.T) {
	cfg, monster := dropTestCfg(t)
	// 构造一个 Money 组（chance 1.0 必掉）。
	groups := []config.DropGroupRef{{Group: config.DropGroup{Chance: 1.0, ItemType: "Money"}}}
	g := NewGenerator(cfg, util.NewRand(42))
	items, money := g.GenerateDrops(monster, 100, groups)
	if len(items) != 0 {
		t.Fatalf("Money 组不应掉物品: %d", len(items))
	}
	if money != 107 {
		t.Fatalf("money=%d, want 107 (100+7)", money)
	}
}

// TestChanceGroupPartition 锁定分区语义：Chance>=1 必掉、<1 机率掉，
// 且总掉落次数受 NumberOfMaximumItemDrops 限制。
func TestChanceGroupPartition(t *testing.T) {
	cfg, monster := dropTestCfg(t)
	groups := []config.DropGroupRef{
		{Group: config.DropGroup{Chance: 1.0, ItemType: "Money"}},
		{Group: config.DropGroup{Chance: 1.0, ItemType: "Money"}},
		{Group: config.DropGroup{Chance: 0.5, ItemType: "RandomItem"}},
	}
	g := NewGenerator(cfg, util.NewRand(7))
	// Bali NumberOfMaximumItemDrops = 1 → 只有 1 个必掉组能生效。
	items, money := g.GenerateDrops(monster, 10, groups)
	if money != 17 { // 10 + 7，仅第一个必掉组
		t.Fatalf("money=%d, want 17", money)
	}
	_ = items
}

// TestRandomItemLevelRule 锁定随机物品的等级规则：
// level = min((怪级 - DropLevel)/3, MaximumItemLevel)，且 DropsFromMonsters 过滤生效。
func TestRandomItemLevelRule(t *testing.T) {
	cfg, monster := dropTestCfg(t)
	g := NewGenerator(cfg, util.NewRand(3))
	groups := []config.DropGroupRef{{Group: config.DropGroup{Chance: 1.0, ItemType: "RandomItem"}}}

	items, money := g.GenerateDrops(monster, 0, groups)
	for _, it := range items {
		def, ok := cfg.Item(int(it.Group), int(it.Number))
		if !ok {
			t.Fatalf("掉出了未定义物品 (%d,%d)", it.Group, it.Number)
		}
		if !def.DropsFromMonsters {
			t.Fatalf("掉出了非 monster-drop 物品 (%d,%d)", it.Group, it.Number)
		}
		wantLevel := (monsterLevelOf(monster) - def.DropLevel) / 3
		if wantLevel > def.MaximumItemLevel {
			wantLevel = def.MaximumItemLevel
		}
		if wantLevel < 0 {
			wantLevel = 0
		}
		if int(it.Level) != wantLevel {
			t.Fatalf("(%d,%d) level=%d, want %d", it.Group, it.Number, it.Level, wantLevel)
		}
	}
	_ = money
}

// TestIsGroupRelevantFiltersByMonsterLevel 锁定原版 IsGroupRelevant：
// 按怪级过滤掉落组，不相关的组**不进入加权游标**。
//
// 依据（真实 S6 导出数据，Lorencia / mon 0 Bull Fighter lvl 6）：
// map 0 共 55 组，其中 51 个 None 组按怪级分档（min_monster_level 从 2 到 108）；
// 对 6 级怪只有 8 组相关，totalChance 从 1.2441 降到 0.8411。
// 漏掉过滤会让本该 4.76% 的 None 权重被抬到 35.61%（挤掉金币概率），
// 并让 6 级怪掉出 80~255 级物品。
func TestIsGroupRelevantFiltersByMonsterLevel(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mon, ok := cfg.Monster(0) // Bull Fighter（Lorencia 可击杀怪，lvl 6，maxdrops 1）
	if !ok {
		t.Fatal("怪物 0 不存在")
	}
	groups := mapDropGroups(cfg, 0)
	if len(groups) == 0 {
		t.Fatal("map 0 无掉落组")
	}

	var total, totalFiltered float64
	var moneyWeight, noneWeight float64
	relevant := 0
	for i := range groups {
		g := &groups[i].Group
		total += g.Chance
		if !isGroupRelevant(mon, g) {
			continue
		}
		relevant++
		totalFiltered += g.Chance
		switch g.ItemType {
		case "Money":
			moneyWeight += g.Chance
		case "", "None":
			noneWeight += g.Chance
		}
	}

	if total <= totalFiltered {
		t.Fatalf("过滤后权重应下降: total=%.4f filtered=%.4f", total, totalFiltered)
	}
	if relevant >= len(groups) {
		t.Fatalf("怪级 6 不应让全部 %d 组都相关", len(groups))
	}
	// 金币权重占过滤后总权重的比例应过半（数据事实：0.5 / 0.8411 ≈ 59.4%）。
	if share := moneyWeight / totalFiltered; share < 0.5 {
		t.Fatalf("金币应占过滤后权重的 50%% 以上, got %.2f%%（filtered=%.4f）", share*100, totalFiltered)
	}
	if noneWeight >= moneyWeight {
		t.Fatalf("等级门控的 None 组权重 %.4f 不应超过金币 %.4f", noneWeight, moneyWeight)
	}
}

// TestMap0MonsterDropsItemsAndMoney 端到端：用真实导出的 Lorencia 掉落表跑大量击杀，
// 断言"击杀怪物既会掉物品也会掉金币"，即实测问题 (a) 的回归网。
func TestMap0MonsterDropsItemsAndMoney(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mon, ok := cfg.Monster(0)
	if !ok {
		t.Fatal("怪物 0 不存在")
	}
	groups := mapDropGroups(cfg, 0)
	g := NewGenerator(cfg, util.NewRand(2026))

	const kills = 4000
	var itemKills, moneyKills, totalItems int
	for i := 0; i < kills; i++ {
		items, money := g.GenerateDrops(mon, 0, groups)
		if len(items) > 0 {
			itemKills++
			totalItems += len(items)
		}
		if money > 0 {
			moneyKills++
			// 经验传 0 → 金额必须恒为 BaseMoneyDrop(7)。
			if money != baseMoneyDrop {
				t.Fatalf("金币=%d, want %d", money, baseMoneyDrop)
			}
		}
	}

	if itemKills == 0 {
		t.Fatal("4000 次击杀一件物品都没掉")
	}
	if moneyKills == 0 {
		t.Fatal("4000 次击杀一次金币都没掉")
	}
	// 期望值 ≈ 59.4%（Bull Fighter lvl 6），放宽到 [0.45, 0.72] 以耐受 RNG 波动。
	if r := float64(moneyKills) / kills; r < 0.45 || r > 0.72 {
		t.Fatalf("金币掉率 %.3f 越出预期区间 [0.45,0.72]", r)
	}
	t.Logf("kills=%d 掉物击杀=%d 掉钱击杀=%d(%.1f%%) 物品总数=%d",
		kills, itemKills, moneyKills, float64(moneyKills)/kills*100, totalItems)
}

// TestDeterministicWithSameSeed 锁定可复现：同种子同输入 → 同掉落序列（T0-d 的意义所在）。
func TestDeterministicWithSameSeed(t *testing.T) {
	cfg, monster := dropTestCfg(t)
	mapGroups := mapDropGroups(cfg, 0)
	run := func() string {
		g := NewGenerator(cfg, util.NewRand(99))
		out := ""
		for i := 0; i < 20; i++ {
			items, money := g.GenerateDrops(monster, 50+i, mapGroups)
			out += "|"
			if money > 0 {
				out += "M"
			}
			for _, it := range items {
				out += string(rune(it.Group)) + ":" + string(rune('a'+it.Number%26))
			}
		}
		return out
	}
	a, b := run(), run()
	if a != b {
		t.Fatal("同种子掉落序列不一致")
	}
	if a == "" {
		t.Fatal("掉落序列为空")
	}
}

func mapDropGroups(cfg *config.GameConfig, mapNumber int) []config.DropGroupRef {
	for i := range cfg.MapDrops {
		if cfg.MapDrops[i].Map == mapNumber && cfg.MapDrops[i].Discriminator == 0 {
			refs := make([]config.DropGroupRef, 0, len(cfg.MapDrops[i].Groups))
			for _, gr := range cfg.MapDrops[i].Groups {
				refs = append(refs, config.DropGroupRef{Group: gr})
			}
			return refs
		}
	}
	return nil
}

// monsterRef 包一层"怪物专属组"标记（原版不做怪级门控的那一路）。
func monsterRef(gr config.DropGroup) config.DropGroupRef {
	return config.DropGroupRef{Group: gr, FromMonster: true}
}
