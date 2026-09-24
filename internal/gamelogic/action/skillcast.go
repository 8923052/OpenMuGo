package action

// skillcast.go —— T2-11 技能施放判定（原版 GameLogic/PlayerActions/Skills/
// TargetedSkillDefaultPlugin.cs + AreaSkillAttackAction.cs + Player.TryConsumeForSkillAsync）。
//
// 本文件只做**纯决策**：技能可用性、目标范围、消耗扣减与伤害组成
// （GetSkillDmg/GetBaseDmg 的主干）；出站动画与死亡掉落由 gameserver 装配。
//
// 施放链路（原版 TargetedSkillDefaultPlugin.PerformSkillAsync 顺序）：
//   1. 技能存在于施法者技能列表且非 PassiveBoost；
//   2. 安全区内禁放（Buff/Regeneration 豁免）；
//   3. 目标存活、距离 ≤ skill.Range + 2；
//   4. TryConsumeForSkillAsync：ConsumeRequirements（CurrentMana/CurrentAbility 等）
//      任一不足 → 拒；全满足 → 逐项扣减；
//   5. 目标选择（DetermineTargets）+ 伤害（AttackByAsync → CalculateDamageAsync）；
//   6. ShowSkillAnimation / ShowAreaSkillAnimation 广播（自己 + 观察者）。

// SkillDef 是技能判定所需的定义投影（由调用方从 config.Skill 投影，action 包不依赖 config）。
type SkillDef struct {
	Number              int
	SkillType           int // 原版 SkillType：0 DirectHit、3 AreaSkillAutomaticHits、4 ExplicitHits、5 ExplicitTarget、10 Buff、11 Regeneration
	Target              int // 原版 SkillTarget：1 Explicit、5 ImplicitAllInRange、7 ImplicitPlayer…
	DamageType          int // 原版 DamageType：0 Physical、1 Wizardry、2 Curse
	Range               int
	ImplicitTargetRange int
	AttackDamage        int
	HitsPerAttack       int
	// Consume 为施放消耗（原版 ConsumeRequirements：designation → 数量，如
	// "Current Mana"/"Current Ability"）。逐项检查并扣减。
	Consume []SkillConsume
	// MagicEffectNumber 为挂接的 MagicEffect（buff/异常状态）；0 = 无。
	MagicEffectNumber int
}

// SkillConsume 是一条消耗要求（原版 AttributeRequirement）。
type SkillConsume struct {
	Attribute string
	Value     int
}

// SkillType / SkillTarget / DamageType 常量（数值与原版枚举一致）。
const (
	SkillTypeDirectHit             = 0
	SkillTypeCastleSiegeSpecial    = 1
	SkillTypeCastleSiegeSkill      = 2
	SkillTypeAreaSkillAutomatic    = 3
	SkillTypeAreaSkillExplicitHits = 4
	SkillTypeAreaSkillExplicitTgt  = 5
	SkillTypeBuff                  = 10
	SkillTypeRegeneration          = 11
	SkillTypePassiveBoost          = 20 // 被动加成，无需施放
	SkillTypeSummonMonster         = 30 // 召唤（随召唤子系统）
	SkillTypeOther                 = 40 // 其它（传送等工具）
)

const (
	SkillTargetUndefined            = 0
	SkillTargetExplicit             = 1
	SkillTargetImplicitParty        = 2
	SkillTargetImplicitPlayersInRng = 3
	SkillTargetImplicitNpcsInRng    = 4
	SkillTargetImplicitAllInRange   = 5
	SkillTargetExplicitWithImplicit = 6 // 主目标 + 其 ImplicitTargetRange 内 splash
	SkillTargetImplicitPlayer       = 7
)

const (
	DamageTypePhysical = 0
	DamageTypeWizardry = 1
	DamageTypeCurse    = 2
)

// 消耗属性 designation（与 80_attributes.json 导出件一致，见原版 Stats 命名）。
const (
	desSkillMana = "Current Mana"
	desSkillAG   = "Current Ability"
)

// IsBuffSkill 对照原版 `SkillType is Buff or Regeneration`（安全区豁免、不伤人）。
func (d *SkillDef) IsBuffSkill() bool {
	return d.SkillType == SkillTypeBuff || d.SkillType == SkillTypeRegeneration
}

// SkillManaCost / SkillAGCost 从 ConsumeRequirements 提取消耗量（缺省 0）。
func (d *SkillDef) SkillManaCost() int { return d.consumeOf(desSkillMana) }
func (d *SkillDef) SkillAGCost() int   { return d.consumeOf(desSkillAG) }

func (d *SkillDef) consumeOf(attr string) int {
	for _, c := range d.Consume {
		if c.Attribute == attr {
			return c.Value
		}
	}
	return 0
}

// SkillOutcome 是施放判定结果（原版各失败分支）。
type SkillOutcome byte

const (
	// SkillOK 施放成功（已扣消耗）。
	SkillOK SkillOutcome = iota
	// SkillUnknown 技能不可用：技能表无此号 / 非本职业合格（≈"未学习"）。
	SkillUnknown
	// SkillInSafezone 安全区禁放（非 buff）。
	SkillInSafezone
	// SkillTargetOutOfRange 目标超出 Range+2（0x19）。
	SkillTargetOutOfRange
	// SkillNoMana 消耗不足（原版 TryConsumeForSkillAsync false）。
	SkillNoMana
)

// SkillCastDecision 是一次定向施放的判定产出。
type SkillCastDecision struct {
	Outcome SkillOutcome
	// Mana/AG 为扣减后的当前值（Outcome == SkillOK 时有效）。
	Mana uint32
	AG   uint32
}

// CastTargeted 按 TargetedSkillDefaultPlugin 顺序判定一次 0x19 定向施放。
//
//	known:      技能是否在施法者技能列表（本仓 ≈ 职业合格，见裁剪登记）。
//	casterSafe: 施法者处于安全区。
//	inRange:    调用方算好的 chebyshev ≤ Range+2。
//	mana/ability: 施法者当前 MP/AG。
func CastTargeted(known bool, def *SkillDef, casterSafe, inRange bool, mana, ability uint32) SkillCastDecision {
	if !known || def == nil || def.SkillType == SkillTypeBuff || def.SkillType == SkillTypeRegeneration {
		// 原版：PassiveBoost 直接 return；Buff/Regeneration 不走攻击主干
		// （技能效果随各自子系统接入，此处一并拒绝——裁剪登记）。
		return SkillCastDecision{Outcome: SkillUnknown}
	}
	if casterSafe {
		return SkillCastDecision{Outcome: SkillInSafezone}
	}
	if !inRange {
		return SkillCastDecision{Outcome: SkillTargetOutOfRange}
	}
	return consumeForSkill(def, mana, ability)
}

// CastArea 按 AreaSkillAttackAction.AttackAsync 顺序判定一次 0x1E 区域施放
// （消耗一次性扣减；目标集与逐目标伤害由调用方装配）。
func CastArea(known bool, def *SkillDef, casterSafe bool, mana, ability uint32) SkillCastDecision {
	if !known || def == nil || def.SkillType == SkillTypeBuff || def.SkillType == SkillTypeRegeneration {
		return SkillCastDecision{Outcome: SkillUnknown}
	}
	if casterSafe {
		return SkillCastDecision{Outcome: SkillInSafezone}
	}
	return consumeForSkill(def, mana, ability)
}

// CastBuff 判定一次 Buff 技能施放（SkillType.Buff，对照原版
// TargetedSkillDefaultPlugin 对 Buff 的处理）：安全区豁免、目标是自己（无距离判定），
// 只做技能可用 + 消耗检查。Regeneration 瞬时恢复不走此路径。
func CastBuff(known bool, def *SkillDef, mana, ability uint32) SkillCastDecision {
	if !known || def == nil || def.SkillType != SkillTypeBuff {
		return SkillCastDecision{Outcome: SkillUnknown}
	}
	return consumeForSkill(def, mana, ability)
}

// CastRegeneration 判定一次瞬时恢复技能施放（SkillType.Regeneration，如 Heal/Recovery，
// 对照原版 TargetedSkillDefaultPlugin 对 Regeneration 的处理）：与 Buff 同样安全区豁免、
// 只做技能可用 + 消耗检查，不伤人、不挂持续效果（恢复量由调用方按效果定义入账）。
func CastRegeneration(known bool, def *SkillDef, mana, ability uint32) SkillCastDecision {
	if !known || def == nil || def.SkillType != SkillTypeRegeneration {
		return SkillCastDecision{Outcome: SkillUnknown}
	}
	return consumeForSkill(def, mana, ability)
}

// consumeForSkill 对照 TryConsumeForSkillAsync：任一不足 → false（不扣）；
// 全满足 → 逐项扣减。返回扣减后的 MP/AG。
func consumeForSkill(def *SkillDef, mana, ability uint32) SkillCastDecision {
	manaCost, agCost := def.SkillManaCost(), def.SkillAGCost()
	if uint64(mana) < uint64(manaCost) || uint64(ability) < uint64(agCost) {
		return SkillCastDecision{Outcome: SkillNoMana}
	}
	return SkillCastDecision{Outcome: SkillOK, Mana: mana - uint32(manaCost), AG: ability - uint32(agCost)}
}

// SkillDamageBounds 对照原版 GetSkillDmg：技能伤害区间 = AttackDamage 与
// AttackDamage×1.5（`skillMaximumDamage += skillDamage + skillDamage/2`），
// 再按 GetBaseDmg 叠到攻击者基础伤害上。
func SkillDamageBounds(attackDamage int) (minExtra, maxExtra int) {
	return attackDamage, attackDamage + attackDamage/2
}
