// Package data 承载从 OpenMU 导出的游戏数值（物品、技能、职业、怪物、地图、掉落、经验表），
// 通过 go:embed 载入，避免把 Persistence/Initialization 的 407 个 C# 文件重写一遍。
//
// 数值是**按版本**初始化的（原版 Persistence/Initialization 下每个客户端版本一个初始化插件）：
//
//	Version075/        0.75
//	Version095d/       0.95d
//	Version097d/       0.97d（仅少量修正）
//	VersionSeasonSix/  S6E3 —— GMO 1.04d 与开源客户端 2.04d 共用，测试账号也在这里
//
// 因此导出件按版本分目录存放，载入时由 GameServer 端点绑定的 GameClientDefinition 决定取哪一份。
// 详见 doc/11-multi-version-adaptation.md。
package data
