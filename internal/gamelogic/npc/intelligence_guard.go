package npc

// intelligence_guard.go —— 守卫 AI，对照 `GameLogic/NPC/GuardIntelligence.cs`。
//
// 与基础怪的三处差异（其余全部沿用基础怪逻辑）：
//   - 索敌：视野内**只挑玩家**，且要求红名（HeroState ≥ PlayerKiller1stStage）、存活、
//     不在安全区，取最近的一个（:38-48）。原版并不攻击怪物 —— 本仓旧注释写反了，见 doc/16。
//   - 可行域：守卫用**行走图**（不是 AIgrid），且允许进安全区（:28、:32-35）。
//   - 闲置：每 tick 只有 20% 概率行动；离出生点 >7 格时朝"出生点/当前位置的中点"走回，
//     否则才随机游走（:62-84）。

import (
	"time"

	"mugo/internal/gamelogic/world"
)

// heroStatePlayerKiller1st 对应 DataModel Character.cs:45 的 HeroState.PlayerKiller1stStage
// （枚举隐式值：New0/Hero1/LightHero2/Normal3/PKWarning4/PK1st5/PK2nd6）。
const heroStatePlayerKiller1st byte = 5

// guardIdleChancePercent 是原版 `NextDouble() > 0.20f → return` 的反面：20% 概率行动。
const guardIdleChancePercent = 20

// guardReturnDistance 是"离出生点多远才开始往回走"的原版常量 7。
const guardReturnDistance = 7

// guardTarget 复刻 SearchNextTargetAsync：视野内最近的"红名活玩家（不在安全区）"。
func guardTarget(i *Intelligence, n *Npc, view WorldView) *world.Player {
	var best *world.Player
	bestDist := -1
	for _, p := range view.PlayersInRange(n.MapNumber, n.X, n.Y) {
		if p == nil || !p.IsAlive {
			continue
		}
		if heroStateOf(p) < heroStatePlayerKiller1st {
			continue // 普通玩家/白名不配成为守卫的目标
		}
		if view.Safezone(n.MapNumber, p.X, p.Y) {
			continue // 安全区里红名也打不了
		}
		d := chebyshev(int(p.X), int(p.Y), int(n.X), int(n.Y))
		if d > int(n.Def.ViewRange) {
			continue
		}
		if bestDist == -1 || d < bestDist || (d == bestDist && best != nil && p.ID < best.ID) {
			best, bestDist = p, d
		}
	}
	return best
}

// guardCanStandOn 用行走图且允许安全区（守卫站在安全区里是常态： Lorencia 广场）。
func guardCanStandOn(i *Intelligence, n *Npc, view WorldView, x, y byte) bool {
	gv, ok := view.(GuardWalkable)
	if !ok {
		return view.WalkableForAI(n.MapNumber, x, y) || view.Safezone(n.MapNumber, x, y)
	}
	return gv.Walkable(n.MapNumber, x, y)
}

// guardIdle 复刻 TickWithoutTargetAsync 的 20% 概率 + 回出生点。
func guardIdle(i *Intelligence, n *Npc, now time.Time, view WorldView) {
	if i.rng.NextDouble() > 0.20 {
		return
	}
	if chebyshev(int(n.X), int(n.Y), int(n.HomeX), int(n.HomeY)) > guardReturnDistance {
		// 原版 (spawn/2 + pos/2) 的整型点运算：先各自整除再相加。
		tx := n.HomeX/2 + n.X/2
		ty := n.HomeY/2 + n.Y/2
		i.stepToward(n, view, byte(int(tx)+i.rng.Next(-1, 2)), byte(int(ty)+i.rng.Next(-1, 2)))
		return
	}
	if len(view.PlayersInRange(n.MapNumber, n.X, n.Y)) > 0 {
		i.randomMove(n, view)
	}
}

// GuardWalkable 是 WorldView 的**可选**行走图查询（守卫的可站立判定用；缺失时按
// "AIgrid 或安全区"近似，语义一致：可走格减去"非安全区"这一条限制）。
type GuardWalkable interface {
	Walkable(mapNumber uint16, x, y byte) bool
}

func heroStateOf(p *world.Player) byte {
	if p == nil || p.Stats == nil {
		return 0
	}
	return p.Stats.HeroState
}
