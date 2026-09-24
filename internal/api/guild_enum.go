package api

// GuildPosition 对应 Interfaces/GuildPosition.cs 的 enum GuildPosition : byte。
// 部分位置在攻城战里有特殊技能，取值顺序不可重排。
type GuildPosition uint8

const (
	GuildPositionUndefined GuildPosition = iota
	GuildPositionNormalMember
	GuildPositionGuildMaster
	GuildPositionBattleMaster
	GuildPositionAssistantMaster
)

// GuildRelationship 对应 Interfaces/IGuildServer.cs 的 enum GuildRelationship。
type GuildRelationship uint8

const (
	GuildRelationshipNone  GuildRelationship = 0
	GuildRelationshipUnion GuildRelationship = 1
	GuildRelationshipRival GuildRelationship = 2
)

// AllianceCreationResult 对应 Interfaces/IGuildServer.cs 的 enum AllianceCreationResult。
type AllianceCreationResult uint8

const (
	AllianceCreationFailed AllianceCreationResult = iota
	AllianceCreationSuccess
	AllianceCreationMasterGuildNotFound
	AllianceCreationTargetGuildNotFound
	AllianceCreationTargetGuildAlreadyInAlliance
	AllianceCreationMaximumAllianceSizeReached
	AllianceCreationGuildNotFoundInTargetContext
	AllianceCreationError
)
