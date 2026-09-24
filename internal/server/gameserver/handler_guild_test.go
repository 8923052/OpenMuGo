package gameserver

// handler_guild_test.go —— S6a 战盟：创建→GuildCreationResult 且本人入盟、成员列表→GuildList、
// 团长踢自己→解散（GuildKickResponse=disband）。用真实 guildserver，经发布器桥接回 GS 回调。

import (
	"testing"

	"mugo/internal/api"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/server/guildserver"
)

// guildPubAdapter 把战盟服变更推送桥接到 GS 的 api.GameServer 回调（更新本地 guildByCharacter）。
type guildPubAdapter struct{ gs *Server }

func (p guildPubAdapter) AssignGuildToPlayerAsync(_ uint8, name string, st api.GuildMemberStatus) error {
	return p.gs.AssignGuildToPlayerAsync(name, st)
}
func (p guildPubAdapter) GuildPlayerKickedAsync(n string) error {
	return p.gs.GuildPlayerKickedAsync(n)
}
func (p guildPubAdapter) GuildDeletedAsync(id uint32) error { return p.gs.GuildDeletedAsync(id) }
func (p guildPubAdapter) AllianceCreatedAsync(uint32, uint32) error {
	return nil
}
func (p guildPubAdapter) AllianceDisbandedAsync(uint32, uint32) error { return nil }
func (p guildPubAdapter) GuildHostilityChangedAsync(uint32, []uint32, uint32, []uint32, bool) error {
	return nil
}

func newGuildTest(t *testing.T) (*Server, *session, *packetRecorder) {
	t.Helper()
	srv, sessA, _, recA, _, _ := newMessengerPair(t)
	srv.SetGuildService(guildserver.New(guildPubAdapter{gs: srv}, nil))
	return srv, sessA, recA
}

func TestGuildCreateThenList(t *testing.T) {
	srv, sessA, recA := newGuildTest(t)
	req := c2s.NewGuildCreateRequest()
	req.SetGuildName("MyGuild")
	srv.handleGuildCreate(sessA, req.Bytes())

	f := findFrame(recA, 0xC1, 0x56)
	if f == nil {
		t.Fatal("应收到 GuildCreationResult(C1 56)")
	}
	if !s2c.AsGuildCreationResult(f).Success() {
		t.Fatal("首次创建应 Success")
	}
	if srv.guildOfCharacter("alpha") == 0 {
		t.Fatal("创建后 alpha 应有战盟归属")
	}

	srv.handleGuildList(sessA)
	lf := findFrame(recA, 0xC2, 0x52)
	if lf == nil {
		t.Fatal("应收到 GuildList(C2 52)")
	}
}

func TestGuildRoleMapper(t *testing.T) {
	cases := []struct {
		pos  api.GuildPosition
		wire byte
	}{
		{api.GuildPositionNormalMember, 0},
		{api.GuildPositionGuildMaster, 128},
		{api.GuildPositionBattleMaster, 32},
		{api.GuildPositionAssistantMaster, 64},
	}
	for _, c := range cases {
		if got := guildPositionToWireRole(c.pos); got != c.wire {
			t.Fatalf("pos %d → wire %d, want %d", c.pos, got, c.wire)
		}
		if back := wireRoleToGuildPosition(c.wire); back != c.pos {
			t.Fatalf("wire %d → pos %d, want %d", c.wire, back, c.pos)
		}
	}
}

// newGuildPair 复用信使双会话（alpha/beta 均入世界+已跟踪），接上真实战盟服。
func newGuildPair(t *testing.T) (*Server, *session, *session, *packetRecorder, *packetRecorder) {
	t.Helper()
	srv, sessA, sessB, recA, recB, _ := newMessengerPair(t)
	srv.SetGuildService(guildserver.New(guildPubAdapter{gs: srv}, nil))
	if c := sessA.getSelected(); c != nil {
		c.Level = 100
	}
	if c := sessB.getSelected(); c != nil {
		c.Level = 100
	}
	return srv, sessA, sessB, recA, recB
}

func TestGuildJoinRequestAndAccept(t *testing.T) {
	srv, sessA, sessB, recA, recB := newGuildPair(t)
	// alpha 建盟→成团长。
	cr := c2s.NewGuildCreateRequest()
	cr.SetGuildName("JoinMe")
	srv.handleGuildCreate(sessA, cr.Bytes())
	gid := srv.guildOfCharacter("alpha")
	if gid == 0 || srv.guildStatusOf("alpha").Position != api.GuildPositionGuildMaster {
		t.Fatal("前置：alpha 应为团长")
	}
	masterID := sessA.getWorldPlayer().ID

	// beta 向 alpha 请求入盟 → alpha 收 GuildJoinRequest。
	jr := c2s.NewGuildJoinRequest()
	jr.SetGuildMasterPlayerId(masterID)
	srv.handleGuildJoinRequest(sessB, jr.Bytes())
	if findFrame(recA, 0xC1, 0x50) == nil {
		t.Fatal("团长 alpha 应收到 GuildJoinRequest(C1 50)")
	}
	if sessA.pendingGuildRequesterName != "beta" {
		t.Fatalf("团长应记住申请者 beta, got %q", sessA.pendingGuildRequesterName)
	}

	// alpha 接受 → beta 收 Accepted 结果；双方收 AssignCharacterToGuild。
	ans := c2s.NewGuildJoinResponse()
	ans.SetAccepted(true)
	srv.handleGuildJoinResponse(sessA, ans.Bytes())
	f := findFrame(recB, 0xC1, 0x51)
	if f == nil {
		t.Fatal("beta 应收到 GuildJoinResponse(C1 51)")
	}
	if s2c.AsGuildJoinResponse(f).Result() != s2c.GuildJoinRequestResult_Accepted {
		t.Fatal("结果应为 Accepted")
	}
	if srv.guildOfCharacter("beta") != gid {
		t.Fatalf("beta 应已入 alpha 的战盟 %d", gid)
	}
	if countFrames(recA, 0xC2, 0x65) == 0 || countFrames(recB, 0xC2, 0x65) == 0 {
		t.Fatal("入盟后在线成员应收到 AssignCharacterToGuild(C2 65)")
	}
}

func TestGuildJoinRequestTooLowLevel(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	cr := c2s.NewGuildCreateRequest()
	cr.SetGuildName("LowLv")
	srv.handleGuildCreate(sessA, cr.Bytes())
	if c := sessB.getSelected(); c != nil {
		c.Level = 3
	}
	jr := c2s.NewGuildJoinRequest()
	jr.SetGuildMasterPlayerId(sessA.getWorldPlayer().ID)
	srv.handleGuildJoinRequest(sessB, jr.Bytes())
	f := findFrame(recB, 0xC1, 0x51)
	if f == nil || s2c.AsGuildJoinResponse(f).Result() != s2c.GuildJoinRequestResult_MinimumLevel6 {
		t.Fatal("等级<6 应收到 MinimumLevel6 结果")
	}
}

func TestGuildInfoRequest(t *testing.T) {
	srv, sessA, _, recA, _ := newGuildPair(t)
	cr := c2s.NewGuildCreateRequest()
	cr.SetGuildName("InfoMe")
	srv.handleGuildCreate(sessA, cr.Bytes())
	gid := srv.guildOfCharacter("alpha")
	req := c2s.NewGuildInfoRequest()
	req.SetGuildId(gid)
	srv.handleGuildInfo(sessA, req.Bytes())
	f := findFrame(recA, 0xC1, 0x66)
	if f == nil {
		t.Fatal("应收到 GuildInformation(C1 66)")
	}
	if s2c.AsGuildInformation(f).GuildNameString() != "InfoMe" {
		t.Fatalf("盟名不符: %q", s2c.AsGuildInformation(f).GuildNameString())
	}
}

func TestGuildKickSelfDisbands(t *testing.T) {
	srv, sessA, recA := newGuildTest(t)
	cr := c2s.NewGuildCreateRequest()
	cr.SetGuildName("DisbandMe")
	srv.handleGuildCreate(sessA, cr.Bytes())
	gid := srv.guildOfCharacter("alpha")
	if gid == 0 {
		t.Fatal("前置：应已建盟")
	}

	kick := c2s.NewGuildKickPlayerRequest(23)
	kick.SetPlayerName("alpha")
	srv.handleGuildKick(sessA, kick.Bytes())

	kf := findFrame(recA, 0xC1, 0x53)
	if kf == nil {
		t.Fatal("应收到 GuildKickResponse(C1 53)")
	}
	if kf[3] != guildKickDisband {
		t.Fatalf("团长踢自己结果应为 disband(3), got %d", kf[3])
	}
	if srv.guildOfCharacter("alpha") != 0 {
		t.Fatal("解散后 alpha 不应再有战盟归属")
	}
}
