package quests

// legacy.go —— 组 0（legacy 任务）的"任务状态位"计算，对照 OpenMU
// `GameServer/RemoteView/Quest/QuestStateExtensions.cs:26-78`（GetLegacyQuestStateByte）与
// `QuestStructExtensions.cs:26-66`（SendLegacyQuestStateAsync 的 7 槽状态表）。
//
// 为什么在本包：这两个函数只用到"该组的上次完成号 / 进行中号 / 锚点号"这三件领域事实；
// 把 2 bit 打包成字节是编码细节，留在 view/remote。原版把它们放在 RemoteView 只因
// 它们以 Player 扩展方法的形态存在。

// LegacyState 是单个任务的状态（数值与 s2c.LegacyQuestState 一致，注释在视图层使用）。
type LegacyState byte

const (
	// LegacyUndefined 表示"该槽没有任务"（客户端不画）。
	LegacyUndefined LegacyState = 0
	// LegacyActive 表示进行中。
	LegacyActive LegacyState = 1
	// LegacyComplete 表示已完成。
	LegacyComplete LegacyState = 2
	// LegacyInactive 表示既未进行也未完成（可接）。
	LegacyInactive LegacyState = 3
)

// LegacyQuestStateListSize 是 0xA0 状态表里的槽数（原版写死 7：号 0..6）。
const LegacyQuestStateListSize = 7

// LegacyFacts 是算状态所需的那组事实（由编排层从配置与角色态解析出**任务号**后传入）。
type LegacyFacts struct {
	// HasState = false 表示该角色在这一组从未有过状态记录（原版 questState is null）。
	HasState bool
	// LastFinished / Active 是任务号；HasLast / HasActive 表示是否为空。
	LastFinished   int
	HasLast        bool
	Active         int
	HasActive      bool
	Anchor         int // ActiveQuest ?? GetNextLegacyQuest() ?? LastFinishedQuest
	HasAnchor      bool
	DarkKnightBase bool // 基础职业是否暗骑士（决定"黑暗石"槽是否可见）
}

// StateByte 复刻 GetLegacyQuestStateByte：把一个 4 任务窗口打包成 8 bit（每任务 2 bit，
// 低位在前）。三个分支逐条照抄，含原版注释里承认的两处"简化"。
func StateByte(f LegacyFacts) byte {
	if !f.HasState {
		return 0xFF // 全 Inactive（含起始那一槽）
	}
	if !f.HasLast && !f.HasActive {
		// 该分支的三元式在"ActiveQuest 为 null"时恒取 Inactive —— 原版的写法如此，照抄。
		state := byte(LegacyInactive)
		if f.HasActive {
			state = byte(LegacyActive)
		}
		return 0b1111_1100 | state
	}
	if !f.HasAnchor {
		return 0xFF // 原版注释：不应发生
	}

	startOffset := (f.Anchor / 4) * 4 // 一字节表达 4 个任务
	result := 0
	for i := 0; i < 4; i++ {
		shift := i * 2
		slot := i + startOffset
		if (f.HasLast && f.LastFinished >= slot) || (f.HasActive && f.Active > slot) {
			result |= int(LegacyComplete) << shift
			continue
		}
		if f.HasActive && f.Active == f.Anchor && f.Anchor == slot {
			result |= int(LegacyActive) << shift
			continue
		}
		result |= int(LegacyInactive) << shift
	}
	return byte(result)
}

// StateList 复刻 SendLegacyQuestStateAsync 的 7 槽表：先全 Inactive，
// 再把 0..LastFinished 号整段标 Complete、进行中那一号标 Active，
// 最后：基础职业不是暗骑士时把"黑暗石"槽（号 3）改成 Undefined。
func StateList(f LegacyFacts) [LegacyQuestStateListSize]LegacyState {
	var states [LegacyQuestStateListSize]LegacyState
	for i := range states {
		states[i] = LegacyInactive
	}
	if f.HasLast {
		for i := 0; i <= f.LastFinished && i < LegacyQuestStateListSize; i++ {
			states[i] = LegacyComplete
		}
	}
	if f.HasActive && f.Active >= 0 && f.Active < LegacyQuestStateListSize {
		states[f.Active] = LegacyActive
	}
	if !f.DarkKnightBase && LegacyQuestStateListSize > 3 {
		states[3] = LegacyUndefined // SecretOfDarkStoneState
	}
	return states
}
