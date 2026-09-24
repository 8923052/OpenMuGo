package action

import (
	"testing"

	"mugo/internal/gamelogic/entity"
)

// TestTraceRecorderSemantic 锁定 trace 骨架：同输入 → 同语义序列（防线 3 的比对单元）。
func TestTraceRecorderSemantic(t *testing.T) {
	c := &entity.Character{Name: "dw", ClassNumber: 0, Level: 1}

	run := func() []TraceEntry {
		r := NewRecordingView(nil)
		_ = r.ShowLoginResult(LoginOkay)
		_ = r.ShowCharacterInScope(ScopeEntry{ID: 0x8001, Name: "dw", X: 10, Y: 20})
		_ = r.ShowObjectWalked(WalkedMove{ObjectID: 1, SourceX: 10, SourceY: 20, TargetX: 11, TargetY: 21, Rotation: 3, Steps: []byte{1}})
		// Stats 不参与 trace（trace 只读 Character.Name），传 nil；避免 import player 形成环。
		_ = r.ShowCharacterInformation(CharacterInformation{Character: c})
		return r.Traces
	}

	a, b := run(), run()
	if !TracesEqual(a, b) {
		t.Fatalf("同输入 trace 不一致: %v vs %v", a, b)
	}
	if len(a) != 4 {
		t.Fatalf("trace 条数=%d, want 4", len(a))
	}
	// 不同输入 → 不同 trace（Character.Name 不同）。
	c2 := &entity.Character{Name: "dk", ClassNumber: 4, Level: 1}
	_ = b
	r := NewRecordingView(nil)
	_ = r.ShowCharacterInformation(CharacterInformation{Character: c2})
	if TracesEqual(a, r.Traces) {
		t.Fatal("不同输入不应产生相同 trace")
	}
}
