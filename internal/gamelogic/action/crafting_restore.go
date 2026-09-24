package action

// crafting_restore.go —— 和谐石还原，对应 PlayerActions/Craftings/RestoreItemCrafting.cs：
// 价格按目标装备当前的和谐选项档位查表，产出即"去掉和谐选项的原装备"。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// restorePricePerOptionLevel 对照 PricePerOptLvl（下标 = 和谐选项等级）。
var restorePricePerOptionLevel = []int64{
	100_000, 110_000, 120_000, 130_000, 140_000, 150_000, 200_000,
	220_000, 240_000, 280_000, 320_000, 360_000, 400_000, 500_000,
}

var restoreHooks = optionHooks{
	price: func(ctx *craftCtx, links []itemLink) int64 {
		it := firstLinkedItem(links)
		if it == nil {
			return restorePricePerOptionLevel[0]
		}
		level := int(it.HarmonyLevel)
		if level < 0 || level >= len(restorePricePerOptionLevel) {
			level = len(restorePricePerOptionLevel) - 1
		}
		return restorePricePerOptionLevel[level]
	},
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		it := firstLinkedItem(links)
		if it == nil {
			return nil
		}
		it.HarmonyNumber = 0
		it.HarmonyLevel = 0
		return []*item.Item{it}
	},
}

// firstLinkedItem 对照 requiredItems.First().Items.First()。
func firstLinkedItem(links []itemLink) *item.Item {
	for _, l := range links {
		if len(l.items) > 0 {
			return l.items[0]
		}
	}
	return nil
}

var _ = config.OptionKindHarmony
