package action

// combo.go —— 连击状态机（TRIM-06e），对照 GameLogic/ComboStateMachine.cs + ComboState.cs。
//
// 原版把每步建成状态对象并按 ReplacedSkill 链匹配（`skill.GetBaseSkill()`），
// 本仓用"已完成的顺序 + 上一手技能号"表达同一台机器，规则逐条对齐：
//   - 超时（默认 3s，来自导出件 maximum_completion_ms）后的这一手先把状态复位，再按起始态匹配；
//   - 起始态只接受 Order=1 的任意一招；之后必须正好是"下一顺序"的某招，
//     且**不能与上一手同招**（原版建态时的 `p.RequiredSkill != step.Skill`，ComboStateMachine.cs:131）；
//   - 匹配失败即复位并返回 false（这一手算错序，不做二次尝试）；
//   - 走到 IsFinalStep 那一步时返回 true（终结击），并立即回到起始态。
//
// 计时只在"从起始态推进"时刷新（原版 :79-82 的 `if CurrentState == InitialState`）。

import (
	"time"

	"mugo/internal/gamelogic/config"
)

// Combo 是一个角色身上的连击状态机。
type Combo struct {
	definition *config.SkillCombo
	order      int
	lastSkill  int
	startedAt  time.Time
}

// NewCombo 构造状态机；definition 为 nil 时返回 nil（该职业链上没有连击定义 = 原版 ComboState 为 null）。
func NewCombo(definition *config.SkillCombo) *Combo {
	if definition == nil {
		return nil
	}
	return &Combo{definition: definition}
}

// RegisterSkill 登记一次出手（skillNumber 需已按 GetBaseSkill 归一到基础技能号），
// 返回这一手是否为终结击。
func (c *Combo) RegisterSkill(skillNumber int, now time.Time) bool {
	if c == nil {
		return false
	}
	timeout := time.Duration(c.definition.MaximumCompletionMS) * time.Millisecond
	if c.order > 0 && !c.startedAt.IsZero() && now.Sub(c.startedAt) > timeout {
		c.reset()
	}

	next := 1
	if c.order > 0 {
		next = c.order + 1
		// 同一招不能连两下（原版把下一顺序里与自己同名的那些排除了）。
		if skillNumber == c.lastSkill {
			c.reset()
			return false
		}
	}
	isFinal, ok := c.definition.MatchesAt(next, skillNumber)
	if !ok {
		c.reset()
		return false
	}
	if c.order == 0 {
		c.startedAt = now
	}
	c.order = next
	c.lastSkill = skillNumber
	if isFinal {
		c.reset()
		return true
	}
	return false
}

// reset 回到起始态（原版 TryAdvanceToAsync(InitialState)）。
func (c *Combo) reset() {
	c.order = 0
	c.lastSkill = 0
	c.startedAt = time.Time{}
}

// Progress 返回当前已完成的顺序与上一手技能号（测试与调试用）。
func (c *Combo) Progress() (order, lastSkill int) {
	if c == nil {
		return 0, 0
	}
	return c.order, c.lastSkill
}
