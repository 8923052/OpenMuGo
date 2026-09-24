package gameserver

// handler_combo_test.go —— TRIM-06 连击端到端：Blade Knight(6) 在有"连击可用"属性时
// 按 19 → 41 → 43 顺序出手，终结那一手额外下发技能号 59 的动画包
// （原版 ShowSkillAnimationPlugIn.cs:29/64-69 的 ComboSkillId，无专用协议）。
//
// 顺序 2 的 41 Twisting Slash 是 AreaSkillAutomaticHits(3) → 走 C3 0x1E；
// 起手 19 与终结 43（DirectHit + ExplicitWithImplicitInRange 溅射）走 C3 0x19
// —— 与游戏里"落斩 → 旋风斩 → 刺穿"的实际节奏一致。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// comboGranted 给会话角色装上任务奖励的"连击可用"属性。
func comboGranted(c *entity.Character) {
	c.AttributeBonuses = append(c.AttributeBonuses, entity.AttributeBonus{
		AttributeID: comboSkillAttributeID, Designation: "Is Skill Combo Available", Value: 1,
	})
}

// newComboScaffold 造一个能连满三手的 Blade Knight 施法现场，补三件脚手架没管的事：
//   - wp.IsAlive：正常由 handler_enter 置真，本脚手架绕过了它；
//   - 目标换成全图血最厚的怪并挪到施法者脚下——定向路径的连击门要求双方都不在安全区
//     且都活着（TargetedSkillDefaultPlugin.cs:256-265），250 级战士两下就把默认那只弱怪
//     打死了；
//   - 属性奖励之后重算战斗快照（任务发奖路径 handler_quest.go:672 同样这么做）。
func newComboScaffold(t *testing.T) *skillScaffold {
	t.Helper()
	sc := newSkillScaffold(t, 6, 2000, 2000)
	sc.c.Level = 250 // 43 Death Stab 有 160 级门槛
	sc.wp.IsAlive = true
	comboGranted(sc.c)

	sc.target.ID = skillTestNpcID - 1 // 让出固定 ID，避免 findNpcTarget 命中两只
	toughest := sc.target
	for _, n := range sc.srv.deps.cfg.NPCs.ByMap(0) {
		if n.Alive() && n.IsAttackableByPlayer() &&
			n.Attribute("Maximum Health") > toughest.Attribute("Maximum Health") {
			toughest = n
		}
	}
	toughest.ID = skillTestNpcID
	toughest.X, toughest.Y = sc.wp.X, sc.wp.Y
	sc.target = toughest

	mp := sc.srv.world.Map(0)
	if mp.Safezone(sc.wp.X, sc.wp.Y) || mp.Safezone(toughest.X, toughest.Y) {
		t.Fatal("连击定向门要求双方都不在安全区")
	}
	sc.srv.refreshCombatValues(sc.sess, sc.c, false)
	return sc
}

// skillAnimationSeen 统计这一帧里出现过指定技能号的施法动画包（C3 19）。
// C3 包布局是 [类型, 长度, 码, ...]，故码在 d[2]。
func skillAnimationSeen(rec *packetRecorder, skill uint16) int {
	n := 0
	for _, f := range rec.frames {
		if len(f) < 5 || f[0] != 0xC3 || f[2] != 0x19 {
			continue
		}
		if s2c.AsSkillAnimation(f).SkillId() == skill {
			n++
		}
	}
	return n
}

func castTargetedSkill(sc *skillScaffold, skill int) {
	req := c2s.NewTargetedSkill()
	req.SetSkillId(uint16(skill))
	req.SetTargetId(skillTestNpcID)
	sc.srv.handleTargetedSkill(sc.sess, req.Bytes())
}

func castAreaSkill(sc *skillScaffold, skill int) {
	req := c2s.NewAreaSkill()
	req.SetSkillId(uint16(skill))
	req.SetTargetX(sc.target.X)
	req.SetTargetY(sc.target.Y)
	req.SetRotation(0)
	sc.srv.handleAreaSkill(sc.sess, req.Bytes())
}

func TestComboFullSequenceSendsSkill59(t *testing.T) {
	sc := newComboScaffold(t)

	if sc.sess.combo != nil {
		t.Fatal("状态机应在第一次出手时才建立")
	}
	castTargetedSkill(sc, 19)
	if sc.sess.combo == nil {
		t.Fatal("有连击属性时出手应建立状态机")
	}
	if order, last := sc.sess.combo.Progress(); order != 1 || last != 19 {
		t.Fatalf("起手后进度 %d/%d，期望 1/19", order, last)
	}
	if n := skillAnimationSeen(sc.rec, comboAnimationSkill); n != 0 {
		t.Fatalf("第一手不该有 59 动画，got %d", n)
	}
	if n := skillAnimationSeen(sc.rec, 19); n != 1 {
		t.Fatalf("第一手应有自己那一招的动画，got %d", n)
	}

	castAreaSkill(sc, 41)
	if order, last := sc.sess.combo.Progress(); order != 2 || last != 41 {
		t.Fatalf("第二手后进度 %d/%d，期望 2/41", order, last)
	}

	sc.rec.frames = nil
	castTargetedSkill(sc, 43) // 顺序 3 的终结步
	if order, last := sc.sess.combo.Progress(); order != 0 || last != 0 {
		t.Fatalf("终结后应回起始态，got %d/%d", order, last)
	}
	if n := skillAnimationSeen(sc.rec, comboAnimationSkill); n != 1 {
		t.Fatalf("终结手应恰好一条 59 动画，got %d", n)
	}
	// 终结手仍然照发自己那一招的动画（59 是额外的一条，且在它之前）。
	if n := skillAnimationSeen(sc.rec, 43); n != 1 {
		t.Fatalf("终结手自己的动画缺失，got %d", n)
	}
}

func TestComboBrokenSequenceDoesNotFinish(t *testing.T) {
	sc := newComboScaffold(t)

	castAreaSkill(sc, 41) // 直接放第二手 → 错序
	if sc.sess.combo == nil {
		t.Fatal("错序也应建立状态机（只是判定失败）")
	}
	if order, _ := sc.sess.combo.Progress(); order != 0 {
		t.Fatalf("错序后应停在起始态，got %d", order)
	}
	if n := skillAnimationSeen(sc.rec, comboAnimationSkill); n != 0 {
		t.Fatalf("错序不该发 59，got %d", n)
	}

	// 重来：19 → 19 同招两下也要复位。
	castTargetedSkill(sc, 19)
	sc.rec.frames = nil
	castTargetedSkill(sc, 19)
	if order, _ := sc.sess.combo.Progress(); order != 0 {
		t.Fatalf("同招两下后应复位，got %d", order)
	}
	if n := skillAnimationSeen(sc.rec, comboAnimationSkill); n != 0 {
		t.Fatalf("复位不该发 59，got %d", n)
	}
}

func TestComboNeedsAttribute(t *testing.T) {
	sc := newComboScaffold(t)
	sc.c.AttributeBonuses = nil // 没做"连击秘诀"任务 → 属性为 0
	sc.srv.refreshCombatValues(sc.sess, sc.c, false)
	// 出手不推进状态机（原版 Player.cs:373 的门）。
	castTargetedSkill(sc, 19)
	castAreaSkill(sc, 41)
	castTargetedSkill(sc, 43)
	if sc.sess.combo != nil {
		order, last := sc.sess.combo.Progress()
		t.Fatalf("无属性不该建状态机，got %d/%d", order, last)
	}
	if n := skillAnimationSeen(sc.rec, comboAnimationSkill); n != 0 {
		t.Fatalf("无属性不该发 59，got %d", n)
	}
}
