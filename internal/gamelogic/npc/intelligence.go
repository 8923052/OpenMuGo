package npc

// intelligence.go —— BasicMonsterIntelligence 的 Go 移植（doc/15 §10 T1-5）。
// 决策逐条对照原版 BasicMonsterIntelligence.TickAsync：
//  1. 死亡 → 等待 RespawnDelay 后原地复活（health 回满、回出生点）；
//  2. 索敌：ResolveTarget——当前目标失效（出 ViewRange/死亡）则重找，
//     取 ViewRange 内最近的活跃玩家（不在安全区）；
//  3. 无目标 → 随机游走（有观察者时才动；受 MoveDelay 与地形约束）；
//  4. 目标在 AttackRange 内 → 攻击（回调；tick 间隔 = AttackDelay 天然限频）；
//  5. 目标在 ViewRange+1 内 → 朝目标附近（AttackRange 半径内随机点）走一步；
//  6. 否则 → 随机游走。
//
// 决策器抽象（TRIM-07）：原版是 INpcIntelligence 接口 + BasicMonsterIntelligence 的
// virtual 钩子（SearchNextTargetAsync / TickWithoutTargetAsync / CanWalkOn）。Go 无继承，
// 改为 Intelligence 结构上的三个钩子字段，由 IntelligenceFor 按
// MonsterDefinition.IntelligenceTypeName → ObjectKind 的次序装配（MapInitializer.cs:186-222）。
//
// 移动可行域：AIgrid == 可走且非安全区（原版 CanWalkOn；守卫换成"只看行走图且可进安全区"）。

import (
	"time"

	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// WorldView 是 AI 决策需要的世界查询（由 world.World 适配）。
type WorldView interface {
	// PlayersInRange 返回 ViewRange 内的玩家（不含隐身等——M7 全员可见）。
	PlayersInRange(mapNumber uint16, x, y byte) []*world.Player
	// WalkableForAI 对应原版 CanWalkOn：AIgrid==1 → 可走且非安全区。
	WalkableForAI(mapNumber uint16, x, y byte) bool
	// Safezone 判断目标是否在安全区（原版 IsAtSafezone 守卫）。
	Safezone(mapNumber uint16, x, y byte) bool
}

// NextStepFinder 是 WorldView 的**可选**寻路能力（A*/预计算表，由 server 层适配实现）。
// 追击遇障时（直线一步被墙挡）问它要绕过障碍的下一步——对应原版
// BasicMonsterIntelligence → Monster.WalkToAsync 的实时 A* 走位。不实现该接口的世界
// （如纯单测的 fakeWorld）保持原贪心走位行为不变。
type NextStepFinder interface {
	// NextStep 返回从 (fromX,fromY) 朝 (toX,toY) 迈出的下一步坐标；无法到达时 ok=false。
	NextStep(mapNumber uint16, fromX, fromY, toX, toY byte) (nextX, nextY byte, ok bool)
}

// Decider 是一个 NPC 的决策器（原版 INpcIntelligence 的 TickAsync）。
type Decider interface {
	Tick(n *Npc, now time.Time, view WorldView)
}

// Intelligence 是 NPC 决策器（每只怪一个实例；tick 由外部驱动，间隔 = AttackDelay）。
// 三个钩子字段即原版子类的覆写点，nil 表示用基础怪行为。
type Intelligence struct {
	rng *util.Rand
	// AttackHook 攻击玩家的回调（GS 装配：伤害结算 + 出站；nil 时仅置攻击状态）。
	AttackHook func(n *Npc, target *world.Player, now time.Time)
	// targetOf 覆写 SearchNextTargetAsync（nil = 基础怪：视野内最近活玩家）。
	targetOf func(i *Intelligence, n *Npc, view WorldView) *world.Player
	// idleOf 覆写 TickWithoutTargetAsync（nil = 基础怪：有观察者则随机游走）。
	idleOf func(i *Intelligence, n *Npc, now time.Time, view WorldView)
	// canStandOn 覆写 CanWalkOn（nil = AIgrid；守卫用"行走图 + 可进安全区"）。
	canStandOn func(i *Intelligence, n *Npc, view WorldView, x, y byte) bool
}

// NewIntelligence 构造基础怪 AI（rng 用于游走/走位随机）。
func NewIntelligence(rng *util.Rand) *Intelligence { return &Intelligence{rng: rng} }

// NewGuardIntelligence 构造守卫 AI（对照 GuardIntelligence.cs）。
func NewGuardIntelligence(rng *util.Rand, hook func(*Npc, *world.Player, time.Time)) *Intelligence {
	i := NewIntelligence(rng)
	i.AttackHook = hook
	i.targetOf = guardTarget
	i.idleOf = guardIdle
	i.canStandOn = guardCanStandOn
	return i
}

// NewTrapIntelligence 构造陷阱 AI（对照 TrapIntelligenceBase + 四个子类）：
// 陷阱不移动、可一次打多个目标，故自带 Tick 而不走基础怪的钩子。
func NewTrapIntelligence(rng *util.Rand, kind trapKind, hook func(*Npc, *world.Player, time.Time)) Decider {
	i := NewIntelligence(rng)
	i.AttackHook = hook
	return trapIntelligence{base: i, kind: kind}
}

// NullIntelligence 是什么也不做的决策器（原版 NullMonsterIntelligence：全空实现）。
type NullIntelligence struct{}

// Tick 空实现。
func (NullIntelligence) Tick(*Npc, time.Time, WorldView) {}

// IntelligenceFor 按原版 MapInitializer.cs:186-222 的次序挑决策器：
// 先看配置里的 IntelligenceTypeName（反射），没有再按 ObjectKind 给默认值。
// 类型名不认识的一律回落 NullIntelligence —— 原版类型加载失败时也是"无 AI"。
func IntelligenceFor(n *Npc, rng *util.Rand, hook func(*Npc, *world.Player, time.Time)) Decider {
	if n == nil || n.Def == nil {
		return NullIntelligence{}
	}
	switch typeNameOf(n) {
	case typeGuardIntelligence:
		return NewGuardIntelligence(rng, hook)
	case typeBasicMonsterIntelligence:
		i := NewIntelligence(rng)
		i.AttackHook = hook
		return i
	case typeNullMonsterIntelligence:
		return NullIntelligence{}
	case typeRandomAttackInRangeTrap:
		return NewTrapIntelligence(rng, trapRandomInRange, hook)
	case typeTrapSinglePressed:
		return NewTrapIntelligence(rng, trapSinglePressed, hook)
	case typeTrapAreaPressed:
		return NewTrapIntelligence(rng, trapAreaPressed, hook)
	case typeTrapAreaDirection:
		return NewTrapIntelligence(rng, trapAreaDirection, hook)
	case "":
		// 无类型名 → 按 ObjectKind 的默认（Trap 的默认是 RandomAttackInRange）。
		switch n.Def.ObjectKind {
		case kindGuard:
			return NewGuardIntelligence(rng, hook)
		case kindTrap:
			return NewTrapIntelligence(rng, trapRandomInRange, hook)
		case kindMonster:
			i := NewIntelligence(rng)
			i.AttackHook = hook
			return i
		}
		return NullIntelligence{} // Merchant/Statue/Gate/Npc…原版直接是 NonPlayerCharacter（无 AI）
	default:
		return NullIntelligence{}
	}
}

// resolveTarget 的默认实现保留原基础怪语义：视野内最近的活玩家（不在安全区）。

// Tick 执行一次 AI 决策。now 为当前时刻（测试注入时钟）。
// 结构对照原版 TickAsync：索敌/攻击每 tick 都做；**移动**受 MoveDelay 节流。
func (i *Intelligence) Tick(n *Npc, now time.Time, view WorldView) {
	n.mu.Lock()
	if n.health <= 0 {
		// 1. 死亡：等待重生。
		if now.Before(n.respawnAt) {
			n.mu.Unlock()
			return
		}
		n.health = n.Attribute("Maximum Health")
		n.State = StateIdle
		n.X, n.Y = n.HomeX, n.HomeY
		n.mu.Unlock()
		return
	}
	n.mu.Unlock()

	// 2. 索敌：当前目标失效则取 ViewRange 内最近玩家（不在安全区）。
	var target *world.Player
	if i.targetOf != nil {
		target = i.targetOf(i, n, view)
	} else {
		target = i.resolveTarget(n, view)
	}
	n.mu.Lock()
	n.targetID = targetIDOf(target)
	n.mu.Unlock()

	// 3. 目标在攻击范围内且自己不在安全区 → 攻击（原版 AttackAsync；伤害结算经
	// AttackHook 由 GS 完成）。攻击限频：AttackDelay 未到不重复攻击。
	if target != nil && inRange(int(target.X), int(target.Y), int(n.X), int(n.Y), int(n.Def.AttackRange)) &&
		!view.Safezone(n.MapNumber, n.X, n.Y) {
		n.mu.Lock()
		if now.Sub(n.lastAttack) < time.Duration(n.Def.AttackDelayMs)*time.Millisecond && !n.lastAttack.IsZero() {
			n.mu.Unlock()
			return
		}
		n.lastAttack = now
		n.mu.Unlock()
		n.State = StateAttack
		if i.AttackHook != nil {
			i.AttackHook(n, target, now)
		}
		return
	}

	// 4. 移动节流：MoveDelay 未到不移动（原版 Walker/MoveDelay 语义）。
	n.mu.Lock()
	canMove := now.Sub(n.lastMove) >= time.Duration(n.Def.MoveDelayMs)*time.Millisecond || n.lastMove.IsZero()
	if canMove {
		n.lastMove = now
	}
	n.mu.Unlock()
	if !canMove {
		return
	}

	// 5. 目标在 ViewRange+1 内 → 朝目标附近走一步（原版 GetRandomCoordinate(target, AttackRange)：
	// 每 tick 取目标周围 AttackRange 内的**随机可走点**作走位目标，使多只怪分散到不同格、
	// 而不是全部叠到目标那一格）。
	if target != nil && inRange(int(target.X), int(target.Y), int(n.X), int(n.Y), int(n.Def.ViewRange)+1) {
		wx, wy := i.randomCoordinate(n, view, target.X, target.Y, int(n.Def.AttackRange))
		i.stepToward(n, view, wx, wy)
		n.mu.Lock()
		n.State = StateChase
		n.mu.Unlock()
		return
	}

	// 6. 无目标时的闲置行为（基础怪 = 有观察者则随机游走；守卫另有覆写）。
	if i.idleOf != nil {
		i.idleOf(i, n, now, view)
		return
	}
	if len(view.PlayersInRange(n.MapNumber, n.X, n.Y)) > 0 {
		i.randomMove(n, view)
	}
}

// resolveTarget 对应原版 ResolveTargetAsync + SearchNextTargetAsync：
// 当前目标仍有效则保持，否则取 ViewRange 内最近玩家（不在安全区）。
// 守卫（ObjectKind=Guard）不索敌玩家（原版 GuardIntelligence 攻击怪物）；
// 死亡玩家不作为目标（原版 IsAlive 检查）。
func (i *Intelligence) resolveTarget(n *Npc, view WorldView) *world.Player {
	players := view.PlayersInRange(n.MapNumber, n.X, n.Y)
	var best *world.Player
	bestDist := -1
	for _, p := range players {
		if !p.IsAlive {
			continue // 尸体不作为攻击目标
		}
		if view.Safezone(n.MapNumber, p.X, p.Y) {
			continue // 原版：安全区玩家不被索敌
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

// stepToward 朝目标附近（AttackRange 半径内的随机点）走一步（受地形约束）。
func (i *Intelligence) stepToward(n *Npc, view WorldView, tx, ty byte) {
	dx, dy := sign(tx, n.X), sign(ty, n.Y)
	nx, ny := n.X, n.Y
	if dx != 0 {
		nx += byte(dx)
	}
	if dy != 0 {
		ny += byte(dy)
	}
	if i.standable(n, view, nx, ny) {
		i.tryMove(n, view, nx, ny)
		return
	}
	// 贪心一步被挡 → 尝试寻路绕行。
	if nsf, ok := view.(NextStepFinder); ok {
		if sx, sy, found := nsf.NextStep(n.MapNumber, n.X, n.Y, tx, ty); found {
			i.tryMove(n, view, sx, sy)
		}
	}
}

// randomCoordinate 复刻原版 Terrain.GetRandomCoordinate：在 (cx,cy) 周围 radius 的
// 方形内随机取一个可走点（最多重试 20 次），全不可走则回落中心点。追击时用它作
// 走位目标，让每只怪朝目标附近的**不同**随机点靠拢，避免多只怪叠到同一坐标。
func (i *Intelligence) randomCoordinate(n *Npc, view WorldView, cx, cy byte, radius int) (byte, byte) {
	lo := func(v int) int {
		if v < 0 {
			return 0
		}
		return v
	}
	hi := func(v int) int {
		if v > 255 {
			return 255
		}
		return v
	}
	for k := 0; k < 20; k++ {
		x := byte(i.rng.Next(lo(int(cx)-radius), hi(int(cx)+radius+1)))
		y := byte(i.rng.Next(lo(int(cy)-radius), hi(int(cy)+radius+1)))
		if i.standable(n, view, x, y) {
			return x, y
		}
	}
	return cx, cy
}

// randomMove 在 Home 周围 MoveRange 内随机走一步（原版 RandomMoveAsync）。
func (i *Intelligence) randomMove(n *Npc, view WorldView) {
	moveRange := int(n.Def.MoveRange)
	dx := i.rng.Next(-moveRange, moveRange+1)
	dy := i.rng.Next(-moveRange, moveRange+1)
	nx := clampByte(int(n.HomeX) + dx)
	ny := clampByte(int(n.HomeY) + dy)
	i.tryMove(n, view, nx, ny)
}

// tryMove 尝试移动一格：目标格须满足本决策器的可站立判定才提交。
func (i *Intelligence) tryMove(n *Npc, view WorldView, nx, ny byte) {
	if nx == n.X && ny == n.Y {
		return
	}
	if i.standable(n, view, nx, ny) {
		n.mu.Lock()
		n.X, n.Y = nx, ny
		n.mu.Unlock()
	}
}

// standable 走钩子，默认即基础怪的 AIgrid 判定（守卫覆写成"行走图且可进安全区"）。
func (i *Intelligence) standable(n *Npc, view WorldView, x, y byte) bool {
	if i.canStandOn != nil {
		return i.canStandOn(i, n, view, x, y)
	}
	return view.WalkableForAI(n.MapNumber, x, y)
}

func sign(target, from byte) int {
	switch {
	case target > from:
		return 1
	case target < from:
		return -1
	}
	return 0
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func chebyshev(x1, y1, x2, y2 int) int {
	dx := x1 - x2
	dy := y1 - y2
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

func inRange(x1, y1, x2, y2, rangeLen int) bool {
	return chebyshev(x1, y1, x2, y2) <= rangeLen
}

func targetIDOf(p *world.Player) uint16 {
	if p == nil {
		return 0
	}
	return p.ID
}
