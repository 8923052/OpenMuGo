// Package quests 是 S10 任务的纯判定层，对照 OpenMU `GameLogic/PlayerActions/Quests/`：
// QuestStartAction / QuestSelectAction / QuestCompletionAction / QuestCancelAction /
// QuestClientAction + `PlugIns/QuestMonsterKillCountPlugIn.cs`。
//
// 刻意保持"纯函数 + 注入"：不碰世界/背包/持久化/封包（那些归 gameserver 编排层与 view/remote，
// 与 crafting 引擎同构）。原版按引用比较任务定义，本仓按 config 下标（见 entity.QuestState）。
package quests

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// Outcome 是开始/完成判定的结果码（对应原版各 Action 里逐条 return 的分支顺序）。
type Outcome int

const (
	// OutcomeOK 对应原版通过全部前置判定。
	OutcomeOK Outcome = iota
	// OutcomeLevel 对应 "character level not in allowed range"（QuestStartAction.cs:31）。
	OutcomeLevel
	// OutcomeAlreadyActive 对应 "There is already an active quest of this group"（:45）。
	// 原版此时会**下发进行中任务的进度包**，不只是静默返回。
	OutcomeAlreadyActive
	// OutcomeNotRepeatable 对应 "The quest is not repeatable"（:52）。
	OutcomeNotRepeatable
	// OutcomeNotEnoughMoney 对应 NotEnoughMoneyToProceed 蓝字（:66）。
	OutcomeNotEnoughMoney
	// OutcomeNoActive 对应完成/取消时"该组无进行中任务或号不匹配"。
	OutcomeNoActive
	// OutcomeRequirements 对应物品/击杀/客户端动作需求未满足（QuestCompletionAction.cs:37-69）。
	OutcomeRequirements
)

// Status 是任务的对外状态（与 s2c 任务状态语义对应）。
type Status int

const (
	StatusNotStarted Status = iota
	StatusActive
	StatusReadyForCompletion
	StatusCompleted
)

// StartContext 是开始判定所需的外部事实（等级/该组状态/现有金币；由编排层提供）。
type StartContext struct {
	Level uint16
	// Money 是可用金币（对照 RequiredStartMoney 的 TryRemoveMoney）。
	Money int64
	// HasActive / IsLastFinished 对应 questState.ActiveQuest != null /
	// Equals(questState.LastFinishedQuest, quest)。
	HasActive      bool
	IsLastFinished bool
}

// CanStart 按原版 QuestStartAction 的判定次序给出结果。
// 任务本身已由 config.QuestAtNpc 做过"NPC + 职业"筛选（原版 GetQuest 返回 null 即静默返回）。
func CanStart(q *config.Quest, ctx StartContext) Outcome {
	if !q.LevelOK(ctx.Level) {
		return OutcomeLevel
	}
	if ctx.HasActive {
		return OutcomeAlreadyActive
	}
	if ctx.IsLastFinished && !q.Repeatable {
		return OutcomeNotRepeatable
	}
	if q.RequiredStartMoney > 0 && ctx.Money < int64(q.RequiredStartMoney) {
		return OutcomeNotEnoughMoney
	}
	return OutcomeOK
}

// Start 开始任务（假定 CanStart 已为 OK）：先清该组旧态（原版 questState.ClearAsync），
// 再置进行中；起始费的扣除由编排层负责。
func Start(q *config.Quest, idx int, st *entity.QuestState) {
	Clear(st)
	st.ActiveQuestIndex = idx
}

// Clear 对应 QuestExtensions.ClearAsync：清进行中任务、客户端动作标记与逐需求击杀计数。
func Clear(st *entity.QuestState) {
	if st == nil {
		return
	}
	st.ActiveQuestIndex = -1
	st.ClientActionDone = false
	st.KillProgress = nil
}

// KillAdvance 是一次击杀命中的需求（下标 + 最新计数），供编排层发进度蓝字
// （QuestMonsterKillCountPlugIn.cs:57 的 "[{0}] Defeat {1} - {2}/{3}"）。
type KillAdvance struct {
	Requirement int
	Current     int
	Required    int
	// Report 即原版条件 `MinimumNumber >= KillCount`：达标前的每次击杀都要报进度。
	Report bool
}

// AdvanceKill 对应 QuestMonsterKillCountPlugIn.AttackableGotKilledAsync：
// 对该进行中任务的**每一条**命中该怪号的击杀需求各加 1。
func AdvanceKill(st *entity.QuestState, q *config.Quest, monsterNumber int) []KillAdvance {
	if st == nil || q == nil {
		return nil
	}
	var out []KillAdvance
	for i := range q.RequiredKills {
		if q.RequiredKills[i].MonsterNumber != monsterNumber {
			continue
		}
		if st.KillProgress == nil {
			st.KillProgress = make(map[int]int)
		}
		st.KillProgress[i]++
		cur := st.KillProgress[i]
		out = append(out, KillAdvance{
			Requirement: i,
			Current:     cur,
			Required:    q.RequiredKills[i].Count,
			Report:      q.RequiredKills[i].Count >= cur,
		})
	}
	return out
}

// KillCount 取某条击杀需求的当前计数（对应 QuestStructExtensions.cs:207）。
func KillCount(st *entity.QuestState, requirement int) int {
	if st == nil || st.KillProgress == nil {
		return 0
	}
	return st.KillProgress[requirement]
}

// itemsMet 逐条比对上交物品。count 由编排层按 (组,号[,等级]) 统计背包。
// 等级口径对照 QuestCompletionAction.cs:39：只有 DropItemGroup.ItemLevel 参与过滤。
func itemsMet(q *config.Quest, count func(*config.QuestItemRequirement) int) bool {
	for i := range q.RequiredItems {
		r := &q.RequiredItems[i]
		if count == nil || count(r) < r.Count {
			return false
		}
	}
	return true
}

func killsMet(q *config.Quest, st *entity.QuestState) bool {
	for i := range q.RequiredKills {
		if KillCount(st, i) < q.RequiredKills[i].Count {
			return false
		}
	}
	return true
}

// CanComplete 判定能否完成任务 q：需是该组当前进行中任务，且物品/击杀/客户端动作都满足
// （判定次序与 QuestCompletionAction.cs:37-69 一致）。
func CanComplete(q *config.Quest, idx int, st *entity.QuestState, count func(*config.QuestItemRequirement) int) Outcome {
	if st == nil || st.ActiveQuestIndex != idx {
		return OutcomeNoActive
	}
	if !itemsMet(q, count) || !killsMet(q, st) {
		return OutcomeRequirements
	}
	if q.RequiresClientAction && !st.ClientActionDone {
		return OutcomeRequirements
	}
	return OutcomeOK
}

// Finish 记为"上次完成"（QuestCompletionAction.cs:71 在扣物品与发奖**之前**只做这一步）。
func Finish(q *config.Quest, idx int, st *entity.QuestState) {
	if st == nil {
		return
	}
	st.LastFinishedQuestIndex = idx
}

// Complete 记完成并立即清进行中态（供不需要在发奖期间读取进行中状态的调用方使用）。
func Complete(q *config.Quest, idx int, st *entity.QuestState) {
	Finish(q, idx, st)
	Clear(st)
}

// MarkClientAction 对应 QuestClientAction：仅当该组进行中任务正是 q 时置标记。
func MarkClientAction(q *config.Quest, idx int, st *entity.QuestState) bool {
	if st == nil || st.ActiveQuestIndex != idx {
		return false
	}
	st.ClientActionDone = true
	return true
}

// StateOf 返回任务对外的状态（供任务列表/进度包）。
func StateOf(q *config.Quest, idx int, st *entity.QuestState, count func(*config.QuestItemRequirement) int) Status {
	if st == nil {
		return StatusNotStarted
	}
	if st.ActiveQuestIndex == idx {
		if itemsMet(q, count) && killsMet(q, st) {
			return StatusReadyForCompletion
		}
		return StatusActive
	}
	if st.LastFinishedQuestIndex == idx && st.ActiveQuestIndex < 0 {
		return StatusCompleted
	}
	return StatusNotStarted
}
