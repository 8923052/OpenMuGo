package action

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// crafting_refine.go —— 精修石合成，对应 PlayerActions/Craftings/RefineStoneCrafting.cs。
// 排除表是原版硬编码的 84 件裸装备（等级<4 时不可精炼），逐条照抄。

var refineExcludedItems = map[[2]int]bool{
	{0, 0}:   true,
	{0, 1}:   true,
	{0, 2}:   true,
	{0, 4}:   true,
	{1, 0}:   true,
	{1, 1}:   true,
	{1, 2}:   true,
	{2, 0}:   true,
	{2, 1}:   true,
	{2, 2}:   true,
	{3, 1}:   true,
	{3, 2}:   true,
	{3, 3}:   true,
	{3, 5}:   true,
	{4, 0}:   true,
	{4, 1}:   true,
	{4, 2}:   true,
	{4, 3}:   true,
	{4, 8}:   true,
	{4, 9}:   true,
	{4, 10}:  true,
	{4, 11}:  true,
	{5, 0}:   true,
	{5, 1}:   true,
	{5, 2}:   true,
	{6, 0}:   true,
	{6, 1}:   true,
	{6, 2}:   true,
	{6, 3}:   true,
	{6, 4}:   true,
	{6, 6}:   true,
	{6, 7}:   true,
	{6, 9}:   true,
	{6, 10}:  true,
	{7, 0}:   true,
	{7, 2}:   true,
	{7, 4}:   true,
	{7, 5}:   true,
	{7, 6}:   true,
	{7, 7}:   true,
	{7, 8}:   true,
	{7, 10}:  true,
	{7, 11}:  true,
	{7, 12}:  true,
	{8, 0}:   true,
	{8, 2}:   true,
	{8, 4}:   true,
	{8, 5}:   true,
	{8, 6}:   true,
	{8, 7}:   true,
	{8, 8}:   true,
	{8, 10}:  true,
	{8, 11}:  true,
	{8, 12}:  true,
	{9, 0}:   true,
	{9, 2}:   true,
	{9, 4}:   true,
	{9, 5}:   true,
	{9, 6}:   true,
	{9, 7}:   true,
	{9, 8}:   true,
	{9, 10}:  true,
	{9, 11}:  true,
	{9, 12}:  true,
	{10, 0}:  true,
	{10, 2}:  true,
	{10, 4}:  true,
	{10, 5}:  true,
	{10, 6}:  true,
	{10, 7}:  true,
	{10, 8}:  true,
	{10, 10}: true,
	{10, 11}: true,
	{10, 12}: true,
	{11, 0}:  true,
	{11, 2}:  true,
	{11, 4}:  true,
	{11, 5}:  true,
	{11, 6}:  true,
	{11, 7}:  true,
	{11, 8}:  true,
	{11, 10}: true,
	{11, 11}: true,
	{11, 12}: true,
}

var refineHooks = optionHooks{
	matches: func(ctx *craftCtx, c MixCandidate, req config.CraftingRequired) bool {
		it, def := c.It, c.Def
		if def == nil || !def.IsWearable() {
			return false
		}
		if isAncientItem(it) {
			return false
		}
		if itemHasOptionKind(ctx.cfg, it, config.OptionKindHarmony) {
			return false
		}
		excluded := refineExcludedItems[[2]int{int(it.Group), it.Number}]
		return !excluded || it.Level > 3
	},
	// CreateOrModifyResultItemsAsync：逐件掷骰，高级石 50%（引用 0x22）、低级石 20%（0x11）。
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		var out []*item.Item
		for _, spec := range []struct {
			reference int
			chance    int
			number    int
		}{{0x22, 50, 44}, {0x11, 20, 43}} {
			count := len(itemsOfLink(links, spec.reference))
			for i := 0; i < count; i++ {
				if !nextRandomBool(ctx.roll, spec.chance) {
					continue
				}
				def, ok := ctx.cfg.Item(14, spec.number)
				if !ok {
					continue
				}
				it := &item.Item{Group: byte(def.Group), Number: def.Number}
				it.Durability = 1
				out = append(out, it)
			}
		}
		return out
	},
}
