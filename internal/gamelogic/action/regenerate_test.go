package action

// regenerate_test.go —— 周期恢复 / 击杀后恢复的黄金值测试。
//
// 期望值全部按原版公式手算（Player.RegenerateAsync / AfterKilledMonsterAsync），
// 不复用被测函数的推导，避免公式写错时测试跟着一起错。

import "testing"

// TestRegenIncrementGolden 锁定 (max×倍率+绝对值)×elapsed/interval。
func TestRegenIncrementGolden(t *testing.T) {
	// max=1000, 倍率 0.02 → 20；绝对值 5 → 25；elapsed=interval → ×1。
	if got := RegenIncrement(1000, 0.02, 5, 3, 3); !near(got, 25) {
		t.Fatalf("RegenIncrement=%v, want 25", got)
	}
	// 半周期：×0.5。
	if got := RegenIncrement(1000, 0.02, 5, 1.5, 3); !near(got, 12.5) {
		t.Fatalf("RegenIncrement(半周期)=%v, want 12.5", got)
	}
	// 周期 0（防御分支）：不除零、返回 0。
	if got := RegenIncrement(1000, 0.02, 5, 3, 0); got != 0 {
		t.Fatalf("interval=0 应返回 0, got %v", got)
	}
}

// TestApplyRegenClampsAndCarries 锁定钳上限与小数结转。
func TestApplyRegenClampsAndCarries(t *testing.T) {
	// 0 → 25（整点数，无余量）。
	if v, rem, ok := ApplyRegen(0, 0, 1000, 0.02, 5, 3, 3); !near(v, 25) || !near(rem, 0) || !ok {
		t.Fatalf("ApplyRegen=%v rem=%v ok=%v, want 25/0/true", v, rem, ok)
	}
	// 0.5/次：首次数值涨 0，余量结转 0.5；第二次凑成 1。
	v, rem, _ := ApplyRegen(0, 0, 1000, 0.0005, 0, 3, 3)
	if !near(v, 0) || !near(rem, 0.5) {
		t.Fatalf("首拍应涨 0 且余量 0.5, got %v/%v", v, rem)
	}
	v, rem, _ = ApplyRegen(v, rem, 1000, 0.0005, 0, 3, 3)
	if !near(v, 1) || !near(rem, 0) {
		t.Fatalf("次拍应凑成 1, got %v/%v", v, rem)
	}
	// 钳到上限且余量清零（原版 min() 后存的就是 max）。
	if v, rem, ok := ApplyRegen(990, 0.9, 1000, 0.02, 5, 3, 3); !near(v, 1000) || rem != 0 || !ok {
		t.Fatalf("应钳到 1000 且余量归零, got %v/%v/%v", v, rem, ok)
	}
	// 已在上限：原样返回、余量清零、未变更。
	if v, rem, ok := ApplyRegen(1000, 0.7, 1000, 0.02, 5, 3, 3); !near(v, 1000) || rem != 0 || ok {
		t.Fatalf("已满值不应变化, got %v/%v/%v", v, rem, ok)
	}
}

// TestShieldRecoveryActiveGolden 锁定 IsShieldRecoveryActive = 安全区 + 到处回盾。
func TestShieldRecoveryActiveGolden(t *testing.T) {
	cases := []struct {
		safe   bool
		everyw float64
		want   bool
		desc   string
	}{
		{true, 0, true, "安全区内恒启用"},
		{false, 0, false, "城外默认不回盾"},
		{false, 1, true, "380 守护 option 到处回盾"},
		{false, 0.5, false, "不足 1 不算启用"},
	}
	for _, c := range cases {
		if got := ShieldRecoveryActive(c.safe, c.everyw); got != c.want {
			t.Fatalf("%s: ShieldRecoveryActive(%v,%v)=%v, want %v", c.desc, c.safe, c.everyw, got, c.want)
		}
	}
}

// TestShieldRegenHiatusThreshold 锁定 10s 静置阈值常量（原版 Regeneration.HiatusThreshold）。
func TestShieldRegenHiatusThreshold(t *testing.T) {
	if ShieldRegenHiatusThreshold != 10 {
		t.Fatalf("护盾静置阈值应为 10s, got %v", ShieldRegenHiatusThreshold)
	}
	if AbilityRegenSafezoneBonus != 3 {
		t.Fatalf("安全区 AG 绝对值加成应为 3, got %v", AbilityRegenSafezoneBonus)
	}
}

// TestAfterKillRecoverGolden 锁定 min(max, cur + (uint)(倍率×max + 绝对值))。
func TestAfterKillRecoverGolden(t *testing.T) {
	// 0.05×1000 + 10 = 60。
	if v, ok := AfterKillRecover(0, 1000, 0.05, 10); !near(v, 60) || !ok {
		t.Fatalf("AfterKillRecover=%v ok=%v, want 60/true", v, ok)
	}
	// 钳上限：990 + 50 → 1000。
	if v, ok := AfterKillRecover(990, 1000, 0.05, 0); !near(v, 1000) || !ok {
		t.Fatalf("应钳到 1000, got %v (ok=%v)", v, ok)
	}
	// 增量先 (uint) 截断再加：1.9 点增量只入账 1 点。
	if v, _ := AfterKillRecover(0, 1000, 0.0019, 0); !near(v, 1) {
		t.Fatalf("增量应先截断为 1 再加, got %v", v)
	}
	// 倍率与绝对值都 0（默认装备）→ 击杀不回血蓝（与原版一致）。
	if v, ok := AfterKillRecover(10, 1000, 0, 0); !near(v, 10) || ok {
		t.Fatalf("默认参数不应回血, got %v (ok=%v)", v, ok)
	}
	// 已满值 → 不变更。
	if v, ok := AfterKillRecover(1000, 1000, 0.05, 0); !near(v, 1000) || ok {
		t.Fatalf("已满值不应变更, got %v (ok=%v)", v, ok)
	}
}
