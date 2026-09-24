// Package gamelogic 是 MU 的服务端业务逻辑，对应 OpenMU `src/GameLogic`（约 600 个 .cs）。
//
// 域子包与原版目录一一对应：
//
//	action       PlayerActions/    玩家动作（登录/移动/战斗/交易/任务/合成…）
//	bots         Bots/             机器人玩家（可延后）
//	castlesiege  CastleSiege/      战盟攻城（含 Actions/ NPC/ Intelligence/ 三层）
//	combat       （原版无独立目录，伤害公式散在 GameLogic/ 根）
//	config       （对应 DataModel/Configuration 的导出配置装载）
//	entity       （原版无独立目录，实体散在 GameLogic/ 根与 DataModel/Entities）
//	guildwar     GuildWar/         战盟对战
//	minigames    MiniGames/        血色城堡/恶魔广场/失落之塔/Kanturu
//	muhelper     MuHelper/         挂机助手
//	npc          NPC/              怪物/NPC 与 AI
//	offline      Offline/          离线挂机
//	pet          Pet/              宠物
//	player       玩家属性推导与成长（原版散在 GameLogic/Attributes 与 Player* 文件）
//	storage      （原版无独立目录，背包/仓库/交易窗口散在 GameLogic/ 根）
//	world        （原版无独立目录，GameMap/GameContext/BucketMap 散在 GameLogic/ 根）
//
// **flat 归并说明**：原版 GameLogic 根目录还有约 50 个散文件（Player.cs / GameMap.cs /
// Party.cs / MagicEffect.cs / GameContext.cs / 十几个 I*.cs 接口 …）。Go 的包边界即目录，
// 无法在包根平铺 50 个文件，因此按上表归并；**归并规则不在此处完整展开，逐条登记在
// doc/12-directory-audit.md 的映射表**（铁律 10 的执行依据）。
//
// **本包（含所有子包）版本无关**：原版 `src/GameLogic` 里没有任何 ClientVersion 判断，
// 版本差异一律落在 view/remote、server/gameserver、transport/crypto 三处。
// 因此本包禁止 import `internal/version`。
package gamelogic
