package api

// ServerType 对应 Interfaces/ServerType.cs 的 enum ServerType。
// 取值顺序与原版一致，不可重排（序列化与比较可能依赖）。
type ServerType uint8

const (
	ServerTypeUndefined     ServerType = 0
	ServerTypeGameServer    ServerType = 1
	ServerTypeConnectServer ServerType = 2
	ServerTypeChatServer    ServerType = 3
)

// String 返回原版枚举名。
func (t ServerType) String() string {
	switch t {
	case ServerTypeGameServer:
		return "GameServer"
	case ServerTypeConnectServer:
		return "ConnectServer"
	case ServerTypeChatServer:
		return "ChatServer"
	default:
		return "Undefined"
	}
}
