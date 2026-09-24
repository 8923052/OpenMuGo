package gameserver

// handler_buff.go —— Buff 技能施放处理（对照原版
// TargetedSkillDefaultPlugin 对 SkillType.Buff 的 ApplyMagicEffectAsync 链路）。
//
// 流程：技能可用/消耗判定（安全区豁免）→ 扣 MP/AG → 按效果定义构造效果 →
// 概率掷骰 → 同 SubType 旧效果顶替（去激活）→ Add 并下发 C1 07 激活 →
// 重算战斗属性（buff 即时进入后续攻击数值）。

import (
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// handleBuffSkill 处理一个对自己施放的 Buff 技能。
func (s *Server) handleBuffSkill(sess *session, c *entity.Character, as *action.SkillDef) {
	d := s.deps
	gc := d.cfg.GameConfig
	def, _ := gc.Skill(as.Number)

	decision := action.CastBuff(skillKnown(c, def), as, c.Stats.CurrentMana, c.Stats.CurrentAbility)
	if decision.Outcome != action.SkillOK {
		d.logger.Printf("gameserver: Buff %d 施放拒绝 %s: outcome=%d", as.Number, c.Name, decision.Outcome)
		return
	}
	c.Stats.CurrentMana = decision.Mana
	c.Stats.CurrentAbility = decision.AG
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(action.CurrentStats{
			Health: c.Stats.CurrentHealth, Shield: c.Stats.CurrentShield,
			Mana: decision.Mana, Ability: decision.AG,
			AttackSpeed: c.Stats.AttackSpeed, MagicSpeed: c.Stats.MagicSpeed,
		})
	}

	// 按效果定义构造效果（as.MagicEffectNumber = skill.MagicEffectDef.Number）。
	effect, chance, ok := player.CreateMagicEffect(gc, c, as.MagicEffectNumber, as.Number, true)
	if !ok {
		d.logger.Printf("gameserver: Buff %d 无效果定义 %s", as.Number, c.Name)
		return
	}
	// 概率掷骰（chance<1 才判定）。
	if chance < 1 && s.world.RNG().NextDouble() >= float64(chance) {
		d.logger.Printf("gameserver: Buff %d 概率失败 chance=%.2f %s", as.Number, chance, c.Name)
		return
	}

	view := s.viewFor(sess)
	// 同 SubType 旧效果顶替 → 先去激活。
	if old, replaced := sess.getEffects().ReplaceBySubType(effect.Definition.SubType); replaced {
		if view != nil {
			_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(old.Definition.Number))
		}
	}
	// Add → added 才下发激活（同号更新不重复发）。
	nowMs := currentMs()
	if added := sess.getEffects().Add(effect, nowMs); added && view != nil {
		_ = view.ShowMagicEffectStatus(true, world.ConstantPlayerID, byte(effect.Definition.Number))
	}
	d.logger.Printf("gameserver: Buff 生效 %s effect#%d sub=%d %dms",
		c.Name, effect.Definition.Number, effect.Definition.SubType, effect.Definition.DurationMs)

	// 重算战斗属性：buff 数值即时影响后续攻击。
	s.refreshCombatValues(sess, c, sess.getResting())
	// 攻速/魔速类 buff 还要回写角色属性并下发 C1 26 FF（属性面板才会变）。
	s.refreshEffectStats(sess, c)
}

// currentMs 返回当前 Unix 毫秒时间戳（效果到期时刻基准）。
func currentMs() int64 { return time.Now().UnixMilli() }

// refreshEffectStats 把活动效果影响到的派生属性（攻击/魔法速度）写回角色属性，
// 并下发 C1 26 FF（对照原版 UpdateStatsExtendedPlugIn：属性系统变化即发当前属性；
// 客户端 ReceiveStatsExtended 的 case 0xff 才会更新属性面板的 AttackSpeed/MagicSpeed）。
//
// 只回写攻速/魔速这两个"纯派生、无当前值语义"的字段：ResolveCharStatsWithEffects 对
// 当前血/蓝/盾是"有持久化值才保留、否则按满状态"，整体覆盖会破坏休息恢复的既有语义
// （player_regen_test.go 里 CurrentHealth/CurrentMana 从 0 起的断言）。
func (s *Server) refreshEffectStats(sess *session, c *entity.Character) {
	if c == nil || c.Stats == nil {
		return
	}
	var effects []action.MagicEffect
	if el := sess.getEffects(); el.Len() > 0 {
		effects = el.Snapshot()
	}
	proj, err := player.ResolveCharStatsWithEffects(s.deps.cfg.GameConfig, c, effects)
	if err != nil {
		s.deps.logger.Printf("gameserver: 重算效果属性失败 %s: %v", c.Name, err)
		return
	}
	c.Stats.AttackSpeed = proj.AttackSpeed
	c.Stats.MagicSpeed = proj.MagicSpeed
	c.Stats.MaximumAttackSpeed = proj.MaximumAttackSpeed
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(currentStatsOf(c.Stats))
	}
}

// handleConsumeEffectItem 处理带 ConsumeEffect 的消耗品（酒 Ale → effect 201）。
//
// 对照 ApplyMagicEffectConsumeHandlerPlugIn.ConsumeItemCoreAsync：
// 按效果号构造效果（chance）→ 概率掷骰 → 同 SubType 旧效果顶替（去激活）→
// 扣耐久/销毁（BaseConsumeHandler）→ Add 并下发 C1 07 激活 → 重算战斗属性。
func (s *Server) handleConsumeEffectItem(sess *session, c *entity.Character, wp *world.Player, si *storage.SlottedItem, effectNum int) {
	gc := s.deps.cfg.GameConfig
	effect, chance, ok := player.CreateMagicEffect(gc, c, effectNum, 0, true)
	if !ok {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 概率掷骰（chance<1 才判定；酒 chance=1 必成功）。
	if chance < 1 && s.world.RNG().NextDouble() >= float64(chance) {
		s.deps.logger.Printf("gameserver: 消耗品效果 %d 概率失败 chance=%.2f %s", effectNum, chance, c.Name)
		s.replyItemConsumeFailed(sess, c)
		return
	}

	view := s.viewFor(sess)
	// 同 SubType 旧效果顶替 → 先去激活。
	if old, replaced := sess.getEffects().ReplaceBySubType(effect.Definition.SubType); replaced && view != nil {
		_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(old.Definition.Number))
	}

	// 扣耐久（消耗品通用：耐久 -1，归零销毁）——在 Add 效果前完成（原版 base.ConsumeItemAsync）。
	si.It.Durability--
	if si.It.Durability == 0 {
		if inv := s.ensureInventory(c); inv.Remove(si) {
			if view != nil {
				_ = view.ShowItemRemoved(si.Slot)
			}
		}
	} else if view != nil {
		_ = view.ShowItemDurabilityChanged(si.Slot, si.It.Durability, true)
	}

	// Add → added 才下发激活（同号更新不重复发）。
	if added := sess.getEffects().Add(effect, currentMs()); added && view != nil {
		_ = view.ShowMagicEffectStatus(true, world.ConstantPlayerID, byte(effect.Definition.Number))
	}
	s.deps.logger.Printf("gameserver: 消耗品效果生效 %s effect#%d sub=%d %dms",
		c.Name, effect.Definition.Number, effect.Definition.SubType, effect.Definition.DurationMs)

	// 重算战斗属性（酒 → AttackSpeedAny +20 进入攻速）。
	s.refreshCombatValues(sess, c, sess.getResting())
	// 回写攻速/魔速并下发 C1 26 FF（属性面板的攻击速度随之变化）。
	s.refreshEffectStats(sess, c)
}

// expireMagicEffects 周期清理所有会话已到期的效果，并下发 C1 07 去激活。
func (s *Server) expireMagicEffects() {
	nowMs := currentMs()
	for _, sess := range s.trackedSessions() {
		el := sess.peekEffects()
		if el == nil {
			continue // 未施放任何效果，不触发懒创建
		}
		expired := el.Expire(nowMs)
		if len(expired) == 0 {
			continue
		}
		view := s.viewFor(sess)
		for _, e := range expired {
			s.deps.logger.Printf("gameserver: Buff 过期 %s effect#%d",
				sess.remoteAddr(), e.Definition.Number)
			if view != nil {
				_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(e.Definition.Number))
			}
		}
		// 效果过期后重算战斗属性，移除过期加成。
		if c := sess.getSelected(); c != nil {
			s.refreshCombatValues(sess, c, sess.getResting())
			// 攻速/魔速回到未加成值并下发 C1 26 FF（属性面板同步回落）。
			s.refreshEffectStats(sess, c)
		}
	}
}

// cancellableSkillEffects 是原版 MagicEffectCancelHandlerPlugIn 的硬编码可取消技能号
// （Infinity Arrow 77/441、Expansion of Wizardry 233/380/383）——原版就是 switch 常量，
// 没有配置开关，照抄不修。
var cancellableSkillEffects = map[uint16]bool{77: true, 233: true, 380: true, 383: true, 441: true}

// handleMagicEffectCancel 处理 C1 1B：玩家主动将某个可取消的自增益从身上摘掉。
func (s *Server) handleMagicEffectCancel(sess *session, frame []byte) {
	if len(frame) < c2s.MagicEffectCancelRequestLength {
		return
	}
	skillID := c2s.AsMagicEffectCancelRequest(frame).SkillId()
	if !cancellableSkillEffects[skillID] {
		return
	}
	c := sess.getSelected()
	if c == nil || s.deps.cfg.GameConfig == nil {
		return
	}
	def, ok := s.deps.cfg.GameConfig.Skill(int(skillID))
	if !ok || def.MagicEffectNumber == nil || !skillKnown(c, def) {
		return
	}
	el := sess.peekEffects()
	if el == nil {
		return
	}
	e, removed := el.RemoveByNumber(int16(*def.MagicEffectNumber))
	if !removed {
		return
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(e.Definition.Number))
	}
	s.refreshCombatValues(sess, c, sess.getResting())
	s.refreshEffectStats(sess, c)
}
