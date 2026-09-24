// Package duel 是出站序列化的决斗域，对应 OpenMU `src/GameServer/RemoteView/Duel/`（12 个 .cs）：
// 决斗请求与请求结果、分数更新、状态更新、生命更新、结束、完成、
// 观众加入/移除/列表更新、初始化决斗。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package duel
