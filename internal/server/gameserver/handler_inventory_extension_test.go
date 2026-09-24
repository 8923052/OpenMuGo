package gameserver

// handler_inventory_extension_test.go —— 扩展背包页（TRIM-05）的编排层闭环：
// 页内搬运、无页时的拒绝、拾取按角色页数建容器、清单带页内槽号。
// 段的几何（首末格、跨页拒绝、溢出顺序）在 gamelogic/storage 已钉，这里只管接线。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// 扩展页槽位（对照 InventoryConstants：主网格 12..75，页 i 起于 76+32i）。
const (
	page0FirstSlot = storage.FirstExtensionSlot          // 76
	page1FirstSlot = storage.FirstExtensionSlot + 32     // 108
	page1LastSlot  = storage.FirstExtensionSlot + 64 - 1 // 139
)

// extScaffold 造一个带 ext 页扩展背包的搬运脚手架。
func extScaffold(t *testing.T, ext byte) *moveScaffold {
	t.Helper()
	m := newMoveScaffold(t, 7, entity.CharStats{
		Money: 100_000, Strength: 300, Agility: 300, Vitality: 200, Energy: 100,
		InventoryExtensions: ext,
	})
	// 容器与上报页数同源（对照原版：都读 character.InventoryExtensions）。
	m.c.Inventory = storage.NewInventory(int(ext))
	return m
}

func jewel(slot byte) *item.Item {
	return &item.Item{Group: 14, Number: 13, Durability: 1} // Bless 宝石：1×1
}

func TestMoveItemIntoExtensionPage(t *testing.T) {
	m := extScaffold(t, 2)
	si := m.putItem(t, 20, jewel(20))
	m.move(storageKindInventory, 20, storageKindInventory, page1FirstSlot)

	if got := m.c.Inventory.GetItem(page1FirstSlot); got != si {
		t.Fatalf("物品应落在页 1 首槽 %d, got %+v", page1FirstSlot, got)
	}
	if got := m.c.Inventory.GetItem(20); got != nil {
		t.Fatalf("源槽应已腾空, got %+v", got)
	}
	f := findFrame(m.rec, 0xC3, 0x24)
	if f == nil {
		t.Fatal("应回 ItemMoved（C3 24）")
	}
	if got := s2c.AsItemMoved(f).TargetSlot(); got != page1FirstSlot {
		t.Fatalf("ItemMoved 的目标槽应是页内绝对槽号 %d, got %d", page1FirstSlot, got)
	}
}

// TestMoveIntoUnpurchasedPageRejected 未购买的页不在容器槽数内 → 目标不可达，
// 物品原地不动并回失败响应（原版由 ItemFitsAtNewLocation/AddItemAsync 的界外拒绝）。
func TestMoveIntoUnpurchasedPageRejected(t *testing.T) {
	m := extScaffold(t, 0)
	si := m.putItem(t, 20, jewel(20))
	m.move(storageKindInventory, 20, storageKindInventory, page0FirstSlot)

	if got := m.c.Inventory.GetItem(20); got != si {
		t.Fatal("无扩展页时源槽物品不应被移走")
	}
	if got := m.c.Inventory.GetItem(page0FirstSlot); got != nil {
		t.Fatal("页 0 首槽不应有物品")
	}
	if countFrames(m.rec, 0xC3, 0x24) == 0 {
		t.Fatal("应收到搬运响应帧（C3 24）")
	}
}

// TestPageBoundaryStillAppliesInsideInventory 页内首格可放、跨页边界的占地不可放：
// 与主网格↔页 0 的段约束一致（几何细节见 storage 层，这里验证搬运路径不绕过它）。
func TestPageBoundaryStillAppliesInsideInventory(t *testing.T) {
	m := extScaffold(t, 2)
	m.putItem(t, page0FirstSlot-1, jewel(0)) // 75：主网格末格（1×1 占位后页 0 首格仍可放）
	if !m.c.Inventory.Grid().FitsAt(page0FirstSlot, 1, 1, nil) {
		t.Fatal("页 0 首格应可用")
	}
	// 2×2 放在页 0 末列（107）会伸进页 1 → 不可放。
	if m.c.Inventory.Grid().FitsAt(page0FirstSlot+31, 2, 2, nil) {
		t.Fatal("跨页摆放应被拒绝")
	}
	if !m.c.Inventory.Grid().FitsAt(page1FirstSlot, 2, 2, nil) {
		t.Fatal("页 1 内 2×2 应可放")
	}
}

// TestPickupUsesCharacterExtensionPages 钉住拾取路径的建容器口径：
// 背包尚未创建时必须按角色扩展页数建（此前写死 NewInventory(0)，会静默丢掉已购页）。
func TestPickupUsesCharacterExtensionPages(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "picker", 100, 100, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: 500, InventoryExtensions: 2}
	c.Inventory = nil // 尚未创建（新登录/首次拾取）

	it := &item.Item{Group: 14, Number: 13, Durability: 1}
	idItem := srv.deps.cfg.Drops.AddItem(0, 101, 100, it, time.Now())
	req := c2s.NewPickupItemRequest()
	req.SetItemId(idItem)
	srv.handlePickupItem(sess, req.Bytes())

	wantSlots := storage.EquippedSlotsCount + storage.GetInventorySize(2)
	if c.Inventory == nil || c.Inventory.Grid().SlotCount() != wantSlots {
		t.Fatalf("拾取建的背包应含 2 页扩展（槽数 %d）, got %+v", wantSlots, c.Inventory)
	}
	if c.Inventory.Count() != 1 {
		t.Fatalf("拾取后应有 1 件物品, got %d", c.Inventory.Count())
	}
}

// TestMainGridFullPickupOverflowsIntoPage 主网格占满后，拾取应溢出到页 0。
func TestMainGridFullPickupOverflowsIntoPage(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "picker", 100, 100, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: 500, InventoryExtensions: 1}
	c.Inventory = storage.NewInventory(1)
	for i := 0; i < storage.GridSlots; i++ {
		if !c.Inventory.AddToSlot(byte(storage.EquippedSlotsCount+i), srv.newSlottedItem(jewel(0))) {
			t.Fatalf("预置主网格 %d 失败", i)
		}
	}

	idItem := srv.deps.cfg.Drops.AddItem(0, 101, 100, jewel(0), time.Now())
	req := c2s.NewPickupItemRequest()
	req.SetItemId(idItem)
	srv.handlePickupItem(sess, req.Bytes())

	if got := c.Inventory.GetItem(page0FirstSlot); got == nil {
		t.Fatalf("主网格已满，拾取应落页 0 首槽 %d", page0FirstSlot)
	}
	if countFrames(rec, 0xC3, 0x22) == 0 {
		t.Fatal("应收到入包通知（C3 22）")
	}
}

// TestInventoryListCarriesPageSlots 背包清单（C4 F3 10）的槽号是绝对号，
// 页内物品必须出现在清单里（否则真机页上是空的）。
func TestInventoryListCarriesPageSlots(t *testing.T) {
	m := extScaffold(t, 2)
	m.putItem(t, page1LastSlot, jewel(0))
	views := m.srv.inventoryViews(m.c)
	var found bool
	for _, v := range views {
		if v.Slot == page1LastSlot {
			found = true
		}
	}
	if !found {
		t.Fatalf("清单应含页 1 末槽 %d 的物品, got %+v", page1LastSlot, views)
	}
}
