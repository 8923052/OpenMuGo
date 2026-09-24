package combat

// damage_pvp_test.go —— TRIM-06 伤害主干里"PvP 选型 / 大师开关板 / 连击 / Soul Barrier
// / 非物理运算次序"这几段的确定性回归。
//
// 造数手法与 damage_test.go 相同：概率设 0 或 1、基伤区间上下限相等，使除命中判定外
// 没有掷骰消耗。唯一例外是 TestCalculateOverratesOnlyPvM：Overrates 分支要求
// 守方防御速率 ≥ 攻方攻击速率，此时命中概率按原版只剩 3% 下限，故用固定种子 35
// （其首个 NextDouble=0.01112927 < 0.03，命中必成立）来稳定这一腿。

import (
	"testing"

	"mugo/internal/util"
)

func TestHitChanceSelectsRatesPerMatchType(t *testing.T) {
	atk := AttackerStats{AttackRate: 100, AttackRatePvP: 50}
	def := DefenderStats{DefenseRate: 0, DefenseRatePvP: 25}
	// PvM 用 Attack Rate / Defense Rate（PvM 那条由调用方并入 Increase Block Bonus）。
	if got := HitChance(false, atk, def); got != 1.0 {
		t.Fatalf("PvM 命中率应 1.0，got %v", got)
	}
	// PvP 换用 PvP 那一对：1 - 25/50 = 0.5。
	if got := HitChance(true, atk, def); got != 0.5 {
		t.Fatalf("PvP 命中率应 0.5，got %v", got)
	}
	// 守方速率不低时落在 3% 下限（原版两分支共用同一 minChance）。
	if got := HitChance(true, atk, DefenderStats{DefenseRatePvP: 500}); got != 0.03 {
		t.Fatalf("PvP 下限应 0.03，got %v", got)
	}
}

func TestCalculatePvpUsesDefensePvP(t *testing.T) {
	atk := AttackerStats{Level: 20, MinPhys: 50, MaxPhys: 50, AttackRate: 1000, AttackRatePvP: 1000}
	def := DefenderStats{Defense: 40, DefensePvP: 10}
	if got := Calculate(fixedRng(), Match{}, atk, def, Skill{}); got.Damage != 10 {
		t.Fatalf("PvM 应读 Defense(40) → 10，got %+v", got)
	}
	if got := Calculate(fixedRng(), Match{IsPvP: true}, atk, def, Skill{}); got.Damage != 40 {
		t.Fatalf("PvP 应读 Defense (PvP)(10) → 40，got %+v", got)
	}
}

func TestCalculateOverratesOnlyPvM(t *testing.T) {
	// 守方防御速率高于攻方攻击速率 → PvM ×0.3（:205-208），PvP 不检查这一条。
	atk := AttackerStats{Level: 200, MinPhys: 100, MaxPhys: 100, AttackRate: 10, AttackRatePvP: 1000}
	def := DefenderStats{DefenseRate: 20}
	rng := util.NewRand(35)
	if got := Calculate(rng, Match{}, atk, def, Skill{}); got.Damage != 30 {
		t.Fatalf("PvM Overrates 应把 100 打成 30，got %+v", got)
	}
	if got := Calculate(fixedRng(), Match{IsPvP: true}, atk, def, Skill{}); got.Damage != 100 {
		t.Fatalf("PvP 不应有 Overrates 减伤，期望 100，got %+v", got)
	}
}

func TestCalculatePhysicalAndNonPhysicalExcellentOrder(t *testing.T) {
	def := DefenderStats{Defense: 40}
	// 物理卓越：先 ×1.2 再减防 → int(100*1.2) - 40 = 80。
	phys := AttackerStats{Level: 10, MinPhys: 100, MaxPhys: 100, AttackRate: 1000, ExcellentChance: 1}
	if got := Calculate(fixedRng(), Match{}, phys, def, Skill{}); got.Damage != 80 || got.Kind != KindExcellent {
		t.Fatalf("物理卓越应 80/excellent，got %+v", got)
	}
	// 非物理卓越：先减防再 ×1.2（:160-166 的顺序与物理不同）→ int(int(100)-40)*1.2 = 72。
	wiz := AttackerStats{Level: 10, MinWiz: 100, MaxWiz: 100, AttackRate: 1000, ExcellentChance: 1}
	sk := Skill{Used: true, DamageType: TypeWizardry}
	if got := Calculate(fixedRng(), Match{}, wiz, def, sk); got.Damage != 72 || got.Kind != KindExcellent {
		t.Fatalf("巫术卓越应先减防后 ×1.2 = 72，got %+v", got)
	}
}

func TestCalculateSkillFinalBonusBeforeMultiplier(t *testing.T) {
	// 原版 :232 加 SkillFinalDamageBonus，:247 才乘倍率 → (100+10)*2 = 220，而不是 100*2+10。
	atk := AttackerStats{Level: 10, MinPhys: 100, MaxPhys: 100, AttackRate: 1000}
	sk := Skill{Used: true, DamageType: TypePhysical, FinalBonus: 10, FinalMultiplier: 2}
	if got := Calculate(fixedRng(), Match{}, atk, DefenderStats{}, sk); got.Damage != 220 {
		t.Fatalf("FinalBonus 应在倍率之前加，期望 220，got %+v", got)
	}
}

func TestCalculateDragonSlasherTripleOnlyPvM(t *testing.T) {
	atk := AttackerStats{Level: 10, MinPhys: 50, MaxPhys: 50, AttackRate: 1000, AttackRatePvP: 1000}
	sk := Skill{Used: true, DamageType: TypePhysical, FinalMultiplier: 2, Number: skillDragonSlasher}
	if got := Calculate(fixedRng(), Match{}, atk, DefenderStats{}, sk); got.Damage != 300 {
		t.Fatalf("屠龙者对怪 ×3 应 50*2*3=300，got %+v", got)
	}
	if got := Calculate(fixedRng(), Match{IsPvP: true}, atk, DefenderStats{}, sk); got.Damage != 100 {
		t.Fatalf("屠龙者 PvP 不 ×3，应 50*2=100，got %+v", got)
	}
}

func TestCalculateFinalDamageBonusUnconditional(t *testing.T) {
	// 被防御压到负值时仍走 1 点下限，随后 FinalDamageBonus 无门直接加（:268）。
	atk := AttackerStats{Level: 10, MinPhys: 50, MaxPhys: 50, AttackRate: 1000}
	def := DefenderStats{Defense: 100}
	if got := Calculate(fixedRng(), Match{}, atk, def, Skill{}); got.Damage != 1 {
		t.Fatalf("无 FinalDamageBonus 时应停在 1，got %+v", got)
	}
	atk.FinalDamageBonus = 7
	if got := Calculate(fixedRng(), Match{}, atk, def, Skill{}); got.Damage != 8 {
		t.Fatalf("FinalDamageBonus 7 应加在 1 点下限之后 = 8，got %+v", got)
	}
}

func TestCalculateBerserkerAndProficiency(t *testing.T) {
	// 狂暴给物理上下限（:108/113/118-119）。
	phys := AttackerStats{Level: 10, MinPhys: 50, MaxPhys: 50, AttackRate: 1000,
		BerserkerMinPhys: 20, BerserkerMaxPhys: 20}
	if got := Calculate(fixedRng(), Match{}, phys, DefenderStats{}, Skill{}); got.Damage != 70 {
		t.Fatalf("狂暴应把物理基伤抬到 70，got %+v", got)
	}
	// 精通乘区只给巫术/诅咒（:191）。
	wiz := AttackerStats{Level: 10, MinWiz: 100, MaxWiz: 100, AttackRate: 1000, BerserkerProficiency: 0.5}
	sk := Skill{Used: true, DamageType: TypeWizardry}
	if got := Calculate(fixedRng(), Match{}, wiz, DefenderStats{}, sk); got.Damage != 150 {
		t.Fatalf("巫术精通 ×1.5 应 150，got %+v", got)
	}
	// 弱点减免只给物理（:195）。
	weak := AttackerStats{Level: 10, MinPhys: 100, MaxPhys: 100, AttackRate: 1000, WeaknessPhysDecrement: 0.3}
	if got := Calculate(fixedRng(), Match{}, weak, DefenderStats{}, Skill{}); got.Damage != 70 {
		t.Fatalf("弱点应把物理打成 70，got %+v", got)
	}
	if got := Calculate(fixedRng(), Match{}, AttackerStats{Level: 10, MinWiz: 100, MaxWiz: 100,
		AttackRate: 1000, WeaknessPhysDecrement: 0.3}, DefenderStats{}, sk); got.Damage != 100 {
		t.Fatalf("弱点不该影响巫术，got %+v", got)
	}
}

func TestCalculateMasterPhysicalBoardStages(t *testing.T) {
	// 前置段在减防之前、且落在双持翻倍之内（:131-138）；后置段在 :196 之后。
	atk := AttackerStats{Level: 10, MinPhys: 100, MaxPhys: 100, AttackRate: 1000,
		MasterPhysPreBuffs: 10, MasterPhysPostBuffs: 10, HasDoubleWield: true}
	got := Calculate(fixedRng(), Match{}, atk, DefenderStats{}, Skill{})
	if got.Damage != 230 {
		// (100+10)*2 - 0 + 10 = 230；若前置段被错放到翻倍之后则是 220。
		t.Fatalf("大师前置加成应随双持翻倍 → 230，got %+v", got)
	}
}

func TestCalculateSoulBarrierClampAndToll(t *testing.T) {
	atk := AttackerStats{Level: 10, MinPhys: 100, MaxPhys: 100, AttackRate: 1000}
	// 抵扣量钳在 0..0.9（原版 Math.Clamp），够付法力费才生效。
	cases := []struct {
		name string
		def  DefenderStats
		want int
	}{
		{"钳到0.9", DefenderStats{SoulBarrierReduction: 0.95, SoulBarrierManaToll: 10, CurrentMana: 100}, 10},
		{"法力不足不抵扣", DefenderStats{SoulBarrierReduction: 0.5, SoulBarrierManaToll: 10, CurrentMana: 5}, 100},
		{"无费不抵扣", DefenderStats{SoulBarrierReduction: 0.5}, 100},
		{"正常抵扣", DefenderStats{SoulBarrierReduction: 0.25, SoulBarrierManaToll: 10, CurrentMana: 100}, 75},
	}
	for _, tc := range cases {
		if got := Calculate(fixedRng(), Match{}, atk, tc.def, Skill{}); got.Damage != tc.want {
			t.Fatalf("%s：期望 %d，got %+v", tc.name, tc.want, got)
		}
	}
}

func TestCalculatePvPEndBlockAndCombo(t *testing.T) {
	atk := AttackerStats{Level: 10, MinPhys: 300, MaxPhys: 300, AttackRate: 1000, AttackRatePvP: 1000,
		FinalDamageIncreasePvP: 50, MasterMasteryPvP: 50, ComboBonus: 250}
	// PvP 末段：FinalDamageIncrease(PvP) + 大师精通 PvP 板（:270-280）。
	if got := Calculate(fixedRng(), Match{IsPvP: true}, atk, DefenderStats{}, Skill{}); got.Damage != 400 {
		t.Fatalf("PvP 末段应 +100 → 400，got %+v", got)
	}
	// PvM 不加这两条。
	if got := Calculate(fixedRng(), Match{}, atk, DefenderStats{}, Skill{}); got.Damage != 300 {
		t.Fatalf("PvM 不该吃 PvP 末段，期望 300，got %+v", got)
	}
	// 渡鸦（AttackerSurrogate）排除在 PvP 末段之外，且非暴非卓越 ÷1.5（:244-252、:270）。
	raven := atk
	raven.IsRaven = true
	if got := Calculate(fixedRng(), Match{IsPvP: true}, raven, DefenderStats{}, Skill{}); got.Damage != 200 {
		t.Fatalf("渡鸦应 300/1.5=200 且不吃 PvP 末段，got %+v", got)
	}
	// 连击终结加成只在 isCombo 且 dmg>0 时（:282-290）。
	if got := Calculate(fixedRng(), Match{IsCombo: true}, atk, DefenderStats{}, Skill{}); got.Damage != 550 {
		t.Fatalf("连击终结应 300+250=550，got %+v", got)
	}
	if got := Calculate(fixedRng(), Match{}, atk, DefenderStats{}, Skill{}); got.Damage != 300 {
		t.Fatalf("非终结击不该加 ComboBonus，got %+v", got)
	}
}
