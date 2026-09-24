// vault.go —— 仓库 Vault 的跨存储搬运判定/应用与金钱移动
// （对照原版 MoveItemAction 的 vault 子集 CanMoveAsync/ItemFitsAtNewLocation
// 与 PlayerMoneyExtensions.TryDeposit/TryTakeVaultMoney）。
//
// 忠实复刻的跨存储规则：
//   - 目标是背包装备区（Vault→Inventory 穿装备）：三道门（槽位/需求/双手冲突），
//     与背包内穿戴同一判定；
//   - IsBoundToCharacter 的物品不可跨存储（原版 CanMoveAsync 先于占位检查拒绝）；
//   - 目标格**有物即拒绝**：堆叠仅允许 Inventory 内部（原版 ItemFitsAtNewLocation
//     的 insidePlayerInventory 判定，跨容器不堆叠）；
//   - 目标格空：矩形占位检查（上锁时"仓库→背包"在 `handleItemMoveVault` 里先被拒，
//     不进到这里）。
package action

import (
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// 仓库金钱移动方向（原版 VaultMoneyMoveDirection）。
const (
	// VaultMoneyDeposit 背包 → 仓库。
	VaultMoneyDeposit = 0
	// VaultMoneyWithdraw 仓库 → 背包。
	VaultMoneyWithdraw = 1
)

// DecideVaultMove 判定背包↔仓库的物品搬运，只判定不改容器。
// toInventory=true 为 Vault→Inventory（可穿到装备区）；false 为 Inventory→Vault。
func DecideVaultMove(toInventory bool, inv *storage.Inventory, vault *storage.Storage,
	fromSlot, toSlot byte, it *item.Item, def *MoveItemDef, env *MoveItemEnv) MoveKind {
	if inv == nil || vault == nil || it == nil || def == nil {
		return MoveNone
	}

	if toInventory {
		grid := inv.Grid()
		// 目标是装备区：与背包内穿戴完全相同的三道门
		// （原版 CanMoveAsync 中装备分支直接 return，不走下面的绑定检查）。
		if inv.WearsSlot(toSlot) {
			if grid.GetItem(toSlot) != nil || len(def.Slots) == 0 {
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
		// 网格区：绑定物品不可跨存储；有物拒绝（不堆叠）；空位做占位检查。
		if def.IsBoundToCharacter {
			return MoveNone
		}
		if grid.GetItem(toSlot) != nil {
			return MoveNone
		}
		if grid.FitsAt(toSlot, def.Width, def.Height, vault.GetItem(fromSlot)) {
			return MoveNormal
		}
		return MoveNone
	}

	// Inventory → Vault：绑定拒绝；有物拒绝；空位占位检查。
	if def.IsBoundToCharacter {
		return MoveNone
	}
	if vault.GetItem(toSlot) != nil {
		return MoveNone
	}
	if vault.FitsAt(toSlot, def.Width, def.Height, inv.Grid().GetItem(fromSlot)) {
		return MoveNormal
	}
	return MoveNone
}

// DecideVaultMoveWithin 判定仓库**内部**搬运（Vault→Vault）：同存储不堆叠
// （对照原版 ItemFitsAtNewLocation：insidePlayerInventory 仅 Inventory↔Inventory 成立），
// 目标格有物即拒绝；空位做矩形检查并忽略源物品自身占位。
func DecideVaultMoveWithin(vault *storage.Storage, fromSlot, toSlot byte, it *item.Item, def *MoveItemDef) MoveKind {
	if vault == nil || it == nil || def == nil {
		return MoveNone
	}
	if vault.GetItem(toSlot) != nil {
		return MoveNone
	}
	if vault.FitsAt(toSlot, def.Width, def.Height, vault.GetItem(fromSlot)) {
		return MoveNormal
	}
	return MoveNone
}

// ApplyVaultMoveWithin 执行仓库内部搬运：先移除再入新槽，失败回滚原槽。
// 返回 false 表示搬运失败（容器已回滚）。
func ApplyVaultMoveWithin(vault *storage.Storage, fromSlot, toSlot byte, si *storage.SlottedItem) bool {
	if si == nil || !vault.Remove(si) {
		return false
	}
	if vault.AddToSlot(toSlot, si) {
		return true
	}
	_ = vault.AddToSlot(fromSlot, si) // 回滚原槽
	return false
}

// ApplyVaultMove 执行跨容器普通搬运：先从源移除，再放入目标；放入失败回滚源槽。
// 返回 false 表示搬运失败（容器已回滚）。
func ApplyVaultMove(toInventory bool, inv *storage.Inventory, vault *storage.Storage,
	fromSlot, toSlot byte, si *storage.SlottedItem) bool {
	src, dst := vault, inv.Grid()
	if !toInventory {
		src, dst = inv.Grid(), vault
	}
	if !src.Remove(si) {
		return false
	}
	if dst.AddToSlot(toSlot, si) {
		return true
	}
	_ = src.AddToSlot(fromSlot, si) // 回滚原槽
	return false
}

// MoveVaultMoney 对照 TryDepositVaultMoney / TryTakeVaultMoney：
// 成功返回新的背包与仓库金钱；失败原值返回。
// 原版仓库金钱上限 MaximumVaultMoney 默认 int.MaxValue，与背包金币上限同值。
func MoveVaultMoney(direction int, amount, invMoney, vaultMoney uint32) (ok bool, newInv, newVault uint32) {
	if amount == 0 {
		return false, invMoney, vaultMoney
	}
	switch direction {
	case VaultMoneyDeposit:
		if invMoney < amount {
			return false, invMoney, vaultMoney
		}
		if uint64(vaultMoney)+uint64(amount) > uint64(MaxInventoryMoney) {
			return false, invMoney, vaultMoney
		}
		return true, invMoney - amount, vaultMoney + amount
	case VaultMoneyWithdraw:
		if vaultMoney < amount {
			return false, invMoney, vaultMoney
		}
		if uint64(invMoney)+uint64(amount) > uint64(MaxInventoryMoney) {
			return false, invMoney, vaultMoney
		}
		return true, invMoney + amount, vaultMoney - amount
	default:
		return false, invMoney, vaultMoney
	}
}
