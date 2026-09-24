package api

// ServerState 对应 Interfaces/IGameServer.cs 顶部的 enum ServerState。
// 原版无显式值，按声明顺序从 0 开始。
type ServerState uint8

const (
	ServerStateStopped ServerState = iota
	ServerStateStarting
	ServerStateStarted
	ServerStateStopping
	ServerStateTimeout
)

// MessageType 对应 Interfaces/IGameServer.cs 的 enum MessageType。
type MessageType uint8

const (
	MessageTypeGoldenCenter MessageType = 0
	MessageTypeBlueNormal   MessageType = 1
	MessageTypeGuildNotice  MessageType = 2
)
