package storage

import (
	"testing"

	"mugo/internal/gamelogic/entity/item"
)

func mkItem(group, number, w, h byte) *SlottedItem {
	return &SlottedItem{It: &item.Item{Group: group, Number: int(number)}, Width: w, Height: h}
}

// TestEquipSlotsAreSingleCell 锁定装备区语义：前 12 槽单格——大件也只占 1 格。
func TestEquipSlotsAreSingleCell(t *testing.T) {
	inv := NewInventory(0)
	big := mkItem(7, 1, 3, 3) // 大件（如翅膀类）
	if !inv.AddToSlot(0, big) {
		t.Fatal("装备区应无条件接受（单格）")
	}
	if inv.Grid().GetItem(0) != big {
		t.Fatal("装备区未入位")
	}
	// 装备区同一槽不可重复。
	if inv.AddToSlot(0, mkItem(7, 2, 1, 1)) {
		t.Fatal("同槽不可重复")
	}
}

// TestGridFootprint 锁定网格占地：宽×高占格、越界拒绝、重叠拒绝。
func TestGridFootprint(t *testing.T) {
	inv := NewInventory(0)
	g := inv.Grid()

	big := mkItem(7, 0, 2, 2)
	// 网格首槽 = 12（装备区 12 + row0 col0）。
	if !inv.AddToSlot(12, big) {
		t.Fatal("2×2 应可放入网格左上")
	}
	// 覆盖区 (12..13, 20..21) → 槽 12,13,20,21 全被占。
	for _, slot := range []byte{12, 13, 20, 21} {
		if inv.AddToSlot(slot, mkItem(14, 0, 1, 1)) {
			t.Fatalf("槽 %d 与 2×2 重叠，应拒绝", slot)
		}
	}
	// 槽 19（row0 col7）旁落一件 1×1：不重叠应成功。
	if !inv.AddToSlot(19, mkItem(14, 0, 1, 1)) {
		t.Fatal("1×1 应可放入相邻槽")
	}
	// 越界：col=7 放 2 宽 → 出界拒绝。
	if inv.AddToSlot(19, mkItem(14, 1, 2, 1)) {
		t.Fatal("出界应拒绝")
	}
	_ = g
}

// TestFindFreeSkipsOccupied 锁定 FindFreeSlot 扫描顺序：跳过被占格，
// 物品落在第一个放得下的矩形位置。
func TestFindFreeSkipsOccupied(t *testing.T) {
	inv := NewInventory(0)
	g := inv.Grid()
	// 占住网格 (0,0) 和 (1,0)（槽 12、20）：1×1 两件。
	a := mkItem(14, 0, 1, 1)
	b := mkItem(14, 1, 1, 1)
	if !inv.AddToSlot(12, a) || !inv.AddToSlot(20, b) {
		t.Fatal("占位失败")
	}
	// 放一件 2×1：第一候选 (0,0)(0,1)——(0,0) 被占；(0,1)(0,2)——(0,1) 即槽 13/14 空？
	// 槽 13 = row1? 不——col1/row0 = 槽 13；(1,0) 是槽 20。故 (0,1)(0,2) 均空 → 2×1 落在 col1..2。
	c := mkItem(14, 2, 2, 1)
	if !inv.AddToFree(c) {
		t.Fatal("应找到空位")
	}
	want := byte(12 + 1) // row0 col1
	if c.Slot != want {
		t.Fatalf("2×1 应落在槽 %d（row0 col1），got %d", want, c.Slot)
	}
	_ = g
}

// TestRemoveClearsFootprint 移除后占位清空、位置可复用。
func TestRemoveClearsFootprint(t *testing.T) {
	inv := NewInventory(0)
	big := mkItem(7, 0, 2, 2)
	if !inv.AddToSlot(12, big) {
		t.Fatal("放入失败")
	}
	small := mkItem(14, 0, 1, 1)
	if inv.AddToSlot(20, small) {
		t.Fatal("重叠应失败")
	}
	if !inv.Remove(big) {
		t.Fatal("移除失败")
	}
	if !inv.AddToSlot(20, small) {
		t.Fatal("移除后应可复用")
	}
	if inv.Count() != 1 {
		t.Fatalf("Count=%d, want 1", inv.Count())
	}
}

// TestExtensionOverflow 锁定扩展页语义：扩展页是**同一片网格的更多行**，
// 不是独立容器（对照原版 InventoryConstants.GetInventorySize：
// 64 + 32×ext 槽、Rows = 8 + 4×ext，物品可跨主网格↔扩展页边界摆放）。
func TestExtensionOverflow(t *testing.T) {
	inv := NewInventory(1) // 1 页扩展 → 64+32 = 96 网格槽，Rows=12
	g := inv.Grid()
	if got := g.SlotCount(); got != EquippedSlotsCount+GridSlots+SlotsPerExtension {
		t.Fatalf("槽数=%d, want %d", got, EquippedSlotsCount+GridSlots+SlotsPerExtension)
	}
	if got := g.Rows(); got != InventoryRows+RowsOfOneExtension {
		t.Fatalf("行数=%d, want %d", got, InventoryRows+RowsOfOneExtension)
	}
	// 主网格 64 格填 64 个 1×1。
	for i := 0; i < GridSlots; i++ {
		it := mkItem(14, byte(i), 1, 1)
		it.Slot = byte(EquippedSlotsCount + i)
		if !g.AddToSlot(it.Slot, it) {
			t.Fatalf("填充主网格 %d 失败", i)
		}
	}
	extra := mkItem(14, 64, 1, 1)
	if !inv.AddToFree(extra) {
		t.Fatal("应溢出到扩展页")
	}
	if extra.Slot != FirstExtensionSlot {
		t.Fatalf("extra 应落在扩展页首槽 %d，got %d", FirstExtensionSlot, extra.Slot)
	}
	if _, slot, ok := inv.FindItem(extra); !ok || slot != FirstExtensionSlot {
		t.Fatalf("FindItem=(%d,%v), want (%d,true)", slot, ok, FirstExtensionSlot)
	}
	// 分段约束：单件物品不得跨"主网格↔扩展页"边界。
	// 对照原版：ItemFitsAtNewLocation 的统一矩阵会放过跨段候选，但真实 AddItemAsync
	// 落在各段自己的小网格上 → 跨段在 Add 时失败并回滚（净效果=拒绝）。
	inv2 := NewInventory(1)
	g2 := inv2.Grid()
	if g2.FitsAt(68, 1, 2, nil) {
		t.Fatal("跨主网格↔扩展页边界应被拒绝（分段约束）")
	}
	// 段内摆放正常：主网格末行末列放 1×1；扩展页首行放 1×1。
	if !g2.FitsAt(75, 1, 1, nil) {
		t.Fatal("主网格末行 1×1 应可放")
	}
	if !g2.FitsAt(76, 1, 1, nil) {
		t.Fatal("扩展页首行 1×1 应可放")
	}
	// 扩展页内跨行（row8→row9）可以。
	if !g2.FitsAt(76, 1, 2, nil) {
		t.Fatal("扩展页内跨行应可放")
	}
}

// TestTwoExtensionPageWindows 锁定两页扩展的槽窗与段边界：
// 主网格 12..75、页 0 76..107、页 1 108..139（对照 InventoryConstants.FirstExtensionItemSlotIndex
// 与 RowsOfOneExtension=4；原版每页是 slotOffset 递增 32 的子网格，物品不得跨页摆放）。
func TestTwoExtensionPageWindows(t *testing.T) {
	g := NewInventory(2).Grid()
	if got := g.SlotCount(); got != EquippedSlotsCount+GridSlots+2*SlotsPerExtension {
		t.Fatalf("槽数=%d, want %d", got, EquippedSlotsCount+GridSlots+2*SlotsPerExtension)
	}
	if got := g.Rows(); got != InventoryRows+2*RowsOfOneExtension {
		t.Fatalf("行数=%d, want %d", got, InventoryRows+2*RowsOfOneExtension)
	}
	// 两页的首末格都能放 1×1，页外的第一格不行。
	for _, slot := range []byte{76, 107, 108, 139} {
		if !g.FitsAt(slot, 1, 1, nil) {
			t.Fatalf("槽 %d 应可用", slot)
		}
	}
	if g.FitsAt(140, 1, 1, nil) {
		t.Fatal("槽 140 超出两页范围，应不可用")
	}
	// 跨页边界：106 在页 0 末行（row 11），2 行高会伸进页 1 → 拒绝；
	// 页内跨行（108→109 即 row 12→13）允许。
	if g.FitsAt(106, 1, 2, nil) {
		t.Fatal("跨页边界摆放应被拒绝")
	}
	if !g.FitsAt(108, 1, 2, nil) {
		t.Fatal("页 1 内跨行应可放")
	}
	// 横向也不得越列（页 0 末列 107 = row 11 col 7）。
	if g.FitsAt(107, 2, 1, nil) {
		t.Fatal("跨列越界应被拒绝")
	}
	// AddToFree 的落位顺序：主网格填满 → 页 0 → 页 1。
	inv := NewInventory(2)
	grid := inv.Grid()
	for i := 0; i < GridSlots; i++ {
		it := mkItem(14, byte(i), 1, 1)
		if !grid.AddToSlot(byte(EquippedSlotsCount+i), it) {
			t.Fatalf("填充主网格 %d 失败", i)
		}
	}
	first := mkItem(14, 200, 1, 1)
	if !inv.AddToFree(first) || first.Slot != FirstExtensionSlot {
		t.Fatalf("应溢出到页 0 首槽，got slot=%d", first.Slot)
	}
	if !grid.AddToFree(mkItem(14, 201, 1, 1)) { // 占掉页 0 余下第一格
		t.Fatal("页 0 第二格应可放")
	}
	// 清空页 0 其余格后，整页高（8×4）的大件正好占满页 0；再一件只能落页 1。
	for slot := byte(FirstExtensionSlot); slot < FirstExtensionSlot+SlotsPerExtension; slot++ {
		if si := grid.GetItem(slot); si != nil {
			grid.Remove(si)
		}
	}
	firstBig := mkItem(14, 202, 8, 4)
	if !inv.AddToFree(firstBig) {
		t.Fatal("8×4 大件应能落进页 0")
	}
	if firstBig.Slot != FirstExtensionSlot {
		t.Fatalf("8×4 应落页 0 首槽 %d，got %d", FirstExtensionSlot, firstBig.Slot)
	}
	secondBig := mkItem(14, 203, 8, 4)
	if !inv.AddToFree(secondBig) {
		t.Fatal("第二件 8×4 应能落页 1")
	}
	if secondBig.Slot != FirstExtensionSlot+SlotsPerExtension {
		t.Fatalf("8×4 应落页 1 首槽 %d，got %d", FirstExtensionSlot+SlotsPerExtension, secondBig.Slot)
	}
	// 两页用尽后不再有位置。
	if inv.AddToFree(mkItem(14, 204, 1, 1)) {
		t.Fatal("两页占满后应放不下")
	}
}

// TestFitsAtIgnoresSelf 锁定同容器搬运时"源物品不挡自己"（原版 ItemFitsAtNewLocation 的
// `i == fromSlot && sameStorage → continue`）。
func TestFitsAtIgnoresSelf(t *testing.T) {
	inv := NewInventory(0)
	g := inv.Grid()
	big := mkItem(7, 0, 2, 2)
	if !g.AddToSlot(12, big) {
		t.Fatal("放入失败")
	}
	// 原位置 → 自身占位被忽略，可以"原地放下"。
	if !g.FitsAt(12, 2, 2, big) {
		t.Fatal("同容器原地搬运应允许（忽略自身占位）")
	}
	// 与自身交叠的错位（槽 13 起放 2×2）也应允许（自身格被掩掉）。
	if !g.FitsAt(13, 2, 2, big) {
		t.Fatal("与自身交叠的搬运应允许")
	}
	// 但另放一件物品时同一区域应被占住。
	if g.FitsAt(12, 2, 2, nil) {
		t.Fatal("不忽略任何物品时应判定为被占")
	}
	// 跨容器传入 ignore 不应生效（同 Storage 校验）：other 的 12 槽另有物品，
	// 把 inv 的 big 当 ignore 传进来也不能把它"忽略掉"。
	other := NewInventory(0)
	blocker := mkItem(14, 9, 2, 2)
	if !other.Grid().AddToSlot(12, blocker) {
		t.Fatal("other 占位失败")
	}
	if other.Grid().FitsAt(12, 2, 2, big) {
		t.Fatal("ignore 不属于本容器时不得被忽略")
	}
}

// TestIsWearableSlot 锁定装备区判定（原版 IsWearingSlot）。
func TestIsWearableSlot(t *testing.T) {
	g := NewInventory(0).Grid()
	for slot := 0; slot < EquippedSlotsCount; slot++ {
		if !g.IsWearableSlot(byte(slot)) {
			t.Fatalf("槽 %d 应属装备区", slot)
		}
	}
	if g.IsWearableSlot(EquippedSlotsCount) || g.IsWearableSlot(200) {
		t.Fatal("网格区/越界不应属装备区")
	}
}
