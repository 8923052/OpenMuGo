package gameserver

// handler_chat_channels_test.go —— TRIM-08c 频道端到端：~ 组队、@ 战盟、@@ 同盟、
// ! GM 金屏公告、$ 血统阵营（原版没有处理器 → 只记日志）。
//
// 关键断言是"前缀不进不出"：原版处理器拿的是含前缀的整条消息，出站包类型字节除私聊外
// 恒 Normal，客户端靠消息里的前缀着色（ChatViewPlugIn.ConvertChatMessageType）。

import (
	"testing"

	"mugo/internal/api"
	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/server/eventbus"
	"mugo/internal/server/guildserver"
)

// addOutsider 再造一个在线第三方（用于"只有同频道成员可见"的反证）。
func addOutsider(t *testing.T, srv *Server) (*session, *packetRecorder) {
	t.Helper()
	rec := &packetRecorder{}
	sess, wp := newScopedSession(9, "gamma", 22, 20, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	srv.trackSession(sess)
	t.Cleanup(func() { srv.untrackSession(sess) })
	return sess, rec
}

// wireEventBus 把 GS 自己注册进进程内事件总线（对照装配层）。
func wireEventBus(srv *Server) {
	srv.SetEventPublisher(eventbus.NewInMemoryPublisher(
		func() []eventbus.GameServerView { return []eventbus.GameServerView{srv} }, nil, nil))
}

// chatFramesOf 取出某会话收到的公共聊天帧内容（C1 00）。
func chatFramesOf(rec *packetRecorder) []string {
	out := []string{}
	for _, f := range rec.frames {
		if len(f) < 4 || f[0] != 0xC1 || f[2] != 0x00 {
			continue
		}
		p := s2c.AsChatMessage(f)
		out = append(out, p.SenderString()+":"+p.MessageString())
	}
	return out
}

func TestPartyChatKeepsPrefixAndStaysInParty(t *testing.T) {
	srv, sessA, sessB, recA, recB := newPartyPair(t)
	_, recC := addOutsider(t, srv)
	srv.handlePartyInvite(sessA, inviteFrame(8))
	resp := c2s.NewPartyInviteResponse()
	resp.SetAccepted(true)
	srv.handlePartyResponse(sessB, resp.Bytes())
	recA.frames, recB.frames, recC.frames = nil, nil, nil

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "~meet at lorencia"))

	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		got := chatFramesOf(rec)
		if len(got) != 1 || got[0] != "alpha:~meet at lorencia" {
			t.Fatalf("%s 应收到含 ~ 前缀的一条组队聊天, got %v", name, got)
		}
	}
	if got := chatFramesOf(recC); len(got) != 0 {
		t.Fatalf("非队友不该收到组队频道, got %v", got)
	}
}

// newGuildChatSetup：alpha 建 "Alpha" 盟、beta 建自己的 "Beta" 盟（两人不同盟），
// 事件总线接回本服，战盟服句柄交给测试以便结盟。
func newGuildChatSetup(t *testing.T) (*Server, *session, *session, *packetRecorder, *packetRecorder, *guildserver.Server) {
	t.Helper()
	srv, sessA, sessB, recA, recB := newPartyPair(t)
	gs := guildserver.New(guildPubAdapter{gs: srv}, nil)
	srv.SetGuildService(gs)
	wireEventBus(srv)

	create := c2s.NewGuildCreateRequest()
	create.SetGuildName("Alpha")
	srv.handleGuildCreate(sessA, create.Bytes())
	guildA := srv.guildOfCharacter("alpha")
	if guildA == 0 {
		t.Fatal("alpha 建盟后应有战盟归属")
	}
	if _, err := gs.CreateGuildAsync("Beta", "beta", nil, 0); err != nil {
		t.Fatal(err)
	}
	if srv.guildOfCharacter("beta") == 0 {
		t.Fatal("建盟应经 publisher 把归属落到 GS 会话上")
	}
	recA.frames, recB.frames = nil, nil
	return srv, sessA, sessB, recA, recB, gs
}

// TestGuildChatBroadcastsToOwnGuildOnly：alpha 与 beta 各在一盟，@ 发言只回到本盟
// （beta 与别盟的 gamma 都收不到；这也证明路由没有降级成公共频道）。
func TestGuildChatBroadcastsToOwnGuildOnly(t *testing.T) {
	srv, sessA, _, recA, recB, _ := newGuildChatSetup(t)
	_, recC := addOutsider(t, srv)

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "@roster check"))

	if got := chatFramesOf(recA); len(got) != 1 || got[0] != "alpha:@roster check" {
		t.Fatalf("本盟成员（含发言者）应收到 1 条含 @ 前缀的战盟聊天, got %v", got)
	}
	if got := chatFramesOf(recB); len(got) != 0 {
		t.Fatalf("他盟成员不该收到战盟频道, got %v", got)
	}
	if got := chatFramesOf(recC); len(got) != 0 {
		t.Fatalf("无盟玩家不该收到战盟频道, got %v", got)
	}
}

// TestGuildChatFromNonMemberIsSilent：无盟角色发 @ 前缀 → 原版 GuildStatus 为空直接 return，
// 既不发布事件也不降级为公共频道。
func TestGuildChatFromNonMemberIsSilent(t *testing.T) {
	srv, sessA, _, recA, recB := newPartyPair(t)
	wireEventBus(srv)

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "@nobody hears this"))

	if len(recA.frames)+len(recB.frames) != 0 {
		t.Fatalf("无盟发 @ 应静默丢弃, got A=%d B=%d", len(recA.frames), len(recB.frames))
	}
}

// TestAllianceChatNeedsAlliance：没结盟时 @@ 一条都不发（原版 GetAllianceGuildsAsync 返回空表，
// 连发言者自己都没有）；结盟后两盟在线成员各收一条，且补 @@ 前缀。
func TestAllianceChatNeedsAlliance(t *testing.T) {
	srv, sessA, _, recA, recB, gs := newGuildChatSetup(t)

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "@@no alliance yet"))
	if len(recA.frames)+len(recB.frames) != 0 {
		t.Fatalf("未结盟时 @@ 不该有任何出站, got A=%d B=%d", len(recA.frames), len(recB.frames))
	}

	guildA, guildB := srv.guildOfCharacter("alpha"), srv.guildOfCharacter("beta")
	if res, err := gs.CreateAllianceAsync(guildA, guildB); err != nil || res != api.AllianceCreationSuccess {
		t.Fatalf("结盟失败: %v %+v", err, res)
	}
	recA.frames, recB.frames = nil, nil

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "@@allies hear this"))
	if got := chatFramesOf(recA); len(got) != 1 || got[0] != "alpha:@@allies hear this" {
		t.Fatalf("发言者所在盟应收到带 @@ 的广播, got %v", got)
	}
	if got := chatFramesOf(recB); len(got) != 1 || got[0] != "alpha:@@allies hear this" {
		t.Fatalf("同盟他盟应收到带 @@ 的广播, got %v", got)
	}
}

// goldenFrames 取某会话收到的系统消息帧（C1 0D，含类型字节与正文）。
func goldenFrames(rec *packetRecorder) []string {
	out := []string{}
	for _, f := range rec.frames {
		if len(f) < 4 || f[0] != 0xC1 || f[2] != 0x0D {
			continue
		}
		p := s2c.AsServerMessage(f)
		out = append(out, string(rune('0'+int(p.Type())))+":"+p.MessageString())
	}
	return out
}

// TestGlobalNotificationRequiresGameMaster：! 前缀是 GM 的金屏公告
// （ChatMessageGlobalNotificationProcessor.cs:26-31 + GameContext.cs:500-504：
// CharacterStatus 不足直接 return；够格时 TrimStart('!') 后走全局金字，不是聊天包）。
func TestGlobalNotificationRequiresGameMaster(t *testing.T) {
	srv, sessA, _, recA, recB := newPartyPair(t)

	sessA.getSelected().Status = entity.CharacterStatusNormal
	srv.handlePublicChat(sessA, publicChatFrame("alpha", "!server rules ahead"))
	if len(recA.frames)+len(recB.frames) != 0 {
		t.Fatalf("普通玩家发 ! 应静默, got A=%d B=%d", len(recA.frames), len(recB.frames))
	}

	sessA.getSelected().Status = entity.CharacterStatusGameMaster
	recA.frames, recB.frames = nil, nil
	srv.handlePublicChat(sessA, publicChatFrame("alpha", "!server rules ahead"))

	// 两端都收到金字（Season>0 的正文带 9 个 0 前缀，见 view/remote ShowMessage），
	// 且消息里前导的 ! 已被剥掉。
	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		got := goldenFrames(rec)
		if len(got) != 1 || got[0] != "0:000000000server rules ahead" {
			t.Fatalf("%s 应收到一条金字公告(类型 0=GoldenCenter)，got %v", name, got)
		}
	}
}

// TestGensChannelIsDropped：$ 前缀在原版注册了类型但没有处理器
// （ChatMessageAction.cs:34-41 的字典里没有 Gens 项）→ 只记日志、一个包都不发。
func TestGensChannelIsDropped(t *testing.T) {
	srv, sessA, _, recA, recB := newPartyPair(t)

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "$gens talk"))

	if len(recA.frames)+len(recB.frames) != 0 {
		t.Fatalf("$ 频道应被丢弃, got A=%d B=%d", len(recA.frames), len(recB.frames))
	}
}

// TestChatSpoofedNameOnlyLogs：包里的角色名与本会话不符时原版**只写日志**、照常广播。
func TestChatSpoofedNameOnlyLogs(t *testing.T) {
	srv, sessA, _, recA, recB := newPartyPair(t)

	srv.handlePublicChat(sessA, publicChatFrame("somebodyelse", "look busy"))

	if len(chatFramesOf(recA)) != 1 || len(chatFramesOf(recB)) != 1 {
		t.Fatalf("伪造名字不应影响广播: A=%v B=%v", chatFramesOf(recA), chatFramesOf(recB))
	}
}
