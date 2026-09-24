package gameserver

// handler_player_shops_scope_test.go —— C2 3F 00 PlayerShops（doc/17 S-3 批次 3）。
// 不发这条，客户端走近也看不见别人开的摊位：原版有两个出口 —— 开店瞬间广播给
// 周围（PlayerShopOpenedPlugIn）与新进入视野时补发清单（NewPlayersInScopePlugIn）。

import (
	"testing"

	"mugo/internal/gamelogic/world"

	s2c "mugo/internal/proto/s2c"
)

// findShopListFrame 取最后一条 C2 3F 00。
func findShopListFrame(rec *packetRecorder) *s2c.PlayerShops {
	f := findFrameSub(rec, 0xC2, 0x3F, 0x00)
	if f == nil {
		return nil
	}
	return s2c.AsPlayerShops(f)
}

// TestShopOpenAnnouncesToEveryoneInView 验证开店时视野内每人（含店主自己）都收到
// 一条单店主的 3F 00；店主自己那条用哨兵 ID 0x200（视野包同一规则）。
func TestShopOpenAnnouncesToEveryoneInView(t *testing.T) {
	sp := newShopPair(t)

	sp.open("zen shop")

	b := findShopListFrame(sp.recB)
	if b == nil {
		t.Fatal("买家应收到 C2 3F 00 摊位清单")
	}
	if b.ShopCount() != 1 {
		t.Fatalf("条目数=%d，期望 1", b.ShopCount())
	}
	e := b.Shops(0)
	if e == nil {
		t.Fatal("Shops(0) 越界")
	}
	if e.PlayerId() != sp.sellerID {
		t.Fatalf("摊位 ID=%d，期望店主 %d", e.PlayerId(), sp.sellerID)
	}
	if got := e.StoreNameString(); got != "zen shop" {
		t.Fatalf("店名=%q，期望 \"zen shop\"", got)
	}

	own := findShopListFrame(sp.recS)
	if own == nil || own.Shops(0) == nil {
		t.Fatal("店主自己也应收到一条")
	}
	if id := own.Shops(0).PlayerId(); id != world.ConstantPlayerID {
		t.Fatalf("店主视角的自身摊位 ID=%d，期望哨兵 %d", id, world.ConstantPlayerID)
	}
}

// TestShopResentWhenOwnerReEntersScope 验证摊位在**重新进入视野**时补发：
// 店主先走出买家视野再开店（买家收不到），走回来时买家必须补到一条 3F 00。
func TestShopResentWhenOwnerReEntersScope(t *testing.T) {
	sp := newShopPair(t)
	srv, seller := sp.srv, sp.seller

	srv.handleWalk(seller, walkFrame(20, 20, 3, 10))
	srv.handleWalk(seller, walkFrame(30, 20, 3, 10))
	if wp := seller.getWorldPlayer(); wp.X != 40 {
		t.Fatalf("前置：店主应已走到 x=40，实际 x=%d", wp.X)
	}
	sp.open("late shop")
	if findShopListFrame(sp.recB) != nil {
		t.Fatal("店主在买家视野外开店时，买家不应收到摊位包")
	}

	srv.handleWalk(seller, walkFrame(40, 20, 7, 10))
	srv.handleWalk(seller, walkFrame(30, 20, 7, 10))

	b := findShopListFrame(sp.recB)
	if b == nil {
		t.Fatal("店主回到视野内时买家应补收到 3F 00")
	}
	if b.ShopCount() != 1 || b.Shops(0) == nil {
		t.Fatalf("补发的清单项数=%d", b.ShopCount())
	}
	if b.Shops(0).PlayerId() != sp.sellerID {
		t.Fatalf("补发摊位 ID=%d，期望 %d", b.Shops(0).PlayerId(), sp.sellerID)
	}
	if got := b.Shops(0).StoreNameString(); got != "late shop" {
		t.Fatalf("补发店名=%q", got)
	}
}

// TestShopsOfSkipsClosedShops 验证收集口径：只列"当前开着店"的玩家，
// 且不含查看者自己。
func TestShopsOfSkipsClosedShops(t *testing.T) {
	sp := newShopPair(t)
	srv := sp.srv
	wpS := sp.seller.getWorldPlayer()
	wpB := sp.buyer.getWorldPlayer()

	if got := srv.shopsOf(wpB.ID, []*world.Player{wpS}); len(got) != 0 {
		t.Fatalf("未开店时不应收集到摊位: %+v", got)
	}
	sp.open("closed test")
	sp.srv.closeStore(sp.seller)
	if got := srv.shopsOf(wpB.ID, []*world.Player{wpS}); len(got) != 0 {
		t.Fatalf("关店后不应收集到摊位: %+v", got)
	}
	sp.open("open again")
	got := srv.shopsOf(wpB.ID, []*world.Player{wpS, wpB})
	if len(got) != 1 || got[0].ObjectID != wpS.ID || got[0].StoreName != "open again" {
		t.Fatalf("收集结果=%+v，期望只含店主一条", got)
	}
}
