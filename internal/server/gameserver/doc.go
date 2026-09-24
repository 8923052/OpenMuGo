// Package gameserver 是加密 GameServer（55901）的装配与帧分发层，
// 对应 OpenMU `src/GameServer` 的根目录部分：
//
//	GameServer.cs / GameServerContext.cs / DefaultTcpGameServerListener.cs /
//	IClientVersionProvider.cs / ClientVersionResolver.cs / PlugInTypeExtensions.cs /
//	PacketType.cs / NetworkObservationHandler.cs / GameServerMapInitializer.cs /
//	MaximumClientAttribute.cs / ClientAttribute.cs / MinimumClientAttribute.cs /
//	RemotePlayerConnectionInfo.cs / PlayerConnectedEventArgs.cs / DirectionExtensions.cs /
//	EncodingExtensions.cs。
//
// 包内文件分工（保持"薄壳"定位，禁止在此拼出站包）：
//
//	server.go              GameServer 本体：端点、监听、装配
//	session.go             每连接状态（登录态、账号、客户端版本）
//	handler.go             帧分发（PacketType 路由）+ 登录组 handler
//	handler_character.go   角色列表/选角
//	handler_enter.go       进图（换图、视野播报）
//	handler_move.go        D4 行走
//
// 入站 handler 的**域子包**放在 handler/ 下，按原版 MessageHandler 的目录名一一对应
// （castlesiege / character / duel / guild / items / login / messenger / minigames /
// muhelper / party / pet / playershop / quests / trade / vault），
// 域内 handler 一多就归位到对应子包，不要继续在根目录堆 handler_*.go。
//
// 出站一律经 view/remote，版本差异只允许出现在本包与 view/remote、transport/crypto 三处。
package gameserver
