package remote

// master_view_test.go —— 大师域四个出站包的逐字节锚定（TRIM-09d）。
// 对照 OpenMU ServerToClientPackets.xml 的 MasterStatsUpdate(Extended) /
// MasterCharacterLevelUpdate(Extended) / MasterSkillLevelUpdate / MasterSkillList，
// 以及 RemoteView/Character/{UpdateMasterStatsExtendedPlugIn,UpdateLevelExtendedPlugIn,
// MasterSkillLevelChangedPlugIn,UpdateMasterSkillsPlugIn}.cs。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// plainClient 是"不满足 MinimumClient(106,3)"的一端（对照原版插件容器会回落普通形态）。
var plainClient = version.ClientVersion{}

func TestMasterStatsUpdateBothVariants(t *testing.T) {
	info := action.MasterStatsView{
		MasterLevel: 7, MasterExperience: 0x0102030405060708, ExperienceOfNextLevel: 999,
		MasterLevelUpPoints: 42, MaximumHealth: 5000, MaximumMana: 3000,
		MaximumShield: 1000, MaximumAbility: 200,
	}
	ext := &recordingSender{}
	if err := NewPlayerView(ext, true, s6e3, nil).ShowMasterStats(info); err != nil {
		t.Fatal(err)
	}
	f := ext.frames[0]
	if len(f) != s2c.MasterStatsUpdateExtendedLength || f[0] != 0xC1 || f[2] != 0xF3 || f[3] != 0x50 {
		t.Fatalf("Extended 应为 C1 F3 50 / 40B, got %X", f[:5])
	}
	p := s2c.AsMasterStatsUpdateExtended(f)
	if p.MasterLevel() != 7 || p.MasterExperience() != info.MasterExperience ||
		p.MasterExperienceOfNextLevel() != 999 || p.MasterLevelUpPoints() != 42 {
		t.Fatalf("Extended 字段错: %+v", p)
	}
	// 四项上限是 u32LE，从 [24] 起每 4 字节一个（这是 Extended 与普通形态唯一的差别）。
	if p.MaximumHealth() != 5000 || p.MaximumMana() != 3000 || p.MaximumShield() != 1000 ||
		p.MaximumAbility() != 200 {
		t.Fatalf("Extended 上限错: hp=%d mp=%d sd=%d ag=%d",
			p.MaximumHealth(), p.MaximumMana(), p.MaximumShield(), p.MaximumAbility())
	}
	if f[24] != 0x88 || f[25] != 0x13 { // 5000 = 0x1388，小端
		t.Fatalf("MaximumHealth 应小端写在 [24:26], got %X", f[24:26])
	}
	// 大师经验是 u64 **大端**（[6:14]）——与经验表其它包一致的怪癖。
	if f[6] != 0x01 || f[13] != 0x08 {
		t.Fatalf("MasterExperience 应大端写在 [6:14], got %X", f[6:14])
	}

	plain := &recordingSender{}
	if err := NewPlayerView(plain, true, plainClient, nil).ShowMasterStats(info); err != nil {
		t.Fatal(err)
	}
	f = plain.frames[0]
	if len(f) != s2c.MasterStatsUpdateLength || f[3] != 0x50 {
		t.Fatalf("普通形态应为 32B, got %d", len(f))
	}
	q := s2c.AsMasterStatsUpdate(f)
	if q.MaximumHealth() != 5000 || q.MaximumAbility() != 200 {
		t.Fatalf("普通形态 u16 上限错: hp=%d ag=%d", q.MaximumHealth(), q.MaximumAbility())
	}
}

func TestMasterCharacterLevelUpdateVariants(t *testing.T) {
	info := action.MasterLevelView{
		MasterLevel: 8, GainedPoints: 1, CurrentPoints: 43, MaximumPoints: 200,
		MaximumHealth: 6000, MaximumMana: 2500, MaximumShield: 1500, MaximumAbility: 300,
	}
	ext := &recordingSender{}
	if err := NewPlayerView(ext, true, s6e3, nil).ShowMasterCharacterLevel(info); err != nil {
		t.Fatal(err)
	}
	f := ext.frames[0]
	if len(f) != s2c.MasterCharacterLevelUpdateExtendedLength || f[2] != 0xF3 || f[3] != 0x51 {
		t.Fatalf("Extended 应为 F3 51 / 28B, got %d %X", len(f), f[:4])
	}
	p := s2c.AsMasterCharacterLevelUpdateExtended(f)
	if p.MasterLevel() != 8 || p.GainedMasterPoints() != 1 || p.CurrentMasterPoints() != 43 ||
		p.MaximumMasterPoints() != 200 {
		t.Fatalf("Extended 点数区错: lvl=%d gained=%d cur=%d max=%d",
			p.MasterLevel(), p.GainedMasterPoints(), p.CurrentMasterPoints(), p.MaximumMasterPoints())
	}
	if p.MaximumHealth() != 6000 || p.MaximumAbility() != 300 {
		t.Fatalf("Extended 上限区错: hp=%d ag=%d", p.MaximumHealth(), p.MaximumAbility())
	}
	// MaximumMasterPoints 槽位放的是**配置的最大大师等级**（原版 UpdateLevelPlugIn.cs:73 即如此）。

	plain := &recordingSender{}
	if err := NewPlayerView(plain, true, plainClient, nil).ShowMasterCharacterLevel(info); err != nil {
		t.Fatal(err)
	}
	if got := len(plain.frames[0]); got != s2c.MasterCharacterLevelUpdateLength {
		t.Fatalf("普通形态应 20B, got %d", got)
	}
}

func TestMasterSkillLevelUpdatePacket(t *testing.T) {
	info := action.MasterSkillUpdateView{
		Points: 33, SkillIndex: 41, SkillNumber: 378, Level: 12,
		DisplayValue: 1.5, DisplayValueOfNextL: 2.25,
	}
	rec := &recordingSender{}
	if err := NewPlayerView(rec, true, s6e3, nil).ShowMasterSkillLevelUpdate(info); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if len(f) != s2c.MasterSkillLevelUpdateLength || f[2] != 0xF3 || f[3] != 0x52 {
		t.Fatalf("应为 C1 F3 52 / 28B, got %d %X", len(f), f[:4])
	}
	p := s2c.AsMasterSkillLevelUpdate(f)
	if !p.Success() {
		t.Fatal("Success 位应恒置位（原版只在成功后发）")
	}
	if f[4] != 0x01 {
		t.Fatalf("Success 是 [4] 的 bit0, got %02X", f[4])
	}
	if p.MasterLevelUpPoints() != 33 || p.MasterSkillIndex() != 41 ||
		p.MasterSkillNumber() != 378 || p.Level() != 12 {
		t.Fatalf("字段错: %+v", p)
	}
	if f[9] != 0 || f[10] != 0 || f[11] != 0 {
		t.Fatalf("[9:12] 是填充区，应保持 0, got %X", f[9:12])
	}
	if p.DisplayValue() != 1.5 {
		t.Fatalf("DisplayValue 错: %v", p.DisplayValue())
	}
	if p.DisplayValueOfNextLevel() != 2.25 {
		t.Fatalf("下一级展示值错: %v", p.DisplayValueOfNextLevel())
	}
}

func TestMasterSkillListPacket(t *testing.T) {
	entries := []action.MasterSkillEntryView{
		{Index: 1, Level: 20, DisplayValue: 90.16, DisplayValueOfNextLev: 90.16},
		{Index: 37, Level: 3, DisplayValue: 1.5, DisplayValueOfNextLev: 1.75},
	}
	rec := &recordingSender{}
	if err := NewPlayerView(rec, true, s6e3, nil).ShowMasterSkillList(entries); err != nil {
		t.Fatal(err)
	}
	f := rec.frames[0]
	if f[0] != 0xC2 {
		t.Fatalf("0x53 是 C2 头（唯一的 C2 大师包）, got %02X", f[0])
	}
	if got, want := len(f), s2c.MasterSkillListRequiredSize(len(entries)); got != want {
		t.Fatalf("帧长 %d, want %d", got, want)
	}
	if f[3] != 0xF3 || f[4] != 0x53 {
		t.Fatalf("C2 的 code/sub 在 [3]/[4], got %X", f[3:5])
	}
	p := s2c.AsMasterSkillList(f)
	if p.MasterSkillCount() != 2 {
		t.Fatalf("计数应写在 [8:12] 小端, got %d", p.MasterSkillCount())
	}
	for i, want := range entries {
		e := p.Skills(i)
		if e == nil {
			t.Fatalf("条目 %d 取不到", i)
		}
		if e.MasterSkillIndex() != want.Index || e.Level() != want.Level ||
			e.DisplayValue() != want.DisplayValue || e.DisplayValueOfNextLevel() != want.DisplayValueOfNextLev {
			t.Fatalf("条目 %d 错: %+v want %+v", i, e, want)
		}
	}
	// 条目 12B：index[0] level[1] 填充[2:4] display[4:8] next[8:12]。
	if p.Skills(0) != nil && f[12] != 1 {
		t.Fatalf("首条目的槽位号应从 [12] 开始, got %X", f[12:24])
	}

	empty := &recordingSender{}
	if err := NewPlayerView(empty, true, s6e3, nil).ShowMasterSkillList(nil); err != nil {
		t.Fatal(err)
	}
	if got := len(empty.frames[0]); got != s2c.MasterSkillListRequiredSize(0) {
		t.Fatalf("空列表也应有 12B 头, got %d", got)
	}
	if s2c.AsMasterSkillList(empty.frames[0]).MasterSkillCount() != 0 {
		t.Fatal("空列表计数应为 0")
	}
}
