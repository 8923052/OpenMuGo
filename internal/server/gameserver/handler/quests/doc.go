// Package quests 对应 OpenMU `src/GameServer/MessageHandler/Quests/`（14 个 .cs）。
//
// 入站：活动任务列表、可接任务、buff 请求、事件任务状态、任务取消、客户端动作、
// 任务完成、任务组包、任务推进、任务选择、任务状态列表（Legacy）、任务状态、
// 任务状态设置（Legacy）、QuestConstants。
//
// 逻辑在 gamelogic/action（PlayerActions/Quests/），
// 出站在 view/remote/quest（QuestProgressPlugIn 含 Extended 变体、LegacyQuest* 两个）。
package quests
