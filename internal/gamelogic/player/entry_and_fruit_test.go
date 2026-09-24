package player

// entry_and_fruit_test.go —— TRIM-11c 的进图属性需求判定与 TRIM-11d 的果实上限现算。
// 两者都直接吃真实导出件（60_maps.json / 80_attributes.json / 职业表），不造数据。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

func TestMapEntryRequirementFailureRealData(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	icarus, ok := cfg.Map(10)
	if !ok {
		t.Fatal("导出件缺 Icarus(map 10)")
	}
	c := &entity.Character{Name: "walker", ClassNumber: 0, Level: 200, MapNumber: 4, X: 18, Y: 250}

	text, rejected := MapEntryRequirementFailure(cfg, c, icarus)
	if !rejected {
		t.Fatal("无翅膀应被 Icarus 的需求拦住")
	}
	// 文案是属性定义的 Description（原版同口径），不是 designation。
	want := "You can enter Icarus only with wings, dinorant, fenrir."
	if text != want {
		t.Fatalf("文案应为属性 Description, got %q", text)
	}

	// 需求属性到位后放行（对照装备翅膀在属性系统里给出的那条 1）。
	c.AttributeBonuses = []entity.AttributeBonus{{
		AttributeID: "ec34c673-84de-4811-8962-cd2164a2248c",
		Designation: "Requirement of the Icarus map.", Value: 1,
	}}
	if _, rejected := MapEntryRequirementFailure(cfg, c, icarus); rejected {
		t.Fatal("满足需求后应放行")
	}

	// 无需求的地图恒放行；nil 目标也放行（调用方已判过门目标）。
	lorencia, _ := cfg.Map(0)
	if _, rejected := MapEntryRequirementFailure(cfg, c, lorencia); rejected {
		t.Fatal("Lorencia 没有进图需求，不该拒")
	}
	if _, rejected := MapEntryRequirementFailure(cfg, c, nil); rejected {
		t.Fatal("nil 地图不该拒")
	}
}

// TestResolveCharStatsComputesFruitCap 锁定 TRIM-11d：果实上限按等级+职业策略现算，
// 且正/负两侧同值（原版 GetMaximumFruitPoints 一次填两处）。
// 表值来自 action.MaxFruitPoints（divisor 400 → 1 级 2 点、100 级 22、400 级 127）。
func TestResolveCharStatsComputesFruitCap(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		level  uint16
		want   uint16
		used   uint16
		carrys bool
	}{
		{level: 1, want: 2},
		{level: 100, want: 22},
		{level: 220, want: 56},
		{level: 400, want: 127},
	}
	for _, tc := range cases {
		c := &entity.Character{Name: "fruit", ClassNumber: 0, Level: tc.level}
		st, err := ResolveCharStats(cfg, c)
		if err != nil {
			t.Fatal(err)
		}
		if st.MaxFruitPoints != tc.want || st.MaxNegFruit != tc.want {
			t.Fatalf("等级 %d 的果实上限 = %d/%d, want %d/%d",
				tc.level, st.MaxFruitPoints, st.MaxNegFruit, tc.want, tc.want)
		}
	}

	// 已用量来自角色态保留，不被重算冲掉。
	c := &entity.Character{Name: "fruit2", ClassNumber: 0, Level: 100,
		Stats: &entity.CharStats{UsedFruitPoints: 7, UsedNegFruit: 3}}
	st, err := ResolveCharStats(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	if st.UsedFruitPoints != 7 || st.UsedNegFruit != 3 {
		t.Fatalf("已用果实点数应保留: %d/%d", st.UsedFruitPoints, st.UsedNegFruit)
	}
	if st.MaxFruitPoints != 22 {
		t.Fatalf("100 级上限应 22, got %d", st.MaxFruitPoints)
	}
}
