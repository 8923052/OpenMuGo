package action

// statpoints.go —— T2-8 属性点分配（原版 GameLogic/PlayerActions/Character/
// IncreaseStatsAction.cs + CharacterExtensions.CanIncreaseStats）。
//
// 分配链路（原版）：
//   1. CanIncreaseStats(amount)：LevelUpPoints ≥ amount（GM 免检，本仓无 GM 态短路）；
//   2. attributeDef = class.GetStatAttribute(targetAttribute)：按 StatType 映射到
//      "Base Strength" 等 StatAttribute，且 IncreasableByPlayer 必须为 true；
//   3. MaximumValue 检查：原版 Base* 属性**没有** MaximumValue（Stats.cs 构造无上限），
//      该分支恒不触发，本仓同样不做上限钳制（u16 溢出由调用方守卫）；
//   4. attributes[attr] += amount; LevelUpPoints -= amount;
//   5. IStatIncreaseResultPlugIn.StatIncreaseResultAsync 下发结果。

// StatType 是客户端 F3 06 的 StatType（s2c.CharacterStatAttribute，数值一致）。
type StatType byte

const (
	StatStrength   StatType = 0
	StatAgility    StatType = 1
	StatVitality   StatType = 2
	StatEnergy     StatType = 3
	StatLeadership StatType = 4
)

// StatAllocation 是一次加点申请的判定入参（纯数据，便于 golden 对拍）。
type StatAllocation struct {
	Stat          StatType
	Amount        uint16 // 客户端固定 1（F3 06 无数量字段；原版 IncreaseStatsAsync 缺省 1）
	LevelUpPoints uint16
	// BaseValue 为该 StatAttribute 的当前值（角色 "Base *" 态；c.Stats 对应维度）。
	BaseValue uint16
}

// StatAllocationResult 是加点判定的产出。
type StatAllocationResult struct {
	// OK 为 true 时 NewBaseValue/NewLevelUpPoints 有效。
	OK bool
	// NotEnoughPoints 对应原版 NotEnoughLevelUpPointsAvailable 蓝字分支。
	NotEnoughPoints bool
	// UnknownStat 对应原版 GetStatAttribute 落空 / 非 IncreasableByPlayer
	// （AttributeNotAvailable 蓝字分支）。
	UnknownStat bool
	// NewBaseValue / NewLevelUpPoints 为分配后的角色态。
	NewBaseValue     uint16
	NewLevelUpPoints uint16
}

// IncreaseStat 按 amount 分配一次属性点。amount == 0 直接拒绝（原版
// ArgumentOutOfRangeException——客户端不可达，防御即可）。
func IncreaseStat(in StatAllocation) StatAllocationResult {
	if in.Amount == 0 {
		return StatAllocationResult{}
	}
	if in.Stat > StatLeadership {
		return StatAllocationResult{UnknownStat: true}
	}
	if in.LevelUpPoints < in.Amount {
		return StatAllocationResult{NotEnoughPoints: true}
	}
	// u16 溢出守卫：原版属性值语义上限远小于 65535（客户端显示上限 32767），
	// 越界按"不可再分配"处理。
	newBase := int(in.BaseValue) + int(in.Amount)
	if newBase > 32767 {
		return StatAllocationResult{UnknownStat: true}
	}
	return StatAllocationResult{
		OK:               true,
		NewBaseValue:     uint16(newBase),
		NewLevelUpPoints: in.LevelUpPoints - in.Amount,
	}
}

// StatBaseValue 把 StatType 映射到角色五维字段（原版 Stats.Base* 的角色态）。
func (st StatType) StatBaseValue(strength, agility, vitality, energy, leadership uint16) uint16 {
	switch st {
	case StatStrength:
		return strength
	case StatAgility:
		return agility
	case StatVitality:
		return vitality
	case StatEnergy:
		return energy
	case StatLeadership:
		return leadership
	}
	return 0
}
