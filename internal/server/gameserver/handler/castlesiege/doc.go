// Package castlesiege 对应 OpenMU `src/GameServer/MessageHandler/CastleSiege/`（31 个 .cs）。
//
// 入站：城门买卖/修理/升级、城门操作、城门列表、战盟组、战盟列表、猎场进出、
// 猎场分组/切换、机械分组/使用、印记登记、NPC 分组/列表、已登记战盟列表、
// 登记/登记状态、雕像列表、税率变更/税率信息、贡金提取、取消登记。
//
// 逻辑在 gamelogic/castlesiege（Actions + NPC + Intelligence），出站在 view/remote/castlesiege。
package castlesiege
