package player

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// master_effects_test.go —— 被动大师技能如何改写角色属性系统（TRIM-09e），对照
// GameLogic/SkillList.cs CreatePowerUpWrappers（:235-259）+ PassiveSkillBoostPowerUp（:280-300）。

func masterCfg(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入导出件失败: %v", err)
	}
	return cfg
}

// TestPassiveMasterSkillRaisesTargetAttribute 用 301（PvP Defence Rate Inc → DefenseRatePvp，
// AddRaw）验证"等级 N 的效果值 = 导出表第 N 项"。
func TestPassiveMasterSkillRaisesTargetAttribute(t *testing.T) {
	cfg := masterCfg(t)
	m, ok := cfg.MasterSkillByNumber(301)
	if !ok || !m.PassiveBoost || m.Aggregation != "AddRaw" || m.TargetAttribute != "Defense Rate (PvP)" {
		t.Fatalf("夹具假设变了: %+v", m)
	}
	c := &entity.Character{ClassNumber: 3, Level: 400, Stats: &entity.CharStats{}}
	before := AttributeValue(cfg, c, m.TargetAttribute)

	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 301, Level: 7}}
	after := AttributeValue(cfg, c, m.TargetAttribute)
	if !almostEqual(after, before+float64(m.ValueAt(7))) {
		t.Fatalf("7 级的效果应加 %v 得到 %v, got %v", m.ValueAt(7), before+float64(m.ValueAt(7)), after)
	}
	// 等级变化即时反映（原版靠 PropertyChanged 重算元素；本仓每次重建系统，效果等价）。
	c.LearnedSkills[0].Level = 8
	if got, want := AttributeValue(cfg, c, m.TargetAttribute), before+float64(m.ValueAt(8)); !almostEqual(got, want) {
		t.Fatalf("升到 8 级后应为 %v, got %v", want, got)
	}
}

// almostEqual 给 float32 聚合留末位余量。
func almostEqual(a, b float64) bool {
	d := a - b
	return d < 1e-4 && d > -1e-4
}

// TestDurabilityReductionFactorExtraElement 钉住那条写死在代码里的附加元素：
// 技能 300/578 除自身目标属性外，还挂 DurabilityReductionFactor = -等级/500。
func TestDurabilityReductionFactorExtraElement(t *testing.T) {
	cfg := masterCfg(t)
	c := &entity.Character{ClassNumber: 3, Level: 400, Stats: &entity.CharStats{}}
	// 该项有职业基值（导出件里 0.1），故一律按**增量**断言。
	base := AttributeValue(cfg, c, desDurabilityReductionFactor)
	for _, number := range []int{300, 578} {
		c.LearnedSkills = nil
		if _, ok := cfg.MasterSkillByNumber(number); !ok {
			t.Fatalf("夹具技能 %d 不在大师表内", number)
		}
		c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: uint16(number), Level: 10}}
		got := AttributeValue(cfg, c, desDurabilityReductionFactor)
		// 属性系统按 float32 聚合，这里比较增量时留出末位余量。
		if !almostEqual(got-base, -10.0/500.0) {
			t.Fatalf("技能 %d 的耐久降低因子增量 = %v, want -0.02", number, got-base)
		}
	}
	// 其它被动大师技不该挂这项。
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 301, Level: 10}}
	if got := AttributeValue(cfg, c, desDurabilityReductionFactor); !almostEqual(got, base) {
		t.Fatalf("301 不该挂耐久因子: %v → %v", base, got)
	}
}

// TestActiveMasterSkillDoesNotAddPassiveElement 说明分层：主动大师技（有 MagicEffect/替换技能）
// 不通过被动通道改属性，它的数值走技能施放侧。
func TestActiveMasterSkillDoesNotAddPassiveElement(t *testing.T) {
	cfg := masterCfg(t)
	c := &entity.Character{ClassNumber: 3, Level: 400, Stats: &entity.CharStats{}}
	// 378 Flame Strengthener：主动（passive_boost=false），带 TargetAttribute 的是强化分支，
	// 这里挑一条**没有** TargetAttribute 的主动技验证"不挂元素"。
	number := 0
	for i := range cfg.MasterSkills {
		m := &cfg.MasterSkills[i]
		if !m.PassiveBoost && m.TargetAttribute == "" {
			number = m.Number
			break
		}
	}
	if number == 0 {
		t.Skip("导出件里没有「主动且无目标属性」的大师技能")
	}
	before := AttributeValue(cfg, c, "Maximum Health")
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: uint16(number), Level: 10}}
	if got := AttributeValue(cfg, c, "Maximum Health"); got != before {
		t.Fatalf("主动大师技不该经被动通道改属性: %v → %v", before, got)
	}
}
