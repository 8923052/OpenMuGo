// Package party 是出站序列化的组队域，对应 OpenMU `src/GameServer/RemoteView/Party/`（5 个 .cs）：
// 组队请求、队伍列表更新（含 075）、队员移除、队伍生命值。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package party
