package player

// buff_producer.go —— Buff 效果的构造、求值与属性注入（对照原版
// MagicEffectPowerUpExtensions.CreateMagicEffectPowerUp +
// AttributeSystemExtensions.CreateElement + MagicEffectsList.AddEffectAsync）。
//
// Go 属性系统按需重建：施法时按"装备后"状态求出效果的时长/各 PowerUp 值，
// 加入会话效果表；每次重建属性系统时（buildWorldSystem）再把活动效果注入，
// 因此 buff 数值随装备/属性即时变化、过期/死亡即随重建移除。

import (
	"math"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// evalPowerUpValue 对照 AttributeSystemExtensions.CreateElement：
// 值 = Constant + Σ RelatedValues（每条：输入属性 ⊗ 操作数，按 operator），
// 再按 MaximumValue 裁剪。
func evalPowerUpValue(v *config.PowerUpValue, system *attribute.AttributeSystem) float32 {
	if v == nil {
		return 0
	}
	result := float32(v.Constant)
	for i := range v.Related {
		result += evalRelation(&v.Related[i], system)
	}
	if v.MaximumValue != nil && result > float32(*v.MaximumValue) {
		result = float32(*v.MaximumValue)
	}
	return result
}

// evalRelation 求一条关系元素值：输入属性与操作数按 InputOperator 运算。
func evalRelation(r *config.AttributeRelationship, system *attribute.AttributeSystem) float32 {
	input := system.GetValueOfAttribute(&attribute.AttributeDefinition{
		ID: r.Input.ID, Designation: r.Input.Designation,
	})
	operand := r.InputOperand
	if r.Operand != nil {
		operand = system.GetValueOfAttribute(&attribute.AttributeDefinition{
			ID: r.Operand.ID, Designation: r.Operand.Designation,
		})
	}
	switch r.InputOperator {
	case "Add":
		return input + operand
	case "Exponentiate":
		return float32(math.Pow(float64(input), float64(operand)))
	case "ExponentiateByAttribute":
		return float32(math.Pow(float64(operand), float64(input)))
	case "Maximum":
		return float32(math.Max(float64(input), float64(operand)))
	case "Minimum":
		return float32(math.Min(float64(input), float64(operand)))
	default: // Multiply
		return input * operand
	}
}

// CreateMagicEffect 按效果定义和角色"装备后"状态构造一个待施加的效果实例，
// 同时返回施加概率（chance：定义无 Chance 为 1）。
// 调用方按 chance 掷骰，成功才把结果 Add 进效果表。
//
// castSkillNumber 是带来这个效果的技能号（0 = 非技能来源）；大师技能的等级值要按
// MagicEffectPowerUpExtensions.cs:73-98 混进 power-up 与时长。
// targetIsPlayer 对应原版选 PvP 三元组的判据 `target is Player`
// （AttackableExtensions.cs:348-355）——注意是**目标**是不是玩家，不是攻击者。
func CreateMagicEffect(cfg *config.GameConfig, c *entity.Character, number, castSkillNumber int,
	targetIsPlayer bool) (action.MagicEffect, float32, bool) {
	def, ok := cfg.MagicEffectByNumber(number)
	if !ok || def == nil {
		return action.MagicEffect{}, 0, false
	}
	// 求值基准：装备后、不含其它 buff（新效果基于当前态）。
	base, err := buildWorldSystem(cfg, c, nil)
	if err != nil {
		return action.MagicEffect{}, 0, false
	}
	durationDef, chanceDef, powerUpDefs := def.ForTarget(targetIsPlayer)
	durationSec := evalPowerUpValue(&durationDef, base)
	// ExtendsDuration 的大师技给时长**加秒**（值 <1 时先 ×100；只加一次）。
	durationSec += MasterDurationExtension(cfg, c, castSkillNumber)
	chance := float32(1)
	if chanceDef != nil {
		chance = evalPowerUpValue(chanceDef, base)
	}
	powerUps := make([]action.MagicEffectPowerUp, 0, len(powerUpDefs))
	for i := range powerUpDefs {
		p := &powerUpDefs[i]
		value := evalPowerUpValue(&p.Boost, base)
		value = MasterEffectBoost(cfg, c, castSkillNumber, p.Target, p.Boost.AggregateType, value)
		powerUps = append(powerUps, action.MagicEffectPowerUp{
			Attribute: p.Target,
			Value:     value,
			AggType:   byte(parsePowerUpAggregate(p.Boost.AggregateType)),
		})
	}
	effect := action.MagicEffect{
		Definition: action.MagicEffectDefinition{
			Number:          int16(def.Number),
			SubType:         def.SubType,
			DurationMs:      int(durationSec * 1000),
			SendDuration:    def.SendDuration,
			InformObservers: def.InformObservers,
			StopByDeath:     def.StopByDeath,
		},
		PowerUps:  powerUps,
		CastSkill: castSkillNumber,
	}
	return effect, chance, true
}

// applyMagicEffectPowerUps 把活动效果注入系统。为随装备/属性动态，按效果编号
// 重取定义、用"装备后"系统现算每个 PowerUp（效果间不互相影响——裁剪，原版为
// 同图订阅；基础 buff 的关系输入多为 Total Energy 等基础属性，安全）。
// 注入的对象总是玩家，因此三元组按"目标是玩家"取（与创建时同一分支）。
func applyMagicEffectPowerUps(cfg *config.GameConfig, c *entity.Character, effects []action.MagicEffect, system *attribute.AttributeSystem) {
	for ei := range effects {
		def, ok := cfg.MagicEffectByNumber(int(effects[ei].Definition.Number))
		if !ok {
			// 无定义的内部效果不注入（基础 buff 均有定义；裁剪登记）。
			continue
		}
		_, _, defs := def.ForTarget(true)
		for i := range defs {
			p := &defs[i]
			if p.Target == "" {
				continue
			}
			targetDef, ok := cfg.AttributeDefinitionByID(p.TargetID)
			if !ok {
				continue
			}
			value := evalPowerUpValue(&p.Boost, system)
			value = MasterEffectBoost(cfg, c, effects[ei].CastSkill, p.Target, p.Boost.AggregateType, value)
			probe := &attribute.AttributeDefinition{
				ID: targetDef.ID, Designation: targetDef.Designation,
			}
			system.AddElement(attribute.NewConstValueAttributeAgg(
				value, probe, parsePowerUpAggregate(p.Boost.AggregateType)), probe)
		}
	}
}
