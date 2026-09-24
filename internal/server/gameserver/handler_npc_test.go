package gameserver

// handler_npc_test.go —— T2-9 端到端：NPC 对话（C3 30 → C3 30+C2 31）、
// 买入（C3 32 → C1 32 + 金币）、堆叠买入、失败分支（C1 32 FF）、
// 卖出（C3 33 → 成功/拒绝帧）、关闭（C1 31）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/util"
)

const testShopNpcID uint16 = 0x2AA

// npcScaffold 装配：真实导出件 + 全量 NPC 生成 + Lorencia 里一家商店
// （ID 固定为 testShopNpcID 便于断言）+ 已进图的买家会话。
type npcScaffold struct {
	srv     *Server
	sess    *session
	wp      *world.Player
	c       *entity.Character
	rec     *packetRecorder
	shopNpc *npc.Npc
}

func newNpcScaffold(t *testing.T, money uint32) *npcScaffold {
	t.Helper()
	srv := newScopeTestSrv(t)
	srv.npcDialogDelay = 0 // 原版开窗前 Task.Delay(500) 在测试里同步跳过（与 dropDelay 同理）
	gc := srv.deps.cfg.GameConfig
	sp := npc.NewSpawner(gc, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	srv.deps.cfg.NPCs = sp

	var shopNpc *npc.Npc
	for _, n := range sp.ByMap(0) {
		if st, ok := gc.MerchantStore(int(n.Number)); ok && len(st.Items) > 0 {
			shopNpc = n
			break
		}
	}
	if shopNpc == nil {
		t.Fatal("Lorencia 应有带商店的 NPC（Potion Girl / 铁匠等）")
	}
	shopNpc.ID = testShopNpcID

	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "buyer", 120, 125, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: money}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return &npcScaffold{srv: srv, sess: sess, wp: wp, c: c, rec: rec, shopNpc: shopNpc}
}

// talk 与商店 NPC 对话并断言窗口 + 商品清单两帧。
func (sc *npcScaffold) talk(t *testing.T) {
	t.Helper()
	req := c2s.NewTalkToNpcRequest()
	req.SetNpcId(testShopNpcID)
	sc.srv.handleNpcTalk(sc.sess, req.Bytes())

	if sc.sess.getOpenedNpc() != sc.shopNpc {
		t.Fatal("对话后应打开该 NPC")
	}
	if n := countFrames(sc.rec, 0xC3, 0x30); n != 1 {
		t.Fatalf("应下发 1 帧 C3 30 窗口, got %d", n)
	}
	if n := countFrames(sc.rec, 0xC2, 0x31); n != 1 {
		t.Fatalf("应下发 1 帧 C2 31 商品清单, got %d", n)
	}
	gc := sc.srv.deps.cfg.GameConfig
	store, _ := gc.MerchantStore(int(sc.shopNpc.Number))
	list := s2c.AsStoreItemList(findFrame(sc.rec, 0xC2, 0x31))
	if int(list.ItemCount()) != len(store.Items) {
		t.Fatalf("商品数应 %d, got %d", len(store.Items), list.ItemCount())
	}
	if list.Type() != s2c.ItemWindow_Normal {
		t.Fatalf("商店类型应为 Normal(0), got %d", list.Type())
	}
}

// cheapestSlot 返回最便宜商品的槽位与价格。
func (sc *npcScaffold) cheapestSlot(t *testing.T) (byte, uint32) {
	t.Helper()
	gc := sc.srv.deps.cfg.GameConfig
	store, _ := gc.MerchantStore(int(sc.shopNpc.Number))
	items := sc.srv.shopItems(store)
	best := -1
	bestPrice := 0
	for i := range items {
		p := items[i].BuyingPrice()
		if best < 0 || p < bestPrice {
			best, bestPrice = i, p
		}
	}
	if best < 0 || bestPrice <= 0 {
		t.Fatal("商店应有正价商品")
	}
	return items[best].Slot, uint32(bestPrice)
}

// TestNpcTalkOpensStore 锁定对话主路径：开窗 + 商品清单 + 帧长自洽。
func TestNpcTalkOpensStore(t *testing.T) {
	sc := newNpcScaffold(t, 1000000)
	sc.talk(t)
	frame := findFrame(sc.rec, 0xC2, 0x31)
	gc := sc.srv.deps.cfg.GameConfig
	store, _ := gc.MerchantStore(int(sc.shopNpc.Number))
	views := sc.srv.storeItemViews(store.Items)

	// 帧长必须等于实写长度（原版 actualSize 收缩语义）：6B 头 + Σ(1 槽位 + 数据)。
	total := 6
	for _, v := range views {
		total += 1 + len(v.Data)
	}
	if len(frame) != total {
		t.Fatalf("C2 31 帧长应 %d（逐物品实写）, got %d", total, len(frame))
	}
	// 逐条槽位 + 数据长度（5~15B 扩展布局）。
	off := 6
	for _, v := range views {
		if frame[off] != v.Slot {
			t.Fatalf("槽位序列错位: want %d got %d @%d", v.Slot, frame[off], off)
		}
		n := len(v.Data)
		if n < 5 || n > 15 {
			t.Fatalf("槽 %d 物品数据长度 %d 超出 5~15", v.Slot, n)
		}
		off += 1 + n
	}
}

// TestNpcTalkUnknownNpc 不存在的 NPC ID：不开窗、不置上下文。
func TestNpcTalkUnknownNpc(t *testing.T) {
	sc := newNpcScaffold(t, 1000000)
	req := c2s.NewTalkToNpcRequest()
	req.SetNpcId(0x7AFE)
	sc.srv.handleNpcTalk(sc.sess, req.Bytes())
	if sc.sess.getOpenedNpc() != nil {
		t.Fatal("未知 NPC 不应置上下文")
	}
	if n := countFrames(sc.rec, 0xC3, 0x30) + countFrames(sc.rec, 0xC2, 0x31); n != 0 {
		t.Fatalf("未知 NPC 不应出窗, got %d 帧", n)
	}
}

// TestNpcBuy 验证买入主路径：扣款、入包、C1 32 + 金币帧。
func TestNpcBuy(t *testing.T) {
	sc := newNpcScaffold(t, 1000000)
	sc.talk(t)
	slot, price := sc.cheapestSlot(t)
	sc.rec.frames = nil // 清掉对话帧，聚焦买入序列

	req := c2s.NewBuyItemFromNpcRequest()
	req.SetItemSlot(slot)
	sc.srv.handleNpcBuy(sc.sess, req.Bytes())

	if sc.c.Stats.Money != 1000000-price {
		t.Fatalf("应扣款 %d, money=%d", price, sc.c.Stats.Money)
	}
	if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 {
		t.Fatalf("应下发 1 帧 C1 32, got %d", n)
	}
	bought := s2c.AsItemBought(findFrame(sc.rec, 0xC1, 0x32))
	if bought.InventorySlot() == 0xFF {
		t.Fatal("买入帧不应是失败子码")
	}
	if si := sc.c.Inventory.GetItem(bought.InventorySlot()); si == nil || si.It == nil {
		t.Fatalf("物品应入包槽 %d", bought.InventorySlot())
	}
	if n := countFrames(sc.rec, 0xC3, 0x22); n != 1 {
		t.Fatalf("应下发 1 帧金币更新（C3 22 FE）, got %d", n)
	}
}

// TestNpcBuyStacked 可堆叠商品买进既有堆：目标耐久合并 + 原版同款
// "失败包结束手势" 序列（耐久 → C1 32 FF → 金币）。
func TestNpcBuyStacked(t *testing.T) {
	sc := newNpcScaffold(t, 1000000)
	sc.talk(t)
	gc := sc.srv.deps.cfg.GameConfig
	store, _ := gc.MerchantStore(int(sc.shopNpc.Number))
	items := sc.srv.shopItems(store)

	// 找一件可堆叠且角色背包里已有同物（预置 12 槽）的商品。
	var target *action.ShopItem
	for i := range items {
		if items[i].IsStackable() {
			target = &items[i]
			break
		}
	}
	if target == nil {
		t.Skip("该商店无可堆叠商品")
	}
	pre := sc.srv.newSlottedItem(&item.Item{
		Group: byte(target.Def.Group), Number: target.Def.Number,
		Durability: byte(target.Def.Durability) - target.Durability, // 恰好能全部堆上
	})
	if !sc.c.Inventory.AddToSlot(12, pre) {
		t.Fatal("预置堆叠目标失败")
	}
	sc.rec.frames = nil

	req := c2s.NewBuyItemFromNpcRequest()
	req.SetItemSlot(target.Slot)
	sc.srv.handleNpcBuy(sc.sess, req.Bytes())

	if got := sc.c.Inventory.GetItem(12); got == nil || got.It.Durability != byte(target.Def.Durability) {
		t.Fatalf("堆叠目标耐久应合并为 %d", target.Def.Durability)
	}
	// 序列：耐久变化 → C1 32 FF（结束手势）→ 金币更新。
	if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 || findFrame(sc.rec, 0xC1, 0x32)[3] != 0xFF {
		t.Fatal("堆叠买入应回 C1 32 FF 结束手势帧")
	}
	if n := countFrames(sc.rec, 0xC3, 0x22); n != 1 {
		t.Fatalf("应下发金币更新, got %d", n)
	}
}

// TestNpcBuyFailures 失败分支：无商店 / 未知槽位 → C1 32 FF，不扣款。
func TestNpcBuyFailures(t *testing.T) {
	t.Run("未打开商店", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000000)
		req := c2s.NewBuyItemFromNpcRequest()
		req.SetItemSlot(0)
		sc.srv.handleNpcBuy(sc.sess, req.Bytes())
		if sc.c.Stats.Money != 1000000 {
			t.Fatalf("不扣款, money=%d", sc.c.Stats.Money)
		}
		if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 || findFrame(sc.rec, 0xC1, 0x32)[3] != 0xFF {
			t.Fatal("应回 C1 32 FF")
		}
	})
	t.Run("未知槽位", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000000)
		sc.talk(t)
		sc.rec.frames = nil
		req := c2s.NewBuyItemFromNpcRequest()
		req.SetItemSlot(0xF5)
		sc.srv.handleNpcBuy(sc.sess, req.Bytes())
		if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 || findFrame(sc.rec, 0xC1, 0x32)[3] != 0xFF {
			t.Fatal("应回 C1 32 FF")
		}
	})
	t.Run("金币不足", func(t *testing.T) {
		sc := newNpcScaffold(t, 1)
		sc.talk(t)
		sc.rec.frames = nil
		slot, _ := sc.cheapestSlot(t)
		req := c2s.NewBuyItemFromNpcRequest()
		req.SetItemSlot(slot)
		sc.srv.handleNpcBuy(sc.sess, req.Bytes())
		if sc.c.Stats.Money != 1 {
			t.Fatalf("不扣款, money=%d", sc.c.Stats.Money)
		}
		if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 || findFrame(sc.rec, 0xC1, 0x32)[3] != 0xFF {
			t.Fatal("应回 C1 32 FF")
		}
	})
}

// TestNpcSell 验证卖出主路径：物去钱来 + 成功帧带新余额。
func TestNpcSell(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	sc.talk(t)
	// 先买一件再卖回（保证物品定义存在于导出件）。
	slot, price := sc.cheapestSlot(t)
	buyReq := c2s.NewBuyItemFromNpcRequest()
	buyReq.SetItemSlot(slot)
	sc.srv.handleNpcBuy(sc.sess, buyReq.Bytes())
	boughtSlot := s2c.AsItemBought(findFrame(sc.rec, 0xC1, 0x32)).InventorySlot()

	sc.rec.frames = nil
	sellReq := c2s.NewSellItemToNpcRequest()
	sellReq.SetItemSlot(boughtSlot)
	sc.srv.handleNpcSell(sc.sess, sellReq.Bytes())

	if sc.c.Inventory.GetItem(boughtSlot) != nil {
		t.Fatal("卖出后源槽应清空")
	}
	frame := findFrame(sc.rec, 0xC3, 0x33)
	if frame == nil {
		t.Fatal("应回 C3 33 结果帧")
	}
	res := s2c.AsNpcItemSellResult(frame)
	if !res.Success() {
		t.Fatalf("卖出应成功: money=%d", res.Money())
	}
	if res.Money() != sc.c.Stats.Money {
		t.Fatalf("结果帧余额应与角色态一致: frame=%d char=%d", res.Money(), sc.c.Stats.Money)
	}
	// 卖出入账 ≥0（苹果等药水卖价可为 0——原版 RoundPrice 后即 0，非缺陷）。
	if sc.c.Stats.Money < 1000-price {
		t.Fatalf("卖出后金币不应减少: %d", sc.c.Stats.Money)
	}
}

// TestNpcSellRejected 拒绝分支：无商店卖出 → fail 帧、物品保留。
func TestNpcSellRejected(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	// 不对话直接卖：storeOpen=false。
	sc.srv.ensureInventory(sc.c)
	pre := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 0, Durability: 1})
	if !sc.c.Inventory.AddToSlot(12, pre) {
		t.Fatal("预置物品失败")
	}
	req := c2s.NewSellItemToNpcRequest()
	req.SetItemSlot(12)
	sc.srv.handleNpcSell(sc.sess, req.Bytes())

	if sc.c.Inventory.GetItem(12) == nil {
		t.Fatal("拒绝时物品应保留")
	}
	if sc.c.Stats.Money != 1000 {
		t.Fatalf("拒绝不入账, money=%d", sc.c.Stats.Money)
	}
	frame := findFrame(sc.rec, 0xC3, 0x33)
	if frame == nil || s2c.AsNpcItemSellResult(frame).Success() {
		t.Fatal("应回 C3 33 fail 帧")
	}
}

// TestNpcClose 验证 C1 31 关闭对话。
func TestNpcClose(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	sc.talk(t)
	req := c2s.NewCloseNpcRequest()
	sc.srv.handleNpcClose(sc.sess, req.Bytes())
	if sc.sess.getOpenedNpc() != nil {
		t.Fatal("关闭后应清上下文")
	}
}
