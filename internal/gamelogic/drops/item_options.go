package drops

// item_options.go —— 掉落时的随机选项（对照原版 DefaultDropGenerator：
// ApplyRandomOptions / ApplyOption / AddRandomExcOptions / ApplyRandomAncientOption）。
//
// 本仓物品是扁平位域模型（出站编码用），随机出的选项直接写回位域
// （幸运 / 普通选项等级 / 卓越位 / 远古判别值与额外属性档位），与出站编码、
// 装备加成计算（player.applyItemOptionPowerUps）共用同一套表示，因此不再需要
// 原版那条 ItemOptionLink 实例链。
//
// 未接线（登记见 doc/15）：镶嵌孔的球等级/子类型随机（原版 SocketCount 之外还要
// 逐孔随机 SocketSlots，依赖 SocketSystem 的 MaximumSocketOptions/offsets 导出）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/util"
)

// additionalDurabilityPerLevel 对照原版 ItemExtensions.AdditionalDurabilityPerLevel。
var additionalDurabilityPerLevel = [16]int{0, 1, 2, 3, 4, 6, 8, 10, 12, 14, 17, 21, 26, 32, 39, 47}

// applyRandomOptions 对照原版 ApplyRandomOptions：对物品定义里"随机附加"的候选定义逐个
// 掷骰（卓越类定义除外——它由 addRandomExcOptions 专门处理），随后按定义补镶嵌孔数与技能位。
func (g *Generator) applyRandomOptions(it *item.Item, def *config.Item) {
	for _, id := range g.cfg.ItemOptionDefinitionsFor(def.Group, def.Number) {
		d, ok := g.cfg.ItemOptionDefinition(id)
		if !ok || !d.AddsRandomly {
			continue
		}
		if d.HasKind(g.cfg, config.OptionKindExcellent) {
			continue
		}
		g.applyOption(it, d)
	}
	// 镶嵌孔数：1..MaximumSockets 随机（原版 NextInt(1, max+1)）。
	if def.MaximumSockets > 0 {
		it.SocketCount = g.rng.Next(1, def.MaximumSockets+1)
	}
	// 技能位：50%（原版 SkillDropChancePercent；NextRandomBool(50) 语义=51/100，原样保留）。
	if canHaveSkill(def) {
		it.HasSkill = g.rng.Next(0, 100) <= skillDropChancePct
	}
}

// canHaveSkill 对照原版 ItemExtensions.CanHaveSkill：
// 可穿戴 + 定义带技能 + 有合格职业（即"自带技能的装备"，如武器）。
func canHaveSkill(def *config.Item) bool {
	return def.IsWearable() && def.SkillNumber != nil && len(def.QualifiedClasses) > 0
}

// applyOption 对照原版 ApplyOption：最多 MaximumOptionsPerItem 次，每次按 AddChance 掷骰；
// 命中则从"尚未附加"的候选里随机取一条并写回位域。
func (g *Generator) applyOption(it *item.Item, d *config.ItemOptionDefinitionExport) {
	for i := 0; i < d.MaximumOptionsPerItem; i++ {
		if g.rng.NextDouble() >= d.AddChance {
			continue
		}
		var candidates []config.IncreasableItemOptionExport
		for _, o := range d.PossibleOptions {
			if !itemHasOption(g.cfg, it, &o) {
				candidates = append(candidates, o)
			}
		}
		if len(candidates) == 0 {
			break
		}
		pick := candidates[g.rng.Next(0, len(candidates))]
		g.applyOptionToBits(it, &pick)
	}
}

// addRandomExcOptions 对照原版 AddRandomExcOptions：卓越定义最多 MaximumOptionsPerItem 条，
// 第 1 条**必加**（不掷骰），之后每条按 AddChance 掷骰且不重复。
func (g *Generator) addRandomExcOptions(it *item.Item, def *config.Item) {
	d := g.cfg.DefinitionOfOptionType(g.cfg.ItemOptionDefinitionsFor(def.Group, def.Number), config.OptionKindExcellent)
	if d == nil || len(d.PossibleOptions) == 0 {
		return
	}
	existing := excellentOptionCount(it)
	for i := existing; i < d.MaximumOptionsPerItem; i++ {
		if i == 0 {
			pick := d.PossibleOptions[g.rng.Next(0, len(d.PossibleOptions))]
			g.applyOptionToBits(it, &pick)
			continue
		}
		if g.rng.NextDouble() >= d.AddChance {
			continue
		}
		var candidates []config.IncreasableItemOptionExport
		for _, o := range d.PossibleOptions {
			if !itemHasOption(g.cfg, it, &o) {
				candidates = append(candidates, o)
			}
		}
		if len(candidates) == 0 {
			return
		}
		pick := candidates[g.rng.Next(0, len(candidates))]
		g.applyOptionToBits(it, &pick)
	}
}

// applyRandomAncientOption 对照原版 ApplyRandomAncientOption：从物品所属的远古套装组里随机
// 选一套，写入判别值（1/2），并随机取该成员条目"额外远古属性"的档位（原版 +5 / +10）。
func (g *Generator) applyRandomAncientOption(it *item.Item, def *config.Item) {
	var sets []*config.ItemSetGroupExport
	for _, sg := range g.cfg.SetGroupsForItem(def.Group, def.Number) {
		if sg.OptionDefinition == "" {
			continue
		}
		if d, ok := g.cfg.ItemOptionDefinition(sg.OptionDefinition); ok &&
			d.HasKind(g.cfg, config.OptionKindAncientOption) {
			sets = append(sets, sg)
		}
	}
	if len(sets) == 0 {
		return
	}
	sg := sets[g.rng.Next(0, len(sets))]
	entry := setEntryFor(sg, def.Group, def.Number)
	if entry == nil {
		return
	}
	it.AncientDiscriminator = byte(entry.AncientSetDiscriminator)
	if entry.BonusOption != nil && len(entry.BonusOption.LevelDependentOptions) > 0 {
		lv := entry.BonusOption.LevelDependentOptions[g.rng.Next(0, len(entry.BonusOption.LevelDependentOptions))]
		it.AncientBonusLevel = byte(lv.Level)
	}
}

// setEntryFor 取套装组里对应 (group, number) 的成员条目（原版 First(i => i.ItemDefinition == item.Definition)）。
func setEntryFor(g *config.ItemSetGroupExport, group, number int) *config.ItemOfItemSetExport {
	for i := range g.Items {
		it := &g.Items[i]
		if it.Group != nil && it.Number != nil && *it.Group == group && *it.Number == number {
			return it
		}
	}
	return nil
}

// applyOptionToBits 把随机到的候选选项写回物品位域。
func (g *Generator) applyOptionToBits(it *item.Item, o *config.IncreasableItemOptionExport) {
	t, ok := g.cfg.ItemOptionType(o.OptionTypeID)
	if !ok {
		return
	}
	switch t.Kind {
	case config.OptionKindLuck:
		it.Luck = true
	case config.OptionKindOption:
		it.OptionLevel = randomOptionLevel(o, g.rng, g.cfg.MaximumItemOptionLevelDrop)
	case config.OptionKindExcellent:
		if o.Number >= 1 && o.Number <= 6 {
			it.ExcellentBits |= 1 << uint(o.Number-1)
		}
	}
}

// itemHasOption 判定物品位域里是否已包含该候选选项（对照原版 ApplyOption 的去重条件
// `item.ItemOptions.All(link => link.ItemOption != possibleOption)`）。
func itemHasOption(cfg *config.GameConfig, it *item.Item, o *config.IncreasableItemOptionExport) bool {
	t, ok := cfg.ItemOptionType(o.OptionTypeID)
	if !ok {
		return false
	}
	switch t.Kind {
	case config.OptionKindLuck:
		return it.Luck
	case config.OptionKindOption:
		return it.OptionLevel != 0 || it.WingOptionNumber > 0
	case config.OptionKindExcellent:
		return o.Number >= 1 && o.Number <= 6 && it.ExcellentBits&(1<<uint(o.Number-1)) != 0
	default:
		return false
	}
}

// excellentOptionCount 数出物品已带的卓越选项条数（位 1..6）。
func excellentOptionCount(it *item.Item) int {
	n := 0
	for bit := 1; bit <= 6; bit++ {
		if it.ExcellentBits&(1<<uint(bit-1)) != 0 {
			n++
		}
	}
	return n
}

// randomOptionLevel 对照原版 ApplyOption 的等级选择：候选等级 = 该选项等级表的等级再补一个 1
// （等级表非空时），过滤掉超过 maxLevel 的，随机取一个；全被过滤则取 0。
func randomOptionLevel(o *config.IncreasableItemOptionExport, rng *util.Rand, maxLevel int) int {
	var levels []int
	if len(o.LevelDependentOptions) > 0 {
		seen := make(map[int]bool, len(o.LevelDependentOptions)+1)
		for i := range o.LevelDependentOptions {
			l := o.LevelDependentOptions[i].Level
			if !seen[l] {
				seen[l] = true
				levels = append(levels, l)
			}
		}
		if !seen[1] {
			levels = append(levels, 1)
		}
	}
	var allowed []int
	for _, l := range levels {
		if l <= maxLevel {
			allowed = append(allowed, l)
		}
	}
	if len(allowed) == 0 {
		return 0
	}
	return allowed[rng.Next(0, len(allowed))]
}

// itemMaxDurability 对照原版 ItemExtensions.GetMaximumDurabilityOfOnePiece：
// 非穿戴物品的耐久只是"件数"，无意义（1）；可训练宠物恒 255；否则 = 定义耐久 + 等级加成，
// 远古 +20、卓越 +15，上限 255。调用方须在写完全部位域之后调用。
func itemMaxDurability(it *item.Item, def *config.Item) byte {
	if !def.IsWearable() {
		return 1
	}
	if action.IsTrainablePet(def.Group, def.Number) {
		return 255
	}
	level := int(it.Level)
	bonus := 0
	if level >= 0 && level < len(additionalDurabilityPerLevel) {
		bonus = additionalDurabilityPerLevel[level]
	}
	result := def.Durability + bonus
	if it.AncientDiscriminator != 0 {
		result += 20
	} else if it.ExcellentBits != 0 {
		result += 15
	}
	return byte(minInt(255, result))
}
