// Package character 是出站序列化的角色域，对应 OpenMU `src/GameServer/RemoteView/Character/`（36 个 .cs，本层最大）：
// 角色列表（含 075/095 变体）、角色创建失败、删除结果、焦点、创建结果、对话框、效果、
// 掉落效果、技能列表（含 075/095）、加点结果（含 Extended）、基础属性更新、
// 属性更新（含 075/097/Extended）、等级更新（含 Extended）、大师技能与大师属性更新、
// 英雄状态、按键配置、果实消耗结果、经验增加（含 Extended）、外观数据适配、
// 角色状态扩展、角色统计类型扩展。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package character
