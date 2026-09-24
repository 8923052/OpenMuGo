// Package guild 对应 OpenMU `src/GameServer/MessageHandler/Guild/`（13 个 .cs）。
//
// 入站：结盟组、结盟列表、取消创建、创建（含 075 变体）、战盟信息、踢人、战盟列表、
// 盟主应答、关系变更/应答、加入请求/应答、战盟战应答、解除结盟。
//
// 逻辑在 gamelogic/action（PlayerActions/Guild/）+ gamelogic/guildwar（战盟战部分），
// 战盟数据的跨服部分原版在独立的 GuildServer 项目，Go 侧服务边界见 doc/12。
// 出站在 view/remote/guild。
package guild
