package gameserver

// handler_chat_command_test.go —— TRIM-08d/08e 聊天命令端到端：
// C1 F5 00 → 逐条 C2 F5 01；/ 前缀走分派（未知/未激活/状态不足都静默）、
// /help /list /add /goldnotice /online 的真实效果。
//
// 入站分派表（handler.go 的 case 0xF5）只有三行，这里直接调对应处理器，
// 与其余 handler 测试同一口径。

import (
	"strings"
	"testing"

	"mugo/internal/gamelogic/entity"
	s2c "mugo/internal/proto/s2c"
)

// seasonPrefix 是 ShowMessage 在 Season>0 时补的渲染占位（见 view/remote/player_view.go）。
const seasonPrefix = "000000000"

// systemMessages 取某会话收到的系统提示（C1 0D），返回 "类型:正文"（已剥季前缀）。
func systemMessages(rec *packetRecorder) []string {
	out := []string{}
	for _, f := range rec.frames {
		if len(f) < 4 || f[0] != 0xC1 || f[2] != 0x0D {
			continue
		}
		p := s2c.AsServerMessage(f)
		out = append(out, string(rune('0'+int(p.Type())))+":"+
			strings.TrimPrefix(p.MessageString(), seasonPrefix))
	}
	return out
}

// say 以角色身份发一条聊天（含 '/' 前缀时即命令）。
func say(srv *Server, sess *session, name, text string) {
	srv.handlePublicChat(sess, publicChatFrame(name, text))
}

func TestChatCommandListNormalPlayer(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	sc.c.Status = entity.CharacterStatusNormal
	sc.rec.frames = nil

	sc.srv.handleChatCommandListRequest(sc.sess)

	frames := 0
	for _, f := range sc.rec.frames {
		if len(f) > 5 && f[0] == 0xC2 && f[3] == 0xF5 && f[4] == 0x01 {
			frames++
		}
	}
	// 普通玩家可用 = 已激活且最小状态为 Normal 的 13 条。
	if frames != 13 {
		t.Fatalf("普通玩家应收到 13 帧 AvailableChatCommand, got %d", frames)
	}
	first := s2c.AsAvailableChatCommand(sc.rec.frames[0])
	if first.Index() != 0 || first.Count() != 13 || first.CommandString() != "/add" ||
		first.MinimumCharacterStatus() != s2c.CharacterStatus_Normal ||
		first.ParameterCount() != 2 {
		t.Fatalf("首帧字段异常: idx=%d cnt=%d cmd=%q st=%d params=%d",
			first.Index(), first.Count(), first.CommandString(),
			first.MinimumCharacterStatus(), first.ParameterCount())
	}
	if first.Parameters(0).ValidValuesString() != "agi|cmd|ene|str|vit" {
		t.Fatalf("ValidValues 应透传: %q", first.Parameters(0).ValidValuesString())
	}
}

func TestChatCommandListGameMasterSeesMore(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	sc.c.Status = entity.CharacterStatusGameMaster
	sc.rec.frames = nil

	sc.srv.handleChatCommandListRequest(sc.sess)

	var last *s2c.AvailableChatCommand
	frames := 0
	for _, f := range sc.rec.frames {
		if len(f) > 5 && f[0] == 0xC2 && f[3] == 0xF5 && f[4] == 0x01 {
			last = s2c.AsAvailableChatCommand(f)
			frames++
		}
	}
	if frames != 43 {
		t.Fatalf("GM 应收到全部 43 条已激活命令, got %d", frames)
	}
	if last == nil || last.Index() != 42 || last.Count() != 42+1 {
		t.Fatalf("末帧 Index/Count 应为 42/43, got %+v", last)
	}
}

func TestChatCommandHelpAndList(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	srv, sess, rec := sc.srv, sc.sess, sc.rec

	say(srv, sess, "stats", "/help add")
	msgs := systemMessages(rec)
	if len(msgs) != 1 || msgs[0] != "1:/add {StatType:agi|cmd|ene|str|vit} {Amount:UInt16} " {
		t.Fatalf("/help add 应回用法蓝字, got %v", msgs)
	}

	rec.frames = nil
	say(srv, sess, "stats", "/help nosuchcommand")
	msgs = systemMessages(rec)
	if len(msgs) != 1 || msgs[0] != "1:The command 'nosuchcommand' does not exist." {
		t.Fatalf("查无此命令应回 CommandDoesNotExist, got %v", msgs)
	}

	// /list：逐条 usage 蓝字，普通玩家 13 条；命令名大小写不敏感（原版 InvariantCultureIgnoreCase）。
	rec.frames = nil
	say(srv, sess, "stats", "/list")
	if got := systemMessages(rec); len(got) != 13 {
		t.Fatalf("/list 应发 13 条用法, got %d", len(got))
	}
	rec.frames = nil
	say(srv, sess, "stats", "/help ADD")
	if got := systemMessages(rec); len(got) != 1 || !strings.Contains(got[0], "/add {") {
		t.Fatalf("/help ADD 应大小写不敏感命中, got %v", got)
	}
}

func TestChatCommandAddStat(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	before := sc.c.Stats.Strength

	say(sc.srv, sc.sess, "stats", "/add str 3")

	if sc.c.Stats.Strength != before+3 {
		t.Fatalf("力量应 +3: %d → %d", before, sc.c.Stats.Strength)
	}
	if sc.c.Stats.LevelUpPoints != 2 {
		t.Fatalf("点数应 5 → 2, got %d", sc.c.Stats.LevelUpPoints)
	}
	f := findFrame(sc.rec, 0xC1, 0xF3)
	if f == nil {
		t.Fatal("应下发与点击加点同一张结果包 C1 F3 06")
	}
	p := s2c.AsCharacterStatIncreaseResponseExtended(f)
	if p.Attribute() != s2c.CharacterStatAttribute_Strength || p.AddedAmount() != 3 {
		t.Fatalf("结果包 attr=%d added=%d", p.Attribute(), p.AddedAmount())
	}
	if n := len(systemMessages(sc.rec)); n != 0 {
		t.Fatalf("成功路径不该有蓝字, got %d", n)
	}
}

// TestChatCommandAddStatRejects：三条拒绝分支各自的对外表现——
// 未知缩写 → UnknownAttribute；该职业没有这条属性（S6 六职业都没有 Base Leadership）
// → CharacterHasNoStatAttribute；点数不足 → NotEnoughLevelUpPointsAvailable。
// 三者都不发结果包。
func TestChatCommandAddStatRejects(t *testing.T) {
	cases := []struct {
		name, command, want string
	}{
		{"未知属性缩写", "/add hp 1", "1:Unknown attribute: 'hp'."},
		{"职业没有该属性", "/add cmd 1", "1:The character has no stat attribute 'cmd'."},
		{"点数不足", "/add str 50", "1:Not enough level up points available."},
	}
	for _, tc := range cases {
		sc := newStatsScaffold(t, 5)
		say(sc.srv, sc.sess, "stats", tc.command)
		msgs := systemMessages(sc.rec)
		if len(msgs) != 1 || msgs[0] != tc.want {
			t.Fatalf("%s：期望 %q, got %v", tc.name, tc.want, msgs)
		}
		if sc.c.Stats.LevelUpPoints != 5 {
			t.Fatalf("%s：不该扣点, got %d", tc.name, sc.c.Stats.LevelUpPoints)
		}
		if findFrame(sc.rec, 0xC1, 0xF3) != nil {
			t.Fatalf("%s：拒绝时不该发结果包", tc.name)
		}
	}
}

// TestChatCommandAmountZeroIsSilent：/addstr 那族被默认停用；/add 的 Amount 转不成数字时
// 原版发一条类型蓝字后仍执行，IncreaseStatsAsync(0) 抛异常被命令基类 catch → 只有类型提示。
func TestChatCommandAmountZeroIsSilent(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	say(sc.srv, sc.sess, "stats", "/add str xx")

	msgs := systemMessages(sc.rec)
	want := "1:The argument Amount was given a invalid type, it expects the value to be of the type UInt16."
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("类型失败应只发一条蓝字, got %v", msgs)
	}
	if sc.c.Stats.LevelUpPoints != 5 || findFrame(sc.rec, 0xC1, 0xF3) != nil {
		t.Fatalf("Amount=0 应不落账也不发包, points=%d", sc.c.Stats.LevelUpPoints)
	}
}

// TestChatCommandUnknownAndDisabledAreSilent：未知命令与"注册了但默认停用"的命令都零出站
// （原版 GetStrategy 落空 → 什么都不做，没有"未知命令"提示）。
func TestChatCommandUnknownAndDisabledAreSilent(t *testing.T) {
	for _, text := range []string{"/nosuchcommand", "/addstr 1", "/getlevel stats", "/teleport 1 1"} {
		sc := newStatsScaffold(t, 5)
		say(sc.srv, sc.sess, "stats", text)
		if n := len(sc.rec.frames); n != 0 {
			t.Fatalf("%q 应完全静默, got %d 帧: %v", text, n, systemMessages(sc.rec))
		}
	}
}

// TestChatCommandStatusGate：/goldnotice 与 /online 是 GM 命令，普通玩家执行只写日志。
// GM 身份下：/goldnotice 全局金字（前导命令词被剥掉）、/online 报本服计数。
// 用 newPartyPair（两个会话都被 track，且都在图）——broadcastMessage 走的是在线玩家表。
func TestChatCommandStatusGate(t *testing.T) {
	srv, sessA, _, recA, recB := newPartyPair(t)

	say(srv, sessA, "alpha", "/goldnotice server restarts soon")
	if n := len(recA.frames) + len(recB.frames); n != 0 {
		t.Fatalf("普通玩家发 GM 命令不该有任何出站, got %d 帧", n)
	}

	sessA.getSelected().Status = entity.CharacterStatusGameMaster
	say(srv, sessA, "alpha", "/goldnotice server restarts soon")
	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		msgs := systemMessages(rec)
		if len(msgs) != 1 || msgs[0] != "0:server restarts soon" {
			t.Fatalf("%s：GM 的 /goldnotice 应发全局金字, got %v", name, msgs)
		}
	}

	recA.frames, recB.frames = nil, nil
	say(srv, sessA, "alpha", "/goldnotice    ")
	if n := len(recA.frames) + len(recB.frames); n != 0 {
		t.Fatalf("空公告不该发包（原版 IsNullOrWhiteSpace 早退）, got %d", n)
	}

	say(srv, sessA, "alpha", "/online")
	msgs := systemMessages(recA)
	if len(msgs) != 1 || msgs[0] != "1:[/online] 1 GM(s) and 1 player(s) online" {
		t.Fatalf("/online 应报本服计数, got %v", msgs)
	}
	if n := len(recB.frames); n != 0 {
		t.Fatalf("/online 只回给发言者自己, beta 收到 %d 帧", n)
	}
}
