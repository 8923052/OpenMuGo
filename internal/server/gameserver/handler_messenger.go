package gameserver

// handler_messenger.go —— P2 好友系统入站，对照 MessageHandler/Messenger/AddFriend·
// FriendAddResponse·DeleteFriend·ChangeOnlineState。GS 经 friendService 窄接口调用好友服，
// 好友服再经 FriendSystemSubscriber（social.go）把结果落到本地会话出站包。

import (
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
)

// 发信结果码（对应 s2c.LetterSendRequestResult）。
const (
	letterSendSuccess        byte = 1
	letterSendCantSendToSelf byte = 4
)

// maxLetters 好友信使最大信件数（离线玩法前的常量占位）。
const maxLetters = 100

// offlineServerByte 对应 SpecialServerIdOffline（0xFF）：FriendAdded 先标离线，在线态后续补正。
const offlineServerByte byte = 0xFF

// friendService 是 GS 入站所需的好友服窄视图（对应 friendserver.Server 的方法集）。
type friendService interface {
	FriendRequestAsync(playerName, friendName string) (bool, error)
	FriendResponseAsync(characterName, friendName string, accepted bool) error
	DeleteFriendAsync(playerName, friendName string) error
	IsFriendAsync(characterName, friendName string) bool
	SetPlayerVisibilityStateAsync(serverId uint8, characterName string, isVisible bool) error
	CreateChatRoomAsync(playerName, friendName string) error
	InviteFriendToChatRoomAsync(playerName, friendName string, roomID uint16) (bool, error)
	SendLetterAsync(sender, receiver, subject, message string, rotation, animation byte, appearance []byte) error
	ReadLetterAsync(receiver string, index uint16) (api.LetterDetail, bool)
	ListLetters(receiver string) []api.LetterHeader
	DeleteLetterAsync(receiver string, index uint16) bool
}

// SetFriendService 注入好友服句柄（装配层在好友服构造后调用）。
func (s *Server) SetFriendService(f friendService) { s.deps.friend = f }

// handleFriendAdd 处理 C1 C1（AddFriendHandler）：向好友服发起请求（好友在线则实时送达请求帧）。
func (s *Server) handleFriendAdd(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	target := c2s.AsFriendAddRequest(frame).FriendNameString()
	if target == "" || target == me.Name || s.deps.friend.IsFriendAsync(me.Name, target) {
		return
	}
	if _, err := s.deps.friend.FriendRequestAsync(me.Name, target); err != nil {
		s.deps.logger.Printf("gameserver: 好友请求失败 %s→%s: %v", me.Name, target, err)
	}
}

// handleFriendAddResponse 处理 C1 C2（FriendAddResponseHandler）：应答后双方各补一条 FriendAdded。
func (s *Server) handleFriendAddResponse(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	resp := c2s.AsFriendAddResponse(frame)
	requester := resp.FriendRequesterNameString()
	accepted := resp.Accepted()
	if err := s.deps.friend.FriendResponseAsync(me.Name, requester, accepted); err != nil {
		s.deps.logger.Printf("gameserver: 好友应答失败 %s←%s: %v", me.Name, requester, err)
		return
	}
	if !accepted {
		return
	}
	const offline = offlineServerByte
	// 自己把请求方加进列表；请求方（若在线）把自己加进列表。服务器号先离线哨兵，
	// 在线状态由好友服随后经 FriendOnlineStateChanged 流式补正。
	_ = s.viewFor(sess).SendFriendAdded(requester, offline)
	if rsess := s.sessionOfCharacter(requester); rsess != nil {
		_ = s.viewFor(rsess).SendFriendAdded(me.Name, offline)
	}
}

// handleFriendDelete 处理 C1 C3（DeleteFriendHandler）：解除关系并回自己一条 FriendDeleted。
func (s *Server) handleFriendDelete(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	target := c2s.AsFriendDelete(frame).FriendNameString()
	if target == "" {
		return
	}
	if err := s.deps.friend.DeleteFriendAsync(me.Name, target); err != nil {
		s.deps.logger.Printf("gameserver: 删除好友失败 %s↔%s: %v", me.Name, target, err)
		return
	}
	_ = s.viewFor(sess).SendFriendDeleted(target)
}

// handleSetFriendOnlineState 处理 C1 C4（ChangeOnlineStateHandler）：切换本人隐身/在线可见。
func (s *Server) handleSetFriendOnlineState(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	online := c2s.AsSetFriendOnlineState(frame).OnlineState()
	if err := s.deps.friend.SetPlayerVisibilityStateAsync(uint8(s.serverID), me.Name, online); err != nil {
		s.deps.logger.Printf("gameserver: 设置可见性失败 %s: %v", me.Name, err)
	}
}

// handleChatRoomCreate 处理 C1 CA（ChatRequestHandler）：向好友服申请建房（双方在线各收 ChatRoomConnectionInfo）。
func (s *Server) handleChatRoomCreate(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	friend := c2s.AsChatRoomCreateRequest(frame).FriendNameString()
	if friend == "" {
		return
	}
	if err := s.deps.friend.CreateChatRoomAsync(me.Name, friend); err != nil {
		s.deps.logger.Printf("gameserver: 建聊天室失败 %s↔%s: %v", me.Name, friend, err)
	}
}

// handleChatRoomInvite 处理 C1 CB（ChatRoomInvitationRequest）：邀请好友加入已有房，回邀请方 FriendInvitationResult。
func (s *Server) handleChatRoomInvite(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	req := c2s.AsChatRoomInvitationRequest(frame)
	ok, err := s.deps.friend.InviteFriendToChatRoomAsync(me.Name, req.FriendNameString(), req.RoomId())
	if err != nil {
		s.deps.logger.Printf("gameserver: 邀请进房失败 %s→%s: %v", me.Name, req.FriendNameString(), err)
	}
	_ = s.viewFor(sess).SendFriendInvitationResult(ok, req.RequestId())
}

// handleLetterSend 处理 C4 C5（LetterSendHandler）：存信（收件人在线则实时推 AddLetter），回发信人结果。
// 偏差：未扣发件邮费（原版 LetterSendPrice）。
func (s *Server) handleLetterSend(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	req := c2s.AsLetterSendRequest(frame)
	receiver, letterID := req.ReceiverString(), req.LetterId()
	if receiver == me.Name {
		_ = s.viewFor(sess).SendLetterSendResult(letterSendCantSendToSelf, letterID)
		return
	}
	if err := s.deps.friend.SendLetterAsync(me.Name, receiver, req.TitleString(), req.MessageString(), req.Rotation(), req.Animation(), me.AppearanceExt); err != nil {
		s.deps.logger.Printf("gameserver: 发信失败 %s→%s: %v", me.Name, receiver, err)
		return
	}
	_ = s.viewFor(sess).SendLetterSendResult(letterSendSuccess, letterID)
}

// handleLetterRead 处理 C1 C7（LetterReadRequest）：读信并下发 OpenLetterExtended（置已读）。
func (s *Server) handleLetterRead(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	idx := c2s.AsLetterReadRequest(frame).LetterIndex()
	detail, ok := s.deps.friend.ReadLetterAsync(me.Name, idx)
	if !ok {
		return
	}
	_ = s.viewFor(sess).SendOpenLetter(idx, detail.SenderAppearance, detail.Rotation, detail.Animation, detail.Message)
}

// handleLetterDelete 处理 C1 C8（LetterDeleteRequest）：删信并回 RemoveLetter。
func (s *Server) handleLetterDelete(sess *session, frame []byte) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	idx := c2s.AsLetterDeleteRequest(frame).LetterIndex()
	ok := s.deps.friend.DeleteLetterAsync(me.Name, idx)
	_ = s.viewFor(sess).SendRemoveLetter(ok, idx)
}

// handleLetterList 处理 C1 C9（LetterListRequest）：逐封下发 AddLetter 信头（已读/未读态）。
func (s *Server) handleLetterList(sess *session) {
	if s.deps.friend == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	view := s.viewFor(sess)
	for _, h := range s.deps.friend.ListLetters(me.Name) {
		ts := time.Unix(0, h.LetterDate).UTC().Format("2006-01-02 15:04:05")
		state := api.LetterStateUnread
		if h.ReadFlag {
			state = api.LetterStateRead
		}
		_ = view.SendAddLetter(h.Index, h.SenderName, ts, h.Subject, state)
	}
}
