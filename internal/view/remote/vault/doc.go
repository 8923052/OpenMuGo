// Package vault 是出站序列化的仓库域，对应 OpenMU `src/GameServer/RemoteView/` **根下**的 5 个 .cs
// （仓库开启、仓库关闭、锁变更响应、金币更新、状态更新）—— 原版未给它们建 Vault 子目录，
// Go 侧按域归入本子包，属"归并"而非"照搬目录"，已登记在 doc/12 的映射表。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package vault
