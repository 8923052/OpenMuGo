package drops

// item_options_test.go —— 掉落随机选项的验收（对照原版 DefaultDropGenerator 的
// ApplyRandomOptions / AddRandomExcOptions / ApplyRandomAncientOption / GenerateSpecialItem）。
// 断言"随机结果写回物品位域"，因为位域同时是出站编码与装备加成计算的输入。

import (
	"testing"

	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/util"
)

// TestApplyRandomOptionsWritesBits 锁定幸运与普通选项的随机写回：
// 短剑(0,1) 这两类定义的 AddChance 都是 0.25，重复生成后出现率应各约 1/4；
// 普通选项等级必须落在 1..MaximumItemOptionLevelDrop 内。
func TestApplyRandomOptionsWritesBits(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	def, ok := cfg.Item(0, 1)
	if !ok {
		t.Fatal("短剑 (0,1) 不存在")
	}
	g := NewGenerator(cfg, util.NewRand(20260920))

	const runs = 4000
	var luck, option int
	for i := 0; i < runs; i++ {
		it := &item.Item{Group: 0, Number: 1, Durability: 20}
		g.applyRandomOptions(it, def)
		if it.Luck {
			luck++
		}
		if it.OptionLevel != 0 {
			option++
			if it.OptionLevel < 1 || it.OptionLevel > cfg.MaximumItemOptionLevelDrop {
				t.Fatalf("普通选项等级 %d 超出 1..%d", it.OptionLevel, cfg.MaximumItemOptionLevelDrop)
			}
		}
	}
	for _, c := range []struct {
		name string
		got  int
	}{{"幸运", luck}, {"普通选项", option}} {
		if ratio := float64(c.got) / runs; ratio < 0.20 || ratio > 0.30 {
			t.Fatalf("%s 出现率 %.3f，期望约 0.25", c.name, ratio)
		}
	}
}

// TestAddRandomExcOptionsAlwaysAddsFirst 锁定卓越选项：第 1 条**必加**（原版 i==0 不掷骰），
// 条数不超过卓越定义的 MaximumOptionsPerItem（卓越物攻为 2）。
func TestAddRandomExcOptionsAlwaysAddsFirst(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	def, ok := cfg.Item(0, 1)
	if !ok {
		t.Fatal("短剑 (0,1) 不存在")
	}
	g := NewGenerator(cfg, util.NewRand(7))

	const runs = 2000
	for i := 0; i < runs; i++ {
		it := &item.Item{Group: 0, Number: 1, Durability: 20}
		g.addRandomExcOptions(it, def)
		n := excellentOptionCount(it)
		if n < 1 {
			t.Fatal("卓越第 1 条应无条件附加")
		}
		if n > 2 {
			t.Fatalf("卓越条数 %d 超过 MaximumOptionsPerItem=2", n)
		}
	}
}

// TestApplyRandomAncientOption 锁定远古随机：写入判别值（该物品所属两套远古之一）
// 与额外远古属性的档位（原版 +5/+10）。
func TestApplyRandomAncientOption(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	def, ok := cfg.Item(11, 5)
	if !ok {
		t.Fatal("物品 (11,5) 不存在")
	}
	g := NewGenerator(cfg, util.NewRand(11))

	disc := map[byte]int{}
	for i := 0; i < 200; i++ {
		it := &item.Item{Group: 11, Number: 5, Durability: 20}
		g.applyRandomAncientOption(it, def)
		if it.AncientDiscriminator == 0 {
			t.Fatal("远古套装成员应写入判别值")
		}
		if it.AncientBonusLevel != 1 && it.AncientBonusLevel != 2 {
			t.Fatalf("额外远古属性档位 %d，期望 1 或 2", it.AncientBonusLevel)
		}
		disc[it.AncientDiscriminator]++
	}
	if disc[1] == 0 || disc[2] == 0 {
		t.Fatalf("(11,5) 属于两套远古（判别值 1/2），两者都应出现: %v", disc)
	}
}

// TestGenerateSpecialItem 锁定特殊掉落组类型（Ancient/Excellent/SocketItem）都能产出
// 带对应位域的物品——此前这三类一律返回 nil。
func TestGenerateSpecialItem(t *testing.T) {
	cfg, _ := dropTestCfg(t)
	g := NewGenerator(cfg, util.NewRand(2026))

	// Excellent / SocketItem 走"按怪级取可掉清单"，而清单条件（DropLevel 落在
	// (怪级-12, 怪级]）窗口很窄，任一固定怪级都可能为空——故扫描一个能产出物品的怪级。
	find := func(itemType string) *item.Item {
		for lvl := 1; lvl <= 255; lvl++ {
			if it := g.generateSpecialItem(lvl, itemType); it != nil {
				return it
			}
		}
		return nil
	}

	if it := find("Excellent"); it == nil {
		t.Fatal("任何怪级都产不出卓越物品（卓越下探差或可掉清单有问题）")
	} else if it.ExcellentBits == 0 {
		t.Fatal("卓越物品应带卓越位")
	}
	if it := find("SocketItem"); it == nil {
		t.Fatal("任何怪级都产不出镶嵌物品（MaximumSockets 过滤有问题）")
	} else if it.SocketCount == 0 {
		t.Fatal("镶嵌物品应有孔数")
	}
	// 远古不按怪级取清单（原版 GenerateRandomAncient 用 _ancientItems）。
	if it := g.generateSpecialItem(255, "Ancient"); it == nil {
		t.Fatal("Ancient 组应产出物品")
	} else if it.AncientDiscriminator == 0 {
		t.Fatal("远古物品应带判别值")
	}
}
