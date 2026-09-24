// Package npc 是出站序列化的 NPC 域，对应 OpenMU `src/GameServer/RemoteView/NPC/`（6 个 .cs）：
// 打开 NPC 窗口、商人商店物品列表、物品合成结果、NPC 对话框关闭（075）、
// 物品登记结果、对象消息。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package npc
