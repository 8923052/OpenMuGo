package gameserver

// handler_skill_delay_test.go —— TRIM-11a：面积技能的多段伤害按原版时刻表排队
// （对照 AreaSkillAttackAction.AttackTargetsAsync:232-378）——
// delay = 轮次累计 DelayBetweenHits + DelayPerOneDistance × 欧氏距离；
// delay 为 0 才就地结算，否则排队；**到点前复检目标仍存活且不在安全区**。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
)

// deferredHit 是一次被排队的伤害（假时钟记录用）。
type deferredHit struct {
	delay time.Duration
	fn    func()
}

// fakeHitClock 是注入的假时钟：只记录，不自动跑；测试显式 fire 才结算。
type fakeHitClock struct{ queued []deferredHit }

func (f *fakeHitClock) schedule(delay time.Duration, fn func()) {
	f.queued = append(f.queued, deferredHit{delay: delay, fn: fn})
}

func (f *fakeHitClock) fire(t *testing.T, index int) {
	t.Helper()
	if index >= len(f.queued) {
		t.Fatalf("只排了 %d 发，要放第 %d 发", len(f.queued), index)
	}
	f.queued[index].fn()
}

// placeAtNonSafezone 把施法者挪到一个"自身与两个测试落点都不在安全区"的锚点，
// 使距离固定为 3 与 4（原版用欧氏距离算延迟）。
func placeAtNonSafezone(t *testing.T, srv *Server, wp *world.Player, c *entity.Character) (byte, byte) {
	t.Helper()
	mp := srv.world.Map(wp.MapNumber)
	for y := 20; y < 240; y++ {
		for x := 20; x < 230; x++ {
			if mp.Safezone(byte(x), byte(y)) || mp.Safezone(byte(x), byte(y+3)) || mp.Safezone(byte(x+4), byte(y)) {
				continue
			}
			wp.X, wp.Y = byte(x), byte(y)
			if c != nil {
				c.X, c.Y = wp.X, wp.Y
			}
			return wp.X, wp.Y
		}
	}
	t.Fatal("找不到可用的非安全区锚点")
	return 0, 0
}

// areaTestTargets 取两只可打怪（并把它们摆到锚点上方 3 与右方 4 的位置）。
func areaTestTargets(t *testing.T, srv *Server, ax, ay byte) []*npc.Npc {
	t.Helper()
	out := make([]*npc.Npc, 0, 2)
	for _, n := range srv.deps.cfg.NPCs.ByMap(0) {
		if !n.Alive() || !n.IsAttackableByPlayer() {
			continue
		}
		out = append(out, n)
		if len(out) == 2 {
			break
		}
	}
	if len(out) < 2 {
		t.Fatal("Lorencia 应有两只可打怪")
	}
	out[0].X, out[0].Y = ax, ay+3 // 距离 3
	out[1].X, out[1].Y = ax+4, ay // 距离 4
	return out
}

// softCombatValues 用"必命中 + 每发 1 点"的属性快照：攻击率抬到 float32 上限附近使
// 1 − 防速/攻速 精确等于 1（原版命中率公式），等级 1 使伤害落在 1 点下限。
func softCombatValues() *player.CombatValues {
	return &player.CombatValues{Level: 1, AttackRatePvM: 1e30}
}

func areaSkillDef() *action.SkillDef {
	return &action.SkillDef{Number: 19, Target: action.SkillTargetExplicit, DamageType: action.DamageTypePhysical, Range: 9}
}

func TestAreaHitsDeferredDelays(t *testing.T) {
	sc := newSkillScaffold(t, 0, 5000, 5000)
	clk := &fakeHitClock{}
	sc.srv.areaHitScheduler = clk.schedule
	ax, ay := placeAtNonSafezone(t, sc.srv, sc.wp, sc.c)
	targets := areaTestTargets(t, sc.srv, ax, ay)
	near, far := targets[0], targets[1]
	beforeNear, beforeFar := near.Health(), far.Health()

	area := &config.AreaSkillSettings{
		UseDeferredHits: true, DelayPerOneDistanceMs: 50, DelayBetweenHitsMs: 1000,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 2, HitChancePerDistanceMultiplier: 1,
	}
	sc.srv.applyAreaHits(sc.sess, sc.c, sc.wp, softCombatValues(), areaSkillDef(), area, targets, false)

	// 排了 4 发：两轮 × 两只，时刻 = 轮次累计 + 50×距离。
	want := []time.Duration{150 * time.Millisecond, 200 * time.Millisecond,
		1150 * time.Millisecond, 1200 * time.Millisecond}
	if len(clk.queued) != len(want) {
		t.Fatalf("排队数 %d, want %d", len(clk.queued), len(want))
	}
	for i, w := range want {
		if clk.queued[i].delay != w {
			t.Fatalf("第 %d 发延迟 %v, want %v", i, clk.queued[i].delay, w)
		}
	}
	// 施放当下不掉血（原版伤害确实是延后的）。
	if near.Health() != beforeNear || far.Health() != beforeFar {
		t.Fatalf("延迟队列未跑前不该有伤害: near %v→%v far %v→%v", beforeNear, near.Health(), beforeFar, far.Health())
	}

	clk.fire(t, 0)
	if near.Health() != beforeNear-1 {
		t.Fatalf("第一发应掉 1 点（伤害下限）: %v→%v", beforeNear, near.Health())
	}
	clk.fire(t, 1)
	if far.Health() != beforeFar-1 {
		t.Fatalf("第二发应打到远处怪: %v→%v", beforeFar, far.Health())
	}
	// 到点前目标死亡 → 该发被跳过（原版在 Task 里复检 IsAlive）。
	deadBefore := near.Health()
	if _, died := near.ApplyDamage(99999, time.Now()); !died {
		t.Fatal("夹具应能把近处怪打死")
	}
	clk.fire(t, 2)
	if near.Health() != 0 || near.Health() >= deadBefore {
		t.Fatalf("已死目标不该再掉血: %v", near.Health())
	}
	clk.fire(t, 3)
	if far.Health() != beforeFar-2 {
		t.Fatalf("远处怪两发后应掉 2 点, got %v→%v", beforeFar, far.Health())
	}
}

// TestAreaHitsSafezoneCheckedAtFireTime：排队时安全区合法、到点前目标走进安全区 → 不发伤害
// （原版两支路都在 ApplySkillAsync 前复检 IsAtSafezone）。
func TestAreaHitsSafezoneCheckedAtFireTime(t *testing.T) {
	sc := newSkillScaffold(t, 0, 5000, 5000)
	clk := &fakeHitClock{}
	sc.srv.areaHitScheduler = clk.schedule
	ax, ay := placeAtNonSafezone(t, sc.srv, sc.wp, sc.c)
	targets := areaTestTargets(t, sc.srv, ax, ay)
	near := targets[0]
	before := near.Health()

	area := &config.AreaSkillSettings{UseDeferredHits: true, DelayPerOneDistanceMs: 50,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1}
	sc.srv.applyAreaHits(sc.sess, sc.c, sc.wp, softCombatValues(), areaSkillDef(), area, []*npc.Npc{near}, false)
	if len(clk.queued) != 1 {
		t.Fatalf("应排 1 发, got %d", len(clk.queued))
	}

	// 挪进安全区（原版打之前复检 IsAtSafezone）：装一份"只有 (sx,sy) 是安全区"的
	// 合成地形（.att 里 v==1 即安全区），单独验这条复检规则本身。
	sx, sy := byte(9), byte(7)
	installSafezoneTile(sc.srv, near.MapNumber, sx, sy)
	near.X, near.Y = sx, sy
	clk.fire(t, 0)
	if near.Health() != before {
		t.Fatalf("安全区内的目标不该被延迟伤害打到: %v→%v", before, near.Health())
	}
}

// installSafezoneTile 给该图装上"除 (x,y) 外全可走、(x,y) 为安全区"的合成地形。
func installSafezoneTile(srv *Server, mapNumber uint16, x, y byte) {
	data := make([]byte, 3+256*256)
	data[3+(int(y)<<8)+int(x)] = 1 // ParseTerrain: v==1 → 可走且安全区
	srv.world.Map(mapNumber).SetTerrain(world.ParseTerrain(data))
}

// TestAreaHitsZeroDelayIsImmediate：延迟全 0 时就地结算，一个排队都不留。
func TestAreaHitsZeroDelayIsImmediate(t *testing.T) {
	sc := newSkillScaffold(t, 0, 5000, 5000)
	clk := &fakeHitClock{}
	sc.srv.areaHitScheduler = clk.schedule
	ax, ay := placeAtNonSafezone(t, sc.srv, sc.wp, sc.c)
	targets := areaTestTargets(t, sc.srv, ax, ay)
	before := targets[0].Health()

	area := &config.AreaSkillSettings{UseDeferredHits: true, MinHitsPerTarget: 1,
		MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1}
	sc.srv.applyAreaHits(sc.sess, sc.c, sc.wp, softCombatValues(), areaSkillDef(), area, targets[:1], false)

	if len(clk.queued) != 0 {
		t.Fatalf("delay=0 应就地结算，不该排队, got %d", len(clk.queued))
	}
	if targets[0].Health() != before-1 {
		t.Fatalf("应已掉 1 点, got %v→%v", before, targets[0].Health())
	}
}

// TestAreaHitsDeadTargetNotScheduled：排队列时已死的目标直接被跳过（不计数、不排队）。
func TestAreaHitsDeadTargetNotScheduled(t *testing.T) {
	sc := newSkillScaffold(t, 0, 5000, 5000)
	clk := &fakeHitClock{}
	sc.srv.areaHitScheduler = clk.schedule
	ax, ay := placeAtNonSafezone(t, sc.srv, sc.wp, sc.c)
	targets := areaTestTargets(t, sc.srv, ax, ay)
	if _, died := targets[0].ApplyDamage(99999, time.Now()); !died {
		t.Fatal("夹具应能先杀死目标")
	}
	area := &config.AreaSkillSettings{UseDeferredHits: true, DelayPerOneDistanceMs: 50,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1}
	sc.srv.applyAreaHits(sc.sess, sc.c, sc.wp, softCombatValues(), areaSkillDef(), area, targets[:1], false)
	if len(clk.queued) != 0 {
		t.Fatalf("死人目标不该排队, got %d", len(clk.queued))
	}
}

// TestAreaHitsCasterLeftWorld：到点时施法者已不在该图（换图/离场）→ 放弃这一发。
// 这是本仓刻意加的守卫（原版属性系统随玩家存活，故不查），登记见 doc/16。
func TestAreaHitsCasterLeftWorld(t *testing.T) {
	sc := newSkillScaffold(t, 0, 5000, 5000)
	clk := &fakeHitClock{}
	sc.srv.areaHitScheduler = clk.schedule
	ax, ay := placeAtNonSafezone(t, sc.srv, sc.wp, sc.c)
	targets := areaTestTargets(t, sc.srv, ax, ay)
	before := targets[0].Health()

	area := &config.AreaSkillSettings{UseDeferredHits: true, DelayPerOneDistanceMs: 50,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1}
	sc.srv.applyAreaHits(sc.sess, sc.c, sc.wp, softCombatValues(), areaSkillDef(), area, targets[:1], false)

	sc.sess.setState(entity.StateEnteringMap)
	clk.fire(t, 0)
	if targets[0].Health() != before {
		t.Fatalf("施法者离场后不该再结算伤害: %v→%v", before, targets[0].Health())
	}
}
