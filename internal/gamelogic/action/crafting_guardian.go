package action

// crafting_guardian.go —— Level380 守护者选项合成，对应
// PlayerActions/Craftings/GuardianOptionCrafting.cs。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// guardianTargetReference 对应原版 ItemReference = 0x88（数据里写作十进制 136）。
const guardianTargetReference = 0x88

var guardianHooks = optionHooks{
	// RequiredItemMatches：带引用的是"目标装备"——必须能挂守护者选项且当前尚未挂。
	matches: func(ctx *craftCtx, c MixCandidate, req config.CraftingRequired) bool {
		if req.Reference != guardianTargetReference {
			return true
		}
		if len(optionDefinitionsOfKind(ctx.cfg, c.Def, config.OptionKindGuardian)) == 0 {
			return false
		}
		return !isGuardianItem(c.It)
	},
	// 只允许一件目标装备。
	afterRequire: func(ctx *craftCtx, links []itemLink, rate byte) int {
		count := 0
		for _, l := range links {
			if l.req.Reference == guardianTargetReference {
				count += len(l.items)
			}
		}
		if count > 1 {
			return CraftTooManyItems
		}
		return craftNoReject
	},
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		target := singleLinkedItem(links, guardianTargetReference)
		if target == nil {
			return nil
		}
		target.GuardianOption = true
		return []*item.Item{target}
	},
}
