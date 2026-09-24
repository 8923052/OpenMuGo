package config

// jewel_mix.go —— 宝石升档/降档映射（98_jewel_mixes.json，对应原版
// DataModel/Configuration/JewelMix.cs 与 GameConfiguration.JewelMixes）。
// 这份表是 C1 BC LahapJewelMixRequest 里 ItemType 字段的解释表：0=Bless … 9=Higher Refine。

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// JewelMixItem 是一个宝石的定义坐标 (group, number)。
type JewelMixItem struct {
	Group  int `json:"group"`
	Number int `json:"number"`
}

// JewelMix 是一条"若干单宝石 ↔ 一个打包宝石"的映射（原版 JewelMix）。
type JewelMix struct {
	// Number 是客户端用的 mix 序号（原版 JewelMix.Number，0..9）。
	Number int `json:"number"`
	// Single 是散宝石定义（原版 SingleJewel）。
	Single JewelMixItem `json:"single"`
	// Mixed 是打包宝石定义（原版 MixedJewel，等级 0/1/2 = 10/20/30 个）。
	Mixed JewelMixItem `json:"mixed"`
}

// loadJewelMixes 读 98_jewel_mixes.json；缺文件容忍为空（旧导出件）。
func (c *GameConfig) loadJewelMixes(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "98_jewel_mixes.json")
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(raw, &c.JewelMixes); err != nil {
		return fmt.Errorf("config: 解析 98_jewel_mixes.json: %w", err)
	}
	return nil
}

// JewelMixByNumber 按客户端 mix 序号取映射（对照 Configuration.JewelMixes
// .FirstOrDefault(m => m.Number == mixId)，找不到时调用方按原版只记警告）。
func (c *GameConfig) JewelMixByNumber(number int) (*JewelMix, bool) {
	for i := range c.JewelMixes {
		if c.JewelMixes[i].Number == number {
			return &c.JewelMixes[i], true
		}
	}
	return nil, false
}

// JewelMixOfMixed 按"打包宝石定义"反查映射（拆分时校验目标件是否该 mix 的产物）。
func (c *GameConfig) JewelMixOfMixed(group, number int) (*JewelMix, bool) {
	for i := range c.JewelMixes {
		if m := c.JewelMixes[i].Mixed; m.Group == group && m.Number == number {
			return &c.JewelMixes[i], true
		}
	}
	return nil, false
}
