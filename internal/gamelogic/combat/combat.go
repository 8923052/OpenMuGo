// Package combat 是战斗伤害的忠实移植（对照原版
// GameLogic/AttackableExtensions.cs 的 CalculateDamageAsync / GetHitChanceTo /
// GetBaseDmg / GetSkillDmg）。
//
// damage.go 的 Calculate 复刻 PvM 伤害主干的完整乘区链：命中判定 → 暴击/卓越/破防
// 掷骰 → 防御（含 DefenseDecrement、GreaterDefenseBonus）→ 按伤害类型选基础值 →
// 双倍/双手加伤 → Overrates×0.3 → ArmorDamageDecrease 减伤 → minLevelDmg 下限 →
// AttackDamageIncrease 增伤 / DamageReceiveDecrement 受伤减免 → 技能倍率 / Dinorant →
// 最终加成 → 双倍伤害。
//
// 属性缺省 = 0：CriticalDamageChance/ExcellentDamageChance 等未装配属性按 0 参与，
// 与原版"未定义属性值为 0"行为一致（暴击/卓越/破防/双倍路径自然短路）。
//
// 裁剪登记（TRIM-06 之后仍缺的部分，都带原因）：
//   - duelDmgDec 的 0.6 分支需要 DuelRoom（决斗整域未起）→ 调用方恒传 1，乘区位置已按原版留好；
//   - Chaos Castle 的两处 ÷2 需要 CurrentMiniGame 实例（小游戏整域未起）；
//   - HP/SD 分流（GetHitInfo:318-332 的 shieldRatio）不在本函数：属受击结算域；
//   - 每技能的属性关系系统（EnsureSkillAttributes：Skill Base/Final Bonus 与 Multiplier）
//     未建模 → 由调用方按可算出的部分填 Skill.ExtraMin/ExtraMax/FinalBonus/FinalMultiplier；
//   - 元素抗性 → 加伤（GetSkillDmg:777-782）同样由调用方并入 ExtraMin/ExtraMax；
//     命中后的元素效果（免疫门/闪电击退）在 handler 侧；
//   - 召唤系基伤（Fenrir 伤害类型、Minimum Curse Base Damage>0 的"isSummonerSkill"次序）
//     由 player 投影按同一规则算好传入。
package combat

import "mugo/internal/util"

// HitChancePvm 对应原版 GetHitChanceTo 的 PvM 分支。
func HitChancePvm(attackRate, defenseRate float32) float32 {
	const minChance = 0.03
	if defenseRate < attackRate {
		return 1.0 - defenseRate/attackRate
	}
	return minChance
}

// HitChance 按对抗关系选攻/防速率（原版 GetHitChanceTo:698-707 的两分支）。
// 注意 PvP 分支**不加** Increase Block Bonus（那条只在 PvM 的 GetDefenseRatePvm 里，
// 由调用方合并进 DefenseRate）。
func HitChance(isPvP bool, atk AttackerStats, def DefenderStats) float32 {
	if isPvP {
		return HitChancePvm(atk.AttackRatePvP, def.DefenseRatePvP)
	}
	return HitChancePvm(atk.AttackRate, def.DefenseRate)
}

// DamageResult 是一次伤害结算结果。
type DamageResult struct {
	Damage int
	// Miss 为 true 时 Damage=0（原版 HitInfo(0,0,Undefined)）。
	Miss bool
	Kind DamageKind
}

// DamageKind 与 s2c.DamageKind 数值一致（颜色语义）。
type DamageKind byte

const (
	KindNormal        DamageKind = 0 // NormalRed
	KindIgnoreDefense            = 1 // IgnoreDefenseCyan
	KindExcellent                = 2
	KindCritical                 = 3
)

// rollHit 对应原版 Rand.NextRandomBool(hitChance)。
func rollHit(rng *util.Rand, chance float32) bool {
	return rng.NextDouble() < float64(chance)
}

// rollChance 对应原版 Rand.NextRandomBool(double probability)（0..1 概率掷骰）。
func rollChance(rng *util.Rand, p float32) bool {
	if p <= 0 {
		return false
	}
	return rng.NextDouble() < float64(p)
}

// ApplyDamageMultipliers 复刻 CalculateDamageAsync 的两个乘区（L220/223）：
// 先按攻方 Attack Damage Increase Multiplier 放大，结果 >1 时再按守方 Damage Receive
// Multiplier 缩减（守护天使/黑王马/蓝 Fenrir/翅膀）。传入 ≤0 的倍率按 1 处理
// （属性未装配不改变结果）。
func ApplyDamageMultipliers(dmg int, attackDamageIncrease, defenderDamageReceiveDecrement float32) int {
	if attackDamageIncrease <= 0 {
		attackDamageIncrease = 1
	}
	dmg = int(float32(dmg) * attackDamageIncrease)
	if dmg > 1 {
		if defenderDamageReceiveDecrement <= 0 {
			defenderDamageReceiveDecrement = 1
		}
		dmg = int(float32(dmg) * defenderDamageReceiveDecrement)
	}
	return dmg
}

// randInt 对应原版 Rand.NextInt(min, max)：[min, max)；max<=min 时取 min。
func randInt(rng *util.Rand, minV, maxV int) int {
	if maxV <= minV {
		return minV
	}
	return rng.Next(minV, maxV)
}
