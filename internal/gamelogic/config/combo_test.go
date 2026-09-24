package config

// combo_test.go —— 连击导出件的载入、继承链解析与校验（TRIM-06b）。
// 数据口径来自 data/season6/10_character_classes.json（tools/goldenconfig 自
// VersionSeasonSix/SkillsInitializer.cs:572-605 CreateSkillCombos 导出）。

import "testing"

func TestComboOwnedByBladeKnightOnly(t *testing.T) {
	cfg := mustLoad(t)
	own := cfg.ComboForClass(6)
	if own == nil {
		t.Fatal("Blade Knight 应持有连击定义")
	}
	if own.Name != "Blade Knight Combo" || own.MaximumCompletionMS != 3000 {
		t.Fatalf("连击定义 %q / %d ms，期望 Blade Knight Combo / 3000", own.Name, own.MaximumCompletionMS)
	}
	if len(own.Steps) != 12 {
		t.Fatalf("步骤数 %d, want 12", len(own.Steps))
	}
	if got := own.StepOrders(); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("顺序应 1..3，got %v", got)
	}
	finals := 0
	for _, s := range own.Steps {
		if s.IsFinal {
			finals++
			if s.Order != 3 {
				t.Fatalf("终结步 order=%d，期望 3", s.Order)
			}
		}
	}
	if finals != 3 {
		t.Fatalf("终结步数 %d, want 3（Twisting Slash/Rageful Blow/Death Stab）", finals)
	}
}

func TestComboInheritedByNextClassOnly(t *testing.T) {
	cfg := mustLoad(t)
	own := cfg.ComboForClass(6)
	// Blade Master 沿"谁的下一转是我"回溯一步即可拿到（Player.cs:1610-1626）。
	if bm := cfg.ComboForClass(7); bm != own {
		t.Fatalf("Blade Master 应继承同一份定义，got %v", bm)
	}
	// Magic Gladiator 在这份数据里没有上一转（没有任何职业 next_class=12），
	// 所以 Duel Master 回溯到它也止步 → 两者都没有连击。
	if mg := cfg.ComboForClass(12); mg != nil {
		t.Fatalf("Magic Gladiator 不该拿到连击，got %+v", mg)
	}
	if dm := cfg.ComboForClass(13); dm != nil {
		t.Fatalf("Duel Master 不该拿到连击，got %+v", dm)
	}
	// 法师/精灵/黑暗之王/召唤师/格斗家链上都没有。
	for _, n := range []int{0, 2, 3, 4, 8, 10, 11, 16, 17, 20, 22, 23, 24, 25} {
		if got := cfg.ComboForClass(n); got != nil {
			t.Fatalf("职业 %d 不该有连击，got %+v", n, got)
		}
	}
}

func TestSkillBaseNumberWalksReplacedChain(t *testing.T) {
	cfg := mustLoad(t)
	// 普通技能原样返回。
	if got := cfg.SkillBaseNumber(19); got != 19 {
		t.Fatalf("19 的基础技能号应 19，got %d", got)
	}
	// 328 = Slash Mastery（大师技），替换 19 = Slash。
	if got := cfg.SkillBaseNumber(328); got != 19 {
		t.Fatalf("328 应归一到 19，got %d", got)
	}
	// 332 → 330 → 41：两级链（大师技替换大师技）。
	if got := cfg.SkillBaseNumber(332); got != 41 {
		t.Fatalf("332 应两跳到 41，got %d", got)
	}
}

func TestMatchesAtAcceptsOnlyItsOwnOrder(t *testing.T) {
	cfg := mustLoad(t)
	combo := cfg.ComboForClass(6)
	// 第一手可以是 19..23 中任意一招，且都不是终结步。
	for _, n := range []int{19, 20, 21, 22, 23} {
		if isFinal, ok := combo.MatchesAt(1, n); !ok || isFinal {
			t.Fatalf("第一手应接受 %d 且非终结，got (%v,%v)", n, isFinal, ok)
		}
	}
	// 第二手接受 41/42/43/232，都不是终结步。
	for _, n := range []int{41, 42, 43, 232} {
		if isFinal, ok := combo.MatchesAt(2, n); !ok || isFinal {
			t.Fatalf("第二手应接受 %d，got (%v,%v)", n, isFinal, ok)
		}
	}
	// 终结手只接受 41/42/43，且标终结。
	for _, n := range []int{41, 42, 43} {
		if isFinal, ok := combo.MatchesAt(3, n); !ok || !isFinal {
			t.Fatalf("第三手 %d 应是终结步，got (%v,%v)", n, isFinal, ok)
		}
	}
	// 232 在第三手不存在（只在第二手）；19 在第二手不存在。
	if _, ok := combo.MatchesAt(3, 232); ok {
		t.Fatal("232 不该出现在第三手")
	}
	if _, ok := combo.MatchesAt(2, 19); ok {
		t.Fatal("19 不该出现在第二手")
	}
}

func TestValidateCombosRejectsBrokenDefinitions(t *testing.T) {
	cfg := mustLoad(t)
	blade := -1
	for i := range cfg.CharacterClasses {
		if cfg.CharacterClasses[i].Number == 6 {
			blade = i
		}
	}
	if blade < 0 {
		t.Fatal("导出件里没有 Blade Knight(6)")
	}
	good := cfg.CharacterClasses[blade].Combo
	if good == nil {
		t.Fatal("Blade Knight 的连击定义应为非空")
	}
	defer func() { cfg.CharacterClasses[blade].Combo = good }()

	cases := []struct {
		name  string
		combo *SkillCombo
	}{
		{"时限非正", &SkillCombo{MaximumCompletionMS: 0, Steps: good.Steps}},
		{"无步骤", &SkillCombo{MaximumCompletionMS: 3000}},
		{"顺序不连续", &SkillCombo{MaximumCompletionMS: 3000, Steps: []SkillComboStep{
			{Skill: 19, Order: 2}, {Skill: 41, Order: 4, IsFinal: true}}}},
		{"引用未知技能", &SkillCombo{MaximumCompletionMS: 3000, Steps: []SkillComboStep{
			{Skill: 9999, Order: 1, IsFinal: true}}}},
		{"终结步不在最大顺序", &SkillCombo{MaximumCompletionMS: 3000, Steps: []SkillComboStep{
			{Skill: 19, Order: 1, IsFinal: true}, {Skill: 41, Order: 2}}}},
		{"最大顺序没有终结步", &SkillCombo{MaximumCompletionMS: 3000, Steps: []SkillComboStep{
			{Skill: 19, Order: 1}, {Skill: 41, Order: 2}}}},
	}
	for _, tc := range cases {
		cfg.CharacterClasses[blade].Combo = tc.combo
		if err := cfg.validateCombos(); err == nil {
			t.Fatalf("%s：应校验失败", tc.name)
		}
	}
	cfg.CharacterClasses[blade].Combo = good
	if err := cfg.validateCombos(); err != nil {
		t.Fatalf("完好定义被拒: %v", err)
	}
}
