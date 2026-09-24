package config

import "testing"

// master_test.go —— 大师树导出件的载入与校验（TRIM-09）。
// 断言的数值全部来自 data/season6/96_master_skills.json（tools/goldenconfig 导出），
// 口径与原版对照见 doc/16 TRIM-09。

func mustLoad(t *testing.T) *GameConfig {
	t.Helper()
	cfg, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 失败: %v", err)
	}
	return cfg
}

func TestMasterTreeLoaded(t *testing.T) {
	cfg := mustLoad(t)
	if got := len(cfg.MasterSkills); got != 172 {
		t.Fatalf("大师技能数 %d, want 172", got)
	}
	roots := cfg.MasterRoots()
	if len(roots) != 3 {
		t.Fatalf("树根数 %d, want 3", len(roots))
	}
	wantNames := []string{"Left (Common Skills)", "Middle Root", "Right Root"} // 初始化顺序=下标
	for i, want := range wantNames {
		if roots[i].Name != want {
			t.Fatalf("根 %d 名 %q, want %q", i, roots[i].Name, want)
		}
	}
	// 7 个大师职业各有一张槽位表（Grand Master 43 槽）。
	if got := cfg.Meta.Counts.MasterClasses; got != 7 {
		t.Fatalf("大师职业数 %d, want 7", got)
	}
	if got := len(cfg.masterIndex); got != 7 {
		t.Fatalf("槽位表职业数 %d, want 7", got)
	}
	if got := cfg.MasterSkillIndex(3, 300); got != 1 {
		t.Fatalf("暗骑士(3) 技能 300 槽位 %d, want 1", got)
	}
	// 中根起点 37（原版注释：每根 36 槽，左 1 / 中 37 / 右 73）。
	if got := cfg.MasterSkillIndex(3, 325); got != 37 {
		t.Fatalf("暗骑士(3) 技能 325 槽位 %d, want 37", got)
	}
	// 非大师技 / 未知职业都是 0（原版 GetMasterSkillIndex 的回落）。
	if got := cfg.MasterSkillIndex(3, 1); got != 0 {
		t.Fatalf("普通技能 1 不该有槽位, got %d", got)
	}
	if got := cfg.MasterSkillIndex(0, 300); got != 0 {
		t.Fatalf("非大师职业 0 不该有槽位, got %d", got)
	}
}

func TestMasterSkillFields(t *testing.T) {
	cfg := mustLoad(t)
	// 通用左根第一条：Durability Reduction (1)，被动、乘算、上限 20、首点 1。
	m, ok := cfg.MasterSkillByNumber(300)
	if !ok {
		t.Fatal("技能 300 不在大师表内")
	}
	if m.Name != "Durability Reduction (1)" || !m.PassiveBoost || m.Rank != 1 || m.Root == nil || *m.Root != 0 {
		t.Fatalf("300 关键字段不符: %+v", m)
	}
	if m.MaximumLevel != 20 || m.InitialPoints != 1 || m.TargetAttribute != "Item Duration Increase" ||
		m.Aggregation != "Multiplicate" {
		t.Fatalf("300 数值口径不符: %+v", m)
	}
	if got := m.ValueAt(0); got != 0 {
		t.Fatalf("等级 0 的取值应为 0（原版 GetValue 越界口径）, got %v", got)
	}
	if got := m.ValueAt(21); got != 0 {
		t.Fatalf("超过上限的取值应为 0, got %v", got)
	}
	if got := m.ValueAt(1); got != m.DisplayAt(1) {
		t.Fatalf("300 的 value 与 display 公式同源（AddPassive 重载只传一条），应相等: %v vs %v",
			got, m.DisplayAt(1))
	}

	// 418 Triple Shot Mastery：唯二"上限 10 且首点 10"的技能（原版 MinimumLevel=MaximumLevel
	// 的特例，一点直接到 10 级），公式 10 级前恒 0。
	tm, ok := cfg.MasterSkillByNumber(418)
	if !ok {
		t.Fatal("技能 418 不在大师表内")
	}
	if tm.InitialPoints != 10 || tm.MaximumLevel != 10 {
		t.Fatalf("418 首点/上限 %d/%d, want 10/10", tm.InitialPoints, tm.MaximumLevel)
	}
	if got := tm.ValueAt(9); got != 0 {
		t.Fatalf("418 在 9 级的值应为 0, got %v", got)
	}
	if got := tm.ValueAt(10); got != 1 {
		t.Fatalf("418 在 10 级的值应为 1, got %v", got)
	}
	// NextDisplayValue 钳到上限（原版 CalculateNextDisplayValue：min(level+1, MaximumLevel)）。
	if got := tm.NextDisplayValue(10); got != tm.DisplayAt(10) {
		t.Fatalf("满级的 next display 应钳到上限值, got %v want %v", got, tm.DisplayAt(10))
	}

	// 技能号必须同时是已知技能（30_skills.json）。
	for i := range cfg.MasterSkills {
		ms := &cfg.MasterSkills[i]
		if _, ok := cfg.Skill(ms.Number); !ok {
			t.Fatalf("大师技能 %d(%s) 在技能表里不存在", ms.Number, ms.Name)
		}
	}
	// 被动/主动数量（原版 102 passive + 70 active，且 70 条都有被替换技能）。
	var passive, withReplaced int
	for i := range cfg.MasterSkills {
		if cfg.MasterSkills[i].PassiveBoost {
			passive++
		}
		if cfg.MasterSkills[i].ReplacedSkill != nil {
			withReplaced++
		}
	}
	if passive != 102 || withReplaced != 70 {
		t.Fatalf("被动 %d / 带替换 %d, want 102/70", passive, withReplaced)
	}
}

// TestValidateMasterCatches 钉住校验会拦下的错位（手搭配置，不依赖导出件）。
func TestValidateMasterCatches(t *testing.T) {
	root0, badRoot := 0, 9
	cases := []struct {
		name  string
		cfg   *GameConfig
		wants string
	}{
		{"根下标越界",
			&GameConfig{MasterSkillTreeRoots: []MasterRoot{{Index: 0}},
				MasterSkills: []MasterSkill{{Number: 300, Root: &badRoot, MaximumLevel: 1,
					InitialPoints: 1, ValueAtLevel: []float32{1}, DisplayAtLevel: []float32{1}}}},
			"根下标"},
		{"数值数组与上限不符",
			&GameConfig{MasterSkillTreeRoots: []MasterRoot{{Index: 0}},
				MasterSkills: []MasterSkill{{Number: 300, Root: &root0, MaximumLevel: 20,
					InitialPoints: 1, ValueAtLevel: []float32{1}, DisplayAtLevel: []float32{1}}}},
			"数值数组长度"},
		{"首点超过上限",
			&GameConfig{MasterSkillTreeRoots: []MasterRoot{{Index: 0}},
				MasterSkills: []MasterSkill{{Number: 300, Root: &root0, MaximumLevel: 1,
					InitialPoints: 2, ValueAtLevel: []float32{1}, DisplayAtLevel: []float32{1}}}},
			"首点花费"},
		{"meta 声明但文件缺失",
			&GameConfig{Meta: withMasterCount(172)},
			"未载入"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.masterSkillByNum = map[int]*MasterSkill{}
			for i := range cfg.MasterSkills {
				cfg.masterSkillByNum[cfg.MasterSkills[i].Number] = &cfg.MasterSkills[i]
			}
			err := cfg.validateMaster()
			if err == nil {
				t.Fatal("应校验失败但通过了")
			}
			if !contains(err.Error(), tc.wants) {
				t.Fatalf("错误 %q 应含 %q", err.Error(), tc.wants)
			}
		})
	}
}

// withMasterCount 造一份只声明大师技能数的 meta（其余计数为 0，不参与本用例）。
func withMasterCount(n int) Meta {
	m := Meta{}
	m.Counts.MasterSkills = n
	return m
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
