// Package bots 对应 OpenMU `src/GameLogic/Bots`（23 个 .cs）。
//
// 用 BotPlayer 冒充真人玩家填充服务器：BotGenerator / BotManager / BotPlayer /
// BotNavigator / BotSkillHandler / BotPartyHandler / BotBuffHandler / BotJewelHandler /
// BotMasterHandler / BotResetHandler / BotWingHandler / BotEquipmentHandler /
// BotShoppingHandler / BotMiniGameHandler / BotProgression / BotPvpRules /
// BotNameGenerator / BotStartupProfile / BotStarterGearEquipper / BotServerPartition /
// BotConfiguration / BotMuHelperSettings / PendingPartyInvite，以及
// BotFeaturePlugIn / BotRevengePlugIn / BotSelfDefensePlugIn / BotSkillProgressionPlugIn。
//
// 前置依赖：NPC（怪物/掉落）、pathfinding（BotNavigator 走位）、MuHelper（BotMuHelperSettings）。
//
// 属"非主线子系统"，按 doc/10 §1 可延后；但目录先占位，避免将来无处落位。
package bots
