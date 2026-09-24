// Package trade 是出站序列化的交易域，对应 OpenMU `src/GameServer/RemoteView/Trade/`（5 个 .cs）：
// 交易请求与应答、金额设置、请求金额已设置、按钮状态、交易物品出现/消失、交易完成。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package trade
