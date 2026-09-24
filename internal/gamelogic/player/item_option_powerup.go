package player

// item_option_powerup.go —— 装备选项加成回灌属性系统（对照原版
// GameLogic/ItemPowerUpFactory：GetPowerUpsOfItemOptions / GetSetPowerUps /
// GetOptionCombinationBonus）。
//
// 本仓物品是扁平位域模型（出站编码用，见 entity/item/item.go），原版那条
// "ItemOptionLink → 具体 IncreasableItemOption"的实例链没有落地。好在**具体选项可由
// 物品定义的候选选项唯一确定**，因此按位域逐项还原：
//
//	幸运      → 候选定义中 kind==Luck 的选项（该定义只有一条候选）
//	普通选项  → kind==Option；Number 由翅膀档位决定（WingOptionNumber>0 取该档，否则 1）
//	卓越/翅膀 → kind∈{Excellent,Wing} 中 Number==位序号（ExcellentBits 位 i ↔ Number i+1）
//	和谐      → kind==HarmonyOption 中 Number==HarmonyNumber，等级取 HarmonyLevel
//	远古      → 套装组（applySetGroupPowerUps）+ 成员条目 BonusOption
//
// 未接线（登记见 doc/15）：
//   - 镶嵌 SocketSlots：反推 (SubOptionType, Number, 等级) 需要原版 SocketSystem 的
//     MaximumSocketOptions / SocketOptionIndexOffsets，尚未导出；
//   - 380 守护选项：同组的两条候选选项 Number 相同、位域只有 1 bit，无法区分是哪条
//     （需要物品实例携带选项明细）。
//
// 宠物选项（黑暗之马 / 三色 Fenrir）已接入 resolvePetOptions：宠物槽（8，渡鸦为 1）
// 落在装备区 0..11，基础加成由 applyEquipmentPowerUps 施加，等级由 applyTrainablePetLevel
// 注入，专属选项由本文件 resolveItemOptions→resolvePetOptions 还原。

import (
	"sort"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// itemOptionRef 是一条"已确定具体选项与生效等级"的选项（对照原版 ItemOptionLink）。
type itemOptionRef struct {
	option *config.IncreasableItemOptionExport
	level  int
}

// equippedItem 是"一件已装备且耐久 > 0 的物品"（套装组/组合奖励统计的输入，
// 对照原版 GetSetPowerUps 的 activeItems）。
type equippedItem struct {
	it  *item.Item
	def *config.Item
}

// optionKey 是组合奖励统计用的"选项类型 + 子类型"键。
type optionKey struct {
	kind string
	sub  int
}

// applyItemOptionPowerUps 把每件已装备物品的选项加成注入系统。
// 必须在 applyEquipmentPowerUps 之后调用：选项的 related 求值要读到已含装备基础加成的系统。
func applyItemOptionPowerUps(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	if c.Inventory == nil {
		return
	}
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		slotted := c.Inventory.GetItem(slot)
		if slotted == nil || slotted.It == nil {
			continue
		}
		it := slotted.It
		def, ok := cfg.Item(int(it.Group), it.Number)
		if !ok {
			continue
		}
		// Dinorant（skill 49）的等级表只到某档，超出时原版仍用基础加成而非跳过。
		isDinorant := def.SkillNumber != nil && *def.SkillNumber == 49
		for _, ref := range resolveItemOptions(cfg, it, def) {
			pu := optionPowerUpForLevel(ref.option, int(it.Level), ref.level, isDinorant)
			addOptionPowerUp(cfg, system, pu)
		}
	}
}

// resolveItemOptions 把物品实例的选项位域还原为具体选项列表（原版 ItemOptionLink 的等价物）。
func resolveItemOptions(cfg *config.GameConfig, it *item.Item, def *config.Item) []itemOptionRef {
	ids := cfg.ItemOptionDefinitionsFor(def.Group, def.Number)
	if len(ids) == 0 {
		return nil
	}
	var out []itemOptionRef
	if it.Luck {
		if o := firstOptionOfKind(cfg, ids, config.OptionKindLuck); o != nil {
			out = append(out, itemOptionRef{option: o})
		}
	}
	// 普通选项：普通装备的候选选项 Number 为 0（原版 CreateOptionDefinition 不设 Number），
	// 取第 1 条即可；翅膀档位写在 OptionByte 高 nibble（本模型 WingOptionNumber），
	// 按该 Number 命中对应档位的候选选项。
	if it.OptionLevel != 0 || it.WingOptionNumber > 0 {
		if d := cfg.DefinitionOfOptionType(ids, config.OptionKindOption); d != nil {
			var o *config.IncreasableItemOptionExport
			if it.WingOptionNumber > 0 {
				o = optionByNumber(d, it.WingOptionNumber)
			} else if len(d.PossibleOptions) > 0 {
				o = &d.PossibleOptions[0]
			}
			if o != nil {
				out = append(out, itemOptionRef{option: o, level: int(it.OptionLevel)})
			}
		}
	}
	// 卓越/翅膀选项位：位 i ↔ Number i+1（对照 ItemSerializerHelper.GetExcellentByte）。
	for bit := 1; bit <= 6; bit++ {
		if it.ExcellentBits&(1<<uint(bit-1)) == 0 {
			continue
		}
		for _, kind := range []string{config.OptionKindExcellent, config.OptionKindWing} {
			d := cfg.DefinitionOfOptionType(ids, kind)
			if d == nil {
				continue
			}
			if o := optionByNumber(d, bit); o != nil {
				out = append(out, itemOptionRef{option: o})
				break
			}
		}
	}
	// 和谐：Number 选条、HarmonyLevel 选档（等级表另受 RequiredItemLevel 门槛约束）。
	if it.HarmonyNumber > 0 {
		if d := cfg.DefinitionOfOptionType(ids, config.OptionKindHarmony); d != nil {
			if o := optionByNumber(d, int(it.HarmonyNumber)); o != nil {
				out = append(out, itemOptionRef{option: o, level: int(it.HarmonyLevel)})
			}
		}
	}
	out = append(out, resolvePetOptions(cfg, it, ids)...)
	return out
}

// resolvePetOptions 还原可训练/坐骑宠物的专属选项（对照原版这些选项挂在
// Item.ItemOptions 上）：
//   - 黑暗之马（13,4）：候选定义 kind==DarkHorse 的选项**固定全部施加**
//     （adds_randomly=false），其关系值按已注入的 "Dark Horse Level"/"Total Agility" 求值；
//   - Fenrir（13,37）：按 it.FenrirBits 位（1=Black / 2=Blue / 4=Gold）取同 Number 的
//     候选选项（Gold 同一 Number 携带多条，须全部命中）。同时供组合奖励（Fenrir 移速）
//     统计命中数量。
func resolvePetOptions(cfg *config.GameConfig, it *item.Item, ids []string) []itemOptionRef {
	var out []itemOptionRef
	if d := cfg.DefinitionOfOptionType(ids, config.OptionKindDarkHorse); d != nil {
		for i := range d.PossibleOptions {
			out = append(out, itemOptionRef{option: &d.PossibleOptions[i]})
		}
	}
	// 三色 Fenrir 共用同一候选定义（含 Black/Blue/Gold 三种 kind），按位域取各条。
	if d := cfg.DefinitionOfOptionType(ids, config.OptionKindBlackFenrir); d != nil {
		for _, bit := range []int{1, 2, 4} { // Black / Blue / Gold
			if it.FenrirBits&byte(bit) == 0 {
				continue
			}
			for _, o := range optionsByNumber(d, bit) {
				out = append(out, itemOptionRef{option: o})
			}
		}
	}
	return out
}

// optionsByNumber 返回定义里所有 Number 命中的候选选项（Gold Fenrir 一条 Number 携多条）。
func optionsByNumber(d *config.ItemOptionDefinitionExport, number int) []*config.IncreasableItemOptionExport {
	var out []*config.IncreasableItemOptionExport
	for i := range d.PossibleOptions {
		if d.PossibleOptions[i].Number == number {
			out = append(out, &d.PossibleOptions[i])
		}
	}
	return out
}

// firstOptionOfKind 取候选定义里指定 kind 的首条候选选项（幸运这类定义只有一条候选）。
func firstOptionOfKind(cfg *config.GameConfig, ids []string, kind string) *config.IncreasableItemOptionExport {
	d := cfg.DefinitionOfOptionType(ids, kind)
	if d == nil || len(d.PossibleOptions) == 0 {
		return nil
	}
	return &d.PossibleOptions[0]
}

// optionByNumber 在定义里按 Number 找候选选项。
func optionByNumber(d *config.ItemOptionDefinitionExport, number int) *config.IncreasableItemOptionExport {
	for i := range d.PossibleOptions {
		if d.PossibleOptions[i].Number == number {
			return &d.PossibleOptions[i]
		}
	}
	return nil
}

// optionPowerUpForLevel 取选项在指定等级的加成，对照原版 GetPowerUpsOfItemOptions：
//   - 等级来源由 LevelType 决定（ItemLevel 取物品等级，否则取选项等级）；
//   - 命中等级表 → 用该档加成；但 RequiredItemLevel > 物品等级时该档**未激活**（和谐常见）；
//   - 未命中等级表 → 回退选项自身加成（等级 1 的正常情形）；等级 >1 却无对应档位时
//     原版只告警并跳过该选项（Dinorant 例外，仍用基础加成）。
func optionPowerUpForLevel(o *config.IncreasableItemOptionExport, itemLevel, level int, isDinorant bool) *config.PowerUpDef {
	if o.LevelType == "ItemLevel" {
		level = itemLevel
	}
	for i := range o.LevelDependentOptions {
		l := &o.LevelDependentOptions[i]
		if l.Level != level {
			continue
		}
		if l.RequiredItemLevel > itemLevel {
			return nil
		}
		if l.PowerUp != nil {
			return l.PowerUp
		}
		return o.PowerUp
	}
	if level > 1 && !isDinorant {
		return nil
	}
	return o.PowerUp
}

// addOptionPowerUp 把一条加成注入系统：值 = 常量 + Σ 关系值（在装配时按当前系统求值，
// 与 buff 生产者同一套 evalPowerUpValue），聚合形态取 Boost.AggregateType。
func addOptionPowerUp(cfg *config.GameConfig, system *attribute.AttributeSystem, pu *config.PowerUpDef) {
	if pu == nil || pu.TargetID == "" {
		return
	}
	targetDef, ok := cfg.AttributeDefinitionByID(pu.TargetID)
	if !ok {
		return
	}
	probe := &attribute.AttributeDefinition{ID: targetDef.ID, Designation: targetDef.Designation}
	value := evalPowerUpValue(&pu.Boost, system)
	system.AddElement(attribute.NewConstValueAttributeAgg(
		value, probe, parsePowerUpAggregate(pu.Boost.AggregateType)), probe)
}

// applyAncientBonusOptions 施加远古套装成员的"额外远古属性"（原版 ItemOfItemSet.BonusOption）：
// 每件远古物品按其 AncientBonusLevel（1/2）取该属性定义的第 1/2 档（原版 +5 / +10）。
func applyAncientBonusOptions(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	if c.Inventory == nil {
		return
	}
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		sl := c.Inventory.GetItem(slot)
		if sl == nil || sl.It == nil {
			continue
		}
		it := sl.It
		if it.AncientDiscriminator == 0 || it.AncientBonusLevel == 0 {
			continue
		}
		for _, g := range cfg.SetGroupsForItem(int(it.Group), it.Number) {
			if g.AlwaysApplies {
				continue
			}
			entry := ancientEntryFor(g, int(it.Group), it.Number, int(it.AncientDiscriminator))
			if entry == nil || entry.BonusOption == nil {
				continue
			}
			pu := optionPowerUpForLevel(entry.BonusOption, int(it.Level), int(it.AncientBonusLevel), false)
			addOptionPowerUp(cfg, system, pu)
		}
	}
}

// ancientEntryFor 在套装组里按 (group, number) 与远古判别值找成员条目。
func ancientEntryFor(g *config.ItemSetGroupExport, group, number, discriminator int) *config.ItemOfItemSetExport {
	for i := range g.Items {
		it := &g.Items[i]
		if it.Group == nil || it.Number == nil || *it.Group != group || *it.Number != number {
			continue
		}
		if it.AncientSetDiscriminator == discriminator {
			return it
		}
	}
	return nil
}

// equippedItems 收集装备区（槽 0..11）中耐久 > 0 的物品。
func equippedItems(cfg *config.GameConfig, c *entity.Character) []equippedItem {
	if c.Inventory == nil {
		return nil
	}
	var out []equippedItem
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		sl := c.Inventory.GetItem(slot)
		if sl == nil || sl.It == nil || sl.It.Durability == 0 {
			continue
		}
		def, ok := cfg.Item(int(sl.It.Group), sl.It.Number)
		if !ok {
			continue
		}
		out = append(out, equippedItem{it: sl.It, def: def})
	}
	return out
}

// applySetGroupPowerUps 对照原版 ItemPowerUpFactory.GetSetPowerUps：统计每个套装组在
// 已装备物品中的件数——集齐取全部套装选项，未集齐（但达 MinimumItemCount）按 Number 升序
// 取前 (件数-1) 条。
//
// 本仓没有"物品显式加入套装组"的实例数据：AlwaysApplies 的组（防御率/等级套装）按成员表
// 判定归属；远古组额外要求物品的 AncientDiscriminator 与该成员条目的判别值一致
// （原版由掉落时的随机远古选项写入 Item.ItemSetGroups，本仓用该位域承载）。
func applySetGroupPowerUps(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	items := equippedItems(cfg, c)
	if len(items) == 0 {
		return
	}
	seen := make(map[*config.ItemSetGroupExport]bool)
	for _, e := range items {
		for _, g := range cfg.SetGroupsForItem(e.def.Group, e.def.Number) {
			if seen[g] {
				continue
			}
			seen[g] = true
			applyOneSetGroup(cfg, g, items, system)
		}
	}
}

// applyOneSetGroup 施加单个套装组的加成。
func applyOneSetGroup(cfg *config.GameConfig, g *config.ItemSetGroupExport, items []equippedItem, system *attribute.AttributeSystem) {
	if g.OptionDefinition == "" {
		return
	}
	def, ok := cfg.ItemOptionDefinition(g.OptionDefinition)
	if !ok || len(def.PossibleOptions) == 0 {
		return
	}
	var inGroup []equippedItem
	for _, e := range items {
		if g.SetLevel > 0 && int(e.it.Level) < g.SetLevel {
			continue
		}
		if !belongsToSetGroup(g, e) {
			continue
		}
		inGroup = append(inGroup, e)
	}
	if len(inGroup) == 0 {
		return
	}
	count := len(inGroup)
	if g.CountDistinct {
		uniq := make(map[[2]int]bool, count)
		for _, e := range inGroup {
			uniq[[2]int{e.def.Group, e.def.Number}] = true
		}
		count = len(uniq)
	}

	ordered := append([]config.IncreasableItemOptionExport(nil), def.PossibleOptions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Number < ordered[j].Number })

	switch {
	case len(g.Items) > 0 && count >= len(g.Items):
		// 集齐：取全部套装选项。
		for i := range ordered {
			addOptionPowerUp(cfg, system, ordered[i].PowerUp)
		}
	case count >= g.MinimumItemCount && count >= 2:
		// 未集齐：按 Number 取前 (件数-1) 条（原版 Take(itemCount - 1)）。
		n := count - 1
		if n > len(ordered) {
			n = len(ordered)
		}
		for i := 0; i < n; i++ {
			addOptionPowerUp(cfg, system, ordered[i].PowerUp)
		}
	}
}

// belongsToSetGroup 判定一件物品是否算作该套装组的成员。
func belongsToSetGroup(g *config.ItemSetGroupExport, e equippedItem) bool {
	for i := range g.Items {
		it := &g.Items[i]
		if it.Group == nil || it.Number == nil || *it.Group != e.def.Group || *it.Number != e.def.Number {
			continue
		}
		if g.AlwaysApplies {
			return true
		}
		if it.AncientSetDiscriminator != 0 && it.AncientSetDiscriminator == int(e.it.AncientDiscriminator) {
			return true
		}
	}
	return false
}

// applyCombinationBonusPowerUps 对照原版 GetOptionCombinationBonus：按已装备物品的选项
// 统计"选项类型 + 子类型"的命中数量，满足要求时给组合加成（AppliesMultipleTimes 可重复给）。
func applyCombinationBonusPowerUps(cfg *config.GameConfig, c *entity.Character, system *attribute.AttributeSystem) {
	if len(cfg.CombinationBonuses) == 0 {
		return
	}
	items := equippedItems(cfg, c)
	if len(items) == 0 {
		return
	}
	var pool []optionKey
	for _, e := range items {
		for _, ref := range resolveItemOptions(cfg, e.it, e.def) {
			t, ok := cfg.ItemOptionType(ref.option.OptionTypeID)
			if !ok {
				continue
			}
			pool = append(pool, optionKey{kind: t.Kind, sub: ref.option.SubOptionType})
		}
	}
	if len(pool) == 0 {
		return
	}
	for i := range cfg.CombinationBonuses {
		b := &cfg.CombinationBonuses[i]
		if b.Bonus == nil {
			continue
		}
		remaining := pool
		for {
			matched, rest := matchCombinationRequirements(cfg, b, remaining)
			if !matched {
				break
			}
			remaining = rest
			addOptionPowerUp(cfg, system, b.Bonus)
			if !b.AppliesMultipleTimes {
				break
			}
		}
	}
}

// matchCombinationRequirements 对照原版 AreRequiredOptionsFound：逐条要求从池中取够
// MinimumCount 个匹配项；全部满足时返回"移除已匹配项后的剩余池"。
func matchCombinationRequirements(cfg *config.GameConfig, b *config.ItemOptionCombinationBonusExport, pool []optionKey) (bool, []optionKey) {
	rest := append([]optionKey(nil), pool...)
	for _, req := range b.Requirements {
		kind := ""
		if t, ok := cfg.ItemOptionType(req.OptionTypeID); ok {
			kind = t.Kind
		}
		need := req.MinimumCount
		if need <= 0 {
			need = 1
		}
		for i := 0; i < len(rest) && need > 0; {
			if rest[i].kind == kind && rest[i].sub == req.SubOptionType {
				rest = append(rest[:i], rest[i+1:]...)
				need--
				continue
			}
			i++
		}
		if need > 0 {
			return false, pool
		}
	}
	return true, rest
}
