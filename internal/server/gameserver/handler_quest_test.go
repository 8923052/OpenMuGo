package gameserver

// handler_quest_test.go —— S10 任务编排端到端，跑**真导出件**（499 条，组 0/15/18/19）：
// 击杀线（18/1 Spider Hunt!）、legacy 线（组 0 的加点/转职/属性/技能奖励）、NPC 与职业门、
// 钱门蓝字、拒绝分支、清单包与 20404 的 C2 Extended 变体。
// 判定分支本身在 gamelogic/quests 有纯函数用例，这里只测接线与出站。

import (
	"fmt"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// spiderHunterLevel 落在 18/1 "Spider Hunt!" 的 1..14 区间内。
const spiderHunterLevel = 10

// questNpc 造一个"正在对话"的 NPC（任务判定只读 Def.Number）。
func questNpc(number int) *npc.Npc {
	return &npc.Npc{Def: &config.Monster{Number: number}}
}

// questFramePositions 给出 0xF6 组帧里 code/sub 的字节位：C1/C3 是 [2]/[3]，
// C2/C4 是 [3]/[4]（长头型的长度占两字节，码位后移 —— 对照 proto 的 setHeader）。
func questFramePositions(header byte) (int, int) {
	if header == 0xC2 || header == 0xC4 {
		return 3, 4
	}
	return 2, 3
}

// findQuestFrame 返回最后一条 header/0xF6/sub 的帧。
func findQuestFrame(rec *packetRecorder, header, sub byte) []byte {
	codeAt, subAt := questFramePositions(header)
	var last []byte
	for _, f := range rec.frames {
		if len(f) > subAt && f[0] == header && f[codeAt] == 0xF6 && f[subAt] == sub {
			last = f
		}
	}
	return last
}

// findLegacyRewardFrame 返回最后一条 C1 A3 帧（[5]=奖励类型，[6]=数值）。
func findLegacyRewardFrame(rec *packetRecorder, reward byte) []byte {
	for _, f := range rec.frames {
		if len(f) >= 7 && f[0] == 0xC1 && f[2] == 0xA3 && f[5] == reward {
			return f
		}
	}
	return nil
}

// questStateOfGroup 只读查询某组状态。
func questStateOfGroup(c *entity.Character, group int) *entity.QuestState {
	for i := range c.QuestStates {
		if c.QuestStates[i].Group == group {
			return &c.QuestStates[i]
		}
	}
	return nil
}

func questProceedFrame(group, number int, action c2s.QuestProceedAction) []byte {
	f := c2s.NewQuestProceedRequest()
	f.SetQuestGroup(uint16(group))
	f.SetQuestNumber(uint16(number))
	f.SetProceedAction(action)
	return f.Bytes()
}

// questCompletionFrame 造一条 0x0D 完成请求。
func questCompletionFrame(t *testing.T, group, number int) []byte {
	t.Helper()
	f := c2s.NewQuestCompletionRequest()
	f.SetQuestGroup(uint16(group))
	f.SetQuestNumber(uint16(number))
	return f.Bytes()
}

// addItem 往背包塞一件指定等级物品。
func addItem(c *entity.Character, srv *Server, group, number, level int) {
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	c.Inventory.AddToFree(srv.newSlottedItem(&item.Item{
		Group: byte(group), Number: number, Level: byte(level), Durability: 1,
	}))
}

// replaceItem 先清空该定义的所有件，再放一件指定等级的。
func replaceItem(c *entity.Character, srv *Server, group, number, level int) {
	for _, si := range c.Inventory.Grid().Items() {
		if si != nil && si.It != nil && si.It.Group == byte(group) && si.It.Number == number {
			c.Inventory.Remove(si)
		}
	}
	addItem(c, srv, group, number, level)
}

// TestQuestKillFlowEndToEnd 走 18/1：接（StepInfo + C2 进度）→ 10 杀（逐条进度蓝字，
// 不发进度包）→ 交（经验奖励 + 完成应答 + 清态）。
func TestQuestKillFlowEndToEnd(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, spiderHunterLevel)
	sess.setOpenedNpc(questNpc(257))
	moneyBefore := c.Stats.Money

	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))

	st := questStateOfGroup(c, 18)
	if st == nil || st.ActiveQuestIndex < 0 {
		t.Fatalf("任务应已开始, got %+v", st)
	}
	if c.Stats.Money != moneyBefore {
		t.Fatalf("该任务无起始费，不应扣钱: %d → %d", moneyBefore, c.Stats.Money)
	}
	if findQuestFrame(rec, 0xC1, 0x0B) == nil {
		t.Fatal("开始应下发 C1 F6 0B StepInfo")
	}
	if findQuestFrame(rec, 0xC2, 0x0C) == nil {
		t.Fatal("20404 的开始确认应下发 **C2** F6 0C Extended 进度包")
	}

	before := len(rec.frames)
	for i := 0; i < 10; i++ {
		srv.advanceQuestKills(sess, c, 3)
	}
	if got := st.KillProgress[0]; got != 10 {
		t.Fatalf("击杀计数应为 10, got %d", got)
	}
	if added := rec.frames[before:]; len(added) != 10 {
		t.Fatalf("10 次击杀应只发 10 条包（进度蓝字），got %d", len(added))
	}
	msgs := serverMessages(rec)
	for i := 1; i <= 10; i++ {
		want := fmt.Sprintf("[Spider Hunt!] Defeat Spider - %d/10", i)
		if got := msgs[len(msgs)-10+i-1].text; got != want {
			t.Fatalf("第 %d 条进度蓝字应为 %q, got %q", i, want, got)
		}
	}

	expBefore := c.Stats.Experience
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 18, 1))
	if c.Stats.Experience <= expBefore {
		t.Fatalf("经验奖励应入账: before=%d after=%d", expBefore, c.Stats.Experience)
	}
	if findQuestFrame(rec, 0xC1, 0x0D) == nil {
		t.Fatal("应收到 C1 F6 0D 完成应答")
	}
	if st.ActiveQuestIndex != -1 || st.KillProgress != nil {
		t.Fatalf("完成后应清进行中与计数: %+v", st)
	}
	if st.LastFinishedQuestIndex < 0 {
		t.Fatal("完成后应记 LastFinished")
	}
}

// TestQuestNeedsOpenedNpc 对照 PlayerQuestExtensions.GetQuest：没有对话 NPC 就取不到任务。
func TestQuestNeedsOpenedNpc(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, spiderHunterLevel)
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 18); st != nil && st.ActiveQuestIndex >= 0 {
		t.Fatal("未开 NPC 不应能接任务")
	}
	sess.setOpenedNpc(questNpc(257))
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 18); st == nil || st.ActiveQuestIndex < 0 {
		t.Fatal("开了 257 号 NPC 后应能接")
	}
}

// TestQuestLevelAndNpcGates 校验等级区间与"别的 NPC 不认"。
func TestQuestLevelAndNpcGates(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, 200) // 18/1 上限 14 级
	sess.setOpenedNpc(questNpc(257))
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 18); st != nil && st.ActiveQuestIndex >= 0 {
		t.Fatal("200 级不应能接 1..14 级的任务")
	}

	srv2, sess2, _, c2, _ := newExpScaffold(t, spiderHunterLevel)
	sess2.setOpenedNpc(questNpc(235))
	srv2.handleQuestPacket(sess2, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c2, 18); st != nil && st.ActiveQuestIndex >= 0 {
		t.Fatal("Sevina 面前不应能接 257 号的任务")
	}
}

// TestQuestAlreadyActiveSendsProgress 对照 QuestProceedRequestHandlerPlugIn.cs:38：
// 该组已有进行中任务时，重复"接受"只回那条的确认与进度包（20404 为 C2 Extended）。
func TestQuestAlreadyActiveSendsProgress(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, spiderHunterLevel)
	sess.setOpenedNpc(questNpc(257))
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 18); st == nil || st.ActiveQuestIndex < 0 {
		t.Fatal("前置：18/1 应已开始")
	}
	rec.frames = nil
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	if findQuestFrame(rec, 0xC1, 0x0B) == nil {
		t.Fatal("确认应答应是 StepInfo")
	}
	f := findQuestFrame(rec, 0xC2, 0x0C)
	if f == nil {
		t.Fatalf("应回一发 C2 F6 0C 进度包, got %d 帧", len(rec.frames))
	}
	st := s2c.AsQuestProgressExtended(f)
	if st.QuestNumber() != 1 || st.QuestGroup() != 18 {
		t.Fatalf("进度包应是 18/1: num=%d group=%d", st.QuestNumber(), st.QuestGroup())
	}
	if st.ConditionsCount() != 1 {
		t.Fatalf("18/1 应有一条击杀条件, got %d", st.ConditionsCount())
	}
	c0 := st.Conditions(0)
	if c0.RequirementId() != 3 || c0.RequiredCount() != 10 {
		t.Fatalf("击杀条件应是怪物 3×10: id=%d req=%d", c0.RequirementId(), c0.RequiredCount())
	}
}

// TestQuestNotEnoughMoneyBlueMessage 对照 QuestStartAction.cs:66 的 NotEnoughMoneyToProceed。
func TestQuestNotEnoughMoneyBlueMessage(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	sess.setOpenedNpc(questNpc(235)) // 0/0 Find the Scroll of Emperor：起始费 1,000,000
	c.Stats.Money = 999_999
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 0, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 0); st != nil && st.ActiveQuestIndex >= 0 {
		t.Fatal("钱不够不应开始")
	}
	wantBlueMessages(t, rec, "Not enough money to proceed")
}

// TestQuestStartDeductsMoney 起始费扣除与状态置位（组 0 的应答是 legacy 0xA2，随 TRIM-07）。
func TestQuestStartDeductsMoney(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, 200)
	sess.setOpenedNpc(questNpc(235))
	c.Stats.Money = 5_000_000
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 0, c2s.QuestProceedAction_AcceptQuest))
	if c.Stats.Money != 4_000_000 {
		t.Fatalf("应扣 1,000,000 起始费, got %d", c.Stats.Money)
	}
	if st := questStateOfGroup(c, 0); st == nil || st.ActiveQuestIndex < 0 {
		t.Fatal("组 0 任务应已开始（服务端状态照走）")
	}
	// 重复"接受"：走"保持运行并确认"分支，不再扣钱。
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 0, c2s.QuestProceedAction_AcceptQuest))
	if c.Stats.Money != 4_000_000 {
		t.Fatalf("已在进行中不应再扣钱, got %d", c.Stats.Money)
	}
}

// TestQuestRefuseSendsRefuseNumber 对照 QuestProceedRequestHandlerPlugIn.cs:58：
// 拒绝只回 StepInfo(RefuseNumber)，不进状态。
func TestQuestRefuseSendsRefuseNumber(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 260) // 19/5：起始号 3，拒绝号 4
	sess.setOpenedNpc(questNpc(257))
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(19, 3, c2s.QuestProceedAction_RefuseQuest))
	f := findQuestFrame(rec, 0xC1, 0x0B)
	if f == nil {
		t.Fatal("拒绝应回 StepInfo")
	}
	if got := s2c.AsQuestStepInfo(f).QuestStepNumber(); got != 4 {
		t.Fatalf("步骤号应为 RefuseNumber 4, got %d", got)
	}
	if st := questStateOfGroup(c, 19); st != nil && st.ActiveQuestIndex >= 0 {
		t.Fatal("拒绝不应进入进行中")
	}
}

// TestQuestItemRequirementLevelFilter 组 0/0 要的是 **等级 0** 的 14/23：
// 给一件 +1 的应拒绝完成，换成等级 0 的才发 LevelUpPoints 奖励。
func TestQuestItemRequirementLevelFilter(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	sess.setOpenedNpc(questNpc(235))
	c.Stats.Money = 10_000_000
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	addItem(c, srv, 14, 23, 1)
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 0, c2s.QuestProceedAction_AcceptQuest))
	pointsBefore := c.Stats.LevelUpPoints
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 0))
	if st := questStateOfGroup(c, 0); st == nil || st.ActiveQuestIndex < 0 {
		t.Fatalf("完成被拒后任务应仍在进行中: %+v", st)
	}
	if c.Stats.LevelUpPoints != pointsBefore {
		t.Fatalf("等级口径不符不应发奖: before=%d after=%d", pointsBefore, c.Stats.LevelUpPoints)
	}
	if findLegacyRewardFrame(rec, byte(s2c.QuestRewardType_LevelUpPoints)) != nil {
		t.Fatal("未通过完成不应有 A3 应答")
	}

	replaceItem(c, srv, 14, 23, 0)
	pointsBefore = c.Stats.LevelUpPoints
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 0))
	if c.Stats.LevelUpPoints != pointsBefore+10 {
		t.Fatalf("LevelUpPoints 奖励应 +10: before=%d after=%d", pointsBefore, c.Stats.LevelUpPoints)
	}
	if findLegacyRewardFrame(rec, byte(s2c.QuestRewardType_LevelUpPoints)) == nil {
		t.Fatal("加点奖励应有 C1 A3 (200) 应答")
	}
	if findQuestFrame(rec, 0xC1, 0x0D) != nil {
		t.Fatal("组 0 完成应走 legacy 应答（0xA2，随 TRIM-07），不得发现代 0x0D")
	}
	if st := questStateOfGroup(c, 0); st.ActiveQuestIndex != -1 {
		t.Fatal("完成后应清进行中")
	}
	if got := c.Inventory.Count(); got != 0 {
		t.Fatalf("上交应销毁需求物品, got %d 件", got)
	}
}

// TestQuestEvolutionSwitchesClass 组 0/1（Treasures of MU (Dark Knight)）：职业 4→6 + A3(201)。
func TestQuestEvolutionSwitchesClass(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	sess.setOpenedNpc(questNpc(235))
	c.Stats.Money = 10_000_000
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	addItem(c, srv, 14, 24, 0)
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 1, c2s.QuestProceedAction_AcceptQuest))
	if st := questStateOfGroup(c, 0); st == nil || st.ActiveQuestIndex < 0 {
		t.Fatal("一转任务应已开始")
	}
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 1))
	if c.ClassNumber != 6 {
		t.Fatalf("转职应把职业 4 换成 6（NextClass）, got %d", c.ClassNumber)
	}
	f := findLegacyRewardFrame(rec, byte(s2c.QuestRewardType_CharacterEvolutionFirstToSecond))
	if f == nil {
		t.Fatal("转职应有 C1 A3 (201) 应答")
	}
	if f[6] != byte(6<<3) {
		t.Fatalf("201 载荷应为新职业号<<3 = %d, got %d", 6<<3, f[6])
	}
}

// TestQuestAttributeReward 组 0/2（Gain Hero Status (Blade Knight)，要 **等级 1** 的 14/23）：
// 角色属性加成 + PointsPerLevelUp 补偿 + A3(202)。
func TestQuestAttributeReward(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 225)
	c.ClassNumber = 6
	sess.setOpenedNpc(questNpc(229))
	c.Stats.Money = 10_000_000
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	addItem(c, srv, 14, 23, 0) // 先给错的等级
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 2, c2s.QuestProceedAction_AcceptQuest))
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 2))
	if len(c.AttributeBonuses) != 0 {
		t.Fatalf("等级不符不应发属性奖励: %+v", c.AttributeBonuses)
	}

	replaceItem(c, srv, 14, 23, 1)
	pointsBefore := c.Stats.LevelUpPoints
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 2))
	if len(c.AttributeBonuses) != 2 {
		t.Fatalf("应有两条属性加成（每级点数 + HeroStatus）: %+v", c.AttributeBonuses)
	}
	if c.AttributeBonuses[0].AttributeID != pointsPerLevelUpAttributeID {
		t.Fatalf("首条应是 PointsPerLevelUp: %+v", c.AttributeBonuses[0])
	}
	// (等级 225 - 任务最低 220) × 1 的补偿点数（QuestCompletionAction.cs:114）。
	if c.Stats.LevelUpPoints != pointsBefore+uint16(225-220) {
		t.Fatalf("每级点数补偿应 +5: before=%d after=%d", pointsBefore, c.Stats.LevelUpPoints)
	}
	if findLegacyRewardFrame(rec, byte(s2c.QuestRewardType_LevelUpPointsPerLevelIncrease)) == nil {
		t.Fatal("应有一发 C1 A3 (202)")
	}
	// "Points per Level up" 是职业 StatAttributes 项 → 奖励必须**加**在职业初值 5 上（不是替换）。
	if got := player.AttributeValue(srv.deps.cfg.GameConfig, c, "Points per Level up"); got != 6 {
		t.Fatalf("属性系统应看到 5(职业初值)+1(奖励)=6, got %v", got)
	}
}

// TestQuestComboAttributeReward 组 0/3（Secret of the Dark Stone，要等级 1 的 14/24）→ A3(203)。
func TestQuestComboAttributeReward(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 250)
	c.ClassNumber = 6
	sess.setOpenedNpc(questNpc(229))
	c.Stats.Money = 10_000_000
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	addItem(c, srv, 14, 24, 1)
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 3, c2s.QuestProceedAction_AcceptQuest))
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 3))
	if findLegacyRewardFrame(rec, byte(s2c.QuestRewardType_ComboSkill)) == nil {
		t.Fatal("连击技能属性奖励应有 C1 A3 (203) 应答")
	}
	// Is Skill Combo Available 不在职业 StatAttributes 里 → 走常量元素分支（不折进 overrides）。
	if len(c.AttributeBonuses) != 1 || c.AttributeBonuses[0].AttributeID != comboSkillAttributeID {
		t.Fatalf("应只记一条连击属性加成: %+v", c.AttributeBonuses)
	}
	if got := player.AttributeValue(srv.deps.cfg.GameConfig, c, "Is Skill Combo Available"); got != 1 {
		t.Fatalf("连击可用属性应为 1, got %v", got)
	}
}

// TestQuestSkillReward 组 0/2（Muse Elf，职业 10）→ 学 Infinity Arrow(77)。
func TestQuestSkillReward(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, 225)
	c.ClassNumber = 10
	sess.setOpenedNpc(questNpc(229))
	c.Stats.Money = 10_000_000
	if c.Inventory == nil {
		c.Inventory = storage.NewInventory(0)
	}
	addItem(c, srv, 14, 23, 1)
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(0, 2, c2s.QuestProceedAction_AcceptQuest))
	srv.handleQuestPacket(sess, 0x0D, questCompletionFrame(t, 0, 2))
	if !learnedSkillContains(c, 77) {
		t.Fatalf("完成应学到 Infinity Arrow: %+v", c.LearnedSkills)
	}
}

// TestQuestActiveAndAvailableLists 0x1A 与 0x30：空清单也发；0x30 带当前对话 NPC 号。
func TestQuestActiveAndAvailableLists(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, spiderHunterLevel)
	sess.setOpenedNpc(questNpc(257))

	srv.handleQuestPacket(sess, 0x1A, nil) // 清单请求无参数
	if findQuestFrame(rec, 0xC1, 0x1A) == nil {
		t.Fatal("无进行中任务也应回 C1 F6 1A（对照 CurrentlyActiveQuestsPlugIn 无提前返回）")
	}
	srv.handleQuestPacket(sess, 0x30, nil)
	f := findQuestFrame(rec, 0xC1, 0x0A)
	if f == nil {
		t.Fatal("0x30 应答 AvailableQuests")
	}
	aq := s2c.AsAvailableQuests(f)
	if got := aq.QuestNpcNumber(); got != 257 {
		t.Fatalf("应答应带 NPC 号, got %d", got)
	}
	// 10 级的职业 4 角色只有 18/1 合格（等级区间 1..14、无职业限定），条目写 StartingNumber。
	if got := aq.QuestCount(); got != 1 {
		t.Fatalf("可接清单应只有 1 条, got %d", got)
	}
	if e := aq.Quests(0); e == nil || e.Group() != 18 || e.Number() != 0 {
		t.Fatalf("清单项应是 (18, starting 0), got %+v", e)
	}
	// 没开 NPC 时 0x30 不应答（对照 ShowAvailableQuestsPlugIn 的 OpenedNpc 判空）。
	sess.setOpenedNpc(nil)
	rec.frames = nil
	srv.handleQuestPacket(sess, 0x30, nil)
	if findQuestFrame(rec, 0xC1, 0x0A) != nil {
		t.Fatal("未开 NPC 不应答应接清单")
	}
}

// TestQuestStateRequestUsesExtendedForMuMain 钉住 20404 的 C2 Extended 变体与包长。
func TestQuestStateRequestUsesExtendedForMuMain(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, spiderHunterLevel)
	sess.setOpenedNpc(questNpc(257))
	srv.handleQuestPacket(sess, 0x0B, questProceedFrame(18, 1, c2s.QuestProceedAction_AcceptQuest))
	rec.frames = nil
	srf := c2s.NewQuestStateRequest()
	srf.SetQuestGroup(18)
	srf.SetQuestNumber(1)
	srv.handleQuestPacket(sess, 0x1B, srf.Bytes())

	f := findQuestFrame(rec, 0xC2, 0x1B)
	if f == nil {
		t.Fatal("0x1B 应答应是 C2 Extended")
	}
	if len(f) != 272 {
		t.Fatalf("QuestStateExtended 应为 272 字节, got %d", len(f))
	}
	st := s2c.AsQuestStateExtended(f)
	if st.QuestNumber() != 1 || st.QuestGroup() != 18 {
		t.Fatalf("任务标识错误: num=%d group=%d", st.QuestNumber(), st.QuestGroup())
	}
	if st.RewardsCount() != 1 {
		t.Fatalf("18/1 有一条经验奖励, got %d", st.RewardsCount())
	}
	r0 := st.Rewards(0)
	if r0.Type() != s2c.RewardType_Experience || r0.RewardCount() != 2000 {
		t.Fatalf("奖励应是经验 2000: type=%d count=%d", r0.Type(), r0.RewardCount())
	}
	if c.QuestStates[0].ActiveQuestIndex < 0 {
		t.Fatal("前置：任务应仍进行中")
	}
}

// TestQuestEventResponseAnswers 0x21 → C1 F6 03，两条固定条目。
func TestQuestEventResponseAnswers(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, spiderHunterLevel)
	srv.handleQuestPacket(sess, 0x21, nil)
	f := findQuestFrame(rec, 0xC1, 0x03)
	if f == nil {
		t.Fatal("0x21 应回 QuestEventResponse")
	}
	if len(f) != s2c.QuestEventResponseLength {
		t.Fatalf("包长应为 %d, got %d", s2c.QuestEventResponseLength, len(f))
	}
}

// TestQuestDataScale 是数据真相守卫：导出件必须是原版那 499 条、4 个组、4 个给予者 NPC。
func TestQuestDataScale(t *testing.T) {
	gc, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(gc.Quests); got != 499 {
		t.Fatalf("任务总数应为 499（Quests.cs 483 + 16 legacy）, got %d", got)
	}
	for group, count := range map[int]int{0: 16, 15: 120, 18: 243, 19: 120} {
		if got := len(gc.QuestsByGroup(group)); got != count {
			t.Fatalf("组 %d 应有 %d 条, got %d", group, count, got)
		}
	}
	if got := len(gc.QuestsForNpc(257)); got != 483 {
		t.Fatalf("257 号 NPC 应挂 483 条, got %d", got)
	}
	// 同一 (group, number) 会按职业有多条：职业筛选必须挑出正确那条。
	dk, ok := gc.QuestAtNpc(235, 0, 0, 4)
	if !ok || dk.Name != "Find the 'Scroll of Emperor' (Dark Knight)" {
		t.Fatalf("职业 4 应拿到 Dark Knight 变体, got %+v", dk)
	}
	if _, ok := gc.QuestAtNpc(235, 0, 0, 16); ok {
		t.Fatal("职业 16 不该拿到 0/0 的任一变体")
	}
	// "不进 0x1B 奖励表"的那几类奖励必须仍可解析并计数。
	var evolution, attribute, skill, levelUp int
	for i := range gc.Quests {
		for _, rw := range gc.Quests[i].Rewards {
			switch rw.Type {
			case config.QuestRewardEvolutionFirstToSecond:
				evolution++
			case config.QuestRewardAttribute:
				attribute++
			case config.QuestRewardSkill:
				skill++
			case config.QuestRewardLevelUpPoints:
				levelUp++
			}
		}
	}
	if evolution != 4 || attribute != 9 || skill != 1 || levelUp != 11 {
		t.Fatalf("奖励类型计数错误: evolution=%d attribute=%d skill=%d levelUp=%d",
			evolution, attribute, skill, levelUp)
	}
}
