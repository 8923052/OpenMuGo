package config

// magic_effect_pvp_test.go —— 效果 PvP 三元组的切换口径（TRIM-06b）。
// 原版按"目标是不是 Player"整套换用 DurationPvp/ChancePvp/PowerUpsPvp
// （AttackableExtensions.cs:348-355），回落规则逐字段不同：时长与概率为 null 时
// 回落 PvE 值，power-up 列表**为空**时才回落（MagicEffectPowerUpExtensions.cs:41/43/51）。
// 断言数值来自 data/season6/35_magic_effects.json。

import "testing"

// effectByName 按编号+名字取定义（S6 里 55/56/58/76/86 号重号，按号查询只能拿到第一条）。
func effectByName(t *testing.T, cfg *GameConfig, number int, name string) *MagicEffectDefinition {
	t.Helper()
	for i := range cfg.MagicEffects {
		if e := &cfg.MagicEffects[i]; e.Number == number && e.Name == name {
			return e
		}
	}
	t.Fatalf("找不到效果 %d(%s)", number, name)
	return nil
}

func TestForTargetSwitchesWholeTriplet(t *testing.T) {
	cfg := mustLoad(t)
	// 编号 76 在 S6 数据里重号（Killing Blow 版与 Summoner 版），按号查询取先出现的
	// Killing Blow 版（原版 GetMagicEffectByNumber 也是 First 语义）；带 PvP 三元组的是后者。
	if first, ok := cfg.MagicEffectByNumber(76); !ok || first.Name != "Weakness Effect (Killing Blow)" {
		t.Fatalf("76 应先命中 Killing Blow 版，got %+v", first)
	}
	weak := effectByName(t, cfg, 76, "Weakness Effect (Summoner)")
	pveDur, pveChance, pveUps := weak.ForTarget(false)
	pvpDur, pvpChance, pvpUps := weak.ForTarget(true)

	if pveDur.Constant != 4 || pvpDur.Constant != 5 {
		t.Fatalf("时长应 PvE 4s / PvP 5s，got %v/%v", pveDur.Constant, pvpDur.Constant)
	}
	if pveChance == nil || pvpChance == nil || pveChance.Constant != 0.32 || pvpChance.Constant != 0.17 {
		t.Fatalf("概率应 PvE .32 / PvP .17，got %v/%v", pveChance, pvpChance)
	}
	if len(pveUps) != 1 || pveUps[0].Boost.Constant != 0.04 {
		t.Fatalf("PvE power-up 应 1 条 0.04，got %+v", pveUps)
	}
	if len(pvpUps) != 1 || pvpUps[0].Boost.Constant != 0.03 {
		t.Fatalf("PvP power-up 应 1 条 0.03，got %+v", pvpUps)
	}
	// PvP 那条关系式的输入集合也不同（原版 PvP 版按等级缩放）。
	if len(pvpDur.Related) != 2 || len(pveDur.Related) != 1 {
		t.Fatalf("时长关系项数应 PvE 1 / PvP 2，got %d/%d", len(pveDur.Related), len(pvpDur.Related))
	}
}

func TestForTargetFallsBackPerField(t *testing.T) {
	cfg := mustLoad(t)

	// 72 Sleep：带 PvP 时长/概率，但没有 PvP power-up 列表 → 整套里只有乘区回落。
	sleep, ok := cfg.MagicEffectByNumber(72)
	if !ok {
		t.Fatal("效果 72 不存在")
	}
	if len(sleep.PowerUpsPvp) != 0 {
		t.Fatalf("72 不该带 PvP power-up，got %+v", sleep.PowerUpsPvp)
	}
	pvpDur, pvpChance, pvpUps := sleep.ForTarget(true)
	if pvpDur.Constant != 4 || pvpChance == nil || pvpChance.Constant != 0.15 {
		t.Fatalf("72 打玩家应用 PvP 时长 4s / 概率 .15，got %v", pvpDur.Constant)
	}
	if len(pvpUps) != len(sleep.PowerUps) || pvpUps[0].Target != "Is asleep" {
		t.Fatalf("72 的 power-up 应回落 PvE 的 Is asleep，got %+v", pvpUps)
	}

	// 132 Decrease Block：只有 PvP power-up，时长/概率为 null → 反向回落 PvE。
	block, ok := cfg.MagicEffectByNumber(132)
	if !ok {
		t.Fatal("效果 132 不存在")
	}
	if block.DurationPvp != nil || block.ChancePvp != nil {
		t.Fatal("132 不该带 PvP 时长/概率")
	}
	pveDur, pveChance, pveUps := block.ForTarget(false)
	_, _, pvpUps = block.ForTarget(true)
	if pveDur.Constant != 10 || pveChance == nil || pveChance.Constant != 0.1 {
		t.Fatalf("132 两条路径都该用 PvE 时长 10s / 概率 .1")
	}
	if pveUps[0].Boost.Constant != -50 || pvpUps[0].Boost.Constant != -20 {
		t.Fatalf("132 减格挡应 PvE -50 / PvP -20，got %v/%v",
			pveUps[0].Boost.Constant, pvpUps[0].Boost.Constant)
	}
}

func TestForTargetIdenticalWhenNoPvpData(t *testing.T) {
	cfg := mustLoad(t)
	withPvp := 0
	for i := range cfg.MagicEffects {
		e := &cfg.MagicEffects[i]
		if e.ChancePvp != nil || e.DurationPvp != nil || len(e.PowerUpsPvp) > 0 {
			withPvp++
			continue
		}
		pveDur, pveChance, pveUps := e.ForTarget(false)
		pvpDur, pvpChance, pvpUps := e.ForTarget(true)
		if pveDur.Constant != pvpDur.Constant || len(pveDur.Related) != len(pvpDur.Related) ||
			pveChance != pvpChance || len(pveUps) != len(pvpUps) {
			t.Fatalf("效果 %d(%s) 无 PvP 数据却切换了", e.Number, e.Name)
		}
	}
	// S6 全量里只有 4 条效果带 PvP 三元组。
	if withPvp != 4 {
		t.Fatalf("带 PvP 三元组的效果数 %d, want 4（72/76/77/132）", withPvp)
	}
}
