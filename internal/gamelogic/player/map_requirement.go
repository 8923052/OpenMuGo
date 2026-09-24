package player

// map_requirement.go —— 进图属性需求（TRIM-11c），对照
// GameLogic/PlayerActions/GameMapDefinitionExtensions.TryGetRequirementError:21-40。
//
// 原版只看一件事：玩家**活属性系统**里那条需求的属性值是否 ≥ MinimumValue。
// 属性由装备/饰品给（S6 数据里两条：Icarus 要翅膀/迪诺特/菲尼瑞/飞龙之一，
// Kanturu 活动图要装备月亮石吊坠），所以这里必须走完整装配而不是角色静态字段。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// MapEntryRequirementFailure 返回第一条未满足的进图属性需求（bool=true 表示应拒绝进图）。
// 文案取该属性定义的 **Description**（原版 `requirement.Attribute?.Description`）而不是
// designation —— 客户端看到的整句因此是 "You can enter Icarus only with wings, ..."。
func MapEntryRequirementFailure(cfg *config.GameConfig, c *entity.Character, target *config.GameMap) (string, bool) {
	if cfg == nil || c == nil || target == nil || len(target.Requirements) == 0 {
		return "", false
	}
	for _, r := range target.Requirements {
		if AttributeValue(cfg, c, r.Attribute) >= float64(r.Value) {
			continue
		}
		text := r.Attribute
		if def, ok := cfg.AttributeByName(r.Attribute); ok && def.Description != "" {
			text = def.Description
		}
		return text, true
	}
	return "", false
}
