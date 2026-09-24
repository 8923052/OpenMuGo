package config

// quests.go —— 任务定义数据面（95_quests.json），对应 OpenMU DataModel/Configuration/Quests/
// {QuestDefinition,QuestMonsterKillRequirement,QuestItemRequirement,QuestReward,QuestRewardType}.cs。
// 只承载数据；判定在 gamelogic/quests（PlayerActions/Quests/*），出站编码在 view/remote。
//
// 数据由 tools/goldenconfig 从原版初始化导出（Quests.cs：483 条 CreateQuest + 16 条 legacy = 499 条，
// 组 0/15/18/19），一律挂在给予者 NPC 的 MonsterDefinition.Quests 上（GameConfiguration 无 Quests 集合）。
// 同一 (group, number) 会按职业有多条定义（guid 含职业号），故取数必须带 NPC 与职业上下文。

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// 奖励种类（QuestRewardType 枚举名，逐字与导出件一致）。
const (
	QuestRewardExperience             = "Experience"
	QuestRewardMoney                  = "Money"
	QuestRewardItem                   = "Item"
	QuestRewardGensAttribution        = "GensAttribution"
	QuestRewardLevelUpPoints          = "LevelUpPoints"
	QuestRewardEvolutionFirstToSecond = "CharacterEvolutionFirstToSecond"
	QuestRewardEvolutionSecondToThird = "CharacterEvolutionSecondToThird"
	QuestRewardAttribute              = "Attribute"
	QuestRewardSkill                  = "Skill"
	QuestRewardUndefined              = "Undefined"
)

// Quest 是一条任务定义（QuestDefinition）。
type Quest struct {
	Group          int `json:"group"`
	Number         int `json:"number"`
	StartingNumber int `json:"starting_number"`
	// RefuseNumber 是玩家拒绝该任务时下发的步骤号（QuestProceedRequestHandlerPlugIn.cs:58）。
	RefuseNumber int    `json:"refuse_number"`
	Name         string `json:"name"`
	// NpcNumber 是给予者 NPC 号（原版 QuestGiver；查询与受理都以"当前对话 NPC"为前提）。
	NpcNumber int    `json:"npc_number"`
	NpcName   string `json:"npc_name"`
	// MinLevel/MaxLevel 对应 Minimum/MaximumCharacterLevel；MaxLevel==0 表示无上限。
	MinLevel int `json:"min_level"`
	MaxLevel int `json:"max_level"`
	// Repeatable 只影响"刚完成的这条能否再接"（原版仅比较 LastFinishedQuest）。
	Repeatable           bool `json:"repeatable"`
	RequiresClientAction bool `json:"requires_client_action"`
	RequiredStartMoney   int  `json:"required_start_money"`
	// QualifiedClass 为 nil 表示不限职业；非 nil 时按**当前职业**严格相等（含转职后不再合格）。
	QualifiedClass *int                   `json:"qualified_class"`
	RequiredKills  []QuestKillRequirement `json:"required_kills"`
	RequiredItems  []QuestItemRequirement `json:"required_items"`
	Rewards        []QuestReward          `json:"rewards"`
}

// IsQualified 报告该任务是否接受这个职业号（原版：QualifiedCharacter 为空或引用相等）。
func (q *Quest) IsQualified(classNumber byte) bool {
	return q.QualifiedClass == nil || *q.QualifiedClass == int(classNumber)
}

// LevelOK 报告等级是否在 [MinLevel, MaxLevel] 内（MaxLevel==0 视为无上限）。
func (q *Quest) LevelOK(level uint16) bool {
	return int(level) >= q.MinLevel && (q.MaxLevel == 0 || int(level) <= q.MaxLevel)
}

// QuestKillRequirement 对应 QuestMonsterKillRequirement。
type QuestKillRequirement struct {
	MonsterNumber int    `json:"monster"`
	MonsterName   string `json:"name"`
	Count         int    `json:"count"`
}

// QuestItemRequirement 对应 QuestItemRequirement。
type QuestItemRequirement struct {
	ItemGroup  int    `json:"group"`
	ItemNumber int    `json:"number"`
	ItemName   string `json:"name"`
	Count      int    `json:"count"`
	// ItemLevel 非 nil 时只接受该等级的物品（原版取 DropItemGroup.ItemLevel，
	// 见 QuestCompletionAction.cs:39；WithItemRequirement 里的 level/luck/skill 模板被原版丢弃）。
	ItemLevel *int `json:"item_level"`
}

// QuestReward 对应 QuestReward：type + value + 三种可选引用。
type QuestReward struct {
	Type      string             `json:"type"`
	Value     int                `json:"value"`
	Item      *QuestItemTemplate `json:"item"`
	Attribute *AttributeRef      `json:"attribute"`
	Skill     *QuestSkillRef     `json:"skill"`
}

// QuestItemTemplate 是奖励物品的实体模板（原版 QuestReward.ItemReward）。
// 原版交付时只造**一件**（不循环 Value，见 QuestCompletionAction.cs:119-131）。
type QuestItemTemplate struct {
	Group       int    `json:"group"`
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Level       int    `json:"level"`
	Durability  int    `json:"durability"`
	HasSkill    bool   `json:"has_skill"`
	Luck        bool   `json:"luck"`
	OptionLevel int    `json:"option_level"`
}

// QuestSkillRef 是奖励技能的引用（按号解析，name 供人读）。
type QuestSkillRef struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// QuestsForNpc 返回挂在该 NPC 号上的全部任务（按 Number 升序，对照 Npc.Definition.Quests）。
func (c *GameConfig) QuestsForNpc(npcNumber int) []*Quest {
	return c.questsByNpc[npcNumber]
}

// QuestByGroupNumber 返回某组内该号的任务（先匹配 Number 再匹配 StartingNumber），
// 不带 NPC/职业上下文——仅用于状态刷新等"已知是哪条"的场合；接/交判定请用 QuestAtNpc。
func (c *GameConfig) QuestByGroupNumber(group, number int) (*Quest, bool) {
	for _, q := range c.questsByGroup[group] {
		if q.Number == number || q.StartingNumber == number {
			return q, true
		}
	}
	return nil, false
}

// QuestAtNpc 复刻 PlayerQuestExtensions.GetQuest：在当前对话 NPC 的任务里按组筛，
// 只留职业合格者，按 Number 升序，取首个 StartingNumber 或 Number 命中者。
// 找不到即"该 NPC 面前没这条任务"（原版返回 null，调用方静默返回）。
func (c *GameConfig) QuestAtNpc(npcNumber, group, number int, classNumber byte) (*Quest, bool) {
	for _, q := range c.questsByNpc[npcNumber] { // 已按 Number 升序
		if q.Group != group || !q.IsQualified(classNumber) {
			continue
		}
		if q.StartingNumber == number || q.Number == number {
			return q, true
		}
	}
	return nil, false
}

// QuestsByGroup 返回某组全部任务（按 Number 升序，载入时已排序）。
func (c *GameConfig) QuestsByGroup(group int) []*Quest {
	return c.questsByGroup[group]
}

// QuestAt 按配置下标取定义（角色态以"原版持有引用"的下标形式记录，见 entity.QuestState）；越界为 nil。
func (c *GameConfig) QuestAt(i int) *Quest {
	if i < 0 || i >= len(c.Quests) {
		return nil
	}
	return &c.Quests[i]
}

// QuestIndexOf 返回定义在配置里的下标（-1 表示不属于本配置）。
func (c *GameConfig) QuestIndexOf(q *Quest) int {
	if q == nil {
		return -1
	}
	for i := range c.Quests {
		if &c.Quests[i] == q {
			return i
		}
	}
	return -1
}

// loadQuests 解析 95_quests.json；缺文件时容忍为空（一律无可接任务）。须在 buildIndex 后调用。
func (c *GameConfig) loadQuests(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "95_quests.json")
	if err != nil {
		return nil
	}
	var doc struct {
		Quests []Quest `json:"quests"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("config: 解析 95_quests.json: %w", err)
	}
	quests := doc.Quests
	c.Quests = quests
	c.questsByGroup = make(map[int][]*Quest)
	c.questsByNpc = make(map[int][]*Quest)
	for i := range c.Quests {
		q := &c.Quests[i]
		c.questsByGroup[q.Group] = append(c.questsByGroup[q.Group], q)
		c.questsByNpc[q.NpcNumber] = append(c.questsByNpc[q.NpcNumber], q)
	}
	for _, list := range c.questsByGroup {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Number < list[j].Number })
	}
	for _, list := range c.questsByNpc {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Number < list[j].Number })
	}
	return nil
}

// validateQuests 校验任务引用的物品/怪物/属性/技能/职业/给予者都能解析。
func (c *GameConfig) validateQuests() error {
	for i := range c.Quests {
		q := &c.Quests[i]
		if _, ok := c.Monster(q.NpcNumber); !ok {
			return fmt.Errorf("任务 %d/%d 的给予者 NPC %d 不存在", q.Group, q.Number, q.NpcNumber)
		}
		if q.QualifiedClass != nil {
			if _, ok := c.Class(*q.QualifiedClass); !ok {
				return fmt.Errorf("任务 %d/%d 引用未知职业 %d", q.Group, q.Number, *q.QualifiedClass)
			}
		}
		for _, r := range q.RequiredKills {
			if _, ok := c.Monster(r.MonsterNumber); !ok {
				return fmt.Errorf("任务 %d/%d 需击杀的怪物 %d 不存在", q.Group, q.Number, r.MonsterNumber)
			}
		}
		for _, r := range q.RequiredItems {
			if _, ok := c.Item(r.ItemGroup, r.ItemNumber); !ok {
				return fmt.Errorf("任务 %d/%d 需上交的物品 (%d,%d) 不存在", q.Group, q.Number, r.ItemGroup, r.ItemNumber)
			}
		}
		for j := range q.Rewards {
			if err := c.validateQuestReward(q, &q.Rewards[j]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *GameConfig) validateQuestReward(q *Quest, rw *QuestReward) error {
	switch rw.Type {
	case QuestRewardItem:
		if rw.Item == nil {
			return fmt.Errorf("任务 %d/%d 是物品奖励但没有物品模板", q.Group, q.Number)
		}
		if _, ok := c.Item(rw.Item.Group, rw.Item.Number); !ok {
			return fmt.Errorf("任务 %d/%d 奖励物品 (%d,%d) 不存在", q.Group, q.Number, rw.Item.Group, rw.Item.Number)
		}
	case QuestRewardAttribute:
		if rw.Attribute == nil {
			return fmt.Errorf("任务 %d/%d 是属性奖励但没有属性引用", q.Group, q.Number)
		}
		if _, ok := c.AttributeDefinitionByID(rw.Attribute.ID); !ok {
			return fmt.Errorf("任务 %d/%d 奖励属性 %s(%s) 不在属性表内", q.Group, q.Number, rw.Attribute.Designation, rw.Attribute.ID)
		}
	case QuestRewardSkill:
		if rw.Skill == nil {
			return fmt.Errorf("任务 %d/%d 是技能奖励但没有技能引用", q.Group, q.Number)
		}
		if _, ok := c.Skill(rw.Skill.Number); !ok {
			return fmt.Errorf("任务 %d/%d 奖励技能 %d 不存在", q.Group, q.Number, rw.Skill.Number)
		}
	case QuestRewardExperience, QuestRewardMoney, QuestRewardLevelUpPoints,
		QuestRewardEvolutionFirstToSecond, QuestRewardEvolutionSecondToThird:
		// 无引用，只需类型合法。
	default:
		return fmt.Errorf("任务 %d/%d 出现未知奖励类型 %q", q.Group, q.Number, rw.Type)
	}
	return nil
}
