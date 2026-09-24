package attribute

import (
	"fmt"
	"math"
	"sync"
)

// relationship.go —— AttributeRelationship / AttributeRelationshipElement
// （对应原版同名文件；六种 InputOperator 的求值语义逐条对照）。

// AttributeRelationship 描述两个属性之间的关系（原版 AttributeRelationship）。
type AttributeRelationship struct {
	TargetAttribute *AttributeDefinition
	InputAttribute  *AttributeDefinition
	// OperandAttribute 非空时取代 InputOperand 作为操作数（其值可联动）。
	OperandAttribute *AttributeDefinition
	InputOperand     float32
	InputOperator    InputOperator
	AggregateType    AggregateType
}

// relationshipElement 关系元素（原版 AttributeRelationshipElement）：
// value = (Σ inputs) ⊗ operand，按 InputOperator 求值；输入或操作数变化即失效。
type relationshipElement struct {
	mu      sync.Mutex
	hub     changeHub
	inputs  []Element
	operand Element
	op      InputOperator
	agg     AggregateType
	cached  *float32
	cancels []func()
}

func (e *relationshipElement) Value() float32 {
	e.mu.Lock()
	if e.cached != nil {
		v := *e.cached
		e.mu.Unlock()
		return v
	}
	e.mu.Unlock()

	v := e.calculate()
	e.mu.Lock()
	e.cached = &v
	e.mu.Unlock()
	return v
}

// calculate 复刻原版 CalculateValue（六算符）。
func (e *relationshipElement) calculate() float32 {
	sum := float32(0)
	for _, in := range e.inputs {
		sum += in.Value()
	}
	operand := e.operand.Value()
	switch e.op {
	case InputMultiply:
		return sum * operand
	case InputAdd:
		return sum + operand
	case InputExponentiate:
		return float32(math.Pow(float64(sum), float64(operand)))
	case InputExponentiateByAttribute:
		return float32(math.Pow(float64(operand), float64(sum)))
	case InputMaximum:
		return float32(math.Max(float64(sum), float64(operand)))
	case InputMinimum:
		return float32(math.Min(float64(sum), float64(operand)))
	default:
		panic(fmt.Sprintf("attribute: 未知 InputOperator %d", e.op))
	}
}

func (e *relationshipElement) invalidate() {
	e.mu.Lock()
	e.cached = nil
	e.mu.Unlock()
	e.hub.raise()
}

// AggregateType 聚合形态（由关系指定）。
func (e *relationshipElement) AggregateType() AggregateType { return e.agg }

// Subscribe 订阅本元素变化。
func (e *relationshipElement) Subscribe(fn func()) func() { return e.hub.subscribe(fn) }

// AttributeSystem 属性系统（原版 AttributeSystem）：一个角色的全部属性。
type AttributeSystem struct {
	mu         sync.Mutex
	attributes map[string]any // *StatAttribute | *ComposableAttribute（键 = 定义 ID）
}

// NewAttributeSystem 构造：stat 原样存放，base 包装为聚合，relationships 建立派生。
func NewAttributeSystem(statAttributes, baseAttributes []any, relationships []AttributeRelationship) *AttributeSystem {
	s := &AttributeSystem{attributes: make(map[string]any)}
	for _, a := range statAttributes {
		switch v := a.(type) {
		case *StatAttribute:
			s.attributes[v.definition.ID] = v
		default:
			panic("attribute: statAttributes 仅接受 *StatAttribute")
		}
	}
	for _, a := range baseAttributes {
		switch v := a.(type) {
		case *ConstValueAttribute:
			s.AddElement(v, v.definition)
		case *StatAttribute:
			s.AddElement(v, v.definition)
		default:
			panic("attribute: baseAttributes 类型不支持")
		}
	}
	for _, rel := range relationships {
		s.AddAttributeRelationship(rel, s, rel.AggregateType)
	}
	return s
}

// GetOrCreateAttribute 取属性，缺失则建空聚合（原版 GetOrCreateAttribute）。
func (s *AttributeSystem) GetOrCreateAttribute(def *AttributeDefinition) Element {
	s.mu.Lock()
	existing, ok := s.attributes[def.ID]
	if !ok {
		c := NewComposableAttribute(def, AggregateAddRaw, nil)
		s.attributes[def.ID] = c
		existing = c
	}
	s.mu.Unlock()
	return existing.(Element)
}

// AddElement 向目标属性追加元素（非聚合目标报错，原版同）。
func (s *AttributeSystem) AddElement(e Element, target *AttributeDefinition) {
	s.mu.Lock()
	existing, ok := s.attributes[target.ID]
	if !ok {
		c := NewComposableAttribute(target, AggregateAddRaw, nil)
		s.attributes[target.ID] = c
		existing = c
	}
	s.mu.Unlock()
	composable, ok := existing.(*ComposableAttribute)
	if !ok {
		panic(fmt.Sprintf("attribute: %s 不是可聚合属性", target.Designation))
	}
	composable.AddElement(e)
}

// RemoveElement 从目标属性移除元素；聚合变空后从系统移除（原版语义）。
func (s *AttributeSystem) RemoveElement(e Element, target *AttributeDefinition) {
	s.mu.Lock()
	existing, ok := s.attributes[target.ID]
	if !ok {
		s.mu.Unlock()
		return
	}
	composable, ok := existing.(*ComposableAttribute)
	if !ok {
		s.mu.Unlock()
		panic(fmt.Sprintf("attribute: %s 不是可聚合属性", target.Designation))
	}
	s.mu.Unlock()

	composable.RemoveElement(e)

	// 原版：移除后仍为空（且仍是同一实例）才删除。
	if len(composable.Elements()) == 0 {
		s.mu.Lock()
		if cur, ok := s.attributes[target.ID]; ok && cur == any(composable) {
			delete(s.attributes, target.ID)
		}
		s.mu.Unlock()
	}
}

// GetValueOfAttribute 读属性值；缺失返回 0；按定义 MaximumValue 钳制（原版语义）。
func (s *AttributeSystem) GetValueOfAttribute(def *AttributeDefinition) float32 {
	if def == nil {
		return 0
	}
	s.mu.Lock()
	existing, ok := s.attributes[def.ID]
	s.mu.Unlock()
	if !ok {
		return 0
	}
	e := existing.(Element)
	v := e.Value()
	if ba, isBase := baseOf(e); isBase && ba.definition != nil && ba.definition.MaximumValue != nil && v > *ba.definition.MaximumValue {
		return *ba.definition.MaximumValue
	}
	return v
}

func baseOf(e Element) (*attribute, bool) {
	switch v := e.(type) {
	case *ComposableAttribute:
		return &v.attribute, true
	case *StatAttribute:
		return &v.attribute, true
	case *ConstValueAttribute:
		return &v.attribute, true
	}
	return nil, false
}

// SetStatAttribute 仅对 StatAttribute 生效（原版语义）。
func (s *AttributeSystem) SetStatAttribute(def *AttributeDefinition, newValue float32) bool {
	s.mu.Lock()
	existing, ok := s.attributes[def.ID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	stat, ok := existing.(*StatAttribute)
	if !ok {
		return false
	}
	stat.SetStat(newValue)
	return true
}

// GetComposableAttribute 取聚合属性（原版 GetComposableAttribute）。
func (s *AttributeSystem) GetComposableAttribute(def *AttributeDefinition) *ComposableAttribute {
	e := s.GetOrCreateAttribute(def)
	c, _ := e.(*ComposableAttribute)
	return c
}

// AddAttributeRelationship 建立派生关系（原版 AddAttributeRelationship 两个重载的合并）。
func (s *AttributeSystem) AddAttributeRelationship(rel AttributeRelationship, source AttributeSystemHolder, agg AggregateType) {
	target := rel.TargetAttribute
	if target == nil {
		panic("attribute: TargetAttribute 未初始化")
	}
	input := rel.InputAttribute
	if input == nil {
		panic("attribute: InputAttribute 未初始化")
	}
	te := s.GetOrCreateAttribute(target)
	composable, ok := te.(*ComposableAttribute)
	if !ok {
		return // 原版：目标不是聚合属性时静默跳过
	}
	inputElement := source.GetOrCreateAttribute(input)

	var operand Element
	if rel.OperandAttribute != nil {
		operand = source.GetOrCreateAttribute(rel.OperandAttribute)
	} else {
		operand = NewConstantElement(rel.InputOperand, AggregateAddRaw)
	}
	re := newRelationshipElement([]Element{inputElement}, operand, rel.InputOperator, agg)
	composable.AddElement(re)
}

// newRelationshipElement 构造关系元素（原版构造函数的订阅部分）。
func newRelationshipElement(inputs []Element, operand Element, op InputOperator, agg AggregateType) *relationshipElement {
	e := &relationshipElement{inputs: inputs, operand: operand, op: op, agg: agg}
	invalidate := e.invalidate
	for _, in := range inputs {
		in.Subscribe(invalidate)
	}
	operand.Subscribe(invalidate)
	return e
}

// AttributeSystemHolder 是关系建立时需要的系统视图（原版 IAttributeSystem 的使用面）。
type AttributeSystemHolder interface {
	GetOrCreateAttribute(def *AttributeDefinition) Element
}
