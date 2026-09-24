package action

// jewel_mix.go —— Lahap 的宝石升档合成与降档拆分（对照
// GameLogic/PlayerActions/Items/ItemStackAction.cs，由 C1 BC 触发）。
//
// 原版的三条口径照抄不修：
//   - 合成产物等级 = stackSize/10 - 1（10/20/30 个 → 等级 0/1/2），耐久恒 1；
//   - 费用是**固定值**：合成 500_000 × (stackSize/10)，拆分 1_000_000，与 NPC 折扣无关；
//   - 拆分要求空槽数 ≥ (等级+1)×10，不够则整笔拒绝（钱不扣）。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// 原版 ItemStackAction 的两个费用常量。
const (
	JewelCombineFeePerTen = 500_000
	JewelDismantleFee     = 1_000_000
)

// JewelMixFail 是拒绝原因（JewelMixNone = 可执行）。
type JewelMixFail byte

const (
	JewelMixNone JewelMixFail = iota
	JewelMixLackOfJewels
	JewelMixNotEnoughMoney
	JewelMixStackedNotFound
	JewelMixNotStackedJewel
	JewelMixNoInventorySpace
)

// JewelStackSize 把包里的 StackSize 枚举（0/1/2）换算成个数；未知值 ok=false
// （原版是 throw InvalidEnumArgumentException，本仓按"记日志不动作"处理）。
func JewelStackSize(code byte) (byte, bool) {
	switch code {
	case 0:
		return 10, true
	case 1:
		return 20, true
	case 2:
		return 30, true
	}
	return 0, false
}

// JewelStackPlan 是一次合成的判定结果。
type JewelStackPlan struct {
	Reject JewelMixFail
	Fee    uint32
	// Sources 是要被吃掉的散宝石槽位（原版 Take(stackSize) 取背包里的前 N 件）。
	Sources []byte
	// Result 是产出的打包宝石（等级与耐久已定好）。
	Result *item.Item
}

// PlanJewelStack 对照 StackItemsAsync 的判定部分：数够 stackSize 件 SingleJewel 才继续，
// 然后按 (stackSize/10)×500_000 查钱。
func PlanJewelStack(mix *config.JewelMix, stackSize byte, inventory *storage.Inventory, money uint32) JewelStackPlan {
	fee := uint32(stackSize/10) * JewelCombineFeePerTen
	var slots []byte
	for _, si := range inventory.Grid().Items() {
		if si == nil || si.It == nil {
			continue
		}
		if int(si.It.Group) != mix.Single.Group || int(si.It.Number) != mix.Single.Number {
			continue
		}
		slots = append(slots, si.Slot)
		if len(slots) == int(stackSize) {
			break
		}
	}
	if len(slots) < int(stackSize) {
		return JewelStackPlan{Reject: JewelMixLackOfJewels}
	}
	if money < fee {
		return JewelStackPlan{Reject: JewelMixNotEnoughMoney}
	}
	return JewelStackPlan{
		Fee:     fee,
		Sources: slots,
		Result: &item.Item{
			Group:      byte(mix.Mixed.Group),
			Number:     mix.Mixed.Number,
			Level:      stackSize/10 - 1,
			Durability: 1,
		},
	}
}

// JewelUnstackPlan 是一次拆分的判定结果。
type JewelUnstackPlan struct {
	Reject  JewelMixFail
	Fee     uint32
	Pieces  int
	Targets []byte
}

// PlanJewelUnstack 对照 UnstackItemsAsync：槽位有物且确为该 mix 的打包宝石 → 扣
// 1_000_000 → 需 (等级+1)×10 个空槽，不足整笔拒绝。
func PlanJewelUnstack(mix *config.JewelMix, stacked *storage.SlottedItem, inventory *storage.Inventory, money uint32) JewelUnstackPlan {
	if stacked == nil || stacked.It == nil {
		return JewelUnstackPlan{Reject: JewelMixStackedNotFound}
	}
	if int(stacked.It.Group) != mix.Mixed.Group || int(stacked.It.Number) != mix.Mixed.Number {
		return JewelUnstackPlan{Reject: JewelMixNotStackedJewel}
	}
	if money < JewelDismantleFee {
		return JewelUnstackPlan{Reject: JewelMixNotEnoughMoney}
	}
	pieces := (int(stacked.It.Level) + 1) * 10
	var free []byte
	for slot := 0; slot < inventory.Grid().SlotCount(); slot++ {
		if len(free) == pieces {
			break
		}
		if inventory.GetItem(byte(slot)) == nil {
			free = append(free, byte(slot))
		}
	}
	if len(free) < pieces {
		// 原版 TryRemoveMoney 发生在这一步之前，所以这条拒绝分支的费用**已经扣掉**；
		// Fee 一并带回，由调用方落地该口径。
		return JewelUnstackPlan{Reject: JewelMixNoInventorySpace, Fee: JewelDismantleFee}
	}
	return JewelUnstackPlan{Fee: JewelDismantleFee, Pieces: pieces, Targets: free}
}
