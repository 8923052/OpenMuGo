package config

// item_options_test.go —— 物品选项数据面（42_item_options.json，T2-12）载入验收。
//
// 只锁"数据面完整且自洽"（选项类型/定义/等级表/物品引用/组合奖励/套装组），
// 不涉及消费端（装备选项加成进属性系统、掉落时随机生成选项）。

import (
	"strings"
	"testing"
)

// TestItemOptionsLoaded 选项域各段都非空且规模合理（原版初始化结果：14 种选项类型、
// 100+ 个定义、300+ 个物品引用、1000+ 个套装成员条目）。
func TestItemOptionsLoaded(t *testing.T) {
	cfg, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 失败: %v", err)
	}
	if len(cfg.ItemOptionTypes) != 14 {
		t.Fatalf("选项类型数=%d，期望 14", len(cfg.ItemOptionTypes))
	}
	if len(cfg.ItemOptionDefinitions) < 100 {
		t.Fatalf("选项定义数=%d，期望 >= 100", len(cfg.ItemOptionDefinitions))
	}
	if len(cfg.ItemOptionEntries) < 300 {
		t.Fatalf("物品选项引用数=%d，期望 >= 300", len(cfg.ItemOptionEntries))
	}
	if len(cfg.CombinationBonuses) == 0 {
		t.Fatal("组合奖励为空（镶嵌套装包/Fenrir 移速等）")
	}
	setItems := 0
	for i := range cfg.ItemSetGroups {
		setItems += len(cfg.ItemSetGroups[i].Items)
	}
	if setItems < 1000 {
		t.Fatalf("套装成员条目=%d，期望 >= 1000", setItems)
	}
	// 每个定义至少有一条候选选项（空定义没有意义，通常是导出错位）。
	for i := range cfg.ItemOptionDefinitions {
		if len(cfg.ItemOptionDefinitions[i].PossibleOptions) == 0 {
			t.Fatalf("选项定义 %q 没有候选选项", cfg.ItemOptionDefinitions[i].Name)
		}
	}
}

// TestItemOptionLevelTableLoaded 锁定普通选项的等级表：原版 CreateOptionDefinition
// 按 `Level n 的值 = n × baseValue`（baseValue=4）生成 2/3/4 三档，即 8/12/16。
func TestItemOptionLevelTableLoaded(t *testing.T) {
	cfg, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 失败: %v", err)
	}
	want := []float64{8, 12, 16}
	for i := range cfg.ItemOptionDefinitions {
		for j := range cfg.ItemOptionDefinitions[i].PossibleOptions {
			o := &cfg.ItemOptionDefinitions[i].PossibleOptions[j]
			if len(o.LevelDependentOptions) < len(want) {
				continue
			}
			matched := true
			for k, w := range want {
				l := o.LevelDependentOptions[k]
				if l.Level != k+2 || l.PowerUp == nil || l.PowerUp.Boost.Constant != w {
					matched = false
					break
				}
			}
			if matched {
				return
			}
		}
	}
	t.Fatal("未找到普通选项的等级表（Level 2/3/4 = 8/12/16）")
}

// TestItemOptionDefinitionsForWeapon 锁定"物品 → 候选选项定义"引用可用：
// Light Saber(0,10) 是可带幸运/卓越的武器，其候选定义里应含 Luck 类型的选项。
func TestItemOptionDefinitionsForWeapon(t *testing.T) {
	cfg, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 失败: %v", err)
	}
	ids := cfg.ItemOptionDefinitionsFor(0, 10)
	if len(ids) == 0 {
		t.Fatal("Light Saber(0,10) 应有候选选项定义")
	}
	luckTypeID := ""
	for i := range cfg.ItemOptionTypes {
		if strings.Contains(cfg.ItemOptionTypes[i].Name, "Luck") {
			luckTypeID = cfg.ItemOptionTypes[i].ID
			break
		}
	}
	if luckTypeID == "" {
		t.Fatal("未找到 Luck 选项类型")
	}
	for _, id := range ids {
		d, ok := cfg.ItemOptionDefinition(id)
		if !ok {
			t.Fatalf("候选选项定义 %s 不可解析", id)
		}
		for k := range d.PossibleOptions {
			if d.PossibleOptions[k].OptionTypeID == luckTypeID {
				return
			}
		}
	}
	t.Fatal("Light Saber 的候选定义里应含 Luck 选项")
}

// TestItemSetGroupsResolvable 锁定套装组（远古套装依赖它）：成员物品与套装加成定义
// 都能解析，且存在带远古判别值的成员（原版 ItemOfItemSet.AncientSetDiscriminator）。
func TestItemSetGroupsResolvable(t *testing.T) {
	cfg, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 失败: %v", err)
	}
	ancient := 0
	for i := range cfg.ItemSetGroups {
		g := &cfg.ItemSetGroups[i]
		if g.OptionDefinition != "" {
			if _, ok := cfg.ItemOptionDefinition(g.OptionDefinition); !ok {
				t.Fatalf("套装组 %q 的加成定义 %s 不可解析", g.Name, g.OptionDefinition)
			}
		}
		for j := range g.Items {
			it := &g.Items[j]
			if it.AncientSetDiscriminator != 0 {
				ancient++
			}
			if it.Group == nil || it.Number == nil {
				continue
			}
			if _, ok := cfg.Item(*it.Group, *it.Number); !ok {
				t.Fatalf("套装组 %q 的成员物品 (%d,%d) 不存在", g.Name, *it.Group, *it.Number)
			}
		}
	}
	if ancient == 0 {
		t.Fatal("应存在带远古判别值的套装成员")
	}
}
