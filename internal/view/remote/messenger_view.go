package remote

// messenger_view.go —— P2 好友系统出站，对应 OpenMU RemoteView/Messenger/*。
// 进图初始化用单帧 MessengerInitialization（C2 C0）；增/删/请求/在线变化用各自小帧。

import (
	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
)

func (v *PlayerView) SendMessengerInitialization(friends []action.FriendEntry, letterCount, maxLetterCount byte) error {
	p := s2c.NewMessengerInitialization(s2c.MessengerInitializationRequiredSize(len(friends)))
	p.SetLetterCount(letterCount)
	p.SetMaximumLetterCount(maxLetterCount)
	p.SetFriendCount(byte(len(friends)))
	for i, f := range friends {
		e := p.Friends(i)
		if e == nil {
			break
		}
		e.SetName(f.Name)
		e.SetServerId(f.ServerID)
	}
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendFriendAdded(friendName string, serverID byte) error {
	p := s2c.NewFriendAdded()
	p.SetFriendName(friendName)
	p.SetServerId(serverID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendFriendDeleted(friendName string) error {
	p := s2c.NewFriendDeleted()
	p.SetFriendName(friendName)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendFriendRequest(requesterName string) error {
	p := s2c.NewFriendRequest()
	p.SetRequester(requesterName)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendFriendOnlineStateUpdate(friendName string, serverID byte) error {
	p := s2c.NewFriendOnlineStateUpdate()
	p.SetFriendName(friendName)
	p.SetServerId(serverID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendChatRoomConnectionInfo(chatServerIP string, roomID uint16, authPassword uint32, clientIndex byte, friendName string, success bool) error {
	p := s2c.NewChatRoomConnectionInfo()
	p.SetChatServerIp(chatServerIP)
	p.SetChatRoomId(roomID)
	p.SetAuthenticationToken(authPassword)
	p.SetType(clientIndex)
	p.SetFriendName(friendName)
	p.SetSuccess(success)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendFriendInvitationResult(success bool, requestID uint32) error {
	p := s2c.NewFriendInvitationResult()
	p.SetSuccess(success)
	p.SetRequestId(requestID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendAddLetter(index uint16, senderName, timestamp, subject string, state byte) error {
	p := s2c.NewAddLetter()
	p.SetLetterIndex(index)
	p.SetSenderName(senderName)
	p.SetTimestamp(timestamp)
	p.SetSubject(subject)
	p.SetState(s2c.LetterState(state))
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendOpenLetter(index uint16, senderAppearance []byte, rotation, animation byte, message string) error {
	p := s2c.NewOpenLetterExtended(s2c.OpenLetterExtendedRequiredSize(len(message)))
	p.SetLetterIndex(index)
	copy(p.SenderAppearance(), senderAppearance)
	p.SetRotation(rotation)
	p.SetAnimation(animation)
	p.SetMessage(message)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendLetterSendResult(result byte, letterID uint32) error {
	p := s2c.NewLetterSendResponse()
	p.SetResult(s2c.LetterSendRequestResult(result))
	p.SetLetterId(letterID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendRemoveLetter(success bool, index uint16) error {
	p := s2c.NewRemoveLetter()
	p.SetRequestSuccessful(success)
	p.SetLetterIndex(index)
	return v.send.Send(p.Bytes())
}
