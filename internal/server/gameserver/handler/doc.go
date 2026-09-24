// Package handler 聚合 GameServer 的入站 handler，对应 OpenMU `src/GameServer/MessageHandler`。
//
// 子包名与原版目录名一一对应（一个 C# 目录 → 一个 Go 子包）：
//
//	castlesiege   CastleSiege/  (31 .cs)  战盟攻城：城门/雕像/机械/税收/登记
//	character     Character/    (13 .cs)  角色列表/创建/删除/焦点/加点/移速
//	duel          Duel/          (6 .cs)  决斗频道/开始/结束
//	guild         Guild/        (13 .cs)  战盟创建/加入/踢人/关系/战盟战
//	items         Items/        (13 .cs)  买卖/混沌合成/消耗/丢弃/移动/修理/拾取
//	login         Login/         (3 .cs)  登录/登出/作弊检测登出
//	messenger     Messenger/     (9 .cs)  好友/信件/聊天室
//	minigames     MiniGames/     (7 .cs)  血色城堡/恶魔广场/失落之塔/Kanturu 入场
//	muhelper      MuHelper/      (3 .cs)  挂机助手保存/状态切换
//	party         Party/         (4 .cs)  组队请求/应答/踢人/列表
//	pet           Pet/           (2 .cs)  宠物命令/信息
//	playershop    PlayerShop/    (6 .cs)  个人商店开/关/买/定价
//	quests        Quests/       (14 .cs)  任务状态/进度/完成/事件
//	trade         Trade/         (5 .cs)  交易请求/接受/取消/金额/按钮
//	vault         Vault/         (6 .cs)  仓库开锁/密码/金币/关闭
//
// 原版另有约 150 个 handler **直接摊在 MessageHandler/ 根下**（未分域），例如
// CharacterMoveHandlerPlugIn / HitHandlerPlugIn / TalkNpcHandlerPlugIn /
// TargetedSkillHandlerPlugIn / WarpHandlerPlugIn / AnimationHandlerPlugIn /
// GroupPacketHandlerPlugIn / ChatMessageHandlerPlugIn 等。
// 这些按"动作主角域"归入上表子包（如移动/走路 → character，攻击/技能 → 归 skills 或 items，
// 具体归并逐条登记在 doc/12 的映射表），**不在本包根目录再堆平铺文件**。
//
// 每个 handler 保持薄壳：解包 → 调 gamelogic/action → 出站交 view/remote。
package handler
