// Package attribute 是 OpenMU AttributeSystem 的 Go 移植（doc/10 T0-a，一切数值的内核）。
//
// 语义逐条对照原版（src/AttributeSystem，net10.0 版）：
//   - ComposableAttribute 聚合公式：raw(AddRaw 之和) += max(Maximum 之最大)；
//     全部元素均为 Multiplicate 时 raw=1；value = raw*multi + final(AddFinal 之和)；
//     先按构造 max 钳制，再按定义 MaximumValue 钳制；空聚合 = 0。
//   - StatAttribute：可加点属性；getter 按定义钳制；setter 忽略 ≤0.01 的变化。
//   - SimpleElement：setter 忽略 ≤0.00001 的变化。
//   - AttributeRelationshipElement：输入之和 ⊗ 操作数（六种 InputOperator）。
//   - AttributeSystem：StatAttribute 原样存放；其余按定义惰性建 ComposableAttribute；
//     值缺失返回 0；RemoveElement 后空聚合从系统移除。
//
// 一致性由 tools/goldenattributes 用原实现产出的向量锁定
// （internal/attribute/testdata/attributes_golden.json）。
//
// 并发策略说明：原版用复制写快照 + 版本计数实现无锁读；Go 侧语义等价、
// 用包级 mutex 串行化（M6 无魔法效果定时器，无热路径竞争）。缓存失效链
// （元素变化 → 聚合失效 → 上层失效）与订阅/退订路径逐条保留。
package attribute

import "sync"

// AttributeDefinition 定义一个属性（原版 AttributeSystem.AttributeDefinition）。
// 以 ID 为等值键（原版是 Guid；Go 侧用其字符串形态——T0-c 导出件/关系引用同键）；
// MaximumValue 为定义级钳制。
type AttributeDefinition struct {
	ID           string
	Designation  string
	Description  string
	MaximumValue *float32
}

// AggregateType 对应原版枚举。
type AggregateType uint8

const (
	// AggregateAddRaw 加到裸基值。
	AggregateAddRaw AggregateType = iota
	// AggregateMultiplicate 乘到裸基值。
	AggregateMultiplicate
	// AggregateAddFinal 加到最终值。
	AggregateAddFinal
	// AggregateMaximum 只取可用值里的最大者加到裸基值（首饰元素抗性）。
	AggregateMaximum
)

// InputOperator 对应原版枚举：输入属性与操作数之间的运算。
type InputOperator uint8

const (
	// InputMultiply 输入 × 操作数。
	InputMultiply InputOperator = iota
	// InputAdd 输入 + 操作数。
	InputAdd
	// InputExponentiate 输入 ^ 操作数。
	InputExponentiate
	// InputExponentiateByAttribute 操作数 ^ 输入。
	InputExponentiateByAttribute
	// InputMaximum max(输入, 操作数)。
	InputMaximum
	// InputMinimum min(输入, 操作数)。
	InputMinimum
)

// Element 是聚合的最小贡献单元（原版 IElement）。
type Element interface {
	Value() float32
	AggregateType() AggregateType
	// Subscribe 订阅"值或形态变化"通知（原版 ValueChanged 事件），返回退订函数。
	Subscribe(fn func()) (cancel func())
}

// changeHub 是极简的订阅列表（原版 event EventHandler 的等价物）。
type changeHub struct {
	mu  sync.Mutex
	fns []func()
}

func (h *changeHub) raise() {
	h.mu.Lock()
	fns := make([]func(), 0, len(h.fns))
	for _, f := range h.fns {
		if f != nil {
			fns = append(fns, f)
		}
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (h *changeHub) subscribe(fn func()) func() {
	h.mu.Lock()
	h.fns = append(h.fns, fn)
	idx := len(h.fns) - 1
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		if idx < len(h.fns) {
			// 置 nil 占位，raise 时跳过（保持其余下标稳定）。
			h.fns[idx] = nil
		}
		h.mu.Unlock()
	}
}

// ConstantElement 固定值元素（原版 ConstantElement）。永不变化，仅作贡献值。
type ConstantElement struct {
	value float32
	agg   AggregateType
}

// NewConstantElement 构造固定值元素。
func NewConstantElement(value float32, agg AggregateType) *ConstantElement {
	return &ConstantElement{value: value, agg: agg}
}

// Value 实现元素接口。
func (e *ConstantElement) Value() float32 { return e.value }

// AggregateType 实现元素接口。
func (e *ConstantElement) AggregateType() AggregateType { return e.agg }

// Subscribe 固定值元素无变化，退订为空操作。
func (e *ConstantElement) Subscribe(func()) func() { return func() {} }

// SimpleElement 可变值元素（原版 SimpleElement）。
type SimpleElement struct {
	mu    sync.Mutex
	hub   changeHub
	value float32
	agg   AggregateType
}

// NewSimpleElement 构造可变值元素。
func NewSimpleElement(value float32, agg AggregateType) *SimpleElement {
	return &SimpleElement{value: value, agg: agg}
}

// Value 实现元素接口。
func (e *SimpleElement) Value() float32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.value
}

// SetValue 变化幅度 ≤0.00001 时忽略（原版语义）。
func (e *SimpleElement) SetValue(v float32) {
	e.mu.Lock()
	changed := abs32(e.value-v) > 0.00001
	if changed {
		e.value = v
	}
	e.mu.Unlock()
	if changed {
		e.hub.raise()
	}
}

// SetAggregateType 变更聚合形态（变化即广播）。
func (e *SimpleElement) SetAggregateType(a AggregateType) {
	e.mu.Lock()
	changed := e.agg != a
	if changed {
		e.agg = a
	}
	e.mu.Unlock()
	if changed {
		e.hub.raise()
	}
}

// AggregateType 实现元素接口。
func (e *SimpleElement) AggregateType() AggregateType {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agg
}

// Subscribe 实现元素接口。
func (e *SimpleElement) Subscribe(fn func()) func() { return e.hub.subscribe(fn) }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
