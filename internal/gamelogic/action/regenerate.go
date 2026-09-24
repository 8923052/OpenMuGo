package action

// regenerate.go —— 周期恢复与"击杀怪物后恢复"的纯函数。
//
// 对照原版：
//   - GameLogic/Player.cs `RegenerateAsync`（(max×倍率+绝对值)×elapsed/interval，钳到 max）
//   - GameLogic/Player.cs `AfterKilledMonsterAsync`（击杀后 (倍率×max+绝对值) 一次性入账）
//   - GameLogic/Attributes/Stats.cs `Regeneration`（Interval / HiatusThreshold）
//   - Persistence/.../CharacterClassInitialization.cs（安全区 → AG 绝对值 +3、护盾恢复启用）
//   - GameLogic/PlugIns/PeriodicTasks/ShieldRecoveryHiatusPlugIn.cs（中断后重新计时）

// 周期（秒）逐条对照 Stats.Regeneration.Interval；原版只跑一个 RecoveryInterval
// 定时器，用 elapsed/Interval 做补偿因子，本仓沿用同一算法。
const (
	ManaRegenIntervalSeconds    = 3.0
	HealthRegenIntervalSeconds  = 7.0
	AbilityRegenIntervalSeconds = 3.0
	ShieldRegenIntervalSeconds  = 1.0
)

// 休息（坐下/倚靠/悬挂，0x18 动画包驱动 IsResting）时的周期，
// 对照 Stats.cs：HealthRegeneration.IntervalResting=5s、ManaRegeneration.IntervalResting=5s；
// AG/护盾无独立休息周期（默认 3s/3s、1s/1s）。
const (
	HealthRegenRestingIntervalSeconds = 5.0
	ManaRegenRestingIntervalSeconds   = 5.0
)

// ShieldRampSlope 对照 `ShieldRecoveryRampFactor += (1/15) × ShieldRecoveryHiatus`
// （CharacterClassInitialization.cs L153，注释 (3-2)/(25-10)；ramp 原版范围 [2,3]）。
const ShieldRampSlope = 1.0 / 15.0

// ShieldRampFactorAt 返回静置 hiatus 秒后的护盾 ramp 因子（基值 4/3 来自数据，
// 经 CombatValues.ShieldRampFactor 传入；恢复从 hiatus≥10s 开始，ramp 已 ≥2）。
func ShieldRampFactorAt(rampBase, hiatus float64) float64 {
	return rampBase + hiatus*ShieldRampSlope
}

// ShieldRegenMultiplierAt 把快照里的倍率（含 ramp(0)）换算到 hiatus 时刻的实际倍率：
// mult(h) = baseMult × ramp(h)/ramp(0)。
func ShieldRegenMultiplierAt(snapshotMult, rampBase, hiatus float64) float64 {
	if rampBase <= 0 {
		return snapshotMult
	}
	return snapshotMult * ShieldRampFactorAt(rampBase, hiatus) / rampBase
}

// AbilityRegenSafezoneBonus 对照 `AbilityRecoveryAbsolute += 3 × IsInSafezone`
// （安全区内 AG 恢复更快；城外只有基值 2）。
const AbilityRegenSafezoneBonus = 3.0

// ShieldRegenHiatusThreshold 对照 Regeneration.HiatusThreshold：护盾被打断后
// 需静置这么久才重新开始恢复（ShieldRecoveryHiatusPlugIn 每秒累加）。
const ShieldRegenHiatusThreshold = 10.0

// RegenRemainder 保存四项恢复"不足 1 点"的小数余量。原版属性是 float 一路累加，
// 本仓 CharStats 是整数——每 3s 丢掉 0.x 点会让长期速率偏离原版约 20%。
type RegenRemainder struct {
	Mana    float64
	Health  float64
	Ability float64
	Shield  float64
}

// RegenIncrement 返回本次恢复量：(max×倍率 + 绝对值) × elapsed/interval。
func RegenIncrement(max, multiplier, absolute, elapsed, interval float64) float64 {
	if interval <= 0 || elapsed <= 0 {
		return 0
	}
	return (max*multiplier + absolute) * (elapsed / interval)
}

// ApplyRegen 累加并钳到上限（对照 `attributes[cur] = min(cur + inc, max)`）；
// remainder 是上次不足 1 点的余量，返回新值与结转余量。
func ApplyRegen(current, remainder, max, multiplier, absolute, elapsed, interval float64) (float64, float64, bool) {
	// 已顶到上限：原版 min() 之后存的就是 max 本身，小数余量一并丢掉（不结转）。
	if current >= max {
		return current, 0, false
	}
	inc := RegenIncrement(max, multiplier, absolute, elapsed, interval)
	if inc <= 0 {
		return current, remainder, false
	}
	total := current + remainder + inc
	next := float64(uint32(total)) // 原版下发前 (uint) 截断
	carry := total - next
	if next > max {
		return max, 0, true
	}
	return next, carry, true
}

// ShieldRecoveryActive 对照 IsShieldRecoveryActive = IsInSafezone + ShieldRecoveryEverywhere
// （≥1 才启用；通常只有安全区，380 级守护 option 才给"到处回盾"）。
func ShieldRecoveryActive(inSafezone bool, recoveryEverywhere float64) bool {
	return inSafezone || recoveryEverywhere >= 1
}

// AfterKillRecover 对照 AfterKilledMonsterAsync：min(max, cur + (uint)(倍率×max + 绝对值))。
func AfterKillRecover(current, max, multiplier, absolute float64) (float64, bool) {
	if current >= max {
		return current, false
	}
	add := multiplier*max + absolute
	if add <= 0 {
		return current, false
	}
	next := current + float64(uint32(add)) // 原版先 (uint) 截断增量再加
	if next > max {
		return max, true
	}
	return next, true
}
