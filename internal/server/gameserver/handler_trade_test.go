package gameserver

// handler_trade_test.go —— S3 交易端到端：请求 C1 36 → 应答 C1 37 → 0x24 把物品搬入
// Trade 临时容器（自己收 C3 24、对方收 C1 39）→ 双方确认 C1 3C → 原子交换：物品易主、
// 双方各收 C1 3D TradeFinished(Success)。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

type tradePair struct {
	srv          *Server
	sessA, sessB *session
	cA, cB       *entity.Character
	recA, recB   *packetRecorder
}

func newTradePair(t *testing.T) *tradePair {
	t.Helper()
	srv := newScopeTestSrv(t)
	recA, recB := &packetRecorder{}, &packetRecorder{}
	sessA, wpA := newScopedSession(7, "alpha", 20, 20, recA)
	sessB, wpB := newScopedSession(8, "beta", 21, 20, recB)
	cA, cB := sessA.getSelected(), sessB.getSelected()
	cA.Stats = &entity.CharStats{Money: 1000, Strength: 100, Agility: 100}
	cB.Stats = &entity.CharStats{Money: 500, Strength: 100, Agility: 100}
	cA.Inventory = storage.NewInventory(0)
	cB.Inventory = storage.NewInventory(0)
	wpA.View, wpB.View = sessA.playerView, sessB.playerView
	srv.world.Map(0).Enter(wpA)
	srv.world.Map(0).Enter(wpB)
	sessA.setWorldPlayer(wpA)
	sessB.setWorldPlayer(wpB)
	srv.trackSession(sessA)
	srv.trackSession(sessB)
	t.Cleanup(func() { srv.untrackSession(sessA); srv.untrackSession(sessB) })
	return &tradePair{srv: srv, sessA: sessA, sessB: sessB, cA: cA, cB: cB, recA: recA, recB: recB}
}

func moveFrame(from, fromSlot, to, toSlot byte) []byte {
	r := c2s.NewItemMoveRequestExtended()
	r.SetFromStorage(c2s.ItemStorageKind(from))
	r.SetFromSlot(fromSlot)
	r.SetToStorage(c2s.ItemStorageKind(to))
	r.SetToSlot(toSlot)
	return r.Bytes()
}

func buttonFrame(checked bool) []byte {
	r := c2s.NewTradeButtonStateChange()
	if checked {
		r.SetNewState(c2s.TradeButtonState_Checked)
	} else {
		r.SetNewState(c2s.TradeButtonState_Unchecked)
	}
	return r.Bytes()
}

func TestTradeOpenRejectsWhenTargetBusy(t *testing.T) {
	tp := newTradePair(t)
	req := c2s.NewTradeRequest()
	req.SetPlayerId(8)
	tp.srv.handleTradeRequest(tp.sessA, req.Bytes())
	if findFrame(tp.recB, 0xC3, 0x36) == nil {
		t.Fatal("beta 应收到交易请求 C3 36")
	}
	if tp.sessB.tradeRequestFrom != "alpha" {
		t.Fatalf("beta 待决邀请=%q, want alpha", tp.sessB.tradeRequestFrom)
	}
}

func TestTradeItemSwapCompletes(t *testing.T) {
	tp := newTradePair(t)
	// alpha 背包放一件物品到槽 12。
	si := tp.srv.newSlottedItem(&item.Item{Group: 255, Number: 255, Level: 0, Durability: 5})
	if !tp.cA.Inventory.AddToSlot(12, si) {
		t.Fatal("预置 alpha 物品失败")
	}

	// 请求 + 接受 → 双方收到 TradeRequestAnswer。
	req := c2s.NewTradeRequest()
	req.SetPlayerId(8)
	tp.srv.handleTradeRequest(tp.sessA, req.Bytes())
	resp := c2s.NewTradeRequestResponse()
	resp.SetTradeAccepted(true)
	tp.srv.handleTradeResponse(tp.sessB, resp.Bytes())
	if tp.sessA.trade == nil || tp.sessB.trade == nil {
		t.Fatal("双方应建立交易")
	}
	if countFrames(tp.recA, 0xC1, 0x37) != 1 || countFrames(tp.recB, 0xC1, 0x37) != 1 {
		t.Fatalf("双方应各收 1 帧 TradeRequestAnswer: A=%d B=%d", countFrames(tp.recA, 0xC1, 0x37), countFrames(tp.recB, 0xC1, 0x37))
	}

	// alpha 把物品搬入交易（0x24，to=Trade）。
	tp.srv.handleItemMove(tp.sessA, moveFrame(0, 12, 1, 0))
	if countFrames(tp.recA, 0xC3, 0x24) == 0 {
		t.Fatal("alpha 应收到自己的 ItemMoved(C3 24)")
	}
	if countFrames(tp.recB, 0xC1, 0x39) == 0 {
		t.Fatal("beta 应收到 TradeItemAdded(C1 39)")
	}
	if tp.cA.Inventory.GetItem(12) != nil {
		t.Fatal("物品应已离开 alpha 背包进入交易临时容器")
	}

	// 双方确认 → 原子交换。
	tp.srv.handleTradeButton(tp.sessA, buttonFrame(true))
	tp.srv.handleTradeButton(tp.sessB, buttonFrame(true))

	if tp.cA.Inventory != nil && tp.cA.Inventory.Count() != 0 {
		t.Fatalf("交换后 alpha 背包应空, got %d", tp.cA.Inventory.Count())
	}
	if tp.cB.Inventory.Count() != 1 {
		t.Fatalf("交换后 beta 背包应有 1 件, got %d", tp.cB.Inventory.Count())
	}
	if tp.sessA.trade != nil || tp.sessB.trade != nil {
		t.Fatal("交易完成后应清空")
	}
	if countFrames(tp.recA, 0xC1, 0x3D) != 1 || countFrames(tp.recB, 0xC1, 0x3D) != 1 {
		t.Fatalf("双方应各收 1 帧 TradeFinished: A=%d B=%d", countFrames(tp.recA, 0xC1, 0x3D), countFrames(tp.recB, 0xC1, 0x3D))
	}
}

func TestTradeCancelReturnsItems(t *testing.T) {
	tp := newTradePair(t)
	si := tp.srv.newSlottedItem(&item.Item{Group: 255, Number: 255, Level: 0})
	_ = tp.cA.Inventory.AddToSlot(12, si)

	req := c2s.NewTradeRequest()
	req.SetPlayerId(8)
	tp.srv.handleTradeRequest(tp.sessA, req.Bytes())
	resp := c2s.NewTradeRequestResponse()
	resp.SetTradeAccepted(true)
	tp.srv.handleTradeResponse(tp.sessB, resp.Bytes())

	tp.srv.handleItemMove(tp.sessA, moveFrame(0, 12, 1, 0))
	if tp.cA.Inventory.Count() != 0 {
		t.Fatal("入交易后背包应空")
	}
	// alpha 取消。
	tp.srv.handleTradeCancel(tp.sessA)
	if tp.cA.Inventory.Count() != 1 {
		t.Fatalf("取消后物品应退回 alpha 背包, got %d", tp.cA.Inventory.Count())
	}
	if tp.sessA.trade != nil || tp.sessB.trade != nil {
		t.Fatal("取消后交易应清空")
	}
}

func TestTradeMoneyUpdatesPartner(t *testing.T) {
	tp := newTradePair(t)
	req := c2s.NewTradeRequest()
	req.SetPlayerId(8)
	tp.srv.handleTradeRequest(tp.sessA, req.Bytes())
	resp := c2s.NewTradeRequestResponse()
	resp.SetTradeAccepted(true)
	tp.srv.handleTradeResponse(tp.sessB, resp.Bytes())

	set := c2s.NewSetTradeMoney()
	set.SetAmount(300)
	tp.srv.handleTradeMoney(tp.sessA, set.Bytes())

	if tp.cA.Stats.Money != 700 { // 1000 - 300
		t.Fatalf("alpha 背包应扣 300, got %d", tp.cA.Stats.Money)
	}
	if countFrames(tp.recB, 0xC1, 0x3B) == 0 {
		t.Fatal("beta 应收到 TradeMoneyUpdate(C1 3B)")
	}
}
