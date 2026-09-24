package gameserver

// handler_party.go —— S2 组队的入站处理与出站编排，对照原版 MessageHandler/Party/*
// 与 RemoteView/Party/*。入站 code：0x40 邀请、0x41 应答、0x42 列表请求、0x43 踢人。
// 出站广播按成员名解析在线视图；被邀请者/成员对象 ID 用其世界 ID（同图唯一）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// handlePartyInvite 处理 C1 40（对照 PartyRequestAction）：队长/自由人可邀，目标须无队无待决。
func (s *Server) handlePartyInvite(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil || !s.parties.canInvite(wp.Name) {
		return
	}
	targetID := c2s.AsPartyInviteRequest(frame).TargetPlayerId() & 0x7FFF
	tp := s.world.Map(wp.MapNumber).Player(targetID)
	if tp == nil || tp.Name == wp.Name || s.parties.partyOf(tp.Name) != nil || s.parties.hasInvite(tp.Name) {
		return
	}
	s.parties.setInvite(tp.Name, wp.Name)
	if tp.View != nil {
		_ = tp.View.ShowPartyRequest(wp.ID)
	}
}

// handlePartyResponse 处理 C1 41（对照 PartyResponseAction）：接受则建队/入队并广播列表。
func (s *Server) handlePartyResponse(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	inviter, ok := s.parties.takeInvite(wp.Name)
	if !ok || !c2s.AsPartyInviteResponse(frame).Accepted() {
		return // 无待决邀请或拒绝：OpenMU 不回显邀请者
	}
	members, added := s.parties.acceptInvite(inviter, wp.Name)
	if !added {
		return
	}
	s.broadcastPartyList(members)
	s.broadcastPartyHealth(members)
}

// handlePartyListRequest 处理 C1 42（对照 PartyListRequestAction）：把当前列表发给请求者。
func (s *Server) handlePartyListRequest(sess *session) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	members := s.parties.membersOf(wp.Name)
	if members == nil {
		return
	}
	views := s.partyMembersView(members)
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowPartyList(byte(indexOfName(members, wp.Name)), views)
	}
}

// handlePartyKick 处理 C1 43（对照 PartyKickAction）：仅队长或自踢。
func (s *Server) handlePartyKick(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	members := s.parties.membersOf(wp.Name)
	if members == nil {
		return
	}
	idx := int(c2s.AsPartyPlayerKickRequest(frame).PlayerIndex())
	if idx < 0 || idx >= len(members) {
		return
	}
	isMaster := members[0] == wp.Name
	isSelf := members[idx] == wp.Name
	if !isMaster && !isSelf {
		s.deps.logger.Printf("gameserver: 非法组队踢人 %s idx=%d", wp.Name, idx)
		return
	}
	s.partyRemove(members[idx])
}

// handlePartyChat 处理 ~ 前缀的组队聊天（对照 ChatMessagePartyProcessor → Party.SendChatMessageAsync）。
func (s *Server) handlePartyChat(wp *world.Player, message string) {
	members := s.parties.membersOf(wp.Name)
	if members == nil {
		return
	}
	for _, mem := range members {
		if sess := s.sessionOfCharacter(mem); sess != nil {
			if view := s.viewFor(sess); view != nil {
				_ = view.ShowChatMessage(wp.Name, message, action.ChatParty)
			}
		}
	}
}

// partyRemove 移出成员并广播（对照 ExitPartyAsync）：解散时逐个发移除包，否则发移除包 + 剩余刷新。
func (s *Server) partyRemove(name string) {
	res, ok := s.parties.remove(name)
	if !ok {
		return
	}
	if res.disbanded {
		for i, mem := range res.oldMembers {
			if sess := s.sessionOfCharacter(mem); sess != nil {
				_ = s.viewFor(sess).ShowPartyMemberRemoved(byte(i))
			}
		}
		return
	}
	if sess := s.sessionOfCharacter(res.member); sess != nil {
		_ = s.viewFor(sess).ShowPartyMemberRemoved(byte(res.index))
	}
	s.broadcastPartyList(res.remaining)
}

// broadcastPartyList 给每名成员发一份"以自己为槽位"的组队列表。
func (s *Server) broadcastPartyList(members []string) {
	if len(members) == 0 {
		return
	}
	views := s.partyMembersView(members)
	for i, mem := range members {
		if sess := s.sessionOfCharacter(mem); sess != nil {
			_ = s.viewFor(sess).ShowPartyList(byte(i), views)
		}
	}
}

// broadcastPartyHealth 给每名成员发一份组队血条（对照 PartyHealthViewPlugIn：值 = 血/上限×10）。
func (s *Server) broadcastPartyHealth(members []string) {
	if len(members) == 0 {
		return
	}
	health := make([]action.PartyHealthView, len(members))
	for i, mem := range members {
		health[i] = action.PartyHealthView{Index: byte(i), Value: s.partyHealthValue(mem)}
	}
	for _, mem := range members {
		if sess := s.sessionOfCharacter(mem); sess != nil {
			_ = s.viewFor(sess).ShowPartyHealth(health)
		}
	}
}

// partyMembersView 把成员名列表解析成 PartyList 条目（离线成员跳过其血量但仍占槽）。
func (s *Server) partyMembersView(members []string) []action.PartyMemberView {
	views := make([]action.PartyMemberView, 0, len(members))
	for i, mem := range members {
		v := action.PartyMemberView{Index: byte(i), Name: mem}
		if wp := s.worldPlayerByName(mem); wp != nil {
			v.MapID = byte(wp.MapNumber)
			v.X, v.Y = wp.X, wp.Y
			if wp.Stats != nil {
				v.CurrentHealth = wp.Stats.CurrentHealth
				v.MaximumHealth = wp.Stats.MaximumHealth
			}
		}
		views = append(views, v)
	}
	return views
}

// partyHealthValue 返回成员血量槽值 0..10。
func (s *Server) partyHealthValue(name string) byte {
	wp := s.worldPlayerByName(name)
	if wp == nil || wp.Stats == nil || wp.Stats.MaximumHealth == 0 {
		return 0
	}
	v := float64(wp.Stats.CurrentHealth) / float64(wp.Stats.MaximumHealth) * 10
	if v > 10 {
		v = 10
	}
	return byte(v)
}

// worldPlayerByName 按角色名（不区分大小写）查在线世界玩家，无则 nil。
func (s *Server) worldPlayerByName(name string) *world.Player {
	if sess := s.sessionOfCharacter(name); sess != nil {
		return sess.getWorldPlayer()
	}
	return nil
}

// indexOfName 返回 name 在 members 中的下标（无则 -1）。
func indexOfName(members []string, name string) int {
	for i, m := range members {
		if m == name {
			return i
		}
	}
	return -1
}
