// Package minigames 对应 OpenMU `src/GameLogic/MiniGames`（约 30 个 .cs）。
//
// 四个活动各自一套 Context + 状态机，另有共用基类：
//
//	MiniGameContext / MiniGameState / MiniGameMapKey                    共用基类
//	BloodCastleContext / BloodCastleItemExtensions                     血色城堡
//	ChaosCastleContext / ChaosCastleDropGenerator / ChaosCastleStatus  恶魔广场
//	DevilSquareContext                                                 失落之塔
//	Kanturu/{KanturuContext, KanturuEventDefinition, KanturuPhaseDefinition,
//	         KanturuPhaseKind, KanturuState, KanturuTerrainArea,
//	         KanturuTransitionDefinition, KanturuMayaDetailState,
//	         KanturuTowerDetailState, KanturuNightmareDefinition|DetailState|HpPhase}
//
// 以及给 view 用的接口（对应原版 GameLogic/Views 的同名 I*PlugIn）：
//
//	IBloodCastleStateViewPlugin / IBloodCastleScoreTableViewPlugin /
//	IChaosCastleStateViewPlugin / IChangeTerrainAttributesViewPlugin /
//	IMiniGameScoreTableViewPlugin / IShowMiniGameEnterResultPlugin /
//	IShowMiniGameOpeningStatePlugIn / IUpdateMiniGameStateViewPlugIn /
//	Kanturu/IKanturuEventViewPlugIn
//
// 活动进度需要定时器与地图事件状态（IEventStateProvider），是三处同域的一部分：
// 入站 handler 在 server/gameserver/handler/minigames，出站在 view/remote/minigames。
package minigames
