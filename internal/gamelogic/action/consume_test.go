package action

// consume_test.go —— T2-6 药水恢复决策混合测试：配方表查找、恢复公式黄金值、
// 分段/一次性判据、复合药水按步聚合、上限钳制。
//
// 黄金值全部按 OpenMU `RecoverConsumeHandlerPlugIn.RecoverAsync` 手算写入，
// 而不是"跑一遍代码看结果"——否则公式写错时测试会跟着一起错（变异检验失效）。

import (
	"math"
	"testing"
)

// near 浮点比较（恢复量是 double 运算，末位可能有 1e-9 级误差）。
func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// partOf 取某 (group, number) 药水的第 idx 个恢复项；不存在则 Fatal。
func partOf(t *testing.T, group, number, idx int) RecoverPart {
	t.Helper()
	r, ok := PotionRecipeFor(group, number)
	if !ok {
		t.Fatalf("配方缺失 group=%d number=%d", group, number)
	}
	if idx >= len(r.Parts) {
		t.Fatalf("group=%d number=%d 只有 %d 个恢复项", group, number, len(r.Parts))
	}
	return r.Parts[idx]
}

// TestPotionRecipeLookup 锁定 (group, number) 键序：键写成 (Number, Group) 会整体查不到。
func TestPotionRecipeLookup(t *testing.T) {
	cases := []struct {
		group, number int
		kind          PotionKind
		parts         int
	}{
		{14, 0, PotionApple, 1},    // Apple
		{14, 1, PotionHealth, 1},   // Small Healing Potion
		{14, 3, PotionHealth, 1},   // Large Healing Potion
		{14, 4, PotionMana, 1},     // Small Mana Potion
		{14, 6, PotionMana, 1},     // Large Mana Potion
		{14, 35, PotionShield, 1},  // Small Shield Potion
		{14, 37, PotionShield, 1},  // Large Shield Potion
		{14, 38, PotionComplex, 2}, // Small Complex Potion（血 + 盾）
		{14, 40, PotionComplex, 2}, // Large Complex Potion
	}
	for _, c := range cases {
		r, ok := PotionRecipeFor(c.group, c.number)
		if !ok {
			t.Fatalf("(%d,%d) 应命中配方", c.group, c.number)
		}
		if r.Kind != c.kind {
			t.Fatalf("(%d,%d) 种类=%d, want %d", c.group, c.number, r.Kind, c.kind)
		}
		if len(r.Parts) != c.parts {
			t.Fatalf("(%d,%d) 恢复项数=%d, want %d", c.group, c.number, len(r.Parts), c.parts)
		}
		if r.CooldownMs != 500 {
			t.Fatalf("(%d,%d) 冷却=%dms, want 500ms", c.group, c.number, r.CooldownMs)
		}
	}
	// 键序反写必须查不到（这是最容易犯的错，必须被锁住）。
	if _, ok := PotionRecipeFor(1, 14); ok {
		t.Fatal("键序 (number, group) 不应命中——配方表按 (group, number) 索引")
	}
	// 未实现分支：武器不是药水。
	if _, ok := PotionRecipeFor(0, 0); ok {
		t.Fatal("武器不应命中药水配方")
	}
}

// TestPlanRecoveryHealthGolden 锁定生命药水公式与分段判据（黄金值手算）。
//
// Small Healing Potion：Multiplier=1 → TotalRecoverPercentage=10、
// AdditionalRecoverMinusCharacterLevel=(1+1)×50=100、IncreaseByPotionLevel=1、
// DelayReductionByPotionLevel=1/16。
func TestPlanRecoveryHealthGolden(t *testing.T) {
	p := partOf(t, 14, 1, 0)
	if p.Target != RecoverHealth {
		t.Fatalf("目标属性应为 Health, got %d", p.Target)
	}
	if !near(p.TotalRecoverPercentage, 10) || !near(p.AdditionalRecoverMinusCharacterLevel, 100) {
		t.Fatalf("配方常量异常: total%%=%v additional=%v",
			p.TotalRecoverPercentage, p.AdditionalRecoverMinusCharacterLevel)
	}

	// 等级 0、角色 20 级、上限 1000：
	//   percentage = 10 + 0×1 = 10
	//   additional = max(0, 100 − 20) = 80
	//   total      = 1000×10/100 + 80 = 180
	//   delayReduction = 0 → 三段 20/60/20 @ 200/600/200ms → 36 / 108 / 36
	segs := PlanRecovery(p, 1000, 0, 20)
	if len(segs) != 3 {
		t.Fatalf("等级 0 应走三段, got %d", len(segs))
	}
	wantDelay := []int{200, 600, 200}
	wantAmt := []float64{36, 108, 36}
	for i := range segs {
		if segs[i].DelayMs != wantDelay[i] || !near(segs[i].Amount, wantAmt[i]) {
			t.Fatalf("第 %d 段=(%dms, %v), want (%dms, %v)",
				i, segs[i].DelayMs, segs[i].Amount, wantDelay[i], wantAmt[i])
		}
	}
	// 三段之和必须等于未分摊总量（百分比之和为 100）。
	if sum := segs[0].Amount + segs[1].Amount + segs[2].Amount; !near(sum, 180) {
		t.Fatalf("三段之和=%v, want 180", sum)
	}

	// 等级 16：delayReduction = 16×(1/16) = 1 → 一次性。
	//   percentage = 10 + 16 = 26；additional = 80；total = 260 + 80 = 340
	one := PlanRecovery(p, 1000, 16, 20)
	if len(one) != 1 || one[0].DelayMs != 0 {
		t.Fatalf("等级 16 应一次性, got %+v", one)
	}
	if !near(one[0].Amount, 340) {
		t.Fatalf("等级 16 一次性恢复=%v, want 340", one[0].Amount)
	}

	// 等级 8：delayReduction = 0.5 → 段延迟减半（100/300/100），总量含等级加成。
	//   percentage = 10 + 8 = 18；total = 180 + 80 = 260 → 52 / 156 / 52
	lvl8 := PlanRecovery(p, 1000, 8, 20)
	if len(lvl8) != 3 {
		t.Fatalf("等级 8 应仍分段, got %d", len(lvl8))
	}
	wantDelay8 := []int{100, 300, 100}
	wantAmt8 := []float64{52, 156, 52}
	for i := range lvl8 {
		if lvl8[i].DelayMs != wantDelay8[i] || !near(lvl8[i].Amount, wantAmt8[i]) {
			t.Fatalf("等级8 第 %d 段=(%dms, %v), want (%dms, %v)",
				i, lvl8[i].DelayMs, lvl8[i].Amount, wantDelay8[i], wantAmt8[i])
		}
	}
}

// TestPlanRecoveryAppleLowLevelAnchor 锁定苹果的经典语义：基础百分比为 0，
// **只**靠"角色等级补偿"回血（Additional − charLevel）。
func TestPlanRecoveryAppleLowLevelAnchor(t *testing.T) {
	p := partOf(t, 14, 0, 0)
	if !near(p.TotalRecoverPercentage, 0) {
		t.Fatalf("苹果基础百分比应为 0, got %v", p.TotalRecoverPercentage)
	}
	if !near(p.AdditionalRecoverMinusCharacterLevel, 50) {
		t.Fatalf("苹果补偿基数应为 (0+1)×50=50, got %v", p.AdditionalRecoverMinusCharacterLevel)
	}
	// 20 级、上限 1000：total = 1000×0/100 + (50−20) = 30。
	segs := PlanRecovery(p, 1000, 0, 20)
	if !near(segs[0].Amount+segs[1].Amount+segs[2].Amount, 30) {
		t.Fatalf("20 级苹果总恢复=%v, want 30",
			segs[0].Amount+segs[1].Amount+segs[2].Amount)
	}
	// 60 级：补偿被钳到 0 → 全 0（原版 max(0, ...)）。
	high := PlanRecovery(p, 1000, 0, 60)
	for i, s := range high {
		if !near(s.Amount, 0) {
			t.Fatalf("60 级苹果第 %d 段=%v, want 0（补偿为负应钳零）", i, s.Amount)
		}
	}
}

// TestPlanRecoveryShieldNoLevelCompensation 锁定护盾药水**没有**角色等级补偿项
// （原版 ShieldPotionConsumeHandlerConfiguration 不含 AdditionalRecoverMinusCharacterLevel）。
// 若误用 healthPart 生成护盾段，60 级角色会凭空多回 0 点、20 级会多回 30 点——本测试锁死。
func TestPlanRecoveryShieldNoLevelCompensation(t *testing.T) {
	p := partOf(t, 14, 35, 0)
	if p.Target != RecoverShield {
		t.Fatalf("目标应为 Shield, got %d", p.Target)
	}
	if !near(p.TotalRecoverPercentage, 20) || !near(p.AdditionalRecoverMinusCharacterLevel, 0) {
		t.Fatalf("护盾配方常量异常: total%%=%v additional=%v",
			p.TotalRecoverPercentage, p.AdditionalRecoverMinusCharacterLevel)
	}
	// 上限 500、1 级角色：total = 500×20/100 + 0 = 100（**不**含 100−1=99 的补偿）。
	segs := PlanRecovery(p, 500, 0, 1)
	if !near(segs[0].Amount, 20) || !near(segs[1].Amount, 60) || !near(segs[2].Amount, 20) {
		t.Fatalf("护盾三段=%v/%v/%v, want 20/60/20（无等级补偿）",
			segs[0].Amount, segs[1].Amount, segs[2].Amount)
	}
}

// TestPlanPotionRecoveryComplexCoalesces 锁定复合药水按"步"聚合：
// 生命段与护盾段并发推进 → 每步只出**一**批（客户端每步只收一包 C1 26 FF）。
func TestPlanPotionRecoveryComplexCoalesces(t *testing.T) {
	r, ok := PotionRecipeFor(14, 38) // Small Complex Potion：Health(mult 1) + Shield(20%)
	if !ok {
		t.Fatal("Small Complex Potion 配方缺失")
	}
	maxima := map[RecoverTarget]float64{RecoverHealth: 1000, RecoverShield: 500}
	batches := PlanPotionRecovery(r, maxima, 0, 20)
	if len(batches) != 3 {
		t.Fatalf("复合药水应聚合成 3 批, got %d", len(batches))
	}
	wantDelay := []int{200, 600, 200}
	wantHealth := []float64{36, 108, 36}
	wantShield := []float64{20, 60, 20}
	for i, b := range batches {
		if b.DelayMs != wantDelay[i] {
			t.Fatalf("第 %d 批延迟=%d, want %d", i, b.DelayMs, wantDelay[i])
		}
		if !near(b.Amounts[RecoverHealth], wantHealth[i]) {
			t.Fatalf("第 %d 批生命=%v, want %v", i, b.Amounts[RecoverHealth], wantHealth[i])
		}
		if !near(b.Amounts[RecoverShield], wantShield[i]) {
			t.Fatalf("第 %d 批护盾=%v, want %v", i, b.Amounts[RecoverShield], wantShield[i])
		}
		if len(b.Amounts) != 2 {
			t.Fatalf("第 %d 批应含 2 个目标属性, got %d", i, len(b.Amounts))
		}
	}
}

// TestPlanPotionRecoverySinglePart 锁定单项目配方只产生一批（避免白跑定时器）。
func TestPlanPotionRecoverySinglePart(t *testing.T) {
	r, _ := PotionRecipeFor(14, 3) // Large Healing Potion
	maxima := map[RecoverTarget]float64{RecoverHealth: 800}
	batches := PlanPotionRecovery(r, maxima, 0, 30)
	if len(batches) != 3 {
		t.Fatalf("应 3 批, got %d", len(batches))
	}
	for i, b := range batches {
		if len(b.Amounts) != 1 {
			t.Fatalf("第 %d 批应只有 Health, got %+v", i, b.Amounts)
		}
	}
	// 空 Parts 不应 panic，也不应产生批次。
	if got := PlanPotionRecovery(PotionRecipe{}, maxima, 0, 30); got != nil {
		t.Fatalf("空配方应返回 nil, got %+v", got)
	}
}

// TestApplyRecoverClampsToMaximum 锁定 `Math.Min(Maximum, Current + amount)` 与负值钳零。
func TestApplyRecoverClampsToMaximum(t *testing.T) {
	cases := []struct {
		current, maximum, amount float64
		want                     uint32
	}{
		{100, 1000, 340, 440},  // 常规
		{900, 1000, 340, 1000}, // 触顶
		{1000, 1000, 50, 1000}, // 已满
		{0, 1000, 30, 30},      // 空血
		{5, 1000, -10, 0},      // 负增量钳零（防御分支）
		{0, 0, 100, 0},         // 上限为 0（无护盾职业）
	}
	for _, c := range cases {
		if got := ApplyRecover(c.current, c.maximum, c.amount); got != c.want {
			t.Fatalf("ApplyRecover(%v,%v,%v)=%d, want %d",
				c.current, c.maximum, c.amount, got, c.want)
		}
	}
}

// TestItemsNeedingSkill 锁定卷轴分支判据（有技能且不可穿戴），供后续接卷轴时复用。
func TestItemsNeedingSkill(t *testing.T) {
	if !ItemsNeedingSkill(true, false) {
		t.Fatal("有技能且不可穿戴应命中卷轴分支")
	}
	if ItemsNeedingSkill(true, true) {
		t.Fatal("可穿戴物品即使带技能也不走卷轴分支")
	}
	if ItemsNeedingSkill(false, false) {
		t.Fatal("无技能不应命中卷轴分支")
	}
}

// ---- MagicEffect 基础设施 ----

// TestMagicEffectsAddAndExpire 锁定到期判据 `nowMs >= ExpiresAtMs` 与移除顺序。
func TestMagicEffectsAddAndExpire(t *testing.T) {
	l := NewMagicEffectsList()
	if l.Len() != 0 {
		t.Fatal("新表应为空")
	}
	if added := l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 5, DurationMs: 3000}}, 1000); !added {
		t.Fatal("首次添加应返回 added=true（才会下发激活包）")
	}
	if l.Len() != 1 {
		t.Fatalf("效果数=%d, want 1", l.Len())
	}
	// 到期前 1ms 不触发。
	if got := l.Expire(3999); got != nil {
		t.Fatalf("未到期不应移除: %+v", got)
	}
	// 恰好到期即触发（>= 语义）。
	got := l.Expire(4000)
	if len(got) != 1 || got[0].Definition.Number != 5 {
		t.Fatalf("到期应移除 1 个效果, got %+v", got)
	}
	if l.Len() != 0 {
		t.Fatalf("移除后应为空, got %d", l.Len())
	}
	// 空表不应 panic。
	if got := l.Expire(9999); got != nil {
		t.Fatalf("空表应返回 nil, got %+v", got)
	}
}

// TestMagicEffectsNoDebuffUpdate 锁定原版"同编号更新不允许减益"：
// 新值不大于旧值 → 什么都不做（连时长都不刷新）。
func TestMagicEffectsNoDebuffUpdate(t *testing.T) {
	l := NewMagicEffectsList()
	def := MagicEffectDefinition{Number: 7, DurationMs: 2000}
	l.Add(MagicEffect{Definition: def, PowerUps: []MagicEffectPowerUp{{Attribute: "Defense", Value: 10}}}, 1000)

	// 更弱 → 忽略。
	if added := l.Add(MagicEffect{Definition: def, PowerUps: []MagicEffectPowerUp{{Attribute: "Defense", Value: 5}}}, 1500); added {
		t.Fatal("同编号更新不应返回 added=true")
	}
	if v := l.PowerUpsFor("Defense"); !near(float64(v), 10) {
		t.Fatalf("弱效果不得覆盖: Defense=%v, want 10", v)
	}
	if snap := l.Snapshot(); snap[0].ExpiresAtMs != 3000 {
		t.Fatalf("被忽略的更新不得刷时长: ExpiresAtMs=%d, want 3000", snap[0].ExpiresAtMs)
	}

	// 更强 → 刷新时长 + 替换 PowerUp，但仍不下发激活包。
	if added := l.Add(MagicEffect{Definition: def, PowerUps: []MagicEffectPowerUp{{Attribute: "Defense", Value: 20}}}, 2000); added {
		t.Fatal("强化更新也不返回 added=true（原版只下发首次激活）")
	}
	if v := l.PowerUpsFor("Defense"); !near(float64(v), 20) {
		t.Fatalf("强效果应替换: Defense=%v, want 20", v)
	}
	if snap := l.Snapshot(); snap[0].ExpiresAtMs != 4000 {
		t.Fatalf("强化更新应刷时长: ExpiresAtMs=%d, want 4000", snap[0].ExpiresAtMs)
	}
}

// TestMagicEffectVisibility 锁定两条可见性判据：编号 0/≥200 不给自己发；观察者还要 InformObservers。
func TestMagicEffectVisibility(t *testing.T) {
	cases := []struct {
		number          int16
		informObservers bool
		toSelf          bool
		toObservers     bool
	}{
		{5, false, true, false},
		{5, true, true, true},
		{0, true, false, false},   // Number<=0：原版插件直接 return
		{199, false, true, false}, // 边界内
		{200, true, false, false}, // InvisibleEffectStartIndex：不可见
		{250, true, false, false},
	}
	for _, c := range cases {
		e := MagicEffect{Definition: MagicEffectDefinition{Number: c.number, InformObservers: c.informObservers}}
		if got := e.SendsToSelf(); got != c.toSelf {
			t.Fatalf("Number=%d InformObservers=%v SendsToSelf=%v, want %v",
				c.number, c.informObservers, got, c.toSelf)
		}
		if got := e.SendsToObservers(); got != c.toObservers {
			t.Fatalf("Number=%d InformObservers=%v SendsToObservers=%v, want %v",
				c.number, c.informObservers, got, c.toObservers)
		}
	}
}

// TestMagicEffectRemainingSeconds 锁定 SendDuration 的秒数取整与到期归零。
func TestMagicEffectRemainingSeconds(t *testing.T) {
	e := MagicEffect{ExpiresAtMs: 5000}
	if got := e.RemainingSeconds(2000); got != 3 {
		t.Fatalf("剩余=%d, want 3", got)
	}
	if got := e.RemainingSeconds(5000); got != 0 {
		t.Fatalf("到期应为 0, got %d", got)
	}
	if got := e.RemainingSeconds(6000); got != 0 {
		t.Fatalf("过期应为 0（不得为负）, got %d", got)
	}
}

// TestMagicEffectsClearByDeath 锁定死亡只清 StopByDeath 的效果，且按编号升序返回。
func TestMagicEffectsClearByDeath(t *testing.T) {
	l := NewMagicEffectsList()
	l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 30, DurationMs: 10000, StopByDeath: true}}, 0)
	l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 10, DurationMs: 10000, StopByDeath: true}}, 0)
	l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 20, DurationMs: 10000}}, 0)

	got := l.ClearByDeath()
	if len(got) != 2 {
		t.Fatalf("应清 2 个, got %d", len(got))
	}
	if got[0].Definition.Number != 10 || got[1].Definition.Number != 30 {
		t.Fatalf("返回顺序应按编号升序, got %d,%d", got[0].Definition.Number, got[1].Definition.Number)
	}
	if l.Len() != 1 {
		t.Fatalf("不随死亡清除的效果应保留, 剩余 %d", l.Len())
	}
	// 已空时再清不应 panic。
	l.ClearByDeath()
	if got := l.ClearByDeath(); got != nil {
		t.Fatalf("无 StopByDeath 效果应返回 nil, got %+v", got)
	}
}

// TestMagicEffectsReplaceBySubType 锁定"同子类互斥"：按 SubType 顶替旧效果。
func TestMagicEffectsReplaceBySubType(t *testing.T) {
	l := NewMagicEffectsList()
	l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 11, SubType: 3, DurationMs: 1000}}, 0)
	l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 12, SubType: 4, DurationMs: 1000}}, 0)

	old, ok := l.ReplaceBySubType(3)
	if !ok || old.Definition.Number != 11 {
		t.Fatalf("应顶替出编号 11, got %+v ok=%v", old, ok)
	}
	if l.Len() != 1 {
		t.Fatalf("顶替后应剩 1 个, got %d", l.Len())
	}
	if _, ok := l.ReplaceBySubType(9); ok {
		t.Fatal("不存在的子类不应命中")
	}
}

// TestMagicEffectsPowerUpsAggregate 锁定多效果同属性加成求和 + 可见性快照判据。
func TestMagicEffectsPowerUpsAggregate(t *testing.T) {
	l := NewMagicEffectsList()
	l.Add(MagicEffect{
		Definition: MagicEffectDefinition{Number: 40, DurationMs: 1000, InformObservers: true},
		PowerUps:   []MagicEffectPowerUp{{Attribute: "Defense", Value: 12}, {Attribute: "AttackSpeed", Value: 5}},
	}, 0)
	l.Add(MagicEffect{
		Definition: MagicEffectDefinition{Number: 41, DurationMs: 1000},
		PowerUps:   []MagicEffectPowerUp{{Attribute: "Defense", Value: 8}},
	}, 0)

	if got := l.PowerUpsFor("Defense"); !near(float64(got), 20) {
		t.Fatalf("Defense 聚合=%v, want 20", got)
	}
	if got := l.PowerUpsFor("AttackSpeed"); !near(float64(got), 5) {
		t.Fatalf("AttackSpeed 聚合=%v, want 5", got)
	}
	if got := l.PowerUpsFor("Nonexistent"); got != 0 {
		t.Fatalf("未知属性应为 0, got %v", got)
	}
	// 只有 InformObservers 的效果进可见快照。
	vis := l.VisibilitySnapshot()
	if len(vis) != 1 || vis[0].Definition.Number != 40 {
		t.Fatalf("可见快照应只含编号 40, got %+v", vis)
	}
}

// TestMagicEffectsNilSafe 锁定零值表不 panic（表可能未初始化就被查询）。
func TestMagicEffectsNilSafe(t *testing.T) {
	var l *MagicEffectsList
	if l.Len() != 0 {
		t.Fatal("nil 表长度应为 0")
	}
	if added := l.Add(MagicEffect{Definition: MagicEffectDefinition{Number: 1}}, 0); added {
		t.Fatal("nil 表添加应返回 false")
	}
	if got := l.Expire(1); got != nil {
		t.Fatalf("nil 表到期应为 nil, got %+v", got)
	}
	if got := l.ClearByDeath(); got != nil {
		t.Fatalf("nil 表死亡清除应为 nil, got %+v", got)
	}
	if got := l.Snapshot(); got != nil {
		t.Fatalf("nil 表快照应为 nil, got %+v", got)
	}
}
