package connectserver

import "mugo/internal/api"

// 契约层实现（P4：接入容器所需，原版 ConnectServer : IConnectServer : IManageableServer）。

// Id 对应原版 ConnectServer.Id = Settings.ServerId ?? SpecialServerIds.ConnectServer。
func (s *Server) Id() int { return api.SpecialServerIdConnectServer }

// ConfigurationId 对应原版 Guid（内存态无持久配置，固定占位）。
func (s *Server) ConfigurationId() string { return "mugo-cs" }

// Description 对应原版 Settings.Description。
func (s *Server) Description() string { return "ConnectServer" }

// Type 对应原版 ServerType。
func (s *Server) Type() api.ServerType { return api.ServerTypeConnectServer }

// ServerState 返回当前状态。
func (s *Server) ServerState() api.ServerState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// MaximumConnections 对应原版 Settings.MaxConnections。
func (s *Server) MaximumConnections() int { return 0 }

// CurrentConnections 当前接入的客户端连接数（原版 ConnectedClients.Count；
// M6 未维护逐连接清单，返回 0 占位——CS 连接是短命的"查完即走"）。
func (s *Server) CurrentConnections() int { return 0 }
