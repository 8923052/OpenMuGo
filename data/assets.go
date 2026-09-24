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
// 因此导出件按版本分目录存放（data/<版本key>/*.json），载入时由 GameServer 端点绑定的
// GameClientDefinition 决定取哪一份。详见 doc/11-multi-version-adaptation.md。
//
// 导出工具：tools/goldenconfig（引用 OpenMU 初始化工程，内存初始化后按域写出 JSON）。
// 文件清单与 JSON 形状见 internal/gamelogic/config（载入器与校验）。
package data

import (
	"embed"
	"fmt"
	"io/fs"
)

// 每个数据版本一个 embed.FS；新增版本时在此登记目录并扩充本函数。
var (
	//go:embed all:season6
	season6FS embed.FS
)

// FS 返回指定数据版本的文件集合（目录名 = 原版 DataInitialization 的 key）。
// 返回的 fs.FS 根即该版本目录（文件名形如 00_meta.json）。
func FS(version string) (fs.FS, error) {
	switch version {
	case "season6":
		return fs.Sub(season6FS, "season6")
	default:
		return nil, fmt.Errorf("data: 未知数据版本 %q（已登记: season6）", version)
	}
}

// Versions 返回已登记的数据版本。
func Versions() []string { return []string{"season6"} }
