// Package guild 是出站序列化的战盟域，对应 OpenMU `src/GameServer/RemoteView/Guild/`（22 个 .cs）：
// 战盟创建对话框与创建结果、加入请求与应答、战盟列表（含 075）、战盟信息、基础战盟信息、
// 成员分配（含 075）、踢人结果、离开战盟、结盟列表与关系请求、关系变更结果、
// 盟主对话框、战盟战请求/宣战/结果/请求结果、战盟战分数更新、枚举扩展。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package guild
