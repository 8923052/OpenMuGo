package player

// combat_switchboard.go —— 大师树里"按当前武器位选一条属性"的两块开关板
// （对照 AttackableExtensions.cs:958-1013 与 :1015-1041）。伤害式只需要两个整数，
// 所以在这里读属性、不在 combat 包里碰 designation。

// valueFunc 按 designation 取属性值（与 resolveCombatValues 的 valueOf 同一个）。
type valueFunc func(designation string) float32

// masterPhysicalPreBuffs 对应 GetMasterSkillTreePhysicalPassiveDamageBonus(preBuffsStage=true)：
// 弓 / 十字弓 / 双手剑三条"强化"系加成，按 if-else 取第一条命中的武器位。
// 原版在物理段的"狂暴加成之后、减防之前"加它（:138）。
func masterPhysicalPreBuffs(value valueFunc) int {
	switch {
	case value("Is Bow Equipped") > 0:
		return int(value("Bow Strengthener Bonus Damage (MST)"))
	case value("Is Cross Bow Equipped") > 0:
		return int(value("Cross Bow Strengthener Bonus Damage (MST)"))
	case value("Is Two Handed Sword Equipped") > 0:
		return int(value("Two Handed Sword Strengthener Bonus Damage (MST)"))
	}
	return 0
}

// masterPhysicalPostBuffs 是同函数的 else 分支（preBuffsStage=false，:196 处使用）：
// 矛 / 权杖 / 拳套三选一，否则"单手剑 + 锤"——两者都有时取平均（原版注释：双持不同武器
// 时取均值，且只在单手剑值为 0 时直接用锤值）。末尾恒再叠加通用桶
// "Master Skill Physical Bonus Damage (MST)"。
func masterPhysicalPostBuffs(value valueFunc) int {
	var bonus int
	switch {
	case value("Is Spear Equipped") > 0:
		return int(value("Spear Bonus Damage (MST)")) + int(value("Master Skill Physical Bonus Damage (MST)"))
	case value("Is Scepter Equipped") > 0:
		return int(value("Scepter Strengthener Bonus Damage (MST)")) + int(value("Master Skill Physical Bonus Damage (MST)"))
	case value("Is Glove Weapon Equipped") > 0:
		return int(value("Glove Weapon Bonus Damage (MST)")) + int(value("Master Skill Physical Bonus Damage (MST)"))
	}
	if value("Is One Handed Sword Equipped") > 0 {
		bonus = int(value("One Handed Sword Bonus Damage (MST)"))
	}
	if value("Is Mace Equipped") > 0 {
		mace := value("Mace Bonus Damage (MST)")
		if bonus == 0 {
			bonus = int(mace)
		} else {
			bonus = int((float32(bonus) + mace) / 2)
		}
	}
	return bonus + int(value("Master Skill Physical Bonus Damage (MST)"))
}

// masterMasteryPvPBonus 对应 GetMasterSkillTreeMasteryPvpDamageBonus（:1015-1041），
// 只在"玩家打玩家且攻击者不是渡鸦"时被加进伤害（:270-280）。
func masterMasteryPvPBonus(value valueFunc) int {
	switch {
	case value("Is Two Handed Sword Equipped") > 0:
		return int(value("Two Handed Sword Mastery PvP Bonus Damage (MST)"))
	case value("Is Two Handed Staff Equipped") > 0:
		return int(value("Two Handed Staff Mastery PvP Bonus Damage (MST)"))
	case value("Is Cross Bow Equipped") > 0:
		return int(value("Cross Bow Mastery PvP Bonus Damage (MST)"))
	case value("Is Stick Equipped") > 0:
		return int(value("Stick Mastery PvP Bonus Damage (MST)"))
	case value("Is Scepter Equipped") > 0:
		return int(value("Scepter Mastery PvP Bonus Damage (MST)"))
	}
	return 0
}
