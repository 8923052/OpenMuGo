package action

// crafting_wings.go —— 混沌武器/一阶/二阶/三阶翅膀与斗篷的成功率相关选项，
// 对应 PlayerActions/Craftings/{ChaosWeaponAndFirstWingsCrafting,SecondWingsCrafting,
// ThirdWingsCrafting}.cs 的 virtual 覆写。掷骰**次数与顺序**必须与原版一致。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

var chaosWingsHooks = optionHooks{
	// AddRandomItemOption：先掷 i，再按 (rate/5 + 4*(i+1)) 决定是否带普通选项，档位 3-i。
	itemOption: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		if definitionOfKind(ctx.cfg, def, config.OptionKindOption) == nil {
			return
		}
		i := nextInt(ctx.roll, 0, 3)
		if !nextRandomBool(ctx.roll, int(rate)/5+4*(i+1)) {
			return
		}
		it.OptionLevel = 3 - i
	},
	// AddRandomLuckOption：(rate/5 + 4)。
	luck: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		if !nextRandomBool(ctx.roll, int(rate)/5+4) {
			return
		}
		if firstOptionOfKind(ctx.cfg, def, config.OptionKindLuck) != nil {
			it.Luck = true
		}
	},
	// AddRandomSkill：(rate/5 + 6)。
	skill: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		if !nextRandomBool(ctx.roll, int(rate)/5+6) {
			return
		}
		if def.SkillNumber != nil && !it.HasSkill {
			it.HasSkill = true
		}
	},
}

// secondWingsHooks 对应 SecondWingsCrafting：三档 (20/10/4) 决定选项等级 (1/2/3)，
// 有两个选项定义时再掷一次取第二个（斗篷只有一个定义）。
var secondWingsHooks = optionHooks{
	itemOption: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		options := optionDefinitionsOfKind(ctx.cfg, def, config.OptionKindOption)
		if len(options) == 0 {
			return
		}
		chance, level := 4, 3
		switch nextInt(ctx.roll, 0, 3) {
		case 0:
			chance, level = 20, 1
		case 1:
			chance, level = 10, 2
		}
		if !nextRandomBool(ctx.roll, chance) {
			return
		}
		pick := options[0]
		if len(options) > 1 {
			pick = options[nextInt(ctx.roll, 0, 2)]
		}
		it.OptionLevel = level
		_ = pick
	},
}

// thirdWingsHooks 对应 ThirdWingsCrafting：四档（其中一档直接不带选项）+ 选项类别两次掷骰；
// 卓越覆写改成"翅膀特效选项"（忽略防御/反射/回血/回蓝四档）。
var thirdWingsHooks = optionHooks{
	itemOption: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		options := optionDefinitionsOfKind(ctx.cfg, def, config.OptionKindOption)
		if len(options) == 0 {
			return
		}
		chance1, level := 3, 3
		switch nextInt(ctx.roll, 0, 4) {
		case 0:
			chance1, level = 0, 0
		case 1:
			chance1, level = 12, 1
		case 2:
			chance1, level = 6, 2
		}
		if !nextRandomBool(ctx.roll, chance1) {
			return
		}
		chance2, idx := 30, 2
		if nextRandomBool(ctx.roll, 50) {
			chance2, idx = 40, 1
		}
		if !nextRandomBool(ctx.roll, chance2) {
			idx = 0
		}
		if idx >= len(options) {
			idx = len(options) - 1
		}
		it.OptionLevel = level
	},
	excellent: func(ctx *craftCtx, it *item.Item, def *config.Item) {
		wingDef := definitionOfKind(ctx.cfg, def, config.OptionKindWing)
		if wingDef == nil || len(wingDef.PossibleOptions) == 0 {
			return
		}
		chance, typeIdx := 7, 3
		switch nextInt(ctx.roll, 0, 4) {
		case 0:
			chance, typeIdx = 4, 0
		case 1:
			chance, typeIdx = 2, 1
		case 2:
			chance, typeIdx = 7, 2
		}
		if !nextRandomBool(ctx.roll, chance) {
			return
		}
		if typeIdx >= len(wingDef.PossibleOptions) {
			typeIdx = len(wingDef.PossibleOptions) - 1
		}
		it.WingOptionNumber = wingDef.PossibleOptions[typeIdx].Number
	},
}

// optionDefinitionsOfKind 列出物品定义里所有含该 kind 的选项定义（原版 Where(...)）。
func optionDefinitionsOfKind(cfg *config.GameConfig, def *config.Item, kind string) []*config.ItemOptionDefinitionExport {
	if cfg == nil || def == nil {
		return nil
	}
	var out []*config.ItemOptionDefinitionExport
	for _, id := range cfg.ItemOptionDefinitionsFor(def.Group, def.Number) {
		if d, ok := cfg.ItemOptionDefinition(id); ok && d.HasKind(cfg, kind) {
			out = append(out, d)
		}
	}
	return out
}
