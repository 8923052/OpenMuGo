package gameserver

// handler_class_unlock_test.go —— 等级解锁可创建职业 + F3 00 的 UnlockFlags + C1 DE 00
// （doc/17 S-3 批次 3）。对照 UnlockCharacterAtLevelBase 的四个插件类与
// ShowCharacterListPlugIn.CreateUnlockFlags:44-50（标记 >0 才另发 DE 00）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// unlockRuleOf 按职业号取规则里的等级门槛（用例里避免把数字抄两遍）。
func unlockRuleOf(t *testing.T, classNumber int) int {
	t.Helper()
	for _, r := range action.ClassUnlockRules {
		if r.ClassNumber == classNumber {
			return r.MinimumLevel
		}
	}
	t.Fatalf("没有职业 %d 的解锁规则", classNumber)
	return 0
}

func gameConfigOrFatal(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestUnlockClassesAtLevelBands 验证四个门槛：DL@250、MG@220、RF@150、Summoner@1，
// 且 1 级账号就能拿到召唤士（原版 UnlockSummonerAtLevel1 的既成事实，照抄）。
func TestUnlockClassesAtLevelBands(t *testing.T) {
	gc := gameConfigOrFatal(t)
	acc := &entity.Account{Name: "zero"}

	unlocked, missing := action.UnlockClassesAtLevel(acc, gc, 1)
	if len(missing) != 0 {
		t.Fatalf("S6 配置应查得到全部四个职业，missing=%v", missing)
	}
	if len(unlocked) != 1 || unlocked[0] != 20 {
		t.Fatalf("1 级解锁结果=%v，期望只解 Summoner(20)", unlocked)
	}

	if got := action.CharacterCreationUnlockFlags(acc, gc); got != 1 {
		t.Fatalf("Summoner 的聚合标记=%d，期望 1", got)
	}

	// 再解到 220 级：MG(4) 与 RF(8) 依次进来，DL 仍差 250。
	action.UnlockClassesAtLevel(acc, gc, unlockRuleOf(t, 24))
	action.UnlockClassesAtLevel(acc, gc, unlockRuleOf(t, 12))
	if got := action.CharacterCreationUnlockFlags(acc, gc); got != 1|4|8 {
		t.Fatalf("220 级聚合标记=%d，期望 %d", got, 1|4|8)
	}
	action.UnlockClassesAtLevel(acc, gc, unlockRuleOf(t, 16))
	if got := action.CharacterCreationUnlockFlags(acc, gc); got != 1|2|4|8 {
		t.Fatalf("250 级聚合标记=%d，期望 15", got)
	}
}

// TestUnlockClassesIdempotent 验证重复升级不会把同一职业解两次
// （原版 All(c => c.Number != n) 的去重语义；重复插入会让账号写回撞唯一约束）。
func TestUnlockClassesIdempotent(t *testing.T) {
	gc := gameConfigOrFatal(t)
	acc := &entity.Account{Name: "twice"}
	first, _ := action.UnlockClassesAtLevel(acc, gc, 250)
	second, _ := action.UnlockClassesAtLevel(acc, gc, 300)
	if len(acc.UnlockedClasses) != len(first) {
		t.Fatalf("解锁表=%v，期望只有首次的 %d 项", acc.UnlockedClasses, len(first))
	}
	if len(second) != 0 {
		t.Fatalf("第二次不应再解锁任何职业，got %v", second)
	}
}

// TestCharacterListCarriesUnlockFlagsAndSendsDE00 验证出站点：F3 00 的 UnlockFlags
// 带聚合标记，且随后紧跟一条 C1 DE 00；标记为 0 时不发 DE 00。
func TestCharacterListCarriesUnlockFlagsAndSendsDE00(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	acc := sess.getAccount()
	acc.Characters = []entity.Character{{Name: "solo", Slot: 0, Level: 5}}
	sess.setState(entity.StateAuthenticated)
	action.UnlockClassesAtLevel(acc, srv.deps.cfg.GameConfig, 220)
	wantFlags := action.CharacterCreationUnlockFlags(acc, srv.deps.cfg.GameConfig)
	if wantFlags == 0 {
		t.Fatal("前置：220 级应至少解锁一个职业")
	}

	srv.handleCharacterList(sess, requestCharacterListFrame(t))

	list := findFrameSub(rec, 0xC1, 0xF3, 0x00)
	if list == nil {
		t.Fatal("未下发角色列表")
	}
	if got := s2c.AsCharacterListExtended(list).UnlockFlags(); byte(got) != wantFlags {
		t.Fatalf("F3 00 UnlockFlags=%d，期望 %d", got, wantFlags)
	}
	de := findFrame(rec, 0xC1, 0xDE)
	if de == nil {
		t.Fatal("标记 >0 时应紧跟一条 C1 DE 00")
	}
	if got := s2c.AsCharacterClassCreationUnlock(de).UnlockFlags(); byte(got) != wantFlags {
		t.Fatalf("DE 00 标记=%d，期望 %d", got, wantFlags)
	}
}

// TestCharacterListWithoutUnlocksSendsNoDE00 验证零标记时不发 DE 00（原版 if 分支）。
func TestCharacterListWithoutUnlocksSendsNoDE00(t *testing.T) {
	srv, sess, _, _, rec := newExpScaffold(t, 200)
	acc := sess.getAccount()
	acc.Characters = []entity.Character{{Name: "solo", Slot: 0, Level: 5}}
	sess.setState(entity.StateAuthenticated)

	srv.handleCharacterList(sess, requestCharacterListFrame(t))

	if findFrame(rec, 0xC1, 0xDE) != nil {
		t.Fatal("未解锁任何职业时不应发 DE 00")
	}
}

// requestCharacterListFrame 构造一帧 C1 F3 00（请求角色列表）。
func requestCharacterListFrame(t *testing.T) []byte {
	t.Helper()
	p := c2s.NewRequestCharacterList()
	p.SetLanguage(0)
	return p.Bytes()
}
