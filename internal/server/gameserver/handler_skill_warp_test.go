package gameserver

// handler_skill_warp_test.go —— T2-10/T2-11 端到端：
//   - C3 0x19 定向技能（消耗/动画/伤害）；C3 0x1E 区域技能（DK 合格 + 中心命中）；
//   - C1 8E 02 传送命令（扣款 + 换图链 + 等级/索引校验）。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/util"
)

const skillTestNpcID uint16 = 0x300

// skillScaffold 装配：真实导出件 + 全量 NPC + 一个可打目标（ID 固定）+ 施法者会话。
type skillScaffold struct {
	srv    *Server
	sess   *session
	wp     *world.Player
	c      *entity.Character
	rec    *packetRecorder
	target *npc.Npc
}

func newSkillScaffold(t *testing.T, class byte, mana, ability uint32) *skillScaffold {
	t.Helper()
	srv := newScopeTestSrv(t)
	sp := npc.NewSpawner(srv.deps.cfg.GameConfig, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	srv.deps.cfg.NPCs = sp
	var target *npc.Npc
	// 选防御率最低的弱怪：高防目标（Berdysh Guard 等）对 20 级 DW 命中率仅 3%，
	// 会掷出 MISS 干扰"掉血"断言。
	for _, n := range sp.ByMap(0) {
		if !n.Alive() || n.Attribute("Maximum Health") <= 0 {
			continue
		}
		if target == nil || n.Attribute("Defense Rate (PvM)") < target.Attribute("Defense Rate (PvM)") {
			target = n
		}
	}
	if target == nil {
		t.Fatal("Lorencia 应有可打目标")
	}
	target.ID = skillTestNpcID

	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "caster", target.X, target.Y, rec)
	c := sess.getSelected()
	c.ClassNumber = class
	wp.Class = class
	c.Stats = &entity.CharStats{Money: 1000000, CurrentMana: mana, CurrentAbility: ability}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)

	// 施法者必须不在安全区（原版 IsAtSafezone 拒放）——向目标周围找非安全区落点。
	mp := srv.world.Map(0)
	if mp.Safezone(wp.X, wp.Y) {
		placed := false
		for dx := -6; dx <= 6 && !placed; dx++ {
			for dy := -6; dy <= 6 && !placed; dy++ {
				nx, ny := byte(int(target.X)+dx), byte(int(target.Y)+dy)
				if mp.Walkable(nx, ny) && !mp.Safezone(nx, ny) {
					wp.X, wp.Y, c.X, c.Y = nx, ny, nx, ny
					placed = true
				}
			}
		}
		if !placed {
			t.Fatal("目标附近找不到非安全区落点")
		}
	}
	return &skillScaffold{srv: srv, sess: sess, wp: wp, c: c, rec: rec, target: target}
}

func TestTargetedSkillEnergyBall(t *testing.T) {
	sc := newSkillScaffold(t, 0, 1000, 1000)
	hpBefore := sc.target.Health()

	req := c2s.NewTargetedSkill()
	req.SetSkillId(17) // Energy Ball：魔法、range 6、MP 1
	req.SetTargetId(skillTestNpcID)
	sc.srv.handleTargetedSkill(sc.sess, req.Bytes())

	if sc.c.Stats.CurrentMana != 999 {
		t.Fatalf("应扣 MP 1: mana=%d", sc.c.Stats.CurrentMana)
	}
	if sc.target.Health() >= hpBefore {
		t.Fatalf("目标应掉血: before=%f after=%f", hpBefore, sc.target.Health())
	}
	// 动画广播（自己 + 观察者）：自己恒收哨兵 0x200。
	frame := findFrame(sc.rec, 0xC3, 0x19)
	if frame == nil {
		t.Fatal("应下发 C3 19 SkillAnimation")
	}
	anim := s2c.AsSkillAnimation(frame)
	if anim.SkillId() != 17 || anim.PlayerId() != world.ConstantPlayerID || anim.TargetId() != skillTestNpcID {
		t.Fatalf("anim=%+v", anim)
	}
}

func TestTargetedSkillRejectedNoMana(t *testing.T) {
	sc := newSkillScaffold(t, 0, 0, 1000)
	hpBefore := sc.target.Health()

	req := c2s.NewTargetedSkill()
	req.SetSkillId(17)
	req.SetTargetId(skillTestNpcID)
	sc.srv.handleTargetedSkill(sc.sess, req.Bytes())

	if sc.c.Stats.CurrentMana != 0 {
		t.Fatalf("拒绝不扣蓝: %d", sc.c.Stats.CurrentMana)
	}
	if sc.target.Health() != hpBefore {
		t.Fatal("拒绝不掉血")
	}
	if n := countFrames(sc.rec, 0xC3, 0x19); n != 0 {
		t.Fatalf("拒绝不应发动画帧, got %d", n)
	}
}

func TestTargetedSkillUnknownSkill(t *testing.T) {
	sc := newSkillScaffold(t, 0, 1000, 1000)
	req := c2s.NewTargetedSkill()
	req.SetSkillId(0xFFFF) // 不存在的技能号
	req.SetTargetId(skillTestNpcID)
	sc.srv.handleTargetedSkill(sc.sess, req.Bytes())
	if n := countFrames(sc.rec, 0xC3, 0x19); n != 0 {
		t.Fatalf("未知技能不应发动画帧, got %d", n)
	}
}

func TestAreaSkillTwistingSlash(t *testing.T) {
	// Twisting Slash (41)：DK/MG/RG 合格——用 DK(4)；MP 10 + AG 10，range 2。
	sc := newSkillScaffold(t, 4, 100, 100)

	req := c2s.NewAreaSkill()
	req.SetSkillId(41)
	req.SetTargetX(sc.target.X)
	req.SetTargetY(sc.target.Y)
	req.SetRotation(0)
	sc.srv.handleAreaSkill(sc.sess, req.Bytes())

	if sc.c.Stats.CurrentMana != 90 || sc.c.Stats.CurrentAbility != 90 {
		t.Fatalf("应扣 MP/AG 各 10: mana=%d ag=%d", sc.c.Stats.CurrentMana, sc.c.Stats.CurrentAbility)
	}
	frame := findFrame(sc.rec, 0xC3, 0x1E)
	if frame == nil {
		t.Fatal("应下发 C3 1E AreaSkillAnimation")
	}
	anim := s2c.AsAreaSkillAnimation(frame)
	if anim.SkillId() != 41 || anim.PlayerId() != world.ConstantPlayerID {
		t.Fatalf("anim skill=%d player=%d", anim.SkillId(), anim.PlayerId())
	}
	if anim.PointX() != sc.target.X || anim.PointY() != sc.target.Y {
		t.Fatalf("区中心应为目标坐标: (%d,%d)", anim.PointX(), anim.PointY())
	}
}

func TestAreaSkillWrongClassRejected(t *testing.T) {
	// DW(0) 不在 Twisting Slash 合格表 → 按未学习拒绝。
	sc := newSkillScaffold(t, 0, 100, 100)
	req := c2s.NewAreaSkill()
	req.SetSkillId(41)
	req.SetTargetX(sc.target.X)
	req.SetTargetY(sc.target.Y)
	sc.srv.handleAreaSkill(sc.sess, req.Bytes())
	if sc.c.Stats.CurrentMana != 100 {
		t.Fatalf("拒绝不扣蓝: %d", sc.c.Stats.CurrentMana)
	}
	if n := countFrames(sc.rec, 0xC3, 0x1E); n != 0 {
		t.Fatalf("拒绝不应发动画帧, got %d", n)
	}
}

// ---- T2-10 传送命令 ----

func warpScaffold(t *testing.T, money uint32, level uint16) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "warper", 120, 125, rec)
	c := sess.getSelected()
	c.Level = level
	c.Stats = &entity.CharStats{Money: money}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return srv, sess, c, rec
}

func sendWarp(srv *Server, sess *session, index uint16) {
	req := c2s.NewWarpCommandRequest()
	req.SetCommandKey(0xDEADBEEF)
	req.SetWarpInfoIndex(index)
	srv.handleWarpCommand(sess, req.Bytes())
}

// TestWarpCommandArena 锁定 T2-10 主路径：清单 1（Arena，2000 金，等级 50）→
// 扣款 + 金币更新帧 + 走 T1-7 换图链（92B 下发 + 状态回 EnteringMap）。
func TestWarpCommandArena(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 10000, 100)
	warp, ok := srv.deps.cfg.GameConfig.WarpByIndex(1)
	if !ok || warp.Name != "Arena" {
		t.Fatalf("传送清单 1 应为 Arena: %+v", warp)
	}

	sendWarp(srv, sess, 1)

	if c.Stats.Money != 10000-2000 {
		t.Fatalf("应扣 2000 金: money=%d", c.Stats.Money)
	}
	if n := countFrames(rec, 0xC3, 0x22); n != 1 {
		t.Fatalf("应下发金币更新帧, got %d", n)
	}
	if c.MapNumber != 6 {
		t.Fatalf("应换到 Arena(map 6), got %d", c.MapNumber)
	}
	if sess.getState() != entity.StateEnteringMap {
		t.Fatalf("状态应回 EnteringMap, got %d", sess.getState())
	}
	if p := srv.world.Map(0).Player(7); p != nil {
		t.Fatal("旧地图应已摘除")
	}
}

func TestWarpCommandNotEnoughLevel(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 10000, 10)
	sendWarp(srv, sess, 1) // Arena 需 50 级
	if c.Stats.Money != 10000 {
		t.Fatalf("拒绝不扣款: %d", c.Stats.Money)
	}
	if c.MapNumber != 0 || sess.getState() != entity.StateEnteredWorld {
		t.Fatal("拒绝不换图")
	}
	if n := countFrames(rec, 0xC3, 0x22); n != 0 {
		t.Fatalf("拒绝不发金币帧, got %d", n)
	}
}

func TestWarpCommandNotEnoughMoney(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 100, 100)
	sendWarp(srv, sess, 1)
	if c.Stats.Money != 100 {
		t.Fatalf("拒绝不扣款: %d", c.Stats.Money)
	}
	if c.MapNumber != 0 {
		t.Fatal("拒绝不换图")
	}
	if n := countFrames(rec, 0xC3, 0x22); n != 0 {
		t.Fatalf("拒绝不发金币帧, got %d", n)
	}
}

func TestWarpCommandUnknownIndex(t *testing.T) {
	srv, sess, c, _ := warpScaffold(t, 10000, 100)
	sendWarp(srv, sess, 0xFFFF)
	if c.Stats.Money != 10000 || c.MapNumber != 0 {
		t.Fatal("未知索引应无任何效果")
	}
}
