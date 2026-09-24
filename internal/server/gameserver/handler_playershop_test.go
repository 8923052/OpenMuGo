package gameserver

// handler_playershop_test.go —— S4 个人商店端到端：店主定价 C3 3F 01 → 开店 C1 3F 02 →
// 买家拉清单 C2 3F 05 → 买家购买 C1 3F 06：钱货两讫（买方背包 +1、卖方 −1，金额转移）。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

type shopPair struct {
	srv           *Server
	seller, buyer *session
	cs, cb        *entity.Character
	recS, recB    *packetRecorder
	sellerID      uint16
}

func newShopPair(t *testing.T) *shopPair {
	t.Helper()
	srv := newScopeTestSrv(t)
	recS, recB := &packetRecorder{}, &packetRecorder{}
	sessS, wpS := newScopedSession(7, "seller", 20, 20, recS)
	sessB, wpB := newScopedSession(8, "buyer", 21, 20, recB)
	cs, cb := sessS.getSelected(), sessB.getSelected()
	cs.Stats = &entity.CharStats{Money: 0, Strength: 100, Agility: 100}
	cb.Stats = &entity.CharStats{Money: 500, Strength: 100, Agility: 100}
	cs.Inventory = storage.NewInventory(0)
	cb.Inventory = storage.NewInventory(0)
	si := srv.newSlottedItem(&item.Item{Group: 255, Number: 255, Level: 0})
	if !cs.Inventory.AddToSlot(12, si) {
		t.Fatal("预置店主物品失败")
	}
	wpS.View, wpB.View = sessS.playerView, sessB.playerView
	srv.world.Map(0).Enter(wpS)
	srv.world.Map(0).Enter(wpB)
	sessS.setWorldPlayer(wpS)
	sessB.setWorldPlayer(wpB)
	srv.trackSession(sessS)
	srv.trackSession(sessB)
	t.Cleanup(func() { srv.untrackSession(sessS); srv.untrackSession(sessB) })
	return &shopPair{srv: srv, seller: sessS, buyer: sessB, cs: cs, cb: cb, recS: recS, recB: recB, sellerID: wpS.ID}
}

func (sp *shopPair) setPrice(slot byte, price uint32) {
	r := c2s.NewPlayerShopSetItemPrice()
	r.SetItemSlot(slot)
	r.SetPrice(price)
	sp.srv.handlePlayerShopSub(sp.seller, c2s.PlayerShopSetItemPriceSubCode, r.Bytes())
}

func (sp *shopPair) open(name string) {
	r := c2s.NewPlayerShopOpen()
	r.SetStoreName(name)
	sp.srv.handlePlayerShopSub(sp.seller, c2s.PlayerShopOpenSubCode, r.Bytes())
}

func (sp *shopPair) list() {
	r := c2s.NewPlayerShopItemListRequest()
	r.SetPlayerId(sp.sellerID)
	r.SetPlayerName("seller")
	sp.srv.handlePlayerShopSub(sp.buyer, c2s.PlayerShopItemListRequestSubCode, r.Bytes())
}

func (sp *shopPair) buy(slot byte) {
	r := c2s.NewPlayerShopItemBuyRequest()
	r.SetPlayerId(sp.sellerID)
	r.SetPlayerName("seller")
	r.SetItemSlot(slot)
	sp.srv.handlePlayerShopSub(sp.buyer, c2s.PlayerShopItemBuyRequestSubCode, r.Bytes())
}

func TestShopSetPriceAndBuyTransfersItemAndMoney(t *testing.T) {
	sp := newShopPair(t)
	sp.setPrice(12, 100)
	sp.open("my shop")
	sp.buy(12)

	if sp.cb.Stats.Money != 400 {
		t.Fatalf("买方金额=%d, want 400", sp.cb.Stats.Money)
	}
	if sp.cs.Stats.Money != 100 {
		t.Fatalf("卖方金额=%d, want 100", sp.cs.Stats.Money)
	}
	if sp.cs.Inventory.Count() != 0 {
		t.Fatalf("卖方背包应空, got %d", sp.cs.Inventory.Count())
	}
	if sp.cb.Inventory.Count() != 1 {
		t.Fatalf("买方背包应有 1 件, got %d", sp.cb.Inventory.Count())
	}
	if countFrames(sp.recB, 0xC1, 0x3F) == 0 {
		t.Fatal("买方应收到购买结果 C1 3F")
	}
	if sp.seller.store == nil || sp.seller.store.open {
		t.Fatal("售罄后卖方商店应已关闭")
	}
}

func TestShopBuyRejectsWhenNotOpen(t *testing.T) {
	sp := newShopPair(t)
	sp.setPrice(12, 100)
	// 未开店直接买。
	sp.buy(12)
	if sp.cb.Stats.Money != 500 || sp.cs.Inventory.Count() != 1 {
		t.Fatalf("未开店不得成交: 买方钱=%d 卖方背包=%d", sp.cb.Stats.Money, sp.cs.Inventory.Count())
	}
}

func TestShopListReturnsItemsToBuyer(t *testing.T) {
	sp := newShopPair(t)
	sp.setPrice(12, 100)
	sp.open("shop")
	sp.list()
	if countFrames(sp.recB, 0xC2, 0x3F) == 0 {
		t.Fatal("买方应收到商店清单 C2 3F")
	}
}

func TestShopSetPriceRejectsLevelTooLow(t *testing.T) {
	sp := newShopPair(t)
	sp.cs.Level = 3 // < 6
	sp.setPrice(12, 100)
	if _, ok := sp.seller.store.prices[12]; ok {
		t.Fatal("低等级不应成功定价")
	}
}
