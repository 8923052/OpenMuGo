// Package muhelper 对应 OpenMU `src/GameLogic/MuHelper`（7 个 .cs）。
//
// MU Helper（挂机助手）的服务器侧状态与计费：
//
//	MuHelper / IMuHelperSettings / MuHelperConfiguration / MuHelperStatus /
//	MuHelperZenCostCalculator / MuHelperFeaturePlugIn / PartyRequestHandler。
//
// 客户端侧对应：入站 handler 在 server/gameserver/handler/muhelper
// （MuHelperGroupHandler / MuHelperSaveDataRequestHandlerPlugin /
// MuHelperStatusChangeRequestHandlerPlugIn），出站在 view/remote/muhelper
// （MuHelperConfigurationUpdatePlugIn / MuHelperSettings(+Serializer) /
// MuHelperSettingsInitializationPlugIn / MuHelperStatusUpdatePlugIn）。
//
// 注意：与 Bots/BotMuHelperSettings 有交集——bot 复用同一套挂机设置结构，
// 落位时本包保持"被 Bots 依赖"，不要反向依赖 Bots。
package muhelper
