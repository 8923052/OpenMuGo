package remote

// npc_window_test.go —— NPC 对话窗口/气泡族出站包的逐字节锚定。
// 对照 OpenMU RemoteView/NPC/OpenNpcWindowPlugIn.cs（映射表 :46-79、分支 :31-44）与
// RemoteView/Quest/{QuestStructExtensions,LegacyQuestStateDialogPlugIn,QuestStateExtensions}.cs。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/quests"
	s2c "mugo/internal/proto/s2c"
)

// TestNpcWindowWireTable 逐项钉住"配置枚举 → 线协议号"（枚举序号≠线号是这张表存在的全部理由）。
func TestNpcWindowWireTable(t *testing.T) {
	cases := []struct {
		config int
		wire   s2c.NpcWindow
	}{
		{1, s2c.NpcWindow_Merchant},                       // → 0
		{2, s2c.NpcWindow_Merchant1},                      // → 1
		{4, s2c.NpcWindow_VaultStorage},                   // → 2
		{5, s2c.NpcWindow_ChaosMachine},                   // → 3
		{6, s2c.NpcWindow_DevilSquare},                    // → 4
		{7, s2c.NpcWindow_BloodCastle},                    // → 6
		{8, s2c.NpcWindow_PetTrainer},                     // → 7
		{9, s2c.NpcWindow_Lahap},                          // → 9
		{10, s2c.NpcWindow_CastleSeniorNPC},               // → 12
		{11, s2c.NpcWindow_ElphisRefinery},                // → 17
		{12, s2c.NpcWindow_RefineStoneMaking},             // → 18
		{13, s2c.NpcWindow_RemoveJohOption},               // → 19
		{14, s2c.NpcWindow_IllusionTemple},                // → 20
		{15, s2c.NpcWindow_ChaosCardCombination},          // → 21
		{16, s2c.NpcWindow_CherryBlossomBranchesAssembly}, // → 22
		{17, s2c.NpcWindow_SeedMaster},                    // → 23
		{18, s2c.NpcWindow_SeedResearcher},                // → 24
		{19, s2c.NpcWindow_StatReInitializer},             // → 25
		{20, s2c.NpcWindow_DelgadoLuckyCoinRegistration},  // → 32
		{21, s2c.NpcWindow_DoorkeeperTitusDuelWatch},      // → 33
		{22, s2c.NpcWindow_LugardDoppelgangerEntry},       // → 35
		{23, s2c.NpcWindow_JerintGaionEvententry},         // → 36
		{24, s2c.NpcWindow_JuliaWarpMarketServer},         // → 37
		{26, s2c.NpcWindow_CombineLuckyItem},              // → 38
	}
	if len(cases) != len(npcWindowWire) {
		t.Fatalf("映射表项数 = %d, 用例只覆盖 %d", len(npcWindowWire), len(cases))
	}
	for _, tc := range cases {
		rec := &recordingSender{}
		v := NewPlayerView(rec, true, s6e3, nil)
		if err := v.ShowNpcWindow(tc.config); err != nil {
			t.Fatalf("窗口 %d 应可下发: %v", tc.config, err)
		}
		f := rec.frames[0]
		if got := s2c.AsNpcWindowResponse(f).Window(); got != tc.wire {
			t.Fatalf("配置窗口 %d → 线号 %d, want %d", tc.config, got, tc.wire)
		}
		if !HasNpcWindowResponse(tc.config) {
			t.Fatalf("HasNpcWindowResponse(%d) 应为 true", tc.config)
		}
	}
}

// TestNpcWindowWithoutWireValue 锁定原版的 throw 支路：这些号不发窗包、只报错。
func TestNpcWindowWithoutWireValue(t *testing.T) {
	for _, w := range []int{0, 3, 25, 27, 28, 29, 30, 99} {
		rec := &recordingSender{}
		v := NewPlayerView(rec, true, s6e3, nil)
		if err := v.ShowNpcWindow(w); err == nil {
			t.Fatalf("窗口 %d 应无协议对应值", w)
		}
		if len(rec.frames) != 0 {
			t.Fatalf("窗口 %d 不该发包, got %d", w, len(rec.frames))
		}
		if HasNpcWindowResponse(w) {
			t.Fatalf("HasNpcWindowResponse(%d) 应为 false", w)
		}
	}
}

func TestShowNpcDialogPacket(t *testing.T) {
	rec := &recordingSender{}
	v := NewPlayerView(rec, true, s6e3, nil)
	if err := v.ShowNpcDialog(257); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if len(f) != s2c.OpenNpcDialogLength {
		t.Fatalf("OpenNpcDialog 应为 %d 字节, got %d", s2c.OpenNpcDialogLength, len(f))
	}
	p := s2c.AsOpenNpcDialog(f)
	if p.NpcNumber() != 257 || p.GensContributionPoints() != 0 {
		t.Fatalf("NPC 号/公勋点错: num=%d gens=%d", p.NpcNumber(), p.GensContributionPoints())
	}
}

func TestObjectMessagePacket(t *testing.T) {
	rec := &recordingSender{}
	v := NewPlayerView(rec, true, s6e3, nil)
	msg := "I have no quests for you."
	if err := v.ShowObjectMessage(0x201, msg); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if f[0] != 0xC1 || f[2] != 0x01 {
		t.Fatalf("应为 C1 .. 01, got %X", f[:3])
	}
	if len(f) != s2c.ObjectMessageRequiredSize(len(msg)) {
		t.Fatalf("帧长应为 5+len+1, got %d", len(f))
	}
	p := s2c.AsObjectMessage(f)
	if p.ObjectId() != 0x201 {
		t.Fatalf("对象号应大端写在 [3:5], got %04X", p.ObjectId())
	}
	if p.MessageString() != msg {
		t.Fatalf("文本错: %q", p.MessageString())
	}
	if f[len(f)-1] != 0x00 {
		t.Fatal("文本应以 NUL 结尾")
	}
}

// TestLegacyQuestStateListPacket 钉住 C1 A0：计数 7、2bit/槽、黑暗石槽按职业基型置 Undefined。
func TestLegacyQuestStateListPacket(t *testing.T) {
	// 暗骑士基型时"黑暗石"槽保留 Inactive（非暗骑士分支见文件末第二例）。
	facts := quests.LegacyFacts{HasState: true, LastFinished: 1, HasLast: true,
		Active: 2, HasActive: true, DarkKnightBase: true}
	rec := &recordingSender{}
	v := NewPlayerView(rec, true, s6e3, nil)
	if err := v.ShowLegacyQuestStateList(statesArray(quests.StateList(facts))); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if f[0] != 0xC1 || f[2] != 0xA0 {
		t.Fatalf("应为 C1 .. A0, got %X", f[:3])
	}
	if len(f) != 6 {
		t.Fatalf("7 槽应为 4+⌈7/4⌉=6 字节, got %d", len(f))
	}
	p := s2c.AsLegacyQuestStateList(f)
	if p.QuestCount() != 7 {
		t.Fatalf("计数应为 7（原版覆盖默认 6）, got %d", p.QuestCount())
	}
	want := []s2c.LegacyQuestState{
		s2c.LegacyQuestState_Complete, s2c.LegacyQuestState_Complete,
		s2c.LegacyQuestState_Active, s2c.LegacyQuestState_Inactive,
		s2c.LegacyQuestState_Inactive, s2c.LegacyQuestState_Inactive,
		s2c.LegacyQuestState_Inactive,
	}
	got := []s2c.LegacyQuestState{
		p.ScrollOfEmperorState(), p.ThreeTreasuresOfMuState(), p.GainHeroStatusState(),
		p.SecretOfDarkStoneState(), p.CertificateOfStrengthState(),
		p.InfiltrationOfBarrackState(), p.InfiltrationOfRefugeState(),
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("槽 %d = %d, want %d", i, got[i], want[i])
		}
	}

	// 非暗骑士基型 → 黑暗石槽变 Undefined（两位清零）。
	states := quests.StateList(quests.LegacyFacts{})
	if err := v.ShowLegacyQuestStateList(statesArray(states)); err != nil {
		t.Fatal(err)
	}
	p2 := s2c.AsLegacyQuestStateList(rec.frames[1])
	if p2.SecretOfDarkStoneState() != s2c.LegacyQuestState_Undefined {
		t.Fatalf("非暗骑士时黑暗石槽应为 Undefined, got %d", p2.SecretOfDarkStoneState())
	}
	if p2.ScrollOfEmperorState() != s2c.LegacyQuestState_Inactive {
		t.Fatalf("其余槽仍应 Inactive, got %d", p2.ScrollOfEmperorState())
	}
}

func TestLegacyQuestDialogAndKillInfoPackets(t *testing.T) {
	rec := &recordingSender{}
	v := NewPlayerView(rec, true, s6e3, nil)
	if err := v.ShowLegacyQuestStateDialog(5, 0b11_10_01_00); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if len(f) != 5 || f[0] != 0xC1 || f[2] != 0xA1 || f[3] != 5 || f[4] != 0b11_10_01_00 {
		t.Fatalf("A1 帧错: %X", f)
	}
	if err := v.ShowLegacySetQuestStateResponse(6, 0, 0x03); err != nil {
		t.Fatal(err)
	}
	f = rec.frames[1]
	if len(f) != 6 || f[2] != 0xA2 || f[3] != 6 || f[4] != 0 || f[5] != 0x03 {
		t.Fatalf("A2 帧错: %X", f)
	}
	if err := v.ShowLegacyQuestMonsterKillInfo(5, []action.LegacyKillView{
		{MonsterNumber: 409, Count: 3}, {MonsterNumber: 410, Count: 20},
	}); err != nil {
		t.Fatal(err)
	}
	f = rec.frames[2]
	if len(f) != 48 || f[2] != 0xA4 || f[3] != 0x00 {
		t.Fatalf("A4 帧头错: %X", f[:4])
	}
	p := s2c.AsLegacyQuestMonsterKillInfo(f)
	if p.QuestIndex() != 5 || p.Result() != 1 {
		t.Fatalf("A4 任务号/结果错: idx=%d result=%d", p.QuestIndex(), p.Result())
	}
	k0, k1 := p.Kills(0), p.Kills(1)
	if k0.MonsterNumber() != 409 || k0.KillCount() != 3 || k1.MonsterNumber() != 410 || k1.KillCount() != 20 {
		t.Fatalf("A4 击杀条目错: %+v %+v", k0, k1)
	}
	if k := p.Kills(2); k.MonsterNumber() != 0 || k.KillCount() != 0 {
		t.Fatalf("未用条目应保持 0, got %+v", k)
	}
	if err := v.ShowGuildMasterDialog(); err != nil {
		t.Fatal(err)
	}
	f = rec.frames[3]
	if f[0] != 0xC1 || f[2] != 0x54 || len(f) != s2c.ShowGuildMasterDialogLength {
		t.Fatalf("C1 54 帧错: %X", f)
	}
}

// statesArray 把 quests.StateList 的定长数组转成视图入参形状。
func statesArray(s [7]quests.LegacyState) [7]byte {
	var out [7]byte
	for i, v := range s {
		out[i] = byte(v)
	}
	return out
}
