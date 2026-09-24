package friendserver

import (
	"testing"

	"mugo/internal/api"
)

// recorder 记录全部推送（替代真 GS，验证好友服的对外行为与哨兵值）。
type recorder struct {
	events []string
}

func (r *recorder) FriendRequestAsync(requester, receiver string, serverId int) error {
	r.events = append(r.events, "request:"+requester+"->"+receiver)
	return nil
}
func (r *recorder) LetterReceivedAsync(letter api.LetterHeader) error {
	r.events = append(r.events, "letter:"+letter.SenderName+"->"+letter.ReceiverName)
	return nil
}
func (r *recorder) FriendOnlineStateChangedAsync(playerServerId int, player, friend string, friendServerId int) error {
	r.events = append(r.events, "state:"+player+" sees "+friend+"@"+serverLabel(friendServerId))
	return nil
}
func (r *recorder) ChatRoomCreatedAsync(serverId int, info api.ChatServerAuthenticationInfo, friendName string) error {
	r.events = append(r.events, "room:"+info.ClientName+"#"+serverLabel(serverId))
	return nil
}
func (r *recorder) InitializeMessengerAsync(serverId int, data api.MessengerInitializationData) error {
	r.events = append(r.events, "messenger-init:"+data.PlayerName)
	return nil
}

// fakeChat 是聊天服假实现（验证好友服的协调行为）。
type fakeChat struct{ nextRoom uint16 }

func (f *fakeChat) CreateChatRoomAsync() (uint16, error) {
	f.nextRoom++
	return f.nextRoom, nil
}
func (f *fakeChat) RegisterClientAsync(roomId uint16, clientName string) (api.ChatServerAuthenticationInfo, bool, error) {
	return api.ChatServerAuthenticationInfo{RoomId: roomId, ClientName: clientName, Index: 1}, true, nil
}

func serverLabel(id int) string {
	switch id {
	case 0xFF:
		return "OFFLINE"
	case 0xFE:
		return "INVISIBLE"
	}
	return "gs" + itoa(id)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func newWired(t *testing.T) (*Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := New(rec, &fakeChat{}, nil)
	return s, rec
}

// TestFriendRequestAndResponse 全链路：请求（在线实时推送）→ 接受（互相同步状态）。
func TestFriendRequestAndResponse(t *testing.T) {
	s, rec := newWired(t)

	// 双方上线（各自在不同 GS）。
	if err := s.PlayerEnteredGameAsync(0, "cid-alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.PlayerEnteredGameAsync(1, "cid-bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	// Alice → Bob 好友请求：Bob 在线，应实时推送。
	isNew, err := s.FriendRequestAsync("Alice", "Bob")
	if err != nil || !isNew {
		t.Fatalf("首次请求应新建: isNew=%v err=%v", isNew, err)
	}
	if last := rec.events[len(rec.events)-1]; last != "request:Alice->Bob" {
		t.Fatalf("Bob 在线应收到实时请求: %v", rec.events)
	}

	// Bob 接受：双方互相同步在线状态（Alice 在 gs0，Bob 在 gs1）。
	rec.events = nil
	if err := s.FriendResponseAsync("Bob", "Alice", true); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range rec.events {
		joined += e + ";"
	}
	// Alice 的 GS 应看到 Bob@gs1；Bob 的 GS 应看到 Alice@gs0。
	if !contains(joined, "state:Alice sees Bob@gs1") || !contains(joined, "state:Bob sees Alice@gs0") {
		t.Fatalf("接受后应互相同步状态: %s", joined)
	}

	// 关系成立。
	if !s.IsFriendAsync("Alice", "Bob") || !s.IsFriendAsync("Bob", "Alice") {
		t.Fatal("双向都应是好友")
	}
}

// TestOfflineSentinelIsFF 锁定哨兵语义：下线通知的 serverId 是 **0xFF**，不是 -1/0。
func TestOfflineSentinelIsFF(t *testing.T) {
	s, rec := newWired(t)
	if err := s.PlayerEnteredGameAsync(0, "cid-a", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.PlayerEnteredGameAsync(1, "cid-b", "Bob"); err != nil {
		t.Fatal(err)
	}
	// 结为好友（离线状态下请求 + 上线后接受，覆盖两条路径）。
	if _, err := s.FriendRequestAsync("Alice", "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.FriendResponseAsync("Bob", "Alice", true); err != nil {
		t.Fatal(err)
	}

	rec.events = nil
	if err := s.PlayerLeftGameAsync("cid-b", "Bob"); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range rec.events {
		joined += e + ";"
	}
	if !contains(joined, "Bob@OFFLINE") {
		t.Fatalf("下线应以 0xFF 哨兵广播: %s", joined)
	}
}

// TestMessengerInitializationData 锁定信使初始化：好友名单 + 未处理请求方。
func TestMessengerInitializationData(t *testing.T) {
	s, rec := newWired(t)
	// 预置关系：Bob→Alice accepted；Charlie→Bob requestOpen。
	isNew, _ := s.FriendRequestAsync("Bob", "Alice")
	if !isNew {
		t.Fatal("请求应新建")
	}
	if err := s.FriendResponseAsync("Alice", "Bob", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FriendRequestAsync("Charlie", "Bob"); err != nil {
		t.Fatal(err)
	}

	rec.events = nil
	_ = s.PlayerEnteredGameAsync(2, "cid-bob", "Bob")
	if last := rec.events[len(rec.events)-1]; last != "messenger-init:Bob" {
		t.Fatalf("进图应触发信使初始化: %v", rec.events)
	}
}

// TestChatRoomCoordination 验收（建聊天室）：好友双方在线 → 建房 + 双方分别注册 + 通知各自 GS。
func TestChatRoomCoordination(t *testing.T) {
	s, rec := newWired(t)
	_ = s.PlayerEnteredGameAsync(0, "cid-a", "Alice")
	_ = s.PlayerEnteredGameAsync(1, "cid-b", "Bob")
	if _, err := s.FriendRequestAsync("Alice", "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.FriendResponseAsync("Bob", "Alice", true); err != nil {
		t.Fatal(err)
	}

	rec.events = nil
	if err := s.CreateChatRoomAsync("Alice", "Bob"); err != nil {
		t.Fatal(err)
	}
	rooms := 0
	for _, e := range rec.events {
		if len(e) > 5 && e[:5] == "room:" {
			rooms++
		}
	}
	if rooms != 2 {
		t.Fatalf("应通知双方所在 GS 各一次: %v", rec.events)
	}
}

// TestInvisibleShowsAsOffline 锁定：隐身（0xFE）对好友显示为离线（0xFF）。
func TestInvisibleShowsAsOffline(t *testing.T) {
	s, rec := newWired(t)
	_ = s.PlayerEnteredGameAsync(0, "cid-a", "Alice")
	_ = s.PlayerEnteredGameAsync(1, "cid-b", "Bob")
	if _, err := s.FriendRequestAsync("Alice", "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.FriendResponseAsync("Bob", "Alice", true); err != nil {
		t.Fatal(err)
	}

	rec.events = nil
	if err := s.SetPlayerVisibilityStateAsync(1, "Bob", false); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range rec.events {
		joined += e + ";"
	}
	if !contains(joined, "Bob@OFFLINE") {
		t.Fatalf("隐身应显示为离线(0xFF): %s", joined)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
