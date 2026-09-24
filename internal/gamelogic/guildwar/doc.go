// Package guildwar 对应 OpenMU `src/GameLogic/GuildWar`（5 个 .cs）。
//
// 战盟对战的状态与计分：
//
//	GuildWarContext / GuildWarState / GuildWarTeam / GuildWarType / GuildWarScore。
//
// 配套：入站 handler 在 server/gameserver/handler/guild（GuildWarResponseHandlerPlugIn），
// 出站在 view/remote/guild（GuildWarScoreUpdatePlugIn / ShowGuildWarDeclaredPlugIn /
// ShowGuildWarRequestPlugIn / ShowGuildWarResultPlugIn / ShowShowGuildWarRequestResultPlugIn），
// 跨服计分插件在 PlugIns/GuildWarKillScorePlugIn（原版，Go 侧不实现插件容器，改为直接调用）。
//
// 注意：战盟本身（GuildContainer / 成员管理）原版在独立的 GuildServer 项目，
// 不在本包；本包只放"对战"部分，别把战盟数据模型混进来。
package guildwar
