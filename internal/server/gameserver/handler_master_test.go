package gameserver

// handler_master_test.go —— 大师域的端到端骨架（TRIM-09f）：入站 F3 0x52 的出站后果、
// 失败静默、技能列表的大师规则、进图大师状态、经验路由。
// 判定表本身在 action/master_points_test.go，包字节在 view/remote/master_view_test.go。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// addPointFrame 构造 C1 F3 52 的入站帧。
func addPointFrame(t *testing.T, skill uint16) []byte {
	t.Helper()
	p := c2s.NewAddMasterSkillPoint()
	p.SetSkillId(skill)
	return p.Bytes()
}

// masterChar 把脚手架角色改成大师职业并给点数。
func masterChar(c *entity.Character, points int) *entity.Character {
	c.ClassNumber = 3 // Grand Master
	c.MasterLevelUpPoints = points
	return c
}

func TestAddMasterPointSendsUpdateOnlyOnSuccess(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 400)
	masterChar(c, 5)
	if _, err := srv.resolveCharStats(c); err != nil {
		t.Fatal(err)
	}
	wp.Stats = c.Stats
	rec.frames = nil // 只观察本次加点的出站后果

	srv.handleAddMasterPoint(sess, addPointFrame(t, 300))
	if n := frameCount(rec, 0xC1, 0xF3, 0x52, false); n != 1 {
		t.Fatalf("成功加点应恰好一条 F3 52, got %d", n)
	}
	p := s2c.AsMasterSkillLevelUpdate(findFrameSub(rec, 0xC1, 0xF3, 0x52))
	if !p.Success() || p.MasterSkillNumber() != 300 || p.Level() != 1 || p.MasterLevelUpPoints() != 4 {
		t.Fatalf("F3 52 内容错: success=%v num=%d level=%d points=%d",
			p.Success(), p.MasterSkillNumber(), p.Level(), p.MasterLevelUpPoints())
	}
	if p.MasterSkillIndex() != byte(srv.deps.cfg.GameConfig.MasterSkillIndex(3, 300)) {
		t.Fatalf("槽位号应取导出表, got %d", p.MasterSkillIndex())
	}
	if got := learnedLevelOf(c, 300); got != 1 {
		t.Fatalf("角色态应记 1 级, got %d", got)
	}
	// 被动大师技不进技能列表（原版 SkillList.cs:224），故不该有 F3 11。
	if n := countFrames(rec, 0xC1, 0xF3) - 1; n != 0 {
		t.Fatalf("被动技能学习不该发技能列表包, F3 帧数-1=%d", n)
	}

	// 失败分支（无点数）：原版只写日志，客户端零帧。
	rec.frames = nil
	c.MasterLevelUpPoints = 0
	srv.handleAddMasterPoint(sess, addPointFrame(t, 301))
	if len(rec.frames) != 0 {
		t.Fatalf("加点失败不应下发任何东西, got %d 帧", len(rec.frames))
	}
	if learnedLevelOf(c, 301) != 0 {
		t.Fatal("失败的加点不得写入角色态")
	}
}

func TestAddActiveMasterSkillRefreshesSkillList(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 400)
	masterChar(c, 30)
	// 378 Flame Strengthener：中根 rank2，前置是普通技能 5（Flame）。
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 325, Level: 10}, {SkillNumber: 5}}
	st, err := srv.resolveCharStats(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Stats, wp.Stats = st, st
	rec.frames = nil
	srv.handleAddMasterPoint(sess, addPointFrame(t, 378))
	list := findFrameSub(rec, 0xC1, 0xF3, 0x11)
	if list == nil {
		t.Fatal("主动大师技学习应刷新技能列表（F3 11）")
	}
	views := srv.skillListViewOf(c)
	if !hasSkillView(views, 378) {
		t.Fatal("列表应含新学的主动大师技 378")
	}
	if hasSkillView(views, 5) {
		t.Fatal("被 378 替换的普通技能 5 不该再下发（BuildSkillList 的替换规则）")
	}
	if hasSkillView(views, 300) {
		t.Fatal("被动大师技不该出现在技能列表")
	}
	for i := 1; i < len(views); i++ {
		if views[i-1].SkillNumber > views[i].SkillNumber {
			t.Fatalf("列表应按技能号升序（原版 OrderBy，决定客户端槽位号）: %+v", views)
		}
	}
}

func TestSendMasterStatsOnlyForMasterClass(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 400)
	st, err := srv.resolveCharStats(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Stats, wp.Stats = st, st

	// 非大师职业（脚手架默认 Dark Knight=4）整段跳过。
	rec.frames = nil
	srv.sendMasterStats(sess, c, st)
	if len(rec.frames) != 0 {
		t.Fatalf("非大师职业不该发大师状态, got %d 帧", len(rec.frames))
	}

	c.ClassNumber = 3
	c.MasterLevel, c.MasterExperience, c.MasterLevelUpPoints = 7, 12345, 3
	c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 300, Level: 4}}
	rec.frames = nil
	srv.sendMasterStats(sess, c, st)
	if frameCount(rec, 0xC1, 0xF3, 0x50, false) != 1 {
		t.Fatalf("大师职业应收到 F3 50, frames=%d", len(rec.frames))
	}
	// 原版插件链：50 之后紧跟大师技能列表（UpdateMasterStatsExtendedPlugIn.cs:52）。
	if frameCount(rec, 0xC2, 0xF3, 0x53, false) != 1 {
		t.Fatalf("应收到 C2 F3 53 大师技能列表, frames=%d", len(rec.frames))
	}
	list := s2c.AsMasterSkillList(findFrameSub(rec, 0xC2, 0xF3, 0x53))
	if list.MasterSkillCount() != 1 {
		t.Fatalf("列表应只含 1 条已学, got %d", list.MasterSkillCount())
	}
	e := list.Skills(0)
	if e == nil || e.Level() != 4 {
		t.Fatalf("条目等级错: %+v", e)
	}
}

// TestKillExperienceRoutesToMasterBranch 钉住路由（TryGetExperienceKind）：
// 大师职业 + 普通满级 → 走大师分支，不再发普通等级包；S6 洛伦西亚的怪都 <95 级，
// 于是原样收到一个 Amount=0 的 33 号结果包（**不是**静默）。
func TestKillExperienceRoutesToMasterBranch(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, uint16(maxLevelForTest(t)))
	masterChar(c, 0)
	st, err := srv.resolveCharStats(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Stats, wp.Stats = st, st
	target := firstAliveMonster(t, srv, 0)
	rec.frames = nil

	got := srv.settleKillExperience(sess, c, wp, target)
	if got <= 0 {
		t.Fatalf("仍应返回计算经验（掉落金钱要用）, got %d", got)
	}
	if c.Stats.Experience != uint64(st.Experience) {
		t.Fatalf("大师分支不得改普通经验: %d → %d", st.Experience, c.Stats.Experience)
	}
	if n := len(levelUpFrames(rec)); n != 0 {
		t.Fatalf("满级大师不该收到普通等级包, got %d", n)
	}
	f := findFrame(rec, 0xC3, 0x16)
	if f == nil {
		t.Fatal("应收到一条 C3 16 经验包")
	}
	if p := s2c.AsExperienceGainedExtended(f); p.Type() != s2c.AddResult_MonsterLevelTooLowForMasterExperience {
		t.Fatalf("低等级怪的门应为 33, got %d", p.Type())
	}
}

// TestApplyMasterLevelUpSendsLevelPacketAndBlue 覆盖大师升级出站：F3 51 + 祝贺蓝字 + 光效。
func TestApplyMasterLevelUpSendsLevelPacketAndBlue(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 400)
	masterChar(c, 0)
	st, err := srv.resolveCharStats(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Stats, wp.Stats = st, st
	c.MasterLevel = 6
	rec.frames = nil

	srv.applyMasterLevelUp(c, wp, srv.viewFor(sess))
	if frameCount(rec, 0xC1, 0xF3, 0x51, false) != 1 {
		t.Fatalf("应收到一条 F3 51, frames=%d", len(rec.frames))
	}
	p := s2c.AsMasterCharacterLevelUpdateExtended(findFrameSub(rec, 0xC1, 0xF3, 0x51))
	if p.MasterLevel() != 7 || p.MaximumMasterPoints() != 200 {
		t.Fatalf("F3 51 错: lvl=%d max=%d", p.MasterLevel(), p.MaximumMasterPoints())
	}
	if p.GainedMasterPoints() != 1 || p.CurrentMasterPoints() != 1 {
		t.Fatalf("每级 1 点、当前 1 点，got gained=%d cur=%d", p.GainedMasterPoints(), p.CurrentMasterPoints())
	}
	if len(serverMessages(rec)) != 1 {
		t.Fatalf("应有一句大师升级祝贺蓝字, got %d", len(serverMessages(rec)))
	}
	if len(effectFrames(rec)) != 1 {
		t.Fatalf("升级光效应发给自已一条, got %d", len(effectFrames(rec)))
	}
}

// findFrameSub 按 (帧头, code, sub) 取最后一帧；C1/C3 的 sub 在 [3]，C2/C4 在 [4]。
func findFrameSub(rec *packetRecorder, header, code, sub byte) []byte {
	var out []byte
	for _, f := range rec.frames {
		if len(f) < 5 || f[0] != header || frameCode(f) != code {
			continue
		}
		var got byte
		if header == 0xC2 || header == 0xC4 {
			got = f[4]
		} else {
			got = f[3]
		}
		if got == sub {
			out = f
		}
	}
	return out
}

// learnedLevelOf / hasSkillView 是本地小工具（避免跨测试文件耦合）。
func learnedLevelOf(c *entity.Character, number uint16) byte {
	for _, e := range c.LearnedSkills {
		if e.SkillNumber == number {
			return e.Level
		}
	}
	return 0
}

func hasSkillView(views []action.SkillListView, number uint16) bool {
	for _, v := range views {
		if v.SkillNumber == number {
			return true
		}
	}
	return false
}
