// attribute_bridge.go —— 把导出件的职业属性装配成 internal/attribute 系统
// （doc/15 §10 T1-0 的"关系图重放"验收 + T1-2 的消费入口）。
package config

import (
	"fmt"

	"mugo/internal/attribute"
)

// BuildClassAttributeSystem 按职业装配真实属性系统（无角色个性化覆盖）。
func (c *GameConfig) BuildClassAttributeSystem(classNumber int) (*attribute.AttributeSystem, error) {
	return c.BuildCharacterAttributeSystem(classNumber, nil)
}

// BuildCharacterAttributeSystem 按职业装配真实属性系统，并以 overrides 覆盖
// 指定 designation 的属性值（角色当前态：Level、Base Strength、Current Health…；
// 对应原版 ItemAwareAttributeSystem 里 character.Attributes 对类初始值的覆盖）。
// 关系引用带 id：按 id 精确解析（数据存在重复 designation）。
func (c *GameConfig) BuildCharacterAttributeSystem(classNumber int, overrides map[string]float32) (*attribute.AttributeSystem, error) {
	cls, ok := c.Class(classNumber)
	if !ok {
		return nil, fmt.Errorf("config: 职业 %d 不存在", classNumber)
	}

	meta := func(id string) (*attribute.AttributeDefinition, error) {
		d, ok := c.AttributeDefinitionByID(id)
		if !ok {
			return nil, fmt.Errorf("config: 属性 %s 未定义", id)
		}
		max := d.MaximumValue
		return &attribute.AttributeDefinition{ID: id, Designation: d.Designation, MaximumValue: max}, nil
	}
	byName := func(designation string) (*attribute.AttributeDefinition, error) {
		for i := range c.Attributes {
			if c.Attributes[i].Designation == designation {
				return meta(c.Attributes[i].ID)
			}
		}
		return nil, fmt.Errorf("config: 未找到属性定义 %q", designation)
	}

	var stats []any
	var bases []any
	for _, s := range cls.StatAttributes {
		def, err := byName(s.Designation)
		if err != nil {
			return nil, err
		}
		v := s.BaseValue
		if o, ok := overrides[s.Designation]; ok {
			v = float64(o)
		}
		stats = append(stats, attribute.NewStatAttribute(def, float32(v)))
	}
	for _, b := range cls.BaseAttributes {
		def, err := byName(b.Designation)
		if err != nil {
			return nil, err
		}
		agg := attribute.AggregateAddRaw
		if b.AggregateType != "" {
			if agg, err = parseAggregateType(b.AggregateType); err != nil {
				return nil, err
			}
		}
		bases = append(bases, attribute.NewConstValueAttributeAgg(float32(b.Value), def, agg))
	}

	system := attribute.NewAttributeSystem(stats, bases, nil)
	for _, r := range cls.AttributeCombinations {
		if err := c.addRelationship(system, r); err != nil {
			return nil, err
		}
	}
	return system, nil
}

// addRelationship 把一条导出关系加进系统（解析 + 枚举转换）。
func (c *GameConfig) addRelationship(system *attribute.AttributeSystem, r AttributeRelationship) error {
	op, err := parseInputOperator(r.InputOperator)
	if err != nil {
		return err
	}
	agg, err := parseAggregateType(r.AggregateType)
	if err != nil {
		return err
	}
	if r.Target == nil || r.Input == nil {
		return fmt.Errorf("config: 关系缺少 target/input")
	}
	target, err := c.attributeDef(r.Target)
	if err != nil {
		return err
	}
	input, err := c.attributeDef(r.Input)
	if err != nil {
		return err
	}
	rel := attribute.AttributeRelationship{
		TargetAttribute: target,
		InputAttribute:  input,
		InputOperand:    r.InputOperand,
		InputOperator:   op,
		AggregateType:   agg,
	}
	if r.Operand != nil {
		operand, err := c.attributeDef(r.Operand)
		if err != nil {
			return err
		}
		rel.OperandAttribute = operand
	}
	system.AddAttributeRelationship(rel, system, agg)
	return nil
}

// attributeDef 把导出引用转成 attribute 定义（按 id）。
func (c *GameConfig) attributeDef(ref *AttributeRef) (*attribute.AttributeDefinition, error) {
	d, ok := c.AttributeDefinitionByID(ref.ID)
	if !ok {
		return nil, fmt.Errorf("config: 属性 %q(%s) 未定义", ref.Designation, ref.ID)
	}
	max := d.MaximumValue
	return &attribute.AttributeDefinition{ID: ref.ID, Designation: d.Designation, MaximumValue: max}, nil
}
