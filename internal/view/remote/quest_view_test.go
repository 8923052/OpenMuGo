package remote

// quest_view_test.go —— 任务出站的**变体选择**与逐字节锚定。
//
// 对照 OpenMU GameServer/RemoteView/Quest/：
//   - QuestStateResponsePlugIn.cs:22  [MinimumClient(0, 90)]  → C1 0xF6 0x1B，251 字节；
//   - QuestStateResponseExtendedPlugIn.cs:21 / QuestProgressExtendedPlugIn.cs:21
//     [MinimumClient(106, 3)]        → C2 0xF6 0x1B / 0x0C，272 字节；
//     插件容器取"满足下限中最高"的那个（ViewPlugInContainer.cs:76-79），
//     故真机 MuMain(106,3 / 20404) 收到的是 Extended。
//   - LegacyQuestRewardPlugIn.cs:41-65 → C1 0xA3 的 200~204 奖励类型。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// s6e4 是低于 (106,3) 的对照版本（0.95d 一系：Season 6 Episode 4）→ 应走 C1 变体。
var s6e4 = version.ClientVersion{Season: 6, Episode: 4, Language: version.LanguageInvariant}

func questItemData() []byte { return make([]byte, 12) }

func TestQuestStateAndProgressFollowClientVersion(t *testing.T) {
	cases := []struct {
		name     string
		client   version.ClientVersion
		header   byte
		size     int
		extended bool
	}{
		{"20404 走 C2 Extended", s6e3, 0xC2, 272, true},
		{"0.95d 走 C1 紧凑", s6e4, 0xC1, 251, false},
	}
	for _, tc := range cases {
		for _, sub := range []struct {
			name string
			send func(v *PlayerView, conds []action.QuestConditionView, rewards []action.QuestRewardView) error
			code byte
		}{
			{"state", func(v *PlayerView, c []action.QuestConditionView, r []action.QuestRewardView) error {
				return v.ShowQuestState(18, 1, c, r)
			}, 0x1B},
			{"progress", func(v *PlayerView, c []action.QuestConditionView, r []action.QuestRewardView) error {
				return v.ShowQuestProgress(18, 1, c, r)
			}, 0x0C},
		} {
			t.Run(tc.name+" "+sub.name, func(t *testing.T) {
				rec := &recordingSender{}
				view := NewPlayerView(rec, true, tc.client, nil)
				conds := []action.QuestConditionView{{
					Type: byte(s2c.ConditionType_MonsterKills), RequirementID: 3,
					Required: 10, Current: 4,
				}}
				rewards := []action.QuestRewardView{{
					Type: byte(s2c.RewardType_Item), RewardID: 13<<9 | 15, Count: 1, ItemData: questItemData(),
				}}
				if err := sub.send(view, conds, rewards); err != nil {
					t.Fatal(err)
				}
				if len(rec.frames) != 1 {
					t.Fatalf("应发 1 帧, got %d", len(rec.frames))
				}
				f := rec.frames[0]
				if len(f) != tc.size {
					t.Fatalf("包长应为 %d, got %d (%X)", tc.size, len(f), f[:6])
				}
				codeAt, subAt := 2, 3
				if tc.header == 0xC2 {
					codeAt, subAt = 3, 4
				}
				if f[0] != tc.header || f[codeAt] != 0xF6 || f[subAt] != sub.code {
					t.Fatalf("头错: header=%X code=%X sub=%X", f[0], f[codeAt], f[subAt])
				}
				if tc.extended {
					e := s2c.AsQuestStateExtended(f)
					if e.QuestNumber() != 1 || e.QuestGroup() != 18 {
						t.Fatalf("任务标识错: num=%d group=%d", e.QuestNumber(), e.QuestGroup())
					}
					if e.ConditionsCount() != 1 || e.RewardsCount() != 1 {
						t.Fatalf("条件/奖励计数错: %d/%d", e.ConditionsCount(), e.RewardsCount())
					}
					c0 := e.Conditions(0)
					if c0.Type() != s2c.ConditionType_MonsterKills || c0.RequirementId() != 3 || c0.CurrentCount() != 4 {
						t.Fatalf("击杀条件错: type=%d id=%d cur=%d", c0.Type(), c0.RequirementId(), c0.CurrentCount())
					}
					r0 := e.Rewards(0)
					if r0.RewardId() != 13<<9|15 || r0.Type() != s2c.RewardType_Item {
						t.Fatalf("奖励错: type=%d id=%d", r0.Type(), r0.RewardId())
					}
					if data := r0.RewardedItemData(); len(data) != 15 || data[12] != 0 {
						t.Fatalf("Extended 的物品位应占 15 字节且尾部留零, got %X", data)
					}
					return
				}
				p := s2c.AsQuestState(f)
				if p.QuestNumber() != 1 || p.QuestGroup() != 18 {
					t.Fatalf("任务标识错: num=%d group=%d", p.QuestNumber(), p.QuestGroup())
				}
				if p.ConditionsCount() != 1 || p.RewardsCount() != 1 {
					t.Fatalf("条件/奖励计数错: %d/%d", p.ConditionsCount(), p.RewardsCount())
				}
			})
		}
	}
}

// TestQuestRewardOnlyMapsPacketTypes 钉住"奖励表只带经验/金币/物品"：
// 原版 EnumExtensions.cs:88-99 对其余 QuestRewardType 抛异常，本层按视图入参原样写，
// 故语义映射（哪些类型不进表）由编排层负责 —— 这里验证进表的字段编码无误。
func TestQuestRewardOnlyMapsPacketTypes(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	conds := []action.QuestConditionView{{
		Type: byte(s2c.ConditionType_Item), RequirementID: 14<<9 | 23,
		Required: 1, Current: 0, ItemData: questItemData(),
	}}
	rewards := []action.QuestRewardView{{Type: byte(s2c.RewardType_Experience), Count: 2000}}
	if err := view.ShowQuestState(0, 5, conds, rewards); err != nil {
		t.Fatal(err)
	}
	e := s2c.AsQuestStateExtended(rec.frames[0])
	c0 := e.Conditions(0)
	if c0.Type() != s2c.ConditionType_Item || c0.RequirementId() != 14<<9|23 {
		t.Fatalf("物品条件错: type=%d id=%d", c0.Type(), c0.RequirementId())
	}
	if got := c0.RequiredItemData(); len(got) != 15 {
		t.Fatalf("Extended 物品位应 15 字节, got %d", len(got))
	}
}

func TestLegacyQuestRewardPacketCodes(t *testing.T) {
	cases := []struct {
		reward action.QuestLegacyReward
		want   byte
	}{
		{action.QuestLegacyRewardLevelUpPoints, 200},
		{action.QuestLegacyRewardEvolutionFirstToSecond, 201},
		{action.QuestLegacyRewardPointsPerLevel, 202},
		{action.QuestLegacyRewardComboSkill, 203},
		{action.QuestLegacyRewardEvolutionSecondToThird, 204},
	}
	for _, tc := range cases {
		rec := &recordingSender{}
		view := NewPlayerView(rec, true, s6e3, nil)
		if err := view.ShowQuestRewardAnnouncement(0x200, tc.reward, 7); err != nil {
			t.Fatal(err)
		}
		f := rec.frames[0]
		if f[0] != 0xC1 || f[2] != 0xA3 {
			t.Fatalf("应为 C1 .. A3, got %X", f[:3])
		}
		p := s2c.AsLegacyQuestReward(f)
		if p.PlayerId() != 0x200 {
			t.Fatalf("对象号应按大端写在 [3:5], got %04X", p.PlayerId())
		}
		if byte(p.Reward()) != tc.want || p.Count() != 7 {
			t.Fatalf("奖励类型/数值错: type=%d count=%d", p.Reward(), p.Count())
		}
	}
}

// TestActiveQuestsAlwaysSentAndCapped 对照 CurrentlyActiveQuestsPlugIn：无提前返回、上限 62。
func TestActiveQuestsAlwaysSentAndCapped(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	if err := view.ShowActiveQuests(nil); err != nil {
		t.Fatal(err)
	}
	empty := s2c.AsQuestStateList(rec.frames[0])
	if empty.QuestCount() != 0 || len(rec.frames[0]) != s2c.QuestStateListRequiredSize(0) {
		t.Fatalf("空清单也应成帧: count=%d len=%d", empty.QuestCount(), len(rec.frames[0]))
	}

	many := make([]action.QuestIDView, 63)
	for i := range many {
		many[i] = action.QuestIDView{Group: 19, Number: uint16(i)}
	}
	if err := view.ShowActiveQuests(many); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[1]
	list := s2c.AsQuestStateList(f)
	if list.QuestCount() != 62 {
		t.Fatalf("应截到 62 条, got %d", list.QuestCount())
	}
	if len(f) != s2c.QuestStateListRequiredSize(62) {
		t.Fatalf("包长应按 62 条计, got %d", len(f))
	}
	if e := list.Quests(61); e == nil || e.Number() != 61 || e.Group() != 19 {
		t.Fatalf("末条错: %+v", e)
	}
}

// TestQuestEventResponseMatchesCaptured 钉住原版抓包常量 C1 0C F6 03 ..（两条条目号 1）。
func TestQuestEventResponseMatchesCaptured(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	if err := view.ShowActiveEventQuests(); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if len(f) != 12 || f[0] != 0xC1 || f[1] != 0x0C || f[2] != 0xF6 || f[3] != 0x03 {
		t.Fatalf("头应为 C1 0C F6 03 且长 12, got %X", f)
	}
	p := s2c.AsQuestEventResponse(f)
	for i := 0; i < 2; i++ {
		if e := p.Quests(i); e == nil || e.Number() != 1 {
			t.Fatalf("第 %d 条条目号应为 1, got %+v", i, e)
		}
	}
}
