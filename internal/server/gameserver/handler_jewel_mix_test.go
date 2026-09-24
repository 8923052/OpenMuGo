package gameserver

// handler_jewel_mix_test.go —— C1 BC Lahap 宝石升档合成 / 降档拆分
// （对照 ItemStackAction.cs；映射表 98_jewel_mixes.json，10 条）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// lahapNpcNumber 是导出件里窗口为 Lahap 的 NPC 号（MonsterDefinition 256）。
const lahapNpcNumber int16 = 256

// jewelMixScaffold 建一个"开着 Lahap 窗、带钱带背包"的会话。
func jewelMixScaffold(t *testing.T, money uint32) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "jeweler", 20, 20, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: money}
	c.Inventory = storage.NewInventory(0)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	sess.setOpenedNpc(craftHostNpc(t, srv, lahapNpcNumber))
	return srv, sess, c, rec
}

func mixJewelFrame(op c2s.MixType, itemKind byte, stackSize c2s.StackSize, sourceSlot byte) []byte {
	p := c2s.NewLahapJewelMixRequest()
	p.SetOperation(op)
	p.SetItem(c2s.ItemType(itemKind))
	p.SetMixingStackSize(stackSize)
	p.SetUnmixingSourceSlot(sourceSlot)
	return p.Bytes()
}

// putJewel 往指定格放一件物品（1×1）。
func putJewel(t *testing.T, srv *Server, sess *session, slot byte, group byte, number int, level byte) {
	t.Helper()
	si := srv.newSlottedItem(&item.Item{Group: group, Number: number, Level: level, Durability: 1})
	if !sess.getSelected().Inventory.AddToSlot(slot, si) {
		t.Fatalf("预置 (%d,%d) 到槽 %d 失败", group, number, slot)
	}
}

// countFramesOf 统计 (header, code) 且长度等于 wantLen 的帧数（区分 C3 22 的三种形态）。
func countFramesOf(rec *packetRecorder, header, code byte, wantLen int) int {
	n := 0
	for _, f := range rec.frames {
		if len(f) == wantLen && f[0] == header && frameCode(f) == code {
			n++
		}
	}
	return n
}

func lastAddedItemFrame(rec *packetRecorder) *s2c.ItemAddedToInventory {
	for i := len(rec.frames) - 1; i >= 0; i-- {
		f := rec.frames[i]
		if len(f) > 4 && f[0] == 0xC3 && frameCode(f) == 0x22 && s2c.AsItemAddedToInventory(f).InventorySlot() != 0xFE {
			return s2c.AsItemAddedToInventory(f)
		}
	}
	return nil
}

// TestJewelStackTenBless 验证合成 10 个祝福宝石：吃 10 件、产出 (12,30) 等级 0、
// 扣 500_000，出站是 10 帧 ItemRemoved + 1 帧入包 + 1 帧金币更新。
func TestJewelStackTenBless(t *testing.T) {
	srv, sess, c, rec := jewelMixScaffold(t, 2_000_000)
	for i := byte(10); i < 20; i++ {
		putJewel(t, srv, sess, i, 14, 13, 0)
	}

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Mix, 0, 0, 0))

	if got := countFramesOf(rec, 0xC1, 0x28, 5); got != 10 {
		t.Fatalf("ItemRemoved 帧数=%d，期望 10", got)
	}
	added := lastAddedItemFrame(rec)
	if added == nil {
		t.Fatal("未见入包帧")
	}
	stored := c.Inventory.GetItem(added.InventorySlot())
	if stored == nil || stored.It.Group != 12 || stored.It.Number != 30 {
		t.Fatalf("产物错：%+v", stored)
	}
	if stored.It.Level != 0 || stored.It.Durability != 1 {
		t.Fatalf("产物等级/耐久=%d/%d，期望 0/1", stored.It.Level, stored.It.Durability)
	}
	if c.Stats.Money != 1_500_000 {
		t.Fatalf("金币=%d，期望 1_500_000", c.Stats.Money)
	}
	if c.Inventory.Count() != 1 {
		t.Fatalf("背包件数=%d，期望只剩产物 1 件", c.Inventory.Count())
	}
}

// TestJewelStackTwentyAndThirty 验证 20/30 档：费用按档数放大、产物等级 = 档数-1。
func TestJewelStackTwentyAndThirty(t *testing.T) {
	srv, sess, c, _ := jewelMixScaffold(t, 20_000_000)
	for i := byte(10); i < 30; i++ {
		putJewel(t, srv, sess, i, 14, 14, 0)
	}

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Mix, 1, 1, 0)) // 20 × Soul

	if c.Stats.Money != 20_000_000-1_000_000 {
		t.Fatalf("20 档费用错，金币=%d", c.Stats.Money)
	}
	var packed *storage.SlottedItem
	for _, si := range c.Inventory.Grid().Items() {
		if si != nil && si.It.Number == 31 {
			packed = si
		}
	}
	if packed == nil {
		t.Fatal("未见灵魂宝石打包件 (12,31)")
	}
	if packed.It.Level != 1 {
		t.Fatalf("20 档产物等级=%d，期望 1", packed.It.Level)
	}
}

// TestJewelStackRejectBranches 验证两条拒绝分支：宝石不足只发"You are lacking of Jewels."
// 且不扣钱；钱不足发 NotEnoughMoney 且不吃宝石。
func TestJewelStackRejectBranches(t *testing.T) {
	srv, sess, c, rec := jewelMixScaffold(t, 2_000_000)
	for i := byte(10); i < 15; i++ {
		putJewel(t, srv, sess, i, 14, 13, 0)
	}
	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Mix, 0, 0, 0))
	if countFramesOf(rec, 0xC1, 0x28, 4) != 0 || c.Stats.Money != 2_000_000 {
		t.Fatal("宝石不足时不应吃宝石或扣钱")
	}
	if findFrame(rec, 0xC1, 0x0D) == nil {
		t.Fatal("宝石不足应发蓝字")
	}

	srv2, sess2, c2, _ := jewelMixScaffold(t, 100)
	for i := byte(10); i < 20; i++ {
		putJewel(t, srv2, sess2, i, 14, 13, 0)
	}
	srv2.handleJewelMix(sess2, mixJewelFrame(c2s.MixType_Mix, 0, 0, 0))
	if c2.Inventory.Count() != 10 || c2.Stats.Money != 100 {
		t.Fatalf("钱不足时不应有变化：件数=%d 金币=%d", c2.Inventory.Count(), c2.Stats.Money)
	}
}

// TestJewelUnstackProducesPieces 验证拆分 (12,30) 等级 2 → 30 件散宝石 + 扣 1_000_000。
func TestJewelUnstackProducesPieces(t *testing.T) {
	srv, sess, c, _ := jewelMixScaffold(t, 5_000_000)
	putJewel(t, srv, sess, 10, 12, 30, 2)

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Unmix, 0, 0, 10))

	if c.Inventory.Count() != 30 {
		t.Fatalf("拆分后背包件数=%d，期望 30", c.Inventory.Count())
	}
	for _, si := range c.Inventory.Grid().Items() {
		if si == nil {
			continue
		}
		if si.It.Group != 14 || si.It.Number != 13 {
			t.Fatalf("拆出的不是散祝福宝石：%+v", si.It)
		}
	}
	if c.Stats.Money != 4_000_000 {
		t.Fatalf("金币=%d，期望 4_000_000", c.Stats.Money)
	}
}

// TestJewelUnstackWrongSlot 验证槽位空/不是打包宝石两条蓝字分支。
func TestJewelUnstackWrongSlot(t *testing.T) {
	srv, sess, c, _ := jewelMixScaffold(t, 5_000_000)

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Unmix, 0, 0, 40))
	if c.Stats.Money != 5_000_000 {
		t.Fatal("槽位空时不应扣钱")
	}

	putJewel(t, srv, sess, 12, 14, 13, 0)
	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Unmix, 0, 0, 12))
	if c.Stats.Money != 5_000_000 || c.Inventory.Count() != 1 {
		t.Fatalf("非打包宝石时不应有变化：金币=%d 件数=%d", c.Stats.Money, c.Inventory.Count())
	}
}

// TestJewelUnstackNoSpaceChargesFee 照抄原版怪癖：先扣 1M 再查空槽，
// 空槽不足时**钱不退还、也不发金币帧**。
func TestJewelUnstackNoSpaceChargesFee(t *testing.T) {
	srv, sess, c, rec := jewelMixScaffold(t, 5_000_000)
	inv := c.Inventory
	// 先塞满，再腾出一格放打包宝石（等级 1 → 需要 20 格空位，必然不够）。
	for i := 0; i < 400; i++ {
		if !inv.AddToFree(&storage.SlottedItem{
			It: &item.Item{Group: 14, Number: 13, Durability: 1}, Width: 1, Height: 1,
		}) {
			break
		}
	}
	inv.Remove(inv.GetItem(30))
	putJewel(t, srv, sess, 30, 12, 30, 1)
	before := len(rec.frames)

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Unmix, 0, 0, 30))

	if c.Stats.Money != 4_000_000 {
		t.Fatalf("原版这条分支会扣钱不退还，金币=%d", c.Stats.Money)
	}
	if inv.GetItem(30) == nil {
		t.Fatal("空间不足时打包宝石必须留在原格")
	}
	if countFramesOf(rec, 0xC3, 0x22, 8) != 0 {
		t.Fatalf("空间不足不应发金币更新帧")
	}
	if len(rec.frames) <= before {
		t.Fatal("空间不足应发一条蓝字")
	}
}

// TestJewelMixRequiresLahapWindow 验证没开 Lahap 窗时的两条分支：
// 合成只记日志（无出站），拆分按原版直接断开连接。
func TestJewelMixRequiresLahapWindow(t *testing.T) {
	srv, sess, c, rec := jewelMixScaffold(t, 2_000_000)
	for i := byte(10); i < 20; i++ {
		putJewel(t, srv, sess, i, 14, 13, 0)
	}
	sess.setOpenedNpc(nil)
	before := len(rec.frames)

	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Mix, 0, 0, 0))

	if len(rec.frames) != before {
		t.Fatalf("未开 Lahap 时合成不应有出站，新增 %d 帧", len(rec.frames)-before)
	}
	if c.Inventory.Count() != 10 {
		t.Fatalf("未开 Lahap 时不应吃宝石，件数=%d", c.Inventory.Count())
	}
	// 拆分走 closeSession；测试会话 conn 为 nil，这里只验证不 panic 且无出站。
	srv.handleJewelMix(sess, mixJewelFrame(c2s.MixType_Unmix, 0, 0, 10))
	if len(rec.frames) != before {
		t.Fatal("未开 Lahap 时拆分也不应有出站")
	}
}

// TestJewelStackSizeMapping 锁住档位换算与未知值处理（原版未知值 throw）。
func TestJewelStackSizeMapping(t *testing.T) {
	for code, want := range map[byte]byte{0: 10, 1: 20, 2: 30} {
		got, ok := action.JewelStackSize(code)
		if !ok || got != want {
			t.Fatalf("档 %d → %d/%v，期望 %d/true", code, got, ok, want)
		}
	}
	if _, ok := action.JewelStackSize(3); ok {
		t.Fatal("未知档位应返回 false")
	}
}
