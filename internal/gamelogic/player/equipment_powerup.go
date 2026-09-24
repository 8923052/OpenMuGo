package player

// equipment_powerup.go —— 装备 PowerUp 回灌属性系统（对照原版
// GameLogic/ItemPowerUpFactory.GetBasePowerUpWrappers + InventoryStorage.
// UpdateItemsOnChangeAsync）。
//
// Go 属性系统按需重建：BuildCharacterAttributeSystem 建好职业基底后，
// 把角色当前已装备物品的基础加成（含物品等级加成）作为常量元素注入，
// 之后再读值——穿上装备即时提升伤害/防御/攻速，脱下即随重建消失。

import (
	"mugo/internal/attribute"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/storage"
)

// buildCharacterSystem 构造角色属性系统：职业/角色基底 + 已装备物品加成。
// ResolveCharStats 与 AttributeValue 共用，保证读到的值含装备加成。
func buildCharacterSystem(cfg *config.GameConfig, c *entity.Character) (*attribute.AttributeSystem, error) {
	return buildWorldSystem(cfg, c, nil)
}

// buildWorldSystem 构造角色的完整属性系统（统一入口）：
//  1. 职业/角色 overrides 基底；
//  2. 已装备物品加成（武器伤害/防具防御/攻速）；
//  3. 活动 buff 效果（在装备后求值，随装备动态）。
//
// ResolveCharStats（无 buff）与 resolveCombatValues（含会话 buff）共用，
// 杜绝"装备/效果只在部分路径生效"的分叉。
func buildWorldSystem(cfg *config.GameConfig, c *entity.Character, effects []action.MagicEffect) (*attribute.AttributeSystem, error) {
	return buildWorldSystemWithOverrides(cfg, c, int(c.ClassNumber), charStatOverrides(c), effects)
}

// buildWorldSystemWithOverrides 是统一系统构建的底层：基底（指定 class/overrides）
// + 装备 + 活动 buff。
func buildWorldSystemWithOverrides(cfg *config.GameConfig, c *entity.Character, classNumber int, overrides map[string]float32, effects []action.MagicEffect) (*attribute.AttributeSystem, error) {
	statBonuses, elementBonuses := splitAttributeBonuses(cfg, c, classNumber)
	for designation, value := range statBonuses {
		// 原版的 character 级 StatAttribute 是"类初值 + 奖励值"（QuestCompletionAction.cs:108
		// 是对已有项做 +=），而 overrides 表达的是角色现值 → 缺项时要以职业初值为基准。
		base, ok := overrides[designation]
		if !ok {
			base = classStatBaseValue(cfg, classNumber, designation)
		}
		overrides[designation] = base + value
	}
	system, err := cfg.BuildCharacterAttributeSystem(classNumber, overrides)
	if err != nil {
		return nil, err
	}
	applyAttributeBonuses(cfg, elementBonuses, system)
	applyEquipmentPowerUps(cfg, c, system)
	// 可训练宠物（黑暗之马/渡鸦）等级先于选项注入：黑暗之马选项按关系读
	// "Dark Horse Level"，装配期即时求值须先看到等级元素（对照 GetPetLevel）。
	applyTrainablePetLevel(cfg, c, system)
	// 装备的选项加成（幸运/普通/卓越/和谐等）与套装组/组合奖励：必须在基础加成之后，
	// 因为它们的关系值要读到"已含装备基础加成"的系统（对照原版逐项 yield 的顺序）。
	applyItemOptionPowerUps(cfg, c, system)
	applyAncientBonusOptions(cfg, c, system)
	applySetGroupPowerUps(cfg, c, system)
	applyCombinationBonusPowerUps(cfg, c, system)
	// 被动大师技能的效果（TRIM-09e）：装备与套装都算完之后再挂（原版 SkillList 晚于
	// ItemAwareAttributeSystem 构造），活动 buff 仍在最后。
	applyMasterSkillPowerUps(cfg, c, system)
	applyMagicEffectPowerUps(cfg, c, effects, system)
	return system, nil
}

// splitAttributeBonuses 把任务"属性奖励"攒下的角色级加成按原版形态分流：
// 原版 ItemAwareAttributeSystem 把 character.Attributes 当作**stat 节点**原样放入系统
// （GetStatAttributes(account, character)），只有类里没有该属性时才走可聚合的基底元素。
// 本仓 character.Attributes 的角色态由 overrides 表达，故：
//   - 职业 StatAttributes 里有的 → 折进 overrides（在该designation 现值上**加**，不是替换）；
//   - 其余 → 建成系统后再挂常量元素（属性缺失时 AddElement 会惰性建空聚合）。
func splitAttributeBonuses(cfg *config.GameConfig, c *entity.Character, classNumber int) (map[string]float32, []entity.AttributeBonus) {
	if len(c.AttributeBonuses) == 0 {
		return nil, nil
	}
	stat := make(map[string]float32)
	var elements []entity.AttributeBonus
	cls, ok := cfg.Class(classNumber)
	if !ok {
		return nil, append(elements, c.AttributeBonuses...)
	}
	for _, b := range c.AttributeBonuses {
		if !classHasStatAttribute(cls, b.Designation) {
			elements = append(elements, b)
			continue
		}
		stat[b.Designation] += float32(b.Value)
	}
	return stat, elements
}

// classHasStatAttribute 报告该职业是否把这条属性列为可加点项。
func classHasStatAttribute(cls *config.CharacterClass, designation string) bool {
	for _, s := range cls.StatAttributes {
		if s.Designation == designation {
			return true
		}
	}
	return false
}

// classStatBaseValue 取该职业某条 stat 属性的初值（无此项时 0）。
func classStatBaseValue(cfg *config.GameConfig, classNumber int, designation string) float32 {
	cls, ok := cfg.Class(classNumber)
	if !ok {
		return 0
	}
	for _, s := range cls.StatAttributes {
		if s.Designation == designation {
			return float32(s.BaseValue)
		}
	}
	return 0
}

// applyAttributeBonuses 把非 stat 形态的角色属性加成作为常量元素注入其属性（聚合方式与
// 原版 StatAttribute 汇入基底一致：AddRaw）。
func applyAttributeBonuses(cfg *config.GameConfig, bonuses []entity.AttributeBonus, system *attribute.AttributeSystem) {
	for _, b := range bonuses {
		def := &attribute.AttributeDefinition{ID: b.AttributeID, Designation: b.Designation}
		if d, ok := cfg.AttributeDefinitionByID(b.AttributeID); ok {
			def.Designation = d.Designation
			def.MaximumValue = d.MaximumValue
		}
		system.AddElement(attribute.NewConstantElement(float32(b.Value), attribute.AggregateAddRaw), def)
	}
}

// applyEquipmentPowerUps 遍历装备区（槽 0..11），把每件已装备物品的
// 基础 PowerUp 注入系统。
func applyEquipmentPowerUps(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	if c.Inventory == nil {
		return
	}
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		slotted := c.Inventory.GetItem(slot)
		if slotted == nil || slotted.It == nil {
			continue
		}
		it := slotted.It
		def, ok := cfg.Item(int(it.Group), it.Number)
		if !ok {
			continue
		}
		for _, p := range def.BasePowerUpAttributes {
			if p.Target == "" {
				continue
			}
			targetDef, ok := cfg.AttributeByName(p.Target)
			if !ok {
				continue
			}
			value := p.BaseValue
			// 物品等级加成：查 BonusPerLevel 表中 level==物品等级 的一条，就地相加
			// （原版 GetBasePowerUpWrappers：基础值与等级加成 CombinedElement）。
			if p.BonusTable != "" {
				if table, ok := cfg.ItemLevelBonusTableByName(p.BonusTable); ok {
					value += table.BonusAt(int(it.Level))
				}
			}
			probe := &attribute.AttributeDefinition{
				ID: targetDef.ID, Designation: targetDef.Designation,
			}
			element := attribute.NewConstValueAttributeAgg(
				float32(value), probe, parsePowerUpAggregate(p.AggregateType))
			system.AddElement(element, probe)
		}
	}
}

// parsePowerUpAggregate 把导出的聚合类型名映射为 attribute 枚举
// （缺省 AddRaw，对照原版 AggregateType 枚举）。
func parsePowerUpAggregate(name string) attribute.AggregateType {
	switch name {
	case "Multiplicate":
		return attribute.AggregateMultiplicate
	case "AddFinal":
		return attribute.AggregateAddFinal
	case "Maximum":
		return attribute.AggregateMaximum
	default:
		return attribute.AggregateAddRaw
	}
}
