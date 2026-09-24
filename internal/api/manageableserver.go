package api

import "context"

// ManageableServer 对应 Interfaces/IManageableServer.cs。
//
// 原版继承 INotifyPropertyChanged + IHostedService；Go 侧分别用"变更回调"与"启动/停止方法"表达，
// 不引入属性通知框架（对应铁律：服务之间的推送用接口 + 回调注册）。
//
// 注意 Id 在原版是 int —— 因为 CS/ChatServer 用 0x10000 / 0x20000 这样的高位 id
// （见特殊服务器号），只有游戏服才落在 0..0xFFFF。
type ManageableServer interface {
	Id() int
	// ConfigurationId 是服务配置的标识（原版 Guid）。
	ConfigurationId() string
	Description() string
	Type() ServerType
	ServerState() ServerState
	MaximumConnections() int
	CurrentConnections() int
	// StartAsync 对应 IHostedService.StartAsync（只建服务，不开监听端口）。
	StartAsync(ctx context.Context) error
	// ShutdownAsync 对应 IHostedService.StopAsync。
	ShutdownAsync(ctx context.Context) error
}

// ServerProvider 对应 Interfaces/IServerProvider.cs。
// 原版还实现 INotifyPropertyChanged —— Go 侧改由调用方在容器增删服务时主动拉取。
type ServerProvider interface {
	Servers() []ManageableServer
}

// GameServerStateObserver 对应 Interfaces/IGameServerStateObserver.cs。
//
// 这是"CS 的服务器列表反映真实 GS 状态"的唯一通道：
// GS 监听器启动时 RegisterGameServer，停止时 UnregisterGameServer，连接数变化时 CurrentConnectionsChanged。
type GameServerStateObserver interface {
	RegisterGameServer(gameServer ServerInfo, publicEndPoint EndPoint)
	UnregisterGameServer(gameServerId uint16)
	CurrentConnectionsChanged(serverId uint16, currentConnections int)
}

// EndPoint 对应原版 IPEndPoint 的 IP + 端口二元组（Go 无内建类型，单独表述）。
type EndPoint struct {
	IpAddress string
	Port      int
}

// LoginServer 对应 Interfaces/ILoginServer.cs。
// 职责：跟踪账号"在哪台服务器上登录"，防止重复登录。
//
// serverId 用 int 而非原版的 byte —— 服务器号可超过 0xFF（见特殊服务器号），
// Go 侧统一成 int，避免上游截断。
type LoginServer interface {
	// TryLoginAsync 尝试把账号登到指定服务器；返回是否成功。
	TryLoginAsync(accountName string, serverId int) (bool, error)
	// LogOffAsync 释放账号在该服务器上的占用。
	LogOffAsync(accountName string, serverId int) error
	// GetSnapshotAsync 返回当前在线账号 → 服务器号 的快照。
	GetSnapshotAsync() (map[string]int, error)
}

// AccountAuthenticator 是登录认证结果的提供者。
//
// 原版把"认证 + 会话占用"合成一次 TryLogin；本项目拆成两条：
//   - 契约层 LoginServer 管占用（供服务装配层调用）；
//   - 本接口管认证 + 状态判定（供 GS 登录流调用）。
//
// 返回 api.OnlineAccount（领域无关 DTO）而非 gamelogic/entity.Account ——
// 契约层不得依赖领域包。GS 需要完整账号实体时，由实现包另提供入口
// （如 loginserver.LoginService.Entity）。
type AccountAuthenticator interface {
	// Authenticate 校验用户名密码并判定账号状态；成功时占用会话。
	// 第三个返回值表示认证是否成功（避免调用方拿 LoginOutcome 去比较具体值）。
	Authenticate(username, password string) (LoginOutcome, OnlineAccount, bool)
}

// EventPublisher 对应 Interfaces/IEventPublisher.cs。
//
// GS 通过它把"玩家上线/下线"等事件广播给好友与战盟服务；
// 是 IFriendServer / IGuildServer 的**共同上游**，没有它两个服务都无从实现。
type EventPublisher interface {
	PlayerEnteredGameAsync(serverId uint8, characterId string, characterName string) error
	// guildId 为 0 表示角色无战盟（原版是默认参数）。
	PlayerLeftGameAsync(serverId uint8, characterId string, characterName string, guildId uint32) error
	GuildMessageAsync(guildId uint32, sender string, message string) error
	AllianceMessageAsync(guildId uint32, sender string, message string) error
	PlayerAlreadyLoggedInAsync(serverId uint8, loginName string) error
}

// FriendSystemSubscriber 对应 Interfaces/IFriendSystemSubscriber.cs。
// 实现方是 GameServer：好友/信件的服务端推送最终都要落到"某个连接的出站包"上。
type FriendSystemSubscriber interface {
	LetterReceivedAsync(letter LetterHeader) error
	FriendRequestAsync(requester string, receiver string) error
	// serverId 是 friend 的新服务器号（含 SpecialServerIdOffline 等哨兵值），故为 int。
	FriendOnlineStateChangedAsync(player string, friend string, serverId int) error
	ChatRoomCreatedAsync(playerAuthenticationInfo ChatServerAuthenticationInfo, friendName string) error
	InitializeMessengerAsync(initializationData MessengerInitializationData) error
}
