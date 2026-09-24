package action

// crafting_options.go —— 合成产出物的选项读写（本包内的扁平位域表示）。
// 原版是 ItemOptionLink 实例链，本仓物品是位域模型（与出站编码、装备加成共用同一表示），
// 因此这里做的是"选项 kind ↔ 位域"的双向映射，判定口径与 drops/item_options.go 一致。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// definitionOfKind 返回物品定义里第一个含该 kind 候选的选项定义。
func definitionOfKind(cfg *config.GameConfig, def *config.Item, kind string) *config.ItemOptionDefinitionExport {
	if cfg == nil || def == nil {
		return nil
	}
	for _, id := range cfg.ItemOptionDefinitionsFor(def.Group, def.Number) {
		d, ok := cfg.ItemOptionDefinition(id)
		if ok && d.HasKind(cfg, kind) {
			return d
		}
	}
	return nil
}

// firstOptionOfKind 返回该 kind 的第一个候选（原版 PossibleOptions.First()）。
func firstOptionOfKind(cfg *config.GameConfig, def *config.Item, kind string) *config.IncreasableItemOptionExport {
	d := definitionOfKind(cfg, def, kind)
	if d == nil || len(d.PossibleOptions) == 0 {
		return nil
	}
	return &d.PossibleOptions[0]
}

// optionKind 反查候选所属选项类型的 kind（未知返回空串）。
func optionKind(cfg *config.GameConfig, o *config.IncreasableItemOptionExport) string {
	if cfg == nil || o == nil {
		return ""
	}
	t, ok := cfg.ItemOptionType(o.OptionTypeID)
	if !ok {
		return ""
	}
	return t.Kind
}

// itemHasOptionKind 判定物品位域是否已带该 kind（投入要求 required_options 用）。
func itemHasOptionKind(cfg *config.GameConfig, it *item.Item, kind string) bool {
	if it == nil {
		return false
	}
	switch kind {
	case config.OptionKindLuck:
		return it.Luck
	case config.OptionKindOption:
		return it.OptionLevel > 0
	case config.OptionKindExcellent:
		return it.ExcellentBits != 0
	case config.OptionKindWing:
		return it.WingOptionNumber > 0
	case config.OptionKindAncientBonus:
		return it.AncientBonusLevel > 0
	case config.OptionKindHarmony:
		return it.HarmonyNumber > 0
	case config.OptionKindGuardian:
		return it.GuardianOption
	case config.OptionKindSocket:
		return hasSocketFilled(it)
	case config.OptionKindSocketBonus:
		return it.HasSocketBonus
	case config.OptionKindBlueFenrir:
		return it.FenrirBits&0x04 != 0
	case config.OptionKindBlackFenrir:
		return it.FenrirBits&0x02 != 0
	case config.OptionKindGoldFenrir:
		return it.FenrirBits&0x08 != 0
	case config.OptionKindDarkHorse:
		return it.FenrirBits&0x10 != 0
	}
	_ = cfg
	return false
}

// writeOptionBit 把一条候选写进物品位域（翅膀/坐骑按 kind 落各自的字段）。
func writeOptionBit(cfg *config.GameConfig, it *item.Item, o *config.IncreasableItemOptionExport) {
	switch optionKind(cfg, o) {
	case config.OptionKindLuck:
		it.Luck = true
	case config.OptionKindOption:
		if it.OptionLevel < 1 {
			it.OptionLevel = 1
		}
	case config.OptionKindExcellent:
		if o.Number >= 1 && o.Number <= 6 {
			it.ExcellentBits |= 1 << uint(o.Number-1)
		}
	case config.OptionKindWing:
		it.WingOptionNumber = o.Number
	case config.OptionKindBlackFenrir:
		it.FenrirBits |= 0x02
	case config.OptionKindBlueFenrir:
		it.FenrirBits |= 0x04
	case config.OptionKindGoldFenrir:
		it.FenrirBits |= 0x08
	case config.OptionKindDarkHorse:
		it.FenrirBits |= 0x10
	case config.OptionKindGuardian:
		it.GuardianOption = true
	case config.OptionKindSocketBonus:
		it.HasSocketBonus = true
		if it.SocketBonus == 0 {
			it.SocketBonus = byte(o.Number)
		}
	}
}

func hasSocketFilled(it *item.Item) bool {
	for _, filled := range it.SocketFilled {
		if filled {
			return true
		}
	}
	return false
}
