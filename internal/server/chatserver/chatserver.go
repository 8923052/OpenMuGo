// Package chatserver 实现聊天室服务（doc/14 P5.3，对应原版 ChatServer/ChatServer.cs
// + ChatRoomManager.cs + ChatRoom.cs + ChatClient.cs）。
//
// 必须原样保留的语义（doc/14 §3 P5 明示，客户端按字节解析）：
//   - 认证 token = [clientIndex, 0, 0, 0] + 偏移 2 起的 2 个随机字节，
//     以大端 dword 的**十进制字符串**表述——**不是 UUID**；
//   - 房间清理判定：AuthenticationRequiredUntil < now **且** 在线人数 < 2（是与，且是 2 不是 1）；
//   - Id = SpecialServerIds.ChatServer（0x20000，高位区间）。
//
// 网络层：独立端口 + 加密管线（原版 ConfigurableNetworkEncryptionPlugIn；Go 侧复用 S6E3 codec）。
// 网络：独立端口监听（见 manageable.go）；每连接的收发帧处理与房间广播见 connection.go（S7）。
// 封包统一用生成物 internal/proto/chat（由 protocol.gen.json 的 chat 目标从 ChatServerPackets.xml 生成），
// token 与发言正文另做 3 字节 XOR（FC CF AB）。
package chatserver

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"mugo/internal/api"
	"mugo/internal/transport"
	"mugo/internal/transport/crypto"
)

// 身份常量。
const (
	// serverID 对应原版 Id = SpecialServerIds.ChatServer。
	serverID = api.SpecialServerIdChatServer
	// authGracePeriod 对应原版构造：AuthenticationRequiredUntil = Now + 30s。
	authGracePeriod = 30 * time.Second
)

// errNotStarted 表示服务未启动。
var errNotStarted = errors.New("chatserver: 服务未启动，先 StartAsync")

// now 是时钟钩子（测试用），默认 time.Now。
var now = time.Now

// Config 是聊天服配置（对应原版 ChatServerSettings 的相关字段）。
type Config struct {
	// ListenAddr 监听地址（空 = 不开网络监听，仅房间管理可用——联调前的部署形态）。
	ListenAddr string
	// MaxConnections 最大连接数（原版 MaximumConnections）。
	MaxConnections int
	// RoomCleanUpInterval 房间清理周期（原版 RoomCleanUpInterval）。
	RoomCleanUpInterval time.Duration
}

// chatRoom 对应原版 ChatRoom。
type chatRoom struct {
	id uint16

	mu             sync.Mutex
	nextIndex      uint8
	authInfos      map[string]api.ChatServerAuthenticationInfo // clientName → 认证信息
	clients        map[string]*chatClient                      // clientName → 已连接会话（S7 转发用）
	connectedCount int
	// authenticationRequiredUntil 房间"必须完成认证"的截止时刻（原版同名字段）。
	authenticationRequiredUntil time.Time
}

// nextClientIndex 对应原版 ChatRoom.GetNextClientIndex（byte 回绕）。
func (r *chatRoom) nextClientIndex() uint8 {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := r.nextIndex
	r.nextIndex++
	return idx
}

// registerClient 对应原版 ChatRoom.RegisterClient。
func (r *chatRoom) registerClient(info api.ChatServerAuthenticationInfo) {
	r.mu.Lock()
	r.authInfos[info.ClientName] = info
	r.mu.Unlock()
}

// chatServerID 实现 api.ManageableServer 等（见 manageable.go）。

// Server 是聊天室服务。
type Server struct {
	cfg    Config
	logger *log.Logger

	stateMu sync.RWMutex
	state   api.ServerState

	listenMu sync.Mutex
	ln       net.Listener

	roomMu   sync.Mutex
	rooms    map[uint16]*chatRoom
	nextRoom uint16

	clientMu sync.Mutex
}

// New 创建聊天服。
func New(cfg Config, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.RoomCleanUpInterval <= 0 {
		cfg.RoomCleanUpInterval = time.Minute
	}
	return &Server{
		cfg:      cfg,
		logger:   logger,
		state:    api.ServerStateStopped,
		rooms:    make(map[uint16]*chatRoom),
		nextRoom: 1,
	}
}

// CreateChatRoomAsync 对应原版 ChatRoomManager.CreateChatRoom：分配房间号。
func (s *Server) CreateChatRoomAsync() (uint16, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	id := s.nextRoom
	s.nextRoom++
	s.rooms[id] = &chatRoom{
		id:                          id,
		authInfos:                   make(map[string]api.ChatServerAuthenticationInfo),
		clients:                     make(map[string]*chatClient),
		authenticationRequiredUntil: now().Add(authGracePeriod),
	}
	return id, nil
}

// RegisterClientAsync 对应原版：房间必须存在；生成认证信息（含 4+2 字节 token）。
func (s *Server) RegisterClientAsync(roomID uint16, clientName string) (api.ChatServerAuthenticationInfo, bool, error) {
	s.roomMu.Lock()
	room, ok := s.rooms[roomID]
	s.roomMu.Unlock()
	if !ok {
		return api.ChatServerAuthenticationInfo{}, false, fmt.Errorf("chatserver: 房间 %d 不存在", roomID)
	}

	index := room.nextClientIndex()
	info := api.ChatServerAuthenticationInfo{
		Index:                       index,
		RoomId:                      roomID,
		ClientName:                  clientName,
		HostAddress:                 s.hostAddress(),
		AuthenticationToken:         randomAuthenticationToken(index),
		AuthenticationRequiredUntil: now().Add(authGracePeriod).UnixNano(),
	}
	room.registerClient(info)
	return info, true, nil
}

// hostAddress 返回对外地址（原版 IIpAddressResolver.ResolveIPv4Async）。
func (s *Server) hostAddress() string {
	if s.cfg.ListenAddr == "" {
		return "127.0.0.1"
	}
	host, _, err := net.SplitHostPort(s.cfg.ListenAddr)
	if err != nil || host == "" {
		return "127.0.0.1"
	}
	return host
}

// randomAuthenticationToken 复刻原版 GetRandomAuthenticationToken（字节级）：
// [clientIndex, 0, 0, 0] + 偏移 2 起的 2 个随机字节 → 大端 dword 的十进制字符串。
func randomAuthenticationToken(clientIndex uint8) string {
	token := []byte{clientIndex, 0, 0, 0}
	if _, err := rand.Read(token[2:4]); err != nil {
		// 随机源失败属致命环境问题；原版 RandomNumberGenerator 亦无降级路径。
		panic(fmt.Sprintf("chatserver: 随机数生成失败: %v", err))
	}
	return fmt.Sprintf("%d", uint32(token[0])<<24|uint32(token[1])<<16|uint32(token[2])<<8|uint32(token[3]))
}

// cleanupUnusedRooms 对应原版 ClientCleanupUnusedRooms：
// AuthenticationRequiredUntil < now **且** ConnectedClients.Count < 2 → 关房。
// 两个条件是与，且用"至少 2 人"——不能简化成 1。
func (s *Server) cleanupUnusedRooms() int {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	closed := 0
	current := now()
	for id, room := range s.rooms {
		room.mu.Lock()
		expired := room.authenticationRequiredUntil.Before(current)
		few := room.connectedCount < 2
		room.mu.Unlock()
		if expired && few {
			delete(s.rooms, id)
			closed++
			s.logger.Printf("chatserver: 清理房间 %d", id)
		}
	}
	return closed
}

// roomCount 返回当前房间数（测试与运维用）。
func (s *Server) roomCount() int {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	return len(s.rooms)
}

// serveConn 处理一条聊天连接：等待 Authenticate，之后按房间转发发言/进出通知。
func (s *Server) serveConn(raw net.Conn) {
	// 每连接独立 codec（SimpleModulus 计数器不能共享）。
	conn := transport.NewSecureConn(raw, transport.MaxPacketSize, crypto.NewS6E3ServerCodec())
	defer conn.Close()

	client := &chatClient{conn: conn}
	if err := conn.Serve(func(_ *transport.Conn, packet []byte) {
		s.handleChatPacket(client, packet)
	}); err != nil {
		s.logger.Printf("chatserver: 连接异常 %s: %v", conn.RemoteAddr(), err)
	}
	// 连接断开→若已进房则离开（通知房其他成员）。
	s.leaveRoom(client)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// cleanupLoop 周期清理（原版 CleanupTimers）。
func (s *Server) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.RoomCleanUpInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupUnusedRooms()
		}
	}
}
