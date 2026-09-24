package gameserver

// handler_stats_test.go —— T2-8 端到端：F3 06 分配点数 → Base* 落账 →
// 属性图联动（Maximum* 实时变化）→ C1 F3 06 扩展 24B 下发；点数不足拒绝。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// statsScaffold 是加点测试脚手架（无 NPC 依赖）。
type statsScaffold struct {
	srv  *Server
	sess *session
	wp   *world.Player
	c    *entity.Character
	rec  *packetRecorder
}

func newStatsScaffold(t *testing.T, points uint16) *statsScaffold {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "stats", 120, 125, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{
		LevelUpPoints: points,
		Strength:      100, Agility: 20, Vitality: 25, Energy: 10, Leadership: 0,
	}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return &statsScaffold{srv: srv, sess: sess, c: c, rec: rec}
}

func (sc *statsScaffold) send(stat c2s.CharacterStatAttribute) {
	req := c2s.NewIncreaseCharacterStatPoint()
	req.SetStatType(stat)
	sc.srv.handleF3(sc.sess, 0x06, req.Bytes())
}

// TestStatIncreaseHandler 验证主路径：力量 +1、点数 -1、扩展结果帧。
func TestStatIncreaseHandler(t *testing.T) {
	sc := newStatsScaffold(t, 5)

	sc.send(c2s.CharacterStatAttribute_Strength)

	if sc.c.Stats.Strength != 101 {
		t.Fatalf("力量应 +1, got %d", sc.c.Stats.Strength)
	}
	if sc.c.Stats.LevelUpPoints != 4 {
		t.Fatalf("剩余点数应为 4, got %d", sc.c.Stats.LevelUpPoints)
	}
	if n := countFrames(sc.rec, 0xC1, 0xF3); n != 1 {
		t.Fatalf("应下发 1 帧 C1 F3 06, got %d", n)
	}
	frame := findFrame(sc.rec, 0xC1, 0xF3)
	if len(frame) != s2c.CharacterStatIncreaseResponseExtendedLength {
		t.Fatalf("扩展结果帧应为 %d 字节（S6 恒扩展）, got %d", s2c.CharacterStatIncreaseResponseExtendedLength, len(frame))
	}
	p := s2c.AsCharacterStatIncreaseResponseExtended(frame)
	if p.Attribute() != s2c.CharacterStatAttribute_Strength || p.AddedAmount() != 1 {
		t.Fatalf("attr=%d added=%d", p.Attribute(), p.AddedAmount())
	}
	// 属性图联动：四项新上限应为真实解析值（非 0）。
	if p.UpdatedMaximumHealth() == 0 || p.UpdatedMaximumHealth() != uint32(sc.c.Stats.MaximumHealth) {
		t.Fatalf("响应帧 MaxHP=%d 与角色态 %d 不一致", p.UpdatedMaximumHealth(), sc.c.Stats.MaximumHealth)
	}
}

// TestStatIncreaseVitalityLinksMaxHP 验证属性图联动：加体力 → 最大生命实时上升。
func TestStatIncreaseVitalityLinksMaxHP(t *testing.T) {
	sc := newStatsScaffold(t, 5)
	// 先解析一次取基线（进图同款语义）。
	if resolved, err := sc.srv.resolveCharStats(sc.c); err != nil {
		t.Fatal(err)
	} else {
		sc.c.Stats = resolved
		sc.c.Stats.LevelUpPoints = 5
	}
	baseline := sc.c.Stats.MaximumHealth

	sc.send(c2s.CharacterStatAttribute_Vitality)

	if sc.c.Stats.Vitality != 26 {
		t.Fatalf("体力应 +1, got %d", sc.c.Stats.Vitality)
	}
	if sc.c.Stats.MaximumHealth <= baseline {
		t.Fatalf("加体力后 MaxHP 应上升: baseline=%d now=%d", baseline, sc.c.Stats.MaximumHealth)
	}
}

// TestStatIncreaseRejectedNoPoints 点数不足：不落账、不回包（原版无确认帧）。
func TestStatIncreaseRejectedNoPoints(t *testing.T) {
	sc := newStatsScaffold(t, 0)

	sc.send(c2s.CharacterStatAttribute_Energy)

	if sc.c.Stats.Energy != 10 || sc.c.Stats.LevelUpPoints != 0 {
		t.Fatalf("拒绝时不得落账: ene=%d points=%d", sc.c.Stats.Energy, sc.c.Stats.LevelUpPoints)
	}
	if n := countFrames(sc.rec, 0xC1, 0xF3); n != 0 {
		t.Fatalf("拒绝不应回确认帧, got %d", n)
	}
}

// TestStatIncreaseRejectedUnknownStat 未知属性（>4）：拒绝。
func TestStatIncreaseRejectedUnknownStat(t *testing.T) {
	sc := newStatsScaffold(t, 5)

	sc.send(c2s.CharacterStatAttribute(9))

	if sc.c.Stats.LevelUpPoints != 5 {
		t.Fatalf("未知属性不得扣点: %d", sc.c.Stats.LevelUpPoints)
	}
	if n := countFrames(sc.rec, 0xC1, 0xF3); n != 0 {
		t.Fatalf("未知属性不应回确认帧, got %d", n)
	}
}
