package gameserver

// handler_magic_effect_cancel_test.go —— C1 1B MagicEffectCancelRequest（doc/17 S-3 批次 1）。
// 对照原版 MagicEffectCancelHandlerPlugIn：只有硬编码的 5 个技能号可取消，
// 且要求"角色会该技能" + "身上挂着该技能的效果"，否则整包忽略。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// seedEffect 按原版激活链路把一个效果挂到会话上（走同一个 CreateMagicEffect + Add）。
func seedEffect(t *testing.T, srv *Server, sess *session, effectNumber int) {
	t.Helper()
	c := sess.getSelected()
	e, _, ok := player.CreateMagicEffect(srv.deps.cfg.GameConfig, c, effectNumber, 0, true)
	if !ok {
		t.Fatalf("效果 %d 不存在", effectNumber)
	}
	if !sess.getEffects().Add(e, currentMs()) {
		t.Fatalf("效果 %d 未能入表", effectNumber)
	}
}

// sendCancel 发一帧 C1 1B（只填 SkillId，PlayerId 原版不读）。
func sendCancel(srv *Server, sess *session, skillID uint16) {
	p := c2s.NewMagicEffectCancelRequest()
	p.SetSkillId(skillID)
	srv.handleMagicEffectCancel(sess, p.Bytes())
}

// TestMagicEffectCancelRemovesBuff 验证 Infinity Arrow（77 → 效果 6）可被主动取消：
// 出 C1 07 去激活（自己=0x200、effectId=6），效果表清空。
func TestMagicEffectCancelRemovesBuff(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: 77, Level: 1})
	seedEffect(t, srv, sess, 6)
	framesBefore := len(rec.frames)

	sendCancel(srv, sess, 77)

	if el := sess.peekEffects(); el == nil || el.Len() != 0 {
		t.Fatalf("取消后效果数=%d，期望 0", el.Len())
	}
	if len(rec.frames) <= framesBefore {
		t.Fatal("取消应下发一帧去激活")
	}
	f := findFrame(rec, 0xC1, 0x07)
	if f == nil {
		t.Fatal("未下发 C1 07")
	}
	st := s2c.AsMagicEffectStatus(f)
	if st.IsActive() {
		t.Fatal("去激活帧 IsActive 应为 false")
	}
	if st.PlayerId() != world.ConstantPlayerID {
		t.Fatalf("PlayerId=%d，期望自己哨兵 %d", st.PlayerId(), world.ConstantPlayerID)
	}
	if st.EffectId() != 6 {
		t.Fatalf("EffectId=%d，期望 6", st.EffectId())
	}
}

// TestMagicEffectCancelIgnoresNotCancellable 验证非硬编码技能号整包忽略：
// 生命之光（48 → 效果 8）在原版不在可取消名单里，取消后效果仍在。
func TestMagicEffectCancelIgnoresNotCancellable(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: 48, Level: 1})
	seedEffect(t, srv, sess, 8)
	framesBefore := len(rec.frames)

	sendCancel(srv, sess, 48)

	if sess.peekEffects().Len() != 1 {
		t.Fatal("不可取消的技能不应摘掉效果")
	}
	if len(rec.frames) != framesBefore {
		t.Fatal("不可取消的技能不应发包")
	}
}

// TestMagicEffectCancelRequiresKnownSkill 验证"会该技能"这一门：77 号在未学习且
// 职业不合格（DK≠10/11）时不得摘效果（原版 player.SkillList.ContainsSkill 分支）。
func TestMagicEffectCancelRequiresKnownSkill(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	seedEffect(t, srv, sess, 6)
	framesBefore := len(rec.frames)

	sendCancel(srv, sess, 77)

	if sess.peekEffects().Len() != 1 {
		t.Fatal("未学习该技能时不应摘效果")
	}
	if len(rec.frames) != framesBefore {
		t.Fatal("未学习该技能时不应发包")
	}
}

// TestMagicEffectCancelWithoutEffect 验证"身上没挂该效果"时安全返回（不 panic、不发包）。
func TestMagicEffectCancelWithoutEffect(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{SkillNumber: 77, Level: 1})
	framesBefore := len(rec.frames)

	sendCancel(srv, sess, 77)

	if len(rec.frames) != framesBefore {
		t.Fatal("无效果可取消时不应发包")
	}
}
