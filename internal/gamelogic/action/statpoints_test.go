package action

// statpoints_test.go —— T2-8 加点判定分支（对照 IncreaseStatsAction +
// CharacterExtensions.CanIncreaseStats）。

import "testing"

func TestIncreaseStatOK(t *testing.T) {
	res := IncreaseStat(StatAllocation{Stat: StatStrength, Amount: 1, LevelUpPoints: 5, BaseValue: 100})
	if !res.OK || res.NewBaseValue != 101 || res.NewLevelUpPoints != 4 {
		t.Fatalf("res=%+v", res)
	}
}

func TestIncreaseStatNotEnoughPoints(t *testing.T) {
	res := IncreaseStat(StatAllocation{Stat: StatEnergy, Amount: 1, LevelUpPoints: 0, BaseValue: 50})
	if res.OK || !res.NotEnoughPoints {
		t.Fatalf("res=%+v", res)
	}
}

func TestIncreaseStatUnknownStat(t *testing.T) {
	res := IncreaseStat(StatAllocation{Stat: StatType(9), Amount: 1, LevelUpPoints: 5, BaseValue: 50})
	if res.OK || !res.UnknownStat {
		t.Fatalf("res=%+v", res)
	}
}

func TestIncreaseStatZeroAmount(t *testing.T) {
	// 原版 ArgumentOutOfRangeException（客户端不可达）——防御性拒绝。
	res := IncreaseStat(StatAllocation{Stat: StatVitality, Amount: 0, LevelUpPoints: 5, BaseValue: 50})
	if res.OK {
		t.Fatalf("res=%+v", res)
	}
}

func TestIncreaseStatOverflowGuard(t *testing.T) {
	res := IncreaseStat(StatAllocation{Stat: StatAgility, Amount: 1, LevelUpPoints: 5, BaseValue: 32767})
	if res.OK || !res.UnknownStat {
		t.Fatalf("u16/显示上限溢出应拒绝: %+v", res)
	}
}

func TestStatBaseValueMapping(t *testing.T) {
	st := StatType(StatLeadership).StatBaseValue(1, 2, 3, 4, 5)
	if st != 5 {
		t.Fatalf("leadership = %d, want 5", st)
	}
	if StatType(StatVitality).StatBaseValue(1, 2, 3, 4, 5) != 3 {
		t.Fatal("vitality 映射错误")
	}
}
