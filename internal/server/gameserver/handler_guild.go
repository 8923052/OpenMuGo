package gameserver

// handler_guild.go —— S6 战盟入站（对照 MessageHandler/Guild + PlayerActions/Guild）。
// S6a：创建(0x55)/成员列表(0x52)/踢人(0x53)。GS 经 guildService 窄接口调用战盟服，
// 战盟服再经 GuildChangePublisher（social.go）把变更落到本地会话出站包。

import (
	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
)

// GuildKickResponse 结果码（对齐 s2c.GuildKickSuccess）。
const (
	guildKickSuccess   byte = 0
	guildKickFailed    byte = 1
	guildKickNotMaster byte = 2
	guildKickDisband   byte = 3
)

// GuildJoinResponse 结果码（对齐 s2c.GuildJoinRequestResult）。
const (
	guildJoinRefused      byte = 0
	guildJoinAccepted     byte = 1
	guildJoinDisconnected byte = 3
	guildJoinNotMaster    byte = 4
	guildJoinAlreadyGuild byte = 5
	guildJoinMinLevelSix  byte = 7
)

// guildPositionToWireRole 把 api.GuildPosition 映射为封包 GuildMemberRole（0/32/64/128/255），
// 对照原版 EnumExtensions.Convert()。
func guildPositionToWireRole(p api.GuildPosition) byte {
	switch p {
	case api.GuildPositionNormalMember:
		return 0
	case api.GuildPositionBattleMaster:
		return 32
	case api.GuildPositionAssistantMaster:
		return 64
	case api.GuildPositionGuildMaster:
		return 128
	default:
		return 255
	}
}

// wireRoleToGuildPosition 是 guildPositionToWireRole 的逆（用于 0xE1 角色分配）。
func wireRoleToGuildPosition(role byte) api.GuildPosition {
	switch role {
	case 0:
		return api.GuildPositionNormalMember
	case 32:
		return api.GuildPositionBattleMaster
	case 64:
		return api.GuildPositionAssistantMaster
	case 128:
		return api.GuildPositionGuildMaster
	default:
		return api.GuildPositionUndefined
	}
}

// guildService 是 GS 入站所需的战盟服窄视图（对应 guildserver.Server 的方法集）。
type guildService interface {
	GuildExistsAsync(guildName string) bool
	CreateGuildAsync(name, masterName string, logo []byte, serverId uint8) (bool, error)
	KickMemberAsync(guildId uint32, playerName string) error
	CreateGuildMemberAsync(guildId uint32, characterName string, role api.GuildPosition, serverId uint8) error
	ChangeGuildMemberPositionAsync(guildId uint32, characterName string, role api.GuildPosition) error
	GetGuildListAsync(guildId uint32) []api.GuildListEntry
	GetGuildScoreAsync(guildId uint32) uint32
	GetGuildInfoAsync(guildId uint32) (name, allianceName string, logo []byte, ok bool)
	// AllianceGuildsAsync 取同盟内全部战盟（不在同盟时为空——同盟聊天因此不发）。
	AllianceGuildsAsync(guildId uint32) []api.AllianceGuildEntry

	// 关系变更（同盟/敌对）所需的关系查询与变更（doc/17 S-3 批次 5）。
	AllianceMasterOfAsync(guildId uint32) (masterId uint32, inAlliance bool)
	IsAllianceMasterAsync(guildId uint32) bool
	AreHostileAsync(guildA, guildB uint32) bool
	HasAnyHostilityAsync(guildId uint32) bool
	CreateAllianceAsync(masterGuildId, targetGuildId uint32) (api.AllianceCreationResult, error)
	RemoveAllianceAsync(targetGuildId uint32) (bool, error)
	SetHostilityAsync(guildIdA, guildIdB uint32, create bool) (bool, error)
	GetGuildIdByNameAsync(guildName string) uint32
}

// SetGuildService 注入战盟服句柄（装配层在战盟服构造后调用）。
func (s *Server) SetGuildService(g guildService) { s.deps.guild = g }

// guildStatusOf 查角色的战盟归属（含职位）。
func (s *Server) guildStatusOf(characterName string) api.GuildMemberStatus {
	s.guildMu.RLock()
	defer s.guildMu.RUnlock()
	return s.guildByCharacter[characterName]
}

// handleGuildCreate 处理 C1 55（GuildCreateHandler）：名占用/已在盟则失败，否则建档并回结果。
// 偏差：未校验创建等级/zen 花费（原版配置项）。
func (s *Server) handleGuildCreate(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	if s.guildStatusOf(me.Name).GuildId != 0 {
		_ = s.viewFor(sess).SendGuildCreationResult(false, 1)
		return
	}
	req := c2s.AsGuildCreateRequest(frame)
	name := req.GuildNameString()
	if name == "" || s.deps.guild.GuildExistsAsync(name) {
		_ = s.viewFor(sess).SendGuildCreationResult(false, 1)
		return
	}
	logo := append([]byte(nil), req.GuildEmblem()...)
	ok, err := s.deps.guild.CreateGuildAsync(name, me.Name, logo, uint8(s.serverID))
	if err != nil {
		s.deps.logger.Printf("gameserver: 创建战盟失败 %s: %v", name, err)
	}
	_ = s.viewFor(sess).SendGuildCreationResult(ok, 0)
}

// handleGuildList 处理 C1 52（GuildListRequestHandler）：回自己的战盟成员列表。
func (s *Server) handleGuildList(sess *session) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	gid := s.guildOfCharacter(me.Name)
	if gid == 0 {
		return
	}
	entries := s.deps.guild.GetGuildListAsync(gid)
	members := make([]action.GuildMemberView, 0, len(entries))
	for _, e := range entries {
		members = append(members, action.GuildMemberView{Name: e.PlayerName, ServerID: e.ServerId, Role: guildPositionToWireRole(e.PlayerPosition)})
	}
	_ = s.viewFor(sess).SendGuildList(members, s.deps.guild.GetGuildScoreAsync(gid))
}

// handleGuildKick 处理 C1 53（GuildKickPlayerHandler）：仅团长可踢；踢团长本人=解散。
func (s *Server) handleGuildKick(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	st := s.guildStatusOf(me.Name)
	if st.GuildId == 0 {
		return
	}
	target := c2s.AsGuildKickPlayerRequest(frame).PlayerNameString()
	if target == "" {
		return
	}
	if st.Position != api.GuildPositionGuildMaster {
		_ = s.viewFor(sess).SendGuildKickResult(guildKickNotMaster)
		return
	}
	if err := s.deps.guild.KickMemberAsync(st.GuildId, target); err != nil {
		s.deps.logger.Printf("gameserver: 踢成员失败 %s←%s: %v", target, me.Name, err)
		_ = s.viewFor(sess).SendGuildKickResult(guildKickFailed)
		return
	}
	if target == me.Name {
		_ = s.viewFor(sess).SendGuildKickResult(guildKickDisband)
	} else {
		_ = s.viewFor(sess).SendGuildKickResult(guildKickSuccess)
	}
}

// resolveViewPlayer 在 (mapNum,x,y) 的视野玩家里找世界对象 ID==id 的名字（加入请求按对象ID定位团长）。
func (s *Server) resolveViewPlayer(mapNum uint16, x, y byte, id uint16) (string, bool) {
	for _, p := range s.world.PlayersInRange(mapNum, x, y) {
		if p.ID == id {
			return p.Name, true
		}
	}
	return "", false
}

// handleGuildJoinRequest 处理 C1 50（GuildRequestHandler）：向视野内团长请求入盟。
func (s *Server) handleGuildJoinRequest(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	me := sess.getSelected()
	if wp == nil || me == nil {
		return
	}
	if s.guildStatusOf(me.Name).GuildId != 0 {
		_ = s.viewFor(sess).SendGuildJoinResult(guildJoinAlreadyGuild)
		return
	}
	if me.Level < 6 {
		_ = s.viewFor(sess).SendGuildJoinResult(guildJoinMinLevelSix)
		return
	}
	masterID := c2s.AsGuildJoinRequest(frame).GuildMasterPlayerId() & 0x7FFF
	masterName, ok := s.resolveViewPlayer(wp.MapNumber, wp.X, wp.Y, masterID)
	if !ok {
		return
	}
	masterSess := s.sessionOfCharacter(masterName)
	if masterSess == nil || s.guildStatusOf(masterName).Position != api.GuildPositionGuildMaster {
		_ = s.viewFor(sess).SendGuildJoinResult(guildJoinNotMaster)
		return
	}
	masterSess.pendingGuildRequesterName = me.Name
	_ = s.viewFor(masterSess).SendGuildJoinRequest(wp.ID)
}

// handleGuildJoinResponse 处理 C1 51（GuildRequestAnswerHandler）：团长应答入盟请求。
func (s *Server) handleGuildJoinResponse(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	st := s.guildStatusOf(me.Name)
	req := c2s.AsGuildJoinResponse(frame)
	accepted := req.Accepted()
	requester := sess.pendingGuildRequesterName
	sess.pendingGuildRequesterName = ""
	if requester == "" {
		return
	}
	if st.GuildId == 0 || st.Position != api.GuildPositionGuildMaster {
		if as := s.sessionOfCharacter(requester); as != nil {
			_ = s.viewFor(as).SendGuildJoinResult(guildJoinNotMaster)
		}
		return
	}
	appSess := s.sessionOfCharacter(requester)
	if appSess == nil {
		return
	}
	if s.guildStatusOf(requester).GuildId != 0 {
		_ = s.viewFor(appSess).SendGuildJoinResult(guildJoinAlreadyGuild)
		return
	}
	if !accepted {
		_ = s.viewFor(appSess).SendGuildJoinResult(guildJoinRefused)
		return
	}
	if err := s.deps.guild.CreateGuildMemberAsync(st.GuildId, requester, api.GuildPositionNormalMember, uint8(s.serverID)); err != nil {
		_ = s.viewFor(appSess).SendGuildJoinResult(guildJoinDisconnected)
		return
	}
	_ = s.viewFor(appSess).SendGuildJoinResult(guildJoinAccepted)
	s.broadcastGuildAssign(st.GuildId)
}

// broadcastGuildAssign 向某战盟全部在线成员下发 AssignCharacterToGuild（成员/角色刷新）。
func (s *Server) broadcastGuildAssign(guildID uint32) {
	if s.deps.guild == nil {
		return
	}
	entries := s.deps.guild.GetGuildListAsync(guildID)
	members := make([]action.GuildAssignEntry, 0, len(entries))
	sessions := make([]*session, 0, len(entries))
	for _, e := range entries {
		ms := s.sessionOfCharacter(e.PlayerName)
		if ms == nil {
			continue
		}
		mw := ms.getWorldPlayer()
		if mw == nil {
			continue
		}
		members = append(members, action.GuildAssignEntry{GuildID: guildID, Role: guildPositionToWireRole(e.PlayerPosition), PlayerID: mw.ID})
		sessions = append(sessions, ms)
	}
	if len(members) == 0 {
		return
	}
	for _, ms := range sessions {
		_ = s.viewFor(ms).SendAssignCharacterToGuild(members)
	}
}

// handleGuildRoleAssign 处理 C1 E1（GuildPlayerRoleChangeHandler）：团长改成员职位并广播。
func (s *Server) handleGuildRoleAssign(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	me := sess.getSelected()
	if me == nil {
		return
	}
	st := s.guildStatusOf(me.Name)
	if st.GuildId == 0 || st.Position != api.GuildPositionGuildMaster {
		return
	}
	req := c2s.AsGuildRoleAssignRequest(frame)
	target := req.PlayerNameString()
	if target == "" {
		return
	}
	_ = s.deps.guild.ChangeGuildMemberPositionAsync(st.GuildId, target, wireRoleToGuildPosition(byte(req.Role())))
	s.broadcastGuildAssign(st.GuildId)
}

// handleGuildInfo 处理 C1 66（GuildInfoRequestHandler）：回指定战盟的详情。
func (s *Server) handleGuildInfo(sess *session, frame []byte) {
	if s.deps.guild == nil || sess.getState() != entity.StateEnteredWorld {
		return
	}
	gid := c2s.AsGuildInfoRequest(frame).GuildId()
	name, alliance, logo, ok := s.deps.guild.GetGuildInfoAsync(gid)
	if !ok {
		return
	}
	_ = s.viewFor(sess).SendGuildInformation(gid, 0, alliance, name, logo)
}

// handleGuildMasterAnswer 处理 C1 54（GuildMasterAnswerHandler）：团长选择“创建”时弹创建窗。
func (s *Server) handleGuildMasterAnswer(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	if c2s.AsGuildMasterAnswer(frame).ShowCreationDialog() {
		_ = s.viewFor(sess).SendShowGuildCreationDialog()
	}
}
