package gameserver

import (
	"testing"

	c2s "mugo/internal/proto/c2s"
)

// TestRegenerationHealSelf 验证 Heal(skill 26, SkillType.Regeneration) 完整链路：
// 扣 MP → 按 Heal Effect(-2) 的 "Current Health" 恢复项给当前血加值并钳到上限 →
// 下发 C1 26 FF。目标恒为自己（本仓无队伍/PvP 目标解析）。
func TestRegenerationHealSelf(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	c.ClassNumber = 8 // Elf 系：Heal 合格职业
	wp := sess.getWorldPlayer()

	c.Stats.CurrentMana = 5000
	c.Stats.CurrentHealth = 10
	beforeMax := c.Stats.MaximumHealth

	req := c2s.NewTargetedSkill()
	req.SetSkillId(26)
	req.SetTargetId(wp.ID)
	srv.handleTargetedSkill(sess, req.Bytes())

	if c.Stats.CurrentHealth <= 10 {
		t.Fatalf("Heal 未回血: hp=%d", c.Stats.CurrentHealth)
	}
	if c.Stats.CurrentHealth > c.Stats.MaximumHealth {
		t.Fatalf("回血越过上限: hp=%d max=%d", c.Stats.CurrentHealth, c.Stats.MaximumHealth)
	}
	if c.Stats.MaximumHealth != beforeMax {
		t.Fatal("Heal 不应改变最大血量")
	}
	if c.Stats.CurrentMana >= 5000 {
		t.Fatalf("Heal 未扣 MP: mana=%d", c.Stats.CurrentMana)
	}
	if findFrame(rec, 0xC1, 0x26) == nil {
		t.Fatal("未下发 C1 26 当前属性更新")
	}
}

// TestRegenerationHealClampsToMax 验证满血时 Heal 只扣蓝、血量钳在上限不外溢。
func TestRegenerationHealClampsToMax(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, 200)
	c.ClassNumber = 8
	wp := sess.getWorldPlayer()
	c.Stats.CurrentMana = 5000
	c.Stats.CurrentHealth = c.Stats.MaximumHealth

	req := c2s.NewTargetedSkill()
	req.SetSkillId(26)
	req.SetTargetId(wp.ID)
	srv.handleTargetedSkill(sess, req.Bytes())

	if c.Stats.CurrentHealth != c.Stats.MaximumHealth {
		t.Fatalf("满血回血应钳在上限: hp=%d max=%d", c.Stats.CurrentHealth, c.Stats.MaximumHealth)
	}
	if c.Stats.CurrentMana >= 5000 {
		t.Fatal("满血仍应扣 MP（原版消耗先于恢复）")
	}
}

// TestRegenerationRejectedWhenNotLearned 验证非 Elf 施放 Heal 被拒（不回血不扣蓝）。
func TestRegenerationRejectedWhenNotLearned(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	// 保持 DK(class 4)：Heal 非其合格技能 → 拒绝。
	wp := sess.getWorldPlayer()
	c.Stats.CurrentMana = 5000
	c.Stats.CurrentHealth = 10

	req := c2s.NewTargetedSkill()
	req.SetSkillId(26)
	req.SetTargetId(wp.ID)
	srv.handleTargetedSkill(sess, req.Bytes())

	if c.Stats.CurrentHealth != 10 {
		t.Fatalf("非合格职业的 Heal 不应回血: hp=%d", c.Stats.CurrentHealth)
	}
	if c.Stats.CurrentMana != 5000 {
		t.Fatal("被拒的施放不应扣 MP")
	}
	// 拒绝路径不应额外下发 C1 26（进图已发过的帧不计：这里只断言血量与蓝未变）。
	_ = rec
}
