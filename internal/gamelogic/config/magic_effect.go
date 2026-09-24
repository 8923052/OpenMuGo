package config

// magic_effect.go —— MagicEffectDefinition 配置结构（buff 生产者数据源，
// 对照原版 DataModel/Configuration/MagicEffectDefinition.cs）。
// 由 35_magic_effects.json 载入，按 number 查询。

// MagicEffectDefinition 是一个魔法效果（buff/异常）的定义。
type MagicEffectDefinition struct {
	Number          int    `json:"number"`
	Name            string `json:"name"`
	SubType         byte   `json:"sub_type"`
	InformObservers bool   `json:"inform_observers"`
	StopByDeath     bool   `json:"stop_by_death"`
	SendDuration    bool   `json:"send_duration"`
	// Duration 为持续时长（秒）的常量+关系值。
	Duration PowerUpValue `json:"duration"`
	// Chance 为施加概率；nil = 必然成功（对照原版默认 ConstantElement(1)）。
	Chance *PowerUpValue `json:"chance"`
	// 持续时长按目标等级缩放（原版 DurationDependsOnTargetLevel + 两个除数）：
	// 目标等级/除数后参与时长计算（怪物与玩家用不同除数）。
	DurationDependsOnTargetLevel bool    `json:"duration_depends_on_target_level"`
	MonsterTargetLevelDivisor    float64 `json:"monster_target_level_divisor"`
	PlayerTargetLevelDivisor     float64 `json:"player_target_level_divisor"`
	// PowerUps 为效果对各目标属性的加成定义。
	PowerUps []PowerUpDef `json:"power_up_definitions"`
	// TRIM-06：对**玩家**目标生效的第二组三元组。原版按"目标是不是 Player"整套切换
	// （AttackableExtensions.cs:348-355），而切换规则是：duration/chance 为 null 时回落
	// PvE 值，powerUps 列表为空时才回落（MagicEffectPowerUpExtensions.cs:41,43,51）。
	ChancePvp   *PowerUpValue `json:"chance_pvp"`
	DurationPvp *PowerUpValue `json:"duration_pvp"`
	PowerUpsPvp []PowerUpDef  `json:"power_up_definitions_pvp"`
}

// ForTarget 返回打这个目标时该效果实际使用的三元组（targetIsPlayer 对应原版 `target is Player`）。
func (m *MagicEffectDefinition) ForTarget(targetIsPlayer bool) (PowerUpValue, *PowerUpValue, []PowerUpDef) {
	if !targetIsPlayer {
		return m.Duration, m.Chance, m.PowerUps
	}
	duration, chance, powerUps := m.Duration, m.Chance, m.PowerUps
	if m.DurationPvp != nil {
		duration = *m.DurationPvp
	}
	if m.ChancePvp != nil {
		chance = m.ChancePvp
	}
	if len(m.PowerUpsPvp) > 0 {
		powerUps = m.PowerUpsPvp
	}
	return duration, chance, powerUps
}

// PowerUpValue 对照原版 PowerUpDefinitionValue：值 = Constant（聚合形态）
// + Σ Related（输入属性 ⊗ 操作数，按 operator）；MaximumValue 为上限裁剪。
type PowerUpValue struct {
	Constant      float64                 `json:"constant"`
	AggregateType string                  `json:"aggregate_type"`
	Related       []AttributeRelationship `json:"related"`
	MaximumValue  *float64                `json:"maximum_value"`
}

// PowerUpDef 对照原版 PowerUpDefinition：目标属性 + Boost（常量+关系值）。
type PowerUpDef struct {
	TargetID string       `json:"target_id"`
	Target   string       `json:"target"`
	Boost    PowerUpValue `json:"boost"`
}

// MagicEffectByNumber 按客户端编号查效果定义（施放 buff 时关联用）。
func (c *GameConfig) MagicEffectByNumber(number int) (*MagicEffectDefinition, bool) {
	for i := range c.MagicEffects {
		if c.MagicEffects[i].Number == number {
			return &c.MagicEffects[i], true
		}
	}
	return nil, false
}
