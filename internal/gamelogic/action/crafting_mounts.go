package action

// crafting_mounts.go —— 黑暗之马与 Dinorant 的选项附加，对应
// PlayerActions/Craftings/{DarkHorseCrafting,DinorantCrafting}.cs。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// darkHorseHooks 对应 DarkHorseCrafting.AddRandomItemOption：把该定义的**全部**坐骑选项加上（不掷骰）。
var darkHorseHooks = optionHooks{
	itemOption: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		d := definitionOfKind(ctx.cfg, def, config.OptionKindDarkHorse)
		if d == nil {
			return
		}
		for i := range d.PossibleOptions {
			writeOptionBit(ctx.cfg, it, &d.PossibleOptions[i])
		}
	},
}

const (
	desDamageReceiveMultiplier = "Damage Receive Multiplier" // 原版 Stats.DamageReceiveDecrement
	desMaximumAbility          = "Maximum Ability"
)

// dinorantHooks 对应 DinorantCrafting：投入的 Horn of Uniria 必须满耐久；
// 30% 出一条普通选项，其中再 20% 追加第二条（不重复时）。
var dinorantHooks = optionHooks{
	afterRequire: func(ctx *craftCtx, links []itemLink, rate byte) int {
		for _, l := range links {
			if !linkContainsNamed(ctx, l, "Horn of Uniria") {
				continue
			}
			for _, it := range l.items {
				if it.Durability < 255 {
					return CraftIncorrectMix
				}
			}
		}
		return craftNoReject
	},
	itemOption: func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte) {
		d := definitionOfKind(ctx.cfg, def, config.OptionKindOption)
		if d == nil || len(d.PossibleOptions) == 0 || !nextRandomBool(ctx.roll, 30) {
			return
		}
		i, ok := pickRandom(ctx.roll, len(d.PossibleOptions))
		if !ok {
			return
		}
		first := &d.PossibleOptions[i]
		it.OptionLevel = dinorantOptionLevel(first)
		if !nextRandomBool(ctx.roll, 20) || len(d.PossibleOptions) < 2 {
			return
		}
		// 第二条：掷到不同选项才追加（原版只在不等同时加链；位域模型只保留档位较大的一条）。
		j := nextInt(ctx.roll, 0, len(d.PossibleOptions))
		if j == i {
			return
		}
		if lvl := dinorantOptionLevel(&d.PossibleOptions[j]); lvl > it.OptionLevel {
			it.OptionLevel = lvl
		}
	},
}

// dinorantOptionLevel 对照原版：按选项加成的目标属性定档（1=减伤、2=AG、其余 4）。
func dinorantOptionLevel(o *config.IncreasableItemOptionExport) int {
	if o == nil || o.PowerUp == nil {
		return 4
	}
	switch o.PowerUp.Target {
	case desDamageReceiveMultiplier:
		return 1
	case desMaximumAbility:
		return 2
	}
	return 4
}

func linkContainsNamed(ctx *craftCtx, l itemLink, name string) bool {
	for _, it := range l.items {
		if it == nil {
			continue
		}
		if def := itemDef(ctx.cfg, it); def != nil && def.Name == name {
			return true
		}
	}
	return false
}
