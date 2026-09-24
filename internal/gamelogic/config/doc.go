// Package config 是游戏配置层，对应 OpenMU 的 DataModel/Configuration（GameConfiguration）。
// 将来承载从 OpenMU 导出的游戏数值（物品、技能、职业、怪物、地图、掉落、经验表）与查询接口。
//
// 客户端版本口径不在这里：见 internal/version（原版分散在 Network/PlugIns 与 DataModel 两处，
// 因不移植反射插件框架而合并）。
package config
