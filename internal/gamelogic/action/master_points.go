package action

// master_points.go —— 大师加点判定（纯函数，对照 GameLogic/PlayerActions/Character/
// AddMasterPointAction.cs）。规则次序逐条对齐：
//
//	1 已选角色 → 2 有剩余点数 → 3 技能存在 → 4 是大师技
//	5 未学过 → CheckRequisitions（首点花费 → 职业合格 → 同根上一 rank 任一条≥10 → 前置全≥10）
//	6 已学过或直接进入 → 付出 requiredPoints 并加等级
//
// 原版所有失败分支**只写服务端日志，不给客户端任何东西**（:32/39/45/107/113/119/125/99），
// 故这里用返回值服务日志与测试，不发明错误码。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// minimumSkillLevelOfRequiredSkill 对应原版 MinimumSkillLevelOfRequiredSkill（:14）。
const minimumSkillLevelOfRequiredSkill = 10

// MasterPointOutcome 是加点结果码（仅用于日志/测试；客户端一律无感）。
type MasterPointOutcome int

const (
	// MasterPointLearned 是新学成（等级直接到 InitialPoints，即原版 MinimumLevel）。
	MasterPointLearned MasterPointOutcome = iota
	// MasterPointLeveled 是已学者 +1 级。
	MasterPointLeveled
	// MasterPointNoCharacter 对应"未选角色"。
	MasterPointNoCharacter
	// MasterPointNoPoints 对应"一点大师点数都没有"。
	MasterPointNoPoints
	// MasterPointUnknownSkill 对应"技能号不存在"。
	MasterPointUnknownSkill
	// MasterPointNotMasterSkill 对应"该技能没有大师定义"。
	MasterPointNotMasterSkill
	// MasterPointRejectedRequisition 对应 CheckRequisitions 不通过。
	MasterPointRejectedRequisition
	// MasterPointNotIncreased 对应"点数不够或已到上限"（原版这条也只记日志）。
	MasterPointNotIncreased
)

// String 给出日志可读名。
func (o MasterPointOutcome) String() string {
	switch o {
	case MasterPointLearned:
		return "learned"
	case MasterPointLeveled:
		return "leveled"
	case MasterPointNoCharacter:
		return "no-character"
	case MasterPointNoPoints:
		return "no-points"
	case MasterPointUnknownSkill:
		return "unknown-skill"
	case MasterPointNotMasterSkill:
		return "not-master-skill"
	case MasterPointRejectedRequisition:
		return "rejected-requisition"
	}
	return "not-increased"
}

// MasterPointResult 是一次加点的结果（视图层据此决定是否下发 F3 52 / 技能列表变更）。
type MasterPointResult struct {
	Outcome MasterPointOutcome
	// Master 是命中的大师技（技能号不存在时为 nil）。
	Master *config.MasterSkill
	// Level 是加点后该技能的等级（失败时为原等级）。
	Level byte
	// Points 是加点后剩余的大师点数。
	Points int
	// SkillNumber 是入参技能号（回显用）。
	SkillNumber uint16
}

// OK 报告本次是否真的改了角色态（只有这两种结果原版才会下发 F3 52）。
func (r MasterPointResult) OK() bool {
	return r.Outcome == MasterPointLearned || r.Outcome == MasterPointLeveled
}

// AddMasterPoint 按原版次序执行一次加点。角色态（点数 / LearnedSkills 等级）就地更新。
func AddMasterPoint(cfg *config.GameConfig, c *entity.Character, skillNumber uint16) MasterPointResult {
	if c == nil {
		return MasterPointResult{Outcome: MasterPointNoCharacter, SkillNumber: skillNumber}
	}
	res := MasterPointResult{SkillNumber: skillNumber, Points: c.MasterLevelUpPoints}
	if c.MasterLevelUpPoints < 1 {
		res.Outcome = MasterPointNoPoints
		return res
	}
	if cfg == nil {
		res.Outcome = MasterPointUnknownSkill
		return res
	}
	if _, ok := cfg.Skill(int(skillNumber)); !ok {
		res.Outcome = MasterPointUnknownSkill
		return res
	}
	master, ok := cfg.MasterSkillByNumber(int(skillNumber))
	if !ok {
		res.Outcome = MasterPointNotMasterSkill
		return res
	}
	res.Master = master
	entry, learned := findLearnedMasterSkill(c, master)
	res.Level = entry.Level
	if !learned {
		if !masterRequisitionsOK(cfg, c, master, skillNumber) {
			res.Outcome = MasterPointRejectedRequisition
			return res
		}
		// 原版 AddLearnedSkillAsync 先建 Level=0 的条目，再交给同一个加点点数逻辑。
		c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: skillNumber})
	}
	return addPointsToMasterSkill(c, master, res, !learned)
}

// addPointsToLearnedSkillAsync 的对应体：花费 = 未学过时 InitialPoints，否则 1。
func addPointsToMasterSkill(c *entity.Character, m *config.MasterSkill, res MasterPointResult, learning bool) MasterPointResult {
	required := byte(m.InitialPoints)
	if res.Level > 0 {
		required = 1
	}
	if c.MasterLevelUpPoints < int(required) || res.Level >= byte(m.MaximumLevel) {
		res.Outcome = MasterPointNotIncreased
		res.Points = c.MasterLevelUpPoints
		return res
	}
	level := res.Level + required
	setMasterSkillLevel(c, m, level)
	c.MasterLevelUpPoints -= int(required)
	res.Level = level
	res.Points = c.MasterLevelUpPoints
	if learning {
		res.Outcome = MasterPointLearned
	} else {
		res.Outcome = MasterPointLeveled
	}
	return res
}

// masterRequisitionsOK 对应 CheckRequisitions（:103-130），四条判定按原序。
func masterRequisitionsOK(cfg *config.GameConfig, c *entity.Character, m *config.MasterSkill, skillNumber uint16) bool {
	if c.MasterLevelUpPoints < m.InitialPoints {
		return false
	}
	skill, ok := cfg.Skill(int(skillNumber))
	if !ok || !skillQualifiedForClass(skill, c.ClassNumber) {
		return false
	}
	if !masterRankOK(cfg, c, m) {
		return false
	}
	return masterRequiredSkillsOK(cfg, c, m)
}

// masterRankOK 对应 CheckRank（:132-144）：rank≤1 直接通过；否则要求同一根里
// 上一 rank 的**任意一条**已学到大师技等级≥10。
func masterRankOK(cfg *config.GameConfig, c *entity.Character, m *config.MasterSkill) bool {
	if m.Rank <= 1 {
		return true
	}
	for _, e := range c.LearnedSkills {
		other, ok := cfg.MasterSkillByNumber(int(e.SkillNumber))
		if !ok || !sameMasterRoot(other, m) || other.Rank != m.Rank-1 {
			continue
		}
		if e.Level >= minimumSkillLevelOfRequiredSkill {
			return true
		}
	}
	return false
}

// masterRequiredSkillsOK 对应 CheckRequiredSkill（:146-157）：列出的前置每条要么
// 已学到大师技等级≥10，要么它本就不是大师技而角色以普通技能身份持有。
func masterRequiredSkillsOK(cfg *config.GameConfig, c *entity.Character, m *config.MasterSkill) bool {
	for _, number := range m.RequiredMasterSkills {
		if other, ok := cfg.MasterSkillByNumber(number); ok {
			if e, learned := findLearnedMasterSkill(c, other); learned && e.Level >= minimumSkillLevelOfRequiredSkill {
				continue
			}
			return false
		}
		if !hasLearnedSkillNumber(c, number) {
			return false
		}
	}
	return true
}

// hasLearnedSkillNumber 报告角色是否以普通技能身份持有该号（原版 SkillList.ContainsSkill）。
func hasLearnedSkillNumber(c *entity.Character, number int) bool {
	for _, e := range c.LearnedSkills {
		if int(e.SkillNumber) == number {
			return true
		}
	}
	return false
}

// skillQualifiedForClass 报告该技能的宿主定义接受这个职业（原版 Skill.QualifiedCharacters）。
func skillQualifiedForClass(sk *config.Skill, classNumber byte) bool {
	for _, n := range sk.QualifiedClasses {
		if n == int(classNumber) {
			return true
		}
	}
	return false
}

func sameMasterRoot(a, b *config.MasterSkill) bool {
	if a.Root == nil || b.Root == nil {
		return false
	}
	return *a.Root == *b.Root
}

// findLearnedMasterSkill 在已学列表里找该大师技的条目（原版的 FirstOrDefault 语义）。
func findLearnedMasterSkill(c *entity.Character, m *config.MasterSkill) (entity.LearnedSkill, bool) {
	for _, e := range c.LearnedSkills {
		if int(e.SkillNumber) == m.Number {
			return e, true
		}
	}
	return entity.LearnedSkill{}, false
}

// setMasterSkillLevel 写入等级；条目一定存在（新学路径在调用前已 append）。
func setMasterSkillLevel(c *entity.Character, m *config.MasterSkill, level byte) {
	for i := range c.LearnedSkills {
		if int(c.LearnedSkills[i].SkillNumber) == m.Number {
			c.LearnedSkills[i].Level = level
			return
		}
	}
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{
		SkillNumber: uint16(m.Number), Level: level,
	})
}
