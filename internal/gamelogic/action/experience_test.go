package action

// experience_test.go —— T2-7 经验/升级决策回归：基础经验公式黄金值、倍率语义、
// 逐步结算序列（跨级拆包 + 每级升级标记）、等级上限与溢出门。
//
// 黄金值按原版源码手算写入，而不是"跑一遍代码看结果"。

import (
	"math"
	"testing"
)

// closeEnough 浮点比较（公式含 float32 中间量，留 1e-3 余量）。
func closeEnough(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

// TestCalculateBaseExperienceGolden 锁定 AttackableExtensions.CalculateBaseExperience：
//
//	temp = (targetLevel + 25) × targetLevel / 3.0
//	if killerLevel > targetLevel + 10: temp ×= (targetLevel + 10) / killerLevel
//	if targetLevel >= 65:            temp += (targetLevel − 64) × (targetLevel / 4)
//	return max(temp, 0) × 1.25
func TestCalculateBaseExperienceGolden(t *testing.T) {
	cases := []struct {
		name           string
		target, killer float32
		want           float64
	}{
		// (4+25)×4/3 = 38.6667；4 > 14 不成立；4 < 65 → ×1.25 = 48.3333
		{"同级低级不衰减", 4, 4, 48.33333},
		// 38.6667 × ((4+10)/200 = 0.07) = 2.70667 → ×1.25 = 3.38333
		{"越级过多衰减", 4, 200, 3.38333},
		// 14 = target+10 边界：killer 不大于 target+10，**不**衰减。
		// (14+25)×14/3 = 182 → ×1.25 = 227.5
		{"衰减边界不触发", 14, 24, 227.5},
		// killer = target+11 刚过边界：182 × (24/25 = 0.96) = 174.72 → ×1.25 = 218.4
		{"衰减边界刚触发", 14, 25, 218.4},
		// 65 级起追加 (65−64)×(65/4) = 16.25：5850/3 = 1950 + 16.25 = 1966.25 → ×1.25 = 2457.8125
		{"65 级起追加", 65, 65, 2457.8125},
		// 64 级不追加：(64+25)×64/3 = 1898.667 → ×1.25 = 2373.333
		{"64 级不追加", 64, 64, 2373.3333},
		// 0/负等级直接 0（防御分支）。
		{"零等级", 0, 100, 0},
		{"负等级", -5, 100, 0},
	}
	for _, c := range cases {
		got := CalculateBaseExperience(c.target, c.killer)
		if !closeEnough(got, c.want) {
			t.Fatalf("%s: CalculateBaseExperience(%v,%v)=%v, want %v",
				c.name, c.target, c.killer, got, c.want)
		}
	}
}

// TestExperienceRateMultiplier 锁定"0 视为 1"：导出件缺项时不改变结果。
func TestExperienceRateMultiplier(t *testing.T) {
	cases := []struct {
		m    ExperienceRateMultiplier
		want float64
	}{
		{ExperienceRateMultiplier{1, 1, 1}, 1},
		{ExperienceRateMultiplier{0, 0, 0}, 1},  // 全部无数据 → 1（不是 0）
		{ExperienceRateMultiplier{2, 0, 1}, 2},  // 缺项按 1
		{ExperienceRateMultiplier{2, 3, 4}, 24}, // 三项相乘
		{ExperienceRateMultiplier{1, 0.5, 1}, 0.5},
	}
	for i, c := range cases {
		if got := c.m.Value(); !closeEnough(got, c.want) {
			t.Fatalf("case %d: Value()=%v, want %v", i, got, c.want)
		}
	}
}

// expTestTable 是测试用**累计**经验表：下标 = 等级，值 = 该等级起点的累计经验。
// 结算用 expTableFor(level+1)，即"下一级的起点"。
var expTestTable = []int64{0, 0, 100, 300, 600, 1000}

// TestApplyExperienceWithinLevel 锁定单级内入账：不升级、只出一步 Normal。
func TestApplyExperienceWithinLevel(t *testing.T) {
	g := ApplyExperience(1, 50, 30, expTestTable, 5, false)
	if g.LevelsGained != 0 || g.NewLevel != 1 || g.NewExperience != 80 {
		t.Fatalf("结算异常: %+v", g)
	}
	if len(g.Steps) != 1 {
		t.Fatalf("应只有 1 步, got %d", len(g.Steps))
	}
	s := g.Steps[0]
	if s.Amount != 30 || s.Type != ExperienceNormal || s.LeveledUp {
		t.Fatalf("步骤异常: %+v", s)
	}
	if g.MaxLevelReached {
		t.Fatal("未达上限不应置 MaxLevelReached")
	}
}

// TestApplyExperienceExactBoundaryDoesNotLevel 锁定原版严格小于判据
// （`expForNextLevel - Experience < gained`）：**刚好**补满不升级。
func TestApplyExperienceExactBoundaryDoesNotLevel(t *testing.T) {
	g := ApplyExperience(1, 50, 50, expTestTable, 5, false)
	if g.LevelsGained != 0 || g.NewLevel != 1 || g.NewExperience != 100 {
		t.Fatalf("刚好补满不应升级: %+v", g)
	}
	if len(g.Steps) != 1 || g.Steps[0].LeveledUp {
		t.Fatalf("步骤异常: %+v", g.Steps)
	}
}

// TestApplyExperienceLevelUpWithRemainder 锁定"升级 + 余量"两步序列：
// 第 1 步恰好补齐到升级边界并带升级标记，剩余经验作为第 2 步单独下发。
func TestApplyExperienceLevelUpWithRemainder(t *testing.T) {
	g := ApplyExperience(1, 50, 51, expTestTable, 5, false)
	if g.LevelsGained != 1 || g.NewLevel != 2 || g.NewExperience != 101 {
		t.Fatalf("结算异常: %+v", g)
	}
	if len(g.Steps) != 2 {
		t.Fatalf("应拆成 2 步, got %d: %+v", len(g.Steps), g.Steps)
	}
	if s := g.Steps[0]; s.Amount != 50 || !s.LeveledUp || s.Level != 2 || s.Type != ExperienceNormal {
		t.Fatalf("第 1 步应为 50 且升级到 2 级: %+v", s)
	}
	if s := g.Steps[1]; s.Amount != 1 || s.LeveledUp || s.Type != ExperienceNormal {
		t.Fatalf("第 2 步应为余量 1: %+v", s)
	}
	// 升级瞬间累计经验必须**恰好**落在边界上（属性系统按此重建，不可有偏差）。
	if boundary := expTestTable[2]; g.Steps[0].Amount != boundary-50 {
		t.Fatalf("升级步金额应补齐到边界 %d", boundary)
	}
}

// TestApplyExperienceMultiLevel 锁定跨级连环：一次击杀连升多级，每级各一步。
func TestApplyExperienceMultiLevel(t *testing.T) {
	// 1 级 0 经验吃 1000 点：100 → 200 → 300 → 余 400（4 级起点 600，差 400 不足升级）。
	g := ApplyExperience(1, 0, 1000, expTestTable, 5, false)
	if g.LevelsGained != 3 || g.NewLevel != 4 || g.NewExperience != 1000 {
		t.Fatalf("结算异常: %+v", g)
	}
	if len(g.Steps) != 4 {
		t.Fatalf("应 4 步, got %d: %+v", len(g.Steps), g.Steps)
	}
	wantAmount := []int64{100, 200, 300, 400}
	wantLevelUp := []bool{true, true, true, false}
	wantLevel := []int{2, 3, 4, 0}
	for i, s := range g.Steps {
		if s.Amount != wantAmount[i] || s.LeveledUp != wantLevelUp[i] {
			t.Fatalf("第 %d 步=(%d,%v) want (%d,%v)", i, s.Amount, s.LeveledUp, wantAmount[i], wantLevelUp[i])
		}
		if s.LeveledUp && s.Level != wantLevel[i] {
			t.Fatalf("第 %d 步升级后等级=%d want %d", i, s.Level, wantLevel[i])
		}
	}
	// 每步的 Amount 之和必须等于实际入账总量（不得丢经验）。
	var sum int64
	for _, s := range g.Steps {
		sum += s.Amount
	}
	if sum != 1000 {
		t.Fatalf("步金额之和=%d want 1000", sum)
	}
	// 每次升级后的累计经验必须落在该级起的表值上。
	acc := int64(0)
	for _, s := range g.Steps {
		acc += s.Amount
		if s.LeveledUp && acc != expTestTable[s.Level] {
			t.Fatalf("升级到 %d 级时累计经验=%d, want 表值 %d", s.Level, acc, expTestTable[s.Level])
		}
	}
}

// TestApplyExperienceMaxLevel 锁定等级上限：单个 0 额 MaxLevelReached 包，剩余经验丢弃。
func TestApplyExperienceMaxLevel(t *testing.T) {
	g := ApplyExperience(5, 1000, 500, expTestTable, 5, false)
	if !g.MaxLevelReached {
		t.Fatal("已达上限应置 MaxLevelReached")
	}
	if len(g.Steps) != 1 {
		t.Fatalf("上限分支应只 1 步, got %+v", g.Steps)
	}
	if s := g.Steps[0]; s.Amount != 0 || s.Type != ExperienceMaxLevelReached || s.LeveledUp {
		t.Fatalf("上限步应为 (0, MaxLevelReached): %+v", s)
	}
	if g.NewLevel != 5 || g.NewExperience != 1000 || g.LevelsGained != 0 {
		t.Fatalf("上限分支不得改动等级/经验: %+v", g)
	}
}

// TestApplyExperiencePreventOverflow 锁定原版 PreventExperienceOverflow：
// 升一级后即停，剩余经验不入账（本仓该配置未启用，但函数语义必须保留）。
func TestApplyExperiencePreventOverflow(t *testing.T) {
	g := ApplyExperience(1, 0, 1000, expTestTable, 5, true)
	if g.LevelsGained != 1 || g.NewLevel != 2 || g.NewExperience != 100 {
		t.Fatalf("溢出保护下应只升 1 级到边界: %+v", g)
	}
	if len(g.Steps) != 1 || !g.Steps[0].LeveledUp {
		t.Fatalf("应只有 1 个升级步: %+v", g.Steps)
	}
}

// TestApplyExperienceZeroGain 锁定零/负经验：不产生任何包（原版 AddAfterKillAsync 早退）。
func TestApplyExperienceZeroGain(t *testing.T) {
	for _, gained := range []int64{0, -1} {
		g := ApplyExperience(1, 50, gained, expTestTable, 5, false)
		if len(g.Steps) != 0 {
			t.Fatalf("gained=%d 不应产生步骤: %+v", gained, g.Steps)
		}
		if g.NewLevel != 1 || g.NewExperience != 50 {
			t.Fatalf("gained=%d 不得改动状态: %+v", gained, g)
		}
	}
}

// TestApplyExperienceShortTableTerminates 锁定表越界时不会死循环
// （载入时已校验表长 = MaximumLevel+2，此分支仅作防御）。
func TestApplyExperienceShortTableTerminates(t *testing.T) {
	done := make(chan ExperienceGain, 1)
	go func() {
		done <- ApplyExperience(1, 0, 500, []int64{0, 0}, 4, false)
	}()
	g := <-done
	if g.NewLevel < 4 {
		t.Fatalf("应一路顶到上限, got %+v", g)
	}
}
