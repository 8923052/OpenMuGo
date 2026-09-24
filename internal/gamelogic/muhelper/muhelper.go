// Package muhelper 是 MU Helper 的领域层（对应 OpenMU GameLogic/MuHelper）：
// 状态枚举、服务器配置（默认值照抄 MuHelperConfiguration）与 Zen 计费公式
// （MuHelperZenCostCalculator）。纯逻辑、版本无关。
//
// 后台运行循环（按 PayInterval 扣费）是有状态/绑连接的编排，落在 gameserver（与
// petManager/partyManager 同层）；OpenMU 里给属性系统加 IsMuHelperActive 增益元素，
// 本仓属性面板暂未接入该元素（见 doc/15）。
package muhelper

import "time"

// Status 是 MU Helper 的开关状态（对照 MuHelperStatus：Enabled=0/Disabled=1）。
type Status byte

const (
	StatusEnabled  Status = 0
	StatusDisabled Status = 1
)

// StatusFromPause 把"是否暂停"标志映射为状态（对照 handler：PauseStatus→Disabled 否则 Enabled）。
func StatusFromPause(pause bool) Status {
	if pause {
		return StatusDisabled
	}
	return StatusEnabled
}

// Configuration 是 MU Helper 的服务器配置（默认值取自 OpenMU MuHelperConfiguration）。
type Configuration struct {
	CostPerStage  []int         // 每阶段的费率（乘以总等级得到一次扣费）
	PayInterval   time.Duration // 每隔多久扣一次
	StageInterval time.Duration // 每隔多久进入下一费率阶段
	MinLevel      int
	MaxLevel      int
}

// DefaultConfiguration 返回与原版一致的默认配置。
func DefaultConfiguration() Configuration {
	return Configuration{
		CostPerStage:  []int{20, 50, 80, 100, 120},
		PayInterval:   5 * time.Minute,
		StageInterval: 200 * time.Minute,
		MinLevel:      1,
		MaxLevel:      400,
	}
}

// ZenCost 计算自 startTimestamp 起、经过 elapsed 后本次应扣的 Zen
// （对照 MuHelperZenCostCalculator.Calculate：costPerStage[stage] × 总等级）。
// totalLevel 由调用方算好（本仓无大师等级，即角色等级）。
func ZenCost(cfg Configuration, totalLevel int, elapsed time.Duration) int {
	if len(cfg.CostPerStage) == 0 || cfg.StageInterval <= 0 {
		return 0
	}
	stage := int(elapsed / cfg.StageInterval)
	if stage < 0 {
		stage = 0
	}
	if stage > len(cfg.CostPerStage)-1 {
		stage = len(cfg.CostPerStage) - 1
	}
	return cfg.CostPerStage[stage] * totalLevel
}
