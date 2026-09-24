package config

// crafting.go —— 从 90_crafting.json 载入合成配方，对应 OpenMU DataModel/Configuration/
// ItemCrafting/{ItemCrafting,SimpleCraftingSettings,ItemCraftingRequiredItem,ItemCraftingResultItem}.cs。
// 只承载数据面；判定与执行在 gamelogic/action/crafting*.go（PlayerActions/Items + Craftings）。

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// Crafting 是一条配方（原版 ItemCrafting）。Handler 是原版 ItemCraftingHandlerClassName，
// 空串即 SimpleItemCraftingHandler；Settings 为 nil 表示处理器自建投入要求（活动券、Fenrir 升级）。
type Crafting struct {
	Number   int               `json:"number"`
	Name     string            `json:"name"`
	Handler  string            `json:"handler"`
	Hosts    []CraftingHost    `json:"hosts"`
	Settings *CraftingSettings `json:"settings"`
}

// CraftingHost 是配方的宿主 NPC（原版配方挂在 MonsterDefinition.ItemCraftings 上）。
type CraftingHost struct {
	Monster   int    `json:"monster"`
	Name      string `json:"name"`
	NpcWindow string `json:"npc_window"`
}

// CraftingSettings 对应 SimpleCraftingSettings。Add* 允许为负（原版是 int，
// 求值时按 (byte) 逐次回绕，Go 侧必须原样模拟，见 action/crafting_rate.go）。
type CraftingSettings struct {
	Money                  int                `json:"money"`
	MoneyPerSuccessPercent int                `json:"money_per_success_percent"`
	NpcPriceDivisor        int                `json:"npc_price_divisor"`
	SuccessPercent         int                `json:"success_percent"`
	MaxSuccessPercent      int                `json:"max_success_percent"`
	MultipleAllowed        bool               `json:"multiple_allowed"`
	ResultSelect           string             `json:"result_select"` // Any / All
	AddLuck                int                `json:"add_luck"`
	AddExcellent           int                `json:"add_excellent"`
	AddAncient             int                `json:"add_ancient"`
	AddGuardian            int                `json:"add_guardian"`
	AddSocket              int                `json:"add_socket"`
	ResultLuckChance       int                `json:"result_luck_chance"`
	ResultSkillChance      int                `json:"result_skill_chance"`
	ResultExcChance        int                `json:"result_exc_chance"`
	ResultMaxExc           int                `json:"result_max_exc"`
	Required               []CraftingRequired `json:"required"`
	ResultItems            []CraftingResult   `json:"result_items"`
}

// CraftingRequired 是投入要求。PossibleItems 为空 = 原版"匹配任意定义"。
type CraftingRequired struct {
	Reference       int       `json:"reference"`
	MinAmount       int       `json:"min_amount"`
	MaxAmount       int       `json:"max_amount"` // 0 = 不限
	MinLevel        int       `json:"min_level"`
	MaxLevel        int       `json:"max_level"`
	AddPercentage   int       `json:"add_percentage"`
	NpcPriceDivisor int       `json:"npc_price_divisor"`
	SuccessResult   string    `json:"success_result"`
	FailResult      string    `json:"fail_result"`
	PossibleItems   []ItemRef `json:"possible_items"`
	RequiredOptions []string  `json:"required_options"` // 选项 kind（Option/Excellent/AncientBonus/HarmonyOption…）
}

// CraftingResult 是产出。Reference>0 → 就地改该投入物等级；否则按 Item 新建。
type CraftingResult struct {
	Reference  int      `json:"reference"`
	AddLevel   int      `json:"add_level"`
	MinLevel   int      `json:"min_level"`
	MaxLevel   int      `json:"max_level"`
	Durability *int     `json:"durability"`
	Item       *ItemRef `json:"item"`
}

// MixResult 取值（原版 enum MixResult 的名字）。
const (
	MixDisappear      = "Disappear"
	MixStaysAsIs      = "StaysAsIs"
	MixChaosDowngrade = "ChaosWeaponAndFirstWingsDowngradedRandom"
	MixWingsDowngrade = "ThirdWingsDowngradedRandom"
)

// HostedBy 报告该 NPC 是否可做这条配方（原版只在 OpenedNpc.Definition.ItemCraftings 里查）。
func (c *Crafting) HostedBy(npcMonster int) bool {
	for _, h := range c.Hosts {
		if h.Monster == npcMonster {
			return true
		}
	}
	return false
}

// CraftingByNumber 返回指定编号的配方。
func (c *GameConfig) CraftingByNumber(number int) (*Crafting, bool) {
	cr, ok := c.craftingByNum[number]
	return cr, ok
}

// CraftingForNpc 返回"该 NPC 能做的第 number 号配方"——编号不属于该 NPC 时 false
// （对照原版按 OpenedNpc 的配方表查找，防跨窗口混做）。
func (c *GameConfig) CraftingForNpc(number, npcMonster int) (*Crafting, bool) {
	cr, ok := c.craftingByNum[number]
	if !ok || !cr.HostedBy(npcMonster) {
		return nil, false
	}
	return cr, true
}

// loadCraftings 解析 90_crafting.json；文件缺失时容忍为空（合成一律拒绝）。
// 必须在 buildIndex 之后调用（validateCraftings 按 (group,number) 解析引用）。
func (c *GameConfig) loadCraftings(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "90_crafting.json")
	if err != nil {
		return nil
	}
	var payload struct {
		Craftings []Crafting `json:"craftings"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("config: 解析 90_crafting.json: %w", err)
	}
	c.Craftings = payload.Craftings
	c.craftingByNum = make(map[int]*Crafting, len(c.Craftings))
	for i := range c.Craftings {
		c.craftingByNum[c.Craftings[i].Number] = &c.Craftings[i]
	}
	return nil
}

// validateCraftings 校验引用可解析、MixResult/ResultSelect 取值合法、
// 且通用处理器（handler 为空）必须带 settings（原版构造器直接 throw）。
func (c *GameConfig) validateCraftings() error {
	for i := range c.Craftings {
		cr := &c.Craftings[i]
		if cr.Handler == "" && cr.Settings == nil {
			return fmt.Errorf("配方 %d 无 handler 也无 settings", cr.Number)
		}
		if len(cr.Hosts) == 0 {
			return fmt.Errorf("配方 %d 没有宿主 NPC", cr.Number)
		}
		if cr.Settings == nil {
			continue
		}
		if cr.Settings.ResultSelect != "Any" && cr.Settings.ResultSelect != "All" {
			return fmt.Errorf("配方 %d result_select=%q 非法", cr.Number, cr.Settings.ResultSelect)
		}
		for _, req := range cr.Settings.Required {
			if !validMixResult(req.SuccessResult) || !validMixResult(req.FailResult) {
				return fmt.Errorf("配方 %d 投入项 MixResult 非法: %q/%q", cr.Number, req.SuccessResult, req.FailResult)
			}
			for _, it := range req.PossibleItems {
				if _, ok := c.Item(it.Group, it.Number); !ok {
					return fmt.Errorf("配方 %d 引用未知物品 (%d,%d)", cr.Number, it.Group, it.Number)
				}
			}
		}
		for _, res := range cr.Settings.ResultItems {
			if res.Reference == 0 && !c.hasItemRef(res.Item) {
				return fmt.Errorf("配方 %d 产出无 reference 也无可解析物品", cr.Number)
			}
		}
	}
	return nil
}

// hasItemRef 报告引用非空且能在物品表里解析。
func (c *GameConfig) hasItemRef(r *ItemRef) bool {
	if r == nil {
		return false
	}
	_, ok := c.Item(r.Group, r.Number)
	return ok
}

func validMixResult(v string) bool {
	switch v {
	case MixDisappear, MixStaysAsIs, MixChaosDowngrade, MixWingsDowngrade:
		return true
	}
	return false
}

// HostsCraftings 报告该 NPC 是否挂有任意配方（原版 TalkNpcAction 据此建 BackupInventory）。
func (c *GameConfig) HostsCraftings(npcMonster int) bool {
	for i := range c.Craftings {
		if c.Craftings[i].HostedBy(npcMonster) {
			return true
		}
	}
	return false
}
