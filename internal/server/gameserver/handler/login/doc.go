// Package login 对应 OpenMU `src/GameServer/MessageHandler/Login/`（3 个 .cs）。
//
// 入站：LogInHandlerPlugIn、LogOutHandlerPlugIn、LogOutByCheatDetectionHandlerPlugIn、
// LogInOutGroup（组包定义）。
//
// 现状：**已在 gameserver 根目录实现**（handler.go 的 handleLogin / handleF1）。
// 目录先占位；待登录逻辑变复杂（多版本登录包 + 作弊检测 + 状态机）时再迁入本包，
// 迁移时保持"薄壳"——认证编排在 server/loginserver，本包只解包与回包。
//
// 出站在 view/remote/login（ShowLoginResultPlugIn / ShowLoginWindowPlugIn / LogoutPlugIn）。
package login
