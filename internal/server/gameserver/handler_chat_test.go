package gameserver

// handler_chat_test.go —— S1 聊天端到端：公共 C1 00 广播给视野内、私聊 C1 02 定向接收者、
// 私聊目标离线时回提示给发送者。字节断言走 s2c.AsChatMessage。

import (
	"testing"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

func publicChatFrame(name, msg string) []byte {
	p := c2s.NewPublicChatMessage(c2s.PublicChatMessageRequiredSize(len(msg)))
	p.SetCharacter(name)
	p.SetMessage(msg)
	return p.Bytes()
}

func whisperFrame(target, msg string) []byte {
	p := c2s.NewWhisperMessage(c2s.WhisperMessageRequiredSize(len(msg)))
	p.SetReceiverName(target)
	p.SetMessage(msg)
	return p.Bytes()
}

func TestPublicChatBroadcastsInScope(t *testing.T) {
	srv := newScopeTestSrv(t)
	recA, recB := &packetRecorder{}, &packetRecorder{}
	sessA, wpA := newScopedSession(7, "alpha", 20, 20, recA)
	sessB, wpB := newScopedSession(8, "beta", 21, 20, recB)
	wpA.View, wpB.View = sessA.playerView, sessB.playerView
	srv.world.Map(0).Enter(wpA)
	srv.world.Map(0).Enter(wpB)
	sessA.setWorldPlayer(wpA)
	sessB.setWorldPlayer(wpB)

	srv.handlePublicChat(sessA, publicChatFrame("alpha", "hello world"))

	// 视野内两人（含发送者自己，sendToSelf:true）各收一帧 C1 00 Normal。
	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		f := findFrame(rec, 0xC1, 0x00)
		if f == nil {
			t.Fatalf("%s 未收到公共聊天帧", name)
		}
		p := s2c.AsChatMessage(f)
		if p.Type() != s2c.ChatMessageType_Normal {
			t.Fatalf("%s 频道=%d, want Normal", name, p.Type())
		}
		if p.SenderString() != "alpha" || p.MessageString() != "hello world" {
			t.Fatalf("%s 内容异常: sender=%q msg=%q", name, p.SenderString(), p.MessageString())
		}
	}
}

func TestWhisperRoutesToReceiverOnly(t *testing.T) {
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
	defer srv.untrackSession(sessA)
	defer srv.untrackSession(sessB)

	srv.handleWhisper(sessA, whisperFrame("beta", "psst"))

	if countFrames(recB, 0xC1, 0x02) != 1 {
		t.Fatalf("beta 应收 1 帧私聊, got %d", countFrames(recB, 0xC1, 0x02))
	}
	p := s2c.AsChatMessage(findFrame(recB, 0xC1, 0x02))
	if p.Type() != s2c.ChatMessageType_Whisper || p.SenderString() != "alpha" || p.MessageString() != "psst" {
		t.Fatalf("私聊帧异常: type=%d sender=%q msg=%q", p.Type(), p.SenderString(), p.MessageString())
	}
	if countFrames(recA, 0xC1, 0x02) != 0 {
		t.Fatalf("alpha 不应收到自己发出的私聊回显（对照 OpenMU 只发接收者）")
	}
}

// 原版 ChatMessageWhisperProcessor 只在收件人在图时转发，找不到人**不发任何包**
// （Go 侧曾自创一句中文 C1 02 回执，属偏离，已按原版收口）。
func TestWhisperUnknownTargetIsSilent(t *testing.T) {
	srv := newScopeTestSrv(t)
	recA := &packetRecorder{}
	sessA, wpA := newScopedSession(7, "alpha", 20, 20, recA)
	wpA.View = sessA.playerView
	srv.world.Map(0).Enter(wpA)
	sessA.setWorldPlayer(wpA)
	srv.trackSession(sessA)
	defer srv.untrackSession(sessA)

	srv.handleWhisper(sessA, whisperFrame("nobody", "hi"))

	if n := len(recA.frames); n != 0 {
		t.Fatalf("收件人不在图应静默丢弃（原版无 else 分支）, got %d 帧", n)
	}
}
