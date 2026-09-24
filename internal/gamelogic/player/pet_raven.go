package player

// pet_raven.go —— 黑暗渡鸦的攻击属性解析（对照 GameLogic/Pet/RavenAttributeSystem.cs）。
//
// 原版用一个只读代理属性系统把渡鸦各项映射到主人属性；本仓更简单：可训练宠物等级
// 已由 applyTrainablePetLevel 注入 "Dark Raven Level"，渡鸦的 min/max 伤害、攻击率、
// 攻速都是**职业关系图**（导出件 10_character_classes.json）按该等级 + 总统率派生的
// 现成属性——装配角色系统后直接按 designation 读值即可，无需另写公式。
//
// RavenAttackDamageIncrease（杖 rise）在关系里以 AddRaw 汇入，未持杖时为 0，
// 故 min/max 不再额外放大（与原版 `?? 1` 但属性恒存在返回 0 的实际行为一致）。

import (
	"fmt"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// RavenAttack 是一次渡鸦攻击所需的结算属性。
type RavenAttack struct {
	MinDamage   int
	MaxDamage   int
	AttackRate  float32
	AttackSpeed float32
}

// AttackDelayMilliseconds 复刻 RavenCommandManager.AttackDelay：
// max(100, 1500 - attackSpeed*10) 毫秒。
func (r RavenAttack) AttackDelayMilliseconds() int {
	d := 1500 - int(r.AttackSpeed*10)
	if d < 100 {
		d = 100
	}
	return d
}

// 渡鸦属性 designation（与导出件 80_attributes.json 逐字一致）。
const (
	desRavenMinDamage   = "Minimum damage of the dark raven."
	desRavenMaxDamage   = "Maximum damage of the dark raven."
	desRavenAttackRate  = "Attack rate of the dark raven."
	desRavenAttackSpeed = "Attack speed of the dark raven."
)

// ResolveRavenCombatValues 从角色（含渡鸦等级注入与装备加成后的）属性系统读取渡鸦攻击属性。
func ResolveRavenCombatValues(cfg *config.GameConfig, c *entity.Character) (RavenAttack, error) {
	if cfg == nil || c == nil {
		return RavenAttack{}, fmt.Errorf("player: 渡鸦属性解析缺少配置或角色")
	}
	system, err := buildWorldSystem(cfg, c, nil)
	if err != nil {
		return RavenAttack{}, err
	}
	valueOf := func(designation string) float32 {
		def, ok := cfg.AttributeByName(designation)
		if !ok {
			return 0
		}
		probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
		return system.GetValueOfAttribute(probe)
	}
	return RavenAttack{
		MinDamage:   int(valueOf(desRavenMinDamage)),
		MaxDamage:   int(valueOf(desRavenMaxDamage)),
		AttackRate:  valueOf(desRavenAttackRate),
		AttackSpeed: valueOf(desRavenAttackSpeed),
	}, nil
}
