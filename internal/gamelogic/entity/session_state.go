package entity

// 会话状态机（对应 OpenMU PlayerState 登录相关跃迁）。
type SessionState byte

const (
	StateConnected     SessionState = iota // TCP 已连，等待 F1 01
	StateAuthenticated                     // 登录成功，可请求角色列表
	StateEnteringMap                       // 已选角（F3 03），等待客户端 F3 12 加载完成
	StateEnteredWorld                      // 已进图，可移动交互
)
