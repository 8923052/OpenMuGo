// Package muhelper 对应 OpenMU `src/GameServer/MessageHandler/MuHelper/`（3 个 .cs）。
//
// 入站：MuHelperGroupHandler（组包）、存档请求、状态切换请求。
//
// 逻辑在 gamelogic/muhelper，出站在 view/remote/muhelper（配置序列化 MuHelperSettingsSerializer）。
package muhelper
