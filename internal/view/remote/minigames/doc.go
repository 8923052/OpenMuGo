// Package minigames 是出站序列化的活动域，对应 OpenMU `src/GameServer/RemoteView/MiniGames/`（11 个 .cs）：
// 血色城堡状态与积分表、恶魔广场状态、Kanturu 事件、地形属性变更、
// 活动入场结果、活动开启状态、活动状态更新、积分表、枚举扩展。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package minigames
