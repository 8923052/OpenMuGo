package chatserver

// connection_test.go —— S7 聊天服数据面单测（直接驱动 handleChatPacket，注入记录器连接）：
// 认证入房→他人收 ClientJoined、自己收 Clients 清单；发言→转发他人不转发自己；离开→他人收 ClientLeft。

import (
	"sync"
	"testing"

	chat "mugo/internal/proto/chat"
)

type recorderConn struct {
	mu     sync.Mutex
	frames [][]byte
}

func (r *recorderConn) Send(p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, append([]byte(nil), p...))
	return nil
}

func chatCode(f []byte) byte {
	if f[0] == 0xC2 {
		return f[3]
	}
	return f[2]
}

func (r *recorderConn) count(code, sub byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, f := range r.frames {
		if chatCode(f) == code && (code != chat.ChatRoomClientJoinedCode || f[3] == sub) {
			n++
		}
	}
	return n
}

func authFrame(roomID uint16, token string) []byte {
	a := chat.NewAuthenticate()
	a.SetRoomId(roomID)
	copy(a.Bytes()[6:16], token)
	chatXorRange(a.Bytes(), 6, 16) // 打成 FC CF AB 密文
	return a.Bytes()
}

func msgFrame(index byte, text string) []byte {
	m := chat.NewChatMessage(chat.ChatMessageRequiredSize(len(text)))
	m.SetSenderIndex(index)
	m.SetMessageLength(byte(len(text)))
	copy(m.Message(), text)
	chatXorRange(m.Bytes(), 5, 5+len(text))
	return m.Bytes()
}

func TestChatDataPlaneJoinMessageLeave(t *testing.T) {
	s := New(Config{}, nil)
	roomID, _ := s.CreateChatRoomAsync()
	infoA, _, _ := s.RegisterClientAsync(roomID, "alice")
	infoB, _, _ := s.RegisterClientAsync(roomID, "bob")

	recA, recB := &recorderConn{}, &recorderConn{}
	cA := &chatClient{conn: recA}
	cB := &chatClient{conn: recB}

	// alice 先入房：只有自己，收 Clients 清单，无 Joined 广播。
	s.handleChatPacket(cA, authFrame(roomID, infoA.AuthenticationToken))
	if !cA.inRoom || cA.index != infoA.Index {
		t.Fatalf("alice 未正确入房 in=%v idx=%d want %d", cA.inRoom, cA.index, infoA.Index)
	}
	if recA.count(chat.ChatRoomClientsCode, 0) == 0 {
		t.Fatal("alice 应收到 ChatRoomClients 清单")
	}

	// bob 入房：alice 应收到 ChatRoomClientJoined(bob)。
	s.handleChatPacket(cB, authFrame(roomID, infoB.AuthenticationToken))
	if recA.count(chat.ChatRoomClientJoinedCode, chat.ChatRoomClientJoinedSubCode) == 0 {
		t.Fatal("alice 应收到 bob 的 ChatRoomClientJoined")
	}

	// alice 发言：转发给 bob，不转发自己。
	s.handleChatPacket(cA, msgFrame(cA.index, "hi bob"))
	if recB.count(chat.ChatMessageCode, 0) == 0 {
		t.Fatal("bob 应收到转发的 ChatMessage")
	}
	if recA.count(chat.ChatMessageCode, 0) != 0 {
		t.Fatal("alice 不应收到自己发言的回声")
	}

	// alice 离开：bob 收到 ChatRoomClientLeft。
	s.handleChatPacket(cA, func() []byte { l := chat.NewLeaveChatRoom(); return l.Bytes() }())
	if recB.count(chat.ChatRoomClientLeftCode, chat.ChatRoomClientLeftSubCode) == 0 {
		t.Fatal("bob 应收到 alice 的 ChatRoomClientLeft")
	}
}

func TestChatAuthenticateBadTokenIgnored(t *testing.T) {
	s := New(Config{}, nil)
	roomID, _ := s.CreateChatRoomAsync()
	_, _, _ = s.RegisterClientAsync(roomID, "alice")
	c := &chatClient{conn: &recorderConn{}}
	s.handleChatPacket(c, authFrame(roomID, "99999999"))
	if c.inRoom {
		t.Fatal("错误 token 不应入房")
	}
}
