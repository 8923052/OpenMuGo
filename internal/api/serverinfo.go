package api

// ServerInfo 对应 Interfaces/ServerInfo.cs 的 record ServerInfo。
//
// 原版是 record 但 CurrentConnections 有 set —— 即"可变记录"：其他字段 init-only，
// 只有 CurrentConnections 可改。Go 侧对应"值类型 + 显式刷新方法"，
// **不要**改成纯不可变结构（观察者的 CurrentConnectionsChanged 语义就靠它可变）。
type ServerInfo struct {
	Id                 uint16
	Description        string
	CurrentConnections int
	MaximumConnections int
}

// NewServerInfo 对应原版主构造函数。
func NewServerInfo(id uint16, description string, currentConnections, maximumConnections int) ServerInfo {
	return ServerInfo{
		Id:                 id,
		Description:        description,
		CurrentConnections: currentConnections,
		MaximumConnections: maximumConnections,
	}
}

// WithCurrentConnections 返回改了连接数的副本，对应原版 `info.CurrentConnections = n`。
func (s ServerInfo) WithCurrentConnections(n int) ServerInfo {
	s.CurrentConnections = n
	return s
}
