package combat

// multiplier_test.go —— 伤害乘区（CalculateDamageAsync 末尾两乘子）单测。

import "testing"

func TestApplyDamageMultipliers(t *testing.T) {
	cases := []struct {
		name                       string
		dmg                        int
		attackIncrease, defReceive float32
		want                       int
	}{
		{"仅增伤", 100, 1.3, 1, 130},
		{"仅减伤", 100, 1, 0.8, 80},
		{"增伤后减伤", 100, 1.3, 0.8, 104}, // 100*1.3=130 → *0.8=104
		{"缺省倍率按1", 50, 0, 0, 50},
		{"小伤不触发减伤", 1, 1, 0.5, 1}, // dmg 不 >1 → 不乘减伤
		{"边界2触发减伤", 2, 1, 0.5, 1}, // 2*0.5=1
		{"零伤害保持", 0, 1.3, 0.8, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyDamageMultipliers(tc.dmg, tc.attackIncrease, tc.defReceive); got != tc.want {
				t.Fatalf("ApplyDamageMultipliers(%d,%v,%v)=%d, want %d",
					tc.dmg, tc.attackIncrease, tc.defReceive, got, tc.want)
			}
		})
	}
}
