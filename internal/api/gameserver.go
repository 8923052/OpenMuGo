package api

// GameServer 对应 Interfaces/IGameServer.cs。
//
// 原版继承 IManageableServer + IFriendSystemSubscriber；方法名去掉 Async 后缀（Go 无此惯例）。
type GameServer interface {
	ManageableServer
	FriendSystemSubscriber

	GuildChatMessageAsync(guildId uint32, sender string, message string) error
	GuildDeletedAsync(guildId uint32) error
	GuildPlayerKickedAsync(playerName string) error
	AllianceChatMessageAsync(guildId uint32, sender string, message string) error
	SendGlobalMessageAsync(message string, messageType MessageType) error

	DisconnectPlayerAsync(playerName string) (bool, error)
	DisconnectAccountAsync(accountName string) (bool, error)
	BanPlayerAsync(playerName string) (bool, error)

	AssignGuildToPlayerAsync(characterName string, guildStatus GuildMemberStatus) error
	PlayerAlreadyLoggedInAsync(serverId uint8, loginName string) error

	AllianceCreatedAsync(masterGuildId uint32, memberGuildId uint32) error
	AllianceDisbandedAsync(masterGuildId uint32, memberGuildId uint32) error
	// GuildHostilityChangedAsync 的同盟列表：无同盟时就是只含自身 id 的单元素切片。
	GuildHostilityChangedAsync(guildIdA uint32, allianceGuildIdsA []uint32, guildIdB uint32, allianceGuildIdsB []uint32, created bool) error
}

// ServerContainer 对应 Startup/{GameServerContainer,ConnectServerContainer,ChatServerContainer}
// 共有的"按 id 索引可增删实例"能力（原版还有 IGameServerInstanceManager /
// IConnectServerInstanceManager 两个接口，Go 侧统一为这一个）。
type ServerContainer interface {
	// Add 注册一个实例；已存在同 id 时由实现决定报错或替换（与调用方无关）。
	Add(server ManageableServer) error
	// Remove 移除实例（对应原版 RemoveGameServerAsync / RemoveConnectServerAsync）。
	Remove(id int) error
	// Get 按 id 取实例。
	Get(id int) (ManageableServer, bool)
	// All 返回全部实例的当前快照。
	All() []ManageableServer
}
