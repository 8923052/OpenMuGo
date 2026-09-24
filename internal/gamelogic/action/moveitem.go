// moveitem.go —— 背包搬运 / 装备穿脱决策（对照原版 GameLogic/PlayerActions/Items/MoveItemAction.cs
// 与 GameLogic/ItemExtensions.cs、DataModel/ItemExtensions.cs、PlayerItemExtensions.cs）。
//
// 三道门（CanMoveAsync 原样保留顺序）：
//  1. 装到装备区：`ItemSlot.ItemSlots.Contains(toSlot)` 槽位匹配；
//  2. `CompliesRequirements`：需求属性（按物品等级/掉落等级推算）+ 职业合格；
//  3. `ConflictsWithEquippedHands`：双手武器与盾/武器的互斥。
//
// 非装备槽走 `ItemFitsAtNewLocation`：目标格有物 → 堆叠（Complete/Partial）或拒绝；
// 否则做"忽略源件自身占位"的矩形占位检查。
package action

import (
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// MoveKind 对照原版 MoveItemAction.Movement 枚举。
type MoveKind int

const (
	MoveNone MoveKind = iota
	MoveNormal
	MovePartiallyStack
	MoveCompleteStack
)

// 装备区槽位与物品组（原版 InventoryConstants / ItemExtensions.ShieldItemGroup）。
const (
	LeftHandSlot    = 0
	RightHandSlot   = 1
	ShieldItemGroup = 6
	// SwordStaffBookGroup 是召唤师书所在的组（原版 GetRequirement 的书类特判 group == 5）。
	SwordStaffBookGroup = 5
)

// 属性 designation（与导出件逐字一致）。
const (
	desReqStrength     = "Total Strength Requirement Value"
	desReqAgility      = "Total Agility Requirement Value"
	desReqEnergy       = "Total Energy Requirement Value"
	desReqVitality     = "Total Vitality Requirement Value"
	desReqLeadership   = "Total Leadership Requirement Value"
	desTotalStrength   = "Total Strength"
	desTotalAgility    = "Total Agility"
	desTotalEnergy     = "Total Energy"
	desTotalVitality   = "Total Vitality"
	desTotalLeadership = "Total Leadership"
)

// requirementAttributeMapping 对照原版 RequirementAttributeMapping：需求属性 → 比对用的总量属性。
var requirementAttributeMapping = map[string]string{
	desReqStrength:   desTotalStrength,
	desReqAgility:    desTotalAgility,
	desReqEnergy:     desTotalEnergy,
	desReqVitality:   desTotalVitality,
	desReqLeadership: desTotalLeadership,
}

// MoveRequirement 对照原版 AttributeRequirement（装备需求条目）。
type MoveRequirement struct {
	Attribute string // 属性 designation
	Value     int    // 原版 MinimumValue
}

// MoveItemDef 是装备判定所需的物品定义投影（由 GS 从 config.Item 装配）。
type MoveItemDef struct {
	Group              int
	Width              int
	Height             int
	DropLevel          int
	Durability         int
	Slots              []int
	IsAmmunition       bool
	IsWearable         bool
	HasSkill           bool
	IsBoundToCharacter bool
	Requirements       []MoveRequirement
	QualifiedClasses   []int
}

// WearsSlot 对照原版 `ItemSlot.ItemSlots.Contains(slot)`。
func (d *MoveItemDef) WearsSlot(slot int) bool {
	if d == nil {
		return false
	}
	for _, s := range d.Slots {
		if s == slot {
			return true
		}
	}
	return false
}

// MoveItemEnv 是决策环境：物品定义查询 + 角色属性/职业（原版 Player 侧输入）。
type MoveItemEnv struct {
	// DefOf 返回物品定义投影（nil = 导出件里没有该物品）。
	DefOf func(*item.Item) *MoveItemDef
	// AttributeOf 返回 designation 的当前值（原版 player.Attributes[attr]）。
	AttributeOf func(designation string) float32
	// ClassNumber 是角色职业编号（原版 SelectedCharacter.CharacterClass）。
	ClassNumber int
}

func (e *MoveItemEnv) defOfSlot(inv *storage.Inventory, slot byte) *MoveItemDef {
	if inv == nil || e == nil || e.DefOf == nil {
		return nil
	}
	si := inv.GetItem(slot)
	if si == nil {
		return nil
	}
	return e.DefOf(si.It)
}

// IsStackable 对照原版 ItemExtensions.IsStackable：非可穿戴且基础耐久 > 1。
func IsStackable(def *MoveItemDef) bool {
	return def != nil && !def.IsWearable && def.Durability > 1
}

// SameItemAs 对照原版 ItemExtensions.IsSameItemAs：同定义且同等级。
func SameItemAs(a, b *item.Item) bool {
	return a != nil && b != nil && a.Group == b.Group && a.Number == b.Number && a.Level == b.Level
}

// CanCompletelyStackOn 对照原版 ItemExtensions.CanCompletelyStackOn（叠满后源件销毁）。
func CanCompletelyStackOn(def *MoveItemDef, src, dst *item.Item) bool {
	return IsStackable(def) && SameItemAs(src, dst) &&
		int(src.Durability)+int(dst.Durability) <= def.Durability
}

// CanPartiallyStackOn 对照原版 ItemExtensions.CanPartiallyStackOn（目标未满即可部分叠加）。
func CanPartiallyStackOn(def *MoveItemDef, src, dst *item.Item) bool {
	return IsStackable(def) && SameItemAs(src, dst) && int(dst.Durability) < def.Durability
}

// CalculateDropLevel 对照原版 ItemExtensions.CalculateDropLevel：
// 远古 +30、卓越 +25（远古优先，二者互斥），再 +3×物品等级。
func CalculateDropLevel(def *MoveItemDef, it *item.Item, itemLevel int) int {
	if def == nil || it == nil {
		return 0
	}
	dl := def.DropLevel
	if it.AncientDiscriminator > 0 {
		dl += 30
	} else if it.ExcellentBits != 0 {
		dl += 25
	}
	return dl + 3*itemLevel
}

// ResolveRequirement 对照原版 ItemExtensions.GetRequirement：把需求条目解析为
// （要比对的属性, 需要达到的值）。非映射属性（如 "Level"）原样返回 MinimumValue。
func ResolveRequirement(def *MoveItemDef, it *item.Item, r MoveRequirement) (string, int) {
	total, mapped := requirementAttributeMapping[r.Attribute]
	if !mapped || def == nil || it == nil {
		return r.Attribute, r.Value
	}
	if !def.IsWearable {
		return total, r.Value
	}
	multiplier := 3
	if total == desTotalEnergy {
		multiplier = 4
		if def.HasSkill && def.Group == SwordStaffBookGroup {
			// 召唤师书走另一条公式（原版 CalculateBookEnergyRequirement，dropLevel 取 itemLevel=0）。
			dl := CalculateDropLevel(def, it, 0)
			return total, ((r.Value * (dl + int(it.Level)) * 3) / 100) + 20
		}
	}
	value := 0
	if r.Value != 0 {
		value = (multiplier*CalculateDropLevel(def, it, int(it.Level))*r.Value)/100 + 20
	}
	if value > 0 && total == desTotalStrength {
		value += it.OptionLevel * 4 // 普通选项每级 +4 力量需求
	}
	return total, value
}

// RequirementsSatisfied 对照原版 PlayerItemExtensions.CompliesRequirements：
// 逐条需求比对当前属性值，并要求角色职业在物品的合格职业集合内。
func RequirementsSatisfied(def *MoveItemDef, it *item.Item, env *MoveItemEnv) bool {
	if def == nil || env == nil {
		return false
	}
	for _, r := range def.Requirements {
		attr, need := ResolveRequirement(def, it, r)
		var cur float32
		if env.AttributeOf != nil {
			cur = env.AttributeOf(attr)
		}
		if cur < float32(need) {
			return false
		}
	}
	for _, c := range def.QualifiedClasses {
		if c == env.ClassNumber {
			return true
		}
	}
	return false
}

// ConflictsWithEquippedHands 对照原版 ItemExtensions.ConflictsWithEquippedHands：
// 右手有非弹药武器时左手放不下 2 宽件；左手有 2 宽件时右手放不下单手武器/盾。
func ConflictsWithEquippedHands(def *MoveItemDef, inv *storage.Inventory, toSlot byte, env *MoveItemEnv) bool {
	if def == nil || len(def.Slots) == 0 || env == nil {
		return false
	}
	rightDef := env.defOfSlot(inv, RightHandSlot)
	leftDef := env.defOfSlot(inv, LeftHandSlot)
	oneHandedOrShield := (def.WearsSlot(RightHandSlot) && def.WearsSlot(LeftHandSlot)) ||
		def.Group == ShieldItemGroup
	rightBlocks := rightDef != nil && !rightDef.IsAmmunition
	leftWide := leftDef != nil && leftDef.Width >= 2
	return (int(toSlot) == LeftHandSlot && def.Width >= 2 && rightBlocks) ||
		(int(toSlot) == RightHandSlot && oneHandedOrShield && leftWide)
}

// DecideMove 对照原版 MoveItemAction.CanMoveAsync + ItemFitsAtNewLocation。
// 只做判定，不改容器状态；MoveNone 表示应回 ItemMoveFailed。
func DecideMove(inv *storage.Inventory, fromSlot, toSlot byte, it *item.Item, def *MoveItemDef, env *MoveItemEnv) MoveKind {
	if inv == nil || it == nil || def == nil {
		return MoveNone
	}

	// 防御性守卫：源槽 == 目标槽。客户端不会发这种请求（拖动未改变落点时不发包），
	// 但原版对"网格区原地搬运"会走到 ItemFitsAtNewLocation 的堆叠分支——那里的
	// targetItem 就是源物品**本身**，CanCompletelyStackOn(self, self) 成立，
	// FullStackAsync 会把数量翻倍再从容器移除（数量复制的潜在漏洞）。
	// 这里直接把原地搬运判为不可行：客户端收回失败包后归位，语义等价且无副作用。
	// 装备区的原地搬运原版本来就返回 None（目标槽非空），与本守卫一致。
	if fromSlot == toSlot {
		return MoveNone
	}

	// 装到装备区：目标槽必须空、物品槽位集合必须含该槽、需求满足、不冲突双手。
	if inv.WearsSlot(toSlot) {
		if inv.GetItem(toSlot) != nil || len(def.Slots) == 0 {
			return MoveNone
		}
		if def.WearsSlot(int(toSlot)) && RequirementsSatisfied(def, it, env) {
			if ConflictsWithEquippedHands(def, inv, toSlot, env) {
				return MoveNone
			}
			return MoveNormal
		}
		return MoveNone
	}

	// 目标格有物：仅同容器内可堆叠（Go 侧背包为单一容器，等价于原版 insidePlayerInventory）。
	if target := inv.GetItem(toSlot); target != nil {
		if CanCompletelyStackOn(def, it, target.It) {
			return MoveCompleteStack
		}
		if CanPartiallyStackOn(def, it, target.It) {
			return MovePartiallyStack
		}
		return MoveNone
	}

	// 目标格空：矩形占位检查，忽略源件自身（原版 `i == fromSlot && sameStorage → continue`）。
	if !inv.Grid().FitsAt(toSlot, def.Width, def.Height, inv.Grid().GetItem(fromSlot)) {
		return MoveNone
	}
	return MoveNormal
}

// ApplyNormalMove 对照原版 MoveNormalAsync：先移除再放入；放入失败回滚原槽，
// 原槽也放不回时退化为任意空位。返回 false 表示搬运失败（容器已尽力回滚）。
func ApplyNormalMove(inv *storage.Inventory, fromSlot, toSlot byte, si *storage.SlottedItem) bool {
	if inv == nil || si == nil {
		return false
	}
	if !inv.Remove(si) {
		return false
	}
	if inv.AddToSlot(toSlot, si) {
		return true
	}
	if inv.AddToSlot(fromSlot, si) {
		return false
	}
	inv.AddToFree(si)
	return false
}

// ApplyFullStack 对照原版 FullStackAsync：目标耐久吃下源件，源件从容器移除。
func ApplyFullStack(inv *storage.Inventory, si, target *storage.SlottedItem) {
	if inv == nil || si == nil || target == nil {
		return
	}
	target.It.Durability += si.It.Durability
	inv.Remove(si)
}

// ApplyPartialStack 对照原版 PartiallyStackAsync：按目标剩余容量搬运，源件留下余量。
// def 为两者共同的定义（SameItemAs 保证同定义），用其 Durability 作为容量上限。
func ApplyPartialStack(inv *storage.Inventory, si, target *storage.SlottedItem, def *MoveItemDef) {
	if inv == nil || si == nil || target == nil || def == nil {
		return
	}
	partial := def.Durability - int(target.It.Durability)
	if rest := int(si.It.Durability); rest < partial {
		partial = rest
	}
	if partial <= 0 {
		return
	}
	target.It.Durability += byte(partial)
	si.It.Durability -= byte(partial)
}
