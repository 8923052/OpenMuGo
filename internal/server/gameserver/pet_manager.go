package gameserver

// pet_manager.go —— 黑暗渡鸦命令管理器（对照 GameLogic/Pet/RavenCommandManager.cs）。
//
// Go 适配要点：
//   - 每种"攻击行为"对应一个后台 goroutine 循环，用 context 取消切换（C# 用 CancellationTokenSource）；
//   - 一切改动角色/世界态的动作（伤害结算、经验、掉落、广播）都在 sess.opMu 下进行，
//     与连接读线程的 handler 分发互斥，消除并发写 c.Stats/c.Level 的竞态；
//   - 渡鸦伤害走已有 combat 主干：ResolveRavenCombatValues 读出渡鸦 min/max/攻击率/攻速
//     （由 "Dark Raven Level" 经职业关系图派生），复用 applyDamageToNpc（击杀给主人经验）。

import (
	"context"
	"sync"
	"time"

	"mugo/internal/gamelogic/combat"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/pet"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
)

// attackNoTarget 是"无锁定目标"的哨兵（与 PetBehaviourChangedViewPlugIn 的 0xFFFF 一致）。
const attackNoTarget uint16 = 0xFFFF

// petManager 管理一只黑暗渡鸦的攻击行为。
type petManager struct {
	s    *Server
	sess *session
	c    *entity.Character
	wp   *world.Player

	mu        sync.Mutex
	cancel    context.CancelFunc
	behaviour pet.Behaviour
	locked    *npc.Npc // AttackTarget 的锁定目标
}

func newPetManager(s *Server, sess *session, c *entity.Character, wp *world.Player) *petManager {
	return &petManager{s: s, sess: sess, c: c, wp: wp, behaviour: pet.BehaviourIdle}
}

// stop 取消当前攻击循环（幂等）。
func (m *petManager) stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
}

// setBehaviour 切换宠物行为：取消旧循环、置新态、广播 PetMode，并按需启动攻击循环。
// 由 handler（持 opMu）在收到 C1 A7 时调用。
func (m *petManager) setBehaviour(b pet.Behaviour, target *npc.Npc) {
	// 耐久归零 → 强制空闲（对照 RavenCommandManager.SetBehaviourAsync）。
	if m.petDurability() == 0 {
		b = pet.BehaviourIdle
	}
	m.stop()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.behaviour = b
	m.locked = target
	m.cancel = cancel
	m.mu.Unlock()

	if view := m.s.viewFor(m.sess); view != nil {
		targetID := attackNoTarget
		if b == pet.BehaviourAttackTarget && target != nil {
			targetID = target.ID
		}
		_ = view.ShowPetMode(pet.PetTypeByte(false), b.CommandMode(), targetID)
	}
	if b == pet.BehaviourIdle {
		return
	}
	go m.attackLoop(ctx, b)
}

// attackLoop 是攻击主循环；每次按渡鸦攻速决定间隔，命中经 opMu 串行化。
func (m *petManager) attackLoop(ctx context.Context, b pet.Behaviour) {
	for {
		rav, err := player.ResolveRavenCombatValues(m.s.deps.cfg.GameConfig, m.c)
		delay := 1500 * time.Millisecond
		if err == nil {
			delay = time.Duration(rav.AttackDelayMilliseconds()) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		stop := m.attackOnce(ctx, b, rav)
		if stop {
			return
		}
	}
}

// attackOnce 执行一次攻击决策与结算；返回 true 表示循环应结束（行为被切换/取消）。
func (m *petManager) attackOnce(ctx context.Context, b pet.Behaviour, rav player.RavenAttack) (stop bool) {
	m.sess.opMu.Lock()
	defer m.sess.opMu.Unlock()

	// 取消检查（持锁后再看一次，避免与切换行为竞态）。
	select {
	case <-ctx.Done():
		return true
	default:
	}
	m.mu.Lock()
	cur := m.behaviour
	locked := m.locked
	m.mu.Unlock()
	if cur != b {
		return true // 行为已变，旧循环退出
	}

	wp := m.wp
	if wp == nil || !wp.IsAlive {
		return false // 主人已离场/死亡 → 本拍不攻击
	}
	map_ := m.s.world.Map(wp.MapNumber)
	if map_ == nil {
		return false
	}
	if map_.Safezone(wp.X, wp.Y) {
		return false // 安全区不攻击（对照 AttackAsync 的 IsAtSafezone 早退）
	}
	if m.petDurability() == 0 {
		return true // 耐久归零，停循环（客户端按 PetMode Idle 已在切换时广播）
	}

	targets := m.selectTargets(b, locked, rav)
	if len(targets) == 0 {
		if b == pet.BehaviourAttackTarget && locked != nil && !locked.Alive() {
			m.broadcastIdle()
			return true // 锁定目标已死 → 转 Idle（对照 AttackTargetUntilDeath 收尾）
		}
		return false
	}

	petType := pet.PetTypeByte(false)
	rng := m.s.world.RNG()
	attackType := pet.AttackSingleTarget
	if rng.NextRandomBool(pet.RangeAttackProbability()) {
		attackType = pet.AttackRange
	}
	// 范围攻击：主目标 + 其周围最多 2 个附加目标，各发一次动画 + 伤害。
	if attackType == pet.AttackRange {
		main := targets[0]
		extra := m.targetsNear(main, pet.RangeAttackHitRadius, pet.RangeAttackExtraTargets)
		bullet := append([]*npc.Npc{main}, extra...)
		for _, t := range bullet {
			m.broadcastAttack(petType, attackType, t)
			m.hitTarget(t, rav)
		}
		return false
	}
	m.broadcastAttack(petType, attackType, targets[0])
	m.hitTarget(targets[0], rav)
	return false
}

// hitTarget 对单个目标结算一次渡鸦伤害并广播（复用玩家攻击同主干）。
func (m *petManager) hitTarget(target *npc.Npc, rav player.RavenAttack) {
	if target == nil || !target.Alive() {
		return
	}
	// 渡鸦属性走 RavenAttributeSystem 语义：物理基伤=渡鸦 min/max、攻击率=渡鸦攻击率，
	// 非暴非卓越 ÷1.5（IsRaven 驱动）；minLevelDmg 用主人等级（AttackerSurrogate 语义）。
	lvl := int(m.c.Level)
	if cv := m.sess.getCombatValues(); cv != nil {
		lvl = cv.Level
	}
	atk := combat.AttackerStats{
		Level:      lvl,
		MinPhys:    rav.MinDamage,
		MaxPhys:    rav.MaxDamage,
		AttackRate: rav.AttackRate,
		IsRaven:    true,
	}
	result := combat.Calculate(m.s.world.RNG(), combat.Match{}, atk, npcDefender(target), combat.Skill{})
	// 主人恢复属性（击杀回血回蓝归主人）。
	cv := m.sess.getCombatValues()
	m.s.applyDamageToNpc(m.sess, m.c, m.wp, cv, target, result)
}

// selectTargets 依行为选出本拍可打目标（可能为空）。
func (m *petManager) selectTargets(b pet.Behaviour, locked *npc.Npc, rav player.RavenAttack) []*npc.Npc {
	wp := m.wp
	switch b {
	case pet.BehaviourAttackTarget:
		if locked != nil && m.isValidTarget(locked) {
			return []*npc.Npc{locked}
		}
		return nil
	case pet.BehaviourAttackWithOwner:
		if id := m.sess.getLastAttackedTarget(); id != 0 {
			if t := m.s.findNpcByID(wp.MapNumber, id); t != nil && m.isValidTarget(t) {
				return []*npc.Npc{t}
			}
		}
		// 主人无有效目标 → 回落随机（原版 AttackSameAsOwner 无目标时不攻击；此处保守随机索敌）。
		return m.randomTargets()
	default: // AttackRandom
		return m.randomTargets()
	}
}

// randomTargets 在主人周围随机挑一个可打怪。
func (m *petManager) randomTargets() []*npc.Npc {
	inRange := m.targetsNearPos(m.wp.X, m.wp.Y, pet.AttackRangeTiles, 0)
	if len(inRange) == 0 {
		return nil
	}
	pick := inRange[m.s.world.RNG().Next(0, len(inRange))]
	return []*npc.Npc{pick}
}

// targetsNear 以 center 为中心、radius 内取最多 maxCount 个可打怪（不含 center 自身目标的重复由调用方保证）。
func (m *petManager) targetsNear(center *npc.Npc, radius, maxCount int) []*npc.Npc {
	return m.targetsNearPos(center.X, center.Y, radius, maxCount, center)
}

// targetsNearPos 收集 (x,y) 半径内的可打怪；maxCount<=0 表示不限；skip 中的目标被排除。
func (m *petManager) targetsNearPos(x, y byte, radius, maxCount int, skip ...*npc.Npc) []*npc.Npc {
	var out []*npc.Npc
	for _, n := range m.s.deps.cfg.NPCs.ByMap(m.wp.MapNumber) {
		if !m.isValidTarget(n, skip...) {
			continue
		}
		if chebyshevByte(x, y, n.X, n.Y) > radius {
			continue
		}
		out = append(out, n)
		if maxCount > 0 && len(out) >= maxCount {
			break
		}
	}
	return out
}

// isValidTarget 报告目标是否可被渡鸦攻击：存活、是怪物（排除 Guard/被动 NPC）、未被 skip。
func (m *petManager) isValidTarget(n *npc.Npc, skip ...*npc.Npc) bool {
	if n == nil || !n.Alive() {
		return false
	}
	if n.Def == nil || n.Def.ObjectKind != "Monster" {
		return false
	}
	for _, s := range skip {
		if s == n {
			return false
		}
	}
	return true
}

// petDurability 返回右手槽渡鸦的当前耐久（无宠物/宠物位为空返回 0）。
func (m *petManager) petDurability() byte {
	c := m.c
	if c == nil || c.Inventory == nil {
		return 0
	}
	sl := c.Inventory.GetItem(item.SlotRightHand)
	if sl == nil || sl.It == nil {
		return 0
	}
	return sl.It.Durability
}

// broadcastAttack 向主人及其视野内观察者广播宠物攻击动画（ownerID 按接收者视角解析）。
func (m *petManager) broadcastAttack(petType byte, attackType pet.AttackType, target *npc.Npc) {
	if target == nil {
		return
	}
	wp := m.wp
	for _, o := range m.s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.View == nil {
			continue
		}
		ownerID := wp.ID
		if o.ID == wp.ID {
			ownerID = world.ConstantPlayerID
		}
		_ = o.View.ShowPetAttack(petType, attackType.SkillTypeByte(), ownerID, target.ID)
	}
}

// broadcastIdle 广播渡鸦转入空闲（锁定目标死亡时）。
func (m *petManager) broadcastIdle() {
	m.mu.Lock()
	m.behaviour = pet.BehaviourIdle
	m.locked = nil
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
	if view := m.s.viewFor(m.sess); view != nil {
		_ = view.ShowPetMode(pet.PetTypeByte(false), pet.BehaviourIdle.CommandMode(), attackNoTarget)
	}
}
