package drops

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/util"
)

// TestPossibleListNotInverted 守住 GetPossibleList 的过滤方向。
//
// 原版条件是**保留**语义（DefaultDropGenerator.GetPossibleList）：
//
//	where CanDropAtMonsterLevel(it, monsterLevel)
//	      && (it.DropLevel > monsterLevel - DropLevelMaxGap)
//
// 即只排除"掉落等级远低于怪级（≤ 怪级-12）"的廉价物品。
// 若误写成"跳过"，怪级 < 12 时 `DropLevel > 负数` 恒成立 → 全部物品被跳过 →
// possibleList 恒空 → RandomItem 组一件都掉不出来（实测问题"怪物死亡不掉物品"的
// 独立根因之一，且与加载顺序 bug 叠加）。
func TestPossibleListNotInverted(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	// Bull Fighter：Lorencia 可击杀怪，lvl 6，maxdrops 1。
	mon, ok := cfg.Monster(0)
	if !ok {
		t.Fatal("怪物 0 不存在")
	}
	lvl := monsterLevelOf(mon)
	if lvl <= 0 {
		t.Fatalf("怪级解析异常: %d", lvl)
	}

	g := NewGenerator(cfg, util.NewRand(1))
	possible := g.possibleList(lvl, false)
	if len(possible) == 0 {
		t.Fatalf("possibleList(%d) 为空——过滤方向可能取反，或数据未正确加载", lvl)
	}

	for _, def := range possible {
		if !def.DropsFromMonsters {
			t.Fatalf("(%d,%d) 不应在可掉清单（DropsFromMonsters=false）", def.Group, def.Number)
		}
		if !canDropAtMonsterLevel(def, lvl) {
			t.Fatalf("(%d,%d) DropLevel=%d 在怪级 %d 不可掉", def.Group, def.Number, def.DropLevel, lvl)
		}
		// 保留条件是 DropLevel > 怪级 - DropLevelMaxGap。
		if def.DropLevel <= lvl-dropLevelMaxGap {
			t.Fatalf("(%d,%d) DropLevel=%d 低于 怪级-%d，应被排除", def.Group, def.Number, def.DropLevel, dropLevelMaxGap)
		}
	}

	// 低怪级下可掉清单应几乎覆盖全部 monster-drop 物品（> -6 恒真）；
	// 高怪级下才应显著收窄。以此确认过滤是"高阶收窄"而非"低阶清空"。
	high := g.possibleList(255, false)
	if len(high) >= len(possible) {
		t.Fatalf("怪级 255 的可掉清单(%d) 应少于怪级 %d 的(%d)", len(high), lvl, len(possible))
	}
}
