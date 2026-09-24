package remote

// guild_view.go —— S6 战盟出站（对照 RemoteView/Guild/*）。S6a：成员列表 / 创建结果 / 踢人结果。

import (
	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
)

func (v *PlayerView) SendGuildList(members []action.GuildMemberView, totalScore uint32) error {
	p := s2c.NewGuildList(s2c.GuildListRequiredSize(len(members)))
	p.SetIsInGuild(len(members) > 0)
	p.SetGuildMemberCount(byte(len(members)))
	p.SetTotalScore(totalScore)
	for i, m := range members {
		e := p.Members(i)
		if e == nil {
			break
		}
		e.SetName(m.Name)
		e.SetServerId(m.ServerID)
		e.SetServerId2(m.ServerID)
		e.SetRole(s2c.GuildMemberRole(m.Role))
	}
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendGuildCreationResult(success bool, errorType byte) error {
	p := s2c.NewGuildCreationResult()
	p.SetSuccess(success)
	p.SetError(s2c.GuildCreationErrorType(errorType))
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendGuildKickResult(result byte) error {
	p := s2c.NewGuildKickResponse()
	p.SetResult(s2c.GuildKickSuccess(result))
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendGuildJoinRequest(requesterObjectID uint16) error {
	p := s2c.NewGuildJoinRequest()
	p.SetRequesterId(requesterObjectID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendGuildJoinResult(result byte) error {
	p := s2c.NewGuildJoinResponse()
	p.SetResult(s2c.GuildJoinRequestResult(result))
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendShowGuildCreationDialog() error {
	return v.send.Send(s2c.NewShowGuildCreationDialog().Bytes())
}

func (v *PlayerView) SendGuildInformation(guildID uint32, guildType byte, allianceName, guildName string, logo []byte) error {
	p := s2c.NewGuildInformation()
	p.SetGuildId(guildID)
	p.SetGuildType(guildType)
	p.SetAllianceGuildName(allianceName)
	p.SetGuildName(guildName)
	if len(logo) > 0 {
		copy(p.Logo(), logo)
	}
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) SendAssignCharacterToGuild(members []action.GuildAssignEntry) error {
	p := s2c.NewAssignCharacterToGuild(s2c.AssignCharacterToGuildRequiredSize(len(members)))
	p.SetPlayerCount(byte(len(members)))
	for i, m := range members {
		e := p.Members(i)
		if e == nil {
			break
		}
		e.SetGuildId(m.GuildID)
		e.SetRole(s2c.GuildMemberRole(m.Role))
		e.SetPlayerId(m.PlayerID)
	}
	return v.send.Send(p.Bytes())
}

// SendGuildRelationshipRequest 实现 action.PlayerView（C2 E5，7B）。
// 对照 ShowGuildRelationshipRequestPlugIn：只带请求者在该视图里的对象 ID。
func (v *PlayerView) SendGuildRelationshipRequest(relationshipType, requestType byte, requesterID uint16) error {
	p := s2c.NewGuildRelationshipRequest()
	p.SetRelationshipType(s2c.GuildRelationshipType(relationshipType))
	p.SetRequestType(s2c.GuildRelationshipRequestType(requestType))
	p.SetSenderId(requesterID)
	return v.send.Send(p.Bytes())
}

// SendGuildRelationshipResult 实现 action.PlayerView（C1 E6，8B）。
// 对照 ShowGuildRelationshipChangeResultPlugIn.ShowResultAsync。
func (v *PlayerView) SendGuildRelationshipResult(relationshipType, requestType, result byte, guildMasterID uint16) error {
	p := s2c.NewGuildRelationshipChangeResult()
	p.SetRelationshipType(s2c.GuildRelationshipType(relationshipType))
	p.SetRequestType(s2c.GuildRelationshipRequestType(requestType))
	p.SetResult(s2c.GuildRelationshipChangeResultType(result))
	p.SetGuildMasterId(guildMasterID)
	return v.send.Send(p.Bytes())
}

// SendAllianceList 实现 action.PlayerView（C2 E9；8B 头 + 每条 41B）。
// 对照 ShowAllianceListPlugIn：Success 恒为 true，两个计数字节不填（原版也不填）。
func (v *PlayerView) SendAllianceList(entries []action.AllianceListEntry) error {
	p := s2c.NewAllianceList(s2c.AllianceListRequiredSize(len(entries)))
	p.SetGuildCount(byte(len(entries)))
	p.SetSuccess(true)
	for i, e := range entries {
		blk := p.Guilds(i)
		if blk == nil {
			return nil
		}
		blk.SetMemberCount(e.MemberCount)
		copy(blk.Logo(), e.Logo)
		blk.SetGuildName(e.GuildName)
	}
	return v.send.Send(p.Bytes())
}

// SendRemoveAllianceGuildResult 实现 action.PlayerView（C1 EB 01，7B）。
// 对照 ShowGuildRelationshipChangeResultPlugIn.ShowRemoveResultAsync 的默认参数
// （关系=同盟、请求=退出）。
func (v *PlayerView) SendRemoveAllianceGuildResult(success bool) error {
	p := s2c.NewRemoveAllianceGuildResult()
	p.SetResult(success)
	p.SetRequestType(s2c.GuildRelationshipRequestType(action.GuildRequestLeave))
	p.SetRelationshipType(s2c.GuildRelationshipType(action.GuildRelationshipAlliance))
	return v.send.Send(p.Bytes())
}
