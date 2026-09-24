package chatserver

import (
	"testing"
	"time"
)

// TestCreateAndRegisterRooms 房间创建与认证信息（房间号、索引递增、token 非空互异）。
func TestCreateAndRegisterRooms(t *testing.T) {
	s := New(Config{}, nil)

	r1, err := s.CreateChatRoomAsync()
	if err != nil || r1 != 1 {
		t.Fatalf("首个房间号应为 1: %v %v", r1, err)
	}
	r2, _ := s.CreateChatRoomAsync()
	if r2 != 2 {
		t.Fatalf("房间号应递增: %d", r2)
	}

	info1, ok, err := s.RegisterClientAsync(r1, "Alice")
	if err != nil || !ok {
		t.Fatalf("注册失败: %v", err)
	}
	if info1.Index != 0 {
		t.Fatalf("首个 clientIndex 应为 0: %d", info1.Index)
	}
	info2, _, _ := s.RegisterClientAsync(r1, "Bob")
	if info2.Index != 1 {
		t.Fatalf("clientIndex 应递增: %d", info2.Index)
	}
	if info1.AuthenticationToken == "" || info1.AuthenticationToken == info2.AuthenticationToken {
		t.Fatalf("token 应非空且互不相同: %q %q", info1.AuthenticationToken, info2.AuthenticationToken)
	}
}

// TestAuthenticationTokenShape 锁定 token 生成语义（doc/14 §3 P5）：
// [clientIndex, 0, 0, 0] + 偏移 2 起 2 随机字节 → 大端 dword 十进制字符串。
// clientIndex=0 时高 16 位为 0 → token 数值 < 65536。
func TestAuthenticationTokenShape(t *testing.T) {
	s := New(Config{}, nil)
	roomID, _ := s.CreateChatRoomAsync()
	info, _, err := s.RegisterClientAsync(roomID, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	var n uint64
	for _, c := range []byte(info.AuthenticationToken) {
		if c < '0' || c > '9' {
			t.Fatalf("token 必须是十进制数字串: %q", info.AuthenticationToken)
		}
		n = n*10 + uint64(c-'0')
	}
	if n >= 65536 {
		t.Fatalf("index=0 时 token 应 < 65536: %d", n)
	}
}

// TestRegisterClientRequiresRoom 房间不存在时注册报错（原版抛异常语义）。
func TestRegisterClientRequiresRoom(t *testing.T) {
	s := New(Config{}, nil)
	if _, _, err := s.RegisterClientAsync(999, "Alice"); err == nil {
		t.Fatal("房间不存在应报错")
	}
}

// TestRoomCleanupCondition 锁定清理判定：AuthenticationRequiredUntil < now
// **且** 在线人数 < 2（是与；宽限期内即使 0 人也不清）。
func TestRoomCleanupCondition(t *testing.T) {
	s := New(Config{RoomCleanUpInterval: time.Hour}, nil)
	roomID, _ := s.CreateChatRoomAsync()
	if _, _, err := s.RegisterClientAsync(roomID, "Alice"); err != nil {
		t.Fatal(err)
	}

	// 宽限期内：不清。
	if got := s.cleanupUnusedRooms(); got != 0 {
		t.Fatalf("宽限期内不应清理: %d", got)
	}
	if s.roomCount() != 1 {
		t.Fatal("房间应仍在")
	}

	// 时间旅行：宽限期已过且不足 2 人 → 清理（把过期时刻拨到过去）。
	past := now().Add(-time.Second)
	s.rooms[roomID].mu.Lock()
	s.rooms[roomID].authenticationRequiredUntil = past
	s.rooms[roomID].mu.Unlock()
	if got := s.cleanupUnusedRooms(); got != 1 {
		t.Fatalf("过期且不足 2 人应清理: %d", got)
	}
	if s.roomCount() != 0 {
		t.Fatal("房间应被清理")
	}
}

// TestRoomSurvivesWithTwoClients 人数 = 2 时即使过期也不清（原版"至少 2 人"语义）。
func TestRoomSurvivesWithTwoClients(t *testing.T) {
	s := New(Config{RoomCleanUpInterval: time.Hour}, nil)
	roomID, _ := s.CreateChatRoomAsync()
	_, _, _ = s.RegisterClientAsync(roomID, "Alice")
	_, _, _ = s.RegisterClientAsync(roomID, "Bob")

	// 宽限期已过，但房间有 2 人（原版 connectedCount；此处直接置以模拟已接入连接）。
	s.rooms[roomID].mu.Lock()
	s.rooms[roomID].connectedCount = 2
	s.rooms[roomID].mu.Unlock()

	if got := s.cleanupUnusedRooms(); got != 0 {
		t.Fatalf("2 人房间不应清理: %d", got)
	}
	if s.roomCount() != 1 {
		t.Fatal("房间应保留")
	}
}
