package chatserver

import (
	"context"
	"net"

	"mugo/internal/api"
)

// 契约层实现（原版 ChatServer : IChatServer : IManageableServer）。
// 容器两段分离：StartAsync 建服务（含清理循环），StartListeners 开端口。

// Id 对应原版 Id = SpecialServerIds.ChatServer (0x20000)。
func (s *Server) Id() int { return serverID }

// ConfigurationId 对应原版 Guid（内存态固定占位）。
func (s *Server) ConfigurationId() string { return "mugo-chat" }

// Description 对应原版 Settings.Description。
func (s *Server) Description() string { return "ChatServer" }

// Type 对应原版 ServerType。
func (s *Server) Type() api.ServerType { return api.ServerTypeChatServer }

// ServerState 返回当前状态。
func (s *Server) ServerState() api.ServerState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// MaximumConnections 对应原版 Settings.MaximumConnections。
func (s *Server) MaximumConnections() int { return s.cfg.MaxConnections }

// CurrentConnections 对应原版 _connectedClients.Count（逐连接清单待信令协议接入，暂 0）。
func (s *Server) CurrentConnections() int { return 0 }

// StartAsync 实现契约层：建服务 + 启动清理循环，**不开端口**。
func (s *Server) StartAsync(ctx context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state != api.ServerStateStopped {
		return nil // 幂等（原版 if (ServerState != Stopped) return）
	}
	s.state = api.ServerStateStarting
	s.state = api.ServerStateStarted

	go s.cleanupLoop(ctx)
	s.logger.Printf("chatserver: 服务已构建")
	return nil
}

// StartListeners 开独立端口（原版 CreateListeners + listener.Start）。已开时幂等。
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
		return nil // 幂等
	}
	if s.cfg.ListenAddr == "" {
		return nil // 无监听配置：仅房间管理可用
	}
	ln, err := netListen(s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.ln = ln
	go s.acceptLoop(ln)
	s.logger.Printf("chatserver: 监听 %s", s.cfg.ListenAddr)
	return nil
}

// ShutdownAsync 实现契约层：停监听（原版 ShutdownAsync）。
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
	s.logger.Printf("chatserver: 已停止")
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

// netListen 独立变量便于测试替换（当前直通 net.Listen）。
var netListen = func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }
