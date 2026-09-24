package api

// LoginOutcome 是登录业务结果，由实现方返回、由调用方映射到协议枚举。
//
// 它属于契约层而非实现包：gameserver 需要判断"是否成功"并映射到 s2c.LoginResult，
// 若该枚举留在 loginserver，gameserver 就不得不 import 实现包（违背 P1 的目标）。
//
// 对应原版：LoginResult 的判定散在 GameServer 的 login handler 里，
// OpenMU 服务端把"认证 + 占用"合成一次 TryLogin 调用；本项目额外区分了封禁类结果。
type LoginOutcome int

const (
	LoginOK LoginOutcome = iota
	LoginInvalidPassword
	LoginAccountAlreadyConnected
	LoginAccountBlocked
	LoginTemporaryBlocked
	LoginConnectionError
)

// String 返回可读结果名（日志用）。
func (o LoginOutcome) String() string {
	switch o {
	case LoginOK:
		return "OK"
	case LoginInvalidPassword:
		return "InvalidPassword"
	case LoginAccountAlreadyConnected:
		return "AccountAlreadyConnected"
	case LoginAccountBlocked:
		return "AccountBlocked"
	case LoginTemporaryBlocked:
		return "TemporaryBlocked"
	default:
		return "ConnectionError"
	}
}

// OnlineAccount 是"一次成功认证的结果"，只含跨服务协商所需的字段。
//
// 为什么另立一个 DTO 而不用 gamelogic/entity.Account：
// api 是契约层，**不得依赖任何领域包**（否则契约层被领域模型绑架，
// 且 gamelogic 与 api 会形成互相依赖的死结）。需要完整账号实体的调用方
// （如 GS 登录流要读角色列表）由实现包另外提供入口。
type OnlineAccount struct {
	Name string
	// ServerId 登录到的服务器号（0..0xFFFF 给游戏服）。
	ServerId int
}
