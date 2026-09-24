package gameserver

import (
	"testing"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// castBuffSkill 发送 C3 0x19 定向技能帧（目标=自己），驱动 buff 施放。
func castBuffSkill(srv *Server, sess *session, skillID uint16) {
	p := c2s.NewTargetedSkill()
	p.SetSkillId(skillID)
	p.SetTargetId(sess.getWorldPlayer().ID)
	srv.handleTargetedSkill(sess, p.Bytes())
}

// TestBuffSkillActivates 验证生命之光（skill 48 → effect#8）完整链路：
// 下发 C1 07 激活帧（自己=0x200、effectId=8），效果入表，
// MaximumHealth 经 SwellLifeHealthIncrease 乘法项放大约 12%。
func TestBuffSkillActivates(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	before := sess.getCombatValues().MaxHealth

	castBuffSkill(srv, sess, 48)

	f := findFrame(rec, 0xC1, 0x07)
	if f == nil {
		t.Fatal("未下发 C1 07 MagicEffectStatus")
	}
	st := s2c.AsMagicEffectStatus(f)
	if !st.IsActive() {
		t.Fatal("IsActive 未置位")
	}
	if st.PlayerId() != 0x200 {
		t.Fatalf("PlayerId=%d，期望自己 0x200", st.PlayerId())
	}
	if st.EffectId() != 8 {
		t.Fatalf("EffectId=%d，期望 8（生命之光）", st.EffectId())
	}
	if sess.peekEffects().Len() != 1 {
		t.Fatalf("活动效果数=%d，期望 1", sess.peekEffects().Len())
	}

	after := sess.getCombatValues().MaxHealth
	if after <= before {
		t.Fatalf("生命之光后 MaxHealth 未提升: %.0f → %.0f", before, after)
	}
	if ratio := float64(after) / float64(before); ratio < 1.11 || ratio > 1.20 {
		t.Fatalf("MaxHealth 倍率=%.3f，期望约 1.12", ratio)
	}
}

// TestBuffClearedOnDeath 验证死亡重生时 StopByDeath 的 buff 被清除并去激活。
func TestBuffClearedOnDeath(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	castBuffSkill(srv, sess, 48)
	if sess.peekEffects().Len() != 1 {
		t.Fatal("前置：应有 1 个效果")
	}

	srv.respawnDeadPlayer(sess)

	if sess.peekEffects().Len() != 0 {
		t.Fatalf("死亡重生后效果数=%d，期望 0", sess.peekEffects().Len())
	}
	f := findFrame(rec, 0xC1, 0x07)
	if f == nil || s2c.AsMagicEffectStatus(f).IsActive() {
		t.Fatal("应下发去激活 C1 07（IsActive=false）")
	}
}

// TestBuffUnknownRejected 验证不可用的 buff（非本职业、未学习）不下发效果。
func TestBuffUnknownRejected(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	// skill 16 灵魂屏障是魔法师技能，DK 不合格也未学习 → 拒绝。
	castBuffSkill(srv, sess, 16)
	if el := sess.peekEffects(); el != nil && el.Len() > 0 {
		t.Fatal("不可用的 buff 不应加入效果表")
	}
	if findFrame(rec, 0xC1, 0x07) != nil {
		t.Fatal("不可用的 buff 不应下发 C1 07")
	}
}
