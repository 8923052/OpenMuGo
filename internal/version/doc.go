// Package version 是客户端版本口径层。
//
// 对应 OpenMU 的四处构件（原版因反射插件框架而分散，Go 端不移植该框架，故合并于此，
// 语义逐条对齐）：
//
//	Network/PlugIns/ClientVersion.cs               → clientversion.go  ClientVersion / ClientLanguage
//	Network/PlugIns/ClientLanguage.cs              → clientversion.go  ClientLanguage 枚举
//	DataModel/Configuration/GameClientDefinition.cs → definition.go     GameClientDefinition
//	GameServer/ClientVersionResolver.cs            → resolver.go        Registry
//	GameServer/PlugInTypeExtensions.cs             → suitable.go        Constraint.Suitable
//
// 版本差异只允许出现在"连接 / 协议 / 视图"边界，业务逻辑层（gamelogic）不得感知版本。
package version
