package world

import "testing"

func TestEnterLeaveScope(t *testing.T) {
	w := New()
	m := w.Map(0)

	a := &Player{ID: w.AllocID()}
	b := &Player{ID: w.AllocID()}
	if a.ID == 0 || b.ID == 0 || a.ID == b.ID {
		t.Fatalf("玩家ID分配异常: a=%d b=%d", a.ID, b.ID)
	}

	if others := m.Enter(a); len(others) != 0 {
		t.Fatalf("首位进场不应看到其他人")
	}
	if others := m.Enter(b); len(others) != 1 || others[0].ID != a.ID {
		t.Fatalf("b 进场应看到 a")
	}
	if m.Count() != 2 {
		t.Fatalf("在场数=%d，应为2", m.Count())
	}
	// 重复 Enter 幂等：不重复计数。
	if others := m.Enter(b); len(others) != 1 || others[0].ID != a.ID {
		t.Fatalf("b 重复 Enter 结果异常")
	}
	if m.Count() != 2 {
		t.Fatalf("重复 Enter 后在场数=%d", m.Count())
	}

	if remaining := m.Leave(b.ID); len(remaining) != 1 || remaining[0].ID != a.ID {
		t.Fatalf("b 离开后剩余玩家异常")
	}
	if m.Leave(9999) != nil {
		t.Fatalf("不存在玩家 Leave 应返回 nil")
	}

	// ID 释放后环形扫描可复用：占满其余槽位后，应回绕复用已释放的 b.ID。
	w.FreeID(b.ID)
	for {
		got := w.AllocID()
		if got == b.ID {
			break // 环形回绕后复用已释放 ID
		}
		if got == 0 {
			t.Fatal("环形复用失败：池耗尽仍未回收 b.ID")
		}
	}
}

func TestWalk(t *testing.T) {
	w := New()
	m := w.Map(0)
	a := &Player{ID: w.AllocID()}
	b := &Player{ID: w.AllocID(), X: 95, Y: 118} // b 预置在 a 目标点附近（视野内）
	m.Enter(a)
	m.Enter(b)

	got, others, entered, left, ok := m.Walk(a.ID, 100, 120, 3)
	if !ok || got.X != 100 || got.Y != 120 || got.Rotation != 3 {
		t.Fatalf("行走结果异常: %+v ok=%v", got, ok)
	}
	if len(others) != 1 || others[0].ID != b.ID {
		t.Fatalf("行走观察者集合异常")
	}
	// b 在 a 移动前就在视野内（a 起点与 b 距离 >12? 起点 (0,0) 距 (95,118) 很远，b 不在旧集合）。
	// a 移动后 b 进入视野 → b 应出现在 entered。
	if len(entered) != 1 || entered[0].ID != b.ID {
		t.Fatalf("b 应为新进入视野: %v", entered)
	}
	if len(left) != 0 {
		t.Fatalf("无离开者: %v", left)
	}
	if _, _, _, _, ok := m.Walk(9999, 1, 1, 0); ok {
		t.Fatalf("不存在玩家行走应失败")
	}
}

// TestAoIOutOfRangeExcluded 锁定 T1-4：视野为**桶订阅覆盖集**（InfoRange=12，
// 原版 GetBucketsInRange 语义——桶内所有对象都可见，可见距离随桶边界浮动 12~27），
// 未覆盖桶内的玩家不进入观察者集合——这是"单锁全量广播"问题的修复验证。
func TestAoIOutOfRangeExcluded(t *testing.T) {
	w := New()
	m := w.Map(0)
	// a 在 Lorencia (140,131)：覆盖桶 bx=floor(128/8)=16..floor(152/8)=19（x 128..159）。
	a := &Player{ID: w.AllocID(), X: 140, Y: 131}
	near := &Player{ID: w.AllocID(), X: 145, Y: 135} // 距离 5
	far := &Player{ID: w.AllocID(), X: 200, Y: 200}  // 距离 60
	m.Enter(far)
	m.Enter(a)
	m.Enter(near)

	// a 进场：应只看到 near（far 所在桶未覆盖）。inRange 含自身（原版同，调用方过滤）。
	others := m.grid.inRange(a.X, a.Y)
	ids := make(map[uint16]bool)
	for _, o := range others {
		if o.ID == a.ID {
			continue
		}
		ids[o.ID] = true
	}
	if len(ids) != 1 || !ids[near.ID] {
		t.Fatalf("a 的视野应只有 near: %v", others)
	}
	// far 移动（走正规 Walk——桶维护随移动更新）到 x=155（距离 15，但桶 19 已覆盖）→ 入视野。
	if _, _, _, _, ok := m.Walk(far.ID, 155, 131, 0); !ok {
		t.Fatal("far 行走失败")
	}
	if others := m.grid.inRange(a.X, a.Y); len(others) != 3 { // 自身 + near + far
		t.Fatalf("覆盖桶内（距离 15）应入视野: %v", others)
	}
	// far 移到 x=161（桶 20 未覆盖，距离 21）→ 出视野。
	if _, _, _, _, ok := m.Walk(far.ID, 161, 131, 0); !ok {
		t.Fatal("far 行走失败")
	}
	if others := m.grid.inRange(a.X, a.Y); len(others) != 2 { // 自身 + near
		t.Fatalf("未覆盖桶（距离 21）应在视野外: %v", others)
	}
}
