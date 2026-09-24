package config

// master.go —— 大师技能树数据面（96_master_skills.json）。对应 OpenMU
// DataModel/Configuration/MasterSkillDefinition.cs + MasterSkillRoot.cs，以及
// GameServer/RemoteView/MasterSkillExtensions.cs 的"技能号 → 客户端树槽位"硬编码表。
//
// 判定在 gamelogic/action/master_points.go（AddMasterPointAction），出站编码在 view/remote。
// 数值（MathParser 公式的取值）由 tools/goldenconfig 在**导出期**用原版方法逐级求值，
// 本仓运行期查表 —— 口径与偏差登记见 doc/16 TRIM-09。

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// MasterRoot 是一个技能树根（左/中/右）。原版只有 Guid + 名字，且 GUID 每次导出都变，
// 故以导出顺序的 Index 作为仓内不透明标识；根唯一的结构性用途是"同根"分组
// （AddMasterPointAction.CheckRank），客户端槽位本身已含根信息（左 1 / 中 37 / 右 73）。
type MasterRoot struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

// MasterSkill 是一条大师技能（Skill.MasterDefinition + 宿主 Skill 的号）。
type MasterSkill struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Root   *int   `json:"root"`
	Rank   int    `json:"rank"`
	// MaximumLevel 是技能等级上限（原版多数 20，少数 10）。
	MaximumLevel int `json:"maximum_level"`
	// InitialPoints 是**第一次**学这条要付的点数（原版 MasterSkillDefinition.MinimumLevel
	// 复用为该语义，AddMasterPointAction.cs:77/105）；此后每点 +1 级。
	InitialPoints int `json:"initial_points"`
	// RequiredMasterSkills 是额外前置技能号（都要 ≥10 级，或以普通技能身份持有）。
	RequiredMasterSkills []int `json:"required_master_skills"`
	// ReplacedSkill 是被本技能替换掉的低级技能（大师技常是普通技的强化版）。
	ReplacedSkill *int `json:"replaced_skill"`
	// TargetAttribute/Aggregation 是被动加成落点（原版 TargetAttribute + Aggregation）。
	TargetAttribute string `json:"target_attribute"`
	Aggregation     string `json:"aggregation"`
	// ExtendsDuration 时，本技能等级会拉长被替换技能的 buff 时长。
	ExtendsDuration bool `json:"extends_duration"`
	// PassiveBoost 的技能不进 C1 F3 11 技能列表（SkillList.cs:224），只在大师树里显示。
	PassiveBoost bool `json:"passive_boost"`
	// 公式原文仅备查（运行期不解析）。
	ValueFormula        string    `json:"value_formula"`
	DisplayValueFormula string    `json:"display_value_formula"`
	ValueAtLevel        []float32 `json:"value_at_level"`
	DisplayAtLevel      []float32 `json:"display_at_level"`

	index int // 在 GameConfig.MasterSkills 里的下标
}

// ValueAt 取等级 level 的生效值（原版 GetValue：level<=0 或超过上限时 0）。
func (m *MasterSkill) ValueAt(level int) float32 {
	return pickLevel(m.ValueAtLevel, level)
}

// DisplayAt 取等级 level 的展示值（同一越界口径）。
func (m *MasterSkill) DisplayAt(level int) float32 {
	return pickLevel(m.DisplayAtLevel, level)
}

func pickLevel(values []float32, level int) float32 {
	if level <= 0 || level > len(values) {
		return 0
	}
	return values[level-1]
}

// NextDisplayValue 对应原版 CalculateNextDisplayValue：min(level+1, 上限) 的展示值。
func (m *MasterSkill) NextDisplayValue(level int) float32 {
	next := level + 1
	if next > m.MaximumLevel {
		next = m.MaximumLevel
	}
	return m.DisplayAt(next)
}

// MasterSkillAt 按下标取大师技能（角色态按下标记录时可用）；越界为 nil。
func (c *GameConfig) MasterSkillAt(i int) *MasterSkill {
	if i < 0 || i >= len(c.MasterSkills) {
		return nil
	}
	return &c.MasterSkills[i]
}

// MasterSkillIndexOf 返回定义在配置里的下标（-1 表示不属于本配置）。
func (c *GameConfig) MasterSkillIndexOf(m *MasterSkill) int {
	if m == nil {
		return -1
	}
	return m.index
}

// MasterSkillByNumber 按技能号取大师定义（非大师技能也返回 false）。
func (c *GameConfig) MasterSkillByNumber(number int) (*MasterSkill, bool) {
	m, ok := c.masterSkillByNum[number]
	return m, ok
}

// IsMasterSkill 报告该技能号是否大师技。
func (c *GameConfig) IsMasterSkill(number int) bool {
	_, ok := c.masterSkillByNum[number]
	return ok
}

// MasterSkillIndex 取该职业下该技能在客户端大师树里的槽位号
// （原版 GetMasterSkillIndex：非大师技或表里没有 → 0）。
func (c *GameConfig) MasterSkillIndex(classNumber, skillNumber int) byte {
	return c.masterIndex[classNumber][skillNumber]
}

// MasterRoots 返回技能树根（按导出顺序）。
func (c *GameConfig) MasterRoots() []MasterRoot { return c.MasterSkillTreeRoots }

// loadMaster 解析 96_master_skills.json；缺文件时容忍为空（该版本无大师树）。
func (c *GameConfig) loadMaster(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "96_master_skills.json")
	if err != nil {
		return nil
	}
	var doc struct {
		Roots     []MasterRoot           `json:"roots"`
		Skills    []MasterSkill          `json:"skills"`
		TreeIndex []masterTreeIndexEntry `json:"tree_indices"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("config: 解析 96_master_skills.json: %w", err)
	}
	c.MasterSkillTreeRoots = doc.Roots
	c.MasterSkills = doc.Skills
	c.masterSkillByNum = make(map[int]*MasterSkill, len(c.MasterSkills))
	for i := range c.MasterSkills {
		m := &c.MasterSkills[i]
		m.index = i
		c.masterSkillByNum[m.Number] = m
	}
	c.masterIndex = make(map[int]map[int]byte, len(doc.TreeIndex))
	for _, entry := range doc.TreeIndex {
		slots := make(map[int]byte, len(entry.Slots))
		for _, s := range entry.Slots {
			slots[s.Skill] = s.Index
		}
		c.masterIndex[entry.Class] = slots
	}
	return nil
}

type masterTreeIndexEntry struct {
	Class int `json:"class"`
	Slots []struct {
		Skill int  `json:"skill"`
		Index byte `json:"index"`
	} `json:"slots"`
}

// validateMaster 校验大师树自洽：与 meta 计数一致、号唯一、根可解析、前置/替换技能存在、
// 数值数组与上限等长、属性落点在属性表内、槽位表只指向大师技能且每个大师职业非空。
// （meta 计数的比对放在这里而不是 config.validate()：后者在域载入之前执行，那时本域还是空的。）
func (c *GameConfig) validateMaster() error {
	cnt := c.Meta.Counts
	switch {
	case len(c.MasterSkills) == 0 && cnt.MasterSkills != 0:
		return fmt.Errorf("meta 声明大师技能 %d 条但 96_master_skills.json 未载入", cnt.MasterSkills)
	case cnt.MasterSkills != 0 && cnt.MasterSkills != len(c.MasterSkills):
		return fmt.Errorf("大师技能数不符 meta=%d 实际=%d", cnt.MasterSkills, len(c.MasterSkills))
	case cnt.MasterRoots != 0 && cnt.MasterRoots != len(c.MasterSkillTreeRoots):
		return fmt.Errorf("大师树根数不符 meta=%d 实际=%d", cnt.MasterRoots, len(c.MasterSkillTreeRoots))
	case cnt.MasterClasses != 0 && cnt.MasterClasses != len(c.masterIndex):
		return fmt.Errorf("大师槽位表职业数不符 meta=%d 实际=%d", cnt.MasterClasses, len(c.masterIndex))
	}
	seen := make(map[int]bool, len(c.MasterSkills))
	for i := range c.MasterSkills {
		m := &c.MasterSkills[i]
		if seen[m.Number] {
			return fmt.Errorf("大师技能号 %d 重复", m.Number)
		}
		seen[m.Number] = true
		if m.Root == nil {
			return fmt.Errorf("大师技能 %d(%s) 没有根", m.Number, m.Name)
		}
		if *m.Root < 0 || *m.Root >= len(c.MasterSkillTreeRoots) {
			return fmt.Errorf("大师技能 %d(%s) 的根下标 %d 越界（共 %d 根）",
				m.Number, m.Name, *m.Root, len(c.MasterSkillTreeRoots))
		}
		if m.MaximumLevel <= 0 {
			return fmt.Errorf("大师技能 %d(%s) 的等级上限 %d 非法", m.Number, m.Name, m.MaximumLevel)
		}
		if len(m.ValueAtLevel) != m.MaximumLevel || len(m.DisplayAtLevel) != m.MaximumLevel {
			return fmt.Errorf("大师技能 %d(%s) 的数值数组长度（value=%d display=%d）与上限 %d 不符",
				m.Number, m.Name, len(m.ValueAtLevel), len(m.DisplayAtLevel), m.MaximumLevel)
		}
		if m.InitialPoints < 1 || m.InitialPoints > m.MaximumLevel {
			return fmt.Errorf("大师技能 %d(%s) 的首点花费 %d 不在 1..%d",
				m.Number, m.Name, m.InitialPoints, m.MaximumLevel)
		}
		for _, req := range m.RequiredMasterSkills {
			if _, ok := c.masterSkillByNum[req]; !ok {
				// 原版 CheckRequiredSkill 允许前置是普通技能（持有即可），故只要求技能存在。
				if _, isSkill := c.Skill(req); !isSkill {
					return fmt.Errorf("大师技能 %d(%s) 的前置 %d 既不是大师技也不是已知技能", m.Number, m.Name, req)
				}
			}
		}
		if m.ReplacedSkill != nil {
			if _, ok := c.Skill(*m.ReplacedSkill); !ok {
				return fmt.Errorf("大师技能 %d(%s) 替换的技能 %d 不存在", m.Number, m.Name, *m.ReplacedSkill)
			}
		}
		if m.TargetAttribute != "" {
			if _, ok := c.AttributeByName(m.TargetAttribute); !ok {
				return fmt.Errorf("大师技能 %d(%s) 的目标属性 %q 不在属性表内", m.Number, m.Name, m.TargetAttribute)
			}
		}
	}
	for classNumber, slots := range c.masterIndex {
		if _, ok := c.Class(classNumber); !ok {
			return fmt.Errorf("大师槽位表引用未知职业 %d", classNumber)
		}
		if len(slots) == 0 {
			return fmt.Errorf("职业 %d 的大师槽位表为空", classNumber)
		}
		for skillNumber := range slots {
			if _, ok := c.masterSkillByNum[skillNumber]; !ok {
				return fmt.Errorf("职业 %d 的槽位表引用非大师技能 %d", classNumber, skillNumber)
			}
		}
	}
	return nil
}
