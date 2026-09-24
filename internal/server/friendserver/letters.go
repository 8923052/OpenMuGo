package friendserver

// letters.go —— 内存信箱（P2 信件）。对应原版 FriendServer 的信件持久化：
// SendLetter 存入收件人信箱并在其在线时经 notifier 推 AddLetter；ReadLetter 读并置已读；
// ListLetters 列信头；DeleteLetter 删除（下标随之压实）。
// 与原版偏差：不校验收件人是否存在（无全局角色库）、无信箱上限；发件邮费在 GS 侧（本处不扣钱）。

import (
	"time"

	"mugo/internal/api"
)

type storedLetter struct {
	header     api.LetterHeader
	message    string
	rotation   byte
	animation  byte
	appearance []byte
}

// SendLetterAsync 存入信箱；收件人在线则推 LetterReceived（GS 侧下发 AddLetter）。
func (s *Server) SendLetterAsync(sender, receiver, subject, message string, rotation, animation byte, appearance []byte) error {
	s.storeMu.Lock()
	idx := uint16(len(s.letters[receiver]))
	hdr := api.LetterHeader{
		SenderName: sender, ReceiverName: receiver, Subject: subject,
		LetterDate: time.Now().UnixNano(), Index: idx,
	}
	s.letters[receiver] = append(s.letters[receiver], storedLetter{
		header: hdr, message: message, rotation: rotation, animation: animation, appearance: append([]byte(nil), appearance...),
	})
	s.storeMu.Unlock()

	if s.onlineFriend(receiver) != nil {
		return s.notifier.LetterReceivedAsync(hdr)
	}
	return nil
}

// ReadLetterAsync 按信箱下标读信并置已读。
func (s *Server) ReadLetterAsync(receiver string, index uint16) (api.LetterDetail, bool) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	box := s.letters[receiver]
	if int(index) >= len(box) {
		return api.LetterDetail{}, false
	}
	box[index].header.ReadFlag = true
	l := box[index]
	return api.LetterDetail{
		LetterHeader: l.header, Message: l.message,
		Rotation: l.rotation, Animation: l.animation, SenderAppearance: l.appearance,
	}, true
}

// ListLetters 返回收件人全部信头（下标即信箱位置）。
func (s *Server) ListLetters(receiver string) []api.LetterHeader {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	box := s.letters[receiver]
	out := make([]api.LetterHeader, 0, len(box))
	for _, l := range box {
		out = append(out, l.header)
	}
	return out
}

// DeleteLetterAsync 删除指定下标的信（其后信件下标前移）。
func (s *Server) DeleteLetterAsync(receiver string, index uint16) bool {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	box := s.letters[receiver]
	if int(index) >= len(box) {
		return false
	}
	box = append(box[:index], box[index+1:]...)
	for i := range box {
		box[i].header.Index = uint16(i)
	}
	if len(box) == 0 {
		delete(s.letters, receiver)
	} else {
		s.letters[receiver] = box
	}
	return true
}
