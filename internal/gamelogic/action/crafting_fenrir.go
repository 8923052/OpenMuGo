package action

// crafting_fenrir.go —— 非泪升级（红非泪 → 蓝/黑非泪），对应
// PlayerActions/Craftings/FenrirUpgradeCrafting.cs。设置里无 settings（要求全在代码里构造）。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/pricing"
)

const (
	desAttackSpeedByWeapon = "Attack Speed by Weapons"
	desDefenseBase         = "Base Defense"
	fenrirHornReference    = 1
	fenrirWeaponReference  = 2
	fenrirArmorReference   = 3
)

type fenrirUpgradeCrafter struct{}

// required 对照 FenrirUpgradeCrafting.TryGetRequiredItems：必须恰好一件 ≥+4 且带普通选项的
// 武器或防具（两者只能择一）+ 非泪号角 + 混沌 + 5 颗生命宝石。
func (fenrirUpgradeCrafter) required(ctx *craftCtx, storage []MixCandidate) ([]itemLink, byte, int) {
	qualified := func(c MixCandidate) bool {
		return c.It != nil && c.It.Level >= 4 && c.It.OptionLevel > 0 &&
			c.Def != nil && c.Def.IsWearable()
	}
	weapons := filterCandidates(storage, func(c MixCandidate) bool {
		return qualified(c) && defHasTarget(ctx.cfg, c.Def, desAttackSpeedByWeapon)
	})
	armors := filterCandidates(storage, func(c MixCandidate) bool {
		return qualified(c) && defHasTarget(ctx.cfg, c.Def, desDefenseBase)
	})
	if len(weapons) > 0 && len(armors) > 0 {
		return nil, 0, CraftIncorrectMix
	}
	if len(weapons) == 0 && len(armors) == 0 {
		return nil, 0, CraftLackingMixItems
	}

	horn := transientByName(ctx, storage, "Horn of Fenrir")
	chaos := transientByName(ctx, storage, chaosItemName)
	life := transientManyByName(ctx, storage, "Jewel of Life", 5)
	if horn == nil || chaos == nil || life == nil {
		return nil, 0, CraftLackingMixItems
	}
	left := removeCandidates(storage, horn, chaos, life)
	target := weapons
	reference := fenrirWeaponReference
	if len(target) == 0 {
		target = armors
		reference = fenrirArmorReference
	}
	left = removeCandidates(left, target)
	if len(left) > 0 {
		return nil, 0, CraftTooManyItems
	}

	links := []itemLink{
		{items: itemsOf(horn), req: config.CraftingRequired{MinAmount: 1, MaxAmount: 1, Reference: fenrirHornReference, SuccessResult: config.MixStaysAsIs, FailResult: config.MixDisappear}},
		{items: itemsOf(chaos), req: transientReq(1, 1)},
		{items: itemsOf(life), req: transientReq(5, 5)},
		{items: itemsOf(target), req: transientReq(1, 1, reference)},
	}
	var price int64
	for _, c := range target {
		price += pricing.SellingPrice(c.Def, c.It)
	}
	rate := price * 100 / 1000000
	if rate > 79 {
		rate = 79
	}
	return links, byte(rate), craftNoReject
}

func (fenrirUpgradeCrafter) price(ctx *craftCtx, rate byte, links []itemLink) int64 {
	return 10_000_000
}

// results 对照 CreateOrModifyResultItemsAsync：号角耐久顶满，并按投入物是武器还是防具
// 加上全部黑/蓝非泪选项。
func (fenrirUpgradeCrafter) results(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
	horn := singleLinkedItem(links, fenrirHornReference)
	if horn == nil {
		return nil
	}
	horn.Durability = 255
	kind := config.OptionKindBlueFenrir
	if len(itemsOfLink(links, fenrirWeaponReference)) > 0 {
		kind = config.OptionKindBlackFenrir
	}
	def := itemDef(ctx.cfg, horn)
	for _, d := range optionDefinitionsOfKind(ctx.cfg, def, kind) {
		for i := range d.PossibleOptions {
			writeOptionBit(ctx.cfg, horn, &d.PossibleOptions[i])
		}
	}
	return []*item.Item{horn}
}

func transientReq(min, max int, reference ...int) config.CraftingRequired {
	req := config.CraftingRequired{MinAmount: min, MaxAmount: max, SuccessResult: config.MixDisappear, FailResult: config.MixDisappear}
	if len(reference) > 0 {
		req.Reference = reference[0]
	}
	return req
}

func transientByName(ctx *craftCtx, storage []MixCandidate, name string) []MixCandidate {
	if c := findByName(ctx, storage, name); c != nil {
		return []MixCandidate{*c}
	}
	return nil
}

func transientManyByName(ctx *craftCtx, storage []MixCandidate, name string, want int) []MixCandidate {
	var out []MixCandidate
	for i := range storage {
		if storage[i].Def != nil && storage[i].Def.Name == name {
			out = append(out, storage[i])
			if len(out) == want {
				return out
			}
		}
	}
	return nil
}

func filterCandidates(in []MixCandidate, keep func(MixCandidate) bool) []MixCandidate {
	var out []MixCandidate
	for _, c := range in {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// removeCandidates 从剩余里剔除已归入链接的候选（按指针身份，不复制）。
func removeCandidates(in []MixCandidate, removed ...[]MixCandidate) []MixCandidate {
	gone := map[*item.Item]bool{}
	for _, group := range removed {
		for _, c := range group {
			gone[c.It] = true
		}
	}
	var out []MixCandidate
	for _, c := range in {
		if !gone[c.It] {
			out = append(out, c)
		}
	}
	return out
}

// defHasTarget 报告定义的加成里是否有指向该属性名的项（武器攻速 / 基础防御）。
func defHasTarget(cfg *config.GameConfig, def *config.Item, designation string) bool {
	if cfg == nil || def == nil {
		return false
	}
	for _, d := range cfg.ItemOptionDefinitionsFor(def.Group, def.Number) {
		_ = d
	}
	for _, p := range def.BasePowerUpAttributes {
		if p.Target == designation {
			return true
		}
	}
	return false
}
