package config

// combo.go —— 连击定义（TRIM-06），对照 DataModel/Configuration/{SkillComboDefinition,SkillComboStep}.cs。
//
// 原版只有 Blade Knight 显式持有 ComboDefinition（SkillsInitializer.cs:572-605）；
// Blade Master / Duel Master 靠 Player.DetermineComboDefinition（Player.cs:1610-1626）
// **沿"谁的 NextGenerationClass 是我"往回走**继承。导出件只落"本人持有的"，
// 继承解析放在这里，保持与原版同一算法。

import (
	"fmt"
	"sort"
)

// SkillCombo 是一套连击定义。
type SkillCombo struct {
	Name string `json:"name"`
	// MaximumCompletionMS 是"整套连击必须在多久内打完"（原版 TimeSpan，S6=3000ms）。
	// 超时后的第一手会先把状态机复位到起始，再按"错序"处理。
	MaximumCompletionMS int64            `json:"maximum_completion_ms"`
	Steps               []SkillComboStep `json:"steps"`
}

// SkillComboStep 是一步（同一 Order 可有多条 = "这一手的任意一招"）。
type SkillComboStep struct {
	Skill   int    `json:"skill"`
	Name    string `json:"skill_name"`
	Order   int    `json:"order"`
	IsFinal bool   `json:"is_final_step"`
}

// StepOrders 返回出现过的 Order（升序）。
func (s *SkillCombo) StepOrders() []int {
	seen := make(map[int]bool)
	for _, st := range s.Steps {
		seen[st.Order] = true
	}
	out := make([]int, 0, len(seen))
	for o := range seen {
		out = append(out, o)
	}
	sort.Ints(out)
	return out
}

// MatchesAt 报告"顺序 nextOrder 这一手是否接受技能 skillNumber"，并给出该步是否终结步。
// 原版建态时的过滤 `p.RequiredSkill != step.Skill`（ComboStateMachine.cs:131）使"同一招不能连着两下"
// —— 这条由调用方（状态机）用上一手技能号判定，本函数只管"这一手能不能是它"。
func (s *SkillCombo) MatchesAt(nextOrder, skillNumber int) (isFinal bool, ok bool) {
	for _, st := range s.Steps {
		if st.Order == nextOrder && st.Skill == skillNumber {
			return st.IsFinal, true
		}
	}
	return false, false
}

// ComboForClass 解析该职业的连击定义（含沿继承链回溯）；nil 表示这个职业没有连击。
// 对照 Player.cs:1610-1626：先看本人，再反复找"NextClass 指向我"的职业（即上一转），直到有定义或走尽。
func (c *GameConfig) ComboForClass(classNumber int) *SkillCombo {
	current := classNumber
	for {
		cls, ok := c.Class(current)
		if !ok {
			return nil
		}
		if cls.Combo != nil {
			return cls.Combo
		}
		prev, ok := c.previousClass(cls.Number)
		if !ok {
			return nil
		}
		current = prev
	}
}

// SkillBaseNumber 把技能号归一到"基础技能"（对照 GameLogic/MasterSkillExtensions.cs:59-67
// 的 GetBaseSkill：只要当前技能是大师技且带 ReplacedSkill 就一直往下走）。
// 连击按基础技能号匹配（大师强化技与它替换的普通技算同一招）。
func (c *GameConfig) SkillBaseNumber(number int) int {
	current := number
	for {
		m, ok := c.MasterSkillByNumber(current)
		if !ok || m.ReplacedSkill == nil {
			return current
		}
		current = *m.ReplacedSkill
	}
}

// previousClass 找"下一转是当前职业"的那个职业（原版 FirstOrDefault(c => c.NextGenerationClass == X)）。
func (c *GameConfig) previousClass(classNumber int) (int, bool) {
	for i := range c.CharacterClasses {
		if n := c.CharacterClasses[i]; n.NextClass != nil && *n.NextClass == classNumber {
			return n.Number, true
		}
	}
	return 0, false
}

// validateCombos 校验连击定义：步骤技能存在、Order 连续（从 1 起、逐级 +1）、时限为正。
func (c *GameConfig) validateCombos() error {
	for i := range c.CharacterClasses {
		cls := &c.CharacterClasses[i]
		combo := cls.Combo
		if combo == nil {
			continue
		}
		if combo.MaximumCompletionMS <= 0 {
			return fmt.Errorf("职业 %d 的连击时限 %d ms 非法", cls.Number, combo.MaximumCompletionMS)
		}
		if len(combo.Steps) == 0 {
			return fmt.Errorf("职业 %d 的连击没有任何步骤", cls.Number)
		}
		orders := combo.StepOrders()
		for want, got := range orders {
			if got != want+1 {
				return fmt.Errorf("职业 %d 的连击步骤不连续：%v（应从 1 逐级 +1）", cls.Number, orders)
			}
		}
		for _, st := range combo.Steps {
			if _, ok := c.Skill(st.Skill); !ok {
				return fmt.Errorf("职业 %d 的连击步骤引用未知技能 %d", cls.Number, st.Skill)
			}
		}
		last := orders[len(orders)-1]
		finalFound := false
		for _, st := range combo.Steps {
			if st.IsFinal && st.Order != last {
				return fmt.Errorf("职业 %d 的连击步骤 %d 标了终结但不是最大顺序 %d", cls.Number, st.Order, last)
			}
			finalFound = finalFound || (st.IsFinal && st.Order == last)
		}
		if !finalFound {
			return fmt.Errorf("职业 %d 的连击在最大顺序 %d 上没有终结步", cls.Number, last)
		}
	}
	return nil
}
