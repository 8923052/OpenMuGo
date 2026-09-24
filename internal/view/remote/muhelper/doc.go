// Package muhelper 是出站序列化的挂机助手域，对应 OpenMU `src/GameServer/RemoteView/MuHelper/`（5 个 .cs）：
// 助手配置更新、助手设置（含序列化器 MuHelperSettingsSerializer、
// 初始化插件 MuHelperSettingsInitializationPlugIn）、助手状态更新。
//
// 注意本域的设置序列化是**自定义加密/编码**（原版 MuHelperSettingsSerializer），
// 不是标准包结构，移植时要逐字节对齐，不要"顺便简化"。
//
// 版本变体沿用原版后缀 `_075` / `_095` / `_097` / `_extended`，
// 每个实现暴露一个 version.Constraint，由本层注册表按连接版本选"下限最紧的适用实现"。
// 完整规则见 remote/doc.go 与 doc/11-multi-version-adaptation.md。
package muhelper
