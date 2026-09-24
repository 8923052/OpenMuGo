// Package connectserver 实现 MU Online ConnectServer（明文、C1/C2）。
// 会话流程（对齐 OpenMU ConnectServer）：
//  1. TCP 接入后服务端立即发送 Hello：C1 04 00 01
//  2. 客户端请求 C1 F4 06（服务器列表）→ 服务端回 C2 F4 06
//  3. 客户端请求 C1 F4 03（选服）→ 服务端回 C1 F4 03 ConnectionInfo（GS 地址端口）
//  4. 旧季客户端分别使用 F4 02 / 5 字节 F4 03
//
// P3（doc/14）：服务器列表改为观察者驱动——GS 启动时 RegisterGameServer、停止时
// UnregisterGameServer、连接数变化时 CurrentConnectionsChanged（原版 ConnectServer
// 实现 IGameServerStateObserver）。列表按 ServerList 缓存 + 失效，序列化形态按
// **连接的客户端版本**选（Season==0 → Old/byte 计数），不再按请求 sub 码猜。
package connectserver

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sort"
	"sync"

	"mugo/internal/api"
	"mugo/internal/proto/connect"
	"mugo/internal/transport"
)

// errNotStarted 表示服务尚未 Start（先 StartAsync 再 StartListeners）。
var errNotStarted = errors.New("connectserver: 服务未启动，先 StartAsync")

// ServerEntry 是服务器列表中的一个游戏服条目及其连接信息。
// P3 起它由观察者从 api.ServerInfo + api.EndPoint 派生（原版 ServerListItem）；
// 静态注入入口（New 的 entries 参数）仍保留，供无 GS 的极简部署使用。
type ServerEntry struct {
	ServerID       uint16
	LoadPercentage byte
	GameServerIP   string
	GameServerPort uint16
	// maxConnections 供 LoadPercentage 重算（0 表示静态条目，不参与重算）。
	maxConnections int
}

// Server 是 ConnectServer。
type Server struct {
	addr     string
	maxFrame int
	logger   *log.Logger
	// clientVersion 是本 CS 服务的客户端版本（原版 ConnectServerDefinition.Client，
	// Season==0 决定列表用 Old 序列化）。零值视为 Season 0。
	season byte

	// stateMu 保护状态（P4 两段分离生命周期）。
	stateMu sync.RWMutex
	state   api.ServerState

	// listenMu 保护监听器（StartListeners 与 ShutdownAsync 并发安全的关键）。
	listenMu sync.Mutex
	ln       net.Listener

	mu      sync.Mutex
	entries map[uint16]*serverListItem // 按 ServerID 索引（原版 SortedSet + GetItem）
	cache   []byte                     // 序列化缓存（原版 ServerList.Cache）
}

// serverListItem 对应原版 ServerListItem：注册信息 + 派生的负载百分比。
type serverListItem struct {
	serverID       uint16
	ip             string
	port           uint16
	current        int
	maxConnections int
	// loadOverride 供静态条目直接指定负载（观察者注册的动态条目不设它，
	// 走 current×100/max 计算）。原版静态形态不存在，此为兼容旧装配入口。
	loadOverride byte
}

// loadPercentage 对应原版 ServerListItem.ServerLoadPercentage 的 getter：
// (byte)(CurrentConnections * 100f / MaximumConnections)。
func (i *serverListItem) loadPercentage() byte {
	if i.maxConnections > 0 {
		return byte(i.current * 100 / i.maxConnections)
	}
	return i.loadOverride
}

// New 创建 ConnectServer。entries 为静态初始列表（可空）；
// GS 的动态注册走观察者接口（Server 同时实现 api.GameServerStateObserver）。
// season 为 CS 服务的客户端版本季数（0 → 旧版列表序列化）。
func New(addr string, entries []ServerEntry, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{
		addr:     addr,
		maxFrame: transport.MaxPacketSize,
		logger:   logger,
		entries:  make(map[uint16]*serverListItem),
	}
	for _, e := range entries {
		s.entries[e.ServerID] = &serverListItem{
			serverID: e.ServerID, ip: e.GameServerIP, port: e.GameServerPort,
			loadOverride: e.LoadPercentage,
		}
	}
	return s
}

// SetSeason 设定服务的客户端版本季数（决定列表序列化形态；原版在构造时由定义决定）。
func (s *Server) SetSeason(season byte) { s.season = season }

// invalidate 使列表缓存失效（原版 ServerList.InvalidateCache）。
func (s *Server) invalidate() { s.cache = nil }

// RegisterGameServer 实现 api.GameServerStateObserver（原版 ConnectServer.RegisterGameServer）。
// 重复注册先注销再重加（原版行为），并把条目连同端点写入列表、使缓存失效。
func (s *Server) RegisterGameServer(gameServer api.ServerInfo, publicEndPoint api.EndPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Printf("connectserver: GS %d 注册 %s:%d（当前连接 %d/%d）",
		gameServer.Id, publicEndPoint.IpAddress, publicEndPoint.Port,
		gameServer.CurrentConnections, gameServer.MaximumConnections)
	s.entries[gameServer.Id] = &serverListItem{
		serverID:       gameServer.Id,
		ip:             publicEndPoint.IpAddress,
		port:           uint16(publicEndPoint.Port),
		current:        gameServer.CurrentConnections,
		maxConnections: gameServer.MaximumConnections,
	}
	s.invalidate()
}

// UnregisterGameServer 实现 api.GameServerStateObserver（原版 ConnectServer.UnregisterGameServer）。
func (s *Server) UnregisterGameServer(gameServerId uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, gameServerId)
	s.invalidate()
	s.logger.Printf("connectserver: GS %d 已注销", gameServerId)
}

// CurrentConnectionsChanged 实现 api.GameServerStateObserver（原版 ConnectServer.CurrentConnectionsChanged）。
// 原版 setter 会就地改缓存字节；Go 侧不复刻该 hack，直接使缓存失效（doc/14 P3 明示）。
func (s *Server) CurrentConnectionsChanged(serverId uint16, currentConnections int) {
	s.mu.Lock()
	if item, ok := s.entries[serverId]; ok {
		item.current = currentConnections
		s.invalidate()
	}
	s.mu.Unlock()
}

// snapshot 返回按 ServerID 排序的条目切片（原版 SortedSet 按 ServerId 排序）。
func (s *Server) snapshot() []serverListItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]serverListItem, 0, len(s.entries))
	for _, i := range s.entries {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].serverID < out[b].serverID })
	return out
}

// ListenAndServe 在配置地址上阻塞监听，直到 ctx 取消或监听器关闭。
// 便捷方法：两段分离接口（StartAsync → StartListeners → 等待 → ShutdownAsync）的串联。
func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.StartAsync(ctx); err != nil {
		return err
	}
	if err := s.StartListeners(ctx); err != nil {
		_ = s.ShutdownAsync(ctx)
		return err
	}
	defer func() { _ = s.ShutdownAsync(ctx) }()
	<-ctx.Done()
	return ctx.Err()
}

// StartAsync 实现契约层 api.ManageableServer：只建服务，不开端口（P4 两段分离）。
func (s *Server) StartAsync(_ context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state != api.ServerStateStopped {
		return nil // 幂等
	}
	s.state = api.ServerStateStarting
	s.state = api.ServerStateStarted
	return nil
}

// StartListeners 开监听端口并启动 accept 循环（P4 两段分离；已开时幂等）。
func (s *Server) StartListeners(_ context.Context) error {
	s.stateMu.RLock()
	started := s.state == api.ServerStateStarted
	s.stateMu.RUnlock()
	if !started {
		return errNotStarted
	}

	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.ln != nil {
		return nil // 幂等：监听已在（原版 "listeners are always started"）
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	go s.acceptLoop(ln)
	return nil
}

// ShutdownAsync 实现契约层 api.ManageableServer：停监听、断开全部连接（原版 ShutdownAsync）。
func (s *Server) ShutdownAsync(_ context.Context) error {
	s.listenMu.Lock()
	ln := s.ln
	s.ln = nil
	s.listenMu.Unlock()

	s.stateMu.Lock()
	s.state = api.ServerStateStopping
	s.stateMu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}

	s.stateMu.Lock()
	s.state = api.ServerStateStopped
	s.stateMu.Unlock()
	return nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(raw)
	}
}

// ServeListener 在给定监听器上接收连接（便于测试注入随机端口）。
func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.serveConn(raw)
	}
}

func (s *Server) serveConn(raw net.Conn) {
	conn := transport.NewConn(raw, s.maxFrame)
	defer conn.Close()

	// 1. 接入即发 Hello（C1 04 00 01），先于开始收包，与 OpenMU 时序一致。
	if err := conn.Send(connect.NewHello().Bytes()); err != nil {
		s.logger.Printf("connectserver: 发送 Hello 失败 %s: %v", conn.RemoteAddr(), err)
		return
	}

	err := conn.Serve(func(c *transport.Conn, packet []byte) {
		s.dispatch(c, packet)
	})
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		s.logger.Printf("connectserver: 连接异常 %s: %v", conn.RemoteAddr(), err)
	}
}

func (s *Server) dispatch(c *transport.Conn, packet []byte) {
	if len(packet) < 3 {
		return
	}
	header := packet[0]
	codeIdx := transport.CodeIndex(header)
	if len(packet) <= codeIdx {
		return
	}
	code := packet[codeIdx]
	hasSub := len(packet) > codeIdx+1
	sub := byte(0)
	if hasSub {
		sub = packet[codeIdx+1]
	}

	// ConnectServer 的功能码全部为 F4。
	if code != 0xF4 {
		// 未知包：忽略不断连，保持与 OpenMU 分组处理器一致的容错策略。
		s.logger.Printf("connectserver: 忽略未知包 code=0x%02X sub=0x%02X len=%d", code, sub, len(packet))
		return
	}

	switch sub {
	case 0x06, 0x02:
		// ServerListRequest（新/旧）：**按连接的客户端版本选序列化形态**（P3 行为修正），
		// sub 码只用于兼容两种客户端的请求习惯。
		s.sendServerList(c)
	case 0x03: // ConnectionInfoRequest：按帧长度区分新旧两种形态
		switch len(packet) {
		case connect.ConnectionInfoRequest075Length:
			req := connect.AsConnectionInfoRequest075(packet)
			s.sendConnectionInfo(c, uint16(req.ServerId()))
		case connect.ConnectionInfoRequestLength:
			req := connect.AsConnectionInfoRequest(packet)
			s.sendConnectionInfo(c, req.ServerId())
		default:
			s.logger.Printf("connectserver: 非法 ConnectionInfoRequest 长度 %d", len(packet))
		}
	default:
		s.logger.Printf("connectserver: 忽略 F4 未知 sub=0x%02X", sub)
	}
}

// sendServerList 发送服务器列表：命中缓存直接回，否则序列化并写缓存。
// Season==0 → ServerListResponseOld（ServerCount 为 byte）；否则 ServerListResponse（ushort）。
func (s *Server) sendServerList(c *transport.Conn) {
	entries := s.snapshot()
	s.mu.Lock()
	cached := s.cache
	s.mu.Unlock()
	if cached != nil {
		if err := c.Send(cached); err != nil {
			s.logger.Printf("connectserver: 发送服务器列表(缓存)失败: %v", err)
		}
		return
	}

	var packet []byte
	if s.season == 0 {
		p := connect.NewServerListResponseOld(connect.ServerListResponseOldRequiredSize(len(entries)))
		p.SetServerCount(byte(len(entries)))
		for i := range entries {
			block := p.Servers(i)
			if block == nil {
				return
			}
			block.SetServerId(byte(entries[i].serverID))
			block.SetLoadPercentage(entries[i].loadPercentage())
		}
		packet = p.Bytes()
	} else {
		p := connect.NewServerListResponse(connect.ServerListResponseRequiredSize(len(entries)))
		p.SetServerCount(uint16(len(entries)))
		for i := range entries {
			block := p.Servers(i)
			if block == nil {
				return
			}
			block.SetServerId(entries[i].serverID)
			block.SetLoadPercentage(entries[i].loadPercentage())
		}
		packet = p.Bytes()
	}

	s.mu.Lock()
	s.cache = packet
	s.mu.Unlock()
	if err := c.Send(packet); err != nil {
		s.logger.Printf("connectserver: 发送服务器列表失败: %v", err)
	}
}

func (s *Server) sendConnectionInfo(c *transport.Conn, serverID uint16) {
	s.mu.Lock()
	entry, ok := s.entries[serverID]
	var info []byte
	if ok {
		// 每连接独立包缓冲（原版 ConnectInfos 复用预生成字节；此处量小直接现生成）。
		p := connect.NewConnectionInfo()
		p.SetIpAddress(entry.ip)
		p.SetPort(entry.port)
		info = p.Bytes()
	}
	s.mu.Unlock()
	if !ok {
		s.logger.Printf("connectserver: 未找到 serverId=%d，忽略", serverID)
		return
	}
	if err := c.Send(info); err != nil {
		s.logger.Printf("connectserver: 发送 ConnectionInfo 失败: %v", err)
	}
}
