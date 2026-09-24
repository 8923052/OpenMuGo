package gameserver

// handler_learn_skill_test.go —— 右键技能书/卷轴（group15）学习技能的测试。
// 对照原版 LearnablesConsumeHandlerPlugIn：技能号映射、需求（能量/等级/职业）、
// 已学拒绝、扣耐久/销毁、SkillListUpdate（C1 F3 11）下发。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
)

// skillListFrames 统计 C1 F3 11（SkillListUpdate）帧。
func skillListFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0xF3 && f[3] == 0x11 {
			out = append(out, f)
		}
	}
	return out
}

// TestLearnSkillFromScroll 主路径：魔法师（class0）能量充足，右键毒咒书
// （15,0 → skill 1，需 energy 140）→ 学得技能、消耗书本、下发技能列表。
func TestLearnSkillFromScroll(t *testing.T) {
	m := newMoveScaffold(t, 0, entity.CharStats{Energy: 300})
	m.c.Level = 1

	// Scroll of Poison：group15 number0，durability=1。
	scroll := &item.Item{Group: 15, Number: 0, Durability: 1}
	m.putItem(t, 12, scroll)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	// 已学技能含 skill 1。
	if !learnedSkillContains(m.c, 1) {
		t.Fatalf("应已学得技能 1，got %v", m.c.LearnedSkills)
	}
	// 书本用尽（durability1→0）→ 销毁，背包槽空。
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("技能书应用尽并移除")
	}
	if n := countFrames(m.rec, 0xC1, 0x28); n != 1 {
		t.Fatalf("应发 C1 28（书本销毁）, got %d", n)
	}
	// 下发 SkillListUpdate。
	updates := skillListFrames(m.rec)
	if len(updates) != 1 {
		t.Fatalf("应下发 1 帧 C1 F3 11 SkillListUpdate, got %d", len(updates))
	}
}

// TestLearnSkillRejectedInsufficientEnergy 需求不满足（能量不足）→ 不学、不消耗。
func TestLearnSkillRejectedInsufficientEnergy(t *testing.T) {
	m := newMoveScaffold(t, 0, entity.CharStats{Energy: 10}) // 毒咒书需 140
	m.c.Level = 1
	m.putItem(t, 12, &item.Item{Group: 15, Number: 0, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if len(m.c.LearnedSkills) != 0 {
		t.Fatal("能量不足不应学得技能")
	}
	// 物品保留（未扣耐久）。
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("需求不满足不应消耗书本")
	}
	if n := len(skillListFrames(m.rec)); n != 0 {
		t.Fatalf("学习失败不应下发 SkillListUpdate, got %d", n)
	}
}

// TestLearnSkillRejectedAlreadyLearned 已学过 → 拒绝且不消耗。
func TestLearnSkillRejectedAlreadyLearned(t *testing.T) {
	m := newMoveScaffold(t, 0, entity.CharStats{Energy: 300})
	m.c.Level = 1
	m.c.LearnedSkills = []entity.LearnedSkill{{SkillNumber: 1}}
	m.putItem(t, 12, &item.Item{Group: 15, Number: 0, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if len(m.c.LearnedSkills) != 1 {
		t.Fatal("已学技能不应重复加入")
	}
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("已学过不应消耗书本")
	}
}

// TestLearnSkillFromOrb 技能石（group12 Orb）路径：使用治疗之石学习治疗技能。
// Orb of Healing（12,8 → skill26），不可穿戴、带技能、合格职业含 class8。
func TestLearnSkillFromOrb(t *testing.T) {
	m := newMoveScaffold(t, 8, entity.CharStats{Energy: 300})
	m.c.Level = 40
	m.putItem(t, 12, &item.Item{Group: 12, Number: 8, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if !learnedSkillContains(m.c, 26) {
		t.Fatalf("应通过技能石学得技能 26，got %v", m.c.LearnedSkills)
	}
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("技能石应用尽并移除")
	}
	if n := len(skillListFrames(m.rec)); n != 1 {
		t.Fatalf("应下发 SkillListUpdate, got %d", n)
	}
}

// TestLearnSkillRejectedWrongClass 职业不合格 → 拒绝。
func TestLearnSkillRejectedWrongClass(t *testing.T) {
	// DK(class4) 不在毒咒书 QualifiedClasses（0/2/3/12/13）中。
	m := newMoveScaffold(t, 4, entity.CharStats{Energy: 3000})
	m.c.Level = 1
	m.putItem(t, 12, &item.Item{Group: 15, Number: 0, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if len(m.c.LearnedSkills) != 0 || m.c.Inventory.GetItem(12) == nil {
		t.Fatal("职业不合格不应学习或消耗")
	}
}

// hasSkillNumber 判定技能列表视图里是否含指定技能号。
func hasSkillNumber(list []action.SkillListView, number uint16) bool {
	for _, s := range list {
		if s.SkillNumber == number {
			return true
		}
	}
	return false
}

// TestEquipWeaponAddsItsSkillToSkillList 锁定"武器技能进技能栏"：
// 对照原版 SkillList 构造，可用技能 = 已学技能 + **已装备物品自带技能**
// （`Inventory.EquippedItems.Where(item => item.HasSkill)` → AddItemSkillAsync，
// 且技能对本职业合格）。只发 LearnedSkills 会让武器技能在技能栏里缺失。
// Light Saber(0,10) 自带 skill 22（Cyclone，合格职业含 DK=4）。
func TestEquipWeaponAddsItsSkillToSkillList(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	m.putItem(t, 12, &item.Item{Group: 0, Number: 10, Level: 0, Durability: 50})

	if hasSkillNumber(m.srv.skillListViewOf(m.c), 22) {
		t.Fatal("未装备武器时不应含武器技能 22")
	}

	// 穿上（左手槽 0）→ 技能列表含 22，且下发 C1 F3 11 刷新。
	m.move(0, 12, 0, byte(item.SlotLeftHand))
	if !hasSkillNumber(m.srv.skillListViewOf(m.c), 22) {
		t.Fatal("装备 Light Saber 后技能列表应含武器技能 22（Cyclone）")
	}
	if n := len(skillListFrames(m.rec)); n == 0 {
		t.Fatal("装备自带技能的物品后应下发 SkillListUpdate")
	}

	// 卸下 → 技能随之移除（原版 RemoveItemSkillAsync）。
	m.move(0, byte(item.SlotLeftHand), 0, 20)
	if hasSkillNumber(m.srv.skillListViewOf(m.c), 22) {
		t.Fatal("卸下武器后技能列表不应再含 22")
	}
}
