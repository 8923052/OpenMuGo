package remote

// quest_view.go —— S10 任务出站，对应 OpenMU `GameServer/RemoteView/Quest/*`。
//
// 版本轴（铁律：变体只在 view/remote 里选）：0x1B QuestState 与 0x0C QuestProgress 各有
// C1（251B）与 C2 Extended（272B）两种布局，Extended 的实现挂 [MinimumClient(106, 3)]
// （QuestStateResponseExtendedPlugIn.cs:21 / QuestProgressExtendedPlugIn.cs:21），原版插件容器
// "取满足下限中最高者"，故真机 MuMain(Season 106 Episode 3 / 20404) 收到的是 **C2 Extended**。
//
// 组 0（legacy 任务）的 0xA0~0xA4 一族不在本文件：它由 NPC 对话窗驱动，见 doc/16 TRIM-07。

import (
	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// questExtendedMin 是两个 Extended 变体共同的客户端下限（对照 MinimumClient(106, 3)）。
var questExtendedMin = version.AtLeast(version.ClientVersion{
	Season: 106, Episode: 3, Language: version.LanguageInvariant,
})

// usesExtendedQuestState 报告 0x1B/0x0C 是否应发 Extended 变体。
func (v *PlayerView) usesExtendedQuestState() bool {
	return questExtendedMin.Suitable(v.clientVersion)
}

// questConditionSetter 同时满足 s2c.QuestCondition 与 QuestConditionExtended。
type questConditionSetter interface {
	SetType(s2c.ConditionType)
	SetRequirementId(uint16)
	SetRequiredCount(uint32)
	SetCurrentCount(uint32)
	RequiredItemData() []byte
}

// questRewardSetter 同时满足 s2c.QuestReward 与 QuestRewardExtended。
type questRewardSetter interface {
	SetType(s2c.RewardType)
	SetRewardId(uint16)
	SetRewardCount(uint32)
	RewardedItemData() []byte
}

func fillQuestCondition(c questConditionSetter, cd action.QuestConditionView) {
	c.SetType(s2c.ConditionType(cd.Type))
	c.SetRequirementId(cd.RequirementID)
	c.SetRequiredCount(cd.Required)
	c.SetCurrentCount(cd.Current)
	if cd.Type == byte(s2c.ConditionType_Item) && len(cd.ItemData) > 0 {
		copy(c.RequiredItemData(), cd.ItemData)
	}
}

func fillQuestReward(r questRewardSetter, rw action.QuestRewardView) {
	r.SetType(s2c.RewardType(rw.Type))
	r.SetRewardId(rw.RewardID)
	r.SetRewardCount(rw.Count)
	if rw.Type == byte(s2c.RewardType_Item) && len(rw.ItemData) > 0 {
		copy(r.RewardedItemData(), rw.ItemData)
	}
}

// questDetailWriter 写入一组条件/奖励（QuestState 与 QuestProgress 结构完全同构，
// 原版也是直接把 progress 包当 state 包填 —— QuestProgressPlugIn.cs:54）。
type questDetailWriter struct {
	setConditionCount func(byte)
	setRewardCount    func(byte)
	condition         func(int) questConditionSetter
	reward            func(int) questRewardSetter
	bytes             func() []byte
}

func (w questDetailWriter) write(conds []action.QuestConditionView, rewards []action.QuestRewardView) []byte {
	w.setConditionCount(byte(len(conds)))
	for i, cd := range conds {
		if c := w.condition(i); c != nil {
			fillQuestCondition(c, cd)
		}
	}
	w.setRewardCount(byte(len(rewards)))
	for i, rw := range rewards {
		if r := w.reward(i); r != nil {
			fillQuestReward(r, rw)
		}
	}
	return w.bytes()
}

func questStateWriter(p *s2c.QuestState, group, number uint16) questDetailWriter {
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	return questDetailWriter{
		setConditionCount: p.SetConditionCount,
		setRewardCount:    p.SetRewardCount,
		condition:         func(i int) questConditionSetter { return p.Conditions(i) },
		reward:            func(i int) questRewardSetter { return p.Rewards(i) },
		bytes:             p.Bytes,
	}
}

func questStateExtendedWriter(p *s2c.QuestStateExtended, group, number uint16) questDetailWriter {
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	return questDetailWriter{
		setConditionCount: p.SetConditionCount,
		setRewardCount:    p.SetRewardCount,
		condition:         func(i int) questConditionSetter { return p.Conditions(i) },
		reward:            func(i int) questRewardSetter { return p.Rewards(i) },
		bytes:             p.Bytes,
	}
}

func questProgressWriter(p *s2c.QuestProgress, group, number uint16) questDetailWriter {
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	return questDetailWriter{
		setConditionCount: p.SetConditionCount,
		setRewardCount:    p.SetRewardCount,
		condition:         func(i int) questConditionSetter { return p.Conditions(i) },
		reward:            func(i int) questRewardSetter { return p.Rewards(i) },
		bytes:             p.Bytes,
	}
}

func questProgressExtendedWriter(p *s2c.QuestProgressExtended, group, number uint16) questDetailWriter {
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	return questDetailWriter{
		setConditionCount: p.SetConditionCount,
		setRewardCount:    p.SetRewardCount,
		condition:         func(i int) questConditionSetter { return p.Conditions(i) },
		reward:            func(i int) questRewardSetter { return p.Rewards(i) },
		bytes:             p.Bytes,
	}
}

func (v *PlayerView) ShowQuestStepInfo(group, stepNumber uint16) error {
	p := s2c.NewQuestStepInfo()
	p.SetQuestGroup(group)
	p.SetQuestStepNumber(stepNumber)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowQuestCompletionResponse(group, number uint16, completed bool) error {
	p := s2c.NewQuestCompletionResponse()
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	p.SetIsQuestCompleted(completed)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowQuestCancelled(group, number uint16) error {
	p := s2c.NewQuestCancelled()
	p.SetQuestGroup(group)
	p.SetQuestNumber(number)
	return v.send.Send(p.Bytes())
}

// ShowQuestState 下发 F6 1B：(106,3) 及以上走 C2 Extended，其余走 C1。
func (v *PlayerView) ShowQuestState(group, number uint16, conds []action.QuestConditionView, rewards []action.QuestRewardView) error {
	if v.usesExtendedQuestState() {
		return v.send.Send(questStateExtendedWriter(s2c.NewQuestStateExtended(), group, number).write(conds, rewards))
	}
	return v.send.Send(questStateWriter(s2c.NewQuestState(), group, number).write(conds, rewards))
}

// ShowQuestProgress 下发 F6 0C，变体选择同 ShowQuestState。
func (v *PlayerView) ShowQuestProgress(group, number uint16, conds []action.QuestConditionView, rewards []action.QuestRewardView) error {
	if v.usesExtendedQuestState() {
		return v.send.Send(questProgressExtendedWriter(s2c.NewQuestProgressExtended(), group, number).write(conds, rewards))
	}
	return v.send.Send(questProgressWriter(s2c.NewQuestProgress(), group, number).write(conds, rewards))
}

// ShowActiveQuests 下发 C1 F6 1A QuestStateList（对照 CurrentlyActiveQuestsPlugIn：
// 上限 62 条，**空清单也发**——原版无提前返回）。
func (v *PlayerView) ShowActiveQuests(quests []action.QuestIDView) error {
	const maxQuestsPerPacket = 62
	if len(quests) > maxQuestsPerPacket {
		quests = quests[:maxQuestsPerPacket]
	}
	p := s2c.NewQuestStateList(s2c.QuestStateListRequiredSize(len(quests)))
	p.SetQuestCount(byte(len(quests)))
	for i, q := range quests {
		e := p.Quests(i)
		if e == nil {
			break
		}
		e.SetNumber(q.Number)
		e.SetGroup(q.Group)
	}
	return v.send.Send(p.Bytes())
}

// ShowAvailableQuests 下发 C1 F6 0A AvailableQuests（对照 ShowAvailableQuestsPlugIn：
// 条目写 StartingNumber）。
func (v *PlayerView) ShowAvailableQuests(npcNumber uint16, quests []action.QuestIDView) error {
	p := s2c.NewAvailableQuests(s2c.AvailableQuestsRequiredSize(len(quests)))
	p.SetQuestNpcNumber(npcNumber)
	p.SetQuestCount(uint16(len(quests)))
	for i, q := range quests {
		e := p.Quests(i)
		if e == nil {
			break
		}
		e.SetNumber(q.Number)
		e.SetGroup(q.Group)
	}
	return v.send.Send(p.Bytes())
}

// ShowActiveEventQuests 下发 C1 F6 03 QuestEventResponse。原版事件任务未实装，
// 固定回两条"条数=1"的占位（QuestEventResponsePlugIn.cs:44-54 的注释即抓包结果）。
func (v *PlayerView) ShowActiveEventQuests() error {
	p := s2c.NewQuestEventResponse()
	for i := 0; i < 2; i++ {
		if e := p.Quests(i); e != nil {
			e.SetNumber(1)
		}
	}
	return v.send.Send(p.Bytes())
}

// ShowQuestRewardAnnouncement 下发 C1 A3 LegacyQuestReward（对照 LegacyQuestRewardPlugIn.ShowAsync：
// 加点 200 / 一转→二转 201 / 每级点数 202 / 连击技 203 / 二转→三转 204）。
func (v *PlayerView) ShowQuestRewardAnnouncement(receiverID uint16, reward action.QuestLegacyReward, count byte) error {
	kind, ok := legacyQuestRewardCode(reward)
	if !ok {
		return nil
	}
	p := s2c.NewLegacyQuestReward()
	p.SetPlayerId(receiverID)
	p.SetReward(kind)
	p.SetCount(count)
	return v.send.Send(p.Bytes())
}

func legacyQuestRewardCode(r action.QuestLegacyReward) (s2c.QuestRewardType, bool) {
	switch r {
	case action.QuestLegacyRewardLevelUpPoints:
		return s2c.QuestRewardType_LevelUpPoints, true
	case action.QuestLegacyRewardEvolutionFirstToSecond:
		return s2c.QuestRewardType_CharacterEvolutionFirstToSecond, true
	case action.QuestLegacyRewardPointsPerLevel:
		return s2c.QuestRewardType_LevelUpPointsPerLevelIncrease, true
	case action.QuestLegacyRewardComboSkill:
		return s2c.QuestRewardType_ComboSkill, true
	case action.QuestLegacyRewardEvolutionSecondToThird:
		return s2c.QuestRewardType_CharacterEvolutionSecondToThird, true
	}
	return 0, false
}
