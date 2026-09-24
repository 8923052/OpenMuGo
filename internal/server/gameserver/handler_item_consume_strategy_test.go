package gameserver

// handler_item_consume_strategy_test.go —— TRIM-01 消耗策略族的 gameserver 闭环测试：
// 果实（C1 2C 应答 + 门槛拒绝的"应答+失败"双包）、Bless 宝石端到端（升级 + 满耐久
// 刷新 + C1 F3 14）、解毒剂（C1 07 去激活）、传送卷轴（C3 1C 换图）、技能石等级偏移。
//
// 掷骰敏感的概率路径在 action 层用固定种子锁定（consume_strategy_test.go）；
// World.RNG() 无注种入口，本文件只测"任何随机结果都必须成立"的不变量
// （100% 档的果实、100% 的 Bless、以及各门槛拒绝分支）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/pricing"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// consumeFrameEx 构造带 TargetSlot / FruitConsumption 的 C3 26 帧。
func consumeFrameEx(itemSlot, targetSlot byte, usage c2s.FruitUsage) []byte {
	req := c2s.NewConsumeItemRequest()
	req.SetItemSlot(itemSlot)
	req.SetTargetSlot(targetSlot)
	req.SetFruitConsumption(usage)
	return req.Bytes()
}

// fruitResponse 返回唯一的 C1 2C 应答帧（断言"恰一帧"后给出解析视图）。
func fruitResponse(t *testing.T, rec *packetRecorder) *s2c.FruitConsumptionResponse {
	t.Helper()
	n := countFrames(rec, 0xC1, 0x2C)
	if n != 1 {
		t.Fatalf("应恰有 1 帧 C1 2C, got %d", n)
	}
	f := findFrame(rec, 0xC1, 0x2C)
	if len(f) != s2c.FruitConsumptionResponseLength {
		t.Fatalf("C1 2C 应为 %dB, got %d", s2c.FruitConsumptionResponseLength, len(f))
	}
	return s2c.AsFruitConsumptionResponse(f)
}

// resolveFruitBaseline 强制解析一次属性（幂等），返回解析后的五维基准，
// 让后续"果实 ±p"能用 Total 属性的**差值**断言（Base Energy 覆盖 → Total Energy 直传）。
func resolveFruitBaseline(m *moveScaffold, t *testing.T) *entity.CharStats {
	t.Helper()
	st, err := m.srv.resolveCharStats(m.c)
	if err != nil {
		t.Fatalf("预解析属性: %v", err)
	}
	m.c.Stats = st
	m.wp.Stats = st
	return st
}

// TestConsumeFruitAddEnergy 锁定加点主路径（DK、20 级 → 上限 6、已用 0 → 100% 档，
// 成功确定；点数 1/2/3 由 World.RNG 决定，用应答帧回读做差值断言）。
func TestConsumeFruitAddEnergy(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	base := resolveFruitBaseline(m, t)
	energyBefore := base.Energy
	m.putItem(t, 12, &item.Item{Group: 13, Number: 15, Level: 0, Durability: 3}) // 能量果实

	m.srv.handleItemConsume(m.sess, consumeFrameEx(12, 0, 0))

	resp := fruitResponse(t, m.rec)
	if int(resp.Result()) != 0 { // FruitConsumptionResult.PlusSuccess
		t.Fatalf("应答结果应为成功(0), got %d", resp.Result())
	}
	if int(resp.StatType()) != 0 { // Energy
		t.Fatalf("目标属性应为 Energy(0), got %d", resp.StatType())
	}
	p := resp.StatPoints()
	if p < 1 || p > 3 {
		t.Fatalf("点数应为 1..3, got %d", p)
	}
	if len(failedFrames(m.rec)) != 0 {
		t.Fatal("成功使用不应回失败包")
	}
	if m.c.Stats.UsedFruitPoints != uint16(p) {
		t.Fatalf("UsedFruitPoints=%d, want %d", m.c.Stats.UsedFruitPoints, p)
	}
	if m.c.Stats.Energy != energyBefore+p {
		t.Fatalf("Total Energy 应 +%d: %d → %d", p, energyBefore, m.c.Stats.Energy)
	}
	// 果实消耗：耐久 3→2 → C1 2A；未用尽不删除。
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 1 {
		t.Fatalf("应发 1 帧 C1 2A, got %d", n)
	}
	dc := s2c.AsItemDurabilityChanged(findFrame(m.rec, 0xC1, 0x2A))
	if dc.InventorySlot() != 12 || dc.Durability() != 2 || !dc.ByConsumption() {
		t.Fatalf("C1 2A 应为槽 12 耐久 2 byConsumption, got 槽 %d 耐久 %d", dc.InventorySlot(), dc.Durability())
	}
	// 属性变化后：上限包 + 当前包各一帧（C1 26 FE / FF）。
	if n := countFrames(m.rec, 0xC1, 0x26); n < 2 {
		t.Fatalf("应至少收到 FE+FF 两帧属性包, got %d", n)
	}
}

// TestConsumeFruitRejections 锁定三类门槛拒绝的**双包**行为
// （原版 return false → C1 2C 应答 + C1 26 FD，果实不消耗）：
// 等级不足 → 2；DK 无统率属性 → 2；穿有装备 → 16；上限用尽 → 33。
func TestConsumeFruitRejections(t *testing.T) {
	cases := []struct {
		name     string
		level    uint16
		fruitLvl byte
		used     uint16
		equip    bool
		outcome  int
	}{
		{"等级不足", 9, 0, 0, false, 2},
		{"职业不可增(统率)", 20, 4, 0, false, 2},
		{"穿戴装备", 20, 0, 0, true, 16},
		{"上限用尽", 20, 0, 6, false, 33},
	}
	for _, tc := range cases {
		m := consumeScaffold(t, 1000, 1000, 0)
		m.c.Level = tc.level
		m.c.Stats.UsedFruitPoints = tc.used
		if tc.equip {
			m.putItem(t, 0, &item.Item{Group: 0, Number: 0, Durability: 20})
		}
		fruit := &item.Item{Group: 13, Number: 15, Level: tc.fruitLvl, Durability: 3}
		m.putItem(t, 12, fruit)
		energyBefore := fruitStatValue(m.c.Stats, action.FruitStat(tc.fruitLvl))

		m.srv.handleItemConsume(m.sess, consumeFrameEx(12, 0, 0))

		resp := fruitResponse(t, m.rec)
		if int(resp.Result()) != tc.outcome {
			t.Fatalf("%s: 应答结果=%d, want %d", tc.name, resp.Result(), tc.outcome)
		}
		if resp.StatPoints() != 0 {
			t.Fatalf("%s: 拒绝时点数应为 0, got %d", tc.name, resp.StatPoints())
		}
		if n := len(failedFrames(m.rec)); n != 1 {
			t.Fatalf("%s: 拒绝应回 1 帧 C1 26 FD, got %d", tc.name, n)
		}
		if fruit.Durability != 3 || m.c.Inventory.GetItem(12) == nil {
			t.Fatalf("%s: 拒绝时果实不得消耗", tc.name)
		}
		if got := fruitStatValue(m.c.Stats, action.FruitStat(tc.fruitLvl)); got != energyBefore {
			t.Fatalf("%s: 拒绝不得改变属性 %d → %d", tc.name, energyBefore, got)
		}
		if n := countFrames(m.rec, 0xC1, 0x2A); n != 0 {
			t.Fatalf("%s: 拒绝不得发耐久包, got %d", tc.name, n)
		}
	}
}

// TestConsumeFruitRemovePoints 锁定洗点方向（usage=RemovePoints 线值 1）：
// 结果 3、UsedNegFruit 记账、Energy 下降、果实消耗。
func TestConsumeFruitRemovePoints(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	base := resolveFruitBaseline(m, t)
	m.c.Stats.Energy = base.Energy + 5 // 有可洗的余量（statBase DK Energy=10）
	base = resolveFruitBaseline(m, t)
	energyBefore := base.Energy
	m.putItem(t, 12, &item.Item{Group: 13, Number: 15, Level: 0, Durability: 2})

	m.srv.handleItemConsume(m.sess, consumeFrameEx(12, 0, c2s.FruitUsage(1)))

	resp := fruitResponse(t, m.rec)
	if int(resp.Result()) != 3 { // MinusSuccess
		t.Fatalf("洗点应答应为 3, got %d", resp.Result())
	}
	p := resp.StatPoints()
	if p < 1 || p > 9 {
		t.Fatalf("洗点点数应为 1..9, got %d", p)
	}
	if m.c.Stats.UsedNegFruit != p {
		t.Fatalf("UsedNegFruit=%d, want %d", m.c.Stats.UsedNegFruit, p)
	}
	if m.c.Stats.Energy != energyBefore-p {
		t.Fatalf("Total Energy 应 −%d: %d → %d", p, energyBefore, m.c.Stats.Energy)
	}
	if m.c.Stats.UsedFruitPoints != 0 {
		t.Fatal("洗点不得累加加点计数")
	}
}

// TestConsumeBlessJewelEndToEnd 锁定 Bless（100%）端到端：目标升 1 级、耐久刷新为
// 满值、宝石扣次、C1 F3 14 带目标槽与完整扩展编码。
func TestConsumeBlessJewelEndToEnd(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	kris := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 5}
	m.putItem(t, 12, kris)
	jewel := &item.Item{Group: 14, Number: 13, Durability: 3}
	m.putItem(t, 13, jewel)

	m.srv.handleItemConsume(m.sess, consumeFrameEx(13, 12, 0))

	if len(failedFrames(m.rec)) != 0 {
		t.Fatal("Bless 100% 不应回失败包")
	}
	if kris.Level != 5 {
		t.Fatalf("等级应 +1 → 5, got %d", kris.Level)
	}
	def, ok := m.srv.deps.cfg.GameConfig.Item(0, 0)
	if !ok {
		t.Fatal("缺少 (0,0) 定义")
	}
	if want := pricing.MaximumDurability(def, kris); kris.Durability != want {
		t.Fatalf("成功后耐久应刷新为满值 %d, got %d", want, kris.Durability)
	}
	if jewel.Durability != 2 {
		t.Fatalf("宝石耐久应 3→2, got %d", jewel.Durability)
	}
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 1 {
		t.Fatalf("宝石应发 1 帧 C1 2A, got %d", n)
	}
	if n := countFrames(m.rec, 0xC1, 0xF3); n != 1 {
		t.Fatalf("应发 1 帧 C1 F3 14, got %d", n)
	}
	f := findFrame(m.rec, 0xC1, 0xF3)
	up := s2c.AsInventoryItemUpgraded(f)
	if f[3] != 0x14 {
		t.Fatalf("子码应为 0x14, got %02X", f[3])
	}
	if up.InventorySlot() != 12 {
		t.Fatalf("F3 14 槽位应为 12, got %d", up.InventorySlot())
	}
	if enc := encodeItemForClient(kris); len(up.ItemData()) != len(enc) {
		t.Fatalf("F3 14 物品数据长度应等于扩展编码 %d, got %d", len(enc), len(up.ItemData()))
	}
}

// TestConsumeJewelGuards 锁定强化类宝石的拒绝分支（全部不消耗宝石、无 F3 14）：
// 目标格空、目标在装备区（原版禁止强化已穿戴物）、目标已到 Bless 上界（+6）。
func TestConsumeJewelGuards(t *testing.T) {
	cases := []struct {
		name       string
		targetSlot byte
		target     *item.Item
	}{
		{"目标格空", 20, nil},
		{"装备区目标", 0, &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20}},
		{"已到上界(+6)", 12, &item.Item{Group: 0, Number: 0, Level: 6, Durability: 20}},
	}
	for _, tc := range cases {
		m := consumeScaffold(t, 1000, 1000, 0)
		if tc.target != nil {
			m.putItem(t, tc.targetSlot, tc.target)
		}
		jewel := &item.Item{Group: 14, Number: 13, Durability: 3}
		m.putItem(t, 13, jewel)

		m.srv.handleItemConsume(m.sess, consumeFrameEx(13, tc.targetSlot, 0))

		if n := len(failedFrames(m.rec)); n != 1 {
			t.Fatalf("%s: 应回 1 帧 FD, got %d", tc.name, n)
		}
		if jewel.Durability != 3 {
			t.Fatalf("%s: 拒绝不得消耗宝石, got %d", tc.name, jewel.Durability)
		}
		if n := countFrames(m.rec, 0xC1, 0xF3); n != 0 {
			t.Fatalf("%s: 拒绝不得发升级包, got %d", tc.name, n)
		}
		if tc.target != nil && tc.target.Durability != 20 {
			t.Fatalf("%s: 拒绝不得改目标耐久", tc.name)
		}
	}
}

// TestConsumeLifeJewelNoCandidates 锁定"目标无候选选项 → 宝石不消耗 + 失败包"
// （生命宝石对药水使用：原版 ItemCanHaveOption=false → ModifyItem false）。
func TestConsumeLifeJewelNoCandidates(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.putItem(t, 12, &item.Item{Group: 14, Number: 4, Durability: 3}) // 目标：法力药水
	jewel := &item.Item{Group: 14, Number: 16, Durability: 3}
	m.putItem(t, 13, jewel)

	m.srv.handleItemConsume(m.sess, consumeFrameEx(13, 12, 0))

	if len(failedFrames(m.rec)) != 1 || jewel.Durability != 3 {
		t.Fatalf("无候选应失败且不消耗: FD=%d 宝石耐久=%d", len(failedFrames(m.rec)), jewel.Durability)
	}
}

// TestConsumeAntidoteRemovesPoison 锁定解毒剂：扣耐久销毁 + 移除中毒效果
// （C1 07 去激活，EffectId=0x37=55）；对照 AntidoteConsumeHandlerPlugIn。
func TestConsumeAntidoteRemovesPoison(t *testing.T) {
	const poisonEffect = 0x37
	m := consumeScaffold(t, 1000, 1000, 0)
	gc := m.srv.deps.cfg.GameConfig
	eff, _, ok := player.CreateMagicEffect(gc, m.c, poisonEffect, 0, true)
	if !ok {
		t.Fatal("构造中毒效果失败")
	}
	if !m.sess.getEffects().Add(eff, currentMs()) {
		t.Fatal("中毒效果应入表")
	}
	m.putItem(t, 12, &item.Item{Group: 14, Number: 8, Durability: 1}) // 解毒剂，最后一次

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if m.sess.getEffects().Len() != 0 {
		t.Fatalf("使用后中毒应被移除, got %d", m.sess.getEffects().Len())
	}
	if n := countFrames(m.rec, 0xC1, 0x07); n != 1 {
		t.Fatalf("应发 1 帧 C1 07 去激活, got %d", n)
	}
	st := s2c.AsMagicEffectStatus(findFrame(m.rec, 0xC1, 0x07))
	if st.IsActive() || st.EffectId() != poisonEffect {
		t.Fatalf("去激活包应为 毒(55) inactive, got active=%v id=%d", st.IsActive(), st.EffectId())
	}
	if n := countFrames(m.rec, 0xC1, 0x28); n != 1 {
		t.Fatalf("解毒剂应用尽销毁(C1 28), got %d", n)
	}
	if len(failedFrames(m.rec)) != 0 {
		t.Fatal("解毒剂使用不应回失败包")
	}
}

// TestConsumeTownPortalScroll 锁定瞬间传送卷轴：目标图 = 当前图 SafezoneMap（洛里亚），
// 落点 = 出生门矩形内首个可走格；卷轴消耗 + C3 1C + 进入"换图中"状态。
func TestConsumeTownPortalScroll(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.X, m.c.Y = 200, 200 // 离门远一点，证明确实被挪动
	scroll := &item.Item{Group: 14, Number: 10, Durability: 2}
	m.putItem(t, 12, scroll)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if len(failedFrames(m.rec)) != 0 {
		t.Fatal("传送卷轴不应被拒绝")
	}
	if scroll.Durability != 1 {
		t.Fatalf("卷轴应消耗 2→1, got %d", scroll.Durability)
	}
	if n := countFrames(m.rec, 0xC3, 0x1C); n != 1 {
		t.Fatalf("应发 1 帧 C3 1C MapChanged, got %d", n)
	}
	if got := m.sess.getState(); got != entity.StateEnteringMap {
		t.Fatalf("使用后应处于换图中状态, got %v", got)
	}
	// 洛里亚出生门矩形 (133,118)..(151,135)（60_maps.json）。
	if m.c.MapNumber != 0 || m.c.X < 133 || m.c.X > 151 || m.c.Y < 118 || m.c.Y > 135 {
		t.Fatalf("落点应落在出生门矩形内: map=%d (%d,%d)", m.c.MapNumber, m.c.X, m.c.Y)
	}
	mc := s2c.AsMapChanged(findFrame(m.rec, 0xC3, 0x1C))
	if int(mc.MapNumber()) != 0 || mc.PositionX() != byte(m.c.X) || mc.PositionY() != byte(m.c.Y) {
		t.Fatalf("MapChanged 应与实际落点一致: got (%d,%d) vs (%d,%d)",
			mc.PositionX(), mc.PositionY(), m.c.X, m.c.Y)
	}
}

// TestConsumeOrbSkillNumberFollowsItemLevel 锁定技能石编号偏移：
// Orb of Healing(12,8) 基础技能 26，**等级 2 → 学 28**（SummoningOrbConsumeHandlerPlugIn）。
func TestConsumeOrbSkillNumberFollowsItemLevel(t *testing.T) {
	m := newMoveScaffold(t, 8, entity.CharStats{Energy: 300})
	m.c.Level = 40
	m.wp.Stats = m.c.Stats
	m.putItem(t, 12, &item.Item{Group: 12, Number: 8, Level: 2, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if !learnedSkillContains(m.c, 28) {
		t.Fatalf("应学得 26+2=28, got %v", m.c.LearnedSkills)
	}
	if learnedSkillContains(m.c, 26) {
		t.Fatal("不得误学基础技能号 26")
	}
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("技能石应用尽销毁")
	}
	if n := len(skillListFrames(m.rec)); n != 1 {
		t.Fatalf("应下发 SkillListUpdate, got %d", n)
	}
}

// TestConsumeRefineStoneNeedsHarmonyOption 验证精炼石的 adds=false：目标没有和谐选项时
// 宝石不消耗、回失败包（Lower/Higher 两档同口径，这里取 14,43）。
func TestConsumeRefineStoneNeedsHarmonyOption(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	kris := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 5}
	m.putItem(t, 12, kris)
	stone := &item.Item{Group: 14, Number: 43, Durability: 3}
	m.putItem(t, 13, stone)

	m.srv.handleItemConsume(m.sess, consumeFrameEx(13, 12, 0))

	if len(failedFrames(m.rec)) != 1 {
		t.Fatalf("无和谐选项时应回 1 帧失败包, got %d", len(failedFrames(m.rec)))
	}
	if stone.Durability != 3 {
		t.Fatalf("被拒时精炼石不应消耗, got %d", stone.Durability)
	}
}

// TestConsumeRefineStoneBlockedByMinDamageGuard 锁住武器最小伤害守卫：Kris(6..11) 的
// 和谐 #1 升到 3 档需要 boost 5 → 11-(6+5)<1，与随机数无关地拒绝（守卫在掷骰之前）。
func TestConsumeRefineStoneBlockedByMinDamageGuard(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	kris := &item.Item{Group: 0, Number: 0, Level: 6, Durability: 5, HarmonyNumber: 1, HarmonyLevel: 2}
	m.putItem(t, 12, kris)
	stone := &item.Item{Group: 14, Number: 44, Durability: 1}
	m.putItem(t, 13, stone)

	m.srv.handleItemConsume(m.sess, consumeFrameEx(13, 12, 0))

	if kris.HarmonyLevel != 2 {
		t.Fatalf("守卫应拦住升档，和谐等级仍为 2, got %d", kris.HarmonyLevel)
	}
	if stone.Durability != 1 || len(failedFrames(m.rec)) != 1 {
		t.Fatalf("守卫拦住时宝石不应消耗并回失败包（耐久=%d 失败帧=%d）",
			stone.Durability, len(failedFrames(m.rec)))
	}
}

// TestConsumeRefineStoneUpgradesHarmonyLevel 是"任何随机结果都必须成立"的不变量：
// 有和谐选项的盾牌 + 下阶精炼石 → 宝石必被消耗、等级只能是最低档或 +1、恰一帧 F3 14。
func TestConsumeRefineStoneUpgradesHarmonyLevel(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	shield := &item.Item{Group: 6, Number: 0, Level: 6, Durability: 5, HarmonyNumber: 1, HarmonyLevel: 1}
	m.putItem(t, 12, shield) // 盾牌 2×2 占 12/13/20/21
	stone := &item.Item{Group: 14, Number: 43, Durability: 5}
	m.putItem(t, 22, stone)

	m.srv.handleItemConsume(m.sess, consumeFrameEx(22, 12, 0))

	if len(failedFrames(m.rec)) != 0 {
		t.Fatalf("有和谐选项且可升档时不应回失败包")
	}
	if shield.HarmonyLevel != 0 && shield.HarmonyLevel != 2 {
		t.Fatalf("和谐等级只能是最低档 0（失败回档）或 2（成功）, got %d", shield.HarmonyLevel)
	}
	if stone.Durability != 4 {
		t.Fatalf("精炼石应被消耗 1 次, got %d", stone.Durability)
	}
	if n := countFrames(m.rec, 0xC1, 0xF3); n != 1 {
		t.Fatalf("应发 1 帧 C1 F3 14 刷新目标, got %d", n)
	}
}
