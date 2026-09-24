// Package messenger 对应 OpenMU `src/GameServer/MessageHandler/Messenger/`（9 个 .cs）。
//
// 入站：加好友、在线状态变更、聊天请求、聊天室邀请、删好友、好友加入应答、
// 信件删除、信件读取、信件发送。
//
// 逻辑在 gamelogic/action（PlayerActions/Messenger/），
// 好友在线状态的跨服部分原版在独立的 FriendServer 项目（Go 侧服务边界见 doc/12）；
// 出站在 view/remote/messenger。
package messenger
