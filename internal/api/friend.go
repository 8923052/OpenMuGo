package api

// Friend 对应 Interfaces/Friend.cs。
type Friend struct {
	Id        string // Guid
	Character string // Guid：本人角色
	Friend    string // Guid：好友角色
	Accepted  bool
	// RequestOpen 请求是否仍未处理。
	RequestOpen bool
}

// FriendViewItem 对应 Interfaces/FriendViewItem.cs（原版继承 Friend 并补两个名字）。
type FriendViewItem struct {
	Friend
	CharacterName string
	FriendName    string
}

// SpecialServerId 对应 Interfaces/IFriendServer.cs 的 enum SpecialServerId : byte。
//
// 语义要点：**好友的"在线"不是 bool，而是"在哪台服务器上"**——
// 离线与隐身都用哨兵服务器号表示，因此不能换成 -1 / 0。
type SpecialServerId uint8

const (
	// SpecialServerIdOffline 表示离线。
	SpecialServerIdOffline SpecialServerId = 0xFF
	// SpecialServerIdInvisible 表示隐身（对其他玩家显示为离线，但仍在线）。
	SpecialServerIdInvisible SpecialServerId = 0xFE
)

// LetterHeader 对应 Interfaces/LetterHeader.cs。
type LetterHeader struct {
	Id           string // Guid
	SenderName   string
	ReceiverName string
	Subject      string
	// LetterDate 原版为 DateTime；Go 用 Unix 纳秒表述"时刻"，由使用方决定格式化。
	LetterDate int64
	ReadFlag   bool
	// Index 是该信在收件人信箱中的下标（AddLetter/读/删都以此定位）。
	Index uint16
}

// LetterDetail 是读信所需的完整信件（正文 + 发件人外观快照），由好友服返回给游戏服下发 OpenLetter。
type LetterDetail struct {
	LetterHeader
	Message          string
	Rotation         byte
	Animation        byte
	SenderAppearance []byte
}

// 信件状态（对应 AddLetter.LetterState）。
const (
	LetterStateRead   byte = 0
	LetterStateUnread byte = 1
	LetterStateNew    byte = 2
)

// MessengerInitializationData 对应 Interfaces/IFriendSystemSubscriber.cs 尾部的 record。
type MessengerInitializationData struct {
	PlayerName         string
	Friends            []string
	OpenFriendRequests []string
}

// ChatServerAuthenticationInfo 对应 Interfaces/ChatServerAuthenticationInfo.cs。
//
// 两个"魔法值"必须原样保留（客户端按字节解析）：
//   - 构造时置 AuthenticationRequiredUntil = now + 30s；
//   - AuthenticationToken 由 IChatServer 生成，是 4+2 字节而非 UUID。
type ChatServerAuthenticationInfo struct {
	Index      uint8
	RoomId     uint16
	ClientName string
	// AuthenticationToken 客户端进入聊天室时必须提供的"随机口令"。
	AuthenticationToken string
	// HostAddress 托管该聊天室的 ChatServer 地址。
	HostAddress string
	// AuthenticationRequiredUntil 此 Unix 纳秒时刻之后若房内不足 2 人，房间自动关闭。
	AuthenticationRequiredUntil int64
}
