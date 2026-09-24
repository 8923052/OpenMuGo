package gameserver

// handler_repair_test.go —— S9 修理端到端：损坏装备 C1 34 → 耐久回满 + C1 2A + 金币扣除；
// 金币不足则不修理、不回包；已满耐久无操作。物品定义从导出配置里挑一件可修理的（有耐久/可穿戴/非宠物）。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/pricing"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

func repairFrame(slot byte) []byte {
	r := c2s.NewRepairItemRequest()
	r.SetItemSlot(slot)
	return r.Bytes()
}

// firstRepairable 返回配置里第一件可修理装备的定义（有耐久、可穿戴、非宠物、maxDur>1）。
func firstRepairable(t *testing.T, srv *Server) *config.Item {
	t.Helper()
	items := srv.deps.cfg.GameConfig.Items
	for i := range items {
		d := &items[i]
		if d.Durability > 1 && (d.Slot != nil || len(d.Slots) > 0) && !d.IsTrainablePet() {
			return d
		}
	}
	t.Fatal("配置中找不到可修理装备定义")
	return nil
}

func newRepairSess(t *testing.T, money uint32) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, _ := newScopedSession(7, "smith", 20, 20, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: money, Strength: 100, Agility: 100}
	c.Inventory = storage.NewInventory(0)
	return srv, sess, c, rec
}

func TestRepairRestoresDurability(t *testing.T) {
	srv, sess, c, rec := newRepairSess(t, 2_000_000_000)
	def := firstRepairable(t, srv)
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: 0, Durability: 0}
	if !c.Inventory.AddToSlot(12, srv.newSlottedItem(it)) {
		t.Fatal("预置物品失败")
	}
	wantMax := pricing.MaximumDurability(def, it)
	before := c.Stats.Money

	srv.handleRepair(sess, repairFrame(12))

	if it.Durability != wantMax {
		t.Fatalf("耐久应恢复到 %d, got %d", wantMax, it.Durability)
	}
	if c.Stats.Money >= before {
		t.Fatalf("修理应扣金币: before=%d after=%d", before, c.Stats.Money)
	}
	if countFrames(rec, 0xC1, 0x2A) != 1 {
		t.Fatalf("应下发 1 帧 C1 2A ItemDurabilityChanged, got %d", countFrames(rec, 0xC1, 0x2A))
	}
}

func TestRepairSkipsFullDurability(t *testing.T) {
	srv, sess, c, rec := newRepairSess(t, 2_000_000_000)
	def := firstRepairable(t, srv)
	full := pricing.MaximumDurability(def, &item.Item{Level: 0})
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: 0, Durability: full}
	_ = c.Inventory.AddToSlot(12, srv.newSlottedItem(it))

	srv.handleRepair(sess, repairFrame(12))

	if countFrames(rec, 0xC1, 0x2A) != 0 {
		t.Fatal("已满耐久不应回耐久变化帧")
	}
	if c.Stats.Money != 2_000_000_000 {
		t.Fatal("已满耐久不应扣钱")
	}
}

func TestRepairInsufficientMoney(t *testing.T) {
	srv, sess, c, rec := newRepairSess(t, 0)
	def := firstRepairable(t, srv)
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: 0, Durability: 0}
	_ = c.Inventory.AddToSlot(12, srv.newSlottedItem(it))

	srv.handleRepair(sess, repairFrame(12))

	if it.Durability != 0 {
		t.Fatalf("金币不足不得修复, got %d", it.Durability)
	}
	if countFrames(rec, 0xC1, 0x2A) != 0 {
		t.Fatal("金币不足不应回耐久变化帧")
	}
}
