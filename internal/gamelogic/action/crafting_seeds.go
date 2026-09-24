package action

// crafting_seeds.go —— 镶嵌系三件配方，对应 PlayerActions/Craftings/{SeedSphereCrafting,
// MountSeedSphereCrafting,RemoveSeedSphereCrafting}.cs。
// 槽位字节按原版 ItemSerializerHelper 的编码写入：(球等级-1)*50 + offsets[子选项类型] + 选项号。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

const (
	maximumSocketOptions = 50

	seedSphereReference = 0x77 // 数据里的 reference 是十进制 119
	seedRef             = 0x77
	sphereRef           = 0x66 // 十进制 102
	socketItemReference = 0x88

	subOptionFire      = 0
	subOptionWater     = 1
	subOptionIce       = 2
	subOptionWind      = 3
	subOptionLightning = 4
	subOptionEarth     = 5
)

// socketOptionIndexOffsets 对照 ItemSerializerHelper.SocketOptionIndexOffsets（按子选项类型）。
var socketOptionIndexOffsets = [6]int{0, 10, 16, 21, 29, 36}

// seedSphereHooks 对应 SeedSphereCrafting：种子(12,60..65) + 球(12,70..75) → 球种(12,100+)。
var seedSphereHooks = optionHooks{
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		seed := singleLinkedItem(links, seedRef)
		sphere := singleLinkedItem(links, sphereRef)
		if seed == nil || sphere == nil {
			return nil
		}
		seedDef := itemDef(ctx.cfg, seed)
		sphereDef := itemDef(ctx.cfg, sphere)
		if seedDef == nil || sphereDef == nil {
			return nil
		}
		const seedTypes = 6
		sphereLevel := sphereDef.Number - 70
		number := 100 + (seedDef.Number - 60) + sphereLevel*seedTypes
		def, ok := ctx.cfg.Item(12, number)
		if !ok {
			return nil
		}
		created := &item.Item{Group: byte(def.Group), Number: def.Number, Level: seed.Level}
		created.Durability = maxDurabilityOfOnePiece(ctx.cfg, created)
		return []*item.Item{created}
	},
}

// mountSeedHooks 对应 MountSeedSphereCrafting：把球种镶进指定槽，满 3 槽且形态匹配时 30% 出槽位奖励。
var mountSeedHooks = optionHooks{
	afterRequire: func(ctx *craftCtx, links []itemLink, rate byte) int {
		sphere := singleLinkedItem(links, seedSphereReference)
		target := singleLinkedItem(links, socketItemReference)
		sphereDef, targetDef := itemDef(ctx.cfg, sphere), itemDef(ctx.cfg, target)
		if sphereDef == nil || targetDef == nil {
			return CraftLackingMixItems
		}
		sphereOptionDef := singleOptionDefinition(ctx.cfg, sphereDef)
		if sphereOptionDef == nil {
			return CraftIncorrectMix
		}
		for _, d := range optionDefinitionsOfKind(ctx.cfg, targetDef, config.OptionKindSocket) {
			if d.ID == sphereOptionDef.ID {
				return craftNoReject
			}
		}
		return CraftIncorrectMix
	},
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		sphere := singleLinkedItem(links, seedSphereReference)
		target := singleLinkedItem(links, socketItemReference)
		if sphere == nil || target == nil {
			return nil
		}
		if target.SocketCount <= int(ctx.socketSlot) {
			return nil // 原版抛 ArgumentException → ItemCraftAction 归为 LackingMixItems（此处不改物）
		}
		if target.SocketFilled[ctx.socketSlot] {
			return nil
		}
		sphereDef := itemDef(ctx.cfg, sphere)
		opt := socketOptionOfLevel(ctx.cfg, sphereDef, int(sphere.Level))
		if opt == nil {
			return nil
		}
		target.SocketFilled[ctx.socketSlot] = true
		target.SocketSlots[ctx.socketSlot] = socketByteOf(opt, int(sphere.Level))
		if socketOptionCount(target) == 3 && nextRandomBool(ctx.roll, 30) {
			if bonus := socketBonusOption(ctx, target); bonus != nil {
				target.HasSocketBonus = true
				target.SocketBonus = byte(bonus.Number)
			}
		}
		return []*item.Item{target}
	},
}

// removeSeedHooks 对应 RemoveSeedSphereCrafting：卸下指定槽的球，槽位<3 时一并去掉槽位奖励。
var removeSeedHooks = optionHooks{
	results: func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
		target := singleLinkedItem(links, socketItemReference)
		if target == nil || ctx.socketSlot >= 5 || !target.SocketFilled[ctx.socketSlot] {
			return nil
		}
		target.SocketFilled[ctx.socketSlot] = false
		target.SocketSlots[ctx.socketSlot] = 0
		if ctx.socketSlot < 3 {
			target.HasSocketBonus = false
			target.SocketBonus = 0
		}
		return []*item.Item{target}
	},
}

// socketByteOf 复刻 GetSocketByte：(球等级-1)*50 + 子选项偏移 + 选项号。
func socketByteOf(opt *config.IncreasableItemOptionExport, level int) byte {
	if level < 1 {
		level = 1
	}
	sub := opt.SubOptionType
	if sub < 0 || sub >= len(socketOptionIndexOffsets) {
		sub = 0
	}
	return byte((level-1)*maximumSocketOptions + socketOptionIndexOffsets[sub] + opt.Number)
}

// socketOptionOfLevel 在球种定义的镶嵌候选里按"等级=种子等级"取选项（原版 Number == seed.Level）。
func socketOptionOfLevel(cfg *config.GameConfig, sphereDef *config.Item, level int) *config.IncreasableItemOptionExport {
	if sphereDef == nil {
		return nil
	}
	for _, d := range optionDefinitionsOfKind(cfg, sphereDef, config.OptionKindSocket) {
		for i := range d.PossibleOptions {
			if d.PossibleOptions[i].Number == level {
				return &d.PossibleOptions[i]
			}
		}
	}
	return nil
}

// singleOptionDefinition 取物品定义里唯一的选项定义（原版 PossibleItemOptions.Single()）。
func singleOptionDefinition(cfg *config.GameConfig, def *config.Item) *config.ItemOptionDefinitionExport {
	if def == nil {
		return nil
	}
	ids := cfg.ItemOptionDefinitionsFor(def.Group, def.Number)
	if len(ids) != 1 {
		return nil
	}
	d, ok := cfg.ItemOptionDefinition(ids[0])
	if !ok {
		return nil
	}
	return d
}

func socketOptionCount(it *item.Item) int {
	n := 0
	for i := 0; i < it.SocketCount && i < len(it.SocketFilled); i++ {
		if it.SocketFilled[i] {
			n++
		}
	}
	return n
}

// socketBonusOption 对照 GetPossibleBonusOption：按槽序取前三颗球的子选项类型组合，
// 火雷冰/雷冰火/水地风/地风水 四种序列分别奖励"最小/最大"编号的槽位奖励选项。
func socketBonusOption(ctx *craftCtx, it *item.Item) *config.IncreasableItemOptionExport {
	def := itemDef(ctx.cfg, it)
	bonus := definitionOfKind(ctx.cfg, def, config.OptionKindSocketBonus)
	if bonus == nil || len(bonus.PossibleOptions) == 0 {
		return nil
	}
	var subs []int
	for slot := 0; slot < 3 && slot < it.SocketCount; slot++ {
		if !it.SocketFilled[slot] {
			return nil
		}
		opt := socketOptionFromByte(ctx.cfg, def, it.SocketSlots[slot])
		if opt == nil {
			return nil
		}
		subs = append(subs, opt.SubOptionType)
	}
	if len(subs) < 3 {
		return nil
	}
	match := func(want ...int) bool {
		for i := range want {
			if subs[i] != want[i] {
				return false
			}
		}
		return true
	}
	ordered := append([]config.IncreasableItemOptionExport(nil), bonus.PossibleOptions...)
	sortByNumber(ordered)
	first := &ordered[0]
	last := &ordered[len(ordered)-1]
	switch {
	case match(subOptionFire, subOptionLightning, subOptionIce),
		match(subOptionWater, subOptionEarth, subOptionWind):
		return first
	case match(subOptionLightning, subOptionIce, subOptionFire),
		match(subOptionEarth, subOptionWind, subOptionWater):
		return last
	}
	return nil
}

func sortByNumber(opts []config.IncreasableItemOptionExport) {
	for i := 1; i < len(opts); i++ {
		for j := i; j > 0 && opts[j].Number < opts[j-1].Number; j-- {
			opts[j], opts[j-1] = opts[j-1], opts[j]
		}
	}
}

// socketOptionFromByte 是 GetSocketByte 的逆运算：从槽字节还原镶嵌选项。
func socketOptionFromByte(cfg *config.GameConfig, def *config.Item, b byte) *config.IncreasableItemOptionExport {
	if def == nil || b == 0 {
		return nil
	}
	optionIndex := int(b) % maximumSocketOptions
	offset := 0
	elementType := 0
	for i, o := range socketOptionIndexOffsets {
		if o <= optionIndex {
			offset, elementType = o, i
		}
	}
	number := optionIndex - offset
	for _, d := range optionDefinitionsOfKind(cfg, def, config.OptionKindSocket) {
		for i := range d.PossibleOptions {
			if d.PossibleOptions[i].SubOptionType == elementType && d.PossibleOptions[i].Number == number {
				return &d.PossibleOptions[i]
			}
		}
	}
	return nil
}

// singleLinkedItem 取指定 reference 要求匹配到的唯一物品（多件时返回第一件）。
func singleLinkedItem(links []itemLink, reference int) *item.Item {
	items := itemsOfLink(links, reference)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
