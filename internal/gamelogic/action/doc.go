// Package action 是玩家动作层，对应 OpenMU 的 GameLogic/PlayerActions：
// 每个玩家"意图"（移动、拾取、使用、买卖、交易…）在这里实现，不 import proto，出站只经 view 接口。
//
// 现状：登录动作在 server/loginserver；本包在进图链路之后的业务动作重写时启用。
package action
