// Package playershop 是出站序列化的个人商店域，对应 OpenMU `src/GameServer/RemoteView/PlayerShop/`（5 个 .cs）：
// 商店开启、商店关闭、商店物品列表（含 Extended）、玩家商店可见列表。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package playershop
