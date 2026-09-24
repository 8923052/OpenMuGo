// inventory.go —— 玩家背包容器（对应原版 InventoryStorage）。
//
// **关键：整块是"一片连续格区"**（对照原版 InventoryStorage 构造 + InventoryConstants）：
//
//	slot 0..11   装备区（单格，一格一件，无占地）
//	slot 12..75  主网格 8 行 × 8 列
//	slot 76..    扩展页，每页 4 行 × 8 列 = 32 槽，最多 4 页
//
// `GetInventorySize(ext) = 64 + 32*ext`、`Rows = 8 + 4*ext`——原版把它当**同一个网格**
// 排行（StartIndex=12，EndIndex=12+InventorySize），所以物品可以跨"主网格↔扩展页"
// 边界摆放。早期实现把扩展页拆成独立 Storage（每页 64 槽），凭空造出一条不可跨越的边界。
package storage

// Inventory 的槽位常量（原版 InventoryConstants）。
const (
	// EquippedSlotsCount 是装备区槽数。
	EquippedSlotsCount = 12
	// InventoryRows 是主网格行数。
	InventoryRows = 8
	// GridSlots 是主网格槽数（8 行 × 8 列）。
	GridSlots = InventoryRows * RowSize
	// RowsOfOneExtension 是每个扩展页的行数。
	RowsOfOneExtension = 4
	// MaxInventoryExtensions 是扩展页数上限。
	MaxInventoryExtensions = 4
	// SlotsPerExtension 是每个扩展页的槽数（4 行 × 8 列 = 32）。
	SlotsPerExtension = RowsOfOneExtension * RowSize
	// FirstExtensionSlot 是扩展页首槽（12 + 64 = 76）。
	FirstExtensionSlot = EquippedSlotsCount + GridSlots
)

// GetInventorySize 对照原版 InventoryConstants.GetInventorySize：64 + 32 × clamp(ext, 0, 4)。
func GetInventorySize(extensions int) int {
	if extensions < 0 {
		extensions = 0
	}
	if extensions > MaxInventoryExtensions {
		extensions = MaxInventoryExtensions
	}
	return GridSlots + SlotsPerExtension*extensions
}

// Inventory 是玩家背包：装备区 + 主网格 + 扩展页，共一片连续格区。
type Inventory struct {
	grid *Storage
}

// NewInventory 构造背包；extensions 为扩展页数（原版 Character.InventoryExtensions）。
func NewInventory(extensions int) *Inventory {
	slots := EquippedSlotsCount + GetInventorySize(extensions)
	return &Inventory{
		grid: NewSegmented("inventory", slots, EquippedSlotsCount, 0, InventoryRows, RowsOfOneExtension),
	}
}

// Grid 返回背包容器（装备区 + 连续网格区）。
func (i *Inventory) Grid() *Storage { return i.grid }

// Count 返回背包内物品总数。
func (i *Inventory) Count() int { return i.grid.Count() }

// AddToSlot 放入指定槽（原版 AddItemAsync(slot, item) 的容器部分）。
func (i *Inventory) AddToSlot(slot byte, it *SlottedItem) bool {
	return i.grid.AddToSlot(slot, it)
}

// AddToFree 依序找空位（原版 AddItemAsync(Item) → FindFreeSlot）。
func (i *Inventory) AddToFree(it *SlottedItem) bool { return i.grid.AddToFree(it) }

// GetItem 取指定槽的物品（空槽返回 nil）。
func (i *Inventory) GetItem(slot byte) *SlottedItem { return i.grid.GetItem(slot) }

// WearsSlot 报告槽号是否属于装备区（原版 InventoryStorage.IsWearingSlot）。
func (i *Inventory) WearsSlot(slot byte) bool { return i.grid.IsWearableSlot(slot) }

// FindItem 返回物品所在容器与槽（背包是单一容器，签名保留以兼容调用方）。
func (i *Inventory) FindItem(it *SlottedItem) (*Storage, byte, bool) {
	for s := 0; s < i.grid.SlotCount(); s++ {
		if i.grid.itemArray[s] == it {
			return i.grid, byte(i.grid.SlotOffset() + s), true
		}
	}
	return nil, 0, false
}

// Remove 从背包移除物品。
func (i *Inventory) Remove(it *SlottedItem) bool { return i.grid.Remove(it) }
