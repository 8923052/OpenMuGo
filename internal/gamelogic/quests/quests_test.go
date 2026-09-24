package quests

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// killQuest 是三条击杀需求的任务（对照 group 0 "Infiltrate The Barracks of Balgass" 的形状）。
func killQuest() *config.Quest {
	return &config.Quest{
		Group: 15, Number: 3, StartingNumber: 3, MinLevel: 1, MaxLevel: 0, Repeatable: true,
		RequiredKills: []config.QuestKillRequirement{
			{MonsterNumber: 409, Count: 2},
			{MonsterNumber: 410, Count: 1},
		},
		Rewards: []config.QuestReward{{Type: config.QuestRewardExperience, Value: 100}},
	}
}

func TestCanStartGates(t *testing.T) {
	q := killQuest()
	if got := CanStart(q, StartContext{Level: 1}); got != OutcomeOK {
		t.Fatalf("应可开始, got %d", got)
	}
	if got := CanStart(q, StartContext{Level: 0}); got != OutcomeLevel {
		t.Fatalf("等级不足应 OutcomeLevel, got %d", got)
	}
	if got := CanStart(q, StartContext{Level: 1, HasActive: true}); got != OutcomeAlreadyActive {
		t.Fatalf("该组已有进行中应 OutcomeAlreadyActive, got %d", got)
	}
	// 等级判定先于"该组已有"（原版 :31 在 :45 之前）。
	if got := CanStart(q, StartContext{Level: 0, HasActive: true}); got != OutcomeLevel {
		t.Fatalf("次序应为等级优先, got %d", got)
	}
	if got := CanStart(q, StartContext{Level: 1, IsLastFinished: true}); got != OutcomeOK {
		t.Fatalf("可重复任务应能再接, got %d", got)
	}
	q.Repeatable = false
	if got := CanStart(q, StartContext{Level: 1, IsLastFinished: true}); got != OutcomeNotRepeatable {
		t.Fatalf("不可重复且刚完成应 OutcomeNotRepeatable, got %d", got)
	}
}

func TestCanStartChecksMoneyLast(t *testing.T) {
	q := killQuest()
	q.RequiredStartMoney = 1000
	q.Repeatable = false
	// 钱最少在等级/已有/可重复之后才判（原版 :58）。
	if got := CanStart(q, StartContext{Level: 1, Money: 999}); got != OutcomeNotEnoughMoney {
		t.Fatalf("钱不够应 OutcomeNotEnoughMoney, got %d", got)
	}
	if got := CanStart(q, StartContext{Level: 0, Money: 0, HasActive: true}); got != OutcomeLevel {
		t.Fatalf("等级不足时不应先看钱, got %d", got)
	}
	if got := CanStart(q, StartContext{Level: 1, Money: 1000}); got != OutcomeOK {
		t.Fatalf("刚好够钱应通过, got %d", got)
	}
}

func TestLevelAndClassGates(t *testing.T) {
	lvl := 5
	q := &config.Quest{Group: 19, Number: 1, MinLevel: 100, MaxLevel: 110, QualifiedClass: &lvl}
	if q.IsQualified(byte(lvl)) != true || q.IsQualified(0) {
		t.Fatal("限定职业应只接受该职业号（原版按 CharacterClass 引用相等）")
	}
	if !q.LevelOK(100) || !q.LevelOK(110) || q.LevelOK(99) || q.LevelOK(111) {
		t.Fatal("等级应为闭区间")
	}
	q.MaxLevel = 0
	if !q.LevelOK(400) {
		t.Fatal("MaxLevel==0 应视为无上限（PlayerQuestExtensions.cs:65 的 == default）")
	}
}

func TestStartClearAndKillProgress(t *testing.T) {
	q := killQuest()
	st := entity.NewQuestState(q.Group)
	Start(q, 7, &st)
	if st.ActiveQuestIndex != 7 || st.KillProgress != nil {
		t.Fatalf("Start 后应激活且清空旧计数: %+v", st)
	}
	if got := CanComplete(q, 7, &st, nil); got != OutcomeRequirements {
		t.Fatalf("0 杀应需求未满足, got %d", got)
	}
	if StateOf(q, 7, &st, nil) != StatusActive {
		t.Fatal("应为进行中")
	}
	// 每条命中该怪的需求各加一；不匹配的怪不加。
	if got := AdvanceKill(&st, q, 411); got != nil {
		t.Fatalf("无关怪物不应推进, got %+v", got)
	}
	advances := AdvanceKill(&st, q, 409)
	if len(advances) != 1 || advances[0].Current != 1 || !advances[0].Report {
		t.Fatalf("首杀应推进并上报, got %+v", advances)
	}
	AdvanceKill(&st, q, 409)
	// 原版条件 MinimumNumber >= KillCount：达标那次仍上报，之后不再。
	if a := AdvanceKill(&st, q, 409); len(a) != 1 || a[0].Current != 3 || a[0].Report {
		t.Fatalf("超过需求后不应再上报, got %+v", a)
	}
	if KillCount(&st, 0) != 3 || KillCount(&st, 1) != 0 {
		t.Fatalf("计数应只落在命中的那条: %+v", st.KillProgress)
	}
	if killsMet(q, &st) {
		t.Fatal("另一条击杀需求未完成，killsMet 应为 false")
	}
	if got := CanComplete(q, 7, &st, nil); got != OutcomeRequirements {
		t.Fatalf("410 未杀应仍不可完成, got %d", got)
	}
	AdvanceKill(&st, q, 410)
	if got := CanComplete(q, 7, &st, nil); got != OutcomeOK {
		t.Fatalf("两条都够应可完成, got %d", got)
	}
	if StateOf(q, 7, &st, nil) != StatusReadyForCompletion {
		t.Fatal("杀够应为 ReadyForCompletion")
	}
}

func TestFinishKeepsActiveUntilCleared(t *testing.T) {
	q := killQuest()
	st := entity.NewQuestState(q.Group)
	Start(q, 3, &st)
	AdvanceKill(&st, q, 409)
	AdvanceKill(&st, q, 409)
	AdvanceKill(&st, q, 410)
	// 原版 QuestCompletionAction.cs:71 只先记 LastFinished，发奖期间进行中仍在，
	// 之后才 ClearAsync —— 属性奖励的 legacy 应答要读它。
	Finish(q, 3, &st)
	if st.LastFinishedQuestIndex != 3 || st.ActiveQuestIndex != 3 {
		t.Fatalf("Finish 不应清进行中: %+v", st)
	}
	Clear(&st)
	if st.ActiveQuestIndex != -1 || st.KillProgress != nil || st.ClientActionDone {
		t.Fatalf("Clear 后应清态: %+v", st)
	}
	if StateOf(q, 3, &st, nil) != StatusCompleted {
		t.Fatal("应为 Completed")
	}
}

func TestItemRequirementLevelFilter(t *testing.T) {
	level7 := 7
	q := &config.Quest{Group: 19, Number: 4, MinLevel: 1,
		RequiredItems: []config.QuestItemRequirement{
			{ItemGroup: 13, ItemNumber: 15, Count: 2, ItemLevel: &level7},
		}}
	count := func(r *config.QuestItemRequirement) int {
		if r.ItemLevel == nil || *r.ItemLevel != 7 {
			t.Fatalf("完成判定应把 DropItemGroup 的等级口径传下来, got %v", r.ItemLevel)
		}
		return 1
	}
	st := entity.NewQuestState(q.Group)
	Start(q, 9, &st)
	if got := CanComplete(q, 9, &st, count); got != OutcomeRequirements {
		t.Fatalf("只有 1 件应不可完成, got %d", got)
	}
	if got := CanComplete(q, 9, &st, func(*config.QuestItemRequirement) int { return 2 }); got != OutcomeOK {
		t.Fatalf("够 2 件应可完成, got %d", got)
	}
}

func TestClientActionGate(t *testing.T) {
	q := &config.Quest{Group: 18, Number: 2, MinLevel: 1, RequiresClientAction: true}
	st := entity.NewQuestState(q.Group)
	Start(q, 1, &st)
	if got := CanComplete(q, 1, &st, nil); got != OutcomeRequirements {
		t.Fatalf("客户端动作未做应不可完成, got %d", got)
	}
	if MarkClientAction(q, 999, &st) {
		t.Fatal("非进行中任务不应被标记")
	}
	if !MarkClientAction(q, 1, &st) {
		t.Fatal("进行中任务应被标记")
	}
	if got := CanComplete(q, 1, &st, nil); got != OutcomeOK {
		t.Fatalf("标记后应可完成, got %d", got)
	}
}

func TestCompleteSetsLastFinishedOnly(t *testing.T) {
	q := killQuest()
	st := entity.NewQuestState(q.Group)
	Start(q, 5, &st)
	AdvanceKill(&st, q, 409)
	AdvanceKill(&st, q, 409)
	AdvanceKill(&st, q, 410)
	Complete(q, 5, &st)
	if st.ActiveQuestIndex != -1 || st.LastFinishedQuestIndex != 5 || st.KillProgress != nil {
		t.Fatalf("Complete 应记完成并清态: %+v", st)
	}
}
