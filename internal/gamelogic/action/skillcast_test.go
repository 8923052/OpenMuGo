package action

// skillcast_test.go —— T2-11 技能施放判定分支（对照 TargetedSkillDefaultPlugin +
// TryConsumeForSkillAsync）。

import "testing"

// twistingSlashDef 对齐导出件 (41) Twisting Slash：物理近战 AOE，
// 消耗 MP 10 + AG 10。
func twistingSlashDef() *SkillDef {
	return &SkillDef{
		Number: 41, SkillType: SkillTypeAreaSkillAutomatic, Target: SkillTargetExplicit,
		DamageType: DamageTypePhysical, Range: 2, AttackDamage: 0, HitsPerAttack: 1,
		Consume: []SkillConsume{{Attribute: desSkillMana, Value: 10}, {Attribute: desSkillAG, Value: 10}},
	}
}

// energyBallDef 对齐导出件 (17) Energy Ball：魔法定向，MP 1。
func energyBallDef() *SkillDef {
	return &SkillDef{
		Number: 17, SkillType: SkillTypeDirectHit, Target: SkillTargetExplicit,
		DamageType: DamageTypeWizardry, Range: 6, AttackDamage: 3,
		Consume: []SkillConsume{{Attribute: desSkillMana, Value: 1}},
	}
}

func TestCastTargetedOK(t *testing.T) {
	res := CastTargeted(true, energyBallDef(), false, true, 100, 50)
	if res.Outcome != SkillOK || res.Mana != 99 || res.AG != 50 {
		t.Fatalf("res=%+v", res)
	}
}

func TestCastTargetedUnknownSkill(t *testing.T) {
	if res := CastTargeted(false, energyBallDef(), false, true, 100, 50); res.Outcome != SkillUnknown {
		t.Fatalf("未学习应拒: %+v", res)
	}
	// Buff/Regeneration 随各自子系统接入——当前同样按不可施放处理。
	heal := &SkillDef{Number: 26, SkillType: SkillTypeRegeneration, Target: SkillTargetExplicit, Range: 6}
	if res := CastTargeted(true, heal, false, true, 100, 50); res.Outcome != SkillUnknown {
		t.Fatalf("regeneration 应拒: %+v", res)
	}
}

func TestCastTargetedInSafezone(t *testing.T) {
	if res := CastTargeted(true, energyBallDef(), true, true, 100, 50); res.Outcome != SkillInSafezone {
		t.Fatalf("安全区应拒: %+v", res)
	}
}

func TestCastTargetedOutOfRange(t *testing.T) {
	if res := CastTargeted(true, energyBallDef(), false, false, 100, 50); res.Outcome != SkillTargetOutOfRange {
		t.Fatalf("超距应拒: %+v", res)
	}
}

func TestCastTargetedNoMana(t *testing.T) {
	res := CastTargeted(true, energyBallDef(), false, true, 0, 50)
	if res.Outcome != SkillNoMana {
		t.Fatalf("没蓝应拒: %+v", res)
	}
	// 原版 TryConsumeForSkillAsync：任一不足则**整笔不扣**。
	if res := CastTargeted(true, twistingSlashDef(), false, true, 10, 0); res.Outcome != SkillNoMana {
		t.Fatalf("AG 不足应拒: %+v", res)
	}
}

func TestCastAreaOK(t *testing.T) {
	res := CastArea(true, twistingSlashDef(), false, 50, 50)
	if res.Outcome != SkillOK || res.Mana != 40 || res.AG != 40 {
		t.Fatalf("res=%+v", res)
	}
	if res := CastArea(true, twistingSlashDef(), true, 50, 50); res.Outcome != SkillInSafezone {
		t.Fatalf("安全区应拒: %+v", res)
	}
}

func TestSkillCostHelpers(t *testing.T) {
	if got := energyBallDef().SkillManaCost(); got != 1 {
		t.Fatalf("mana = %d, want 1", got)
	}
	if got := energyBallDef().SkillAGCost(); got != 0 {
		t.Fatalf("ag = %d, want 0（导出件无 AG 项）", got)
	}
	if got := twistingSlashDef().SkillAGCost(); got != 10 {
		t.Fatalf("twisting ag = %d, want 10", got)
	}
}

func TestSkillDamageBounds(t *testing.T) {
	lo, hi := SkillDamageBounds(45) // Evil Spirit
	if lo != 45 || hi != 67 {       // 45 + 45/2 = 67
		t.Fatalf("bounds = [%d,%d], want [45,67]", lo, hi)
	}
}
