package player

// master_effects.go —— 被动大师技能的效果进角色属性系统（TRIM-09e），对照
// GameLogic/SkillList.cs `CreatePowerUpWrappers`（:235-259）+ `PassiveSkillBoostPowerUp`
// （:280-300）：每条 PassiveBoost 的大师技能按 MasterDefinition 的 TargetAttribute /
// Aggregation 挂一个常量元素，值取导出表在该技能等级上的取值（原版运行期跑 MathParser，
// 本仓在导出期求值后查表 —— 口径见 doc/16 TRIM-09）。
//
// 装配时机与原版一致：AttributeSystem（含装备加成）建好之后、活动 buff 之前
// （原版 Player.cs:1735 建 Attributes、:1741 才 new SkillList）。

import (
	"mugo/internal/attribute"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// desDurabilityReductionFactor 与导出件 designation 逐字一致。
const desDurabilityReductionFactor = "Durability Reduction Factor"

// 原版对这两条"耐久降低"技能额外挂 DurabilityReductionFactor = -等级/500（SkillList.cs:253-258）：
// 300 是通用左根那一条，578 是狂战士（Fist Master）自己的同名条。
const (
	skillDurabilityReduction1        = 300
	skillDurabilityReduction1FistMax = 578
)

// applyMasterSkillPowerUps 注入被动大师技能的效果元素。
func applyMasterSkillPowerUps(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	if cfg == nil || c == nil || system == nil {
		return
	}
	for _, entry := range c.LearnedSkills {
		m, ok := cfg.MasterSkillByNumber(int(entry.SkillNumber))
		if !ok || !m.PassiveBoost {
			continue
		}
		// 原版：TargetAttribute 为空时只留一条注释级的"要不要记日志"，直接返回。
		if m.TargetAttribute == "" {
			continue
		}
		def, ok := cfg.AttributeByName(m.TargetAttribute)
		if !ok {
			continue
		}
		agg, ok := config.AggregateTypeByName(m.Aggregation)
		if !ok {
			continue
		}
		system.AddElement(attribute.NewConstantElement(m.ValueAt(int(entry.Level)), agg),
			&attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation})
		if m.Number == skillDurabilityReduction1 || m.Number == skillDurabilityReduction1FistMax {
			addDurabilityReductionFactor(cfg, entry.Level, system)
		}
	}
}

// addDurabilityReductionFactor 是那条"按等级线性降低耐久"的附加元素（值可为负）。
func addDurabilityReductionFactor(cfg *config.GameConfig, level byte, system *attribute.AttributeSystem) {
	def, ok := cfg.AttributeByName(desDurabilityReductionFactor)
	if !ok {
		return
	}
	value := -float32(level) / 500
	system.AddElement(attribute.NewConstantElement(value, attribute.AggregateAddRaw),
		&attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation})
}

// MasterSkillChain 返回该技能号"参与混算的大师技序列"：自己（若是大师技）+
// 沿 ReplacedSkill 只走大师技的那一段（对照 GameLogic/MasterSkillExtensions.cs:59-89
// 的 GetBaseSkills(onlyMasterSkills=true)，以及 GameLogic 版本 :105-113 的取值顺序）。
func MasterSkillChain(cfg *config.GameConfig, number int) []*config.MasterSkill {
	if cfg == nil {
		return nil
	}
	var out []*config.MasterSkill
	current := number
	for {
		m, ok := cfg.MasterSkillByNumber(current)
		if !ok {
			return out
		}
		out = append(out, m)
		if m.ReplacedSkill == nil {
			return out
		}
		// 只继续走"大师技"这一段：碰到普通技能就停（原版的 onlyMasterSkills 语义）。
		if _, isMaster := cfg.MasterSkillByNumber(*m.ReplacedSkill); !isMaster {
			return out
		}
		current = *m.ReplacedSkill
	}
}

// LearnedLevelOf 返回角色在该技能上的等级（普通技能书等级或大师技能等级，同一张表）。
func LearnedLevelOf(c *entity.Character, number int) byte {
	if c == nil {
		return 0
	}
	for _, e := range c.LearnedSkills {
		if int(e.SkillNumber) == number {
			return e.Level
		}
	}
	return 0
}

// SkillAttackDamage 对应原版 GetDamage（AttackableExtensions.cs:733-757）：
// 技能自身 AttackDamage，加上**没有 TargetAttribute** 的大师技能的等级取值
// （有目标属性的那些走属性系统，见本文件上面的 applyMasterSkillPowerUps，
// 两处都加会重复计入——原版就是互斥的）。
func SkillAttackDamage(cfg *config.GameConfig, c *entity.Character, sk *config.Skill) int {
	if sk == nil {
		return 0
	}
	total := sk.AttackDamage
	if cfg == nil || c == nil {
		return total
	}
	level := LearnedLevelOf(c, sk.Number)
	if level == 0 {
		return total
	}
	for _, m := range MasterSkillChain(cfg, sk.Number) {
		if m.TargetAttribute != "" {
			continue
		}
		total += int(m.ValueAt(int(LearnedLevelOf(c, m.Number))))
	}
	return total
}

// MasterEffectBoost 将大师技能等级值混入 buff 的一个 power-up
// （对照 MagicEffectPowerUpExtensions.cs:115-127：仅当目标属性相同且聚合方式相同才相加，
// 相加而非相乘——原版 CombinedElement 恒为 e1+e2）。
// 返回 (新值, 是否被混入)。
func MasterEffectBoost(cfg *config.GameConfig, c *entity.Character, castNumber int,
	targetAttribute, aggregate string, value float32) float32 {
	if cfg == nil || c == nil || targetAttribute == "" {
		return value
	}
	if LearnedLevelOf(c, castNumber) == 0 {
		return value
	}
	for _, m := range MasterSkillChain(cfg, castNumber) {
		if m.TargetAttribute != targetAttribute || !sameAggregate(m.Aggregation, aggregate) {
			continue
		}
		value += m.ValueAt(int(LearnedLevelOf(c, m.Number)))
	}
	return value
}

// sameAggregate 比较两处聚合方式：导出件一边是 DataModel 枚举名（AddRaw/Multiplicate/…），
// buff 定义那边也是同一枚举，故直接比字符串；空串按 AddRaw 处理（原版默认值）。
func sameAggregate(master, powerUp string) bool {
	if master == "" {
		master = "AddRaw"
	}
	if powerUp == "" {
		powerUp = "AddRaw"
	}
	return master == powerUp
}

// MasterDurationExtension 算 ExtendsDuration 那条大师技给 buff 加的**秒数**
// （对照 MagicEffectPowerUpExtensions.cs:77-88：值 <1 时先 ×100，再加到时长上——
// 原版 CombinedElement 是加法，不是倍乘）。返回 0 表示没有这条。
func MasterDurationExtension(cfg *config.GameConfig, c *entity.Character, castNumber int) float32 {
	if cfg == nil || c == nil {
		return 0
	}
	if LearnedLevelOf(c, castNumber) == 0 {
		return 0
	}
	for _, m := range MasterSkillChain(cfg, castNumber) {
		if !m.ExtendsDuration {
			continue
		}
		value := m.ValueAt(int(LearnedLevelOf(c, m.Number)))
		if value < 1 {
			value *= 100
		}
		return value // 原版 durationExtended 使这条只加一次
	}
	return 0
}
