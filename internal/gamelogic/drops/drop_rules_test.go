// drop_rules_test.go —— TRIM-04 掉落规则对拍：组内物品等级过滤（含怪物专属组豁免、
// 宝石豁免）、分区门控豁免、以及"耐久按 +0 计、等级随后写入"的成型次序。
//
// 数据全部取自真实 S6 导出件（判据用谓词现搜，不写死物品号），随机性用固定种子。
package drops

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/util"
)

// findDef 按谓词取第一件符合条件的物品定义。
func findDef(cfg *config.GameConfig, pred func(*config.Item) bool) *config.Item {
	for i := range cfg.Items {
		if pred(&cfg.Items[i]) {
			return &cfg.Items[i]
		}
	}
	return nil
}

// monsterAtLeast 返回第一件"等级 ≥ want 且允许掉物"的怪物（NumberOfMaximumItemDrops 为 0
// 的怪在原版同样不掉任何东西，选它会让用例断言失真）。
func monsterAtLeast(t *testing.T, cfg *config.GameConfig, want int) *config.Monster {
	t.Helper()
	for i := range cfg.Monsters {
		m := &cfg.Monsters[i]
		if m.NumberOfMaximumItemDrops > 0 && monsterLevelOf(m) >= want {
			return m
		}
	}
	t.Fatalf("导出件里没有等级 ≥ %d 且可掉物的怪物", want)
	return nil
}

// dropKeys 跑若干次掉落，返回出现过的 (group,number) 集合。
func dropKeys(t *testing.T, cfg *config.GameConfig, monster *config.Monster, refs []config.DropGroupRef, seed int, rolls int) map[[2]int]bool {
	t.Helper()
	seen := map[[2]int]bool{}
	g := NewGenerator(cfg, util.NewRand(seed))
	for i := 0; i < rolls; i++ {
		items, _ := g.GenerateDrops(monster, 0, refs)
		for _, it := range items {
			seen[[2]int{int(it.Group), it.Number}] = true
		}
	}
	return seen
}

// TestGroupItemFilterDropsTooHighAndTooLow 锁定原版 GenerateItemFromGroup 的两道筛：
// DropLevel 高于怪级（掉不出）与"远低于怪级的非宝石货"（掉不出）。
func TestGroupItemFilterDropsTooHighAndTooLow(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	mon := monsterAtLeast(t, cfg, 60)
	lvl := monsterLevelOf(mon)

	tooHigh := findDef(cfg, func(d *config.Item) bool {
		return d.DropsFromMonsters && d.DropLevel > lvl
	})
	cheap := findDef(cfg, func(d *config.Item) bool {
		return d.DropsFromMonsters && d.DropLevel > 0 && d.DropLevel <= lvl-dropLevelMaxGap
	})
	fine := findDef(cfg, func(d *config.Item) bool {
		return d.DropsFromMonsters && canDropAtMonsterLevel(d, lvl) &&
			(d.DropLevel == 0 || d.DropLevel > lvl-dropLevelMaxGap)
	})
	if tooHigh == nil || cheap == nil || fine == nil {
		t.Skip("导出数据不满足本用例前提（缺高级/廉价/合格物品）")
	}
	refs := []config.DropGroupRef{{Group: config.DropGroup{Chance: 1.0, PossibleItems: []config.ItemRef{
		{Group: tooHigh.Group, Number: tooHigh.Number},
		{Group: cheap.Group, Number: cheap.Number},
	}}}}

	seen := dropKeys(t, cfg, mon, refs, 2026, 300)
	if len(seen) != 0 {
		t.Fatalf("两道筛都该拦下: 掉出 %v", seen)
	}

	// 合格物品单独成组时必掉（证明不是"整组都被清空"造成的空结果）。
	okRefs := []config.DropGroupRef{{Group: config.DropGroup{Chance: 1.0, PossibleItems: []config.ItemRef{
		{Group: fine.Group, Number: fine.Number},
	}}}}
	if seen := dropKeys(t, cfg, mon, okRefs, 2026, 20); len(seen) == 0 {
		t.Fatalf("合格物品 (%d,%d) 应当可掉", fine.Group, fine.Number)
	}
}

// TestMonsterSpecificGroupBypassesFilters 锁定"怪物专属组不做等级门控"：
// 同一批物品，标为怪物专属后高级/廉价件都能掉出来。
func TestMonsterSpecificGroupBypassesFilters(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	mon := monsterAtLeast(t, cfg, 60)
	lvl := monsterLevelOf(mon)
	tooHigh := findDef(cfg, func(d *config.Item) bool {
		return d.DropsFromMonsters && d.DropLevel > lvl
	})
	if tooHigh == nil {
		t.Skip("导出件里没有高于该怪等级的可掉物品")
	}
	ref := config.DropGroupRef{FromMonster: true, Group: config.DropGroup{
		Chance:        1.0,
		PossibleItems: []config.ItemRef{{Group: tooHigh.Group, Number: tooHigh.Number}},
	}}
	// 怪物专属组连 MinimumMonsterLevel 门控都不过（原版无参 PartitionDropGroups）。
	ref.Group.MinimumMonsterLevel = intPtr(lvl + 100)

	seen := dropKeys(t, cfg, mon, []config.DropGroupRef{ref}, 11, 20)
	if len(seen) == 0 {
		t.Fatal("怪物专属组应豁免怪级门控与组内等级过滤")
	}
}

// TestJewelGroupIgnoresLevelGap 锁定 `isJewel` 逃逸：宝石组不受"远低于怪级"限制。
func TestJewelGroupIgnoresLevelGap(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	mon := monsterAtLeast(t, cfg, 60)
	lvl := monsterLevelOf(mon)
	cheap := findDef(cfg, func(d *config.Item) bool {
		return d.DropLevel > 0 && d.DropLevel <= lvl-dropLevelMaxGap &&
			canDropAtMonsterLevel(d, lvl)
	})
	if cheap == nil {
		t.Skip("导出件里没有远低于怪级的可掉物品")
	}
	items := []config.ItemRef{{Group: cheap.Group, Number: cheap.Number}}

	if seen := dropKeys(t, cfg, mon, []config.DropGroupRef{{Group: config.DropGroup{
		Chance: 1.0, ItemType: "Jewel", PossibleItems: items,
	}}}, 5, 20); len(seen) == 0 {
		t.Fatal("宝石组应豁免 DropLevelMaxGap 规则")
	}
	if seen := dropKeys(t, cfg, mon, []config.DropGroupRef{{Group: config.DropGroup{
		Chance: 1.0, ItemType: "None", PossibleItems: items,
	}}}, 5, 20); len(seen) != 0 {
		t.Fatalf("非宝石组应受 DropLevelMaxGap 约束, got %v", seen)
	}
}

// TestDroppedDurabilityUsesLevelZeroBeforeGroupLevel 锁定成型次序：耐久在**写入组等级之前**
// 取满值（故不含等级加成），等级随后仍按组等级钳到定义上限——原版即如此。
func TestDroppedDurabilityUsesLevelZeroBeforeGroupLevel(t *testing.T) {
	cfg, mon := dropTestCfg(t)
	def := findDef(cfg, func(d *config.Item) bool {
		if !d.IsWearable() || d.Durability < 2 || d.MaximumItemLevel < 4 || d.DropLevel > monsterLevelOf(mon) {
			return false
		}
		// 卓越/远古会改耐久，只挑没有这两类候选定义的件。
		for _, id := range cfg.ItemOptionDefinitionsFor(d.Group, d.Number) {
			if od, ok := cfg.ItemOptionDefinition(id); ok &&
				(od.HasKind(cfg, config.OptionKindExcellent) || od.HasKind(cfg, config.OptionKindAncientOption)) {
				return false
			}
		}
		return true
	})
	if def == nil {
		t.Skip("导出件里没有无卓越/远古候选且可升到 +4 的可穿戴物品")
	}
	lvl4 := 4
	seen := dropKeys(t, cfg, mon, []config.DropGroupRef{{Group: config.DropGroup{
		Chance: 1.0, ItemLevel: &lvl4, PossibleItems: []config.ItemRef{{Group: def.Group, Number: def.Number}},
	}}}, 17, 1)
	if len(seen) != 1 {
		t.Fatalf("应掉出 1 件, got %v", seen)
	}

	g := NewGenerator(cfg, util.NewRand(17))
	items, _ := g.GenerateDrops(mon, 0, []config.DropGroupRef{{Group: config.DropGroup{
		Chance: 1.0, ItemLevel: &lvl4, PossibleItems: []config.ItemRef{{Group: def.Group, Number: def.Number}},
	}}})
	it := items[0]
	if it.Level != 4 {
		t.Fatalf("等级应取组等级 4, got %d", it.Level)
	}
	if int(it.Durability) != def.Durability+additionalDurabilityPerLevel[0] {
		t.Fatalf("耐久应按 +0 计（原版在写等级之前取满值）: got %d want %d",
			it.Durability, def.Durability+additionalDurabilityPerLevel[0])
	}
}

// TestDropGroupsForPutsMonsterFirst 锁定分区顺序（加权游标对顺序敏感）。
func TestDropGroupsForPutsMonsterFirst(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	if len(cfg.MonsterDrops) == 0 {
		t.Skip("导出件无怪物专属掉落组")
	}
	monsterNumber := cfg.MonsterDrops[0].Monster
	refs := cfg.DropGroupsFor(0, monsterNumber)
	if len(refs) == 0 || !refs[0].FromMonster {
		t.Fatalf("首位应为怪物专属组, got %+v", refs)
	}
	seenMap := false
	for _, r := range refs {
		if r.FromMonster && seenMap {
			t.Fatal("怪物专属组必须整体排在地图组之前")
		}
		if !r.FromMonster {
			seenMap = true
		}
	}
}

func intPtr(v int) *int { return &v }
