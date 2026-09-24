package attribute

import (
	"sync"
)

// attribute.go —— BaseAttribute / ComposableAttribute / StatAttribute / ConstValueAttribute
// （对应原版同名文件；聚合公式与钳制语义逐条对照，见包文档）。

// attribute 是"有定义的属性"公共骨架（原版 BaseAttribute）。
type attribute struct {
	definition *AttributeDefinition
	agg        AggregateType
	hub        changeHub
}

func (a *attribute) definitionMax() *float32 {
	if a.definition == nil {
		return nil
	}
	return a.definition.MaximumValue
}

// raise 变化广播（原版 RaiseValueChanged）。
func (a *attribute) raise() { a.hub.raise() }

// ComposableAttribute 聚合属性（原版 ComposableAttribute）。
type ComposableAttribute struct {
	attribute
	mu       sync.Mutex
	elements []Element
	cancels  []func()
	maxValue *float32 // 构造级钳制（原版 _maximumValue）
	cached   *float32
}

// NewComposableAttribute 构造聚合属性。
func NewComposableAttribute(definition *AttributeDefinition, agg AggregateType, maximumValue *float32) *ComposableAttribute {
	return &ComposableAttribute{attribute: attribute{definition: definition, agg: agg}, maxValue: maximumValue}
}

// AddElement 追加元素并使缓存失效（原版 AddElement + ElementChanged）。
func (c *ComposableAttribute) AddElement(e Element) {
	c.mu.Lock()
	c.elements = append(c.elements, e)
	c.cancelCacheLocked()
	cancel := e.Subscribe(c.invalidate)
	c.cancels = append(c.cancels, cancel)
	c.mu.Unlock()
	c.raise()
}

// RemoveElement 移除元素并使缓存失效（原版 RemoveElement）。
func (c *ComposableAttribute) RemoveElement(e Element) {
	c.mu.Lock()
	idx := -1
	for i, x := range c.elements {
		if x == e {
			idx = i
			break
		}
	}
	if idx >= 0 {
		c.elements = append(c.elements[:idx], c.elements[idx+1:]...)
		if idx < len(c.cancels) {
			c.cancels[idx]()
			c.cancels = append(c.cancels[:idx], c.cancels[idx+1:]...)
		}
		c.cancelCacheLocked()
	}
	c.mu.Unlock()
	if idx >= 0 {
		c.raise()
	}
}

// Elements 返回当前元素快照。
func (c *ComposableAttribute) Elements() []Element {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Element(nil), c.elements...)
}

// Value 聚合值（缓存 + 失效，公式见包文档）。
func (c *ComposableAttribute) Value() float32 {
	c.mu.Lock()
	if c.cached != nil {
		v := *c.cached
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	v := c.aggregate()

	c.mu.Lock()
	c.cached = &v
	c.mu.Unlock()
	return v
}

// aggregate 复刻原版 GetAndCacheValue 的公式部分。
func (c *ComposableAttribute) aggregate() float32 {
	elements := c.Elements()
	if len(elements) == 0 {
		return 0
	}
	raw := float32(0)
	multi := float32(1)
	final := float32(0)
	maxVal := float32(0)
	maxSeen := false
	allMulti := true
	for _, e := range elements {
		switch e.AggregateType() {
		case AggregateAddRaw:
			raw += e.Value()
			allMulti = false
		case AggregateMultiplicate:
			multi *= e.Value()
		case AggregateAddFinal:
			final += e.Value()
			allMulti = false
		case AggregateMaximum:
			if !maxSeen || e.Value() > maxVal {
				maxVal = e.Value()
			}
			maxSeen = true
			allMulti = false
		}
	}
	raw += maxVal
	if allMulti && len(elements) > 0 {
		raw = 1
	}

	newValue := raw * multi
	newValue += final
	if c.maxValue != nil && *c.maxValue < newValue {
		newValue = *c.maxValue
	}
	if dm := c.definitionMax(); dm != nil && *dm < newValue {
		newValue = *dm
	}
	return newValue
}

// invalidate 使缓存失效并向上传播（原版 ElementChanged）。
func (c *ComposableAttribute) invalidate() {
	c.mu.Lock()
	c.cancelCacheLocked()
	c.mu.Unlock()
	c.raise()
}

func (c *ComposableAttribute) cancelCacheLocked() { c.cached = nil }

// Subscribe 订阅本属性变化。
func (c *ComposableAttribute) Subscribe(fn func()) func() { return c.hub.subscribe(fn) }

// AggregateType 聚合形态（构造时固定）。
func (c *ComposableAttribute) AggregateType() AggregateType { return c.agg }

// StatAttribute 可加点属性（原版 StatAttribute）。
type StatAttribute struct {
	attribute
	mu   sync.Mutex
	stat float32
}

// NewStatAttribute 构造（baseValue 为初始值）。
func NewStatAttribute(definition *AttributeDefinition, baseValue float32) *StatAttribute {
	return &StatAttribute{attribute: attribute{definition: definition, agg: AggregateAddRaw}, stat: baseValue}
}

// Value getter：按定义 MaximumValue 钳制（原版语义）。
func (s *StatAttribute) Value() float32 {
	s.mu.Lock()
	v := s.stat
	s.mu.Unlock()
	if max := s.definitionMax(); max != nil && v > *max {
		return *max
	}
	return v
}

// SetStat 变化幅度 ≤0.01 时忽略（原版语义）。
func (s *StatAttribute) SetStat(v float32) {
	s.mu.Lock()
	if abs32(s.stat-v) > 0.01 {
		s.stat = v
		s.mu.Unlock()
		s.raise()
		return
	}
	s.mu.Unlock()
}

// Subscribe 订阅本属性变化。
func (s *StatAttribute) Subscribe(fn func()) func() { return s.hub.subscribe(fn) }

// AggregateType 聚合形态（AddRaw）。
func (s *StatAttribute) AggregateType() AggregateType { return s.agg }

// ConstValueAttribute 固定值属性（原版 ConstValueAttribute）。
type ConstValueAttribute struct {
	attribute
	value float32
}

// NewConstValueAttribute 构造。
func NewConstValueAttribute(value float32, definition *AttributeDefinition) *ConstValueAttribute {
	return &ConstValueAttribute{attribute: attribute{definition: definition, agg: AggregateAddRaw}, value: value}
}

// NewConstValueAttributeAgg 构造（指定聚合形态；原版 CreateConstValueAttribute
// 可带 AggregateType，如护盾恢复倍率的 Multiplicate 乘法项 1/75000）。
func NewConstValueAttributeAgg(value float32, definition *AttributeDefinition, agg AggregateType) *ConstValueAttribute {
	return &ConstValueAttribute{attribute: attribute{definition: definition, agg: agg}, value: value}
}

// Value 实现属性接口。
func (c *ConstValueAttribute) Value() float32 { return c.value }

// Subscribe 固定值属性无变化。
func (c *ConstValueAttribute) Subscribe(func()) func() { return func() {} }

// AggregateType 聚合形态（AddRaw）。
func (c *ConstValueAttribute) AggregateType() AggregateType { return c.agg }
