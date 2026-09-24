package config

import (
	"testing"

	"mugo/internal/attribute"
)

// TestBuildClassAttributeSystem 是 T1-0 验收：Dark Wizard 的导出件
// （StatAttributes/BaseAttributes/AttributeCombinations）可在 Go 侧装配成
// 真实属性系统，且派生关系图可计算（原版 CharacterClass 装配语义重放）。
func TestBuildClassAttributeSystem(t *testing.T) {
	c := loadSeason6(t)

	// 职业编号 0 = Dark Wizard（原版 CharacterClassNumber）。
	system, err := c.BuildClassAttributeSystem(0)
	if err != nil {
		t.Fatalf("装配 Dark Wizard 属性系统失败: %v", err)
	}

	cls, _ := c.Class(0)
	if len(cls.AttributeCombinations) == 0 {
		t.Fatal("Dark Wizard 应有派生关系")
	}

	// 每个关系的目标属性都可读出聚合值（图已连成、聚合无 panic）；
	// 至少一个目标非零（如 Total Strength ← Base Strength 派生）。
	nonZero := 0
	for _, r := range cls.AttributeCombinations {
		if r.Target == nil {
			continue
		}
		def, ok := c.AttributeDefinitionByID(r.Target.ID)
		if !ok {
			t.Fatalf("目标属性 %s 未定义", r.Target.ID)
		}
		probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
		if v := system.GetValueOfAttribute(probe); v != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("所有派生目标值均为 0——关系图未正确建立")
	}
}

// TestBuildClassAttributeSystemAllClasses 全职业装配冒烟：任何职业都不应报错。
func TestBuildClassAttributeSystemAllClasses(t *testing.T) {
	c := loadSeason6(t)
	for i := range c.CharacterClasses {
		cls := &c.CharacterClasses[i]
		if _, err := c.BuildClassAttributeSystem(cls.Number); err != nil {
			t.Fatalf("职业 %s(%d) 装配失败: %v", cls.Name, cls.Number, err)
		}
	}
}
