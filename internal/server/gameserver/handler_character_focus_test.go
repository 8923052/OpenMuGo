package gameserver

// handler_character_focus_test.go —— C1 F3 15 聚焦角色 与 C1 F3 30 保存键位
// （doc/17 S-3 批次 1）。对照 FocusCharacterAction（只在选角态、按名查本账号角色）
// 与 SaveKeyConfigurationAction（只存 blob，无回包）。

import (
	"bytes"
	"testing"

	"mugo/internal/gamelogic/entity"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// focusScaffold 借经验脚手架建会话后把状态退回"已登录、未选角"，并挂两个角色。
func focusScaffold(t *testing.T) (*Server, *session, *packetRecorder) {
	t.Helper()
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	acc := sess.getAccount()
	acc.Characters = []entity.Character{{Name: "alpha"}, {Name: "beta"}}
	sess.setState(entity.StateAuthenticated)
	return srv, sess, rec
}

func focusFrame(t *testing.T, name string) []byte {
	t.Helper()
	p := c2s.NewFocusCharacter()
	// 生成物按固定 14B 布局，名字字段在 [4:14]。
	copy(p.Bytes()[4:14], name)
	return p.Bytes()
}

// TestFocusCharacterEchoesName 验证选角界面聚焦已知角色：回 C1 F3 15 且角色名一致。
func TestFocusCharacterEchoesName(t *testing.T) {
	srv, sess, rec := focusScaffold(t)

	srv.handleF3(sess, 0x15, focusFrame(t, "alpha"))

	f := findFrameSub(rec, 0xC1, 0xF3, 0x15)
	if f == nil {
		t.Fatal("未下发 C1 F3 15 CharacterFocused")
	}
	if got := s2c.AsCharacterFocused(f).CharacterNameString(); got != "alpha" {
		t.Fatalf("回显角色名=%q，期望 alpha", got)
	}
}

// TestFocusCharacterRejectsUnknownAndEnteredWorld 验证两条原版拒绝分支：
// 名字不在本账号角色里 → 不回包；已进图（非 CharacterSelection）→ 不回包。
func TestFocusCharacterRejectsUnknownAndEnteredWorld(t *testing.T) {
	srv, sess, rec := focusScaffold(t)

	srv.handleF3(sess, 0x15, focusFrame(t, "nobody"))
	if findFrameSub(rec, 0xC1, 0xF3, 0x15) != nil {
		t.Fatal("非本账号角色不应回包")
	}

	sess.setState(entity.StateEnteredWorld)
	srv.handleF3(sess, 0x15, focusFrame(t, "alpha"))
	if findFrameSub(rec, 0xC1, 0xF3, 0x15) != nil {
		t.Fatal("已进图时不应回包")
	}
}

// TestSaveKeyConfigurationStoresBlob 验证 F3 30 把键位 blob 原样存到角色，且不发包。
func TestSaveKeyConfigurationStoresBlob(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	p := c2s.NewSaveKeyConfiguration(48)
	payload := []byte{0x01, 0x02, 0x03, 0xFF, 0x7F}
	copy(p.Bytes()[4:], payload)
	framesBefore := len(rec.frames)

	srv.handleF3(sess, 0x30, p.Bytes())

	if !bytes.HasPrefix(c.KeyConfiguration, payload) {
		t.Fatalf("键位 blob=%X，期望以 %X 开头", c.KeyConfiguration, payload)
	}
	if len(c.KeyConfiguration) != 44 {
		t.Fatalf("blob 长度=%d，期望 44（48-4）", len(c.KeyConfiguration))
	}
	if len(rec.frames) != framesBefore {
		t.Fatal("保存键位不应下发任何包")
	}
}
