// handler_message_test.go —— TRIM-11 系统消息（C1 0D ServerMessage）在 handler 层的接线锚定：
// 谁在什么时机发哪一句、与失败包的先后顺序、蓝字通道。
package gameserver

import (
	"strconv"
	"strings"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/view/remote"
)

// blueMessage 是一条出站系统提示（已剥掉 Season>0 的 9 个 0 前缀）。
type blueMessage struct {
	msgType s2c.MessageType
	text    string
}

// serverMessages 按发送顺序返回会话收到的全部 C1 0D。
func serverMessages(rec *packetRecorder) []blueMessage {
	var out []blueMessage
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == s2c.ServerMessageCode {
			p := s2c.AsServerMessage(f)
			out = append(out, blueMessage{
				msgType: p.Type(),
				text:    strings.TrimPrefix(p.MessageString(), "000000000"),
			})
		}
	}
	return out
}

// wantBlueMessages 断言系统提示的数量、文本（逐条）与通道（一律蓝字）。
func wantBlueMessages(t *testing.T, rec *packetRecorder, want ...string) {
	t.Helper()
	got := serverMessages(rec)
	if len(got) != len(want) {
		t.Fatalf("系统提示应为 %d 条, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i].text != want[i] {
			t.Fatalf("第 %d 条提示应为 %q, got %q", i+1, want[i], got[i].text)
		}
		if got[i].msgType != s2c.MessageType_BlueNormal {
			t.Fatalf("第 %d 条提示通道应为 BlueNormal(1), got %d", i+1, got[i].msgType)
		}
	}
}

// TestConsumeUnimplementedSendsBlueAfterFailPacket 锁定顺序：原版 ItemConsumeAction
// 先回 C1 26 FD，再发 UsingThisItemNotImplemented 蓝字（蓝字不得抢在失败包之前）。
func TestConsumeUnimplementedSendsBlueAfterFailPacket(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 100
	// 创造宝石在原版也没有消耗插件（ItemConsumeActions 目录无 CreationJewel*）
	// → 落到"未实现消耗品"分支。精炼石 14,43/14,44 已于 S-3 后续登记为策略。
	it := &item.Item{Group: 14, Number: 22, Level: 0, Durability: 3}
	m.putItem(t, 12, it)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	wantBlueMessages(t, m.rec, "Using this item is not implemented.")
	if len(failedFrames(m.rec)) != 1 {
		t.Fatalf("未实现消耗品仍应回 1 帧 C1 26 FD, got %d", len(failedFrames(m.rec)))
	}
	fdAt, blueAt := -1, -1
	for i, f := range m.rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0x26 && f[3] == 0xFD {
			fdAt = i
		}
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == s2c.ServerMessageCode && blueAt < 0 {
			blueAt = i
		}
	}
	if fdAt > blueAt {
		t.Fatalf("出站顺序应为 FD(%d) → 蓝字(%d)", fdAt, blueAt)
	}
	if it.Durability != 3 {
		t.Fatalf("未实现分支不得消耗物品, got %d", it.Durability)
	}
}

// TestLevelUpSendsCongratsAfterLevelPacket 锁定升级祝贺：蓝字 LevelUpCongrats
// 在 C1 F3 05 之后（对照 UpdateLevelExtendedPlugIn 的发包次序）。
func TestLevelUpSendsCongratsAfterLevelPacket(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(31, "upgrader", 100, 100, rec)
	c := sess.getSelected()
	c.Level = 5
	c.Stats = player.NewCharStats(c.ClassNumber, c.Level)
	wp.Stats = c.Stats
	wp.IsAlive = true
	sess.setWorldPlayer(wp)

	srv.applyLevelUp(sess, c, wp, srv.viewFor(sess))

	levelAt, blueAt := -1, -1
	for i, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0xF3 && f[3] == 0x05 {
			levelAt = i
		}
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == s2c.ServerMessageCode && blueAt < 0 {
			blueAt = i
		}
	}
	if levelAt < 0 {
		t.Fatal("应发 1 帧 C1 F3 05 等级包")
	}
	if levelAt > blueAt {
		t.Fatalf("祝贺应在等级包之后: 等级包 %d, 蓝字 %d", levelAt, blueAt)
	}
	wantBlueMessages(t, rec, "Congratulations, you are Level 5 now.")
}

// TestStatRejectionsSendBlue 锁定加点两条拒绝：无确认帧、只有蓝字。
func TestStatRejectionsSendBlue(t *testing.T) {
	t.Run("点数不足", func(t *testing.T) {
		sc := newStatsScaffold(t, 0)
		sc.send(c2s.CharacterStatAttribute_Energy)
		wantBlueMessages(t, sc.rec, "Not enough level up points available.")
		if n := countFrames(sc.rec, 0xC1, 0xF3); n != 0 {
			t.Fatalf("拒绝不应回确认帧, got %d", n)
		}
	})
	t.Run("属性不可用", func(t *testing.T) {
		sc := newStatsScaffold(t, 5)
		sc.send(c2s.CharacterStatAttribute(9))
		wantBlueMessages(t, sc.rec, "Attribute not available.")
	})
}

// TestNpcBuyRejectionsSendBlue 锁定商店拒绝路径：具体原因各一条蓝字；
// 未开商店按原版只回失败包、无文字。
func TestNpcBuyRejectionsSendBlue(t *testing.T) {
	t.Run("未开商店", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000000)
		req := c2s.NewBuyItemFromNpcRequest()
		req.SetItemSlot(0)
		sc.srv.handleNpcBuy(sc.sess, req.Bytes())
		if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 {
			t.Fatalf("应回 1 帧 C1 32 FF, got %d", n)
		}
		wantBlueMessages(t, sc.rec)
	})
	t.Run("金币不足", func(t *testing.T) {
		sc := newNpcScaffold(t, 1)
		sc.talk(t)
		sc.rec.frames = nil
		slot, _ := sc.cheapestSlot(t)
		req := c2s.NewBuyItemFromNpcRequest()
		req.SetItemSlot(slot)
		sc.srv.handleNpcBuy(sc.sess, req.Bytes())
		wantBlueMessages(t, sc.rec, "You don't have enough Money")
		if n := countFrames(sc.rec, 0xC1, 0x32); n != 1 {
			t.Fatalf("蓝字之后仍应回 1 帧 C1 32 FF, got %d", n)
		}
	})
}

// TestRepairRejectionsSendBlue 锁定修理拒绝：原版只发蓝字、不回任何失败包。
func TestRepairRejectionsSendBlue(t *testing.T) {
	srv, sess, c, rec := newRepairSess(t, 0)
	def := firstRepairable(t, srv)
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: 0, Durability: 0}
	_ = c.Inventory.AddToSlot(12, srv.newSlottedItem(it))

	srv.handleRepair(sess, repairFrame(5))  // 空格子
	srv.handleRepair(sess, repairFrame(12)) // 钱不够

	wantBlueMessages(t, rec,
		"No item there to repair.",
		"You don't have enough money to repair.",
	)
	if n := countFrames(rec, 0xC1, 0x2A); n != 0 {
		t.Fatalf("修理拒绝不得发耐久包, got %d", n)
	}
}

// TestWarpCommandRejectionsSendBlue 锁定传送清单拒绝文案：UnknownWarpIndex 走目录
// （原版不传参 → "{0}" 原样到客户端），等级/金额是 WarpAction 就地拼接的字面。
func TestWarpCommandRejectionsSendBlue(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	gc := m.srv.deps.cfg.GameConfig

	warpFrame := func(index uint16) []byte {
		req := c2s.NewWarpCommandRequest()
		req.SetWarpInfoIndex(index)
		return req.Bytes()
	}

	// 1) 不存在的索引。
	m.srv.handleWarpCommand(m.sess, warpFrame(60000))
	wantBlueMessages(t, m.rec, "Unknown warp index {0}")

	// 2) 等级够、钱不够：挑一条带费用的传送项，余额设成费用-1。
	var paid *config.Warp
	for i := range gc.Warps {
		w := &gc.Warps[i]
		if w.Costs > 0 && w.Gate != nil && w.Gate.Map != nil {
			paid = w
			break
		}
	}
	if paid == nil {
		t.Skip("导出件无带费用的传送项")
	}
	m.c.Level = 65535
	m.c.Stats.Money = uint32(paid.Costs) - 1
	m.rec.frames = nil

	m.srv.handleWarpCommand(m.sess, warpFrame(uint16(paid.Index)))
	wantBlueMessages(t, m.rec, "You need "+strconv.Itoa(paid.Costs)+" in order to warp")
	if n := countFrames(m.rec, 0xC1, 0x1C); n != 0 {
		t.Fatalf("金额不足不得换图, got %d 帧 C1 1C", n)
	}
}

// TestNpcTalkWithoutWindowSendsBlue 锁定无窗口 NPC 的兜底提示（带编号与名字两个参数）。
func TestNpcTalkWithoutWindowSendsBlue(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	gc := sc.srv.deps.cfg.GameConfig
	// 找一个"既无商店、窗口又是 Undefined"的 NPC（原版该分支才发
	// TalkingNotImplementedFormat；其余窗口各有自己的应答路径）。
	var target *npc.Npc
	for _, n := range sc.srv.deps.cfg.NPCs.ByMap(0) {
		if n.Def == nil || int(n.Def.NpcWindow) != remote.NpcWindowUndefined {
			continue
		}
		if st, ok := gc.MerchantStore(int(n.Number)); ok && len(st.Items) > 0 {
			continue
		}
		target = n
		break
	}
	if target == nil {
		t.Skip("Lorencia 无窗口未定义的 NPC")
	}

	req := c2s.NewTalkToNpcRequest()
	req.SetNpcId(target.ID)
	sc.srv.handleNpcTalk(sc.sess, req.Bytes())

	wantBlueMessages(t, sc.rec,
		"Talking to this NPC ("+strconv.Itoa(int(target.Number))+", "+target.Name+") is not implemented yet.")
}
