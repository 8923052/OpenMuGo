package attribute

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// golden 是 tools/goldenattributes 产出的向量（internal/attribute/testdata）。
type golden struct {
	Step1 struct {
		Defense float32 `json:"defense"`
		Speed   float32 `json:"speed"`
		HP      float32 `json:"hp"`
		MaxHP   float32 `json:"maxhp"`
		Agility float32 `json:"agility_value"`
	} `json:"step1_initial"`
	Step2 struct {
		Defense float32 `json:"defense"`
		Speed   float32 `json:"speed"`
		HP      float32 `json:"hp"`
		MaxHP   float32 `json:"maxhp"`
	} `json:"step2_after_str_150"`
	Step3Extra float32 `json:"step3_extra_agi_element"`
	Step3After float32 `json:"step3_after_remove"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/attributes_golden.json")
	if err != nil {
		t.Fatalf("读属性 golden 失败（tools/goldenattributes 生成）: %v", err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("解析属性 golden 失败: %v", err)
	}
	return g
}

// buildScenario 复刻 tools/goldenattributes 的场景（定义/关系逐条对应）。
func buildScenario(t *testing.T) (*AttributeSystem, *AttributeDefinition, *AttributeDefinition, *AttributeDefinition, *AttributeDefinition, *AttributeDefinition, *AttributeDefinition) {
	t.Helper()
	mustDef := func(id string, name string, max *float32) *AttributeDefinition {
		d := &AttributeDefinition{ID: id, Designation: name}
		if max != nil {
			m := *max
			d.MaximumValue = &m
		}
		return d
	}
	f32 := func(v float32) *float32 { return &v }

	str := mustDef("00000001", "Strength", nil)
	agi := mustDef("00000002", "Agility", nil)
	def := mustDef("00000003", "Defense", f32(5000))
	speed := mustDef("00000004", "Speed", nil)
	hp := mustDef("00000005", "HP", nil)
	maxhp := mustDef("00000006", "MaxHP", f32(100))
	multOperand := mustDef("00000007", "MultOperand", nil)

	system := NewAttributeSystem(
		[]any{
			NewStatAttribute(str, 100),
			NewStatAttribute(agi, 57.5),
		},
		[]any{
			NewConstValueAttribute(20, def),
			NewConstValueAttribute(50, hp),
		},
		[]AttributeRelationship{
			{TargetAttribute: def, InputOperand: 2, InputAttribute: agi, InputOperator: InputMultiply, AggregateType: AggregateAddRaw},
			{TargetAttribute: def, InputOperand: 0.1, InputAttribute: str, InputOperator: InputMultiply, AggregateType: AggregateMultiplicate},
			{TargetAttribute: def, InputOperand: 1, InputAttribute: str, InputOperator: InputMultiply, AggregateType: AggregateAddFinal},
			{TargetAttribute: hp, InputOperand: 0.3, InputAttribute: str, InputOperator: InputMultiply, AggregateType: AggregateMaximum},
			{TargetAttribute: speed, InputOperand: 2, InputAttribute: str, InputOperator: InputExponentiate, AggregateType: AggregateAddRaw},
			{TargetAttribute: speed, OperandAttribute: multOperand, InputAttribute: str, AggregateType: AggregateMultiplicate},
			{TargetAttribute: maxhp, InputOperand: 5, InputAttribute: str, InputOperator: InputMultiply, AggregateType: AggregateAddRaw},
		},
	)
	system.AddElement(NewConstValueAttribute(3, multOperand), multOperand)
	return system, str, agi, def, speed, hp, maxhp
}

func approx(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	scale := math.Abs(float64(b))
	if scale < 1 {
		scale = 1
	}
	return float64(d) <= 1e-4*scale+1e-4
}

// TestAttributeGolden 锁定 T0-a：四聚合 + 六算符 + 定义钳制 + 关系传播，
// 全部值与 OpenMU AttributeSystem 原实现产出逐值一致（float32 精度内）。
func TestAttributeGolden(t *testing.T) {
	g := loadGolden(t)
	system, str, agi, def, speed, hp, maxhp := buildScenario(t)

	// step1：初始态。
	if got := system.GetValueOfAttribute(def); !approx(got, g.Step1.Defense) {
		t.Fatalf("step1 defense = %v, want %v", got, g.Step1.Defense)
	}
	if got := system.GetValueOfAttribute(speed); !approx(got, g.Step1.Speed) {
		t.Fatalf("step1 speed = %v, want %v", got, g.Step1.Speed)
	}
	if got := system.GetValueOfAttribute(hp); !approx(got, g.Step1.HP) {
		t.Fatalf("step1 hp = %v, want %v", got, g.Step1.HP)
	}
	if got := system.GetValueOfAttribute(maxhp); !approx(got, g.Step1.MaxHP) {
		t.Fatalf("step1 maxhp = %v, want %v（定义钳制 100）", got, g.Step1.MaxHP)
	}
	if got := system.GetValueOfAttribute(agi); !approx(got, g.Step1.Agility) {
		t.Fatalf("step1 agility = %v, want %v", got, g.Step1.Agility)
	}

	// step2：STR 100→150，派生属性联动。
	if !system.SetStatAttribute(str, 150) {
		t.Fatal("SetStatAttribute(str) 应成功")
	}
	if got := system.GetValueOfAttribute(def); !approx(got, g.Step2.Defense) {
		t.Fatalf("step2 defense = %v, want %v", got, g.Step2.Defense)
	}
	if got := system.GetValueOfAttribute(speed); !approx(got, g.Step2.Speed) {
		t.Fatalf("step2 speed = %v, want %v", got, g.Step2.Speed)
	}
	if got := system.GetValueOfAttribute(hp); !approx(got, g.Step2.HP) {
		t.Fatalf("step2 hp = %v, want %v", got, g.Step2.HP)
	}
	if got := system.GetValueOfAttribute(maxhp); !approx(got, g.Step2.MaxHP) {
		t.Fatalf("step2 maxhp = %v, want %v", got, g.Step2.MaxHP)
	}

	// step3：加一个额外 AGI 关系元素再移除（RemoveElement 语义）。
	system.SetStatAttribute(str, 100)
	defComposable := system.GetComposableAttribute(def)
	extra := newRelationshipElement(
		[]Element{system.GetOrCreateAttribute(agi)},
		NewConstantElement(2, AggregateAddRaw),
		InputMultiply, AggregateAddRaw)
	defComposable.AddElement(extra)
	if got := system.GetValueOfAttribute(def); !approx(got, g.Step3Extra) {
		t.Fatalf("step3 加元素后 defense = %v, want %v", got, g.Step3Extra)
	}
	defComposable.RemoveElement(extra)
	if got := system.GetValueOfAttribute(def); !approx(got, g.Step3After) {
		t.Fatalf("step3 移除后 defense = %v, want %v", got, g.Step3After)
	}
}
