package gameserver

// handler_pickup_fail_test.go —— 拾取失败原因包 C3 22 ItemPickUpRequestFailed
// （doc/17 S-3 批次 2）。原版 PickupItemAction 的四条失败出口都发它（General=0xFF），
// 本仓此前把"背包满"回成丢弃响应 C1 23，属协议错包。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// pickupScaffold 造一个站在 (100,100) 的进图会话。
func pickupScaffold(t *testing.T, name string) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, name, 100, 100, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	c := &entity.Character{Name: name, Stats: &entity.CharStats{Money: 10}}
	sess.setSelected(c)
	return srv, sess, c, rec
}

// tryPickup 按地面物 ID 走一次 C3 22 拾取。
func tryPickup(srv *Server, sess *session, dropID uint16) {
	req := c2s.NewPickupItemRequest()
	req.SetItemId(dropID)
	srv.handlePickupItem(sess, req.Bytes())
}

// pickUpFailedFrames 收集所有"拾取失败"帧（C3 22 定长 4B，与入包帧区分）。
func pickUpFailedFrames(rec *packetRecorder) []s2c.ItemPickUpFailReason {
	var out []s2c.ItemPickUpFailReason
	for _, f := range rec.frames {
		if len(f) == 4 && f[0] == 0xC3 && f[2] == 0x22 {
			out = append(out, s2c.AsItemPickUpRequestFailed(f).FailReason())
		}
	}
	return out
}

// TestPickupUnknownDropSendsGeneral 验证原版 switch 的 default 分支：ID 不在地面
// （已被他人拿走/从未掉落）也要回 General，而不是静默。
func TestPickupUnknownDropSendsGeneral(t *testing.T) {
	srv, sess, _, rec := pickupScaffold(t, "ghost")

	tryPickup(srv, sess, 0x1AA)

	if got := pickUpFailedFrames(rec); len(got) != 1 || got[0] != s2c.ItemPickUpFailReason_General {
		t.Fatalf("应回一帧 General(255)，got %v", got)
	}
}

// TestPickupOutOfRangeSendsGeneral 验证距离 > 3 时拒绝并回 General，地面物保留。
func TestPickupOutOfRangeSendsGeneral(t *testing.T) {
	srv, sess, _, rec := pickupScaffold(t, "far")
	id := srv.deps.cfg.Drops.AddItem(0, 150, 150,
		&item.Item{Group: 0, Number: 7, Level: 0, Durability: 20}, time.Now())

	tryPickup(srv, sess, id)

	if got := pickUpFailedFrames(rec); len(got) != 1 || got[0] != s2c.ItemPickUpFailReason_General {
		t.Fatalf("超距拾取应回 General，got %v", got)
	}
	if _, ok := srv.deps.cfg.Drops.PeekItem(id); !ok {
		t.Fatal("超距拾取不应把地面物取走")
	}
}

// TestPickupWithFullInventorySendsGeneral 验证背包满时回 C3 22 General
// （而不是此前错用的丢弃响应 C1 23）。
func TestPickupWithFullInventorySendsGeneral(t *testing.T) {
	srv, sess, c, rec := pickupScaffold(t, "stuffed")
	inv := srv.ensureInventory(c)
	jewel := &item.Item{Group: 14, Number: 1, Level: 0, Durability: 1} // 1×1 宝石类
	for i := 0; i < 500; i++ {
		if !inv.AddToFree(&storage.SlottedItem{It: jewel, Width: 1, Height: 1}) {
			break
		}
		if i == 499 {
			t.Fatal("500 次填充后背包仍收得下 1×1 物品，无法构造满包")
		}
	}
	before := len(rec.frames)
	id := srv.deps.cfg.Drops.AddItem(0, 101, 100,
		&item.Item{Group: 14, Number: 2, Level: 0, Durability: 1}, time.Now())

	tryPickup(srv, sess, id)

	if len(rec.frames) <= before {
		t.Fatal("背包满时应回包")
	}
	if got := pickUpFailedFrames(rec); len(got) != 1 || got[0] != s2c.ItemPickUpFailReason_General {
		t.Fatalf("背包满应回 General，got %v", got)
	}
	if _, ok := srv.deps.cfg.Drops.PeekItem(id); !ok {
		t.Fatal("拾取失败后地面物必须保留（原版不回滚地面）")
	}
}

// TestPickupSuccessSendsNoFailure 验证正常拾取不误发失败帧（回归护栏）。
func TestPickupSuccessSendsNoFailure(t *testing.T) {
	srv, sess, _, rec := pickupScaffold(t, "lucky")
	id := srv.deps.cfg.Drops.AddItem(0, 101, 100,
		&item.Item{Group: 0, Number: 7, Level: 0, Durability: 20}, time.Now())

	tryPickup(srv, sess, id)

	if got := pickUpFailedFrames(rec); len(got) != 0 {
		t.Fatalf("成功拾取不应发失败帧，got %v", got)
	}
	if _, ok := srv.deps.cfg.Drops.PeekItem(id); ok {
		t.Fatal("成功拾取后地面物应消失")
	}
}
