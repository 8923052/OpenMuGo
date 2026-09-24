// Package quest 是出站序列化的任务域，对应 OpenMU `src/GameServer/RemoteView/Quest/`（19 个 .cs）：
// 当前活动任务、可接任务、任务开始/取消/完成响应、任务进度（含 Extended）、
// 任务状态响应（含 Extended）、任务事件响应、任务步骤信息、
// Legacy 任务状态对话框与奖励、任务状态扩展、任务结构扩展、角色职业扩展、枚举扩展。
//
// 注意子包名是单数 `quest`（原版目录名 RemoteView/Quest），
// 与入站侧 `handler/quests`（原版 MessageHandler/Quests）不同 —— 原版两处就不一致，Go 侧照抄。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package quest
