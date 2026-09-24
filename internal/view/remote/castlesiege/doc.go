// Package castlesiege 是出站序列化的战盟攻城域，对应 OpenMU `src/GameServer/RemoteView/CastleSiege/`（21 个 .cs）：
// 王冠访问状态与王冠状态、城门列表、猎场守卫信息与结果、加入阵营、生命石状态、
// 机械接口与区域通知与使用结果、印记登记结果、NPC 列表与操作结果、归属变更、
// 已登记战盟列表、登记结果与状态、开关信息、税收信息与变更结果、贡金提取结果。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package castlesiege
