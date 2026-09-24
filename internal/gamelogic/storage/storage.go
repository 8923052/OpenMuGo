// Package storage 是物品容器（doc/15 §10 T1-3，对应原版 GameLogic/Storage.cs +
// InventoryStorage.cs 的核心语义；原版 Equipment/Warehouse/Inventory 均为其子类）。
//
// 忠实复刻的规则：
//   - 槽位模型：slot < boxOffset 为"单格区"（装备栏，一格一件，无占地）；
//     slot >= boxOffset 为 8 列网格区（RowSize=8，物品按 Width×Height 占格）；
//   - FitsInside：物品矩形必须整体落在网格内且所有格未被占用（原版同名方法）；
//   - FindFreeSlot：按行/列顺序找第一个放得下的位置（原版扫描顺序）；
//   - Remove：清除占位格；slot = slotOffset + boxOffset + column + row*RowSize。
//
// 扩展背包已复刻：不是独立子容器，而是**同一片网格的更多行**（见 inventory.go 的
// 分段约束，与原版"统一矩阵预检 + 各段小网格落位"的净效果一致）。
package storage

import (
	"fmt"

	"mugo/internal/gamelogic/entity/item"
)

// RowSize 是容器网格的列数（原版 InventoryConstants.RowSize）。
const RowSize = 8

// SlottedItem 是放进容器的物品：领域物品 + 占地尺寸（来自配置 Width×Height）。
type SlottedItem struct {
	It     *item.Item
	Slot   byte // 容器内槽号（含 boxOffset/slotOffset 语义由 Storage 管理）
	Width  byte
	Height byte
}

// Storage 是一个物品容器。
type Storage struct {
	name       string
	itemArray  []*SlottedItem // 长度 = numberOfSlots，下标 = slot - slotOffset
	usedSlots  []bool         // rows × RowSize（仅网格区）
	rows       int
	boxOffset  int // < boxOffset 为单格区
	slotOffset int // 槽号整体偏移（仓库/扩展页用）
	// 分段网格：主段 firstSegmentRows 行，之后每 segmentRows 行一段；segmentRows=0 不分段。
	// 背包用它表达"主网格 8 行 + 每扩展页 4 行"——单件物品**不得跨段**（对照原版：
	// 预检用统一矩阵，而真实 AddItemAsync 落在各段自己的小网格上，跨段会在 Add 时失败）。
	firstSegmentRows int
	segmentRows      int
}

// New 构造容器：numberOfSlots 个槽，前 boxOffset 个为单格区，网格 (numberOfSlots-boxOffset)/8 行。
// slotOffset 为槽号整体偏移。
func New(name string, numberOfSlots, boxOffset, slotOffset int) *Storage {
	s := newStorage(name, numberOfSlots, boxOffset, slotOffset)
	s.firstSegmentRows, s.segmentRows = s.rows, 0
	return s
}

// NewSegmented 构造分段网格容器（背包：主网格 firstSegmentRows 行 + 每段 segmentRows 行）。
func NewSegmented(name string, numberOfSlots, boxOffset, slotOffset, firstSegmentRows, segmentRows int) *Storage {
	s := newStorage(name, numberOfSlots, boxOffset, slotOffset)
	s.firstSegmentRows, s.segmentRows = firstSegmentRows, segmentRows
	return s
}

func newStorage(name string, numberOfSlots, boxOffset, slotOffset int) *Storage {
	if (numberOfSlots-boxOffset)%RowSize != 0 {
		panic(fmt.Sprintf("storage %s: (numberOfSlots-boxOffset) 必须是 %d 的倍数", name, RowSize))
	}
	rows := (numberOfSlots - boxOffset) / RowSize
	return &Storage{
		name:       name,
		itemArray:  make([]*SlottedItem, numberOfSlots),
		usedSlots:  make([]bool, rows*RowSize),
		rows:       rows,
		boxOffset:  boxOffset,
		slotOffset: slotOffset,
	}
}

// rowLimit 返回从 row 起该行所属段的行上界（不分段时即总行数）。
func (s *Storage) rowLimit(row int) int {
	if s.segmentRows <= 0 || row < s.firstSegmentRows {
		return s.firstSegmentRows
	}
	seg := (row - s.firstSegmentRows) / s.segmentRows
	return s.firstSegmentRows + (seg+1)*s.segmentRows
}

// Name 返回容器名。
func (s *Storage) Name() string { return s.name }

// Count 返回容器内物品数。
func (s *Storage) Count() int {
	n := 0
	for _, it := range s.itemArray {
		if it != nil {
			n++
		}
	}
	return n
}

// Items 返回槽位数组的只读快照（下标 = 槽号 - slotOffset；T2-9 堆叠查找用）。
func (s *Storage) Items() []*SlottedItem { return s.itemArray }

// HasSpaceFor 判断 w×h 物品是否还能放进网格区（原版 CheckInvSpace 的纯检查；
// T2-9 买入前预检）。
func (s *Storage) HasSpaceFor(w, h int) bool {
	for row := 0; row < s.rows; row++ {
		for col := 0; col < RowSize; col++ {
			if s.fitsInside(row, col, w, h) {
				return true
			}
		}
	}
	return false
}

// GetItem 取指定槽的物品（空槽返回 nil）。
func (s *Storage) GetItem(slot byte) *SlottedItem {
	idx := int(slot) - s.slotOffset
	if idx < 0 || idx >= len(s.itemArray) {
		return nil
	}
	return s.itemArray[idx]
}

// AddToSlot 把物品放入指定槽（原版 AddItemInternal + AddItem）：
// 单格区要求空格；网格区要求 FitsInside。失败返回 false 且无副作用。
func (s *Storage) AddToSlot(slot byte, it *SlottedItem) bool {
	idx := int(slot) - s.slotOffset
	if idx < 0 || idx >= len(s.itemArray) || it == nil || s.itemArray[idx] != nil {
		return false
	}
	if idx < s.boxOffset {
		s.itemArray[idx] = it
		it.Slot = slot
		return true
	}
	gridIdx := idx - s.boxOffset // 网格内相对槽号：col = 相对槽号 % 8，row = 相对槽号 / 8
	col := gridIdx % RowSize
	row := gridIdx / RowSize
	if !s.fitsInside(row, col, int(it.Width), int(it.Height)) {
		return false
	}
	s.itemArray[idx] = it
	it.Slot = slot
	s.setUsed(it, col, row, true)
	return true
}

// AddToFree 把物品放入第一个放得下的槽（原版 FindFreeSlot + AddItem 扫描顺序）。
func (s *Storage) AddToFree(it *SlottedItem) bool {
	w, h := int(it.Width), int(it.Height)
	for row := 0; row < s.rows; row++ {
		for col := 0; col < RowSize; col++ {
			if !s.fitsInside(row, col, w, h) {
				continue
			}
			idx := s.boxOffset + row*RowSize + col
			if s.itemArray[idx] != nil {
				continue
			}
			slot := byte(s.slotOffset + idx)
			return s.AddToSlot(slot, it)
		}
	}
	return false
}

// Remove 移除物品并清除占位（原版 RemoveItemAsync）。
func (s *Storage) Remove(it *SlottedItem) bool {
	idx := int(it.Slot) - s.slotOffset
	if idx < 0 || idx >= len(s.itemArray) || s.itemArray[idx] != it {
		return false
	}
	if gridIdx := idx - s.boxOffset; gridIdx >= 0 {
		col := gridIdx % RowSize
		row := gridIdx / RowSize
		s.setUsed(it, col, row, false)
	}
	s.itemArray[idx] = nil
	return true
}

// SlotCount 返回容器槽数（不含 slotOffset）。
func (s *Storage) SlotCount() int { return len(s.itemArray) }

// SlotOffset 返回槽号整体偏移（原版 StorageInfo.StartIndex）。
func (s *Storage) SlotOffset() int { return s.slotOffset }

// BoxOffset 返回单格区槽数（装备区长度）。
func (s *Storage) BoxOffset() int { return s.boxOffset }

// Rows 返回网格行数。
func (s *Storage) Rows() int { return s.rows }

// IsWearableSlot 报告槽号是否落在单格装备区（原版 IsWearingSlot——
// 装上/卸下装备时据此判断"外观是否变化"）。
func (s *Storage) IsWearableSlot(slot byte) bool {
	idx := int(slot) - s.slotOffset
	return idx >= 0 && idx < s.boxOffset
}

// FitsAt 判断把 w×h 的物品放到 slot 是否可行，并**忽略 ignore 自身的占位**
// （对照原版 ItemFitsAtNewLocation 里的 `i == fromSlot && sameStorage → continue`：
// 同容器搬运时源物品不能挡住自己）。
//
// ignore 仅在确实属于本容器时才被忽略（同 Storage 校验），跨容器传入无效。
func (s *Storage) FitsAt(slot byte, w, h int, ignore *SlottedItem) bool {
	idx := int(slot) - s.slotOffset
	if idx < s.boxOffset || idx >= len(s.itemArray) {
		return false // 单格区不走网格判定；越界直接拒绝
	}
	gridIdx := idx - s.boxOffset
	row, col := gridIdx/RowSize, gridIdx%RowSize
	if row+h > s.rowLimit(row) || col+w > RowSize {
		return false
	}

	// ignore 的占地矩形（仅当它是本容器的条目）。
	ignCol, ignRow, ignW, ignH := -1, -1, 0, 0
	if ignore != nil {
		if iidx := int(ignore.Slot) - s.slotOffset; iidx >= s.boxOffset && iidx < len(s.itemArray) && s.itemArray[iidx] == ignore {
			ig := iidx - s.boxOffset
			ignRow, ignCol = ig/RowSize, ig%RowSize
			ignW, ignH = int(ignore.Width), int(ignore.Height)
		}
	}
	for r := row; r < row+h; r++ {
		for c := col; c < col+w; c++ {
			if !s.usedSlots[r*RowSize+c] {
				continue
			}
			if ignCol >= 0 && r >= ignRow && r < ignRow+ignH && c >= ignCol && c < ignCol+ignW {
				continue // 源物品自身的格
			}
			return false
		}
	}
	return true
}

// Clear 清空容器（原版 Clear）。
func (s *Storage) Clear() {
	for i := range s.itemArray {
		s.itemArray[i] = nil
	}
	for i := range s.usedSlots {
		s.usedSlots[i] = false
	}
}

// fitsInside 复刻原版 FitsInside：越界=false；逐格查占用。
func (s *Storage) fitsInside(row, col, w, h int) bool {
	if row+h > s.rowLimit(row) || col+w > RowSize {
		return false
	}
	for r := row; r < row+h; r++ {
		for c := col; c < col+w; c++ {
			if s.usedSlots[r*RowSize+c] {
				return false
			}
		}
	}
	return true
}

// setUsed 设置/清除物品占地（原版 SetItemUsedSlots 的钳制：出界格跳过）。
func (s *Storage) setUsed(it *SlottedItem, col, row int, used bool) {
	for r := row; r < row+int(it.Height); r++ {
		for c := col; c < col+int(it.Width); c++ {
			if r < s.rows && c < RowSize {
				s.usedSlots[r*RowSize+c] = used
			}
		}
	}
}
