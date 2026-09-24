package npc

// intelligence_trap.go —— 陷阱 AI，对照 `GameLogic/NPC/TrapIntelligenceBase.cs` 与四个子类。
//
// 与怪物的根本差别（原版语义，勿"顺手统一"）：
//   - 陷阱是 `Trap : NonPlayerCharacter, IAttacker`，**没有血量、不可被攻击**，
//     `RegisterHit` 在原版直接抛异常（TrapIntelligenceBase.cs:76-79）；
//   - 永不移动（CanWalkOn => false）；
//   - 目标集合不是"视野内玩家"而是 `Trap.Observers`（能看到它的玩家），按各自变体过滤；
//   - 一次可打多个目标（面积型），基础怪那一套"单目标 + 走位"完全不适用，故本文件自带 Tick。
//
// 偏差登记：原版每处目标筛选还要 `HasLineOfSightTo`。本仓世界层没有视线查询
// （怪物 AI 同样没有，见 doc/16），故此处按"距离 + 非安全区 + 朝向/踩踏"判定。

import (
	"time"

	"mugo/internal/gamelogic/world"
)

// trapIntelligence 用 kind 表达四个子类，行为差异集中在 targets 一个函数里。
type trapIntelligence struct {
	base *Intelligence
	kind trapKind
}

// Tick 驱动一次陷阱判定（攻击限频同怪物：AttackDelay 未到不重复攻击）。
func (t trapIntelligence) Tick(n *Npc, now time.Time, view WorldView) {
	if n == nil || n.Def == nil {
		return
	}
	targets := t.targets(n, view)
	if len(targets) == 0 {
		return
	}
	if !n.lastAttack.IsZero() && now.Sub(n.lastAttack) < time.Duration(n.Def.AttackDelayMs)*time.Millisecond {
		return
	}
	n.State = StateAttack
	for _, p := range targets {
		n.lastAttack = now
		if t.base.AttackHook != nil {
			t.base.AttackHook(n, p, now)
		}
		if t.kind == trapAreaDirection {
			break // 原版该变体是逐个串行攻击，其余是并行；此处只体现"朝向命中一个"
		}
	}
}

// targets 是四个子类各自的目标集合。
func (t trapIntelligence) targets(n *Npc, view WorldView) []*world.Player {
	rangeLen := int(n.Def.AttackRange)
	switch t.kind {
	case trapSinglePressed:
		// 只有"踩在陷阱那一格上"的玩家（AttackSingleWhenPressedTrapIntelligence.cs:22-36）。
		var out []*world.Player
		for _, p := range view.PlayersInRange(n.MapNumber, n.X, n.Y) {
			if p.X == n.X && p.Y == n.Y {
				out = append(out, p)
			}
		}
		return out

	case trapAreaPressed:
		// 有人踩上才被触发（:32），随后打范围内所有可打目标。
		pressed := false
		for _, p := range view.PlayersInRange(n.MapNumber, n.X, n.Y) {
			if p.X == n.X && p.Y == n.Y {
				pressed = true
				break
			}
		}
		if !pressed {
			return nil
		}
		return t.inArea(n, view, rangeLen, false)

	case trapAreaDirection:
		// 无触发条件，只打朝向那一向的目标（:31）。
		return t.inArea(n, view, rangeLen, true)

	default: // trapRandomInRange
		best := nearestTrapTarget(n, view, rangeLen+1)
		if best == nil {
			return nil
		}
		return []*world.Player{best}
	}
}

// inArea 取范围内的可打目标（非安全区格上的玩家）；byDirection 时还要求落在陷阱朝向上。
func (t trapIntelligence) inArea(n *Npc, view WorldView, rangeLen int, byDirection bool) []*world.Player {
	var out []*world.Player
	for _, p := range view.PlayersInRange(n.MapNumber, n.X, n.Y) {
		if !p.IsAlive || view.Safezone(n.MapNumber, p.X, p.Y) {
			continue
		}
		if !inRange(int(p.X), int(p.Y), int(n.X), int(n.Y), rangeLen) {
			continue
		}
		if byDirection && trapDirectionOf(n.X, n.Y, p.X, p.Y) != n.Rotation {
			continue
		}
		out = append(out, p)
	}
	return out
}

// nearestTrapTarget 是 RandomAttackInRangeTrapIntelligence 的取目标：
// 观察者里"不在安全区格上"的最近一个，攻击距离放宽到 AttackRange+1（:52-56）。
func nearestTrapTarget(n *Npc, view WorldView, rangeLen int) *world.Player {
	var best *world.Player
	bestDist := -1
	for _, p := range view.PlayersInRange(n.MapNumber, n.X, n.Y) {
		if !p.IsAlive || view.Safezone(n.MapNumber, p.X, p.Y) {
			continue
		}
		d := chebyshev(int(p.X), int(p.Y), int(n.X), int(n.Y))
		if d > rangeLen {
			continue
		}
		if bestDist == -1 || d < bestDist || (d == bestDist && best != nil && p.ID < best.ID) {
			best, bestDist = p, d
		}
	}
	return best
}

// trapDirectionOf 把位移归到 8 方向号（与朝向字段同源：原版 Trap.GetDirectionTo 与
// NonPlayerCharacter.Rotation 都是 1..8 的 Direction，见 spawnDirection）。
func trapDirectionOf(fromX, fromY, toX, toY byte) byte {
	dx, dy := int(toX)-int(fromX), int(toY)-int(fromY)
	switch {
	case dx == 0 && dy == 0:
		return 0
	case dy < 0 && dx == 0:
		return 1 // 北
	case dy < 0:
		return 2 // 东北
	case dx > 0 && dy == 0:
		return 3 // 东
	case dx > 0:
		return 4 // 东南
	case dy > 0 && dx == 0:
		return 5 // 南
	case dy > 0:
		return 6 // 西南
	case dx < 0 && dy == 0:
		return 7 // 西
	default:
		return 8 // 西北
	}
}
