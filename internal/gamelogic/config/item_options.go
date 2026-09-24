package config

// item_options.go —— 物品选项数据面（42_item_options.json，T2-12）。
//
// 一次性载入**全部**选项定义，避免后续每接一种选项（幸运/普通/卓越/远古/和谐/镶嵌/
// 守护/翅膀/宠物）都要重跑导出器。对照原版数据模型（DataModel/Configuration/Items）：
//
//	GameConfiguration.ItemOptions（全局定义表）
//	  └─ ItemOptionDefinition.PossibleOptions（候选选项）
//	       └─ IncreasableItemOption.LevelDependentOptions（按等级的加成表）
//	            └─ PowerUpDefinition{TargetAttribute, Boost}
//	ItemDefinition.PossibleItemOptions（物品 → 候选定义引用）
//	ItemOptionCombinationBonus（镶嵌"套装包"等组合奖励）
//	ItemSetGroup / ItemOfItemSet（远古套装、防御率套装、等级套装）
//
// 本文件只做**数据面**的载入与校验：把选项加成算进角色属性（ItemPowerUpFactory）、
// 掉落时随机生成选项（DefaultDropGenerator）属于消费端，另行接入。

import "fmt"

// ItemOptionTypeExport 对照原版 ItemOptionType（卓越/翅膀/幸运/普通/和谐/远古/远古奖励/
// 守护/镶嵌/镶嵌奖励/三色 Fenrir/黑王马）。
type ItemOptionTypeExport struct {
	ID string `json:"id"`
	// Kind 是稳定的语义标识：Luck / Option / Excellent / Wing / AncientOption /
	// AncientBonus / HarmonyOption / SocketOption / SocketBonusOption / GuardianOption /
	// BlueFenrir / BlackFenrir / GoldFenrir / DarkHorse。
	// 由导出器按原版 ItemOptionTypes 静态实例的 Id 判定，消费端**不要**按 Name 匹配。
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsVisible   bool   `json:"is_visible"`
}

// 选项类型 kind 常量（与导出器 OptionTypeKind 逐字一致）。
const (
	OptionKindLuck          = "Luck"
	OptionKindOption        = "Option"
	OptionKindExcellent     = "Excellent"
	OptionKindWing          = "Wing"
	OptionKindAncientOption = "AncientOption"
	OptionKindAncientBonus  = "AncientBonus"
	OptionKindHarmony       = "HarmonyOption"
	OptionKindSocket        = "SocketOption"
	OptionKindSocketBonus   = "SocketBonusOption"
	OptionKindGuardian      = "GuardianOption"
	OptionKindBlueFenrir    = "BlueFenrir"
	OptionKindBlackFenrir   = "BlackFenrir"
	OptionKindGoldFenrir    = "GoldFenrir"
	OptionKindDarkHorse     = "DarkHorse"
)

// ItemOptionDefinitionExport 对照原版 ItemOptionDefinition（一组候选选项 + 掉落随机参数）。
type ItemOptionDefinitionExport struct {
	ID                    string                        `json:"id"`
	Name                  string                        `json:"name"`
	AddsRandomly          bool                          `json:"adds_randomly"`
	AddChance             float64                       `json:"add_chance"`
	MaximumOptionsPerItem int                           `json:"maximum_options_per_item"`
	PossibleOptions       []IncreasableItemOptionExport `json:"possible_options"`
}

// IncreasableItemOptionExport 对照原版 IncreasableItemOption（一条可增长的选项）。
type IncreasableItemOptionExport struct {
	Number        int    `json:"number"`
	OptionTypeID  string `json:"option_type"`
	SubOptionType int    `json:"sub_option_type"`
	// LevelType 为 "OptionLevel"（普通/和谐/镶嵌：等级取**选项等级**）或
	// "ItemLevel"（翅膀：等级取**物品等级**）。查 LevelDependentOptions 时不能混用
	// （对照 ItemPowerUpFactory.GetPowerUpsOfItemOptions 的分支）。
	LevelType string `json:"level_type"`
	// Weight 是和谐选项的 roll 权重。
	Weight int `json:"weight"`
	// PowerUp 是等级 1 的加成（原版 ItemOption.PowerUpDefinition）；nil = 无加成。
	PowerUp *PowerUpDef `json:"power_up"`
	// LevelDependentOptions 是等级 2..N 的加成表（按 level 升序）。
	LevelDependentOptions []ItemOptionOfLevelExport `json:"level_dependent_options"`
}

// ItemOptionOfLevelExport 对照原版 ItemOptionOfLevel（一条按等级的加成）。
type ItemOptionOfLevelExport struct {
	Level int `json:"level"`
	// RequiredItemLevel 仅和谐选项使用（该档所需的物品等级）。
	RequiredItemLevel int         `json:"required_item_level"`
	PowerUp           *PowerUpDef `json:"power_up"`
}

// ItemOptionCombinationBonusExport 对照原版 ItemOptionCombinationBonus：同时满足若干
// "选项类型 + 子类型 + 最小数量"时给额外加成（如镶嵌套装包、Fenrir 移速）。
type ItemOptionCombinationBonusExport struct {
	Number               int                                 `json:"number"`
	Description          string                              `json:"description"`
	AppliesMultipleTimes bool                                `json:"applies_multiple_times"`
	Requirements         []CombinationBonusRequirementExport `json:"requirements"`
	Bonus                *PowerUpDef                         `json:"bonus"`
}

// CombinationBonusRequirementExport 对照原版 CombinationBonusRequirement。
type CombinationBonusRequirementExport struct {
	OptionTypeID  string `json:"option_type"`
	SubOptionType int    `json:"sub_option_type"`
	MinimumCount  int    `json:"minimum_count"`
}

// ItemOptionEntryExport 是"某物品可出现的候选选项定义"（原版 ItemDefinition.PossibleItemOptions）。
type ItemOptionEntryExport struct {
	Group             int      `json:"group"`
	Number            int      `json:"number"`
	OptionDefinitions []string `json:"option_definitions"`
}

// ItemSetGroupExport 对照原版 ItemSetGroup（远古套装、防御率套装、等级套装等）。
type ItemSetGroupExport struct {
	Name             string `json:"name"`
	AlwaysApplies    bool   `json:"always_applies"`
	CountDistinct    bool   `json:"count_distinct"`
	MinimumItemCount int    `json:"minimum_item_count"`
	SetLevel         int    `json:"set_level"`
	// OptionDefinition 是套装加成定义（引用 definitions[].id）；空串 = 无。
	OptionDefinition string                `json:"option_definition"`
	Items            []ItemOfItemSetExport `json:"items"`
}

// ItemOfItemSetExport 对照原版 ItemOfItemSet（套装中的一件物品）。
type ItemOfItemSetExport struct {
	Group  *int `json:"group"`
	Number *int `json:"number"`
	// AncientSetDiscriminator 仅远古套装相关：同一件物品最多属于两套远古（1/2）。
	AncientSetDiscriminator int `json:"ancient_set_discriminator"`
	// BonusOption 是该件物品在套装中额外携带的远古属性（原版 ItemOfItemSet.BonusOption）。
	BonusOption *IncreasableItemOptionExport `json:"bonus_option"`
}

// ItemOptionType 按 id 返回选项类型（42_item_options.json 的 option_types[].id）。
func (c *GameConfig) ItemOptionType(id string) (*ItemOptionTypeExport, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.optTypeByID[id]
	return t, ok
}

// ItemOptionTypeByKind 按语义标识返回选项类型（如 OptionKindLuck）。
func (c *GameConfig) ItemOptionTypeByKind(kind string) (*ItemOptionTypeExport, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.optTypeByKind[kind]
	return t, ok
}

// ItemOptionDefinition 按 id 返回选项定义。
func (c *GameConfig) ItemOptionDefinition(id string) (*ItemOptionDefinitionExport, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.optDefByID[id]
	return d, ok
}

// SetGroupsForItem 返回 (group, number) 所属的套装组（同一物品可属多组；远古最多两套，
// 由 ItemOfItemSet.AncientSetDiscriminator 区分）。
func (c *GameConfig) SetGroupsForItem(group, number int) []*ItemSetGroupExport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.optSetGroups[itemKey{group, number}]
}

// HasKind 判定定义里是否有指定 kind 的候选选项（如"这条定义是不是卓越定义"）。
func (d *ItemOptionDefinitionExport) HasKind(cfg *GameConfig, kind string) bool {
	for i := range d.PossibleOptions {
		if t, ok := cfg.ItemOptionType(d.PossibleOptions[i].OptionTypeID); ok && t.Kind == kind {
			return true
		}
	}
	return false
}

// DefinitionOfOptionType 返回候选定义里指定 kind 的定义（找不到返回 nil）。
// 同一物品的同类定义通常唯一（如普通伤害选项按武器类型只加一条）。
func (c *GameConfig) DefinitionOfOptionType(definitionIDs []string, kind string) *ItemOptionDefinitionExport {
	for _, id := range definitionIDs {
		d, ok := c.ItemOptionDefinition(id)
		if !ok {
			continue
		}
		for i := range d.PossibleOptions {
			if t, ok := c.ItemOptionType(d.PossibleOptions[i].OptionTypeID); ok && t.Kind == kind {
				return d
			}
		}
	}
	return nil
}

// ItemOptionDefinitionsFor 返回 (group, number) 可出现的候选选项定义 id 列表（无则 nil）。
func (c *GameConfig) ItemOptionDefinitionsFor(group, number int) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.optEntryByKey[itemKey{group, number}]
}

// validateItemOptions 校验物品选项域（T2-12）。
//
// 与 validateDrops 的"剔除悬空引用"不同：选项引用错误会**静默改变数值**（少一条加成、
// 或加成落到错误属性上），所以这里一律报错而不是丢弃。
func (c *GameConfig) validateItemOptions() error {
	typeIDs := make(map[string]bool, len(c.ItemOptionTypes))
	typeKinds := make(map[string]bool, len(c.ItemOptionTypes))
	for i := range c.ItemOptionTypes {
		t := &c.ItemOptionTypes[i]
		if t.ID == "" || t.Name == "" {
			return fmt.Errorf("选项类型缺少 id/name: %+v", t)
		}
		// kind 是消费端唯一的类型判据（不按 Name 匹配），必须存在且唯一。
		if t.Kind == "" {
			return fmt.Errorf("选项类型 %q 缺少 kind（导出器 OptionTypeKind 未覆盖）", t.Name)
		}
		if typeKinds[t.Kind] {
			return fmt.Errorf("选项类型 kind 重复 %s (%s)", t.Kind, t.Name)
		}
		typeKinds[t.Kind] = true
		if typeIDs[t.ID] {
			return fmt.Errorf("选项类型 id 重复 %s (%s)", t.ID, t.Name)
		}
		typeIDs[t.ID] = true
	}

	defIDs := make(map[string]bool, len(c.ItemOptionDefinitions))
	for i := range c.ItemOptionDefinitions {
		d := &c.ItemOptionDefinitions[i]
		if d.ID == "" {
			return fmt.Errorf("选项定义缺少 id: %q", d.Name)
		}
		if defIDs[d.ID] {
			return fmt.Errorf("选项定义 id 重复 %s (%q)", d.ID, d.Name)
		}
		defIDs[d.ID] = true
		owner := fmt.Sprintf("选项定义 %q", d.Name)
		for j := range d.PossibleOptions {
			if err := c.validateItemOption(&d.PossibleOptions[j], typeIDs, owner); err != nil {
				return err
			}
		}
	}

	for i := range c.ItemOptionEntries {
		e := &c.ItemOptionEntries[i]
		if _, ok := c.itemByKey[itemKey{e.Group, e.Number}]; !ok {
			return fmt.Errorf("选项引用指向不存在的物品 (%d,%d)", e.Group, e.Number)
		}
		for _, id := range e.OptionDefinitions {
			if !defIDs[id] {
				return fmt.Errorf("物品 (%d,%d) 引用了不存在的选项定义 %s", e.Group, e.Number, id)
			}
		}
	}

	for i := range c.CombinationBonuses {
		b := &c.CombinationBonuses[i]
		for _, r := range b.Requirements {
			if r.OptionTypeID != "" && !typeIDs[r.OptionTypeID] {
				return fmt.Errorf("组合奖励 %d 引用了不存在的选项类型 %s", b.Number, r.OptionTypeID)
			}
		}
		if err := c.validateOptionPowerUp(b.Bonus, fmt.Sprintf("组合奖励 %d", b.Number)); err != nil {
			return err
		}
	}

	for i := range c.ItemSetGroups {
		g := &c.ItemSetGroups[i]
		if g.OptionDefinition != "" && !defIDs[g.OptionDefinition] {
			return fmt.Errorf("套装组 %q 引用了不存在的选项定义 %s", g.Name, g.OptionDefinition)
		}
		for j := range g.Items {
			it := &g.Items[j]
			if it.Group == nil || it.Number == nil {
				continue
			}
			if _, ok := c.itemByKey[itemKey{*it.Group, *it.Number}]; !ok {
				return fmt.Errorf("套装组 %q 引用了不存在的物品 (%d,%d)", g.Name, *it.Group, *it.Number)
			}
			if it.BonusOption != nil {
				if err := c.validateItemOption(it.BonusOption, typeIDs, fmt.Sprintf("套装组 %q", g.Name)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateItemOption 校验一条候选选项：选项类型可解析、等级类型合法、各档加成合法。
func (c *GameConfig) validateItemOption(o *IncreasableItemOptionExport, typeIDs map[string]bool, owner string) error {
	if o.OptionTypeID != "" && !typeIDs[o.OptionTypeID] {
		return fmt.Errorf("%s: 选项 %d 引用了不存在的选项类型 %s", owner, o.Number, o.OptionTypeID)
	}
	switch o.LevelType {
	case "OptionLevel", "ItemLevel":
	default:
		return fmt.Errorf("%s: 选项 %d 的 level_type 未知 %q", owner, o.Number, o.LevelType)
	}
	if err := c.validateOptionPowerUp(o.PowerUp, fmt.Sprintf("%s 选项 %d", owner, o.Number)); err != nil {
		return err
	}
	for i := range o.LevelDependentOptions {
		l := &o.LevelDependentOptions[i]
		if err := c.validateOptionPowerUp(l.PowerUp, fmt.Sprintf("%s 选项 %d 等级 %d", owner, o.Number, l.Level)); err != nil {
			return err
		}
	}
	return nil
}

// validateOptionPowerUp 校验一条加成：目标属性可解析、聚合形态与关系算子合法、
// 关系里的属性引用可解析（枚举白名单与 validateAttributes 同源）。
func (c *GameConfig) validateOptionPowerUp(p *PowerUpDef, owner string) error {
	if p == nil {
		return nil
	}
	if p.TargetID != "" && !c.attrResolvable(p.TargetID) {
		return fmt.Errorf("%s: 目标属性 %q(%s) 不存在", owner, p.Target, p.TargetID)
	}
	switch p.Boost.AggregateType {
	case "AddRaw", "Multiplicate", "AddFinal", "Maximum":
	default:
		return fmt.Errorf("%s: 未知 AggregateType %q", owner, p.Boost.AggregateType)
	}
	for _, r := range p.Boost.Related {
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
		// related 的 target 在原版里允许为 null：该条只贡献"输入属性 ⊗ 操作数"的值，
		// 目标属性由外层 PowerUpDefinition.TargetAttribute 承担（消费端 evalRelation
		// 也确实不读 target）。非 null 时才要求可解析，以便发现真正的悬空引用。
		if r.Target != nil && !c.attrResolvable(r.Target.ID) {
			return fmt.Errorf("%s: 关系目标属性 %q(%s) 不可解析", owner, r.Target.Designation, r.Target.ID)
		}
		if r.Input == nil || !c.attrResolvable(r.Input.ID) {
			return fmt.Errorf("%s: 关系输入属性不可解析", owner)
		}
		if r.Operand != nil && !c.attrResolvable(r.Operand.ID) {
			return fmt.Errorf("%s: 关系操作数属性不可解析", owner)
		}
	}
	return nil
}

// attrResolvable 判定属性 id 是否在 80_attributes.json 中（调用前须已 buildIndex）。
func (c *GameConfig) attrResolvable(id string) bool {
	_, ok := c.attrByID[id]
	return ok
}
