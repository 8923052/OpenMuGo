// Package duel 对应 OpenMU `src/GameServer/MessageHandler/Duel/`（6 个 .cs）。
//
// 入站：决斗频道加入/退出、决斗组包、决斗开始请求/应答、决斗结束应答。
//
// 逻辑在 gamelogic（DuelRoom / DuelRoomManager / PlayerActions/Duel/），
// 出站在 view/remote/duel（ShowDuelRequestPlugIn / DuelStatusUpdatePlugIn 等 12 个）。
package duel
