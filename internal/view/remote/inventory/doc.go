// Package inventory 是出站序列化的背包域，对应 OpenMU `src/GameServer/RemoteView/Inventory/`（22 个 .cs）：
// 物品出现、物品移除、物品移动、移动失败、掉落结果、拾取失败、耐久变化、物品升级、
// 卖给 NPC、NPC 购买与购买失败、个人商店购买结果（含 Extended）、金钱更新、背包列表、
// 价格设置应答、消耗失败（含 Extended）、物品出售给玩家商店、枚举扩展。
//
// 本域与 remote 根下的 itemserializer*.go 关系密切：物品块的字节编码在根部序列化器，
// 本域只负责"把物品块放进哪个包、放几个、附带什么元信息"。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package inventory
