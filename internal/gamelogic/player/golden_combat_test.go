package player

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/config"
)

// goldenEntry 对应 tools/goldencombat 产出的向量（原版 AttributeSystem 原实现计算）。
type goldenEntry struct {
	ClassName   string             `json:"class_name"`
	ClassNumber int                `json:"class_number"`
	Level       int                `json:"level"`
	Values      map[string]float64 `json:"values"`
}

func loadCombatGolden(t *testing.T) []goldenEntry {
	t.Helper()
	raw, err := os.ReadFile("testdata/class_combat_golden.json")
	if err != nil {
		t.Fatalf("读战斗属性 golden 失败（tools/goldencombat 生成）: %v", err)
	}
	var payload struct {
		Entries []goldenEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("解析 golden 失败: %v", err)
	}
	if len(payload.Entries) == 0 {
		t.Fatal("golden 向量为空")
	}
	return payload.Entries
}

// approxF32 以 float32 精度比较（原版 AttributeSystem 内部是 float 运算）。
func approxF32(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale < 1 {
		scale = 1
	}
	return math.Abs(a-b) <= 1e-4*scale+1e-4
}

// TestClassAttributeGolden 是 T2-0 的核心验收：Go 侧属性装配（T1-2 桥）
// 与 OpenMU AttributeSystem 原实现（tools/goldencombat）逐值一致——
// 覆盖 5 个可创建职业 × 2 个等级点的全部战斗相关属性。
func TestClassAttributeGolden(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range loadCombatGolden(t) {
		g := g
		t.Run(g.ClassName, func(t *testing.T) {
			system, err := cfg.BuildCharacterAttributeSystem(g.ClassNumber, map[string]float32{
				"Level": float32(g.Level),
			})
			if err != nil {
				t.Fatalf("装配失败: %v", err)
			}
			for designation, want := range g.Values {
				def, ok := cfg.AttributeByName(designation)
				if !ok {
					t.Fatalf("Go 侧找不到属性定义 %q", designation)
				}
				probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
				got := float64(system.GetValueOfAttribute(probe))
				if !approxF32(got, want) {
					t.Errorf("%s[%s] = %v, want %v", g.ClassName, designation, got, want)
				}
			}
		})
	}
}
