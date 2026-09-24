// combat_muhelper_test.go —— IsMuHelperActive 属性元素：助手运行期间该属性在快照里为 1，
// 停止/未启动为 0（对照 MuHelper.cs 的 AddElement(ConstantElement(1)) / RemoveElement）。
package player

import (
	"testing"

	"mugo/internal/gamelogic/entity"
)

func muHelperChar() *entity.Character {
	return &entity.Character{Name: "idler", ClassNumber: 0, Level: 100,
		Stats: &entity.CharStats{Strength: 20, Agility: 20, Vitality: 20, Energy: 20}}
}

func TestCombatValuesCarryMuHelperFlag(t *testing.T) {
	cfg := loadCfg(t)
	c := muHelperChar()

	off, err := ResolveCombatValuesFlags(cfg, c, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if off.MuHelperActive != 0 {
		t.Fatalf("未开助手时标志应为 0, got %v", off.MuHelperActive)
	}

	on, err := ResolveCombatValuesFlags(cfg, c, false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if on.MuHelperActive != 1 {
		t.Fatalf("助手运行中标志应为 1, got %v", on.MuHelperActive)
	}
	// 证据核对（doc/16 TRIM-10）：原版没有任何导出数据引用 IsMuHelperActive，
	// 因此开/关只改标志本身，不得改动战斗/恢复数值。
	on.MuHelperActive, off.MuHelperActive = 0, 0
	if on != off {
		t.Fatalf("开关注入不应改变其它战斗数值: %+v vs %+v", on, off)
	}
}

func TestMuHelperActiveDefinitionMatchesOriginal(t *testing.T) {
	if StatIsMuHelperActive.Designation != "Is MU Helper active" {
		t.Fatalf("designation 应与 Stats.cs 一致, got %q", StatIsMuHelperActive.Designation)
	}
	if StatIsMuHelperActive.ID != "1FBD3CC0-DFDC-4A19-9B73-5B2DC0E12983" {
		t.Fatalf("属性 GUID 应与 Stats.cs:1479 一致, got %s", StatIsMuHelperActive.ID)
	}
}
