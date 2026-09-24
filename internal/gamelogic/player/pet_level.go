package player

// pet_level.go —— 可训练宠物（黑暗之马 / 黑暗渡鸦）的等级注入属性系统
// （对照原版 ItemPowerUpFactory.GetPetLevel：把 item.Level 以 AddRaw 落到
// Stats.HorseLevel / Stats.RavenLevel）。
//
// 这一步必须在 applyItemOptionPowerUps **之前**执行：黑暗之马的选项（减伤、
// 防御）按关系读 "Dark Horse Level"，而 Go 的 evalPowerUpValue 是装配期即时求值，
// 等级元素须先于选项求值进系统。渡鸦的伤害/攻速/攻击率同样由 "Dark Raven Level"
// 经职业关系图派生（见导出件 10_character_classes.json）。

import (
	"mugo/internal/attribute"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/storage"
)

// 可训练宠物等级属性的 designation（与导出件 80_attributes.json 逐字一致）。
const (
	desHorseLevel = "Dark Horse Level"
	desRavenLevel = "Dark Raven Level"
)

// applyTrainablePetLevel 把装备区里可训练宠物的等级注入属性系统。
func applyTrainablePetLevel(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
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
		if !ok || !def.IsTrainablePet() {
			continue
		}
		designation := desRavenLevel
		if def.IsDarkHorse() {
			designation = desHorseLevel
		}
		addRawAttribute(cfg, system, designation, float32(it.Level))
	}
}

// addRawAttribute 以 AddRaw 形态把常量注入指定 designation 的属性（找不到定义则忽略）。
func addRawAttribute(cfg *config.GameConfig, system *attribute.AttributeSystem, designation string, value float32) {
	attrDef, ok := cfg.AttributeByName(designation)
	if !ok {
		return
	}
	probe := &attribute.AttributeDefinition{ID: attrDef.ID, Designation: attrDef.Designation}
	system.AddElement(attribute.NewConstValueAttributeAgg(value, probe, attribute.AggregateAddRaw), probe)
}
