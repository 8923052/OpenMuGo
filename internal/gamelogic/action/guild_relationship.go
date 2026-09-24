package action

// guild_relationship.go —— 战盟关系（同盟/敌对）的动作层词汇（对照
// GameLogic/Views/Guild/* 与 PlayerActions/Guild/GuildRelationshipChangeAction）。
//
// 关系与请求类型的线值与原版一致（Alliance/Join=1，Hostility/Leave=2），直接透传；
// 下面的结果常量已经是**线上值**（GUILD_ANS_*），所以视图层不再做序号转换。

// 关系类型（s2c.GuildRelationshipType_* / c2s.ClientToServerGuildRelationshipType_*）。
const (
	GuildRelationshipAlliance  byte = 1
	GuildRelationshipHostility byte = 2
)

// 请求类型（s2c.GuildRelationshipRequestType_*）。
const (
	GuildRequestJoin  byte = 1
	GuildRequestLeave byte = 2
)

// 关系变更结果（线值；命名与原版逻辑枚举一一对应）。
const (
	ResultGuildRelationshipFailed                     byte = 0
	ResultGuildRelationshipSuccess                    byte = 1
	ResultGuildRelationshipGuildNotFound              byte = 16
	ResultGuildRelationshipNoAuthorization            byte = 17
	ResultGuildRelationshipAlreadyInAlliance          byte = 21
	ResultGuildRelationshipAlreadyInHostility         byte = 22
	ResultGuildRelationshipHostileGuildDoesNotExist   byte = 26
	ResultGuildRelationshipNotMasterOfGuildAlliance   byte = 27
	ResultGuildRelationshipMaximumAllianceSizeReached byte = 31
	ResultGuildRelationshipRequestCancelled           byte = 32
)

// AllianceListEntry 是 C1 E9 里的一条同盟战盟（名字/成员数/徽标）。
type AllianceListEntry struct {
	MemberCount byte
	Logo        []byte
	GuildName   string
}
