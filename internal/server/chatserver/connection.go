package chatserver

// connection.go —— S7 聊天服数据面：进房认证、发言转发、进出通知。对应原版 ChatServer/ChatClient.cs。
// 帧级用 internal/proto/chat 生成物；token 与发言正文额外做 3 字节 XOR（FC CF AB，绝对下标 %3），
// 与原版 Xor3Transformer 一致（与传输层的 SimpleModulus/Xor32 是两回事）。

import (
	"strings"
	"sync"

	chat "mugo/internal/proto/chat"
)

var chatXorKey = [3]byte{0xFC, 0xCF, 0xAB}

// chatXorRange 对 buf 的 [lo,hi) 按绝对下标 %3 做 3 字节 XOR（对称，加解密同函数）。
func chatXorRange(buf []byte, lo, hi int) {
	if hi > len(buf) {
		hi = len(buf)
	}
	for i := lo; i < hi; i++ {
		buf[i] ^= chatXorKey[i%3]
	}
}

// packetWriter 是聊天连接向对端写帧的最小接口（*transport.Conn 实现；测试可注入记录器）。
type packetWriter interface {
	Send(packet []byte) error
}

// chatClient 是一条聊天连接；认证后绑定 name/index/room。
type chatClient struct {
	conn packetWriter
	mu   sync.Mutex // 串行化 conn.Send（多路广播并发写）

	room   *chatRoom
	name   string
	index  byte
	inRoom bool
}

func (c *chatClient) send(b []byte) {
	c.mu.Lock()
	_ = c.conn.Send(b)
	c.mu.Unlock()
}

// handleChatPacket 分派一帧。code 位置：C1@2、C2@3。
func (s *Server) handleChatPacket(c *chatClient, packet []byte) {
	if len(packet) < 3 {
		return
	}
	code := packet[2]
	if packet[0] == 0xC2 {
		if len(packet) < 4 {
			return
		}
		code = packet[3]
	}
	switch code {
	case chat.AuthenticateCode:
		s.authenticate(c, packet)
	case chat.ChatMessageCode:
		s.relayMessage(c, packet)
	case chat.LeaveChatRoomCode:
		s.leaveRoom(c)
	case chat.KeepAliveCode:
		// 保活：无操作（维持连接与房间）。
	}
}

// authenticate 校验房间号 + token（解密后为十进制字符串），命中注册信息则入房并通知。
func (s *Server) authenticate(c *chatClient, packet []byte) {
	if c.inRoom {
		return
	}
	p := chat.AsAuthenticate(packet)
	roomID := p.RoomId()
	full := append([]byte(nil), packet...)
	chatXorRange(full, 6, 16)
	token := strings.TrimRight(string(full[6:16]), "\x00 ")

	s.roomMu.Lock()
	room := s.rooms[roomID]
	s.roomMu.Unlock()
	if room == nil {
		return
	}
	room.mu.Lock()
	var (
		matchedName string
		matchedIdx  byte
		found       bool
	)
	for name, info := range room.authInfos {
		if info.AuthenticationToken == token {
			matchedName, matchedIdx, found = name, info.Index, true
			break
		}
	}
	if !found {
		room.mu.Unlock()
		return
	}
	c.room, c.name, c.index, c.inRoom = room, matchedName, matchedIdx, true
	room.clients[matchedName] = c
	room.connectedCount++
	others := make([]*chatClient, 0, len(room.clients))
	connected := make([]*chatClient, 0, len(room.clients))
	for _, cl := range room.clients {
		connected = append(connected, cl)
		if cl != c {
			others = append(others, cl)
		}
	}
	room.mu.Unlock()

	// 告知其他成员"某人加入"。
	joined := chat.NewChatRoomClientJoined()
	joined.SetClientIndex(c.index)
	joined.SetName(c.name)
	jb := joined.Bytes()
	for _, cl := range others {
		cl.send(jb)
	}
	// 给自己回当前房内已连接成员清单。
	c.send(chatRoomClientsPacket(connected))
}

// relayMessage 解码发言，转发给房内其他成员（重打 3 字节 XOR）。
func (s *Server) relayMessage(c *chatClient, packet []byte) {
	if !c.inRoom {
		return
	}
	msg := chat.AsChatMessage(packet)
	n := int(msg.MessageLength())
	full := append([]byte(nil), packet...)
	chatXorRange(full, 5, 5+n)
	plain := full[5 : 5+n]

	room := c.room
	room.mu.Lock()
	others := make([]*chatClient, 0, len(room.clients))
	for _, cl := range room.clients {
		if cl != c {
			others = append(others, cl)
		}
	}
	room.mu.Unlock()

	out := chat.NewChatMessage(chat.ChatMessageRequiredSize(n))
	out.SetSenderIndex(c.index)
	out.SetMessageLength(byte(n))
	copy(out.Message(), plain)
	chatXorRange(out.Bytes(), 5, 5+n)
	ob := out.Bytes()
	for _, cl := range others {
		cl.send(ob)
	}
}

// leaveRoom 从房间移除该连接并向其他成员广播离开。
func (s *Server) leaveRoom(c *chatClient) {
	if !c.inRoom {
		return
	}
	room := c.room
	c.inRoom = false
	room.mu.Lock()
	if room.clients[c.name] == c {
		delete(room.clients, c.name)
	}
	if room.connectedCount > 0 {
		room.connectedCount--
	}
	others := make([]*chatClient, 0, len(room.clients))
	for _, cl := range room.clients {
		others = append(others, cl)
	}
	room.mu.Unlock()

	left := chat.NewChatRoomClientLeft()
	left.SetClientIndex(c.index)
	left.SetName(c.name)
	lb := left.Bytes()
	for _, cl := range others {
		cl.send(lb)
	}
}

// chatRoomClientsPacket 组一帧"房内已连接成员清单"。
func chatRoomClientsPacket(clients []*chatClient) []byte {
	p := chat.NewChatRoomClients(chat.ChatRoomClientsRequiredSize(len(clients)))
	p.SetClientCount(byte(len(clients)))
	for i, cl := range clients {
		e := p.Clients(i)
		if e == nil {
			break
		}
		e.SetIndex(cl.index)
		e.SetName(cl.name)
	}
	return p.Bytes()
}
