package config

// map_index_test.go —— 守住"索引指针 ↔ 切片排布"的不变量（TRIM-11 期间发现的既有缺陷：
// buildIndex 先按 &c.Maps[i] 建索引、末尾又 sort 了 c.Maps，导致 Map(n) 大面积返回错图）。

import "testing"

func TestMapIndexMatchesSliceLayout(t *testing.T) {
	cfg := mustLoad(t)
	if n := len(cfg.Maps); n < 70 {
		t.Fatalf("地图数 %d，导出件异常", n)
	}
	// 1) 每个主变体都能按编号取回**自己**（不是别的元素）。
	for i := range cfg.Maps {
		want := &cfg.Maps[i]
		if want.Discriminator != 0 {
			continue
		}
		got, ok := cfg.Map(want.Number)
		if !ok || got != want {
			t.Fatalf("Map(%d) 应指向该主变体（name=%q），got ok=%v %+v", want.Number, want.Name, ok, got)
		}
	}
	// 2) 切片确按 (编号, 变体) 升序（稳定遍历序是 buildIndex 的前提）。
	for i := 1; i < len(cfg.Maps); i++ {
		a, b := cfg.Maps[i-1], cfg.Maps[i]
		if a.Number > b.Number || (a.Number == b.Number && a.Discriminator > b.Discriminator) {
			t.Fatalf("地图切片未排序：[%d]=(%d,%d) [%d]=(%d,%d)", i-1, a.Number, a.Discriminator, i, b.Number, b.Discriminator)
		}
	}
}

// TestMapIndexFindsVariantHostages 用曾经被排布打乱的具体编号做定点回归。
func TestMapIndexFindsVariantHostages(t *testing.T) {
	cfg := mustLoad(t)
	cases := []struct {
		number int
		name   string
		reqs   int
	}{
		// 9/32 在 S6 数据里**没有** discriminator=0 的主变体，取回的是最小变体号那条。
		{9, "Devil Square 1", 0},
		{10, "Icarus", 1}, // 曾被解析成 Devil Square 的某个变体
		{39, "Kanturu Event", 1},
		{57, "LaCleon", 0},
	}
	for _, tc := range cases {
		m, ok := cfg.Map(tc.number)
		if !ok {
			t.Fatalf("Map(%d) 不存在", tc.number)
		}
		if m.Number != tc.number {
			t.Fatalf("Map(%d) 返回了 %d(%s)", tc.number, m.Number, m.Name)
		}
		if tc.name != "" && m.Name != tc.name {
			t.Fatalf("Map(%d).Name = %q, want %q", tc.number, m.Name, tc.name)
		}
		if tc.reqs != len(m.Requirements) {
			t.Fatalf("Map(%d) 需求数 %d, want %d", tc.number, len(m.Requirements), tc.reqs)
		}
	}
}
