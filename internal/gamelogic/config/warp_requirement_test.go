package config

// warp_requirement_test.go —— 进图/传送等级门槛的职业折减（TRIM-11b），对照
// CharacterExtensions.GetEffectiveMoveLevelRequirement:113-126。

import (
	"sort"
	"testing"
)

func TestEffectiveMoveLevelRequirement(t *testing.T) {
	special := &CharacterClass{Number: 12, LevelWarpRequirementReductionPercent: 34}
	plain := &CharacterClass{Number: 0, LevelWarpRequirementReductionPercent: 0}
	cases := []struct {
		class *CharacterClass
		want  string
		in    int
		out   int
	}{
		{special, "MG 280 → 184", 280, 184},
		{special, "MG 160 → 105", 160, 105},
		{special, "MG 50 → 33", 50, 33},
		// 整数除法向下取整：120 × 66 / 100 = 79.2 → 79。
		{special, "MG 120 → 79", 120, 79},
		// 原版硬特例：门槛 400 一律不折减（Peace Swamp）。
		{special, "MG 400 不折减", 400, 400},
		{plain, "普通职业 280 原样", 280, 280},
		{nil, "职业缺失原样", 280, 280},
	}
	for _, tc := range cases {
		if got := tc.class.EffectiveMoveLevelRequirement(tc.in); got != tc.out {
			t.Fatalf("%s：got %d, want %d", tc.want, got, tc.out)
		}
	}
}

// 导出件里的实际分布：只有 MG/DL/RF 及其转职带 34%（原版 Math.Ceiling(100/3)，
// 更新插件把历史的 33 修成 34），其余职业为 0。
func TestWarpReductionDistributionInExport(t *testing.T) {
	cfg := mustLoad(t)
	var reduced []int
	for i := range cfg.CharacterClasses {
		c := &cfg.CharacterClasses[i]
		switch c.LevelWarpRequirementReductionPercent {
		case 0:
		case 34:
			reduced = append(reduced, c.Number)
		default:
			t.Fatalf("职业 %d 的折减 %d 非 0/34", c.Number, c.LevelWarpRequirementReductionPercent)
		}
	}
	want := []int{12, 13, 16, 17, 24, 25}
	if len(reduced) != len(want) {
		t.Fatalf("带折减的职业 %v, want %v", reduced, want)
	}
	sort.Ints(reduced) // 导出件按建职顺序而非编号序
	for i, n := range want {
		if reduced[i] != n {
			t.Fatalf("带折减的职业 %v, want %v", reduced, want)
		}
	}
}
