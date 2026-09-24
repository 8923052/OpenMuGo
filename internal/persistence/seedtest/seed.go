// Package seedtest 提供与 OpenMU 一致的内存测试账号种子（全集见 full.go）。
// 拔插边界：本包只依赖 gamelogic/config、gamelogic/entity、gamelogic/entity/item、
// gamelogic/player、gamelogic/storage 与 view/remote，不 import server / world / proto；
// 是否装入完全由 cmd/mugo 决定（-seed=openmu|none），接 DB 后整体不引用即可。
// view/remote 属有意例外：种子要预编码外观字节（Appearance/AppearanceExt），而外观编码按
// 铁律只能放 view/remote；本包无任何版本判断，不违反版本无关铁律（layoutcheck 对本包豁免）。
//
// 数据来源：OpenMU src/Persistence/Initialization/VersionSeasonSix/TestAccounts/。
package seedtest

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// 出生门（Gates.cs，IsSpawnGate 门）：
// Lorencia(0) X133..151 Y118..135 dir0；Noria(3) X171..177 Y108..117 dir0。
const (
	lorenciaNumber = 0
	lorenciaX1     = 133
	lorenciaY1     = 118
	lorenciaX2     = 151
	lorenciaY2     = 135

	noriaNumber = 3
	noriaX1     = 171
	noriaY1     = 108
	noriaX2     = 177
	noriaY2     = 117
)

// testMoney 与 OpenMU 种子一致（ItemStorage.Money = 10000000）。
const testMoney = 10_000_000

// Accounts 生成 OpenMU 测试账号全集（test0..9、test300、test400、ancient、socket、
// quest1/2/3、testgm、testgm2、testunlock，规格见 full.go），密码同名。
// cfg 提供物品定义（宽高/耐久）与经验表；测试脚手架可传 nil（物品按 1×1、耐久取规格值）。
func Accounts(cfg *config.GameConfig) []*entity.Account {
	return BuildSpecs(cfg)
}
