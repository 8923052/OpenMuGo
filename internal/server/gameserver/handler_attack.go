package gameserver

// handler_attack.go —— T2-2 战斗 Hit（C1 11 HitRequest → 0x11/0x17 出站），
// 对照原版 CharacterHitHandlerPlugIn + AttackableExtensions.CalculateDamageAsync。
// MVP 裁剪：无装备 → 物理基础伤害走属性系统（无武器为 0，靠 minLevelDmg=level/10
// 下限，与原版空手行为一致）；暴击/卓越等未装配属性按 0 自然短路；PvP/技能后续接。

import (
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/combat"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/view/remote"
)

// encodeItemForClient 用 S6 客户端要求的**扩展物品布局**编码（ItemSerializerExtended
// 的 5~15B 动态长度），返回精确长度的切片——客户端 CalcItemLength 会按此定长，
// 多写/少写都会让后续物品整体错位。领域模型 item.Item 只描述"是什么"，字节布局在视图层。
func encodeItemForClient(it *item.Item) []byte {
	buf := make([]byte, remote.ItemExtendedMaxSize)
	n := remote.EncodeItemExtended(it, buf)
	return buf[:n]
}

// handleHit 处理 C1 11：玩家普通攻击怪物。
func (s *Server) handleHit(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil || s.deps.cfg.NPCs == nil {
		return
	}
	req := c2s.AsHitRequest(frame)
	targetID := req.TargetId() & 0x7FFF // 出生位旗标不参与对象匹配

	var target *npc.Npc
	for _, n := range s.deps.cfg.NPCs.ByMap(wp.MapNumber) {
		if n.ID == targetID {
			target = n
			break
		}
	}
	if target == nil || !target.Alive() {
		return // PvP 目标随 T2 后续接入
	}
	if !target.IsAttackableByPlayer() {
		return // 守卫/商人/雕像/Gate/陷阱：原版压根不把它们当 IAttackable
	}
	// 记录最近攻击目标（供渡鸦 AttackWithOwner 行为，对照原版 Player.LastAttackedTarget）。
	sess.setLastAttackedTarget(targetID)

	// 距离校验：MVP 近战固定切比雪夫 ≤2（原版用攻击者 AttackRange 属性；
	// 无武器近战距离 2——裁剪登记，接装备系统后改为属性查询）。
	if chebyshevByte(wp.X, wp.Y, target.X, target.Y) > 2 {
		return
	}

	cv := sess.getCombatValues()
	if cv == nil {
		resolved, err := player.ResolveCombatValues(s.deps.cfg.GameConfig, c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 攻击属性解析失败 %s: %v", c.Name, err)
			return
		}
		cv = &resolved
		sess.setCombatValues(cv)
	}

	// 命中 + 伤害（AttackableExtensions.CalculateDamageAsync 完整主干，见 combat 包）。
	result := combat.Calculate(s.world.RNG(), combat.Match{}, cv.AttackerStats(), npcDefender(target), combat.Skill{})
	s.deps.logger.Printf("[debug] hit %s→npc#%d(%s): atkRate=%.1f defRate=%.1f base=[%d..%d] lvl=%d → %s%d kind=%d",
		c.Name, targetID, target.Name, cv.AttackRatePvM, target.Attribute("Defense Rate (PvM)"),
		cv.PhysDmgMin, cv.PhysDmgMax, cv.Level, missTag(result.Miss), result.Damage, result.Kind)

	// 目标视野覆盖集为接收者集合（攻击者是其观察者——能看到自己的伤害数字）。
	// 结算 + 出站广播（普通攻击与技能共用，见 applyDamageToNpc）。
	s.applyDamageToNpc(sess, c, wp, cv, target, result)
}

// applyDamageToNpc 把一次伤害结算落到 NPC 并广播（T2-2 普通攻击 / T2-11 技能共用）：
//   - healthStatus = **扣血后**的受击者血量条（CalcStatStatus：cur/max*250，0xFF=无该资源）；
//     ShowHitExtendedPlugIn：targetId 恒为真实 ID（目标是 NPC，无 GetId 视角差异）；
//   - 死亡：广播 0x17 + 经验（T2-7，顺序**先经验后掉落**、掉落金钱 = 计算经验+7）+
//     击杀即时恢复 + 掉落接线（T2-3）。
//
// 返回接收者集合（动画类广播复用同一 audience）。
func (s *Server) applyDamageToNpc(sess *session, c *entity.Character, wp *world.Player, cv *player.CombatValues, target *npc.Npc, result combat.DamageResult) []*world.Player {
	remaining, died := target.ApplyDamage(result.Damage, time.Now())
	healthStatus := byte(0xFF)
	if maxHP := target.Attribute("Maximum Health"); maxHP > 0 {
		if remaining <= 0 {
			healthStatus = 0
		} else {
			healthStatus = byte(remaining/maxHP*250 + 0.5)
		}
	}
	audience := s.world.Map(wp.MapNumber).PlayersInRangeFor(target.X, target.Y)
	s.deps.logger.Printf("[debug] hit-result npc#%d(%s) hp=%.0f/%.0f status=%d dmg=%s%d aud=%d",
		target.ID, target.Name, remaining, target.Attribute("Maximum Health"), healthStatus, missTag(result.Miss), result.Damage, len(audience))
	for _, o := range audience {
		if o.View != nil {
			_ = o.View.ShowObjectHit(target.ID, healthStatus, 0xFF, uint32(result.Damage), 0, action.DamageKind(result.Kind))
		}
	}

	// 死亡：广播 0x17 + 掉落接线（T2-3：地图掉落组 ∪ 怪物掉落组 → 地面）。
	if died {
		for _, o := range audience {
			if o.View != nil {
				_ = o.View.ShowObjectGotKilled(target.ID)
			}
		}
		// 经验结算（T2-7）：原版 OnDeathAsync 的顺序是**先经验、后掉落**，
		// 且掉落金钱用**计算出的**经验（money = gainedExperience + 7）。
		gained := s.settleKillExperience(sess, c, wp, target)
		// 任务击杀推进（S10）：命中进行中任务的需求怪则累加进度。
		s.advanceQuestKills(sess, c, target.Def.Number)
		// 击杀后即时恢复（原版 OnDeathAsync 顺序：经验 → AfterKilledMonsterAsync → 掉落）。
		s.recoverAfterMonsterKill(sess, c, cv)
		if s.dropGen != nil && s.deps.cfg.Drops != nil {
			moneyRate := 0.0
			if cv != nil && cv.MoneyAmountRate > 0 {
				moneyRate = float64(cv.MoneyAmountRate) // 独角兽"Money Drop Amount Rate"，击杀瞬间取样
			}
			s.scheduleKillLoot(killLoot{
				def: target.Def, mapNumber: wp.MapNumber, x: target.X, y: target.Y,
				gained: gained, moneyRate: moneyRate,
			})
		}
	}
	return audience
}

// maxDropDistance 对照原版 AttackableNpcBase.MaximumDropDistance：首件落原地，
// 其余在此半径内随机可走点。
const maxDropDistance byte = 2

// killLoot 是一次击杀掉落的入参快照。原版 `DropItemDelayedAsync` 里读的是怪物实例
// （this.Position / this.CurrentMap），Go 侧 NPC 对象会被重生复用、字段在别处加锁改写，
// 故在击杀瞬间取坐标快照（可观测结果与原版一致，重生延迟远大于 1s）。
type killLoot struct {
	def       *config.Monster
	mapNumber uint16
	x, y      byte
	gained    int64
	moneyRate float64
}

// scheduleKillLoot 对照 `_ = DropItemDelayedAsync(...)`：整段掉落（生成 + 落地）延后
// dropDelay（原版 1s）且不等待完成——击杀帧的出站因此先于掉落物出现。
// dropDelay ≤ 0 时内联执行（测试与"零延迟"配置下保持可同步断言）。
func (s *Server) scheduleKillLoot(k killLoot) {
	if s.dropDelay <= 0 {
		s.dropKillLoot(k)
		return
	}
	go func() {
		time.Sleep(s.dropDelay)
		s.dropKillLoot(k)
	}()
}

// dropKillLoot 生成并落地掉落物：金钱与首件物品落在怪物坐标，其余在
// MaximumDropDistance(2) 内随机可走点；下发对象是**落地那一刻**视野内的玩家
// （原版 GameMap.AddAsync 按每个掉落物各自的观察者通知，不是击杀瞬间的观众集合）。
func (s *Server) dropKillLoot(k killLoot) {
	groups := s.deps.cfg.GameConfig.DropGroupsFor(k.mapNumber, k.def.Number)
	items, money := s.dropGen.GenerateDrops(k.def, int(k.gained), groups)
	if money > 0 && k.moneyRate > 0 {
		money = uint32(float64(money) * k.moneyRate)
	}
	m := s.world.Map(k.mapNumber)
	now := time.Now()
	if money > 0 {
		id := s.deps.cfg.Drops.AddMoney(k.mapNumber, k.x, k.y, money, now)
		entry := []action.MoneyEntry{{ID: id, X: k.x, Y: k.y, Amount: money, FreshDrop: true}}
		s.showDroppedTo(m, k.x, k.y, nil, entry)
	}
	first := money == 0
	for _, it := range items {
		x, y := k.x, k.y
		if first {
			first = false
		} else {
			x, y = m.RandomCoordinateNear(k.x, k.y, maxDropDistance, s.world.RNG())
		}
		id := s.deps.cfg.Drops.AddItem(k.mapNumber, x, y, it, now)
		s.showDroppedTo(m, x, y,
			[]action.DropEntry{{ID: id, X: x, Y: y, Data: encodeItemForClient(it), FreshDrop: true}}, nil)
	}
	s.deps.logger.Printf("[debug] drops +%v npc#%d(%s): items=%d money=%d (groups=%d)",
		s.dropDelay, k.def.Number, k.def.Name, len(items), money, len(groups))
}

// showDroppedTo 把落地物发给该坐标视野内的玩家（C2 20 / C1 2F）。
func (s *Server) showDroppedTo(m *world.Map, x, y byte, items []action.DropEntry, money []action.MoneyEntry) {
	for _, o := range m.PlayersInRangeFor(x, y) {
		if o.View != nil {
			_ = o.View.ShowDropsInScope(items, money)
		}
	}
}

func missTag(miss bool) string {
	if miss {
		return "MISS "
	}
	return ""
}

// npcDefender 把怪物属性投影为 combat 防御方入参。
// 怪物未装配 GreaterDefense/DefenseDecrement/ArmorDecrease/DamageReceive 时为 0，
// Calculate 内部对 ≤0 的减伤/减免倍率按 1 处理（与怪物属性栏默认一致）。
func npcDefender(n *npc.Npc) combat.DefenderStats {
	return combat.DefenderStats{
		Defense:     n.Attribute("Base Defense"),
		DefenseRate: n.Attribute("Defense Rate (PvM)"),
	}
}
