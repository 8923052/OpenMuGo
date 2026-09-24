package quests

import "testing"

// legacy_test.go —— 组 0 状态位的三条分支，逐条对照 QuestStateExtensions.cs:26-78。

func TestStateByteWithoutStateRecord(t *testing.T) {
	if got := StateByte(LegacyFacts{}); got != 0xFF {
		t.Fatalf("该组从无状态记录时应为 0xFF（全 Inactive）, got %08b", got)
	}
}

func TestStateByteNoActiveNoFinished(t *testing.T) {
	// 原版的三元式在此分支恒取 Inactive（条件已排除 ActiveQuest 非空）—— 高位保持 Inactive。
	got := StateByte(LegacyFacts{HasState: true})
	if got != 0b1111_1100|byte(LegacyInactive) {
		t.Fatalf("got %08b, want %08b", got, 0b1111_1100|byte(LegacyInactive))
	}
}

// TestStateByteAnchoredWindow 锚定"4 个任务一字节"的窗口与优先级：
// 锚点 = 进行中任务（无进行中则取下一条可接，再无则上次完成）。
func TestStateByteAnchoredWindow(t *testing.T) {
	cases := []struct {
		name string
		f    LegacyFacts
		want byte
	}{
		// 窗口是锚点所在那一组 4 槽；判序：LastFinished>=slot 或 Active>slot → Complete，
		// 否则"就是进行中那一条"→ Active，再否则 Inactive。
		{"锚点 5（窗口 4..7）：4 Complete、5 Active、6/7 Inactive",
			LegacyFacts{HasState: true, HasActive: true, Active: 5, Anchor: 5, HasAnchor: true, HasLast: true, LastFinished: 4},
			byte(LegacyComplete) | byte(LegacyActive)<<2 | byte(LegacyInactive)<<4 | byte(LegacyInactive)<<6},
		{"上次完成更早时，号小于进行中锚点的槽位由 Active>slot 判 Complete",
			LegacyFacts{HasState: true, HasActive: true, Active: 5, Anchor: 5, HasAnchor: true, HasLast: true, LastFinished: 1},
			byte(LegacyComplete) | byte(LegacyActive)<<2 | byte(LegacyInactive)<<4 | byte(LegacyInactive)<<6},
		{"锚点 2（窗口 0..3）：0/1 Complete、2 Active、3 Inactive",
			LegacyFacts{HasState: true, HasActive: true, Active: 2, Anchor: 2, HasAnchor: true},
			byte(LegacyComplete) | byte(LegacyComplete)<<2 | byte(LegacyActive)<<4 | byte(LegacyInactive)<<6},
		{"只有上次完成（号 3）：0..3 全 Complete",
			LegacyFacts{HasState: true, HasLast: true, LastFinished: 3, Anchor: 3, HasAnchor: true},
			byte(LegacyComplete) | byte(LegacyComplete)<<2 | byte(LegacyComplete)<<4 | byte(LegacyComplete)<<6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StateByte(tc.f); got != tc.want {
				t.Fatalf("got %08b, want %08b", got, tc.want)
			}
		})
	}
}

func TestStateListSlots(t *testing.T) {
	// 上次完成到 1、进行中在 2：0/1 Complete、2 Active、其余 Inactive（暗骑士基型保留黑暗石）。
	states := StateList(LegacyFacts{HasState: true, HasLast: true, LastFinished: 1,
		HasActive: true, Active: 2, DarkKnightBase: true})
	want := []LegacyState{LegacyComplete, LegacyComplete, LegacyActive, LegacyInactive,
		LegacyInactive, LegacyInactive, LegacyInactive}
	for i, w := range want {
		if states[i] != w {
			t.Fatalf("槽 %d = %d, want %d", i, states[i], w)
		}
	}
	// 非暗骑士基型：黑暗石（号 3）槽改 Undefined。
	other := StateList(LegacyFacts{HasState: true})
	if other[3] != LegacyUndefined {
		t.Fatalf("非暗骑士时槽 3 应为 Undefined, got %d", other[3])
	}
	if other[0] != LegacyInactive {
		t.Fatalf("其余槽应保持 Inactive, got %d", other[0])
	}
	// 越界的上次完成号不应写坏数组（暗骑士基型，黑暗石槽也在 Complete 段内）。
	edge := StateList(LegacyFacts{HasState: true, HasLast: true, LastFinished: 99, DarkKnightBase: true})
	for i := range edge {
		if edge[i] != LegacyComplete {
			t.Fatalf("号 99 时全部 7 槽应为 Complete, 槽 %d = %d", i, edge[i])
		}
	}
}
