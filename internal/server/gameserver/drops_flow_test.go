package gameserver

// drops_flow_test.go —— T2-3/T2-4 闭环测试：击杀 → 掉落入地面 → 拾取入背包 →
// 丢弃回地面，以及金币拾取与过期清理。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/server/loginserver"
	"mugo/internal/util"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

// newKillTestScaffold 构造带怪物+掉落注册表的 GS 与一个 200 级会话（已进世界）。
func newKillTestScaffold(t *testing.T) (*Server, *session, *world.Player, *entity.Character) {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp, Drops: world.NewDropRegistry()})
	srv.dropDelay = 0 // 掉落内联执行，便于同步步断言（延迟语义另有专测）

	rec := &packetRecorder{}
	c := &entity.Character{
		Name: "killer", ClassNumber: 4, Level: 200, // 伤害下限 = 200/10 = 20
		MapNumber: 0, X: 139, Y: 127, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(&entity.Account{Name: "killer"})
	sess.setSelected(c)
	sess.setState(entity.StateEnteringMap)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()
	srv.enterWorld(sess, c)
	if sess.getState() != entity.StateEnteredWorld {
		t.Fatalf("进世界失败: %d", sess.getState())
	}
	wp := sess.getWorldPlayer()
	if wp == nil {
		t.Fatal("世界玩家缺失")
	}
	return srv, sess, wp, c
}

// TestKillDropsToGround 锁定 T2-3：击杀怪物后地面出现掉落（金币/物品至少其一），
// 固定种子下结果可复现。掉落为概率事件（如 Money 组 0.5），故击杀多只累积断言。
func TestKillDropsToGround(t *testing.T) {
	srv, sess, wp, c := newKillTestScaffold(t)
	cfg := srv.deps.cfg.GameConfig
	if groups := cfg.DropGroupsFor(0, 0); len(groups) == 0 {
		t.Fatal("Lorencia Bull Fighter 应有掉落组（地图级 ∪ 怪物级）")
	}

	before := len(srv.deps.cfg.Drops.ItemsOnMap(0)) + len(srv.deps.cfg.Drops.MoneyOnMap(0))
	killed := 0
	for _, target := range srv.deps.cfg.NPCs.ByMap(0) {
		if !target.Alive() || killed >= 6 {
			continue
		}
		// 把唯一的杀手会话搬到怪旁连击（handleHit 校验切比雪夫 ≤2）。
		wp.X, wp.Y = target.X-2, target.Y
		c.X, c.Y = wp.X, wp.Y
		for i := 0; i < 60 && target.Alive(); i++ {
			req := c2s.NewHitRequest()
			req.SetTargetId(target.ID)
			srv.handleHit(sess, req.Bytes())
		}
		if !target.Alive() {
			killed++
		}
	}
	if killed < 6 {
		t.Fatalf("击杀数不足: %d", killed)
	}
	after := len(srv.deps.cfg.Drops.ItemsOnMap(0)) + len(srv.deps.cfg.Drops.MoneyOnMap(0))
	if after <= before {
		t.Fatalf("击杀 %d 只后地面应出现掉落: before=%d after=%d", killed, before, after)
	}
}

// TestPickupItemAndMoney 锁定 T2-4 拾取：物品入背包 + C2 21 移除 + C3 22 入包；
// 金币拾取入账 + 金币更新包。
func TestPickupItemAndMoney(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "picker", 100, 100, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: 500}

	it := &item.Item{Group: 0, Number: 7, Level: 3, Durability: 20}
	idItem := srv.deps.cfg.Drops.AddItem(0, 101, 100, it, time.Now())
	idMoney := srv.deps.cfg.Drops.AddMoney(0, 102, 100, 777, time.Now())

	// 拾取物品。
	req := c2s.NewPickupItemRequest()
	req.SetItemId(idItem)
	srv.handlePickupItem(sess, req.Bytes())

	if c.Inventory == nil || c.Inventory.Count() != 1 {
		t.Fatalf("拾取后背包应有 1 件物品")
	}
	if n := countFrames(rec, 0xC2, 0x21); n != 1 {
		t.Fatalf("应收到 1 帧地面物移除包, got %d", n)
	}
	if n := countFrames(rec, 0xC3, 0x22); n < 1 {
		t.Fatalf("应收到物品入背包包")
	}
	if _, ok := srv.deps.cfg.Drops.TakeItem(idItem); ok {
		t.Fatal("地面物应已被拾取移除")
	}

	// 拾取金币。
	req2 := c2s.NewPickupItemRequest()
	req2.SetItemId(idMoney)
	srv.handlePickupItem(sess, req2.Bytes())
	if c.Stats.Money != 500+777 {
		t.Fatalf("金币应入账: %d want %d", c.Stats.Money, 500+777)
	}
	if _, ok := srv.deps.cfg.Drops.TakeMoney(idMoney); ok {
		t.Fatal("地面金币应已被拾取移除")
	}
}

// TestDropItemFromInventory 锁定 T2-4 丢弃：背包移除 + 地面新增 + 成功响应。
func TestDropItemFromInventory(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "dropper", 100, 100, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	c := sess.getSelected()
	c.Inventory = storage.NewInventory(0)
	it := &item.Item{Group: 2, Number: 4, Level: 0, Durability: 28}
	si := srv.newSlottedItem(it)
	if !c.Inventory.AddToFree(si) {
		t.Fatal("预置背包物品失败")
	}
	slot := si.Slot

	req := c2s.NewDropItemRequest()
	req.SetItemSlot(slot)
	req.SetTargetX(102)
	req.SetTargetY(100)
	srv.handleDropItem(sess, req.Bytes())

	if c.Inventory.Count() != 0 {
		t.Fatalf("丢弃后背包应为空")
	}
	items := srv.deps.cfg.Drops.ItemsOnMap(0)
	if len(items) != 1 || items[0].X != 102 || items[0].Y != 100 {
		t.Fatalf("地面应出现掉落物 @(102,100): %v", items)
	}
	if n := countFrames(rec, 0xC1, 0x23); n != 1 {
		t.Fatalf("应收到 1 帧丢弃响应, got %d", n)
	}
	if n := countFrames(rec, 0xC2, 0x20); n != 1 {
		t.Fatalf("应收到 1 帧地面物包, got %d", n)
	}
}

// TestDropExpiryBroadcast 锁定过期清理：过期后从地面移除（明细返回）。
func TestDropExpiryBroadcast(t *testing.T) {
	srv := newScopeTestSrv(t)
	now := time.Now()
	id := srv.deps.cfg.Drops.AddItem(0, 100, 100, &item.Item{Group: 0, Number: 3}, now.Add(-world.DefaultDropDuration-time.Second))
	expired := srv.deps.cfg.Drops.ExpireBefore(now)
	if len(expired) != 1 || expired[0].ID != id {
		t.Fatalf("过期清理明细异常: %v", expired)
	}
	if _, ok := srv.deps.cfg.Drops.TakeItem(id); ok {
		t.Fatal("过期物应已移除")
	}
}
