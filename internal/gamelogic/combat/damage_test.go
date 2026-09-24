package combat

// damage_test.go —— CalculateDamageAsync 完整 PvM 管线的确定性回归测试。
// 每个用例把命中/暴击/卓越/破防/双倍等概率设为 0 或 1，使掷骰结果与随机种子无关
// （rollChance(p<=0) 不消耗 rng、p=1 恒真；randInt(min==max) 不消耗 rng），
// 从而伤害值可精确断言。

import (
	"testing"

	"mugo/internal/util"
)

func fixedRng() *util.Rand { return util.NewRand(0x1234) }

// baseAttacker 构造一个"必命中、无暴击/卓越/破防/双倍"的物理攻方，伤害下限不触发。
func baseAttacker() AttackerStats {
	return AttackerStats{Level: 100, MinPhys: 50, MaxPhys: 50, AttackRate: 1000}
}

func TestCalculatePhysicalNormal(t *testing.T) {
	res := Calculate(fixedRng(), Match{}, baseAttacker(), DefenderStats{}, Skill{})
	if res.Miss || res.Damage != 50 || res.Kind != KindNormal {
		t.Fatalf("普通物理应 50 点普通伤害，got %+v", res)
	}
}

func TestCalculateDefenseSubtract(t *testing.T) {
	res := Calculate(fixedRng(), Match{}, baseAttacker(), DefenderStats{Defense: 20}, Skill{})
	if res.Damage != 30 {
		t.Fatalf("防御 20 应扣成 30，got %d", res.Damage)
	}
}

func TestCalculateDefenseDecrement(t *testing.T) {
	// defense = (20) * 0.5 = 10 → dmg 40。
	res := Calculate(fixedRng(), Match{}, baseAttacker(), DefenderStats{Defense: 20, DefenseDecrement: 0.5}, Skill{})
	if res.Damage != 40 {
		t.Fatalf("DefenseDecrement 0.5 → 防御 10，伤害应 40，got %d", res.Damage)
	}
}

func TestCalculateArmorDamageDecrease(t *testing.T) {
	// dmg 50，护甲减伤 0.4 → 50 - int(50*0.4)=50-20=30。
	res := Calculate(fixedRng(), Match{}, baseAttacker(), DefenderStats{ArmorDamageDecrease: 0.4}, Skill{})
	if res.Damage != 30 {
		t.Fatalf("ArmorDamageDecrease 0.4 → 伤害 30，got %d", res.Damage)
	}
}

func TestCalculateDamageReceiveDecrement(t *testing.T) {
	// dmg 50，受伤减免 0.5 → int(50*0.5)=25。
	res := Calculate(fixedRng(), Match{}, baseAttacker(), DefenderStats{DamageReceiveDecrement: 0.5}, Skill{})
	if res.Damage != 25 {
		t.Fatalf("DamageReceive 0.5 → 伤害 25，got %d", res.Damage)
	}
}

func TestCalculateMinLevelFloor(t *testing.T) {
	// 防御压制到负，minLevelDmg=max(1,level/10)=3。
	a := baseAttacker()
	a.Level = 30
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{Defense: 99999, DefenseRate: 0}, Skill{})
	if res.Damage != 3 {
		t.Fatalf("minLevelDmg 应为 3，got %d", res.Damage)
	}
}

func TestCalculateCritical(t *testing.T) {
	a := baseAttacker()
	a.MaxPhys = 100
	a.MinPhys = 50
	a.CriticalChance = 1
	a.CriticalBonus = 10
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	if res.Kind != KindCritical || res.Damage != 110 {
		t.Fatalf("暴击应 110 且 Kind=Critical，got %+v", res)
	}
}

func TestCalculateExcellentPhysical(t *testing.T) {
	a := baseAttacker()
	a.MaxPhys = 100
	a.MinPhys = 50
	a.ExcellentChance = 1
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	// int(100*1.2 + 0) + critBonus(0) = 120。
	if res.Kind != KindExcellent || res.Damage != 120 {
		t.Fatalf("卓越物理应 120，got %+v", res)
	}
}

func TestCalculateIgnoreDefense(t *testing.T) {
	a := baseAttacker()
	a.DefenseIgnoreChance = 1
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{Defense: 99999}, Skill{})
	if res.Kind != KindIgnoreDefense || res.Damage != 50 {
		t.Fatalf("破防应无视防御得 50 且 Kind=IgnoreDefense，got %+v", res)
	}
}

func TestCalculateDoubleDamage(t *testing.T) {
	a := baseAttacker()
	a.DoubleDamageChance = 1
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	if res.Damage != 100 {
		t.Fatalf("双倍伤害应 100，got %d", res.Damage)
	}
}

func TestCalculateAttackDamageIncrease(t *testing.T) {
	a := baseAttacker()
	a.AttackDamageIncrease = 1.5 // 小恶魔等
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	if res.Damage != 75 {
		t.Fatalf("增伤 1.5 → 75，got %d", res.Damage)
	}
}

func TestCalculateWizardrySkillAppliesWizIncrease(t *testing.T) {
	a := baseAttacker()
	a.MinWiz, a.MaxWiz = 40, 40
	a.WizardryIncrease = 1.2
	sk := Skill{Used: true, DamageType: TypeWizardry, ExtraMin: 0, ExtraMax: 0}
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, sk)
	// base = int(40*1.2)=48; normal rand(48,48)=48; 减防 0；无技能倍率(1)。
	if res.Damage != 48 {
		t.Fatalf("魔法技能应含 WizardryIncrease 得 48，got %d", res.Damage)
	}
}

func TestCalculateSkillMultiplier(t *testing.T) {
	a := baseAttacker()
	a.MinPhys, a.MaxPhys = 0, 0
	a.SkillDamageMultiplier = 2 // DK 基值
	sk := Skill{Used: true, DamageType: TypePhysical, ExtraMin: 50, ExtraMax: 50}
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, sk)
	// base = 0+50=50; 无暴击卓越 rand(50,50)=50; 减防0; floor(level100→10)不触发;
	// 技能倍率 2 → 100。
	if res.Damage != 100 {
		t.Fatalf("技能倍率 2 → 100，got %d", res.Damage)
	}
}

func TestCalculateDinorantBonus(t *testing.T) {
	a := baseAttacker()
	a.IsDinorant = true
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	if res.Damage != 65 { // 50*1.3
		t.Fatalf("Dinorant 普攻 ×1.3 → 65，got %d", res.Damage)
	}
}

func TestCalculateRavenDividesBy1_5(t *testing.T) {
	a := baseAttacker()
	a.IsRaven = true
	a.MinPhys, a.MaxPhys = 150, 150
	res := Calculate(fixedRng(), Match{}, a, DefenderStats{}, Skill{})
	// 非暴非卓越 → int(150/1.5)=100。
	if res.Damage != 100 {
		t.Fatalf("渡鸦非暴/卓越应 ÷1.5 → 100，got %d", res.Damage)
	}
}

func TestCalculateMissStatistical(t *testing.T) {
	// 防守率远压攻击率 → 命中率 0.03，多次应出现大量 MISS。
	rng := fixedRng()
	misses := 0
	for i := 0; i < 200; i++ {
		if Calculate(rng, Match{}, baseAttacker(), DefenderStats{DefenseRate: 1e9}, Skill{}).Miss {
			misses++
		}
	}
	if misses < 100 {
		t.Fatalf("防守压制下应大量 MISS，got %d/200", misses)
	}
}
