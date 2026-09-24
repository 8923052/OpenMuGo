package gameserver

// handler_npc_dialog_test.go —— TRIM-07 对话流端到端（真导出件）：
// legacy 任务三段门槛与 C1 A1/A4、NpcDialog 的 C3 F9 01、会长 NPC 的 C1 54 与两条气泡、
// 0xA2 请求驱动的接/交、以及"商店开窗前 500ms"这一原版的可观察配置。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// findNpcByDefNumber 在指定地图找某定义号的实例（对话测试按定义号取对象）。
func findNpcByDefNumber(t *testing.T, srv *Server, mapNumber uint16, defNumber int) *npc.Npc {
	t.Helper()
	for _, n := range srv.deps.cfg.NPCs.ByMap(mapNumber) {
		if int(n.Number) == defNumber {
			return n
		}
	}
	t.Fatalf("地图 %d 上没有定义号 %d 的 NPC", mapNumber, defNumber)
	return nil
}

// talkTo 造一次 C3 30 对话（并把玩家/角色挪到该 NPC 所在图，对话只按地图取对象）。
func (sc *npcScaffold) talkTo(n *npc.Npc) {
	sc.wp.MapNumber = n.MapNumber
	sc.c.MapNumber = n.MapNumber
	req := c2s.NewTalkToNpcRequest()
	req.SetNpcId(n.ID)
	sc.srv.handleNpcTalk(sc.sess, req.Bytes())
}

func (sc *npcScaffold) setClass(class byte) {
	sc.c.ClassNumber = class
	if sc.wp != nil {
		sc.wp.Class = class
	}
}

// frameCount 按"头类型 + 码位 + 可选子码位"数帧。
// 短头（C1/C3）是 code@[2]/sub@[3]，长头（C2/C4）是 code@[3]/sub@[4]（对照 proto setHeader）。
func frameCount(rec *packetRecorder, header, code, sub byte, anySub bool) int {
	codeAt, subAt := 2, 3
	if header == 0xC2 || header == 0xC4 {
		codeAt, subAt = 3, 4
	}
	n := 0
	for _, f := range rec.frames {
		if len(f) <= codeAt || f[0] != header || f[codeAt] != code {
			continue
		}
		if anySub || (len(f) > subAt && f[subAt] == sub) {
			n++
		}
	}
	return n
}

// TestNpcDialogWindowUsesOpenNpcDialog 锁定 Elf Soldier(257, NpcDialog) 走 C3 F9 01，
// 而不是窗包（原版 Convert 对 NpcDialog 是 throw，专门分支发对话框）。
func TestNpcDialogWindowUsesOpenNpcDialog(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	sc.srv.npcDialogDelay = 0
	target := findNpcByDefNumber(t, sc.srv, 0, 257)
	sc.talkTo(target)

	if n := frameCount(sc.rec, 0xC3, 0xF9, 0x01, false); n != 1 {
		t.Fatalf("应收到 1 帧 C3 F9 01 OpenNpcDialog, got %d", n)
	}
	if n := frameCount(sc.rec, 0xC3, 0x30, 0, true); n != 0 {
		t.Fatalf("NpcDialog 不该同时发 C3 30 窗包, got %d", n)
	}
	for _, f := range sc.rec.frames {
		if len(f) >= 5 && f[0] == 0xC3 && f[2] == 0xF9 {
			if got := s2c.AsOpenNpcDialog(f).NpcNumber(); got != 257 {
				t.Fatalf("对话框应带 NPC 定义号 257, got %d", got)
			}
		}
	}
}

// TestLegacyQuestDialogBranches 用 Marlon(229) 验三段门槛：职业不符、等级不够、可对话。
func TestLegacyQuestDialogBranches(t *testing.T) {
	t.Run("职业不符→气泡并关对话", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000)
		sc.srv.npcDialogDelay = 0
		sc.setClass(16) // Marlon 的 legacy 任务只给 6/2/10/22
		sc.talkTo(findNpcByDefNumber(t, sc.srv, 0, 229))
		wantBluelessBubble(t, sc.rec, "I have no quests for you.")
		if sc.sess.getOpenedNpc() != nil {
			t.Fatal("该分支应关掉对话")
		}
		if frameCount(sc.rec, 0xC1, 0xA1, 0, true) != 0 {
			t.Fatal("该分支不该发对话框")
		}
	})

	t.Run("等级不够→气泡", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000)
		sc.srv.npcDialogDelay = 0
		sc.setClass(6)
		sc.c.Level = 100 // Marlon 的任务都要 220 级
		sc.talkTo(findNpcByDefNumber(t, sc.srv, 0, 229))
		wantBluelessBubble(t, sc.rec, "I have nothing to do for you. Come back with more power.")
	})

	t.Run("可对话→C1 A1 带任务号与状态字节", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000)
		sc.srv.npcDialogDelay = 0
		sc.setClass(6)
		sc.c.Level = 225
		sc.talkTo(findNpcByDefNumber(t, sc.srv, 0, 229))
		f := findFrame(sc.rec, 0xC1, 0xA1)
		if f == nil {
			t.Fatalf("应收到 C1 A1 对话框, frames=%d", len(sc.rec.frames))
		}
		dlg := s2c.AsLegacyQuestStateDialog(f)
		if dlg.QuestIndex() != 2 {
			t.Fatalf("展示的应是下一条可接的 0/2（Gain Hero Status (Blade Knight)）, got %d", dlg.QuestIndex())
		}
		// 该任务无击杀需求 → 不该有 A4。
		if frameCount(sc.rec, 0xC1, 0xA4, 0x00, false) != 0 {
			t.Fatal("无击杀需求时不该发 A4")
		}
	})
}

// TestLegacyQuestDialogWithKillInfo 用 Priest Devin(406) 验 A4：进行中任务是 0/5
// （三条击杀需求）时，对话框后紧跟一帧击杀进度。
func TestLegacyQuestDialogWithKillInfo(t *testing.T) {
	sc := newNpcScaffold(t, 20_000_000)
	sc.srv.npcDialogDelay = 0
	devin := findNpcByDefNumber(t, sc.srv, 2, 406)
	sc.setClass(4)
	sc.c.Level = 400
	sc.c.QuestStates = []entity.QuestState{entity.NewQuestState(legacyQuestGroup)}
	st := questStateOfGroup(sc.c, legacyQuestGroup)
	idx := sc.srv.deps.cfg.GameConfig.QuestIndexOf(mustQuestAtNpc(t, sc.srv, devin.Def.Number, 0, 5, sc.c.ClassNumber))
	st.ActiveQuestIndex = idx

	sc.talkTo(devin)
	if findFrame(sc.rec, 0xC1, 0xA1) == nil {
		t.Fatal("应收到 C1 A1")
	}
	f := findFrame(sc.rec, 0xC1, 0xA4)
	if f == nil {
		t.Fatal("进行中任务有 3 条击杀需求，应收到 C1 A4")
	}
	info := s2c.AsLegacyQuestMonsterKillInfo(f)
	if info.QuestIndex() != 5 {
		t.Fatalf("A4 任务号应为 5, got %d", info.QuestIndex())
	}
	want := []struct {
		monster uint32
		count   uint32
	}{{409, 0}, {410, 0}, {411, 0}}
	for i, w := range want {
		k := info.Kills(i)
		if k == nil || k.MonsterNumber() != w.monster || k.KillCount() != w.count {
			t.Fatalf("第 %d 条击杀条目错: %+v", i, k)
		}
	}
}

// TestLegacyQuestStateSetRequest 走 C1 A2：Active→开始（回 A2 应答并置进行中）。
func TestLegacyQuestStateSetRequest(t *testing.T) {
	sc := newNpcScaffold(t, 20_000_000)
	sc.srv.npcDialogDelay = 0
	sc.setClass(6)
	sc.c.Level = 225
	marlon := findNpcByDefNumber(t, sc.srv, 0, 229)
	sc.talkTo(marlon) // 先开对话（原版接任务要求在 NPC 面前）
	sc.rec.frames = nil

	req := c2s.NewLegacyQuestStateSetRequest()
	req.SetQuestNumber(2)
	req.SetNewState(c2s.LegacyQuestState_Active)
	sc.srv.handleLegacyQuestStateSet(sc.sess, req.Bytes())

	st := questStateOfGroup(sc.c, legacyQuestGroup)
	if st == nil || st.ActiveQuestIndex < 0 {
		t.Fatalf("0xA2 Active 应开始任务, got %+v", st)
	}
	f := findFrame(sc.rec, 0xC1, 0xA2)
	if f == nil {
		t.Fatal("应回 C1 A2 应答")
	}
	if got := s2c.AsLegacySetQuestStateResponse(f).QuestIndex(); got != 2 {
		t.Fatalf("应答任务号应为 2, got %d", got)
	}
	// 再发一次 Active：原版走"已完成过→完成"分支，这里因需求未满足而不变进行中号。
	sc.srv.handleLegacyQuestStateSet(sc.sess, req.Bytes())
	if st2 := questStateOfGroup(sc.c, legacyQuestGroup); st2.ActiveQuestIndex != st.ActiveQuestIndex {
		t.Fatal("重复 Active 不应改变进行中任务")
	}
}

// TestGuildMasterDialogBranches 验会长 NPC：等级不足回气泡，够格回 C1 54。
func TestGuildMasterDialogBranches(t *testing.T) {
	t.Run("等级不足", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000)
		sc.srv.npcDialogDelay = 0
		sc.c.Level = 99
		sc.talkTo(findNpcByDefNumber(t, sc.srv, 2, 241))
		wantBluelessBubble(t, sc.rec, "Your level should be at least level 100")
		if frameCount(sc.rec, 0xC1, 0x54, 0, true) != 0 {
			t.Fatal("等级不足不该弹建盟窗")
		}
	})
	t.Run("够格→C1 54", func(t *testing.T) {
		sc := newNpcScaffold(t, 1000)
		sc.srv.npcDialogDelay = 0
		sc.c.Level = 120
		sc.talkTo(findNpcByDefNumber(t, sc.srv, 2, 241))
		if frameCount(sc.rec, 0xC1, 0x54, 0, true) != 1 {
			t.Fatalf("应收到 1 帧 C1 54 ShowGuildMasterDialog, frames=%d", len(sc.rec.frames))
		}
	})
}

// TestChaosMachineTalkSendsWindowOnly 收口 TRIM-02 的偏差：对话只开窗，
// 合成清单在一次合成之后才发（对照 TalkNpcAction 的 ChaosMachine 分支）。
func TestChaosMachineTalkSendsWindowOnly(t *testing.T) {
	sc := newNpcScaffold(t, 1000)
	sc.srv.npcDialogDelay = 0
	goblin := findNpcByDefNumber(t, sc.srv, 3, 238)
	sc.talkTo(goblin)

	win := findFrame(sc.rec, 0xC3, 0x30)
	if win == nil {
		t.Fatal("应收到 C3 30 窗包")
	}
	if got := s2c.AsNpcWindowResponse(win).Window(); got != s2c.NpcWindow_ChaosMachine {
		t.Fatalf("窗口号应为 ChaosMachine(3), got %d", got)
	}
	if frameCount(sc.rec, 0xC2, 0x31, 0, true) != 0 {
		t.Fatal("对话阶段不该发合成清单（C2 31）")
	}
	if sc.sess.craftStorage == nil {
		t.Fatal("该 NPC 挂有配方，应建临时容器（原版 BackupInventory）")
	}
}

// TestMerchantStoreKeepsDialogDelay 钉住"500ms 延迟"确实存在且只作用于商店分支：
// 生产默认值必须是原版的 500ms，且商店对话会等它。
func TestMerchantStoreKeepsDialogDelay(t *testing.T) {
	if got := newScopeTestSrv(t).npcDialogDelay; got != defaultNpcDialogDelay {
		t.Fatalf("默认对话延迟应为 %v, got %v", defaultNpcDialogDelay, got)
	}
	sc := newNpcScaffold(t, 1000)
	sc.srv.npcDialogDelay = 40 * time.Millisecond
	start := time.Now()
	sc.talk(t) // 商店分支（内部已含开窗 + 清单两帧断言）
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("商店开窗前应等待延迟, elapsed=%v", elapsed)
	}
	// 非商店分支（NpcDialog）不等。
	sc.srv.npcDialogDelay = 400 * time.Millisecond
	start = time.Now()
	sc.talkTo(findNpcByDefNumber(t, sc.srv, 0, 257))
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("非商店分支不该等对话延迟, elapsed=%v", elapsed)
	}
}

// wantBluelessBubble 断言"只有一条 C1 01 气泡、没有系统蓝字"（原版这些文案硬编码在
// TalkNpcAction 里，走 ObjectMessage 而非 PlayerMessage.resx）。
func wantBluelessBubble(t *testing.T, rec *packetRecorder, want string) {
	t.Helper()
	if got := len(serverMessages(rec)); got != 0 {
		t.Fatalf("这些分支不该发系统蓝字, got %d", got)
	}
	for _, f := range rec.frames {
		if len(f) > 5 && f[0] == 0xC1 && f[2] == 0x01 {
			if got := s2c.AsObjectMessage(f).MessageString(); got == want {
				return
			}
		}
	}
	t.Fatalf("应收到气泡 %q, frames=%d", want, len(rec.frames))
}

// mustQuestAtNpc 取"某 NPC 面前该号的任务定义"（找不到即致命错误）。
func mustQuestAtNpc(t *testing.T, srv *Server, npcNumber, group, number int, class byte) *config.Quest {
	t.Helper()
	q, ok := srv.deps.cfg.GameConfig.QuestAtNpc(npcNumber, group, number, class)
	if !ok {
		t.Fatalf("NPC %d 面前没有任务 %d/%d（职业 %d）", npcNumber, group, number, class)
	}
	return q
}
