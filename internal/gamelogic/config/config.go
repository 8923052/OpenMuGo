// Package config 是游戏配置层，对应 OpenMU 的 DataModel/Configuration（GameConfiguration）。
// 将来承载从 OpenMU 导出的游戏数值（物品、技能、职业、怪物、地图、掉落、经验表）与查询接口。
//
// 客户端版本口径不在这里：见 internal/version（原版分散在 Network/PlugIns 与 DataModel 两处，
// 因不移植反射插件框架而合并）。
//
// T0-c（doc/10 防线 1 数据对齐）：本包提供 data/<版本>/*.json（由 tools/goldenconfig 从
// OpenMU 初始化工程导出）的**载入与校验**。导出即原版初始化的最终形态（含 Updates 净效果），
// 数值一致性由"导出件 = 原版初始化结果"保证，Go 侧不重写任何数值。
//
// 分层约束（layoutcheck）：本包不得 import version/proto/server/view/transport——
// 配置数据按 GameClientDefinition 的 key（如 "season6"）选择，而不是引用版本类型。
package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"sync"

	"mugo/data"
	"mugo/internal/attribute"
)

// Meta 是导出件的元信息（00_meta.json）。
type Meta struct {
	SchemaVersion             int    `json:"schema_version"`
	DataInitializationID      string `json:"data_initialization_id"`
	DataInitializationCaption string `json:"data_initialization_caption"`
	ExportedAtUTC             string `json:"exported_at_utc"`
	Note                      string `json:"note"`
	Counts                    struct {
		CharacterClasses int `json:"character_classes"`
		Skills           int `json:"skills"`
		Items            int `json:"items"`
		Monsters         int `json:"monsters"`
		Maps             int `json:"maps"`
		MonsterSpawns    int `json:"monster_spawns"`
		Warps            int `json:"warps"`
		MasterSkills     int `json:"master_skills"`
		MasterRoots      int `json:"master_skill_roots"`
		MasterClasses    int `json:"master_classes"`
		ChatCommands     int `json:"chat_commands"`
	} `json:"counts"`
}

// AttributeVal 是"命名属性 → 数值"（怪物/职业的属性集合；原版 MonsterAttribute/ConstValueAttribute）。
// AggregateType 对应原版 CreateConstValueAttribute 的聚合形态（缺省 AddRaw；
// 同名属性可有多条不同形态的条目，如护盾恢复倍率 100·AddRaw 与 1/75000·Multiplicate）。
type AttributeVal struct {
	Designation   string  `json:"designation"`
	Value         float64 `json:"value"`
	AggregateType string  `json:"aggregate_type,omitempty"`
}

// StatAttribute 是职业的可加点属性（原版 StatAttributeDefinition）。
type StatAttribute struct {
	Designation         string  `json:"designation"`
	BaseValue           float64 `json:"base_value"`
	IncreasableByPlayer bool    `json:"increasable_by_player"`
}

// CharacterClass 对应原版 CharacterClass。
type CharacterClass struct {
	Number              int    `json:"number"`
	Name                string `json:"name"`
	CanGetCreated       bool   `json:"can_get_created"`
	CreationAllowedFlag int    `json:"creation_allowed_flag"`
	// LevelRequirementByCreation 是建该职业的最低等级（原版 LevelRequirementByCreation，大师职业门槛）。
	LevelRequirementByCreation int `json:"level_requirement_by_creation"`
	// LevelWarpRequirementReductionPercent 降低转职等级要求百分比；FruitCalculation 属性果实策略枚举。
	LevelWarpRequirementReductionPercent int  `json:"level_warp_requirement_reduction_percent"`
	FruitCalculation                     int  `json:"fruit_calculation"`
	IsMasterClass                        bool `json:"is_master_class"`
	NextClass                            *int `json:"next_class"`
	// Combo 是本职业**显式持有**的连击定义（原版只有 Blade Knight 有）；
	// 后续转职通过 ComboForClass 沿继承链取得，不在数据里固化。
	Combo                 *SkillCombo             `json:"combo_definition"`
	HomeMap               *int                    `json:"home_map"`
	BaseAttributes        []AttributeVal          `json:"base_attributes"`
	StatAttributes        []StatAttribute         `json:"stat_attributes"`
	AttributeCombinations []AttributeRelationship `json:"attribute_combinations"`
}

// EffectiveMoveLevelRequirement 按本职业的折减百分比算进图/传送的真实等级门槛
// （对照 CharacterExtensions.GetEffectiveMoveLevelRequirement:113-126：整数除法
// `levelRequirement × (100 − percent) ÷ 100`）。
//
// 原版有一条**硬特例**：门槛正好 400 时不折减（S6 数据里就是 Atlans 的 Peace Swamp，
// Gates.cs 的 Lv400 —— 特殊职业也不降）。S6 里带 34% 折减的是 MG/DL/RF 及其转职
// （Initialization 用 Math.Ceiling(100/3)=34；更新插件把历史的 33 修成 34）。
func (c *CharacterClass) EffectiveMoveLevelRequirement(levelRequirement int) int {
	if levelRequirement == 400 {
		return levelRequirement
	}
	if c == nil || c.LevelWarpRequirementReductionPercent <= 0 {
		return levelRequirement
	}
	return levelRequirement * (100 - c.LevelWarpRequirementReductionPercent) / 100
}

// AttributeRef 是关系里对属性定义的引用（id 精确解析；designation 供人读/日志）。
type AttributeRef struct {
	ID          string `json:"id"`
	Designation string `json:"designation"`
}

// AttributeRelationship 是一条派生关系（原版 AttributeRelationship）：
// target ⊗= (Σ input) ⊗ operand，运算与聚合形态见两个字符串枚举。
// 属性引用带 id —— 真实数据存在少量重复 designation（"Temp ..." 变体），按名解析不可靠。
type AttributeRelationship struct {
	Target        *AttributeRef `json:"target"`
	Input         *AttributeRef `json:"input"`
	Operand       *AttributeRef `json:"operand"`
	InputOperand  float32       `json:"input_operand"`
	InputOperator string        `json:"input_operator"`
	AggregateType string        `json:"aggregate_type"`
}

// AttributeDefinitionExport 是导出的属性定义（原版 AttributeSystem.AttributeDefinition）。
type AttributeDefinitionExport struct {
	ID           string   `json:"id"`
	Designation  string   `json:"designation"`
	Description  string   `json:"description"`
	MaximumValue *float32 `json:"maximum_value"`
}

// LevelBonus 是物品等级加成表的一行。
type LevelBonus struct {
	Level           int     `json:"level"`
	AdditionalValue float32 `json:"additional_value"`
}

// ItemLevelBonusTableExport 对应原版 ItemLevelBonusTable。
type ItemLevelBonusTableExport struct {
	Name          string       `json:"name"`
	BonusPerLevel []LevelBonus `json:"bonus_per_level"`
}

// Experience 对应原版 GameConfiguration 的经验公式 + GameContext.CreateExpTable 的预求值表。
type Experience struct {
	ExperienceFormula       string  `json:"experience_formula"`
	MaximumLevel            int     `json:"maximum_level"`
	MasterExperienceFormula string  `json:"master_experience_formula"`
	MaximumMasterLevel      int     `json:"maximum_master_level"`
	Table                   []int64 `json:"table"`
	MasterTable             []int64 `json:"master_table"`
}

// GlobalConfig 对应原版 GameConfiguration 的全局标量（玩法常量），由 05_game_config.json 载入。
// 之前 Go 侧多处硬编码，现在统一从数据源取。ItemDropDurationMs 为地面掉落物存活时长。
type GlobalConfig struct {
	MaximumLevel                           int     `json:"maximum_level"`
	MaximumMasterLevel                     int     `json:"maximum_master_level"`
	ExperienceRate                         float64 `json:"experience_rate"`
	MasterExperienceRate                   float64 `json:"master_experience_rate"`
	PreventExperienceOverflow              bool    `json:"prevent_experience_overflow"`
	MinimumMonsterLevelForMasterExperience int     `json:"minimum_monster_level_for_master_experience"`
	InfoRange                              int     `json:"info_range"`
	AreaSkillHitsPlayer                    bool    `json:"area_skill_hits_player"`
	MaximumInventoryMoney                  int     `json:"maximum_inventory_money"`
	MaximumVaultMoney                      int     `json:"maximum_vault_money"`
	ClampMoneyOnPickup                     bool    `json:"clamp_money_on_pickup"`
	RecoveryInterval                       int     `json:"recovery_interval"`
	MaximumLetters                         int     `json:"maximum_letters"`
	LetterSendPrice                        int     `json:"letter_send_price"`
	MaximumCharactersPerAccount            int     `json:"maximum_characters_per_account"`
	CharacterNameRegex                     string  `json:"character_name_regex"`
	MaximumPasswordLength                  int     `json:"maximum_password_length"`
	MaximumPartySize                       int     `json:"maximum_party_size"`
	ShouldDropMoney                        bool    `json:"should_drop_money"`
	ItemDropDurationMs                     int64   `json:"item_drop_duration_ms"`
	DamagePerOneItemDurability             float64 `json:"damage_per_one_item_durability"`
	DamagePerOnePetDurability              float64 `json:"damage_per_one_pet_durability"`
	HitsPerOneItemDurability               float64 `json:"hits_per_one_item_durability"`
}

// Skill 对应原版 Skill（本载入阶段仅取内建标量字段；属性关系后续按需扩展）。
type Skill struct {
	Number              int    `json:"number"`
	Name                string `json:"name"`
	AttackDamage        int    `json:"attack_damage"`
	Range               int    `json:"range"`
	ImplicitTargetRange int    `json:"implicit_target_range"`
	HitsPerAttack       int    `json:"hits_per_attack"`
	MovesToTarget       bool   `json:"moves_to_target"`
	MovesTarget         bool   `json:"moves_target"`
	QualifiedClasses    []int  `json:"qualified_classes"`
	// T2-11 扩展（原版 SkillType/SkillTarget/DamageType 枚举值）：
	SkillType  int `json:"skill_type"`
	Target     int `json:"target"`
	DamageType int `json:"damage_type"`
	// Consume 为施放消耗（原版 ConsumeRequirements：Current Mana/Current Ability…）。
	Consume []SkillConsume `json:"consume"`
	// MagicEffectNumber 为挂接的 MagicEffect 号（buff/异常状态；null=无）。
	MagicEffectNumber *int `json:"magic_effect_number"`
	// TargetRestriction 为原版 SkillTargetRestriction：0 无限制、1 仅自身、2 自身或队友、3 仅玩家。
	TargetRestriction int `json:"target_restriction"`
	// Requirements 为施放前置要求（原版 Skill.Requirements，如最低等级）。
	Requirements []ItemRequirement `json:"requirements"`
	// AttributeRelationships 为技能伤害按属性缩放的派生关系（原版 Skill.AttributeRelationships）。
	AttributeRelationships []AttributeRelationship `json:"attribute_relationships"`
	// ElementalModifierTarget 为元素克制目标属性 designation（命中可附加该元素效果）；空=无。
	ElementalModifierTarget string `json:"elemental_modifier_target"`
	// SkipElementalModifier=true 时忽略元素抗性，技能走自带逻辑（如 Pollution/Explosion）。
	SkipElementalModifier bool `json:"skip_elemental_modifier"`
	// Area 为区域技能形状设置（原版 Skill.AreaSkillSettings）：扇形(frustum)/
	// 小圆(target-area)/作用半径(effect-range)/多段命中/命中衰减等。
	// 优先从导出件 area_skill_settings 载入（goldenconfig 已应用 AreaSkillSettings 更新插件时）；
	// 旧导出件缺此域时由 areaSkillSettingsTable 兜底（见 area_skill.go）。
	Area *AreaSkillSettings `json:"area_skill_settings,omitempty"`
}

// SkillConsume 是技能施放消耗项（原版 AttributeRequirement）。
type SkillConsume struct {
	Attribute string `json:"attribute"`
	Value     int    `json:"value"`
}

// ItemRequirement 是物品的装备需求（对照原版 AttributeRequirement）。
type ItemRequirement struct {
	Attribute string `json:"attribute"` // 属性 designation（如 "Total Strength Requirement Value"、"Level"）
	Value     int    `json:"value"`     // 原版 MinimumValue
}

// Item 对应原版 ItemDefinition（内建标量字段）。
type Item struct {
	Group              int    `json:"group"`
	Number             int    `json:"number"`
	Name               string `json:"name"`
	Slot               *int   `json:"slot"`
	Slots              []int  `json:"slots"` // 完整槽位集合（原版 ItemSlots）；Slot 只是 FirstOrDefault，有损
	SlotDescription    string `json:"slot_description"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	DropLevel          int    `json:"drop_level"`
	MaximumDropLevel   *int   `json:"maximum_drop_level"`
	MaximumItemLevel   int    `json:"maximum_item_level"`
	Durability         int    `json:"durability"`
	Value              int    `json:"value"`
	MaximumSockets     int    `json:"maximum_sockets"`
	DropsFromMonsters  bool   `json:"drops_from_monsters"`
	IsAmmunition       bool   `json:"is_ammunition"`
	IsBoundToCharacter bool   `json:"is_bound_to_character"`
	IsQuestItem        bool   `json:"is_quest_item"`
	// StorageLimitPerCharacter 是该类扩展存储箱（混沌/仓库等）每角色的格子上限（0=不适用）。
	StorageLimitPerCharacter int  `json:"storage_limit_per_character"`
	HasSkill                 bool `json:"has_skill"`
	// SkillNumber 是物品携带的技能号（技能书/卷轴 group 15；对照 OpenMU
	// ItemDefinition.Skill 导航属性，LearnablesConsumeHandlerPlugIn 据此学得技能）。
	// nil = 非技能书（或导出件未带此映射）。
	SkillNumber      *int              `json:"skill_number"`
	Requirements     []ItemRequirement `json:"requirements"`
	QualifiedClasses []int             `json:"qualified_classes"`
	// BasePowerUpAttributes 是装备穿上后的基础加成（对照 ItemBasePowerUpDefinition）。
	BasePowerUpAttributes []ItemBasePowerUp `json:"base_power_up_attributes"`
	// ConsumeEffectNumber 是使用该物品时产生的魔法效果编号（酒=201 攻速；
	// 对照 ItemDefinition.ConsumeEffect）。nil = 普通恢复/无效果。
	ConsumeEffectNumber *int `json:"consume_effect"`
}

// ItemBasePowerUp 是装备对某属性的基础加成（目标属性 + 基础值 + 聚合形态
// + 每级加成表名，关联 ItemLevelBonusTable）。
type ItemBasePowerUp struct {
	Target        string  `json:"target"`
	BaseValue     float64 `json:"base_value"`
	AggregateType string  `json:"aggregate_type"`
	BonusTable    string  `json:"bonus_table"`
}

// IsWearable 对照原版 ItemExtensions.IsWearable：有 ItemSlot 即可穿戴。
func (i *Item) IsWearable() bool { return len(i.Slots) > 0 }

// WearsSlot 对照原版 `ItemSlot.ItemSlots.Contains(toSlot)`。
func (i *Item) WearsSlot(slot int) bool {
	for _, s := range i.Slots {
		if s == slot {
			return true
		}
	}
	return false
}

// ItemRef 是掉落组对物品定义的 (group, number) 引用。
type ItemRef struct {
	Group  int `json:"group"`
	Number int `json:"number"`
}

// DropGroup 对应原版 DropItemGroup / ItemDropItemGroup（掉落组的公共字段）。
type DropGroup struct {
	Chance              float64   `json:"chance"`
	ItemType            string    `json:"item_type"`
	MoneyAmount         int       `json:"money_amount"`
	MinimumLevel        int       `json:"min_level"`
	MaximumLevel        int       `json:"max_level"`
	ItemLevel           *int      `json:"item_level"`
	MinimumMonsterLevel *int      `json:"min_monster_level"`
	MaximumMonsterLevel *int      `json:"max_monster_level"`
	PossibleItems       []ItemRef `json:"possible_items"`
}

// MonsterDropEntry 是"某怪物的掉落组清单"。
type MonsterDropEntry struct {
	Monster int         `json:"monster"`
	Groups  []DropGroup `json:"groups"`
}

// MapDropEntry 是"某地图的掉落组清单"（Season6 的主掉落挂在地图上）。
type MapDropEntry struct {
	Map           int         `json:"map"`
	Discriminator int         `json:"discriminator"`
	Groups        []DropGroup `json:"groups"`
}

// DropGroupRef 是带来源标记的掉落组。原版以 monster.DropItemGroups.Contains(group)
// 判"怪物专属"（这类组不做怪级门控），Go 侧来源由导出结构决定。
type DropGroupRef struct {
	Group       DropGroup
	FromMonster bool
}

// DropGroupsFor 按原版分区顺序返回掉落组：monster → map。原版的 character / quest
// 两级在 S6 初始化数据里恒为空（证据登记在 doc/16 TRIM-04）。
func (c *GameConfig) DropGroupsFor(mapNumber uint16, monsterNumber int) []DropGroupRef {
	var monsterSide, mapSide []DropGroupRef
	for i := range c.MonsterDrops {
		if c.MonsterDrops[i].Monster != monsterNumber {
			continue
		}
		for j := range c.MonsterDrops[i].Groups {
			monsterSide = append(monsterSide, DropGroupRef{
				Group: c.MonsterDrops[i].Groups[j], FromMonster: true})
		}
	}
	for i := range c.MapDrops {
		if c.MapDrops[i].Map != int(mapNumber) || c.MapDrops[i].Discriminator != 0 {
			continue
		}
		for j := range c.MapDrops[i].Groups {
			mapSide = append(mapSide, DropGroupRef{Group: c.MapDrops[i].Groups[j]})
		}
	}
	return append(monsterSide, mapSide...)
}

// Monster 对应原版 MonsterDefinition（含属性集合）。
type Monster struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	ObjectKind string `json:"object_kind"`
	NpcWindow  int    `json:"npc_window"`
	// Element 是怪物元素属性（原版 MonsterDefinition.Attribute，byte）：决定被哪种
	// 技能的 ElementalModifierTarget 克制/免疫。字段名避开已有方法 Attribute(designation)。
	Element                  int            `json:"attribute"`
	MoveRange                int            `json:"move_range"`
	AttackRange              int            `json:"attack_range"`
	ViewRange                int            `json:"view_range"`
	MoveDelayMs              int64          `json:"move_delay_ms"`
	AttackDelayMs            int64          `json:"attack_delay_ms"`
	RespawnDelayMs           int64          `json:"respawn_delay_ms"`
	NumberOfMaximumItemDrops int            `json:"number_of_maximum_item_drops"`
	Intelligence             *string        `json:"intelligence"`
	AttackSkill              *int           `json:"attack_skill"`
	Attributes               []AttributeVal `json:"attributes"`
}

// Attribute 归一化读取（缺失返回 0）。
func (m *Monster) Attribute(designation string) float64 {
	for _, a := range m.Attributes {
		if a.Designation == designation {
			return a.Value
		}
	}
	return 0
}

// Spawn 对应原版 MonsterSpawnArea。
type Spawn struct {
	Monster   *int   `json:"monster"`
	X1        int    `json:"x1"`
	Y1        int    `json:"y1"`
	X2        int    `json:"x2"`
	Y2        int    `json:"y2"`
	Quantity  int    `json:"quantity"`
	Direction int    `json:"direction"`
	Trigger   string `json:"trigger"`
	Wave      int    `json:"wave"`
}

// GateTarget 是出站门的目标点（换图落点：目标地图 + 坐标 + 朝向）。
type GateTarget struct {
	Map         *int `json:"map"`
	X1          int  `json:"x1"`
	Y1          int  `json:"y1"`
	X2          int  `json:"x2"`
	Y2          int  `json:"y2"`
	Direction   int  `json:"direction"`
	IsSpawnGate bool `json:"is_spawn_gate"`
}

// EnterGate 对应原版 EnterGate（含目标门内联坐标）。
type EnterGate struct {
	Number           int         `json:"number"`
	X1               int         `json:"x1"`
	Y1               int         `json:"y1"`
	X2               int         `json:"x2"`
	Y2               int         `json:"y2"`
	LevelRequirement int         `json:"level_requirement"`
	Target           *GateTarget `json:"target"`
}

// ExitGate 对应原版 ExitGate。
type ExitGate struct {
	X1          int  `json:"x1"`
	Y1          int  `json:"y1"`
	X2          int  `json:"x2"`
	Y2          int  `json:"y2"`
	Direction   int  `json:"direction"`
	IsSpawnGate bool `json:"is_spawn_gate"`
}

// GameMap 对应原版 GameMapDefinition。
//
// 同一地图编号可能有多个**变体**（原版 Discriminator：如 Devil Square 的状态切换），
// Discriminator==0 为主变体；Map(number) 返回主变体，变体切换由游戏逻辑负责（原版同）。
type GameMap struct {
	Number        int    `json:"number"`
	Discriminator int    `json:"discriminator"`
	Name          string `json:"name"`
	// ExpMultiplier 是该地图经验倍率（原版 GameMapDefinition.ExpMultiplier）。
	ExpMultiplier float64 `json:"exp_multiplier"`
	// Requirements 是进图条件（原版 MapRequirements）；CharacterPowerUps 是地图内全局光环。
	Requirements      []ItemRequirement `json:"requirements"`
	CharacterPowerUps []PowerUpDef      `json:"character_power_ups"`
	SafezoneMap       *int              `json:"safezone_map"`
	Terrain           *string           `json:"terrain"` // 原始 .att 内容（base64），解析器 T1-d 移植
	Spawns            []Spawn           `json:"spawns"`
	EnterGates        []EnterGate       `json:"enter_gates"`
	ExitGates         []ExitGate        `json:"exit_gates"`
}

// EnterGateAt 返回覆盖 (x,y) 的进门定义（无则 nil）。
func (m *GameMap) EnterGateAt(x, y byte) *EnterGate {
	for i := range m.EnterGates {
		g := &m.EnterGates[i]
		if int(x) >= g.X1 && int(x) <= g.X2 && int(y) >= g.Y1 && int(y) <= g.Y2 {
			return g
		}
	}
	return nil
}

// EnterGateByNumber 按门编号取进门（对照原版 WarpGateHandlerPlugIn:53 的
// EnterGates.FirstOrDefault(g => g.Number == gateNumber)；无则 nil）。
func (m *GameMap) EnterGateByNumber(number int) *EnterGate {
	for i := range m.EnterGates {
		if m.EnterGates[i].Number == number {
			return &m.EnterGates[i]
		}
	}
	return nil
}

// TerrainBytes 解码地形数据（.att 原始字节；无地形返回 nil）。
func (m *GameMap) TerrainBytes() ([]byte, error) {
	if m.Terrain == nil {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(*m.Terrain)
}

// Warp 对应原版 WarpInfo（传送命令清单）。
type Warp struct {
	Index            int         `json:"index"`
	Name             string      `json:"name"`
	Costs            int         `json:"costs"`
	LevelRequirement int         `json:"level_requirement"`
	Gate             *GateTarget `json:"gate"`
}

// MerchantStoreItem 是 NPC 商店清单里的一件商品（原版 MerchantStore.Items 的实体面：
// ItemSlot + Level + Durability + HasSkill + 选项；定义面按 (group, number) 查 Items）。
type MerchantStoreItem struct {
	Slot        byte `json:"slot"`
	Group       byte `json:"group"`
	Number      byte `json:"number"`
	Level       byte `json:"level"`
	Durability  byte `json:"durability"`
	HasSkill    bool `json:"has_skill"`
	Luck        bool `json:"luck"`
	OptionLevel byte `json:"option_level"`
}

// MerchantStoreEntry 是"某 NPC 的商店"（85_merchant_stores.json 的一行）。
type MerchantStoreEntry struct {
	NpcNumber int                 `json:"npc"`
	Name      string              `json:"name"`
	Items     []MerchantStoreItem `json:"items"`
}

// GameConfig 是一个数据版本的完整配置。
type GameConfig struct {
	Meta Meta
	// Globals 是 GameConfiguration 的全局标量（玩法常量，05_game_config.json）。
	Globals              GlobalConfig
	CharacterClasses     []CharacterClass
	Experience           Experience
	Skills               []Skill
	Items                []Item
	Monsters             []Monster
	Maps                 []GameMap
	Warps                []Warp
	Attributes           []AttributeDefinitionExport
	GlobalCombinations   []AttributeRelationship
	ItemLevelBonusTables []ItemLevelBonusTableExport
	MonsterDrops         []MonsterDropEntry
	MapDrops             []MapDropEntry
	MerchantStores       []MerchantStoreEntry
	Craftings            []Crafting
	Quests               []Quest
	// 大师技能树数据面（TRIM-09，96_master_skills.json）：172 条大师技能 + 3 根 +
	// (职业, 技能号) → 客户端树槽位索引表。判定在 action，编码在 view/remote。
	MasterSkills         []MasterSkill
	MasterSkillTreeRoots []MasterRoot
	// 聊天命令表（TRIM-08，97_chat_commands.json）：原版反射式插件容器的元数据在导出期求值。
	ChatCommands []ChatCommand
	// 宝石升档/降档映射（S-3 后续，98_jewel_mixes.json）：C1 BC 的 ItemType 解释表。
	JewelMixes   []JewelMix
	MagicEffects []MagicEffectDefinition
	// 物品选项数据面（T2-12，42_item_options.json）：选项类型 + 定义 + 物品引用 +
	// 组合奖励 + 套装组。载入即全量，消费端按需取用（见 item_options.go）。
	ItemOptionTypes       []ItemOptionTypeExport
	ItemOptionDefinitions []ItemOptionDefinitionExport
	ItemOptionEntries     []ItemOptionEntryExport
	CombinationBonuses    []ItemOptionCombinationBonusExport
	ItemSetGroups         []ItemSetGroupExport
	// MaximumItemOptionLevelDrop 是掉落时随机选项的最高等级（原版
	// GameConfiguration.MaximumItemOptionLevelDrop，DefaultDropGenerator 用）。
	MaximumItemOptionLevelDrop int
	// ExcellentItemDropLevelDelta 是"卓越物品按怪级下探的等级差"（原版
	// GameConfiguration.ExcellentItemDropLevelDelta）。
	ExcellentItemDropLevelDelta int

	mu            sync.RWMutex
	mapByNum      map[int]*GameMap // 主变体（Discriminator==0）；变体按 (编号, discriminator) 区分
	monByID       map[int]*Monster
	itemByKey     map[itemKey]*Item
	itemByName    map[string]*Item
	classByID     map[int]*CharacterClass
	attrByID      map[string]*AttributeDefinitionExport
	storeByNpcID  map[int]*MerchantStoreEntry
	skillByNum    map[int]*Skill
	optTypeByID   map[string]*ItemOptionTypeExport
	optTypeByKind map[string]*ItemOptionTypeExport
	optDefByID    map[string]*ItemOptionDefinitionExport
	optEntryByKey map[itemKey][]string
	optSetGroups  map[itemKey][]*ItemSetGroupExport
	craftingByNum map[int]*Crafting
	questsByGroup map[int][]*Quest
	questsByNpc   map[int][]*Quest
	// 大师树：按技能号索引 + 按职业的技能号→槽位索引两层表。
	masterSkillByNum map[int]*MasterSkill
	masterIndex      map[int]map[int]byte
	// 聊天命令：按命令串（含 '/'）索引。
	chatCommandByKey map[string]*ChatCommand
}

type itemKey struct{ group, number int }

// Map 返回编号对应的**主变体**地图（Discriminator==0；变体由游戏逻辑切换，原版同）。
func (c *GameConfig) Map(number int) (*GameMap, bool) {
	m, ok := c.mapByNum[number]
	return m, ok
}

// Monster 返回编号对应的怪物/NPC。
func (c *GameConfig) Monster(number int) (*Monster, bool) {
	m, ok := c.monByID[number]
	return m, ok
}

// Item 返回 (group, number) 对应的物品定义。
func (c *GameConfig) Item(group, number int) (*Item, bool) {
	i, ok := c.itemByKey[itemKey{group, number}]
	return i, ok
}

// ItemByName 按物品名返回定义（对照 OpenMU 初始化里 `Items.First(i => i.Name == ...)`）。
func (c *GameConfig) ItemByName(name string) (*Item, bool) {
	i, ok := c.itemByName[name]
	return i, ok
}

// MerchantStore 返回 NPC 编号对应的商店（无商店 ok=false，与原版
// definition.MerchantStore == null 语义一致）。
func (c *GameConfig) MerchantStore(npcNumber int) (*MerchantStoreEntry, bool) {
	st, ok := c.storeByNpcID[npcNumber]
	return st, ok
}

// Class 返回编号对应的职业。
func (c *GameConfig) Class(number int) (*CharacterClass, bool) {
	cl, ok := c.classByID[number]
	return cl, ok
}

// BaseClassNumber 沿 NextClass 链回溯到一阶职业号（对照 RemoteView/Quest/
// CharacterClassExtensions.GetBaseClass：找"谁的 NextClass 是我"，递归到没有人指过来为止）。
// legacy 任务的 0xA0 状态表用它判断"黑暗石"槽是否可见。
func (c *GameConfig) BaseClassNumber(classNumber int) int {
	current := classNumber
	for {
		parent := -1
		for i := range c.CharacterClasses {
			next := c.CharacterClasses[i].NextClass
			if next != nil && *next == current && c.CharacterClasses[i].Number != current {
				parent = c.CharacterClasses[i].Number
				break
			}
		}
		if parent < 0 {
			return current
		}
		current = parent
	}
}

// Skill 返回技能号对应的技能定义。
func (c *GameConfig) Skill(number int) (*Skill, bool) {
	sk, ok := c.skillByNum[number]
	return sk, ok
}

// ItemLevelBonusTableByName 按名查物品每级加成表（装备等级加成关联用）。
func (c *GameConfig) ItemLevelBonusTableByName(name string) (*ItemLevelBonusTableExport, bool) {
	for i := range c.ItemLevelBonusTables {
		if c.ItemLevelBonusTables[i].Name == name {
			return &c.ItemLevelBonusTables[i], true
		}
	}
	return nil, false
}

// BonusAt 返回表中指定物品等级的加成值（无对应等级返回 0）。
func (t *ItemLevelBonusTableExport) BonusAt(level int) float64 {
	for _, b := range t.BonusPerLevel {
		if b.Level == level {
			return float64(b.AdditionalValue)
		}
	}
	return 0
}

// WarpByIndex 返回传送清单条目（原版 WarpList[index]；无条目 ok=false）。
func (c *GameConfig) WarpByIndex(index int) (*Warp, bool) {
	for i := range c.Warps {
		if c.Warps[i].Index == index {
			return &c.Warps[i], true
		}
	}
	return nil, false
}

// ExperienceForLevel 返回等级对应的累计经验（越界返回 -1）。
// 原版语义：GameContext.ExperienceTable[level]。
func (c *GameConfig) ExperienceForLevel(level int) int64 {
	if level < 0 || level >= len(c.Experience.Table) {
		return -1
	}
	return c.Experience.Table[level]
}

// Load 从数据版本文件集合载入并校验配置。
func Load(fsys fs.FS) (*GameConfig, error) {
	c := &GameConfig{}
	// 80_attributes.json 是复合对象（attributes + global_combinations），单独解。
	var attrFile struct {
		Attributes         []AttributeDefinitionExport `json:"attributes"`
		GlobalCombinations []AttributeRelationship     `json:"global_combinations"`
	}
	// 55_drops.json 同为复合对象（monster_drops + map_drops）。
	var dropFile struct {
		MonsterDrops []MonsterDropEntry `json:"monster_drops"`
		MapDrops     []MapDropEntry     `json:"map_drops"`
	}
	// 42_item_options.json 同为复合对象（掉落随机参数 + 选项类型/定义/物品引用/组合奖励/套装组）。
	var itemOptionFile struct {
		MaximumItemOptionLevelDrop  int                                `json:"maximum_item_option_level_drop"`
		ExcellentItemDropLevelDelta int                                `json:"excellent_item_drop_level_delta"`
		OptionTypes                 []ItemOptionTypeExport             `json:"option_types"`
		Definitions                 []ItemOptionDefinitionExport       `json:"definitions"`
		Items                       []ItemOptionEntryExport            `json:"items"`
		CombinationBonuses          []ItemOptionCombinationBonusExport `json:"combination_bonuses"`
		SetGroups                   []ItemSetGroupExport               `json:"set_groups"`
	}
	steps := []struct {
		name   string
		target any
	}{
		{"00_meta.json", &c.Meta},
		{"05_game_config.json", &c.Globals},
		{"10_character_classes.json", &c.CharacterClasses},
		{"20_experience.json", &c.Experience},
		{"30_skills.json", &c.Skills},
		{"35_magic_effects.json", &c.MagicEffects},
		{"40_items.json", &c.Items},
		{"42_item_options.json", &itemOptionFile},
		{"45_item_level_bonus.json", &c.ItemLevelBonusTables},
		{"50_monsters.json", &c.Monsters},
		{"55_drops.json", &dropFile},
		{"60_maps.json", &c.Maps},
		{"70_warps.json", &c.Warps},
		{"80_attributes.json", &attrFile},
	}
	for _, s := range steps {
		raw, err := fs.ReadFile(fsys, s.name)
		if err != nil {
			return nil, fmt.Errorf("config: 读 %s: %w", s.name, err)
		}
		if err := json.Unmarshal(raw, s.target); err != nil {
			return nil, fmt.Errorf("config: 解析 %s: %w", s.name, err)
		}
	}
	// 区域技能形状设置：优先读权威导出件 32_area_skill_settings.json（goldenconfig 已应用
	// 更新插件），缺文件时回落到按技能号的内置表兜底。
	if err := c.loadAreaSkillSettings(fsys); err != nil {
		return nil, err
	}
	c.applyAreaSkillSettings()
	// 85_merchant_stores.json 是 T2-9 新增域（T2-9 起 goldenconfig 才导出）：
	// 旧导出件缺文件时容忍为空（无商店 → 商店交互按原版"无商店"拒绝）。
	var stores []MerchantStoreEntry
	if raw, err := fs.ReadFile(fsys, "85_merchant_stores.json"); err == nil {
		if err := json.Unmarshal(raw, &stores); err != nil {
			return nil, fmt.Errorf("config: 解析 85_merchant_stores.json: %w", err)
		}
		c.MerchantStores = stores
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: 校验失败: %w", err)
	}
	c.Attributes = attrFile.Attributes
	c.GlobalCombinations = attrFile.GlobalCombinations
	c.MonsterDrops = dropFile.MonsterDrops
	c.MapDrops = dropFile.MapDrops
	c.MaximumItemOptionLevelDrop = itemOptionFile.MaximumItemOptionLevelDrop
	c.ExcellentItemDropLevelDelta = itemOptionFile.ExcellentItemDropLevelDelta
	c.ItemOptionTypes = itemOptionFile.OptionTypes
	c.ItemOptionDefinitions = itemOptionFile.Definitions
	c.ItemOptionEntries = itemOptionFile.Items
	c.CombinationBonuses = itemOptionFile.CombinationBonuses
	c.ItemSetGroups = itemOptionFile.SetGroups
	// 必须先建索引再校验掉落域：validateDrops 用 itemByKey 判定"悬空物品引用"，
	// 若索引还没建，hasItem 对**所有**引用都返回 false，会把每个掉落组的
	// PossibleItems 清空、并把 item_type == "None" 的组整体剔除——表现为
	// "怪物击杀不掉物品"（只剩 Money/RandomItem/Excellent/Jewel 等无清单组）。
	// buildIndex 只依赖 c.Items/c.Monsters/c.Maps/c.CharacterClasses/c.Attributes，
	// 这些在此处均已就绪（见上方各 steps 的赋值）。
	c.buildIndex()
	// 90_crafting.json 是 S8 新增域（配方数据）：文件缺失容忍为空；解析后校验物品名可解析。
	if err := c.loadCraftings(fsys); err != nil {
		return nil, err
	}
	// 95_quests.json 是 S10 任务域：缺文件容忍为空；解析后校验物品名可解析。
	if err := c.loadQuests(fsys); err != nil {
		return nil, err
	}
	// 96_master_skills.json 是 TRIM-09 大师树域：缺文件容忍为空。
	if err := c.loadMaster(fsys); err != nil {
		return nil, err
	}
	// 97_chat_commands.json 是 TRIM-08 聊天命令域：缺文件容忍为空。
	if err := c.loadChatCommands(fsys); err != nil {
		return nil, err
	}
	// 98_jewel_mixes.json 是宝石合成/拆分域：缺文件容忍为空。
	if err := c.loadJewelMixes(fsys); err != nil {
		return nil, err
	}
	if err := c.validateAttributes(); err != nil {
		return nil, fmt.Errorf("config: 属性域校验失败: %w", err)
	}
	// 选项域校验依赖 attrByID/itemByKey，必须在上面的 buildIndex 之后。
	if err := c.validateItemOptions(); err != nil {
		return nil, fmt.Errorf("config: 物品选项域校验失败: %w", err)
	}
	if err := c.validateDrops(); err != nil {
		return nil, fmt.Errorf("config: 掉落域校验失败: %w", err)
	}
	if err := c.validateCraftings(); err != nil {
		return nil, fmt.Errorf("config: 配方域校验失败: %w", err)
	}
	if err := c.validateQuests(); err != nil {
		return nil, fmt.Errorf("config: 任务域校验失败: %w", err)
	}
	if err := c.validateMaster(); err != nil {
		return nil, fmt.Errorf("config: 大师树域校验失败: %w", err)
	}
	if err := c.validateCombos(); err != nil {
		return nil, fmt.Errorf("config: 连击域校验失败: %w", err)
	}
	if err := c.validateChatCommands(); err != nil {
		return nil, fmt.Errorf("config: 聊天命令域校验失败: %w", err)
	}
	return c, nil
}

// validateDrops 校验掉落域（T1-6）。原版数据存在**悬空物品引用**（如怪物 132 引用
// (13,19)，不在 677 个物品定义内）——与原版运行时的容错一致：剔除无效引用；
// 无 PossibleItems 的组（Money/RandomItem/Excellent 等特殊类型）按类型保留。
func (c *GameConfig) validateDrops() error {
	hasItem := func(group, number int) bool {
		_, ok := c.itemByKey[itemKey{group, number}]
		return ok
	}
	prune := func(groups []DropGroup) []DropGroup {
		out := make([]DropGroup, 0, len(groups))
		for i := range groups {
			g := groups[i]
			if len(g.PossibleItems) > 0 {
				valid := g.PossibleItems[:0]
				for _, pi := range g.PossibleItems {
					if hasItem(pi.Group, pi.Number) {
						valid = append(valid, pi)
					}
				}
				g.PossibleItems = valid
			}
			// 保留条件：有可掉物品，或本就是"按类型生成"的组（无清单）。
			if len(g.PossibleItems) > 0 || g.ItemType != "None" {
				out = append(out, g)
			}
		}
		return out
	}
	for i := range c.MonsterDrops {
		c.MonsterDrops[i].Groups = prune(c.MonsterDrops[i].Groups)
	}
	for i := range c.MapDrops {
		c.MapDrops[i].Groups = prune(c.MapDrops[i].Groups)
	}
	return nil
}

// LoadSeason6 便捷入口：载入 season6 数据（MuMain/GMO/开源端点共用）。
func LoadSeason6() (*GameConfig, error) {
	fsys, err := data.FS("season6")
	if err != nil {
		return nil, err
	}
	return Load(fsys)
}

// validate 对导出件做载入期校验（doc/10 防线 1：防止导出器与 Go 结构体静默错位）。
func (c *GameConfig) validate() error {
	if c.Meta.SchemaVersion != 1 {
		return fmt.Errorf("schema_version=%d, want 1", c.Meta.SchemaVersion)
	}
	cnt := c.Meta.Counts
	switch {
	case cnt.CharacterClasses != len(c.CharacterClasses):
		return fmt.Errorf("职业数不符 meta=%d 实际=%d", cnt.CharacterClasses, len(c.CharacterClasses))
	case cnt.Skills != len(c.Skills):
		return fmt.Errorf("技能数不符 meta=%d 实际=%d", cnt.Skills, len(c.Skills))
	case cnt.Items != len(c.Items):
		return fmt.Errorf("物品数不符 meta=%d 实际=%d", cnt.Items, len(c.Items))
	case cnt.Monsters != len(c.Monsters):
		return fmt.Errorf("怪物数不符 meta=%d 实际=%d", cnt.Monsters, len(c.Monsters))
	case cnt.Maps != len(c.Maps):
		return fmt.Errorf("地图数不符 meta=%d 实际=%d", cnt.Maps, len(c.Maps))
	case cnt.Warps != len(c.Warps):
		return fmt.Errorf("传送数不符 meta=%d 实际=%d", cnt.Warps, len(c.Warps))
	}

	// 经验表：长度 = 最大等级 + 2（0..Max+1），单调不减。
	if want := c.Experience.MaximumLevel + 2; len(c.Experience.Table) != want {
		return fmt.Errorf("经验表长度 %d ≠ maxLevel+2 = %d", len(c.Experience.Table), want)
	}
	for i := 1; i < len(c.Experience.Table); i++ {
		if c.Experience.Table[i] < c.Experience.Table[i-1] {
			return fmt.Errorf("经验表在等级 %d 处回退: %d < %d", i, c.Experience.Table[i], c.Experience.Table[i-1])
		}
	}

	// 唯一性。
	classIDs := make(map[int]bool, len(c.CharacterClasses))
	for i := range c.CharacterClasses {
		n := c.CharacterClasses[i].Number
		if classIDs[n] {
			return fmt.Errorf("职业编号重复 %d", n)
		}
		classIDs[n] = true
	}
	monsterIDs := make(map[int]bool, len(c.Monsters))
	for i := range c.Monsters {
		n := c.Monsters[i].Number
		if monsterIDs[n] {
			return fmt.Errorf("怪物编号重复 %d", n)
		}
		monsterIDs[n] = true
	}
	itemKeys := make(map[itemKey]bool, len(c.Items))
	for i := range c.Items {
		k := itemKey{c.Items[i].Group, c.Items[i].Number}
		if itemKeys[k] {
			return fmt.Errorf("物品键重复 (%d,%d)", k.group, k.number)
		}
		itemKeys[k] = true
	}
	// 唯一性：地图按 (编号, discriminator) 唯一（同号变体是原版设计：DS/攻城等状态切换）。
	mapVariant := make(map[[2]int]bool, len(c.Maps))
	mapNums := make(map[int]bool, len(c.Maps))
	for i := range c.Maps {
		n := c.Maps[i].Number
		k := [2]int{n, c.Maps[i].Discriminator}
		if mapVariant[k] {
			return fmt.Errorf("地图变体重复 (%d,%d)", n, c.Maps[i].Discriminator)
		}
		mapVariant[k] = true
		mapNums[n] = true
	}

	// 交叉引用：出生点引用的怪物必须存在。
	totalSpawns := 0
	for i := range c.Maps {
		mp := &c.Maps[i]
		for j := range mp.Spawns {
			sp := &mp.Spawns[j]
			if sp.Monster == nil {
				continue
			}
			if !monsterIDs[*sp.Monster] {
				return fmt.Errorf("地图 %d 出生点引用了不存在的怪物 %d", mp.Number, *sp.Monster)
			}
			totalSpawns++
		}
		// 传送门目标地图必须存在。
		for _, g := range mp.EnterGates {
			if g.Target == nil || g.Target.Map == nil {
				continue
			}
			if !mapNums[*g.Target.Map] {
				return fmt.Errorf("地图 %d 传送门 %d 指向不存在的地图 %d", mp.Number, g.Number, *g.Target.Map)
			}
		}
		for _, w := range c.Warps {
			if w.Gate == nil || w.Gate.Map == nil {
				continue
			}
		}
	}
	if cnt.MonsterSpawns != totalSpawns {
		return fmt.Errorf("出生点总数不符 meta=%d 实际=%d", cnt.MonsterSpawns, totalSpawns)
	}
	for _, w := range c.Warps {
		if w.Gate == nil || w.Gate.Map == nil {
			return fmt.Errorf("传送 %d 缺少目标门", w.Index)
		}
		if !mapNums[*w.Gate.Map] {
			return fmt.Errorf("传送 %d 指向不存在的地图 %d", w.Index, *w.Gate.Map)
		}
	}
	return nil
}

// validateAttributes 校验属性域（T1-0）：id 唯一、关系引用可解析、枚举字符串合法。
func (c *GameConfig) validateAttributes() error {
	if len(c.Attributes) == 0 {
		return fmt.Errorf("属性定义为空")
	}
	byID := make(map[string]bool, len(c.Attributes))
	for i := range c.Attributes {
		a := &c.Attributes[i]
		if a.ID == "" || a.Designation == "" {
			return fmt.Errorf("属性定义缺少 id/designation: %+v", a)
		}
		if byID[a.ID] {
			return fmt.Errorf("属性 id 重复 %s (%s)", a.ID, a.Designation)
		}
		byID[a.ID] = true
	}
	resolvable := func(ref *AttributeRef, what string, owner string) error {
		if ref == nil {
			return fmt.Errorf("%s: %s 引用为空", owner, what)
		}
		if !byID[ref.ID] {
			return fmt.Errorf("%s: %s 引用了不存在的属性 %q(%s)", owner, what, ref.Designation, ref.ID)
		}
		return nil
	}
	checkRel := func(r AttributeRelationship, owner string) error {
		switch r.InputOperator {
		case "Multiply", "Add", "Exponentiate", "ExponentiateByAttribute", "Maximum", "Minimum":
		default:
			return fmt.Errorf("%s: 未知 InputOperator %q", owner, r.InputOperator)
		}
		switch r.AggregateType {
		case "AddRaw", "Multiplicate", "AddFinal", "Maximum":
		default:
			return fmt.Errorf("%s: 未知 AggregateType %q", owner, r.AggregateType)
		}
		if err := resolvable(r.Target, "target", owner); err != nil {
			return err
		}
		if err := resolvable(r.Input, "input", owner); err != nil {
			return err
		}
		if r.Operand != nil {
			return resolvable(r.Operand, "operand", owner)
		}
		return nil
	}
	for i := range c.GlobalCombinations {
		if err := checkRel(c.GlobalCombinations[i], "全局关系"); err != nil {
			return err
		}
	}
	for i := range c.CharacterClasses {
		cls := &c.CharacterClasses[i]
		for j := range cls.AttributeCombinations {
			if err := checkRel(cls.AttributeCombinations[j], "职业 "+cls.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// AttributeDefinitionByID 按 id 取属性定义。
func (c *GameConfig) AttributeDefinitionByID(id string) (*AttributeDefinitionExport, bool) {
	a, ok := c.attrByID[id]
	return a, ok
}

// AttributeByName 按 designation 取属性定义（首个匹配；核心属性名在导出件中唯一，
// 少量重复为事件变体——关系解析一律走 id，本方法仅供 T1-2 的角色装配按名取值）。
func (c *GameConfig) AttributeByName(designation string) (*AttributeDefinitionExport, bool) {
	for i := range c.Attributes {
		if c.Attributes[i].Designation == designation {
			return &c.Attributes[i], true
		}
	}
	return nil, false
}

// parseEnum 把导出的枚举字符串转成 internal/attribute 的类型（T1-2 装配用）。
func parseAggregateType(s string) (attribute.AggregateType, error) {
	switch s {
	case "AddRaw":
		return attribute.AggregateAddRaw, nil
	case "Multiplicate":
		return attribute.AggregateMultiplicate, nil
	case "AddFinal":
		return attribute.AggregateAddFinal, nil
	case "Maximum":
		return attribute.AggregateMaximum, nil
	}
	return 0, fmt.Errorf("未知 AggregateType %q", s)
}

// AggregateTypeByName 导出版入口（供 player 等消费方把导出件的 aggregation 字符串
// 转成属性系统枚举；词表与原版 AggregateType 逐字一致）。
func AggregateTypeByName(s string) (attribute.AggregateType, bool) {
	t, err := parseAggregateType(s)
	return t, err == nil
}

func parseInputOperator(s string) (attribute.InputOperator, error) {
	switch s {
	case "Multiply":
		return attribute.InputMultiply, nil
	case "Add":
		return attribute.InputAdd, nil
	case "Exponentiate":
		return attribute.InputExponentiate, nil
	case "ExponentiateByAttribute":
		return attribute.InputExponentiateByAttribute, nil
	case "Maximum":
		return attribute.InputMaximum, nil
	case "Minimum":
		return attribute.InputMinimum, nil
	}
	return 0, fmt.Errorf("未知 InputOperator %q", s)
}

// buildIndex 建立 id → 对象索引。
func (c *GameConfig) buildIndex() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 稳定遍历序（日志与测试的可重复性）。**必须先排好再建索引**：下面全部按
	// `&slice[i]` 取指针存进 map，排布一变这些指针就指向别的元素（曾是"68 个地图编号里
	// 59 个的 Map(n) 返回错图"的根因，守卫见 map_index_test.go）。
	sort.Slice(c.Maps, func(i, j int) bool {
		if c.Maps[i].Number != c.Maps[j].Number {
			return c.Maps[i].Number < c.Maps[j].Number
		}
		return c.Maps[i].Discriminator < c.Maps[j].Discriminator
	})
	// 主变体索引：同编号取 Discriminator 最小者（0 = 主）。
	c.mapByNum = make(map[int]*GameMap, len(c.Maps))
	for i := range c.Maps {
		mp := &c.Maps[i]
		if cur, ok := c.mapByNum[mp.Number]; ok && cur.Discriminator <= mp.Discriminator {
			continue
		}
		c.mapByNum[mp.Number] = mp
	}
	c.monByID = make(map[int]*Monster, len(c.Monsters))
	for i := range c.Monsters {
		c.monByID[c.Monsters[i].Number] = &c.Monsters[i]
	}
	c.itemByKey = make(map[itemKey]*Item, len(c.Items))
	for i := range c.Items {
		c.itemByKey[itemKey{c.Items[i].Group, c.Items[i].Number}] = &c.Items[i]
	}
	c.itemByName = make(map[string]*Item, len(c.Items))
	for i := range c.Items {
		if c.Items[i].Name != "" {
			if _, dup := c.itemByName[c.Items[i].Name]; !dup {
				c.itemByName[c.Items[i].Name] = &c.Items[i]
			}
		}
	}
	c.skillByNum = make(map[int]*Skill, len(c.Skills))
	for i := range c.Skills {
		c.skillByNum[c.Skills[i].Number] = &c.Skills[i]
	}
	c.classByID = make(map[int]*CharacterClass, len(c.CharacterClasses))
	for i := range c.CharacterClasses {
		c.classByID[c.CharacterClasses[i].Number] = &c.CharacterClasses[i]
	}
	c.attrByID = make(map[string]*AttributeDefinitionExport, len(c.Attributes))
	for i := range c.Attributes {
		c.attrByID[c.Attributes[i].ID] = &c.Attributes[i]
	}
	c.storeByNpcID = make(map[int]*MerchantStoreEntry, len(c.MerchantStores))
	for i := range c.MerchantStores {
		c.storeByNpcID[c.MerchantStores[i].NpcNumber] = &c.MerchantStores[i]
	}
	c.optTypeByID = make(map[string]*ItemOptionTypeExport, len(c.ItemOptionTypes))
	c.optTypeByKind = make(map[string]*ItemOptionTypeExport, len(c.ItemOptionTypes))
	for i := range c.ItemOptionTypes {
		c.optTypeByID[c.ItemOptionTypes[i].ID] = &c.ItemOptionTypes[i]
		if k := c.ItemOptionTypes[i].Kind; k != "" {
			c.optTypeByKind[k] = &c.ItemOptionTypes[i]
		}
	}
	c.optDefByID = make(map[string]*ItemOptionDefinitionExport, len(c.ItemOptionDefinitions))
	for i := range c.ItemOptionDefinitions {
		c.optDefByID[c.ItemOptionDefinitions[i].ID] = &c.ItemOptionDefinitions[i]
	}
	c.optEntryByKey = make(map[itemKey][]string, len(c.ItemOptionEntries))
	for i := range c.ItemOptionEntries {
		c.optEntryByKey[itemKey{c.ItemOptionEntries[i].Group, c.ItemOptionEntries[i].Number}] = c.ItemOptionEntries[i].OptionDefinitions
	}
	// 物品 → 所属套装组（套装组成员表反查；同一物品可属多组，远古最多两套）。
	c.optSetGroups = make(map[itemKey][]*ItemSetGroupExport)
	for i := range c.ItemSetGroups {
		g := &c.ItemSetGroups[i]
		for j := range g.Items {
			it := &g.Items[j]
			if it.Group == nil || it.Number == nil {
				continue
			}
			k := itemKey{*it.Group, *it.Number}
			c.optSetGroups[k] = append(c.optSetGroups[k], g)
		}
	}
}
