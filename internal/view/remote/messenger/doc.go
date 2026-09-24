// Package messenger 是出站序列化的信使域，对应 OpenMU `src/GameServer/RemoteView/Messenger/`（12 个 .cs）：
// 好友请求与邀请结果、好友添加/删除/状态更新、信件列表添加、信件发送结果与删除、
// 信件显示（含 Extended）、聊天室创建、信使初始化。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package messenger
