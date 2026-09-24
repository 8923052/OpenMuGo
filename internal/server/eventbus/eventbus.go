// Package eventbus 实现进程内事件总线（doc/14 P5.1，对应原版 Startup/InMemoryEventPublisher）。
//
// 扇出映射（顺序与条件是原版写死的，不得调整）：
//   - PlayerEnteredGame：**先** GuildServer.PlayerEnteredGame **再** FriendServer.PlayerEnteredGame；
//   - PlayerLeftGame：**guildId > 0 才**调 GuildServer.GuildMemberLeftGame；随后必调 FriendServer.PlayerLeftGame；
//   - GuildMessage / AllianceMessage / PlayerAlreadyLoggedIn：扇出到**全部**游戏服。
package eventbus

import "mugo/internal/api"

// GameServerView 是事件总线需要的游戏服最小视图
// （原版依赖 IDictionary<int, IGameServer>，这里按使用面收窄）。
type GameServerView interface {
	GuildChatMessageAsync(guildId uint32, sender string, message string) error
	AllianceChatMessageAsync(guildId uint32, sender string, message string) error
	PlayerAlreadyLoggedInAsync(serverId uint8, loginName string) error
}

// FriendServerView 是好友服需要的最小视图（原版 IFriendServer 的相关方法）。
type FriendServerView interface {
	PlayerEnteredGameAsync(serverId uint8, characterId string, characterName string) error
	PlayerLeftGameAsync(characterId string, characterName string) error
}

// GuildServerView 是战盟服需要的最小视图（原版 IGuildServer 的相关方法）。
type GuildServerView interface {
	PlayerEnteredGameAsync(characterId string, characterName string, serverId uint8) error
	GuildMemberLeftGameAsync(guildId uint32, characterId string, serverId uint8) error
}

// InMemoryPublisher 对应原版 InMemoryEventPublisher：
// 把事件直接发布给本进程内已启动的服务。
type InMemoryPublisher struct {
	// gameServers 返回全部游戏服视图（原版持有字典引用，实时遍历；
	// 用函数而非切片，保证运行中注册的新 GS 也能收到扇出）。
	gameServers func() []GameServerView
	friend      FriendServerView
	guild       GuildServerView
}

// NewInMemoryPublisher 创建事件总线。
// gameServers 为 nil 时聊天消息扇出为空操作（原版空字典行为一致）。
func NewInMemoryPublisher(gameServers func() []GameServerView, friend FriendServerView, guild GuildServerView) *InMemoryPublisher {
	return &InMemoryPublisher{gameServers: gameServers, friend: friend, guild: guild}
}

// PlayerEnteredGameAsync 对应原版：先战盟后好友。
func (p *InMemoryPublisher) PlayerEnteredGameAsync(serverId uint8, characterId string, characterName string) error {
	if p.guild != nil {
		if err := p.guild.PlayerEnteredGameAsync(characterId, characterName, serverId); err != nil {
			return err
		}
	}
	if p.friend != nil {
		if err := p.friend.PlayerEnteredGameAsync(serverId, characterId, characterName); err != nil {
			return err
		}
	}
	return nil
}

// PlayerLeftGameAsync 对应原版：guildId > 0 才通知战盟；好友必通知。
func (p *InMemoryPublisher) PlayerLeftGameAsync(serverId uint8, characterId string, characterName string, guildId uint32) error {
	if guildId > 0 && p.guild != nil {
		if err := p.guild.GuildMemberLeftGameAsync(guildId, characterId, serverId); err != nil {
			return err
		}
	}
	if p.friend != nil {
		if err := p.friend.PlayerLeftGameAsync(characterId, characterName); err != nil {
			return err
		}
	}
	return nil
}

// GuildMessageAsync 扇出到全部游戏服。
func (p *InMemoryPublisher) GuildMessageAsync(guildId uint32, sender string, message string) error {
	for _, gs := range p.allGameServers() {
		if err := gs.GuildChatMessageAsync(guildId, sender, message); err != nil {
			return err
		}
	}
	return nil
}

// AllianceMessageAsync 扇出到全部游戏服。
func (p *InMemoryPublisher) AllianceMessageAsync(guildId uint32, sender string, message string) error {
	for _, gs := range p.allGameServers() {
		if err := gs.AllianceChatMessageAsync(guildId, sender, message); err != nil {
			return err
		}
	}
	return nil
}

// PlayerAlreadyLoggedInAsync 扇出到全部游戏服。
func (p *InMemoryPublisher) PlayerAlreadyLoggedInAsync(serverId uint8, loginName string) error {
	for _, gs := range p.allGameServers() {
		if err := gs.PlayerAlreadyLoggedInAsync(serverId, loginName); err != nil {
			return err
		}
	}
	return nil
}

// 确保 api.EventPublisher 契约不变（编译期断言）。
var _ api.EventPublisher = (*InMemoryPublisher)(nil)

func (p *InMemoryPublisher) allGameServers() []GameServerView {
	if p.gameServers == nil {
		return nil
	}
	return p.gameServers()
}
