package api

// Guild 对应 Interfaces/Guild.cs。
//
// 原版备注（必须保留语义）：**guild id 由 IGuildServer 在内存中按需分配，不持久化**。
// 持久化的是一份"姓名 → 运行时 id"的映射。因此本结构体**没有 Id 字段**——
// 不要"顺手"补一个 Id，那会丢掉合并两套数据库（realm）的能力。
type Guild struct {
	Name   string
	Logo   []byte // 16 色 8x8 位图，固定 32 字节
	Score  int
	Notice string
	// Hostility 敌对战盟：其成员可无惩罚击杀。
	Hostility *Guild
	// AllianceGuild 上级同盟战盟。
	AllianceGuild *Guild
}

// GuildMemberStatus 对应 Interfaces/GuildMemberStatus.cs。
type GuildMemberStatus struct {
	GuildId  uint32
	Position GuildPosition
}

// NewGuildMemberStatus 对应原版主构造函数。
func NewGuildMemberStatus(guildId uint32, position GuildPosition) GuildMemberStatus {
	return GuildMemberStatus{GuildId: guildId, Position: position}
}

// GuildListEntry 对应 Interfaces/IGuildServer.cs 尾部的 class GuildListEntry。
type GuildListEntry struct {
	// PlayerName 原版是可空（离线成员可能缺名）。
	PlayerName string
	ServerId   uint8
	// PlayerPosition 原版字段名如此（不是 GuildPosition）。
	PlayerPosition GuildPosition
}

// AllianceGuildEntry 对应 Interfaces/AllianceGuildEntry.cs 的 record。
type AllianceGuildEntry struct {
	Id          uint32
	GuildName   string
	MemberCount int
	Logo        []byte
}
