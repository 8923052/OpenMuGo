package remote

// quest_legacy_view.go —— 组 0（legacy 任务）的对话族出站包，对应
// `GameServer/RemoteView/Quest/` 下的 QuestStructExtensions / LegacyQuest* 插件：
//   - C1 A0 LegacyQuestStateList（7 槽 × 2bit，QuestStructExtensions.cs:26-66）
//   - C1 A1 LegacyQuestStateDialog（5B：任务号 + 一个 4 任务状态字节）
//   - C1 A2 LegacySetQuestStateResponse（6B：任务号 + 结果 + 状态字节）
//   - C1 A4 00 LegacyQuestMonsterKillInfo（48B：任务号 + 至多 5 组 (怪物号, 击杀数)）
//   - C1 01 ObjectMessage（NPC 头顶气泡，ShowMessageOfObjectPlugIn.cs:30-33）
//   - C1 54 ShowGuildMasterDialog
//
// 状态位的**计算**在 gamelogic/quests（legacy.go），本层只做 2bit 打包与写包。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/quests"
	s2c "mugo/internal/proto/s2c"
)

// legacyQuestStateSlots 是 0xA0 状态表的槽数（原版写死 7：任务号 0..6）。
const legacyQuestStateSlots = quests.LegacyQuestStateListSize

// legacyQuestStateListSize 对照 LegacyQuestStateListRef.GetRequiredSize(n) = 4 + ⌈n/4⌉。
func legacyQuestStateListSize(n int) int { return 4 + (n+3)/4 }

// ShowLegacyQuestStateList 下发 C1 A0。槽序即任务号 0..6（黑暗石在号 3）。
func (v *PlayerView) ShowLegacyQuestStateList(states [legacyQuestStateSlots]byte) error {
	p := s2c.NewLegacyQuestStateList(legacyQuestStateListSize(legacyQuestStateSlots))
	p.SetQuestCount(legacyQuestStateSlots)
	setters := []func(s2c.LegacyQuestState){
		p.SetScrollOfEmperorState,
		p.SetThreeTreasuresOfMuState,
		p.SetGainHeroStatusState,
		p.SetSecretOfDarkStoneState,
		p.SetCertificateOfStrengthState,
		p.SetInfiltrationOfBarrackState,
		p.SetInfiltrationOfRefugeState,
	}
	for i, set := range setters {
		set(s2c.LegacyQuestState(states[i]))
	}
	return v.send.Send(p.Bytes())
}

// ShowLegacyQuestStateDialog 下发 C1 A1（LegacyQuestStateDialogPlugIn.cs:43）。
func (v *PlayerView) ShowLegacyQuestStateDialog(questIndex byte, state byte) error {
	p := s2c.NewLegacyQuestStateDialog()
	p.SetQuestIndex(questIndex)
	p.SetState(state)
	return v.send.Send(p.Bytes())
}

// ShowLegacySetQuestStateResponse 下发 C1 A2（QuestStartedPlugIn.cs:45 与
// QuestCompletionResponsePlugIn.cs:36-38 用 (号, 0=成功, 状态字节)）。
func (v *PlayerView) ShowLegacySetQuestStateResponse(questIndex, result, state byte) error {
	p := s2c.NewLegacySetQuestStateResponse()
	p.SetQuestIndex(questIndex)
	p.SetResult(result)
	p.SetNewState(state)
	return v.send.Send(p.Bytes())
}

// ShowLegacyQuestMonsterKillInfo 下发 C1 A4 00：任务号 + 至多 5 组击杀进度
// （LegacyQuestStateDialogPlugIn.cs:45-66，Result 默认为 1）。
func (v *PlayerView) ShowLegacyQuestMonsterKillInfo(questIndex byte, kills []action.LegacyKillView) error {
	p := s2c.NewLegacyQuestMonsterKillInfo()
	p.SetResult(1)
	p.SetQuestIndex(questIndex)
	for i, k := range kills {
		e := p.Kills(i)
		if e == nil {
			break
		}
		e.SetMonsterNumber(uint32(k.MonsterNumber))
		e.SetKillCount(uint32(k.Count))
	}
	return v.send.Send(p.Bytes())
}

// ShowObjectMessage 下发 C1 01（NPC 头顶气泡：文本 UTF-8 + NUL，发送者对象号大端）。
func (v *PlayerView) ShowObjectMessage(objectID uint16, message string) error {
	p := s2c.NewObjectMessage(s2c.ObjectMessageRequiredSize(len(message)))
	p.SetObjectId(objectID)
	p.SetMessage(message)
	return v.send.Send(p.Bytes())
}

// ShowGuildMasterDialog 下发 C1 54（会长 NPC 允许建盟时的对话框）。
func (v *PlayerView) ShowGuildMasterDialog() error {
	p := s2c.NewShowGuildMasterDialog()
	return v.send.Send(p.Bytes())
}
