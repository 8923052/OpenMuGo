package gameserver

// handler_skill.go —— T2-11 技能：C3 0x19 定向 / C3 0x1E 区域，
// 对照原版：
//   - TargetedSkillHandlerPlugIn（0x19）→ TargetedSkillDefaultPlugin.PerformSkillAsync
//     （技能存在 → 安全区拒 → Range+2 → TryConsumeForSkillAsync → 伤害 →
//     ForEachWorldObserverAsync ShowSkillAnimation，sendToSelf: true）；
//   - AreaSkillAttackHandlerPlugIn（0x1E）→ AreaSkillAttackAction.AttackAsync
//     （消耗一次性扣减 → 区域目标自动命中 → ShowAreaSkillAnimation 广播）；
//   - 伤害 = AttackableExtensions.CalculateDamageAsync 携带 SkillEntry 的主干
//     （combat.Calculate：技能加成 [AttackDamage, ×1.5] 叠基础伤害后走同一
//     命中/防御/暴击/卓越/减伤/下限/倍率管线）。
//
// 裁剪登记（doc/15 T2-11）：
//   - 已实现 DetermineTargets 对齐：ExplicitWithImplicitInRange（target=6，如 Death Stab/Fire Burst）
//     溅射主目标附近活怪；NumberOfHitsPerAttack>1（如 Killing Blow）逐发重掷；
//   - SkillHitValidator（客户端命中计数防作弊）未复刻；技能列表≈职业合格集（未做学习/持久化）；
//   - 玩家 splash 随 PvP 玩法路径接入（当前只取怪）；AreaSkillExplicitHits 二段包(0x1D)、召唤、
//     位移(MovesToTarget)、命中附带元素效果(TryApplyElementalEffects)随各自子系统。
//   - 连击(Combo)状态机与推进已接（TRIM-06，见 comboMachine）；完成时按原版发技能动画包 59。

import (
	"math"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/combat"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// findNpcTarget 按 ID 在当前地图找活着的 NPC（handleHit 同一查询语义）。
func (s *Server) findNpcTarget(wp *world.Player, targetID uint16) *npc.Npc {
	if s.deps.cfg.NPCs == nil {
		return nil
	}
	for _, n := range s.deps.cfg.NPCs.ByMap(wp.MapNumber) {
		if n.ID == targetID {
			return n
		}
	}
	return nil
}

// skillKnown 判定角色"已学习"该技能。原版查 SkillList（持久化的已学技能）；
// 本仓以角色 LearnedSkills 为准，未持久化学习（T3 前）时回落职业合格集，
// 保持 T2-11 既有可施放性（裁剪登记）。
func skillKnown(c *entity.Character, def *config.Skill) bool {
	if learnedSkillContains(c, def.Number) {
		return true
	}
	for _, n := range def.QualifiedClasses {
		if n == int(c.ClassNumber) {
			return true
		}
	}
	return false
}

// skillDef 投影 config.Skill → action.SkillDef。
func skillDef(sk *config.Skill) *action.SkillDef {
	d := &action.SkillDef{
		Number:              sk.Number,
		SkillType:           sk.SkillType,
		Target:              sk.Target,
		DamageType:          sk.DamageType,
		Range:               sk.Range,
		ImplicitTargetRange: sk.ImplicitTargetRange,
		AttackDamage:        sk.AttackDamage,
		HitsPerAttack:       sk.HitsPerAttack,
	}
	for _, c := range sk.Consume {
		d.Consume = append(d.Consume, action.SkillConsume{Attribute: c.Attribute, Value: c.Value})
	}
	if sk.MagicEffectNumber != nil {
		d.MagicEffectNumber = *sk.MagicEffectNumber
	}
	return d
}

// casterCombatValues 取（或补算）施法者战斗属性快照。
func (s *Server) casterCombatValues(sess *session, c *entity.Character) *player.CombatValues {
	if cv := sess.getCombatValues(); cv != nil {
		return cv
	}
	resolved, err := player.ResolveCombatValues(s.deps.cfg.GameConfig, c)
	if err != nil {
		s.deps.logger.Printf("gameserver: 技能属性解析失败 %s: %v", c.Name, err)
		return nil
	}
	cv := &resolved
	sess.setCombatValues(cv)
	return cv
}

// broadcastSkillAnimation 把技能动画发给自己 + 观察者（原版 sendToSelf: true；
// 自己恒收哨兵 0x200，观察者收施法者真实 ID）。
func (s *Server) broadcastSkillAnimation(wp *world.Player, skillID uint16, area bool, x, y, rotation byte, targetID uint16) {
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.View == nil {
			continue
		}
		pid := o.ID // 观察者视角：施法者真实 ID；自己 = 哨兵
		if o.ID == wp.ID {
			pid = world.ConstantPlayerID
		}
		if area {
			_ = o.View.ShowAreaSkillAnimation(pid, skillID, x, y, rotation)
		} else {
			_ = o.View.ShowSkillAnimation(pid, skillID, targetID)
		}
	}
}

// handleTargetedSkill 处理 C3 0x19：对单一目标施放技能。
func (s *Server) handleTargetedSkill(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.TargetedSkillLength {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil || c.Stats == nil {
		return
	}
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	req := c2s.AsTargetedSkill(frame)
	def, ok := gc.Skill(int(req.SkillId()))
	if !ok {
		return
	}
	as := skillDef(def)

	// Buff 技能（目标自己）分流：不走 NPC 目标/距离判定。
	if def.SkillType == action.SkillTypeBuff {
		s.handleBuffSkill(sess, c, as)
		return
	}
	// Regeneration 瞬时恢复（Heal/Recovery）：目标自己，按效果定义给当前血/盾加值。
	if def.SkillType == action.SkillTypeRegeneration {
		s.handleRegenerationSkill(sess, c, wp, as)
		return
	}
	// PassiveBoost/SummonMonster/Other 不走 NPC 直接伤害结算（被动无需施放；召唤/传送随各自子系统）。
	if def.SkillType == action.SkillTypePassiveBoost || def.SkillType == action.SkillTypeSummonMonster || def.SkillType == action.SkillTypeOther {
		return
	}

	target := s.findNpcTarget(wp, req.TargetId()&0x7FFF)
	if target == nil || !target.Alive() {
		return // PvP 目标随 T3-B PvP 接入（原版 GetObject 统一取 IAttackable）
	}

	cv := s.casterCombatValues(sess, c)
	if cv == nil {
		return
	}
	mp := s.world.Map(wp.MapNumber)
	inRange := int(chebyshevByte(wp.X, wp.Y, target.X, target.Y)) <= as.Range+2
	decision := action.CastTargeted(skillKnown(c, def), as,
		mp.Safezone(wp.X, wp.Y), inRange, c.Stats.CurrentMana, c.Stats.CurrentAbility)
	if decision.Outcome != action.SkillOK {
		// 原版失败分支无回包（客户端动画自然中断），仅记日志。
		s.deps.logger.Printf("gameserver: 技能 %d 施放拒绝 %s: outcome=%d", def.Number, c.Name, decision.Outcome)
		return
	}
	c.Stats.CurrentMana = decision.Mana
	c.Stats.CurrentAbility = decision.AG
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(action.CurrentStats{
			Health: uint32(c.Stats.CurrentHealth), Shield: uint32(c.Stats.CurrentShield),
			Mana: decision.Mana, Ability: decision.AG,
			AttackSpeed: c.Stats.AttackSpeed, MagicSpeed: c.Stats.MagicSpeed,
		})
	}

	// 连击推进（对照 TargetedSkillDefaultPlugin.cs:256-265）：只有直伤类技能、角色有连击
	// 状态机（属性 Is Skill Combo Available）、双方都不在安全区且都活着时才登记一次。
	isCombo := false
	if cv.ComboAvailable &&
		(def.SkillType == action.SkillTypeDirectHit || def.SkillType == action.SkillTypeCastleSiegeSkill) &&
		!mp.Safezone(wp.X, wp.Y) && !mp.Safezone(target.X, target.Y) && wp.IsAlive && target.Alive() {
		isCombo = s.comboMachine(sess, c).RegisterSkill(gc.SkillBaseNumber(def.Number), time.Now())
	}

	// 目标集合：ExplicitWithImplicitInRange 会溅射主目标附近活怪（对照 DetermineTargets）。
	for _, tg := range s.determineSkillTargets(wp, target, as) {
		s.castDamage(sess, c, wp, cv, as, tg, isCombo)
		// NumberOfHitsPerAttack > 1：后续 hit 逐发重掷（原版 AttackByAsync 循环）。
		for hit := 2; hit <= maxInt(as.HitsPerAttack, 1); hit++ {
			if tg.Alive() {
				s.castDamage(sess, c, wp, cv, as, tg, isCombo)
			}
		}
	}

	if isCombo {
		// 终结击额外广播一次"连击动画"（ShowSkillAnimationPlugIn.cs:29,64-69：技能号恒 59）。
		s.broadcastSkillAnimation(wp, comboAnimationSkill, false, 0, 0, 0, target.ID)
	}

	s.broadcastSkillAnimation(wp, uint16(def.Number), false, 0, 0, 0, target.ID)
}

// determineSkillTargets 依据 SkillTarget 选出本次命中目标（对照 TargetedSkillDefaultPlugin.DetermineTargets）。
// 仅 ExplicitWithImplicitInRange（target=6）会溅射：主目标 + 其 ImplicitTargetRange 内的其他活怪；
// 其余（含 Explicit）只打主目标。玩家 splash 随 PvP 接入（原版 AreaSkillHitsPlayer 默认 false，只取怪）。
func (s *Server) determineSkillTargets(wp *world.Player, primary *npc.Npc, as *action.SkillDef) []*npc.Npc {
	if as.Target != action.SkillTargetExplicitWithImplicit || as.ImplicitTargetRange <= 0 || s.deps.cfg.NPCs == nil {
		return []*npc.Npc{primary}
	}
	res := []*npc.Npc{primary}
	for _, n := range s.deps.cfg.NPCs.ByMap(wp.MapNumber) {
		if n == primary || !n.Alive() || !n.IsAttackableByPlayer() {
			continue
		}
		if int(chebyshevByte(primary.X, primary.Y, n.X, n.Y)) <= as.ImplicitTargetRange {
			res = append(res, n)
		}
	}
	return res
}

// handleAreaSkill 处理 C3 0x1E：以目标区中心为圆心（切比雪夫）自动命中。
func (s *Server) handleAreaSkill(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.AreaSkillLength {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil || c.Stats == nil {
		return
	}
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	req := c2s.AsAreaSkill(frame)
	def, ok := gc.Skill(int(req.SkillId()))
	if !ok {
		return
	}
	as := skillDef(def)
	cv := s.casterCombatValues(sess, c)
	if cv == nil {
		return
	}
	mp := s.world.Map(wp.MapNumber)
	decision := action.CastArea(skillKnown(c, def), as,
		mp.Safezone(wp.X, wp.Y), c.Stats.CurrentMana, c.Stats.CurrentAbility)
	if decision.Outcome != action.SkillOK {
		s.deps.logger.Printf("gameserver: 区域技能 %d 施放拒绝 %s: outcome=%d", def.Number, c.Name, decision.Outcome)
		return
	}
	c.Stats.CurrentMana = decision.Mana
	c.Stats.CurrentAbility = decision.AG
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(action.CurrentStats{
			Health: uint32(c.Stats.CurrentHealth), Shield: uint32(c.Stats.CurrentShield),
			Mana: decision.Mana, Ability: decision.AG,
			AttackSpeed: c.Stats.AttackSpeed, MagicSpeed: c.Stats.MagicSpeed,
		})
	}

	// 目标选择 + 结算（对照 AreaSkillAttackAction.GetTargets/GetTargetsInRange +
	// PerformAutomaticHitsAsync/AttackTargetsAsync）：
	//   - 候选：点击点周围 (EffectRange>0 ? EffectRange : Range) 的活怪（切比雪夫，与原版
	//     AOI 网格半径一致），排除安全区目标；
	//   - 形状：扇形(frustum) 以**施法者位置 + rotation** 展开梯形，梯形外（含背后）不打；
	//     点击点小圆(target-area) 以 TargetAreaDiameter/2 为欧氏半径收口；
	//   - 结算：默认设置每目标一次；否则按最大次数/命中概率衰减多段命中。
	// 裁剪登记：UseDeferredHits 的按距离/轮次**延迟错峰**（纯视觉到达时序）当前同步结算，
	// 随计时子系统接入；命中集合与每目标次数与原版一致。
	// 区域技能路径的连击门更松（AreaSkillAttackAction.cs:199-203：只要角色有连击状态机
	// 就推进，不看安全区），因为它在选目标之前登记。
	isCombo := false
	if cv.ComboAvailable {
		isCombo = s.comboMachine(sess, c).RegisterSkill(gc.SkillBaseNumber(def.Number), time.Now())
	}

	area := def.Area
	cx, cy := req.TargetX(), req.TargetY()
	rotation := req.Rotation()
	targets := s.selectAreaTargets(wp, s.world.Map(wp.MapNumber), cx, cy, rotation, as, area, req.ExtraTargetId())

	if area == nil || area.IsDefault() {
		for _, n := range targets {
			s.castDamage(sess, c, wp, cv, as, n, isCombo)
		}
	} else {
		s.applyAreaHits(sess, c, wp, cv, as, area, targets, isCombo)
	}

	s.broadcastSkillAnimation(wp, uint16(def.Number), true, cx, cy, rotation, 0)
	if isCombo {
		s.broadcastSkillAnimation(wp, comboAnimationSkill, false, 0, 0, 0, 0)
	}
}

// selectAreaTargets 组装区域技能的命中目标集（对照 GetTargets/GetTargetsInRange）。
func (s *Server) selectAreaTargets(wp *world.Player, mp *world.Map, cx, cy byte, rotation byte,
	as *action.SkillDef, area *config.AreaSkillSettings, extraTargetID uint16) []*npc.Npc {
	if s.deps.cfg.NPCs == nil {
		return nil
	}
	// 显式额外目标（AreaSkillExplicitTarget / 二段命中）：在施法者 Range+2 内、非安全区则纳入。
	var extra *npc.Npc
	if extraTargetID != 0 && extraTargetID != 0xFFFF {
		if n := s.findNpcTarget(wp, extraTargetID); n != nil && n.Alive() &&
			!mp.Safezone(n.X, n.Y) &&
			int(chebyshevByte(wp.X, wp.Y, n.X, n.Y)) <= as.Range+2 {
			extra = n
		}
	}
	// AreaSkillExplicitTarget（type=5）：只打显式额外目标（原版 yield break 分支）。
	if as.SkillType == action.SkillTypeAreaSkillExplicitTgt {
		if extra != nil {
			return []*npc.Npc{extra}
		}
		return nil
	}

	candRadius := as.Range
	if area != nil && area.EffectRange > 0 {
		candRadius = area.EffectRange
	}
	var filter *action.FrustumFilter
	if area != nil && area.UseFrustumFilter {
		filter = action.NewFrustumFilter(area.FrustumStartWidth, area.FrustumEndWidth, area.FrustumDistance, area.ProjectileCount)
	}
	useTargetArea := area != nil && area.UseTargetAreaFilter
	targetAreaRadius := 0.0
	if useTargetArea {
		targetAreaRadius = area.TargetAreaDiameter * 0.5
	}

	targets := make([]*npc.Npc, 0, 8)
	if extra != nil {
		targets = append(targets, extra) // 原版先产出额外目标，后面跳过其 ID
	}
	for _, n := range s.deps.cfg.NPCs.ByMap(wp.MapNumber) {
		if !n.Alive() || !n.IsAttackableByPlayer() || (extra != nil && n.ID == extra.ID) {
			continue
		}
		if int(chebyshevByte(cx, cy, n.X, n.Y)) > candRadius {
			continue // 候选：点击点周围
		}
		if mp.Safezone(n.X, n.Y) {
			continue // 安全区目标不打（原版 !a.IsAtSafezone()）
		}
		if filter != nil && !filter.WithinBounds(int(wp.X), int(wp.Y), rotation, int(n.X), int(n.Y)) {
			continue // 扇形：梯形外（含背后）不打
		}
		if useTargetArea && euclidTo(n.X, n.Y, cx, cy) >= targetAreaRadius {
			continue // 点击点小圆收口
		}
		targets = append(targets, n)
	}
	return targets
}

// applyAreaHits 按 AreaSkillSettings 的多段命中/命中衰减/**延迟错峰**结算
// （对照 AreaSkillAttackAction.AttackTargetsAsync:232-378）。
//
// 原版把每一发的到达时刻算成 `currentDelay + DelayPerOneDistance × 距离`
// （currentDelay 每轮累加 DelayBetweenHits），时刻为 0 才就地结算，否则
// `Task.Run` + `Task.Delay` 延后打 —— 也就是说**伤害本身**是延迟的，不只是动画。
// S6 导出件里有 14/44 条面积技能开着 UseDeferredHits（如 8：300ms/格 + 1000ms/轮，
// 24/414/418：50ms/格，65 Electric Spike：10ms/格）。
//
// 命中集合与判定序照原版：跳过已死目标 → 轮次 ≥ MinHitsPerTarget 后按
// mult^距离、否则超过 minAttacks 后按 0.5 → 到点前再复检目标仍存活且不在安全区
// （原版 `!target.IsAtSafezone() && target.IsActive()`；怪物没有传送态，故等价于存活）。
func (s *Server) applyAreaHits(sess *session, c *entity.Character, wp *world.Player, cv *player.CombatValues,
	as *action.SkillDef, area *config.AreaSkillSettings, targets []*npc.Npc, isCombo bool) {
	maxAttacks := area.MaxHitsPerAttack
	unlimited := maxAttacks == 0 // 原版用 int.MaxValue
	if unlimited {
		maxAttacks = len(targets) * maxInt(area.MaxHitsPerTarget, 1)
	}
	minAttacks := area.MinHitsPerAttack
	if minAttacks == 0 {
		minAttacks = maxAttacks
	}
	attackRounds := maxInt(area.MaxHitsPerTarget, 1)
	var currentDelay time.Duration
	attackCount := 0
	for round := 0; round < attackRounds; round++ {
		for _, n := range targets {
			if attackCount >= maxAttacks {
				break
			}
			if !n.Alive() {
				continue // 前几轮已击杀（原版 IsAlive 复检）
			}
			dist := euclidTo(n.X, n.Y, wp.X, wp.Y)
			var hitChance float64
			switch {
			case round >= area.MinHitsPerTarget:
				hitChance = math.Pow(area.HitChancePerDistanceMultiplier, dist)
			case attackCount >= minAttacks:
				hitChance = 0.5
			default:
				hitChance = 1.0
			}
			if hitChance < 1.0 && !s.world.RNG().NextRandomChance(hitChance) {
				continue
			}
			delay := currentDelay + time.Duration(float64(area.DelayPerOneDistanceMs)*dist)*time.Millisecond
			attackCount++
			if !areaHitLegal(s, n) {
				continue // 打之前复检（原版两支路都有这道门）
			}
			if delay <= 0 {
				s.castDamage(sess, c, wp, cv, as, n, isCombo)
				continue
			}
			s.scheduleAreaHit(sess, c, wp, cv, as, n, isCombo, delay)
		}
		currentDelay += time.Duration(area.DelayBetweenHitsMs) * time.Millisecond
	}

	// Electric Spike(65) 的队伍代价（:361-372）：确实打中过目标、且附近有队友时，
	// 抽掉"视野里能看到施法者"的队友 20% 当前生命与 5% 当前法力。
	// 原版的门是活属性 NearbyPartyMemberCount（Party.cs:511 按 Observers 现算）；
	// 本仓没有那条活属性 → 用同一个"队友在施法者视野内"的判据直接数（等价，登记见 doc/16）。
	if as.Number == skillElectricSpike && attackCount > 0 {
		s.electricSpikePartyDrain(wp)
	}
}

// scheduleAreaHit 排队一次延迟伤害。到点时按原版复检目标（存活 + 不在安全区），
// 并额外要求施法者仍在世界里：原版不查施法者，但那边玩家下线后属性系统仍存活，
// 本仓的会话/连接会被释放，继续写会打到已注销的角色（差异登记见 doc/16 TRIM-11）。
func (s *Server) scheduleAreaHit(sess *session, c *entity.Character, wp *world.Player, cv *player.CombatValues,
	as *action.SkillDef, n *npc.Npc, isCombo bool, delay time.Duration) {
	schedule := s.areaHitScheduler
	if schedule == nil {
		schedule = scheduleAfterDefault
	}
	schedule(delay, func() {
		sess.opMu.Lock()
		defer sess.opMu.Unlock()
		if sess.getState() != entity.StateEnteredWorld || sess.getWorldPlayer() != wp {
			return
		}
		if !areaHitLegal(s, n) {
			return
		}
		s.castDamage(sess, c, wp, cv, as, n, isCombo)
	})
}

// areaHitLegal 复检原版打一發前的两道门：目标仍活跃（怪物 = 存活，IsActive 里的 !IsTeleporting
// 对怪物恒真）且不在安全区（LocateableExtensions.IsAtSafezone）。
func areaHitLegal(s *Server, n *npc.Npc) bool {
	if !n.Alive() {
		return false
	}
	mp := s.world.Map(n.MapNumber)
	return mp == nil || !mp.Safezone(n.X, n.Y)
}

// scheduleAfterDefault 是真时钟实现（对照 Task.Run + Task.Delay）。
func scheduleAfterDefault(delay time.Duration, fn func()) {
	time.AfterFunc(delay, fn)
}

// skillElectricSpike 是原版在 AttackTargetsAsync 里硬编码的技能号（:23）。
const skillElectricSpike = 65

// electricSpikePartyDrain 对"视野里能看到施法者"的队友抽 20% 当前生命、5% 当前法力
// （原版 `player.Party.PartyList.OfType<Player>().Where(m => m.Observers.Contains(player))`）。
func (s *Server) electricSpikePartyDrain(caster *world.Player) {
	for _, name := range s.parties.membersOf(caster.Name) {
		if name == caster.Name {
			continue
		}
		sess := s.sessionOfCharacter(name)
		if sess == nil {
			continue
		}
		mwp := sess.getWorldPlayer()
		if mwp == nil || mwp.MapNumber != caster.MapNumber || !mwp.IsAlive {
			continue
		}
		if !playerSees(s.world.Map(mwp.MapNumber), mwp, caster) {
			continue // 队友看不见施法者（原版 Observers.Contains）
		}
		drain := mwp.Stats
		if mc := sess.getSelected(); mc != nil && mc.Stats != nil {
			drain = mc.Stats // 与 wp.Stats 通常同对象；取角色态为权威
		}
		if drain == nil {
			continue
		}
		drain.CurrentHealth = uint32(float32(drain.CurrentHealth) * 0.8)
		drain.CurrentMana = uint32(float32(drain.CurrentMana) * 0.95)
		mwp.Stats = drain
		// 原版靠属性变更事件把新值带进后续包；本仓没有活属性系统，立即补发一帧当前状态，
		// 避免客户端在下次刷新前仍显示旧血蓝（差异登记见 doc/16 TRIM-11）。
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowCurrentStatsExtended(action.CurrentStats{
				Health: drain.CurrentHealth, Shield: drain.CurrentShield, Mana: drain.CurrentMana,
				Ability: drain.CurrentAbility, AttackSpeed: drain.AttackSpeed, MagicSpeed: drain.MagicSpeed,
			})
		}
	}
}

// playerSees 报告 viewer 的可视集合里有没有 target（对照原版维护的 Observers 集合；
// 本仓可见集按坐标即时算，同一判据见 world.Map.PlayersInRangeFor）。
func playerSees(mp *world.Map, viewer, target *world.Player) bool {
	if mp == nil || viewer == nil || target == nil {
		return false
	}
	for _, o := range mp.PlayersInRangeFor(viewer.X, viewer.Y) {
		if o.ID == target.ID {
			return true
		}
	}
	return false
}

// euclidTo 返回两点欧氏距离（原版 GetDistanceTo 语义）。
func euclidTo(ax, ay, bx, by byte) float64 {
	return math.Hypot(float64(int(ax)-int(bx)), float64(int(ay)-int(by)))
}

// castDamage 对单个 NPC 目标做一次技能伤害结算（含死亡/掉落全链）。
func (s *Server) castDamage(sess *session, c *entity.Character, wp *world.Player, cv *player.CombatValues, as *action.SkillDef, target *npc.Npc, isCombo bool) {
	// 技能基伤：原版 GetDamage(:733-757) 会把"没有 TargetAttribute 的大师技能"的等级值
	// 直接加进 AttackDamage（有目标属性那些已走属性系统，见 player/master_effects.go）。
	damage := as.AttackDamage
	if gc := s.deps.cfg.GameConfig; gc != nil {
		if sk, ok := gc.Skill(as.Number); ok {
			damage = player.SkillAttackDamage(gc, c, sk)
		}
	}
	skillMin, skillMax := action.SkillDamageBounds(damage)
	sk := combat.Skill{
		Used:         true,
		DamageType:   as.DamageType,
		ExtraMin:     skillMin,
		ExtraMax:     skillMax,
		DamageFactor: 1,
		Number:       as.Number,
	}
	result := combat.Calculate(s.world.RNG(), combat.Match{IsCombo: isCombo}, cv.AttackerStats(), npcDefender(target), sk)
	s.applyDamageToNpc(sess, c, wp, cv, target, result)
}

// handleRegenerationSkill 处理瞬时恢复技能（Heal/Recovery，SkillType.Regeneration）。
// 对照 AttackableExtensions.ApplyRegenerationAsync：按效果定义里指向"当前血/盾/蓝"
// 的恢复项给目标加值并钳到上限；不走持续效果、不伤人。本仓目标恒为自己
// （原版 Explicit 目标可为队友，队伍/PvP 目标解析随 T3 接入——裁剪登记）。
func (s *Server) handleRegenerationSkill(sess *session, c *entity.Character, wp *world.Player, as *action.SkillDef) {
	d := s.deps
	gc := d.cfg.GameConfig
	def, _ := gc.Skill(as.Number)

	decision := action.CastRegeneration(skillKnown(c, def), as, c.Stats.CurrentMana, c.Stats.CurrentAbility)
	if decision.Outcome != action.SkillOK {
		d.logger.Printf("gameserver: 恢复技能 %d 施放拒绝 %s: outcome=%d", as.Number, c.Name, decision.Outcome)
		return
	}
	c.Stats.CurrentMana = decision.Mana
	c.Stats.CurrentAbility = decision.AG

	effect, _, ok := player.CreateMagicEffect(gc, c, as.MagicEffectNumber, as.Number, true)
	if !ok {
		d.logger.Printf("gameserver: 恢复技能 %d 无效果定义 %d %s", as.Number, as.MagicEffectNumber, c.Name)
		return
	}
	for _, p := range effect.PowerUps {
		switch p.Attribute {
		case "Current Health":
			c.Stats.CurrentHealth = addClampU32(c.Stats.CurrentHealth, uint32(p.Value), c.Stats.MaximumHealth)
		case "Current Shield":
			c.Stats.CurrentShield = addClampU32(c.Stats.CurrentShield, uint32(p.Value), c.Stats.MaximumShield)
		case "Current Mana":
			c.Stats.CurrentMana = addClampU32(c.Stats.CurrentMana, uint32(p.Value), c.Stats.MaximumMana)
		}
	}
	d.logger.Printf("gameserver: 恢复技能生效 %s skill#%d hp=%d sd=%d", c.Name, as.Number, c.Stats.CurrentHealth, c.Stats.CurrentShield)
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(currentStatsOf(c.Stats))
	}
	s.broadcastSkillAnimation(wp, uint16(as.Number), false, 0, 0, 0, world.ConstantPlayerID)
}

// addClampU32 把 add 加到 cur 并钳到 max（溢出防护：add 已很小，仍防 cur+add 回绕）。
func addClampU32(cur, add, max uint32) uint32 {
	if add >= max-cur {
		return max
	}
	return cur + add
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// comboAnimationSkill 是"连击完成"动画的技能号（原版 ShowSkillAnimationPlugIn.cs:29 的
// ComboSkillId 常量；原版用它给自已与观察者各发一条 C1 19 动画包）。
const comboAnimationSkill = 59

// comboMachine 懒建角色职业链上的连击状态机（对照 Player.cs:1743-1746 建、
// :373 按属性 Is Skill Combo Available 交出）。返回 nil 表示这条职业链上没有连击定义。
func (s *Server) comboMachine(sess *session, c *entity.Character) *action.Combo {
	if sess.combo != nil {
		return sess.combo
	}
	gc := s.deps.cfg.GameConfig
	if gc == nil || c == nil {
		return nil
	}
	def := gc.ComboForClass(int(c.ClassNumber))
	if def == nil {
		return nil
	}
	sess.combo = action.NewCombo(def)
	return sess.combo
}
