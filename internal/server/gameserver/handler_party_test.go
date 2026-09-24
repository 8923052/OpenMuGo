package gameserver

// handler_party_test.go —— S2 组队端到端：邀请 C1 40 → 应答 C1 41 → 双方收 PartyList C1 42
// → 队长踢人（2 人队触发解散，双方各收 C1 43）→ 非队长不得踢他人。

import (
	"testing"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

func newPartyPair(t *testing.T) (*Server, *session, *session, *packetRecorder, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	recA, recB := &packetRecorder{}, &packetRecorder{}
	sessA, wpA := newScopedSession(7, "alpha", 20, 20, recA)
	sessB, wpB := newScopedSession(8, "beta", 21, 20, recB)
	wpA.View, wpB.View = sessA.playerView, sessB.playerView
	srv.world.Map(0).Enter(wpA)
	srv.world.Map(0).Enter(wpB)
	sessA.setWorldPlayer(wpA)
	sessB.setWorldPlayer(wpB)
	srv.trackSession(sessA)
	srv.trackSession(sessB)
	t.Cleanup(func() { srv.untrackSession(sessA); srv.untrackSession(sessB) })
	return srv, sessA, sessB, recA, recB
}

func inviteFrame(target uint16) []byte {
	p := c2s.NewPartyInviteRequest()
	p.SetTargetPlayerId(target)
	return p.Bytes()
}

func TestPartyInviteReachesTarget(t *testing.T) {
	srv, sessA, _, _, recB := newPartyPair(t)
	srv.handlePartyInvite(sessA, inviteFrame(8))

	f := findFrame(recB, 0xC1, 0x40)
	if f == nil {
		t.Fatal("beta 应收到组队邀请帧")
	}
	if id := s2c.AsPartyRequest(f).RequesterId(); id != 7 {
		t.Fatalf("邀请帧 RequesterId=%d, want 7", id)
	}
}

func TestPartyAcceptBuildsList(t *testing.T) {
	srv, sessA, sessB, recA, recB := newPartyPair(t)
	srv.handlePartyInvite(sessA, inviteFrame(8))

	resp := c2s.NewPartyInviteResponse()
	resp.SetAccepted(true)
	srv.handlePartyResponse(sessB, resp.Bytes())

	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		f := findFrame(rec, 0xC1, 0x42)
		if f == nil {
			t.Fatalf("%s 未收到 PartyList", name)
		}
		p := s2c.AsPartyList(f)
		if p.Count() != 2 {
			t.Fatalf("%s 列表人数=%d, want 2", name, p.Count())
		}
		if p.Members(0).NameString() != "alpha" || p.Members(1).NameString() != "beta" {
			t.Fatalf("%s 成员异常: [0]=%q [1]=%q", name, p.Members(0).NameString(), p.Members(1).NameString())
		}
	}
}

func TestPartyMasterKickDisbandsPair(t *testing.T) {
	srv, sessA, sessB, recA, recB := newPartyPair(t)
	srv.handlePartyInvite(sessA, inviteFrame(8))
	resp := c2s.NewPartyInviteResponse()
	resp.SetAccepted(true)
	srv.handlePartyResponse(sessB, resp.Bytes())

	kick := c2s.NewPartyPlayerKickRequest()
	kick.SetPlayerIndex(1) // 队长 alpha 踢 beta（index 1）
	srv.handlePartyKick(sessA, kick.Bytes())

	// 2 人队移除后剩余 <2 → 解散，双方各收一帧 C1 43。
	if countFrames(recA, 0xC1, 0x43) != 1 || countFrames(recB, 0xC1, 0x43) != 1 {
		t.Fatalf("解散应双方各收 1 帧移除: A=%d B=%d", countFrames(recA, 0xC1, 0x43), countFrames(recB, 0xC1, 0x43))
	}
	if srv.parties.membersOf("alpha") != nil {
		t.Fatal("解散后 alpha 不应仍归属组队")
	}
}

func TestPartyNonMasterCannotKickOther(t *testing.T) {
	srv, sessA, sessB, _, recB := newPartyPair(t)
	srv.handlePartyInvite(sessA, inviteFrame(8))
	resp := c2s.NewPartyInviteResponse()
	resp.SetAccepted(true)
	srv.handlePartyResponse(sessB, resp.Bytes())

	// beta 是 index1（非队长），尝试踢 index0（alpha）→ 应被拒。
	kick := c2s.NewPartyPlayerKickRequest()
	kick.SetPlayerIndex(0)
	srv.handlePartyKick(sessB, kick.Bytes())

	if countFrames(recB, 0xC1, 0x43) != 0 {
		t.Fatalf("非队长踢他人应被拒，got %d 帧移除", countFrames(recB, 0xC1, 0x43))
	}
	if srv.parties.membersOf("alpha") == nil {
		t.Fatal("被拒后 alpha 仍应在队")
	}
}
