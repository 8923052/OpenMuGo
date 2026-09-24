package gameserver

// handler_messenger_test.go —— P2 好友系统入站/出站：加好友走好友服、应答双方各收 FriendAdded、
// 删除回 FriendDeleted、进图信使初始化发 MessengerInitialization + 待处理 FriendRequest。

import (
	"testing"
	"time"

	"mugo/internal/api"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// fakeFriend 是 friendService 的测试替身（带内存信箱，不依赖真实好友服）。
type fakeFriend struct {
	requests  [][2]string
	responses [][2]string
	deletes   [][2]string
	mailbox   map[string][]api.LetterDetail
}

func (f *fakeFriend) FriendRequestAsync(p, fri string) (bool, error) {
	f.requests = append(f.requests, [2]string{p, fri})
	return true, nil
}
func (f *fakeFriend) FriendResponseAsync(c, fri string, accepted bool) error {
	r := [2]string{c, fri}
	if accepted {
		r = [2]string{c + "|y", fri}
	}
	f.responses = append(f.responses, r)
	return nil
}
func (f *fakeFriend) DeleteFriendAsync(p, fri string) error {
	f.deletes = append(f.deletes, [2]string{p, fri})
	return nil
}
func (f *fakeFriend) IsFriendAsync(string, string) bool { return false }
func (f *fakeFriend) SetPlayerVisibilityStateAsync(uint8, string, bool) error {
	return nil
}
func (f *fakeFriend) CreateChatRoomAsync(string, string) error { return nil }
func (f *fakeFriend) InviteFriendToChatRoomAsync(string, string, uint16) (bool, error) {
	return true, nil
}
func (f *fakeFriend) SendLetterAsync(sender, receiver, subject, message string, rot, anim byte, app []byte) error {
	if f.mailbox == nil {
		f.mailbox = map[string][]api.LetterDetail{}
	}
	box := f.mailbox[receiver]
	box = append(box, api.LetterDetail{
		LetterHeader: api.LetterHeader{SenderName: sender, ReceiverName: receiver, Subject: subject, LetterDate: time.Now().UnixNano(), Index: uint16(len(box))},
		Message:      message, Rotation: rot, Animation: anim, SenderAppearance: app,
	})
	f.mailbox[receiver] = box
	return nil
}
func (f *fakeFriend) ReadLetterAsync(receiver string, index uint16) (api.LetterDetail, bool) {
	box := f.mailbox[receiver]
	if int(index) >= len(box) {
		return api.LetterDetail{}, false
	}
	return box[index], true
}
func (f *fakeFriend) ListLetters(receiver string) []api.LetterHeader {
	var out []api.LetterHeader
	for _, l := range f.mailbox[receiver] {
		out = append(out, l.LetterHeader)
	}
	return out
}
func (f *fakeFriend) DeleteLetterAsync(receiver string, index uint16) bool {
	box := f.mailbox[receiver]
	if int(index) >= len(box) {
		return false
	}
	f.mailbox[receiver] = append(box[:index], box[index+1:]...)
	return true
}

func newMessengerPair(t *testing.T) (*Server, *session, *session, *packetRecorder, *packetRecorder, *fakeFriend) {
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
	fake := &fakeFriend{}
	srv.SetFriendService(fake)
	return srv, sessA, sessB, recA, recB, fake
}

func friendAddFrame(name string) []byte {
	r := c2s.NewFriendAddRequest()
	copy(r.Bytes()[3:13], name)
	return r.Bytes()
}

func friendResponseFrame(requester string, accepted bool) []byte {
	r := c2s.NewFriendAddResponse()
	if accepted {
		r.Bytes()[3] |= 1
	}
	copy(r.Bytes()[4:14], requester)
	return r.Bytes()
}

func friendDeleteFrame(name string) []byte {
	r := c2s.NewFriendDelete()
	copy(r.Bytes()[3:13], name)
	return r.Bytes()
}

func TestFriendAddCallsService(t *testing.T) {
	srv, sessA, _, _, _, fake := newMessengerPair(t)
	srv.handleFriendAdd(sessA, friendAddFrame("beta"))
	if len(fake.requests) != 1 || fake.requests[0] != [2]string{"alpha", "beta"} {
		t.Fatalf("应调用 FriendRequestAsync(alpha,beta), got %v", fake.requests)
	}
}

func TestFriendAddResponseAddsBothSides(t *testing.T) {
	srv, sessA, sessB, recA, recB, fake := newMessengerPair(t)
	_ = sessA
	srv.handleFriendAddResponse(sessB, friendResponseFrame("alpha", true))
	if len(fake.responses) != 1 || fake.responses[0] != [2]string{"beta|y", "alpha"} {
		t.Fatalf("应答未落到好友服: %v", fake.responses)
	}
	// beta（自己）收到把 alpha 加入的 FriendAdded（C1 code 0xC1）
	if countFrames(recB, 0xC1, 0xC1) == 0 {
		t.Fatal("beta 应收到 FriendAdded(alpha)")
	}
	// alpha（请求方，在线）收到把 beta 加入的 FriendAdded
	if countFrames(recA, 0xC1, 0xC1) == 0 {
		t.Fatal("alpha 应收到 FriendAdded(beta)")
	}
}

func TestFriendDeleteSendsDeleted(t *testing.T) {
	srv, sessA, _, recA, _, fake := newMessengerPair(t)
	srv.handleFriendDelete(sessA, friendDeleteFrame("beta"))
	if len(fake.deletes) != 1 || fake.deletes[0] != [2]string{"alpha", "beta"} {
		t.Fatalf("应调用 DeleteFriendAsync: %v", fake.deletes)
	}
	if countFrames(recA, 0xC1, 0xC3) == 0 {
		t.Fatal("alpha 应收到 FriendDeleted(beta)")
	}
}

func TestChatRoomInviteSendsResult(t *testing.T) {
	srv, _, sessB, _, recB, _ := newMessengerPair(t)
	req := c2s.NewChatRoomInvitationRequest()
	copy(req.Bytes()[3:13], "alpha")
	req.SetRoomId(5)
	req.SetRequestId(77)
	srv.handleChatRoomInvite(sessB, req.Bytes()) // 邀请方=sessB，结果回自己
	if findFrame(recB, 0xC3, 0xCB) == nil {
		t.Fatal("邀请方应收到 FriendInvitationResult(C3 CB)")
	}
}

func TestInitializeMessengerSendsListAndRequests(t *testing.T) {
	srv, sessA, _, recA, _, _ := newMessengerPair(t)
	_ = sessA
	if err := srv.InitializeMessengerAsync(api.MessengerInitializationData{
		PlayerName: "alpha", Friends: []string{"beta"}, OpenFriendRequests: []string{"gamma"},
	}); err != nil {
		t.Fatal(err)
	}
	if countFrames(recA, 0xC2, 0xC0) == 0 {
		t.Fatal("alpha 应收到 MessengerInitialization(C2 C0)")
	}
	if countFrames(recA, 0xC1, 0xC2) == 0 {
		t.Fatal("alpha 应收到待处理好友请求 FriendRequest(C1 C2)")
	}
}

func letterSendFrame(receiver, title, message string, id uint32) []byte {
	r := c2s.NewLetterSendRequest(c2s.LetterSendRequestRequiredSize(len(message)))
	r.SetReceiver(receiver)
	r.SetTitle(title)
	r.SetMessage(message)
	r.SetLetterId(id)
	return r.Bytes()
}

func TestLetterSendStoreAndResult(t *testing.T) {
	srv, sessA, _, recA, _, fake := newMessengerPair(t)
	srv.handleLetterSend(sessA, letterSendFrame("beta", "hi", "body", 42))
	if len(fake.mailbox["beta"]) != 1 || fake.mailbox["beta"][0].Message != "body" {
		t.Fatalf("beta 信箱应存 1 封: %v", fake.mailbox["beta"])
	}
	f := findFrame(recA, 0xC1, 0xC5)
	if f == nil {
		t.Fatal("发信人应收到 LetterSendResponse(C1 C5)")
	}
	if s2c.AsLetterSendResponse(f).Result() != s2c.LetterSendRequestResult_Success {
		t.Fatal("结果应为 Success")
	}
}

func TestLetterLifecycleListReadDelete(t *testing.T) {
	srv, sessA, sessB, _, recB, _ := newMessengerPair(t)
	srv.handleLetterSend(sessA, letterSendFrame("beta", "hi", "body", 1))

	srv.handleLetterList(sessB)
	if countFrames(recB, 0xC3, 0xC6) == 0 {
		t.Fatal("beta 列信应收到 AddLetter(C3 C6)")
	}

	read := c2s.NewLetterReadRequest()
	read.SetLetterIndex(0)
	srv.handleLetterRead(sessB, read.Bytes())
	if findFrame(recB, 0xC4, 0xC7) == nil {
		t.Fatal("beta 读信应收到 OpenLetterExtended(C4 C7)")
	}

	del := c2s.NewLetterDeleteRequest()
	del.SetLetterIndex(0)
	srv.handleLetterDelete(sessB, del.Bytes())
	if findFrame(recB, 0xC1, 0xC8) == nil {
		t.Fatal("beta 删信应收到 RemoveLetter(C1 C8)")
	}
}

func TestLetterReceivedNotification(t *testing.T) {
	srv, _, _, _, recB, _ := newMessengerPair(t)
	if err := srv.LetterReceivedAsync(api.LetterHeader{
		SenderName: "alpha", ReceiverName: "beta", Subject: "yo", Index: 3, LetterDate: time.Now().UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	f := findFrame(recB, 0xC3, 0xC6)
	if f == nil {
		t.Fatal("在线收件人应实时收到 AddLetter")
	}
	if s2c.AsAddLetter(f).LetterIndex() != 3 {
		t.Fatal("AddLetter 下标应为 3")
	}
}
