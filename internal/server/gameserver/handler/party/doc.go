// Package party 对应 OpenMU `src/GameServer/MessageHandler/Party/`（4 个 .cs）。
//
// 入站：踢出队员、队伍列表请求、组队请求、组队应答。
//
// 逻辑在 gamelogic（Party / PartyManager）+ gamelogic/action（PlayerActions/Party/），
// 出站在 view/remote/party（UpdatePartyListPlugIn 含 075 变体、PartyHealthViewPlugIn 等）。
package party
