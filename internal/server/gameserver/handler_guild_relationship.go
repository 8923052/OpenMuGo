package gameserver

// handler_guild_relationship.go —— 战盟关系变更的入站入口（doc/17 S-3 批次 5）：
// C1 E5 请求 / E6 应答 / E9 同盟清单 / EB 01 把某盟移出同盟 / C1 57 取消建盟窗。
// 对照 MessageHandler/Guild/{GuildRelationshipChange,AllianceListRequest,RemoveAllianceGuild,
// CancelGuildCreation}* 与 PlayerActions/Guild/GuildRelationshipChangeAction。
//
// 敌对(Hostility)的**关系建立/解除**一并落地；但"宣战"本身（61 应答 + 62/63/64 战争进程与
// 积分）需要公会战计时与计分，属公会战域（doc/17 SUB 层）——本轮只登记不实现。

import (
	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/world"
	"mugo/internal/view/remote"

	c2s "mugo/internal/proto/c2s"
)

// relationshipRequest 是挂在被请求团长身上的待应答关系变更
// （对照 Player.PendingAllianceRequest 元组；原版存 Player 引用，本仓存角色名）。
type relationshipRequest struct {
	from         string
	relationship byte
	request      byte
}

// guildBrief 是关系判定需要的战盟摘要。
type guildBrief struct {
	id         uint32
	name       string
	masterID   uint32 // 所在同盟的盟主（不在同盟时为 0）
	inAlliance bool
}

// guildBriefOf 取角色所在战盟的摘要；无盟或战盟查不到时 ok=false。
func (s *Server) guildBriefOf(characterName string) (guildBrief, bool) {
	if s.deps.guild == nil {
		return guildBrief{}, false
	}
	status := s.guildStatusOf(characterName)
	if status.GuildId == 0 {
		return guildBrief{}, false
	}
	name, _, _, ok := s.deps.guild.GetGuildInfoAsync(status.GuildId)
	if !ok {
		return guildBrief{}, false
	}
	master, inAlliance := s.deps.guild.AllianceMasterOfAsync(status.GuildId)
	return guildBrief{id: status.GuildId, name: name, masterID: master, inAlliance: inAlliance}, true
}

// relationshipSourceChecks 对照 CommonChecksAsync 的四道前置：已有待应答请求 →
// RequestCancelled；无盟 → Failed；非团长 → NoAuthorization；盟籍查不到 → GuildNotFound。
func (s *Server) relationshipSourceChecks(sess *session, relationship, request byte) (byte, guildBrief, bool) {
	if sess.pendingRelationship != nil {
		return action.ResultGuildRelationshipRequestCancelled, guildBrief{}, false
	}
	c := sess.getSelected()
	if c == nil {
		return action.ResultGuildRelationshipFailed, guildBrief{}, false
	}
	status := s.guildStatusOf(c.Name)
	if status.GuildId == 0 {
		return action.ResultGuildRelationshipFailed, guildBrief{}, false
	}
	if status.Position != api.GuildPositionGuildMaster {
		return action.ResultGuildRelationshipNoAuthorization, guildBrief{}, false
	}
	brief, ok := s.guildBriefOf(c.Name)
	if !ok {
		return action.ResultGuildRelationshipGuildNotFound, guildBrief{}, false
	}
	return action.ResultGuildRelationshipFailed, brief, true
}

// handleGuildRelationshipRequest 处理 C1 E5：团长向对方团长发起同盟/敌对的建或解。
func (s *Server) handleGuildRelationshipRequest(sess *session, frame []byte) {
	if len(frame) < c2s.GuildRelationshipChangeRequestLength || s.deps.guild == nil {
		return
	}
	me := enteredPlayer(sess)
	if me == nil {
		return
	}
	req := c2s.AsGuildRelationshipChangeRequest(frame)
	relationship, request := byte(req.RelationshipType()), byte(req.RequestType())
	targetID := req.TargetPlayerId()

	// 原版：目标是"自己" + 同盟 + 退出 → 直接走"把自己盟移出同盟"，没有应答环节。
	if targetID == world.ConstantPlayerID && relationship == action.GuildRelationshipAlliance &&
		request == action.GuildRequestLeave {
		s.handleLeaveAlliance(sess, "")
		return
	}

	result, source, ok := s.relationshipSourceChecks(sess, relationship, request)
	if !ok {
		s.sendRelationshipResult(sess, relationship, request, result, targetID)
		return
	}
	targetSess, targetWP := s.sessionByObjectID(me, targetID)
	if targetSess == nil {
		s.sendRelationshipResult(sess, relationship, request, action.ResultGuildRelationshipFailed, targetID)
		return
	}
	targetBrief, ok := s.guildBriefOf(targetWP.Name)
	if !ok || s.guildStatusOf(targetWP.Name).Position != api.GuildPositionGuildMaster {
		s.sendRelationshipResult(sess, relationship, request, action.ResultGuildRelationshipFailed, targetID)
		return
	}
	if conflict, reason := s.relationshipConflict(source, targetBrief, relationship, request); conflict {
		s.sendRelationshipResult(sess, relationship, request, reason, targetID)
		return
	}
	if targetSess.pendingRelationship != nil {
		s.sendRelationshipResult(sess, relationship, request, action.ResultGuildRelationshipRequestCancelled, targetID)
		return
	}
	c := sess.getSelected()
	targetSess.pendingRelationship = &relationshipRequest{from: c.Name, relationship: relationship, request: request}
	if view := s.viewFor(targetSess); view != nil {
		_ = view.SendGuildRelationshipRequest(relationship, request, me.ID)
	}
}

// sessionByObjectID 按"查看者视角的对象 ID"找到对方会话与其世界态。
func (s *Server) sessionByObjectID(viewer *world.Player, objectID uint16) (*session, *world.Player) {
	name := ""
	if objectID == world.ConstantPlayerID || objectID == viewer.ID {
		name = viewer.Name
	} else if p := s.world.Map(viewer.MapNumber).Player(objectID & 0x7FFF); p != nil {
		name = p.Name
	}
	if name == "" {
		return nil, nil
	}
	targetSess := s.sessionOfCharacter(name)
	if targetSess == nil {
		return nil, nil
	}
	wp := targetSess.getWorldPlayer()
	if wp == nil {
		return nil, nil
	}
	return targetSess, wp
}

// relationshipConflict 对照 RequestAsync 的敌对/同盟冲突分支。
func (s *Server) relationshipConflict(source, target guildBrief, relationship, request byte) (bool, byte) {
	switch {
	case relationship == action.GuildRelationshipHostility && request == action.GuildRequestJoin:
		if s.deps.guild.HasAnyHostilityAsync(source.id) || s.deps.guild.HasAnyHostilityAsync(target.id) {
			return true, action.ResultGuildRelationshipAlreadyInHostility
		}
	case relationship == action.GuildRelationshipHostility && request == action.GuildRequestLeave:
		if !s.deps.guild.AreHostileAsync(source.id, target.id) {
			return true, action.ResultGuildRelationshipHostileGuildDoesNotExist
		}
	case relationship == action.GuildRelationshipAlliance && request == action.GuildRequestJoin:
		if target.inAlliance {
			return true, action.ResultGuildRelationshipAlreadyInAlliance
		}
		// 已在同盟、但请求者不是盟主 → 无权（原版比较 sourceGuild.AllianceGuild != sourceGuild）。
		if source.inAlliance && source.masterID != source.id {
			return true, action.ResultGuildRelationshipNoAuthorization
		}
		// 原版还查 Stats.MaximumAllianceSize（>0 才限），S6 导出件里没有该属性 → 恒不限。
	}
	return false, 0
}

// handleGuildRelationshipResponse 处理 C1 E6：被请求的团长给出同意/拒绝。
func (s *Server) handleGuildRelationshipResponse(sess *session, frame []byte) {
	if len(frame) < c2s.GuildRelationshipChangeResponseLength || s.deps.guild == nil {
		return
	}
	pending := sess.pendingRelationship
	sess.pendingRelationship = nil
	if pending == nil {
		return // 原版：没有挂着的请求 → 忽略。
	}
	req := c2s.AsGuildRelationshipChangeResponse(frame)
	relationship, request := byte(req.RelationshipType()), byte(req.RequestType())
	// 原版：应答内容与挂着的请求不符 → 什么也不发。
	if pending.relationship != relationship || pending.request != request {
		return
	}
	responder := enteredPlayer(sess)
	responderChar := sess.getSelected()
	if responder == nil || responderChar == nil {
		return
	}
	requesterSess := s.sessionOfCharacter(pending.from)
	if requesterSess == nil {
		return
	}
	requesterWP := requesterSess.getWorldPlayer()
	requesterGuild, okA := s.guildBriefOf(pending.from)
	responderGuild, okB := s.guildBriefOf(responderChar.Name)
	if !okA || !okB {
		return
	}
	// 双方各自看到"对方团长"的真实对象 ID（自己那条不会出现在这两个包里）。
	masterID := responder.ID
	requesterMasterID := requesterWP.ID
	if !req.Response() {
		s.sendRelationshipResult(requesterSess, relationship, request, action.ResultGuildRelationshipRequestCancelled, masterID)
		return
	}
	result := action.ResultGuildRelationshipFailed
	switch {
	case relationship == action.GuildRelationshipAlliance && request == action.GuildRequestJoin:
		created, err := s.deps.guild.CreateAllianceAsync(requesterGuild.id, responderGuild.id)
		result = allianceResultToWire(created, err)
	case relationship == action.GuildRelationshipHostility:
		ok, err := s.deps.guild.SetHostilityAsync(requesterGuild.id, responderGuild.id, request == action.GuildRequestJoin)
		if ok && err == nil {
			result = action.ResultGuildRelationshipSuccess
		}
	}
	// 原版：结果同时回给请求者与应答者。
	s.sendRelationshipResult(requesterSess, relationship, request, result, masterID)
	s.sendRelationshipResult(sess, relationship, request, result, requesterMasterID)
}

// allianceResultToWire 把战盟服的建盟结果映射为线上结果（对照 ProcessResponseAsync 的 switch）。
func allianceResultToWire(created api.AllianceCreationResult, err error) byte {
	if err != nil {
		return action.ResultGuildRelationshipFailed
	}
	switch created {
	case api.AllianceCreationSuccess:
		return action.ResultGuildRelationshipSuccess
	case api.AllianceCreationMasterGuildNotFound, api.AllianceCreationTargetGuildNotFound:
		return action.ResultGuildRelationshipGuildNotFound
	case api.AllianceCreationTargetGuildAlreadyInAlliance:
		return action.ResultGuildRelationshipAlreadyInAlliance
	case api.AllianceCreationMaximumAllianceSizeReached:
		return action.ResultGuildRelationshipMaximumAllianceSizeReached
	default:
		return action.ResultGuildRelationshipFailed
	}
}

// handleAllianceListRequest 处理 C1 E9（团长看同盟清单）。
func (s *Server) handleAllianceListRequest(sess *session) {
	c := sess.getSelected()
	if s.deps.guild == nil || c == nil {
		return
	}
	status := s.guildStatusOf(c.Name)
	if status.GuildId == 0 {
		return // 原版：无战盟直接 return，不发清单。
	}
	guilds := s.deps.guild.AllianceGuildsAsync(status.GuildId)
	entries := make([]action.AllianceListEntry, 0, len(guilds))
	for _, g := range guilds {
		entries = append(entries, action.AllianceListEntry{
			MemberCount: byte(g.MemberCount), Logo: g.Logo, GuildName: g.GuildName,
		})
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.SendAllianceList(entries)
	}
}

// handleRemoveAllianceGuild 处理 C1 EB 01（把某盟移出同盟；空名=移出自己）。
func (s *Server) handleRemoveAllianceGuild(sess *session, frame []byte) {
	if len(frame) < c2s.RemoveAllianceGuildRequestLength || s.deps.guild == nil {
		return
	}
	name := c2s.AsRemoveAllianceGuildRequest(frame).GuildNameString()
	if name == "" {
		return // 原版：名字空白直接 return（连 CommonChecks 都不走）。
	}
	s.handleLeaveAlliance(sess, name)
}

// handleLeaveAlliance 对照 RequestLeaveAllianceAsync：CommonChecks（不带 targetPlayerId）→
// 移出自己，或（须为同盟盟主）按名移出某个成员盟。
func (s *Server) handleLeaveAlliance(sess *session, targetGuildName string) {
	result, source, ok := s.relationshipSourceChecks(sess, action.GuildRelationshipAlliance, action.GuildRequestLeave)
	if !ok {
		s.sendRelationshipResult(sess, action.GuildRelationshipAlliance, action.GuildRequestLeave, result, 0)
		return
	}
	targetID := source.id
	if targetGuildName != "" && targetGuildName != source.name {
		if !s.deps.guild.IsAllianceMasterAsync(source.id) {
			s.sendRelationshipResult(sess, action.GuildRelationshipAlliance, action.GuildRequestLeave,
				action.ResultGuildRelationshipNoAuthorization, 0)
			return
		}
		targetID = s.deps.guild.GetGuildIdByNameAsync(targetGuildName)
	}
	removed, err := s.deps.guild.RemoveAllianceAsync(targetID)
	if view := s.viewFor(sess); view != nil {
		_ = view.SendRemoveAllianceGuildResult(removed && err == nil)
	}
}

// sendRelationshipResult 回 C1 E6（guildMasterID 是"对方团长"在接收者视角里的 ID）。
func (s *Server) sendRelationshipResult(sess *session, relationship, request, result byte, guildMasterID uint16) {
	if view := s.viewFor(sess); view != nil {
		_ = view.SendGuildRelationshipResult(relationship, request, result, guildMasterID)
	}
}

// handleCancelGuildCreation 处理 C1 57：只有关着"战盟管理员"对话时才生效
// （对照 GuildMasterAnswerAction.ProcessAnswerAsync 的 Cancel 分支：关窗 + 状态回 EnteredWorld）。
func (s *Server) handleCancelGuildCreation(sess *session) {
	n := sess.getOpenedNpc()
	if n == nil || n.Def == nil || int(n.Def.NpcWindow) != remote.NpcWindowGuildMaster {
		return
	}
	sess.setOpenedNpc(nil)
}
