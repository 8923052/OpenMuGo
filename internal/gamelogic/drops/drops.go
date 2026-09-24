// Package drops 是掉落生成器（doc/15 §10 T1-6，对应原版 GameLogic/DefaultDropGenerator.cs）。
//
// 忠实复刻的核心语义（与 internal/gamelogic/config 的掉落域数据配合）：
//   - 分区顺序 monster → map（原版为 monster → character → map → quest），且**怪物专属组
//     不做怪级门控**（原版 PartitionDropGroups 无 monster 参数那一路），其余来源过
//     IsGroupRelevant（MinimumMonsterLevel ≤ 怪级 ≤ MaximumMonsterLevel）——这一步决定
//     加权游标的分母，漏了会让低级怪掉高级物品并挤掉金币；
//   - PartitionDropGroups：Chance ≥ 1.0 → 必掉组；否则机率组；
//   - GenerateDrops：剩余掉落次数 = NumberOfMaximumItemDrops；必掉组逐个生成；
//     机率组按 SelectRandomGroup（NextDouble 阈值游标，Chance 总和 >1 时按比例缩放）；
//   - 金钱：Money 组选中且无物品清单时，money = gainedExperience + BaseMoneyDrop(7)；
//     注意 MoneyAmount 字段**不参与**怪物击杀路径（原版只在物品箱 GenerateItemDrop(groups)
//     路径用它），故此处与 MoneyAmount 无关；
//   - 组内物品过滤（GenerateItemFromGroup）：非怪物专属组再过
//     CanDropAtMonsterLevel + （宝石 || DropLevel==0 || DropLevel > 怪级-12）；
//   - 成型次序照抄：耐久在**组等级写入之前**按 +0 取满值（GenerateItemDrop 分支），
//     而按怪级掉落的分支（GenerateRandomItem(monsterLevel)）在等级之后取满值；
//     等级最终钳 MaximumItemLevel；Ancient/Excellent 的技能位是"必然带"（覆写随机结果）；
//   - 物品等级：ItemDropItemGroup → NextInt(min, max+1)（导出件判据见 generateItemDrop）；
//     组级 ItemLevel 优先级次之；怪物级掉落 = min((怪级-DropLevel)/3, Max)。
//
// 裁剪登记（尚未实现，接入条件与证据登记在 doc/16 TRIM-04）：
//  1. 角色级 / 任务级掉落组：原版顺序里的 character、quest 两级。OpenMU 自身对
//     Character.DropItemGroups **零写入**（S6 初始化无处赋值）；任务组挂在
//     GameConfiguration.DropItemGroups 由任务状态解析，导出件未带（95_quests 只有需求物品）。
//  2. 镶嵌球内容：原版只随机 SocketCount（已实现），逐孔的球等级/子类型由
//     ItemSerializerHelper 的 MaximumSocketOptions/SocketOptionIndexOffsets 常量决定，
//     这些常量不在导出件里。
//  3. 掉落物归属保护：原版 new DroppedItem(..., owners = killer.Party ?? killer)，
//     本仓 DropRegistry 尚无 owner 维度（拾取校验只做距离/格子占用）。
package drops

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/util"
)

// 常量对照原版 DefaultDropGenerator。
const (
	baseMoneyDrop      = 7
	dropLevelMaxGap    = 12
	skillDropChancePct = 50
)

// Generator 是掉落生成器（原版 DefaultDropGenerator）。
type Generator struct {
	// cfg 是完整配置：物品定义（PossibleItems 解析）+ 选项域（掉落随机选项）。
	cfg *config.GameConfig
	rng *util.Rand
	// perMonsterLevel 缓存（原版 _droppableItemsPerMonsterLevel /
	// _droppableSocketItemsPerMonsterLevel 两份）。
	possibleCache       map[int][]*config.Item
	socketPossibleCache map[int][]*config.Item
}

// NewGenerator 构造：cfg 为导出件的完整配置（物品 + 选项域）。
func NewGenerator(cfg *config.GameConfig, rng *util.Rand) *Generator {
	return &Generator{
		cfg:                 cfg,
		rng:                 rng,
		possibleCache:       make(map[int][]*config.Item),
		socketPossibleCache: make(map[int][]*config.Item),
	}
}

func (g *Generator) byKey(group, number int) *config.Item {
	for i := range g.cfg.Items {
		if g.cfg.Items[i].Group == group && g.cfg.Items[i].Number == number {
			return &g.cfg.Items[i]
		}
	}
	return nil
}

// canDropAtMonsterLevel 对应原版 CanDropAtMonsterLevel。
func canDropAtMonsterLevel(def *config.Item, monsterLevel int) bool {
	if def.DropLevel > monsterLevel {
		return false
	}
	return def.MaximumDropLevel == nil || monsterLevel <= *def.MaximumDropLevel
}

// possibleList 对应原版 GetPossibleList（带缓存）。
//
// 条件逐条对照 DefaultDropGenerator.GetPossibleList：
//
//	from it in this._droppableItems            // = Items.Where(i => i.DropsFromMonsters)
//	where CanDropAtMonsterLevel(it, monsterLevel)
//	      && (it.DropLevel > monsterLevel - DropLevelMaxGap)
//
// 第二个条件是**保留**语义：只排除"物品掉落等级远低于怪级（≤ 怪级-12）"的廉价物品，
// 即高级怪不掉低级杂货。反过来写成"跳过"会让怪级 < 12 时 `DropLevel > 负数` 恒成立而
// 跳过全部物品，possibleList 恒空 → RandomItem 组一件都掉不出来。
func (g *Generator) possibleList(monsterLevel int, socketOnly bool) []*config.Item {
	cache := g.possibleCache
	if socketOnly {
		cache = g.socketPossibleCache
	}
	if v, ok := cache[monsterLevel]; ok {
		return v
	}
	var out []*config.Item
	for i := range g.cfg.Items {
		def := &g.cfg.Items[i]
		if !def.DropsFromMonsters || !canDropAtMonsterLevel(def, monsterLevel) {
			continue
		}
		if def.DropLevel <= monsterLevel-dropLevelMaxGap {
			continue
		}
		// 镶嵌物品掉落只从"有孔"的定义里选（原版 GetPossibleList 的 MaximumSockets > 0）。
		if socketOnly && def.MaximumSockets == 0 {
			continue
		}
		out = append(out, def)
	}
	cache[monsterLevel] = out
	return out
}

// selectRandomGroup 对应原版 SelectRandomGroup。
func (g *Generator) selectRandomGroup(groups []config.DropGroupRef, totalChance float64) *config.DropGroupRef {
	remaining := g.rng.NextDouble()
	if totalChance > 1.0 {
		remaining *= totalChance
	}
	for i := range groups {
		if remaining > groups[i].Group.Chance {
			remaining -= groups[i].Group.Chance
			continue
		}
		return &groups[i]
	}
	return nil
}

// isGroupRelevant 对应原版 DefaultDropGenerator.IsGroupRelevant：
// 按怪物等级过滤掉落组（MinimumMonsterLevel ≤ 怪级 ≤ MaximumMonsterLevel）。
//
// 原版该判定的第 3 个条件是 group.Monster 必须等于当前怪；本项目由导出结构承担：
// monster_drops 以怪物号为键、groups 已是该怪专属；map_drops 的组不带怪物绑定
// （原版里 group.Monster == null 亦直接通过），故只保留等级两个条件。
//
// 注意原版只在 PartitionDropGroups(groups, monster) 带 monster 参数的调用里过滤，
// 而 monster.DropItemGroups 那条是不带参数的（不过滤）。本项目统一过滤不会产生偏差，
// 依据是导出数据里 11 条 monster_drops 组全部 min/max_monster_level 为 null——
// 恒等变换。若将来导出器开始给怪物专属组写等级门控，需在此引入"来源"区分。
func isGroupRelevant(m *config.Monster, g *config.DropGroup) bool {
	lvl := monsterLevelOf(m)
	if g.MinimumMonsterLevel != nil && lvl < *g.MinimumMonsterLevel {
		return false
	}
	if g.MaximumMonsterLevel != nil && lvl > *g.MaximumMonsterLevel {
		return false
	}
	return true
}

// GenerateDrops 对应原版 GenerateDrops：分区 → 必掉组 → 机率组（次数 = 怪物最大掉落数）。
//
// 分区规则照抄 PartitionDropGroups：怪物专属组走"无 monster 参数"的调用（不做怪级门控），
// 其余来源（map / character / quest）才过 IsGroupRelevant。顺序亦按原版：monster 在前。
func (g *Generator) GenerateDrops(monster *config.Monster, gainedExperience int, refs []config.DropGroupRef) (items []*item.Item, money uint32) {
	var guaranteed, chance []config.DropGroupRef
	for i := range refs {
		if !refs[i].FromMonster && !isGroupRelevant(monster, &refs[i].Group) {
			continue
		}
		if refs[i].Group.Chance >= 1.0 {
			guaranteed = append(guaranteed, refs[i])
		} else {
			chance = append(chance, refs[i])
		}
	}

	remaining := monster.NumberOfMaximumItemDrops
	for i := range guaranteed {
		if remaining <= 0 {
			break
		}
		it, m := g.generateDropOrMoney(monster, &guaranteed[i], gainedExperience)
		if it != nil {
			items = append(items, it)
		}
		if m != nil {
			money += *m
		}
		remaining--
	}

	if remaining > 0 && len(chance) > 0 {
		total := 0.0
		for i := range chance {
			total += chance[i].Group.Chance
		}
		for i := 0; i < remaining; i++ {
			group := g.selectRandomGroup(chance, total)
			if group == nil {
				continue
			}
			it, m := g.generateDropOrMoney(monster, group, gainedExperience)
			if it != nil {
				items = append(items, it)
			}
			if m != nil {
				money += *m
			}
		}
	}
	return items, money
}

// generateDropOrMoney 对应原版 GenerateItemDropOrMoney。
func (g *Generator) generateDropOrMoney(monster *config.Monster, ref *config.DropGroupRef, gainedExperience int) (*item.Item, *uint32) {
	group := &ref.Group
	if len(group.PossibleItems) > 0 {
		return g.generateItemFromGroup(monster, ref), nil
	}
	// 特殊类型（原版 GenerateSpecialItem）：Ancient / Excellent / RandomItem / SocketItem
	// 都按怪物等级从可掉清单随机，差别在筛选与随后的选项附加。
	if it := g.generateSpecialItem(monsterLevelOf(monster), group.ItemType); it != nil {
		return it, nil
	}
	if group.ItemType == "Money" {
		m := uint32(gainedExperience + baseMoneyDrop)
		return nil, &m
	}
	return nil, nil
}

// generateSpecialItem 对应原版 GenerateSpecialItem：按组类型生成特殊物品。
func (g *Generator) generateSpecialItem(monsterLevel int, itemType string) *item.Item {
	switch itemType {
	case "Ancient":
		return g.generateRandomAncient()
	case "Excellent":
		return g.generateRandomExcellentItem(monsterLevel)
	case "RandomItem":
		return g.generateRandomItem(monsterLevel, false)
	case "SocketItem":
		return g.generateRandomItem(monsterLevel, true)
	default:
		return nil
	}
}

// generateRandomAncient 对应原版 GenerateRandomAncient：从"属于某远古套装组"的可掉物品里
// 随机取一件。技能位不是随机——远古件**必带**技能（ApplyRandomOptions 之后覆写）。
func (g *Generator) generateRandomAncient() *item.Item {
	def := pickItem(g.ancientItems(), g.rng)
	if def == nil {
		return nil
	}
	it := g.newBlankFor(def)
	g.applyRandomOptions(it, def)
	it.HasSkill = canHaveSkill(def) // every ancient item got skill
	g.applyRandomAncientOption(it, def)
	it.Durability = itemMaxDurability(it, def)
	return it
}

// generateRandomExcellentItem 对应原版 GenerateRandomExcellentItem(monsterLevel)：
// 可掉清单按"怪级 - ExcellentItemDropLevelDelta"取，其余同卓越通用成型。
func (g *Generator) generateRandomExcellentItem(monsterLevel int) *item.Item {
	if monsterLevel < g.cfg.ExcellentItemDropLevelDelta {
		return nil
	}
	return g.generateRandomExcellentFrom(g.possibleList(monsterLevel-g.cfg.ExcellentItemDropLevelDelta, false))
}

// generateRandomExcellentFrom 卓越物品的成型核心（组的 PossibleItems 分支与特殊类型分支共用）。
func (g *Generator) generateRandomExcellentFrom(possible []*config.Item) *item.Item {
	def := pickItem(possible, g.rng)
	if def == nil {
		return nil
	}
	it := g.newBlankFor(def)
	g.applyRandomOptions(it, def)
	it.HasSkill = canHaveSkill(def) // every excellent item got skill
	g.addRandomExcOptions(it, def)
	it.Durability = itemMaxDurability(it, def)
	return it
}

// pickItem 在候选里随机取一件定义（原版 NextInt(0, count)）。
func pickItem(possible []*config.Item, rng *util.Rand) *config.Item {
	if len(possible) == 0 {
		return nil
	}
	return possible[rng.Next(0, len(possible))]
}

// ancientItems 返回"属于某个远古套装组"的可掉物品（原版构造期的 _ancientItems）。
func (g *Generator) ancientItems() []*config.Item {
	var out []*config.Item
	for i := range g.cfg.Items {
		def := &g.cfg.Items[i]
		if !def.DropsFromMonsters {
			continue
		}
		for _, sg := range g.cfg.SetGroupsForItem(def.Group, def.Number) {
			if sg.OptionDefinition == "" {
				continue
			}
			if d, ok := g.cfg.ItemOptionDefinition(sg.OptionDefinition); ok &&
				d.HasKind(g.cfg, config.OptionKindAncientOption) {
				out = append(out, def)
				break
			}
		}
	}
	return out
}

// generateItemFromGroup 对应原版 GenerateItemFromGroup：怪物专属组直接按组的 PossibleItems 选；
// 其余来源还要过"怪级能掉 + 不是远低于怪级的廉价货（宝石与 DropLevel=0 例外）"。
func (g *Generator) generateItemFromGroup(monster *config.Monster, ref *config.DropGroupRef) *item.Item {
	group := &ref.Group
	possibles := g.itemsOf(group.PossibleItems)
	if ref.FromMonster {
		return g.generateItemDrop(ref, possibles)
	}
	lvl := monsterLevelOf(monster)
	isJewel := group.ItemType == "Jewel"
	var kept []*config.Item
	for _, def := range possibles {
		if !canDropAtMonsterLevel(def, lvl) {
			continue
		}
		if !isJewel && def.DropLevel != 0 && def.DropLevel <= lvl-dropLevelMaxGap {
			continue
		}
		kept = append(kept, def)
	}
	return g.generateItemDrop(ref, kept)
}

// itemsOf 把组内 (group, number) 引用解析为物品定义（悬空引用不可选）。
func (g *Generator) itemsOf(refs []config.ItemRef) []*config.Item {
	out := make([]*config.Item, 0, len(refs))
	for _, r := range refs {
		if def := g.byKey(r.Group, r.Number); def != nil {
			out = append(out, def)
		}
	}
	return out
}

// generateItemDrop 对应原版 GenerateItemDrop(group, possibleItems)：按组类型成型 →
// 补满耐久（**在写入组等级之前**，故按 +0 件算）→ 组等级/等级区间 → 钳到定义上限。
func (g *Generator) generateItemDrop(ref *config.DropGroupRef, possibles []*config.Item) *item.Item {
	group := &ref.Group
	var it *item.Item
	switch group.ItemType {
	case "Ancient":
		it = g.generateRandomAncient()
	case "Excellent":
		it = g.generateRandomExcellentFrom(possibles)
	default:
		it = g.generateRandomFrom(possibles)
	}
	if it == nil {
		return nil
	}
	def := g.byKey(int(it.Group), it.Number)
	if def == nil {
		return nil
	}
	if it.Durability == 0 {
		it.Durability = itemMaxDurability(it, def)
	}
	// 原版按"是否 ItemDropItemGroup"分派随机等级区间；导出件里怪物/地图级组 0 条带区间
	// （S6 实测 4041 组），故以"有区间"为等价判据。
	switch {
	case group.MinimumLevel > 0 || group.MaximumLevel > 0:
		it.Level = g.randomItemLevel(group)
	case group.ItemLevel != nil:
		it.Level = byte(*group.ItemLevel)
	}
	if int(it.Level) > def.MaximumItemLevel {
		it.Level = byte(def.MaximumItemLevel)
	}
	return it
}

// generateRandomItem 对应原版 GenerateRandomItem(monsterLevel, isSocketItem)：等级由怪级推导，
// 耐久在等级之后取满值（与组等级分支的"+0 耐久"次序不同，照抄）。
func (g *Generator) generateRandomItem(monsterLevel int, socketOnly bool) *item.Item {
	it := g.generateRandomFrom(g.possibleList(monsterLevel, socketOnly))
	if it == nil {
		return nil
	}
	def := g.byKey(int(it.Group), it.Number)
	it.Level = byte(g.randomItemLevelFor(def, monsterLevel))
	it.Durability = itemMaxDurability(it, def)
	return it
}

// generateRandomFrom 从候选里随机一件并附随机选项（原版 GenerateRandomItem(possibleItems)）。
// 耐久留 0 由调用方补满——与原版 TemporaryItem 一致。
func (g *Generator) generateRandomFrom(possible []*config.Item) *item.Item {
	def := pickItem(possible, g.rng)
	if def == nil {
		return nil
	}
	it := g.newBlankFor(def)
	g.applyRandomOptions(it, def)
	return it
}

// newBlankFor 建"未成型"物品（等级与耐久都由后续步骤写入，对照原版 new TemporaryItem）。
func (g *Generator) newBlankFor(def *config.Item) *item.Item {
	return &item.Item{Group: byte(def.Group), Number: def.Number}
}

// randomItemLevelFor 计算随机物品等级：min((怪级 - DropLevel)/3, MaximumItemLevel)
// （原版 GetItemLevelByMonsterLevel）。
func (g *Generator) randomItemLevelFor(def *config.Item, monsterLevel int) int {
	level := (monsterLevel - def.DropLevel) / 3
	if level > def.MaximumItemLevel {
		level = def.MaximumItemLevel
	}
	if level < 0 {
		level = 0
	}
	return level
}

func (g *Generator) randomItemLevel(group *config.DropGroup) byte {
	return byte(g.rng.Next(group.MinimumLevel, group.MaximumLevel+1))
}

func monsterLevelOf(m *config.Monster) int {
	for _, a := range m.Attributes {
		if a.Designation == "Level" {
			return int(a.Value)
		}
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
