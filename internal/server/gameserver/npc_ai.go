package gameserver

// npc_ai.go —— 怪物 AI 的驱动、战斗钩子与视野适配（doc/15 §10 T1-5 / §11 T2-2）。
//
// worldViewAdapter 把 GS 持有的 world 适配为 npc.WorldView（索敌/可行域/安全区查询）；
// AI tick 由 GS 的后台循环驱动（间隔 400ms ≈ 怪物最小 MoveDelay）。
// AI 移动/复活经事件广播（对照原版 MoveObjectAsync 的观察者通知）；
// 怪物攻击玩家经 AttackHook 结算伤害并出站（对照 AttackableExtensions 主干）。

import (
	"context"
	"sync"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/combat"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	"mugo/internal/pathfinding"
)

// worldAIView 实现 npc.WorldView（委托给 world.World 的导出查询），
// 并实现 npc.NextStepFinder（追击遇障时用实时 A* 绕行——原版 Monster.WalkToAsync）。
type worldAIView struct {
	w     *world.World
	route *routeFinder
}

func (v worldAIView) PlayersInRange(mapNumber uint16, x, y byte) []*world.Player {
	return v.w.PlayersInRange(mapNumber, x, y)
}

func (v worldAIView) WalkableForAI(mapNumber uint16, x, y byte) bool {
	return v.w.WalkableForAI(mapNumber, x, y)
}

func (v worldAIView) Safezone(mapNumber uint16, x, y byte) bool {
	return v.w.SafezoneAt(mapNumber, x, y)
}

// NextStep 用 A* 在地图 AIgrid 上算出从 (fx,fy) 朝 (tx,ty) 的下一步。
// 无寻路器或无路径时 ok=false（调用方退回原地不动，与旧行为一致）。
func (v worldAIView) NextStep(mapNumber uint16, fx, fy, tx, ty byte) (byte, byte, bool) {
	if v.route == nil {
		return 0, 0, false
	}
	grid := v.route.gridFor(v.w.Map(mapNumber).Terrain())
	return v.route.next(pathfinding.Point{X: fx, Y: fy}, pathfinding.Point{X: tx, Y: ty}, grid)
}

// routeFinder 封装一个非线程安全的 PathFinder，用互斥锁串行化（原版为每对象池化；
// 本仓 AI tick 单 goroutine 驱动，锁仅作防御）。SearchLimit 放宽到 1000 以允许中等绕障。
type routeFinder struct {
	mu   sync.Mutex
	pf   *pathfinding.PathFinder
	open *pathfinding.Grid // 无地形地图的共享全可走网格
}

func newRouteFinder() *routeFinder {
	pf := pathfinding.NewPathFinder(pathfinding.NewFullGridNetwork(true), nil)
	pf.SearchLimit = 1000
	r := &routeFinder{pf: pf}
	// 默认地形：全可走（与 world.Terrain nil → 全部可走一致）。
	og := &pathfinding.Grid{}
	for x := range og {
		for y := range og[x] {
			og[x][y] = 1
		}
	}
	r.open = og
	return r
}

// gridFor 取地图的 AIgrid；无地形时返回共享全可走网格。
func (r *routeFinder) gridFor(t *world.Terrain) *pathfinding.Grid {
	if t == nil {
		return r.open
	}
	if g := t.AIGrid(); g != nil {
		return g
	}
	return r.open
}

// next 跑一次实时 A*，返回首步。PathFinder 非并发安全，全程持锁。
func (r *routeFinder) next(from, to pathfinding.Point, grid *pathfinding.Grid) (byte, byte, bool) {
	if from == to {
		return 0, 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pf.ResetPathFinder()
	path := r.pf.FindPath(from, to, grid, false)
	if len(path) == 0 {
		return 0, 0, false
	}
	first := path[0].Point
	return first.X, first.Y, true
}

// AITick 驱动全部怪物 AI 一次并广播移动/复活事件。
func (s *Server) AITick(now time.Time) {
	moved, revived := s.deps.cfg.NPCs.TickAll(now, worldAIView{w: s.world, route: s.route}, s.activeMapNumbers())
	for _, m := range moved {
		s.broadcastNpcMove(m)
	}
	for _, n := range revived {
		s.broadcastNpcSpawn(n)
	}
	// 周期恢复（原版 RecoverTimer 3s）。
	s.regeneratePlayers(now)
	// T2-4：地面掉落物过期清理（时长取 05_game_config.ItemDropDurationMs，缺省回落 60s）。
	if s.deps.cfg.Drops != nil {
		dropDur := world.DefaultDropDuration
		if gc := s.deps.cfg.GameConfig; gc != nil && gc.Globals.ItemDropDurationMs > 0 {
			dropDur = time.Duration(gc.Globals.ItemDropDurationMs) * time.Millisecond
		}
		for _, e := range s.deps.cfg.Drops.ExpireBefore(now.Add(-dropDur)) {
			for _, p := range s.world.Map(e.MapNumber).PlayersInRangeFor(e.X, e.Y) {
				if p.View != nil {
					_ = p.View.ShowItemDropRemoved([]uint16{e.ID})
				}
			}
		}
	}
}

// activeMapNumbers 返回有玩家的地图集合（AI tick 只驱动这些地图的怪物——
// 原版每怪独立定时器且无观察者时不思考的等价优化）。
func (s *Server) activeMapNumbers() map[uint16]bool {
	active := make(map[uint16]bool)
	for _, sess := range s.trackedSessions() {
		if wp := sess.getWorldPlayer(); wp != nil {
			active[wp.MapNumber] = true
		}
	}
	return active
}

// broadcastNpcMove 广播怪物移动：留存观察者收 D4 移动包；
// 因移动新进入覆盖集的观察者收出生包（C2 13）、离开的收移出包（C1 14）
// （对照原版 MoveObjectAsync + UpdateObservingBucketsAsync）。
func (s *Server) broadcastNpcMove(m npc.MovedNpc) {
	aud := s.world.Map(m.Npc.MapNumber)
	before := aud.PlayersInRangeFor(m.OldX, m.OldY)
	after := aud.PlayersInRangeFor(m.Npc.X, m.Npc.Y)
	afterIDs := make(map[uint16]bool, len(after))
	for _, p := range after {
		afterIDs[p.ID] = true
	}
	move := action.WalkedMove{
		ObjectID: m.Npc.ID,
		SourceX:  m.OldX, SourceY: m.OldY,
		TargetX: m.Npc.X, TargetY: m.Npc.Y,
		Rotation: m.Npc.Rotation,
		Steps:    []byte{npcMoveNibble(m.Npc.X, m.OldX, m.Npc.Y, m.OldY)},
	}
	for _, p := range before {
		if afterIDs[p.ID] {
			if p.View != nil {
				_ = p.View.ShowObjectWalked(move)
			}
			continue
		}
		if p.View != nil {
			_ = p.View.ShowObjectsOutOfScope([]uint16{m.Npc.ID})
		}
	}
	for _, p := range after {
		if !afterIDs[p.ID] {
			continue
		}
		if !inBefore(before, p.ID) && p.View != nil {
			_ = p.View.ShowNpcsInScope([]action.NpcScopeEntry{{
				ID: m.Npc.ID, TypeNumber: uint16(m.Npc.Number),
				X: m.Npc.X, Y: m.Npc.Y, Rotation: m.Npc.Rotation,
			}})
		}
	}
}

// broadcastNpcSpawn 复活后在覆盖集内重新出生（C2 13）。
func (s *Server) broadcastNpcSpawn(n *npc.Npc) {
	for _, p := range s.world.Map(n.MapNumber).PlayersInRangeFor(n.X, n.Y) {
		if p.View != nil {
			_ = p.View.ShowNpcsInScope([]action.NpcScopeEntry{{
				ID: n.ID, TypeNumber: uint16(n.Number),
				X: n.X, Y: n.Y, Rotation: n.Rotation,
			}})
		}
	}
}

// inBefore 辅助：ID 是否在集合中。
func inBefore(players []*world.Player, id uint16) bool {
	for _, p := range players {
		if p.ID == id {
			return true
		}
	}
	return false
}

// statStatus 对照原版 ShowHitExtendedPlugIn.CalcStatStatus：血/盾条 0..250
// （0=空，0xFF=无该资源），受击者血量条由接收端客户端渲染。
func statStatus(cur, maxV float32) byte {
	if maxV == 0 {
		return 0xFF
	}
	if cur <= 0 {
		return 0
	}
	return byte(cur/maxV*250 + 0.5)
}

// npcMoveNibble 由位移增量反查方向字节（action.DirectionDeltas 的逆映射）。
//
// 返回的就是**方向包字节** = OpenMU Direction 枚举 - 1（客户端 ToPacketByte 语义）：
// 行走 nibble / 0x18 动画方向 / 0xD4 步点 nibble 共用同一套编码。
// 增量先按符号归一化到 8 个卦限，因此多格位移（randomMove 的 ±MoveRange 跳步）
// 也能得到正确朝向——旧实现只做精确单格匹配，多格时回落 0（West）导致朝向错误。
func npcMoveNibble(nx, ox, ny, oy byte) byte {
	dx, dy := signByte(nx, ox), signByte(ny, oy)
	for nib, delta := range [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}} {
		if delta[0] == dx && delta[1] == dy {
			return byte(nib)
		}
	}
	return 0
}

// signByte 返回 a 相对 b 的方向符号（-1/0/1）。
func signByte(a, b byte) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}
	return 0
}

// startAILoop 启动 AI 后台循环（StartAsync 调用；ctx 取消即退出）。
func (s *Server) startAILoop(ctx context.Context) {
	if s.deps.cfg.NPCs == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.AITick(time.Now())
			}
		}
	}()
}

// wireMonsterCombat 装配怪物攻击玩家的钩子（New 时调用；伤害结算对照
// AttackableExtensions 主干，出站 ShowHit/Health/Killed 按接收者视角解析 ID）。
func (s *Server) wireMonsterCombat() {
	if s.deps.cfg.NPCs == nil {
		return
	}
	s.deps.cfg.NPCs.SetAttackHook(func(n *npc.Npc, target *world.Player, now time.Time) {
		// 命中 + 伤害走与玩家攻击同一完整公式（combat.Calculate）：怪物的暴击/破防属性为 0，
		// 玩家防御含 DefenseDecrement、ArmorDamageDecrease、DamageReceiveDecrement 与 Overrates。
		atk := combat.AttackerStats{
			Level:      int(n.Attribute("Level")),
			MinPhys:    int(n.Attribute("Minimum Physical Base Damage")),
			MaxPhys:    int(n.Attribute("Maximum Physical Base Damage")),
			AttackRate: n.Attribute("Attack Rate (PvM)"),
		}
		def := combat.DefenderStats{
			Defense:                target.DefensePvM,
			DefenseRate:            target.DefenseRatePvM,
			DamageReceiveDecrement: target.DamageReceiveMultiplier,
			ArmorDamageDecrease:    target.ArmorDamageDecrease,
			DefenseDecrement:       target.DefenseDecrement,
			GreaterDefenseBonus:    target.GreaterDefenseBonus,
			// Soul Barrier 对"任何守方"都生效（怪物打玩家也要抵扣），法力取当前值。
			SoulBarrierReduction: target.SoulBarrierReduction,
			SoulBarrierManaToll:  target.SoulBarrierToll,
		}
		if target.Stats != nil {
			def.CurrentMana = float32(target.Stats.CurrentMana)
		}
		res := combat.Calculate(s.world.RNG(), combat.Match{}, atk, def, combat.Skill{})
		damage := res.Damage
		// **先扣血，再广播**（原版 ShowHitAsync 在 ApplyDamage 之后取 healthStatus）。
		remained, died := target.ApplyDamage(uint16(damage))
		// 受击后回调：扣减玩家宠物耐久（对照 DecreaseDefenseItemDurabilityAsync）。
		if target.OnHit != nil {
			target.OnHit(damage)
		}
		healthStatus, shieldStatus := byte(0xFF), byte(0xFF)
		if target.Stats != nil {
			healthStatus = statStatus(float32(remained), float32(target.Stats.MaximumHealth))
			shieldStatus = statStatus(float32(target.Stats.CurrentShield), float32(target.Stats.MaximumShield))
		}
		// 受击者的观察者集合收 ObjectHitExtended（targetId 按接收者视角解析：
		// 自己=0x200）。注意：原版**不**在此处发 0x26 FF——客户端按伤害自行扣减
		// 血条；服务端额外发包会覆盖客户端状态（SD 乱跳，真机事故）。
		aud := s.world.Map(target.MapNumber).PlayersInRangeFor(target.X, target.Y)
		for _, o := range aud {
			if o.View == nil {
				continue
			}
			tid := target.ID
			if o.ID == target.ID {
				tid = world.ConstantPlayerID // 自己恒为哨兵（GetId 语义）
			}
			_ = o.View.ShowObjectHit(tid, healthStatus, shieldStatus, uint32(damage), 0, action.DamageKind(res.Kind))
		}
		// 攻击动画（0x18 ObjectAnimation，animation = AT_ATTACK1 = 120）——对照
		// Monster.AttackAsync：先结算伤害、再 ForEachWorldObserverAsync(ShowMonsterAttackAnimationAsync)。
		// **必须发**：S6 客户端的怪物近身挥击只由 0x18 触发，缺失时近战怪"隔空掉血"、
		// 看起来像远程。观众集合用**怪物**的观察者（原版 this.ForEachWorldObserver），
		// 方向 = 怪 → 目标（Direction 枚举 packet byte）。
		dir := npcMoveNibble(target.X, n.X, target.Y, n.Y)
		for _, o := range s.world.Map(n.MapNumber).PlayersInRangeFor(n.X, n.Y) {
			if o.View == nil {
				continue
			}
			tid := target.ID
			if o.ID == target.ID {
				tid = world.ConstantPlayerID // 自己恒为哨兵（GetId 语义）
			}
			_ = o.View.ShowObjectAnimation(n.ID, dir, action.AnimationAttack1, tid)
		}
		if died {
			for _, o := range aud {
				if o.View != nil {
					_ = o.View.ShowObjectGotKilled(world.ConstantPlayerID)
				}
			}
			s.deps.logger.Printf("[debug] player-death %s by %s (dmg=%d) → 3s 后重生", target.Name, n.Name, damage)
			if target.OnDeath != nil {
				target.OnDeath() // 原版 OnDeathAsync：3s 后回出生门满血重生
			}
		} else {
			s.deps.logger.Printf("[debug] npc-hit %s→%s: dmg=%d remain=%d/%d status=%d kind=%d",
				n.Name, target.Name, damage, remained, target.Stats.MaximumHealth, healthStatus, res.Kind)
		}
	})
}

// regeneratePlayers 周期恢复（原版 GameContext.RecoverTimer 3s + Player.RegenerateAsync）。
//
// 逐条对照原版：①顺序 = Stats.IntervalRegenerationAttributes（法力→生命→AG→护盾）；
// ②各属性 (max×倍率+绝对值) × elapsed/各周期，钳到 max（休息中 HP 5s/MP 3s+5s 叠加）；
// ③AG 在安全区额外 +3 绝对值；④护盾只在"恢复启用"（安全区或 ShieldRecoveryEverywhere）
// 且静置 ≥10s 后恢复，倍率随 hiatus 线性 ramp（÷15 斜率，基值 4/3）。
//
// 下发必须走 **C1 26 FF 的 24B 扩展形态**：客户端 `case 0x26` 只有
// `ReceiveStatsExtended`（`PRECEIVE_STATS_EXTENDED` = 24B，含 Mana/BP/攻速/魔速），
// 发 9B 的 CurrentHealthAndShield 会被当 24B 解析 → 血/蓝/AG/攻速全读成越界垃圾。
func (s *Server) regeneratePlayers(now time.Time) {
	s.expireMagicEffects()
	for _, sess := range s.trackedSessions() {
		cv := sess.getCombatValues()
		c := sess.getSelected()
		wp := sess.getWorldPlayer()
		if cv == nil || c == nil || c.Stats == nil || wp == nil || !wp.IsAlive {
			continue
		}
		rem, elapsed, shieldHiatus := sess.beginRegen(now)
		if elapsed <= 0 {
			continue
		}
		st := c.Stats
		inSafezone := s.world.SafezoneAt(wp.MapNumber, wp.X, wp.Y)
		resting := sess.getResting()
		changed := false

		// 休息周期（原版 RegenerateAsync：resting → interval = IntervalResting；
		// MP 休息时在常规周期之上**再叠加** elapsed/Interval——原版 L918-922。
		// 两个因子之和等效于一个更短的合成周期 1/(1/3+1/5)=1.875s）。
		manaInterval := action.ManaRegenIntervalSeconds
		if resting {
			manaInterval = 1.0 / (1.0/action.ManaRegenIntervalSeconds + 1.0/action.ManaRegenRestingIntervalSeconds)
		}
		healthInterval := action.HealthRegenIntervalSeconds
		if resting {
			healthInterval = action.HealthRegenRestingIntervalSeconds
		}
		if v, r, ok := action.ApplyRegen(float64(st.CurrentMana), rem.Mana, float64(st.MaximumMana),
			float64(cv.ManaRegenMult), float64(cv.ManaRegenAbs), elapsed, manaInterval); ok {
			st.CurrentMana, rem.Mana, changed = uint32(v), r, true
		}
		if v, r, ok := action.ApplyRegen(float64(st.CurrentHealth), rem.Health, float64(st.MaximumHealth),
			float64(cv.HealthRegenMult), float64(cv.HealthRegenAbs), elapsed, healthInterval); ok {
			st.CurrentHealth, rem.Health, changed = uint32(v), r, true
		}
		abilityAbs := float64(cv.AbilityRegenAbs)
		if inSafezone {
			abilityAbs += action.AbilityRegenSafezoneBonus
		}
		if v, r, ok := action.ApplyRegen(float64(st.CurrentAbility), rem.Ability, float64(st.MaximumAbility),
			float64(cv.AbilityRegenMult), abilityAbs, elapsed, action.AbilityRegenIntervalSeconds); ok {
			st.CurrentAbility, rem.Ability, changed = uint32(v), r, true
		}

		// 护盾 hiatus：离开安全区、被打断或回满 → 重新计时（Stats.cs L1189：
		// "last interrupted, either by leaving a safezone, the shield being
		// damaged or maxing out"）。恢复倍率随 hiatus 线性 ramp（1/15 斜率）。
		shieldFull := st.MaximumShield > 0 && st.CurrentShield >= st.MaximumShield
		hiatus := shieldHiatus + elapsed
		if !inSafezone || shieldFull {
			hiatus = 0
		}
		if action.ShieldRecoveryActive(inSafezone, float64(cv.ShieldRecoveryEverywhere)) && hiatus >= action.ShieldRegenHiatusThreshold {
			shieldMult := action.ShieldRegenMultiplierAt(float64(cv.ShieldRegenMult), float64(cv.ShieldRampFactor), hiatus)
			if v, r, ok := action.ApplyRegen(float64(st.CurrentShield), rem.Shield, float64(st.MaximumShield),
				shieldMult, float64(cv.ShieldRegenAbs), elapsed, action.ShieldRegenIntervalSeconds); ok {
				st.CurrentShield, rem.Shield, changed = uint32(v), r, true
			}
		}
		sess.endRegen(rem, hiatus)

		if changed && wp.View != nil {
			_ = wp.View.ShowCurrentStatsExtended(currentStatsOf(st))
			s.deps.logger.Printf("[debug] regen %s: hp=%d/%d mp=%d/%d ag=%d/%d sd=%d/%d safe=%v hiatus=%.0fs (elapsed=%.1fs)",
				c.Name, st.CurrentHealth, st.MaximumHealth, st.CurrentMana, st.MaximumMana,
				st.CurrentAbility, st.MaximumAbility, st.CurrentShield, st.MaximumShield,
				inSafezone, hiatus, elapsed)
		}
	}
}
