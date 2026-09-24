// Package offline 对应 OpenMU `src/GameLogic/Offline`（13 个 .cs）。
//
// 离线挂机（玩家下线后角色仍在地图里按 MuHelper 设置行动）：
//
//	OfflinePlayer / OfflinePlayerManager / OfflinePlayerMuHelper / OfflinePartyMember /
//	OfflineViewPlugInContainer / OfflineMapChangePlugIn / OfflineRespawnPlugIn /
//	MovementHandler / CombatHandler / HealingHandler / BuffHandler /
//	ItemPickupHandler / PetHandler / RepairHandler / ZenConsumptionHandler。
//
// 关键设计：离线玩家通过 **OfflineViewPlugInContainer** 把自己的出站视图替换成"假容器"
// （丢弃或不发包），从而复用同一套 Player 逻辑而不污染真实连接。Go 侧落地时要保留这个
// "视图容器可替换"的接缝，否则离线与在线的逻辑会分叉成两份。
//
// 前置依赖：MuHelper（沿用挂机设置）、NPC、combat、storage。
package offline
