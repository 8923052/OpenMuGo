package action

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// master_points_test.go —— 大师加点判定（TRIM-09c），逐条对照 AddMasterPointAction.cs。
// 一律用真实导出件（172 条大师树），技能号旁注原版名字，避免自造夹具。

func loadCfg(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入导出件失败: %v", err)
	}
	return cfg
}

// newMasterChar 造一个已选职业的角色（大师点可指定）。
func newMasterChar(class byte, points int) *entity.Character {
	return &entity.Character{ClassNumber: class, MasterLevelUpPoints: points, Level: 400}
}

// TestAddMasterPointRejectsBeforeLearning 覆盖"进门就被挡下"的四条，全部不得改动角色态。
func TestAddMasterPointRejectsBeforeLearning(t *testing.T) {
	cfg := loadCfg(t)
	cases := []struct {
		name    string
		char    *entity.Character
		skill   uint16
		outcome MasterPointOutcome
	}{
		{"一点都没有", newMasterChar(3, 0), 300, MasterPointNoPoints},
		{"技能号不存在", newMasterChar(3, 5), 9999, MasterPointUnknownSkill},
		{"是普通技能不是大师技", newMasterChar(3, 5), 5, MasterPointNotMasterSkill},
		{"职业不合格（300 不给 Fist Master，它有自己的 578）", newMasterChar(25, 5), 300, MasterPointRejectedRequisition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.char.MasterLevelUpPoints
			res := AddMasterPoint(cfg, tc.char, tc.skill)
			if res.Outcome != tc.outcome {
				t.Fatalf("结果 %s, want %s", res.Outcome, tc.outcome)
			}
			if res.OK() {
				t.Fatal("失败分支不该被当成成功")
			}
			if tc.char.MasterLevelUpPoints != before {
				t.Fatalf("失败分支扣了点: %d → %d", before, tc.char.MasterLevelUpPoints)
			}
			if len(tc.char.LearnedSkills) != 0 {
				t.Fatalf("失败分支写了已学列表: %+v", tc.char.LearnedSkills)
			}
		})
	}
}

// TestAddMasterPointRankGate 钉住 CheckRank：rank2 需要**同一根**上一 rank 任一条 ≥10 级。
func TestAddMasterPointRankGate(t *testing.T) {
	cfg := loadCfg(t)
	// 325 Attack Succ Rate Inc：左根之外的中根 rank1；378 Flame Strengthener：中根 rank2。
	const rank1, rank2 = uint16(325), uint16(378)
	if m, _ := cfg.MasterSkillByNumber(int(rank1)); m.Rank != 1 || m.Root == nil {
		t.Fatalf("夹具假设变了: %+v", m)
	}
	if m, _ := cfg.MasterSkillByNumber(int(rank2)); m.Rank != 2 {
		t.Fatalf("夹具假设变了: %+v", m)
	}
	c := newMasterChar(3, 30)
	// 同时备好 378 的普通技能前置（5=Flame），让唯一失败点落在 rank 门上。
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: rank1, Level: 9}, {SkillNumber: 5}}
	if res := AddMasterPoint(cfg, c, rank2); res.Outcome != MasterPointRejectedRequisition {
		t.Fatalf("上一 rank 只有 9 级时应被拒, got %s", res.Outcome)
	}
	// 到 10 级即放行（原版 MinimumSkillLevelOfRequiredSkill=10，且"任意一条"即可）。
	c.LearnedSkills[0].Level = 10
	if res := AddMasterPoint(cfg, c, rank2); res.Outcome != MasterPointLearned {
		t.Fatalf("上一 rank 有 10 级时应可学, got %s", res.Outcome)
	}
	if lvl, points := learnedLevel(c, rank2), c.MasterLevelUpPoints; lvl != 1 || points != 29 {
		t.Fatalf("首点应花 1 点到 1 级, level=%d points=%d", lvl, points)
	}
}

// learnedLevel 取已学列表里该技能号的等级（没有则 0）。
func learnedLevel(c *entity.Character, number uint16) byte {
	for _, e := range c.LearnedSkills {
		if e.SkillNumber == number {
			return e.Level
		}
	}
	return 0
}

// TestAddMasterPointRequiredSkills 钉住 CheckRequiredSkill：前置要么大师技 ≥10 级，
// 要么以**普通技能**身份持有（378 的前置 5=Flame 就是普通技能）。
func TestAddMasterPointRequiredSkills(t *testing.T) {
	cfg := loadCfg(t)
	c := newMasterChar(3, 30)
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 325, Level: 10}}
	// 378 需要 5（普通技能 Flame）——没有 → 拒。
	if res := AddMasterPoint(cfg, c, 378); res.Outcome != MasterPointRejectedRequisition {
		t.Fatalf("缺普通技能前置时应被拒, got %s", res.Outcome)
	}
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: 5})
	if res := AddMasterPoint(cfg, c, 378); res.Outcome != MasterPointLearned {
		t.Fatalf("持有普通技能前置后应可学, got %s", res.Outcome)
	}

	// 381 Inferno Strengthener 需要 14（普通技能）+ 378（大师技 ≥10 级）。
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: 14})
	if res := AddMasterPoint(cfg, c, 381); res.Outcome != MasterPointRejectedRequisition {
		t.Fatalf("378 只有 1 级时不该放行 rank3, got %s", res.Outcome)
	}
	for i := range c.LearnedSkills {
		if c.LearnedSkills[i].SkillNumber == 378 {
			c.LearnedSkills[i].Level = 10
		}
	}
	if res := AddMasterPoint(cfg, c, 381); res.Outcome != MasterPointLearned {
		t.Fatalf("前置都 ≥10 级后应可学, got %s", res.Outcome)
	}
}

// TestAddMasterPointIncreasePath 钉住"已学者每点 +1 级、到上限后不再加点、点数按花费扣"。
func TestAddMasterPointIncreasePath(t *testing.T) {
	cfg := loadCfg(t)
	c := newMasterChar(3, 6)
	if res := AddMasterPoint(cfg, c, 300); res.Level != 1 || res.Points != 5 {
		t.Fatalf("首点 level=%d points=%d, want 1/5", res.Level, res.Points)
	}
	for i := 0; i < 4; i++ {
		if res := AddMasterPoint(cfg, c, 300); !res.OK() {
			t.Fatalf("第 %d 次加点失败: %s", i+2, res.Outcome)
		}
	}
	// 6 次点击（1 次学习 + 5 次 +1 级）花完 6 点。
	if res := AddMasterPoint(cfg, c, 300); res.Level != 6 || res.Points != 0 {
		t.Fatalf("六次加点后应 6 级 0 点, got %d/%d", res.Level, res.Points)
	}
	// 点数耗尽（<1）→ 外层 no-points 门；再补满点也不超上限。
	c.MasterLevelUpPoints = 100
	for i := 0; i < 20; i++ {
		AddMasterPoint(cfg, c, 300)
	}
	m, _ := cfg.MasterSkillByNumber(300)
	if res := AddMasterPoint(cfg, c, 300); res.Level != byte(m.MaximumLevel) ||
		res.Outcome != MasterPointNotIncreased {
		t.Fatalf("满级后应停在 %d 级并给出 not-increased, got %d/%s",
			m.MaximumLevel, res.Level, res.Outcome)
	}
	// 上限处不再扣点（原版扣费与判级在同一个 if 里）。
	before := c.MasterLevelUpPoints
	AddMasterPoint(cfg, c, 300)
	if c.MasterLevelUpPoints != before {
		t.Fatalf("满级仍扣点: %d → %d", before, c.MasterLevelUpPoints)
	}
}

// TestAddMasterPointInitialCostJumpsToLevel 是 Triple Shot Mastery(418) 那条特例：
// MaximumLevel=10 → MinimumLevel(首点)=10，**一次**点击直接到 10 级。
func TestAddMasterPointInitialCostJumpsToLevel(t *testing.T) {
	cfg := loadCfg(t)
	m, ok := cfg.MasterSkillByNumber(418)
	if !ok || m.InitialPoints != 10 || m.MaximumLevel != 10 {
		t.Fatalf("夹具假设变了: %+v", m)
	}
	// 418 属 High Elf 线（职业 11），前置 414 需 ≥10 级。
	c := newMasterChar(11, 9)
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 325, Level: 10}, {SkillNumber: 414, Level: 10}}
	if res := AddMasterPoint(cfg, c, 418); res.Outcome != MasterPointRejectedRequisition {
		t.Fatalf("只有 9 点时不该成功, got %s", res.Outcome)
	}
	c.MasterLevelUpPoints = 20
	res := AddMasterPoint(cfg, c, 418)
	if res.Outcome != MasterPointLearned || res.Level != 10 || res.Points != 10 {
		t.Fatalf("一次点击应 0→10 级并花 10 点, got %+v", res)
	}
	if got := m.DisplayAt(int(res.Level)); got == 0 {
		t.Fatalf("满级的展示值应非 0（原版 if(level<10,0,1)）")
	}
}

// TestAddMasterPointNilCharacter 对应"未选角色"门（原版 :24-28）。
func TestAddMasterPointNilCharacter(t *testing.T) {
	cfg := loadCfg(t)
	if res := AddMasterPoint(cfg, nil, 300); res.Outcome != MasterPointNoCharacter {
		t.Fatalf("got %s", res.Outcome)
	}
}
