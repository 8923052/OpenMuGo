package action

// combo_test.go —— 连击状态机（TRIM-06e）。步骤表按 S6 Blade Knight Combo 的形状造
// （Order1 五招任选、Order2/Order3 各数招、Order3 标终结，时限 3s），
// 语义对照 ComboStateMachine.cs:60-95 与 :131。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
)

func testCombo() *config.SkillCombo {
	return &config.SkillCombo{
		Name:                "Blade Knight Combo",
		MaximumCompletionMS: 3000,
		Steps: []config.SkillComboStep{
			{Skill: 19, Order: 1}, {Skill: 20, Order: 1}, {Skill: 21, Order: 1},
			{Skill: 41, Order: 2}, {Skill: 42, Order: 2}, {Skill: 43, Order: 2},
			{Skill: 41, Order: 3, IsFinal: true}, {Skill: 42, Order: 3, IsFinal: true},
		},
	}
}

func TestComboCompletesThreeSteps(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	if c.RegisterSkill(19, t0) {
		t.Fatal("第一手不该是终结")
	}
	if o, last := c.Progress(); o != 1 || last != 19 {
		t.Fatalf("第一手后进度 %+v,%+v，期望 1,19", o, last)
	}
	if c.RegisterSkill(41, t0.Add(time.Second)) {
		t.Fatal("第二手不该是终结")
	}
	if o, last := c.Progress(); o != 2 || last != 41 {
		t.Fatalf("第二手后进度 %+v,%+v，期望 2,41", o, last)
	}
	if !c.RegisterSkill(42, t0.Add(2*time.Second)) {
		t.Fatal("第三手应是终结击")
	}
	// 终结后立即回起始态（原版 TryAdvanceToAsync(InitialState)）。
	if o, last := c.Progress(); o != 0 || last != 0 {
		t.Fatalf("终结后应复位，got %+v,%+v", o, last)
	}
	// 复位后可以从头再来。
	if c.RegisterSkill(20, t0.Add(3*time.Second)) {
		t.Fatal("重新起手不该是终结")
	}
	if o, last := c.Progress(); o != 1 || last != 20 {
		t.Fatalf("重起手后进度 %+v,%+v，期望 1,20", o, last)
	}
}

func TestComboWrongOrderResetsMachine(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	// 起始态只接受 Order=1：直接放第二手 → 拒绝并留在起始态。
	if c.RegisterSkill(41, t0) {
		t.Fatal("起手放第二手不该通过")
	}
	if o, _ := c.Progress(); o != 0 {
		t.Fatalf("错序后应仍在起始态，got %d", o)
	}
	if c.RegisterSkill(19, t0) {
		t.Fatal("复位后起手应可通过")
	}
	if o, _ := c.Progress(); o != 1 {
		t.Fatalf("复位后应走到第 1 步，got %d", o)
	}
}

func TestComboRejectsSameSkillTwice(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	c.RegisterSkill(19, t0)
	// 原版建态时把"下一顺序里与自己同名"的排除（:131）→ 同一招不能连两下。
	if c.RegisterSkill(19, t0.Add(100*time.Millisecond)) {
		t.Fatal("同一招不该推进")
	}
	if o, _ := c.Progress(); o != 0 {
		t.Fatalf("同招两下后应复位，got %d", o)
	}
	// 复位后 41 不再是合法起手。
	if c.RegisterSkill(41, t0.Add(200*time.Millisecond)) {
		t.Fatal("复位后直接放第二手不该通过")
	}
}

func TestComboTimeoutResetsBeforeMatching(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	c.RegisterSkill(19, t0)
	// 超时后的这一手先复位，再按起始态匹配：41 不是 Order1 → 拒绝且不记进度。
	if c.RegisterSkill(41, t0.Add(3500*time.Millisecond)) {
		t.Fatal("超时后第二手不该通过")
	}
	if o, _ := c.Progress(); o != 0 {
		t.Fatalf("超时后应无进度，got %d", o)
	}
	// 同一时刻换个起手招则成立。
	if c.RegisterSkill(20, t0.Add(3500*time.Millisecond)) {
		t.Fatal("超时后起手应通过")
	}
	if o, last := c.Progress(); o != 1 || last != 20 {
		t.Fatalf("超时后起手进度 %+v,%+v，期望 1,20", o, last)
	}
}

func TestComboTimeoutBoundaryIsExclusive(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	c.RegisterSkill(19, t0)
	// 恰好 3s 未"超过"时限，仍在窗口内。
	if c.RegisterSkill(41, t0.Add(3*time.Second)) {
		t.Fatal("第二手不该是终结")
	}
	if o, last := c.Progress(); o != 2 || last != 41 {
		t.Fatalf("恰好到期时应继续，got %+v,%+v", o, last)
	}
}

func TestComboTimerStartsAtFirstStepOnly(t *testing.T) {
	c := NewCombo(testCombo())
	t0 := time.Unix(0, 0)
	c.RegisterSkill(19, t0)
	c.RegisterSkill(41, t0.Add(2900*time.Millisecond))
	// 计时只在从起始态推进时刷新（:79-82）：这一手距 t0 已 5.5s → 超时复位。
	if c.RegisterSkill(42, t0.Add(5500*time.Millisecond)) {
		t.Fatal("超时的终结手不该通过")
	}
	if o, _ := c.Progress(); o != 0 {
		t.Fatalf("应从起始态重来，got %d", o)
	}
}

func TestComboWithoutDefinitionIsInert(t *testing.T) {
	var c *Combo
	if NewCombo(nil) != nil {
		t.Fatal("无定义应返回 nil 状态机")
	}
	if c.RegisterSkill(19, time.Unix(0, 0)) {
		t.Fatal("nil 状态机不该判定终结")
	}
	if o, last := c.Progress(); o != 0 || last != 0 {
		t.Fatalf("nil 状态机进度 %+v,%+v", o, last)
	}
}
