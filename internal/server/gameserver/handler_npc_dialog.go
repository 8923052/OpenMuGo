package gameserver

// handler_npc_dialog.go —— 两类"有专门应答"的 NPC 窗口：
//   - NpcWindow.LegacyQuest（28）：TalkNpcAction.ShowLegacyQuestDialogAsync（:147-184）
//     的三段门槛 + C1 A1 对话框 +（该任务有击杀需求时）C1 A4 击杀进度；
//   - NpcWindow.GuildMaster（25）：IsPlayedAllowedToCreateGuildAsync（:186-201）+ C1 54。
//
// 气泡文案是原版**硬编码英文**（不走 PlayerMessage.resx），按原样照抄：
// IShowMessageOfObjectPlugIn → C1 01 ObjectMessage（对象号 + UTF-8 + NUL）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/quests"
	c2s "mugo/internal/proto/c2s"
)

// legacy 任务与会话气泡文案（逐字对照 TalkNpcAction.cs 的行号）。
const (
	msgNoQuestsForYou   = "I have no quests for you."
	msgComeBackStronger = "I have nothing to do for you. Come back with more power."
	msgAllQuestsSolved  = "I have nothing to do for you. You solved all my quests already."
	msgLevelForGuild    = "Your level should be at least level 100"
	msgAlreadyInGuild   = "You already belong to a guild"
)

// showLegacyQuestDialog 对照 ShowLegacyQuestDialogAsync：三段拒绝各回一条气泡并关对话，
// 通过则下发对话框（对话框内部再决定"展示哪一条任务"）。
func (s *Server) showLegacyQuestDialog(sess *session, target *npc.Npc) {
	gc := s.deps.cfg.GameConfig
	c := sess.getSelected()
	if gc == nil || c == nil || target == nil || target.Def == nil {
		return
	}
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	// 第 1 段：该 NPC 的任务里有没有本职业可接的（原版只按 QualifiedCharacter 筛）。
	qualified := legacyQuestsOfNpc(gc, target.Def.Number, c.ClassNumber)
	if len(qualified) == 0 {
		s.closeNpcDialogWithMessage(sess, target, msgNoQuestsForYou)
		return
	}
	// 第 2 段：全部都要更高的等级。
	allTooHigh := true
	for _, q := range qualified {
		if q.MinLevel <= int(c.Level) {
			allTooHigh = false
			break
		}
	}
	if allTooHigh {
		s.closeNpcDialogWithMessage(sess, target, msgComeBackStronger)
		return
	}
	// 第 3 段：这一位 NPC 的最高任务号都已完成过 → "全做完了"。
	maxNumber := 0
	group := 0
	for _, q := range qualified {
		if q.Number > maxNumber {
			maxNumber, group = q.Number, q.Group
		}
	}
	st := questStateOfGroupReadOnly(c, group)
	if st != nil && st.LastFinishedQuestIndex >= 0 &&
		gc.QuestAt(st.LastFinishedQuestIndex).Number >= maxNumber {
		s.closeNpcDialogWithMessage(sess, target, msgAllQuestsSolved)
		return
	}

	index, kills := s.legacyDialogQuest(sess, target, c)
	stateByte := quests.StateByte(s.legacyFacts(c, target, gc))
	_ = view.ShowLegacyQuestStateDialog(index, stateByte)
	if len(kills) > 0 {
		views := make([]action.LegacyKillView, 0, len(kills))
		for _, k := range kills {
			views = append(views, action.LegacyKillView{MonsterNumber: k.monster, Count: k.count})
		}
		_ = view.ShowLegacyQuestMonsterKillInfo(index, views)
	}
}

// legacyDialogQuest 复刻 LegacyQuestStateDialogPlugIn.cs:41-45：
// 展示对象 = 进行中的那条 ?? 下一条可接 ?? 上次完成的那条；并带回它的击杀进度。
func (s *Server) legacyDialogQuest(sess *session, target *npc.Npc, c *entity.Character) (byte, []questViewKill) {
	gc := s.deps.cfg.GameConfig
	st := questStateOfGroupReadOnly(c, legacyQuestGroup)
	if st != nil && st.ActiveQuestIndex >= 0 {
		q := gc.QuestAt(st.ActiveQuestIndex)
		return byte(q.Number), s.legacyKillProgress(st, q)
	}
	if next := s.nextLegacyQuest(target, c, gc); next != nil {
		return byte(next.Number), s.legacyKillProgress(nil, next)
	}
	if st != nil && st.LastFinishedQuestIndex >= 0 {
		q := gc.QuestAt(st.LastFinishedQuestIndex)
		return byte(q.Number), s.legacyKillProgress(st, q)
	}
	return 0, nil
}

// nextLegacyQuest 对照 GetNextLegacyQuest：可接清单（职业 + 等级）按号升序，
// 取第一条号大于"上次完成任务号"的（无上次完成则从最小号起）。
func (s *Server) nextLegacyQuest(target *npc.Npc, c *entity.Character, gc *config.GameConfig) *config.Quest {
	if target == nil || target.Def == nil {
		return nil
	}
	last := -1
	if st := questStateOfGroupReadOnly(c, legacyQuestGroup); st != nil && st.LastFinishedQuestIndex >= 0 {
		last = gc.QuestAt(st.LastFinishedQuestIndex).Number
	}
	var best *config.Quest
	for _, q := range legacyQuestsOfNpc(gc, target.Def.Number, c.ClassNumber) {
		if !q.LevelOK(c.Level) || q.Number <= last {
			continue
		}
		if best == nil || q.Number < best.Number {
			best = q
		}
	}
	return best
}

// legacyFacts 组装状态位所需事实（打包规则在 quests.StateByte，逐字对照原版）。
func (s *Server) legacyFacts(c *entity.Character, target *npc.Npc, gc *config.GameConfig) quests.LegacyFacts {
	f := quests.LegacyFacts{DarkKnightBase: s.isDarkKnightBase(c)}
	st := questStateOfGroupReadOnly(c, legacyQuestGroup)
	if st == nil {
		return f
	}
	f.HasState = true
	if st.LastFinishedQuestIndex >= 0 {
		f.HasLast, f.LastFinished = true, gc.QuestAt(st.LastFinishedQuestIndex).Number
	}
	if st.ActiveQuestIndex >= 0 {
		f.HasActive, f.Active = true, gc.QuestAt(st.ActiveQuestIndex).Number
	}
	switch {
	case f.HasActive:
		f.Anchor, f.HasAnchor = f.Active, true
	default:
		if next := s.nextLegacyQuest(target, c, gc); next != nil {
			f.Anchor, f.HasAnchor = next.Number, true
		} else if f.HasLast {
			f.Anchor, f.HasAnchor = f.LastFinished, true
		}
	}
	return f
}

// legacyStateList 是 C1 A0 的 7 槽表（进图、0xA0 请求与取消任务后重发共用）。
func (s *Server) legacyStateList(c *entity.Character, gc *config.GameConfig) [7]byte {
	f := quests.LegacyFacts{DarkKnightBase: s.isDarkKnightBase(c)}
	if st := questStateOfGroupReadOnly(c, legacyQuestGroup); st != nil {
		f.HasState = true
		if st.LastFinishedQuestIndex >= 0 {
			f.HasLast, f.LastFinished = true, gc.QuestAt(st.LastFinishedQuestIndex).Number
		}
		if st.ActiveQuestIndex >= 0 {
			f.HasActive, f.Active = true, gc.QuestAt(st.ActiveQuestIndex).Number
		}
	}
	states := quests.StateList(f)
	var out [7]byte
	for i, v := range states {
		out[i] = byte(v)
	}
	return out
}

// isDarkKnightBase 对照 QuestStructExtensions.cs:57：基础职业（沿 NextClass 链回溯到一阶）
// 不是暗骑士时，"黑暗石"那一槽要标成 Undefined。
func (s *Server) isDarkKnightBase(c *entity.Character) bool {
	gc := s.deps.cfg.GameConfig
	if gc == nil || c == nil {
		return false
	}
	return gc.BaseClassNumber(int(c.ClassNumber)) == int(player.ClassDarkKnight)
}

// closeNpcDialogWithMessage 是原版三段拒绝的共同收尾：气泡 → 清对话 → 回 EnteredWorld。
func (s *Server) closeNpcDialogWithMessage(sess *session, target *npc.Npc, message string) {
	objectID := target.ID
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowObjectMessage(objectID, message)
	}
	sess.setOpenedNpc(nil)
}

// showGuildMasterDialog 对照 GuildMaster 分支：等级不足或已在盟 → 气泡 + 关对话；
// 否则 C1 54 让客户端弹建盟窗。
func (s *Server) showGuildMasterDialog(sess *session, target *npc.Npc) {
	c := sess.getSelected()
	view := s.viewFor(sess)
	if c == nil || view == nil {
		return
	}
	switch {
	case c.Level < 100:
		s.closeNpcDialogWithMessage(sess, target, msgLevelForGuild)
		return
	case s.guildStatusOf(c.Name).GuildId != 0:
		s.closeNpcDialogWithMessage(sess, target, msgAlreadyInGuild)
		return
	}
	_ = view.ShowGuildMasterDialog()
}

// sendLegacyQuestStateResponse 下发 C1 A2（接/交 legacy 任务的应答）。
// result 恒 0（成功）——原版两处调用者都传 0。
func (s *Server) sendLegacyQuestStateResponse(sess *session, c *entity.Character, questNumber int) {
	view := s.viewFor(sess)
	if view == nil || c == nil || s.deps.cfg.GameConfig == nil {
		return
	}
	_ = view.ShowLegacySetQuestStateResponse(byte(questNumber), 0,
		quests.StateByte(s.legacyFacts(c, sess.getOpenedNpc(), s.deps.cfg.GameConfig)))
}

// sendLegacyQuestStateList 下发 C1 A0（进图、0xA0 请求、取消任务后重发共用）。
func (s *Server) sendLegacyQuestStateList(sess *session) {
	c := sess.getSelected()
	gc := s.deps.cfg.GameConfig
	view := s.viewFor(sess)
	if c == nil || gc == nil || view == nil {
		return
	}
	_ = view.ShowLegacyQuestStateList(s.legacyStateList(c, gc))
}

// handleLegacyQuestStateList 处理 C1 A0：回 7 槽状态表。
// 原版插件挂 [MinimumClient(0, 90)]，MuMain(106,3) 满足下限，故这条路径对真机有效。
func (s *Server) handleLegacyQuestStateList(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	s.sendLegacyQuestStateList(sess)
}

// handleLegacyQuestStateSet 处理 C1 A2（QuestStateSetLegacyRequestHandlerPlugIn.cs:34-60）：
// Active → 该号尚未进行中就"开始"，否则"完成"；Complete → 完成；Inactive → 取消。
// 一律作用于组 0，且**不要求**开着 NPC 对话（原版无此校验）。
func (s *Server) handleLegacyQuestStateSet(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.LegacyQuestStateSetRequestLength {
		return
	}
	c := sess.getSelected()
	if c == nil {
		return
	}
	req := c2s.AsLegacyQuestStateSetRequest(frame)
	number := int(req.QuestNumber())
	switch req.NewState() {
	case c2s.LegacyQuestState_Active:
		if questStateMatching(c, s.deps.cfg.GameConfig, legacyQuestGroup, number) == nil {
			s.startQuest(sess, c, legacyQuestGroup, number)
			return
		}
		s.completeQuest(sess, c, legacyQuestGroup, number)
	case c2s.LegacyQuestState_Complete:
		s.completeQuest(sess, c, legacyQuestGroup, number)
	case c2s.LegacyQuestState_Inactive:
		s.cancelQuest(sess, c, legacyQuestGroup, number)
	default:
		s.deps.logger.Printf("gameserver: legacy 任务状态请求状态值未知 %s number=%d state=%d",
			c.Name, number, req.NewState())
	}
}

// questViewKill 是一条击杀进度条目（A4 包的 8 字节条目）。
type questViewKill struct {
	monster int
	count   int
}

// legacyKillProgress 取某条任务当前的击杀进度（对照 RequirementStates 查表）。
func (s *Server) legacyKillProgress(st *entity.QuestState, q *config.Quest) []questViewKill {
	if q == nil || len(q.RequiredKills) == 0 {
		return nil
	}
	out := make([]questViewKill, 0, len(q.RequiredKills))
	for i := range q.RequiredKills {
		out = append(out, questViewKill{
			monster: q.RequiredKills[i].MonsterNumber,
			count:   quests.KillCount(st, i),
		})
	}
	return out
}

// legacyQuestsOfNpc 是该 NPC 任务表里本职业可接的那些（原版第一段只按职业筛，不看等级）。
func legacyQuestsOfNpc(gc *config.GameConfig, npcNumber int, classNumber byte) []*config.Quest {
	var out []*config.Quest
	for _, q := range gc.QuestsForNpc(npcNumber) {
		if q.Group != legacyQuestGroup || !q.IsQualified(classNumber) {
			continue
		}
		out = append(out, q)
	}
	return out
}
