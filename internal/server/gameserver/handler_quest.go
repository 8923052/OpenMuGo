package gameserver

// handler_quest.go —— S10 任务 GS 编排：入站分派对照 GameServer/MessageHandler/Quests/*，
// 流程与判定口径对照 GameLogic/PlayerActions/Quests/*（Select/Start/Completion/Cancel/
// ClientAction + QuestMonsterKillCountPlugIn + PlayerQuestExtensions）。
// 本层只做取数、状态装配、奖励落地与出站调用：判定在 gamelogic/quests，封包在 view/remote。
//
// 组 0（原版 QuestConstants.LegacyQuestGroup，S6 共 16 条）的**对话族**出站是 0xA0~0xA4
// （由 TalkNpcAction 的 LegacyQuest 窗口驱动），随 TRIM-07 的 NPC 窗口映射一并落位；
// 本文件对它照常推进状态与发奖，只少了那几只 legacy 应答包（见 doc/16 TRIM-03）。

import (
	"fmt"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/pricing"
	"mugo/internal/gamelogic/quests"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// 0xF6 入站子码（对照各 *RequestHandlerPlugIn 的 Key）。
const (
	questSubSelect        = 0x0A
	questSubProceed       = 0x0B
	questSubCompletion    = 0x0D
	questSubCancel        = 0x0F
	questSubClientAct     = 0x10
	questSubActiveList    = 0x1A
	questSubState         = 0x1B
	questSubEventState    = 0x21
	questSubAvailableList = 0x30
)

// legacyQuestGroup 对应原版 QuestConstants.LegacyQuestGroup。
const legacyQuestGroup = 0

// questKillProgressFormat 对照 QuestMonsterKillCountPlugInConfiguration.cs:18 的默认模板
// （{0}任务名 {1}怪物名 {2}当前 {3}需求）。
const questKillProgressFormat = "[%s] Defeat %s - %d/%d"

// pointsPerLevelUpAttributeID / comboAvailableAttributeID 是任务属性奖励在 S6 用到的两个
// 属性定义（原版 Stats.cs:102/786，GUID 硬编码，故与 80_attributes.json 逐字一致）。
const (
	pointsPerLevelUpAttributeID = "48074bc6-ddc9-4264-8f1e-004d46d5b6ec"
	comboSkillAttributeID       = "0b648f95-e9c1-4afd-90a6-3dd954bf6995"
)

// handleQuestPacket 按子码分派一条 0xF6 任务请求。
func (s *Server) handleQuestPacket(sess *session, sub byte, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || s.deps.cfg.GameConfig == nil {
		return
	}
	switch sub {
	case questSubSelect:
		s.handleQuestSelect(sess, frame)
	case questSubProceed:
		s.handleQuestProceed(sess, frame)
	case questSubCompletion:
		s.handleQuestCompletion(sess, frame)
	case questSubCancel:
		s.handleQuestCancel(sess, frame)
	case questSubClientAct:
		s.handleQuestClientAction(sess, frame)
	case questSubActiveList:
		s.pushActiveQuests(sess)
	case questSubState:
		s.handleQuestStateRequest(sess, frame)
	case questSubEventState:
		_ = s.viewFor(sess).ShowActiveEventQuests()
	case questSubAvailableList:
		s.pushAvailableQuests(sess)
	}
}

// questStateFor 取（或建）某组的运行时状态。
func questStateFor(c *entity.Character, group int) *entity.QuestState {
	for i := range c.QuestStates {
		if c.QuestStates[i].Group == group {
			return &c.QuestStates[i]
		}
	}
	c.QuestStates = append(c.QuestStates, entity.NewQuestState(group))
	return &c.QuestStates[len(c.QuestStates)-1]
}

// questStateOfGroupReadOnly 只读查询某组状态（不像 questStateFor 那样顺手建一条空记录）。
func questStateOfGroupReadOnly(c *entity.Character, group int) *entity.QuestState {
	for i := range c.QuestStates {
		if c.QuestStates[i].Group == group {
			return &c.QuestStates[i]
		}
	}
	return nil
}

// questStateMatching 对照 PlayerQuestExtensions.GetQuestState(player, group, number)：
// 该组状态的进行中任务号（Number 或 StartingNumber）命中才返回。
func questStateMatching(c *entity.Character, gc *config.GameConfig, group, number int) *entity.QuestState {
	for i := range c.QuestStates {
		st := &c.QuestStates[i]
		if st.Group != group || st.ActiveQuestIndex < 0 {
			continue
		}
		if q := gc.QuestAt(st.ActiveQuestIndex); q != nil && (q.Number == number || q.StartingNumber == number) {
			return st
		}
	}
	return nil
}

// questAtNpc 对照 PlayerQuestExtensions.GetQuest：任务必须在**当前对话 NPC** 的任务表里，
// 且职业合格（原版按 CharacterClass 引用相等）。返回定义与其配置下标。
func (s *Server) questAtNpc(sess *session, c *entity.Character, group, number int) (*config.Quest, int) {
	n := sess.getOpenedNpc()
	if n == nil || n.Def == nil || c == nil {
		return nil, -1
	}
	q, ok := s.deps.cfg.GameConfig.QuestAtNpc(n.Def.Number, group, number, c.ClassNumber)
	if !ok {
		return nil, -1
	}
	return q, s.deps.cfg.GameConfig.QuestIndexOf(q)
}

// handleQuestSelect 0x0A —— 对照 QuestSelectAction。
func (s *Server) handleQuestSelect(sess *session, frame []byte) {
	c := sess.getSelected()
	req := c2s.AsQuestSelectRequest(frame)
	group, number := int(req.QuestGroup()), int(req.QuestNumber())
	q, _ := s.questAtNpc(sess, c, group, number)
	if q == nil {
		return
	}
	st := questStateFor(c, q.Group)
	if st.ActiveQuestIndex >= 0 {
		// 原版：该组已有进行中任务 → 只把它的数据作为进度发回。
		s.pushQuestProgress(sess, c, s.deps.cfg.GameConfig.QuestAt(st.ActiveQuestIndex))
		return
	}
	idx := s.deps.cfg.GameConfig.QuestIndexOf(q)
	if st.LastFinishedQuestIndex == idx && !q.Repeatable {
		return
	}
	if !q.LevelOK(c.Level) {
		return
	}
	if q.StartingNumber == number && q.Number != number {
		_ = s.viewFor(sess).ShowQuestStepInfo(uint16(q.Group), uint16(q.StartingNumber))
	}
}

// handleQuestProceed 0x0B —— 对照 QuestProceedRequestHandlerPlugIn（Accept / Refuse 两支）。
func (s *Server) handleQuestProceed(sess *session, frame []byte) {
	c := sess.getSelected()
	req := c2s.AsQuestProceedRequest(frame)
	group, number := int(req.QuestGroup()), int(req.QuestNumber())
	if req.ProceedAction() != c2s.QuestProceedAction_AcceptQuest {
		s.handleQuestRefused(sess, c, group, number)
		return
	}
	// 已在进行中：保持运行并"确认已开"（原版 QuestStartedAsync(activeQuest)）。
	if st := questStateMatching(c, s.deps.cfg.GameConfig, group, number); st != nil && st.ActiveQuestIndex >= 0 {
		s.questStarted(sess, c, s.deps.cfg.GameConfig.QuestAt(st.ActiveQuestIndex))
		return
	}
	s.startQuest(sess, c, group, number)
}

// handleQuestRefused 拒绝分支：在对话 NPC 的任务表里按 (组, StartingNumber) 找该条，
// 回一张 StepInfo(组, RefuseNumber)。
func (s *Server) handleQuestRefused(sess *session, c *entity.Character, group, number int) {
	n := sess.getOpenedNpc()
	if n == nil || n.Def == nil {
		return
	}
	for _, q := range s.deps.cfg.GameConfig.QuestsForNpc(n.Def.Number) {
		if q.Group == group && q.StartingNumber == number {
			_ = s.viewFor(sess).ShowQuestStepInfo(uint16(q.Group), uint16(q.RefuseNumber))
			return
		}
	}
}

// startQuest 对照 QuestStartAction.StartQuestAsync —— 判定次序（等级→该组已有→可重复→
// 起始费）与"钱不够发蓝字 NotEnoughMoneyToProceed"逐条对齐。
func (s *Server) startQuest(sess *session, c *entity.Character, group, number int) {
	gc := s.deps.cfg.GameConfig
	q, idx := s.questAtNpc(sess, c, group, number)
	if q == nil {
		return
	}
	st := questStateFor(c, q.Group)
	money := int64(0)
	if c.Stats != nil {
		money = int64(c.Stats.Money)
	}
	switch got := quests.CanStart(q, quests.StartContext{
		Level: c.Level, Money: money,
		HasActive: st.ActiveQuestIndex >= 0, IsLastFinished: st.LastFinishedQuestIndex == idx,
	}); got {
	case quests.OutcomeAlreadyActive:
		s.pushQuestProgress(sess, c, gc.QuestAt(st.ActiveQuestIndex))
		return
	case quests.OutcomeNotEnoughMoney:
		// 原版 :66 ShowLocalizedBlueMessageAsync(nameof(PlayerMessage.NotEnoughMoneyToProceed))。
		s.showLocalizedMessage(sess, player.MsgNotEnoughMoneyToProceed)
		return
	case quests.OutcomeOK:
	default:
		s.deps.logger.Printf("gameserver: 任务 %d/%d 开始被拒 %s: outcome=%d", q.Group, q.Number, c.Name, got)
		return
	}
	if q.RequiredStartMoney > 0 {
		c.Stats.Money -= uint32(q.RequiredStartMoney)
		_ = s.viewFor(sess).ShowInventoryMoneyUpdate(c.Stats.Money)
	}
	quests.Start(q, idx, st)
	s.questStarted(sess, c, q)
}

// questStarted 对照 QuestStartedPlugIn.QuestStartedAsync：非 legacy 组回
// StepInfo(号) + 一次进度包；组 0 的 legacy 应答（0xA2）见文件头说明。
func (s *Server) questStarted(sess *session, c *entity.Character, q *config.Quest) {
	if q == nil {
		return
	}
	if q.Group == legacyQuestGroup {
		// 原版：C1 A2 LegacySetQuestStateResponse（QuestStartedPlugIn.cs:45）。
		s.sendLegacyQuestStateResponse(sess, c, q.Number)
		return
	}
	view := s.viewFor(sess)
	_ = view.ShowQuestStepInfo(uint16(q.Group), uint16(q.Number))
	s.pushQuestProgress(sess, c, q)
}

// handleQuestStateRequest 0x1B —— 对照 QuestStateRequestHandlerPlugIn：找不到该组进行中
// 状态时只记日志（原版 LogError），否则回 QuestState。
func (s *Server) handleQuestStateRequest(sess *session, frame []byte) {
	c := sess.getSelected()
	req := c2s.AsQuestStateRequest(frame)
	group, number := int(req.QuestGroup()), int(req.QuestNumber())
	st := questStateMatching(c, s.deps.cfg.GameConfig, group, number)
	if st == nil {
		s.deps.logger.Printf("gameserver: 任务状态未找到 %s group=%d number=%d", c.Name, group, number)
		return
	}
	if group == legacyQuestGroup {
		return
	}
	var active *config.Quest
	if st.ActiveQuestIndex >= 0 {
		active = s.deps.cfg.GameConfig.QuestAt(st.ActiveQuestIndex)
	}
	conds, rewards := s.buildQuestViews(c, active, st)
	// 无进行中任务时原版仍发一帧空详情（计数全 0），保持一致。
	stateNumber := uint16(0)
	if active != nil {
		stateNumber = uint16(active.Number)
	}
	_ = s.viewFor(sess).ShowQuestState(uint16(group), stateNumber, conds, rewards)
}

// handleQuestCompletion 0x0D —— 对照 QuestCompletionAction：判定→扣物品→发奖→清态→应答。
func (s *Server) handleQuestCompletion(sess *session, frame []byte) {
	c := sess.getSelected()
	if c == nil || c.Stats == nil {
		return
	}
	req := c2s.AsQuestCompletionRequest(frame)
	s.completeQuest(sess, c, int(req.QuestGroup()), int(req.QuestNumber()))
}

// completeQuest 完成"该组里号命中 number 的那条进行中任务"（0x0D 与 legacy 0xA2 共用）。
func (s *Server) completeQuest(sess *session, c *entity.Character, group, number int) {
	gc := s.deps.cfg.GameConfig
	st := questStateMatching(c, gc, group, number)
	if st == nil || st.ActiveQuestIndex < 0 {
		return
	}
	q := gc.QuestAt(st.ActiveQuestIndex)
	idx := st.ActiveQuestIndex
	if got := quests.CanComplete(q, idx, st, s.questItemCountGetter(c, gc)); got != quests.OutcomeOK {
		s.deps.logger.Printf("gameserver: 任务 %d/%d 完成被拒 %s: outcome=%d", q.Group, q.Number, c.Name, got)
		return
	}
	quests.Finish(q, idx, st)
	for i := range q.RequiredItems {
		s.destroyQuestItems(c, gc, &q.RequiredItems[i])
	}
	wp := sess.getWorldPlayer()
	view := s.viewFor(sess)
	for i := range q.Rewards {
		s.awardQuestReward(sess, c, wp, view, q, &q.Rewards[i])
	}
	// 发奖之后才清态（原版 ClearAsync 在奖励之后）：属性奖励的 legacy 应答要读到进行中任务。
	quests.Clear(st)
	s.pushActiveQuests(sess)
	if q.Group == legacyQuestGroup {
		// 原版：C1 A2（QuestCompletionResponsePlugIn.cs:36-38）。
		s.sendLegacyQuestStateResponse(sess, c, q.Number)
		return
	}
	_ = view.ShowQuestCompletionResponse(uint16(q.Group), uint16(q.Number), true)
}

// handleQuestCancel 0x0F —— 对照 QuestCancelAction（只认该组**当前进行中**的那条）。
func (s *Server) handleQuestCancel(sess *session, frame []byte) {
	c := sess.getSelected()
	req := c2s.AsQuestCancelRequest(frame)
	s.cancelQuest(sess, c, int(req.QuestGroup()), int(req.QuestNumber()))
}

// cancelQuest 放弃"该组里号命中 number 的那条进行中任务"（0x0F 与 legacy 0xA2 共用）。
func (s *Server) cancelQuest(sess *session, c *entity.Character, group, number int) {
	gc := s.deps.cfg.GameConfig
	st := questStateMatching(c, gc, group, number)
	if st == nil || st.ActiveQuestIndex < 0 {
		return
	}
	q := gc.QuestAt(st.ActiveQuestIndex)
	quests.Clear(st)
	if q.Group == legacyQuestGroup {
		// 原版取消**不**发 A2，而是重发整张 A0 状态表（QuestCancelledPlugIn.cs:36-39）。
		s.sendLegacyQuestStateList(sess)
		return
	}
	_ = s.viewFor(sess).ShowQuestCancelled(uint16(q.Group), uint16(q.Number))
}

// handleQuestClientAction 0x10 —— 对照 QuestClientAction（同样按对话 NPC 取任务）。
func (s *Server) handleQuestClientAction(sess *session, frame []byte) {
	c := sess.getSelected()
	req := c2s.AsQuestClientActionRequest(frame)
	q, idx := s.questAtNpc(sess, c, int(req.QuestGroup()), int(req.QuestNumber()))
	if q == nil {
		return
	}
	quests.MarkClientAction(q, idx, questStateFor(c, q.Group))
}

// pushActiveQuests 下发进行中清单（C1 F6 1A）。对照 Player.cs:1753 与
// CurrentlyActiveQuestsPlugIn：组 0 不进这张表，空清单也发。
func (s *Server) pushActiveQuests(sess *session) {
	c := sess.getSelected()
	view := s.viewFor(sess)
	if c == nil || view == nil {
		return
	}
	list := make([]action.QuestIDView, 0, len(c.QuestStates))
	for i := range c.QuestStates {
		st := &c.QuestStates[i]
		if st.Group == legacyQuestGroup || st.ActiveQuestIndex < 0 {
			continue
		}
		if q := s.deps.cfg.GameConfig.QuestAt(st.ActiveQuestIndex); q != nil {
			list = append(list, action.QuestIDView{Group: uint16(q.Group), Number: uint16(q.Number)})
		}
	}
	_ = view.ShowActiveQuests(list)
}

// pushAvailableQuests 应答 0x30（对照 ShowAvailableQuestsPlugIn）：只对**当前对话 NPC**
// 应答，条目写 StartingNumber，按职业+等级筛（GetAvailableQuestsOfOpenedNpc）。
func (s *Server) pushAvailableQuests(sess *session) {
	c := sess.getSelected()
	view := s.viewFor(sess)
	n := sess.getOpenedNpc()
	if c == nil || view == nil || n == nil || n.Def == nil {
		return
	}
	list := make([]action.QuestIDView, 0, 4)
	for _, q := range s.deps.cfg.GameConfig.QuestsForNpc(n.Def.Number) {
		if !q.IsQualified(c.ClassNumber) || !q.LevelOK(c.Level) {
			continue
		}
		list = append(list, action.QuestIDView{Group: uint16(q.Group), Number: uint16(q.StartingNumber)})
	}
	_ = view.ShowAvailableQuests(uint16(n.Def.Number), list)
}

// pushQuestProgress 把某条任务的当前条件/奖励作为进度包下发（0x0C，组 0 除外）。
func (s *Server) pushQuestProgress(sess *session, c *entity.Character, q *config.Quest) {
	if q == nil || q.Group == legacyQuestGroup {
		return
	}
	st := questStateFor(c, q.Group)
	conds, rewards := s.buildQuestViews(c, q, st)
	_ = s.viewFor(sess).ShowQuestProgress(uint16(q.Group), uint16(q.Number), conds, rewards)
}

// advanceQuestKills 在击杀 monsterNumber 时推进该角色所有进行中任务的击杀计数。
// 对照 QuestMonsterKillCountPlugIn：**每条**命中该怪的击杀需求各加一，并在达标前
// （含正好达标那次）回一条进度蓝字；原版此处**不**发任务进度包。
func (s *Server) advanceQuestKills(sess *session, c *entity.Character, monsterNumber int) {
	if c == nil || s.deps.cfg.GameConfig == nil {
		return
	}
	gc := s.deps.cfg.GameConfig
	monsterName := ""
	if m, ok := gc.Monster(monsterNumber); ok {
		monsterName = m.Name
	}
	for i := range c.QuestStates {
		st := &c.QuestStates[i]
		if st.ActiveQuestIndex < 0 {
			continue
		}
		q := gc.QuestAt(st.ActiveQuestIndex)
		if q == nil {
			continue
		}
		for _, adv := range quests.AdvanceKill(st, q, monsterNumber) {
			if !adv.Report {
				continue
			}
			s.showBlueMessage(sess, fmt.Sprintf(questKillProgressFormat, q.Name, monsterName, adv.Current, adv.Required))
		}
	}
}

// buildQuestViews 组装条件/奖励视图，镜像 QuestStructExtensions.AssignActiveQuestData：
// 有上交物品就只列物品条件，否则列击杀条件；奖励逐项，但**只有**经验/金币/物品
// 能进包（原版 EnumExtensions.Convert 对其余类型抛异常，而这几种只出现在组 0）。
func (s *Server) buildQuestViews(c *entity.Character, q *config.Quest, st *entity.QuestState) ([]action.QuestConditionView, []action.QuestRewardView) {
	gc := s.deps.cfg.GameConfig
	if q == nil {
		return nil, nil
	}
	var conds []action.QuestConditionView
	switch {
	case len(q.RequiredItems) > 0:
		for i := range q.RequiredItems {
			r := &q.RequiredItems[i]
			def, ok := gc.Item(r.ItemGroup, r.ItemNumber)
			if !ok {
				continue
			}
			conds = append(conds, action.QuestConditionView{
				Type: byte(s2c.ConditionType_Item), RequirementID: questItemType(def),
				Required: uint32(r.Count), Current: uint32(questCountDefinition(c, def)),
				ItemData: encodeItemForClient(questConditionPreview(def)),
			})
		}
	case len(q.RequiredKills) > 0:
		for i := range q.RequiredKills {
			r := &q.RequiredKills[i]
			conds = append(conds, action.QuestConditionView{
				Type: byte(s2c.ConditionType_MonsterKills), RequirementID: uint16(r.MonsterNumber),
				Required: uint32(r.Count), Current: uint32(quests.KillCount(st, i)),
			})
		}
	}
	var rewards []action.QuestRewardView
	for i := range q.Rewards {
		rw := &q.Rewards[i]
		v := action.QuestRewardView{Count: uint32(rw.Value)}
		switch rw.Type {
		case config.QuestRewardMoney:
			v.Type = byte(s2c.RewardType_Money)
		case config.QuestRewardExperience:
			v.Type = byte(s2c.RewardType_Experience)
		case config.QuestRewardItem:
			if rw.Item == nil {
				continue
			}
			def, ok := gc.Item(rw.Item.Group, rw.Item.Number)
			if !ok {
				continue
			}
			v.Type = byte(s2c.RewardType_Item)
			v.RewardID = questItemType(def)
			v.ItemData = encodeItemForClient(questRewardItem(gc, rw.Item))
		default:
			continue
		}
		rewards = append(rewards, v)
	}
	return conds, rewards
}

// questItemType 对照原版 GetItemType：组左移 9 位或上编号（每类支持 512 件）。
func questItemType(def *config.Item) uint16 { return uint16((def.Group << 9) | def.Number) }

// questConditionPreview 造物品条件的预览物：只有定义与**满耐久**
// （对照 QuestStructExtensions.cs:178-180 的 TemporaryItem）。
func questConditionPreview(def *config.Item) *item.Item {
	it := &item.Item{Group: byte(def.Group), Number: def.Number}
	it.Durability = pricing.MaximumDurability(def, it)
	return it
}

// questRewardItem 按奖励模板造物（等级/耐久/技能/幸运/选项都取模板值）。
func questRewardItem(gc *config.GameConfig, t *config.QuestItemTemplate) *item.Item {
	return &item.Item{
		Group: byte(t.Group), Number: t.Number, Level: byte(t.Level),
		Durability: byte(t.Durability), HasSkill: t.HasSkill, Luck: t.Luck,
		OptionLevel: t.OptionLevel,
	}
}

// questItemCountGetter 给完成判定用：按定义（+ DropItemGroup 等级）统计背包件数。
func (s *Server) questItemCountGetter(c *entity.Character, gc *config.GameConfig) func(*config.QuestItemRequirement) int {
	return func(r *config.QuestItemRequirement) int {
		def, ok := gc.Item(r.ItemGroup, r.ItemNumber)
		if !ok || c == nil || c.Inventory == nil {
			return 0
		}
		n := 0
		for _, si := range c.Inventory.Grid().Items() {
			if si == nil || si.It == nil || si.It.Group != byte(def.Group) || si.It.Number != def.Number {
				continue
			}
			if r.ItemLevel != nil && int(si.It.Level) != *r.ItemLevel {
				continue
			}
			n++
		}
		return n
	}
}

// questCountDefinition 统计背包中该定义的件数（**不按等级筛** —— 与包里的 CurrentCount 口径一致，
// 见 QuestStructExtensions.cs:176）。
func questCountDefinition(c *entity.Character, def *config.Item) int {
	if c == nil || c.Inventory == nil || def == nil {
		return 0
	}
	n := 0
	for _, si := range c.Inventory.Grid().Items() {
		if si != nil && si.It != nil && si.It.Group == byte(def.Group) && si.It.Number == def.Number {
			n++
		}
	}
	return n
}

// destroyQuestItems 上交并销毁需求物品（Take(MinimumNumber) 件，等级口径同完成判定）。
func (s *Server) destroyQuestItems(c *entity.Character, gc *config.GameConfig, r *config.QuestItemRequirement) {
	def, ok := gc.Item(r.ItemGroup, r.ItemNumber)
	if !ok || c == nil || c.Inventory == nil {
		return
	}
	var toRemove []*storage.SlottedItem
	for _, si := range c.Inventory.Grid().Items() {
		if si == nil || si.It == nil || si.It.Group != byte(def.Group) || si.It.Number != def.Number {
			continue
		}
		if r.ItemLevel != nil && int(si.It.Level) != *r.ItemLevel {
			continue
		}
		toRemove = append(toRemove, si)
		if len(toRemove) >= r.Count {
			break
		}
	}
	for _, si := range toRemove {
		c.Inventory.Remove(si)
	}
}

// awardQuestReward 落地一条奖励，逐类型对照 QuestCompletionAction.AddRewardAsync。
func (s *Server) awardQuestReward(sess *session, c *entity.Character, wp *world.Player, view action.PlayerView, q *config.Quest, rw *config.QuestReward) {
	gc := s.deps.cfg.GameConfig
	switch rw.Type {
	case config.QuestRewardExperience:
		s.awardQuestExperience(sess, c, wp, view, int64(rw.Value))
	case config.QuestRewardMoney:
		c.Stats.Money += uint32(rw.Value)
		if view != nil {
			_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
		}
	case config.QuestRewardItem:
		s.awardQuestItem(c, wp, view, gc, rw.Item)
	case config.QuestRewardLevelUpPoints:
		c.Stats.LevelUpPoints += uint16(rw.Value)
		s.showQuestRewardToSelf(sess, view, action.QuestLegacyRewardLevelUpPoints, byte(rw.Value))
	case config.QuestRewardAttribute:
		s.awardQuestAttribute(sess, c, view, q, rw)
	case config.QuestRewardSkill:
		s.awardQuestSkill(c, view, rw)
	case config.QuestRewardEvolutionFirstToSecond, config.QuestRewardEvolutionSecondToThird:
		s.awardQuestEvolution(sess, c, rw, rw.Type == config.QuestRewardEvolutionSecondToThird)
	default:
		// GensAttribution / Undefined：原版亦未实现（QuestCompletionAction.cs:166）。
		s.deps.logger.Printf("gameserver: 任务 %d/%d 奖励类型 %s 原版未实现", q.Group, q.Number, rw.Type)
	}
}

// awardQuestExperience 给固定经验（复用击杀经验那条链，含升级广播）。
func (s *Server) awardQuestExperience(sess *session, c *entity.Character, wp *world.Player, view action.PlayerView, amount int64) {
	gc := s.deps.cfg.GameConfig
	if amount <= 0 || gc == nil || c.Stats == nil {
		return
	}
	gain := action.ApplyExperience(int(c.Level), int64(c.Stats.Experience), amount, gc.Experience.Table, gc.Experience.MaximumLevel, gc.Globals.PreventExperienceOverflow)
	for _, step := range gain.Steps {
		if view != nil {
			_ = view.ShowExperienceGained(action.ExperienceGainPacket{
				Result: step.Type, Experience: uint32(step.Amount), KillerObjectID: constantPlayerID,
			})
		}
		if step.LeveledUp {
			c.Level = uint16(step.Level)
			s.applyLevelUp(sess, c, wp, view)
		}
	}
	c.Level = uint16(gain.NewLevel)
	c.Stats.Experience = uint64(gain.NewExperience)
	if next := gc.ExperienceForLevel(gain.NewLevel + 1); next >= 0 {
		c.Stats.ExperienceNext = uint64(next)
	}
}

// awardQuestItem 造一件奖励物品入背包；满了按原版丢在脚下（QuestCompletionAction.cs:128）。
// 注意：原版**不看 Value**，物品奖励恒为 1 件（:119-131 只 CreateNew 一次）。
func (s *Server) awardQuestItem(c *entity.Character, wp *world.Player, view action.PlayerView, gc *config.GameConfig, t *config.QuestItemTemplate) {
	if t == nil || c == nil {
		return
	}
	if _, ok := gc.Item(t.Group, t.Number); !ok {
		return
	}
	it := questRewardItem(gc, t)
	inv := s.ensureInventory(c)
	slot := inv.Count()
	si := s.newSlottedItem(it)
	if inv.AddToFree(si) {
		if view != nil {
			_ = view.ShowItemAddedToInventory(byte(slot), encodeItemForClient(it))
		}
		return
	}
	if wp == nil {
		return
	}
	m := s.world.Map(wp.MapNumber)
	id := s.deps.cfg.Drops.AddItem(wp.MapNumber, wp.X, wp.Y, it, time.Now())
	s.showDroppedTo(m, wp.X, wp.Y,
		[]action.DropEntry{{ID: id, X: wp.X, Y: wp.Y, Data: encodeItemForClient(it), FreshDrop: true}}, nil)
}

// awardQuestAttribute 属性奖励：累加角色级属性（对照 :100-109），
// "每级点数"额外补发等级差 × 点数，并按属性走 legacy 应答（ComboSkill / PointsPerLevel）。
func (s *Server) awardQuestAttribute(sess *session, c *entity.Character, view action.PlayerView, q *config.Quest, rw *config.QuestReward) {
	if rw.Attribute == nil {
		return
	}
	addAttributeBonus(c, rw.Attribute.ID, rw.Attribute.Designation, float64(rw.Value))
	switch rw.Attribute.ID {
	case pointsPerLevelUpAttributeID:
		// 补偿晚接任务少拿的点数（:111-115 用角色等级与该条任务的最低等级）。
		c.Stats.LevelUpPoints += uint16((int(c.Level) - q.MinLevel) * rw.Value)
		questLevel := 220 // 原版取 legacy 组进行中任务的最低等级，缺省 220
		if st := questStateFor(c, legacyQuestGroup); st.ActiveQuestIndex >= 0 {
			if lq := s.deps.cfg.GameConfig.QuestAt(st.ActiveQuestIndex); lq != nil {
				questLevel = lq.MinLevel
			}
		}
		s.showQuestRewardToSelf(sess, view, action.QuestLegacyRewardPointsPerLevel, byte((int(c.Level)-questLevel)*rw.Value))
	case comboSkillAttributeID:
		s.showQuestRewardToSelf(sess, view, action.QuestLegacyRewardComboSkill, 0)
	}
	s.refreshCombatValues(sess, c, false)
}

// addAttributeBonus 把一条属性奖励累加进角色级加成表（对照 Character.Attributes 的 StatAttribute）。
func addAttributeBonus(c *entity.Character, id, designation string, value float64) {
	for i := range c.AttributeBonuses {
		if c.AttributeBonuses[i].AttributeID == id {
			c.AttributeBonuses[i].Value += value
			return
		}
	}
	c.AttributeBonuses = append(c.AttributeBonuses, entity.AttributeBonus{
		AttributeID: id, Designation: designation, Value: value,
	})
}

// awardQuestSkill 奖励技能：已学则跳过（对照 :169-190 的 ContainsSkill 判断与 Level=0）。
func (s *Server) awardQuestSkill(c *entity.Character, view action.PlayerView, rw *config.QuestReward) {
	if rw.Skill == nil || learnedSkillContains(c, rw.Skill.Number) {
		return
	}
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: uint16(rw.Skill.Number)})
	if view != nil {
		_ = view.ShowSkillList(s.skillListViewOf(c))
	}
}

// awardQuestEvolution 转职奖励：换到下一职（对照 :136-157），并把 legacy 奖励包广播给
// 视野内玩家（sendToSelf=true，自己收 0x200 视角号）；2→3 还追发大师状态（TRIM-09 已接）。
func (s *Server) awardQuestEvolution(sess *session, c *entity.Character, rw *config.QuestReward, toThird bool) {
	gc := s.deps.cfg.GameConfig
	class, ok := gc.Class(int(c.ClassNumber))
	if !ok || class.NextClass == nil {
		s.deps.logger.Printf("gameserver: 任务转职奖励 %s 无下一职业可转", c.Name)
		return
	}
	next, ok := gc.Class(*class.NextClass)
	if !ok {
		s.deps.logger.Printf("gameserver: 任务转职奖励目标职业 %d 不存在", *class.NextClass)
		return
	}
	c.ClassNumber = byte(next.Number)
	reward := action.QuestLegacyRewardEvolutionFirstToSecond
	if toThird {
		reward = action.QuestLegacyRewardEvolutionSecondToThird
	}
	count := byte(next.Number << 3)
	self := sess.getWorldPlayer()
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowQuestRewardAnnouncement(constantPlayerID, reward, count)
	}
	if self != nil {
		for _, o := range s.world.Map(self.MapNumber).PlayersInRangeFor(self.X, self.Y) {
			if o.ID == self.ID || o.View == nil {
				continue
			}
			_ = o.View.ShowQuestRewardAnnouncement(self.ID, reward, count)
		}
		s.refreshCombatValues(sess, c, false)
		if toThird {
			// 2→3 之后原版紧接着 IUpdateMasterStatsPlugIn.SendMasterStatsAsync
			// （大师职业刚成立，客户端要立刻看到 F3 50 + 大师技能列表）——TRIM-09 已接。
			s.sendMasterStats(sess, c, c.Stats)
		}
	}
}

// showQuestRewardToSelf 给自己发一条 legacy 任务奖励应答（原版 InvokeViewPlugInAsync，
// 只发给本人；转职那条才是 ForEachWorldObserverAsync）。
func (s *Server) showQuestRewardToSelf(sess *session, view action.PlayerView, reward action.QuestLegacyReward, count byte) {
	if view == nil {
		view = s.viewFor(sess)
	}
	if view != nil {
		_ = view.ShowQuestRewardAnnouncement(constantPlayerID, reward, count)
	}
}
