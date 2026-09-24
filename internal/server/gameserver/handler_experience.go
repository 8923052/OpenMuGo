package gameserver

// handler_experience.go —— T2-7 击杀经验结算与升级（本仓第三层：装配 + 出站驱动）。
//
// 对照原版：
//   - GameLogic/AttackableExtensions.cs `CalculateBaseExperience`（公式在 action/experience.go）
//   - GameLogic/PlayerExperience.cs `CalculateAfterKillAsync`（三项倍率）
//     / `AddExperienceCoreAsync`（逐步入账 + 升级 + 跨级连环）
//   - GameLogic/NPC/AttackableNpcBase.cs `OnDeathAsync`（**先经验、后掉落**；掉落金钱用计算值）
//   - GameLogic/Player.cs `SetReclaimableAttributesToMaximum`（升级即补满四项资源）
//   - GameServer/RemoteView/Character/AddExperienceExtendedPlugIn.cs（C3 16 扩展形态）
//   - GameServer/RemoteView/Character/UpdateLevelExtendedPlugIn.cs（C1 F3 05）
//   - GameServer/RemoteView/Character/ShowEffectPlugIn.cs（C1 48；自己 →
//     ViewExtensions.ConstantPlayerId，观察者 → 真实对象 ID）
//
// 裁剪登记（doc/15 T2-7）：
//   - 队伍分摊（`Party.DistributeExperienceAfterKillAsync`）未接：本仓单人结算。
//   - 大师经验分支（MasterExperience / MasterLevel）已接：TRIM-09，见 handler_master.go。
//   - 升级蓝字 LevelUpCongrats 已接（每级一句，随 C1 F3 05 之后；大师版 MasterLevelUpCongrats
//     随 F3 51，同样在 handler_master.go）。
//   - 果实上限（TRIM-11d）：`GetMaximumFruitPoints` 的三张表已在 action.MaxFruitPoints 落地，
//     ResolveCharStats 按等级+职业 FruitCalculation 现算 → 原版把**同一个**值填进
//     MaximumFruitPoints 与 MaximumNegativeFruitPoints，本仓同样（见下方 levelUpInfo）。
//   - 原版掉落延迟 1s 已接（TRIM-04 的 dropDelay，见 handler_attack.go）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
)

const (
	// desExperienceRate / desBonusExperienceRate 与导出件 designation 逐字一致。
	desExperienceRate      = "Experience Rate"
	desBonusExperienceRate = "Bonus Experience Rate"

	// desPointsPerLevelUp 与导出件 designation 逐字一致（T2-8 升级点累加）。
	desPointsPerLevelUp = "Points per Level up"

	// constantPlayerID 对照原版 ViewExtensions.ConstantPlayerId =
	// "接收者视角下自己的对象 ID"（0x200）。
	constantPlayerID uint16 = 0x0200
)

// settleKillExperience 结算一次击杀的经验，返回**计算出的**经验量。
//
// 为什么返回计算值而不是实际入账值：原版 `AddAfterKillAsync` 返回的是
// `CalculateAfterKillAsync` 的结果，`DropItemAsync` 把它（`experienceShares` 合计）
// 交给掉落器算金钱（`money = gainedExperience + BaseMoneyDrop(7)`）。因此即使角色
// 已达等级上限、实际入账为 0，掉落金钱仍按计算值给。
//
// 注意调用顺序：原版 OnDeathAsync 是**先结算经验、后生成掉落**，不可颠倒。
func (s *Server) settleKillExperience(sess *session, c *entity.Character, wp *world.Player, target *npc.Npc) int64 {
	gc := s.deps.cfg.GameConfig
	if c == nil || c.Stats == nil || gc == nil || target == nil {
		return 0
	}

	base := action.CalculateBaseExperience(target.Attribute("Level"), float32(c.Level))
	if base <= 0 {
		return 0
	}
	// 三项倍率（原版逐项相乘）。读不到倍率属性时 AttributeValue 返回 0，
	// ExperienceRateMultiplier 把 0 视为 1——即"无数据不改变结果"。
	// 服务端倍率与地图倍率改从 05_game_config / 地图定义取（不再写死 1）。
	mapExpMultiplier := 1.0
	if m, ok := gc.Map(int(wp.MapNumber)); ok && m != nil && m.ExpMultiplier > 0 {
		mapExpMultiplier = m.ExpMultiplier
	}
	rate := action.ExperienceRateMultiplier{
		// GameConfiguration.ExperienceRate（05_game_config；0 由 Value() 视为 1）。
		ServerRate: gc.Globals.ExperienceRate,
		// attr[Experience Rate] + attr[Bonus Experience Rate]（职业基值 1 + 无基值 0）。
		Character: player.AttributeValue(gc, c, desExperienceRate) +
			player.AttributeValue(gc, c, desBonusExperienceRate),
		// map.Definition.ExpMultiplier。
		Map: mapExpMultiplier,
	}
	calculated := int64(float64(base) * rate.Value())

	// 大师经验分支（对照 PlayerExperience.TryGetExperienceKind :136-149）：
	// 大师职业且**普通等级已满**时才改吃大师经验，计算式完全相同。
	if cls, ok := gc.Class(int(c.ClassNumber)); ok && cls.IsMasterClass &&
		int(c.Level) == gc.Experience.MaximumLevel {
		return s.settleMasterExperience(sess, c, wp, target, calculated)
	}

	startLevel := c.Level
	gain := action.ApplyExperience(int(c.Level), int64(c.Stats.Experience), calculated,
		gc.Experience.Table, gc.Experience.MaximumLevel, gc.Globals.PreventExperienceOverflow)

	view := s.viewFor(sess)
	for _, step := range gain.Steps {
		if view != nil {
			_ = view.ShowExperienceGained(action.ExperienceGainPacket{
				Result:     step.Type,
				Experience: uint32(step.Amount),
				// DamageOfLastHit 只对**队友**非零（原版 `player.Id != obj.LastDeath.KillerId`），
				// 自己击杀 → 0。
				Damage: 0,
				// KilledObjectId 是怪物真实对象 ID；KillerObjectId 是自己 → 哨兵 0x200。
				KilledObjectID: target.ID,
				KillerObjectID: constantPlayerID,
			})
		}
		if !step.LeveledUp {
			continue
		}
		// 先落地新等级：属性系统按等级重建，必须看到升级后的等级。
		c.Level = uint16(step.Level)
		s.applyLevelUp(sess, c, wp, view)
	}

	// 终态落地（不可达上限：MaxLevelReached 分支不改经验，此处为等值写回）。
	c.Level = uint16(gain.NewLevel)
	c.Stats.Experience = uint64(gain.NewExperience)
	if next := gc.ExperienceForLevel(gain.NewLevel + 1); next >= 0 {
		c.Stats.ExperienceNext = uint64(next)
	}
	s.deps.logger.Printf("[debug] exp %s: npc#%d lvl=%.0f base=%.2f rate=%.2f → %d, charLvl %d→%d (+%d), max=%v",
		c.Name, target.ID, target.Attribute("Level"), base, rate.Value(), calculated,
		startLevel, c.Level, gain.LevelsGained, gain.MaxLevelReached)
	// 可训练宠物（黑暗之马/渡鸦）分享主人击杀经验并升级（对照 PetExperiencePlugIn）。
	s.grantPetExperience(sess, c, calculated)
	return calculated
}

// unlockCharacterClasses 按等级把账号的"可创建职业"表补齐（对照原版
// UnlockCharacterAtLevelBase：升级即写入账号，出站只在下一次角色列表里体现）。
func (s *Server) unlockCharacterClasses(sess *session, level int) {
	unlocked, missing := action.UnlockClassesAtLevel(sess.getAccount(), s.deps.cfg.GameConfig, level)
	for _, n := range missing {
		s.deps.logger.Printf("gameserver: 配置里查不到职业 %d，解锁跳过（原版此处 LogWarning）", n)
	}
	for _, n := range unlocked {
		s.deps.logger.Printf("gameserver: 等级 %d 解锁可创建职业 %d", level, n)
	}
}

// applyLevelUp 对照原版升级分支（PlayerExperience.AddExperienceCoreAsync 的循环体后半）：
// 等级 +1 → 加升级点 → 四项当前值补满 → 下发等级更新 → 升级光效。
//
// 升级点：原版是 `SelectedCharacter.LevelUpPoints += attr[PointsPerLevelUp]` 的累加。
// T2-8 起 LevelUpPoints 是角色态的**剩余值**（加点会扣减，重新解析不回满），
// 因此升级在这里显式累加每级点数，而不是靠重建属性派生。
func (s *Server) applyLevelUp(sess *session, c *entity.Character, wp *world.Player, view action.PlayerView) {
	// 等级到线解锁可创建职业（原版是 ICharacterLevelUpPlugIn 的四个
	// UnlockCharacterAtLevelNN 插件；解锁只影响下次角色列表，无即时出站）。
	s.unlockCharacterClasses(sess, int(c.Level))
	prevPoints := uint16(0)
	if c.Stats != nil {
		prevPoints = c.Stats.LevelUpPoints
	}
	if st, err := s.resolveCharStats(c); err == nil {
		if per := player.AttributeValue(s.deps.cfg.GameConfig, c, desPointsPerLevelUp); per > 0 {
			st.LevelUpPoints = prevPoints + uint16(per)
		}
		c.Stats = st
	} else {
		s.deps.logger.Printf("gameserver: 升级后解析属性失败 %s: %v", c.Name, err)
	}
	if st := c.Stats; st != nil {
		// SetReclaimableAttributesToMaximum：IntervalRegenerationAttributes 的
		// Current ← Maximum（血/法力/BP/盾四项，原版 Stats.IntervalRegenerationAttributes）。
		st.CurrentHealth = st.MaximumHealth
		st.CurrentMana = st.MaximumMana
		st.CurrentAbility = st.MaximumAbility
		st.CurrentShield = st.MaximumShield
	}
	// 世界态持的是同一份属性引用（enterWorld 时注入），重建后必须同步，
	// 否则受击/恢复继续读旧上限。
	if wp != nil {
		wp.Stats = c.Stats
	}
	if view != nil {
		_ = view.ShowLevelUp(levelUpInfo(c))
		// 原版 UpdateLevelExtendedPlugIn：等级包之后紧跟一句蓝字祝贺。
		_ = view.ShowMessage(player.LocalizedMessage(player.MsgLevelUpCongrats, c.Level),
			action.MessageBlueNormal)
	}
	s.showLevelUpEffect(wp, view)
}

// levelUpInfo 把角色投影为 C1 F3 05 的内容（入参顺序对照 UpdateLevelExtendedPlugIn）。
func levelUpInfo(c *entity.Character) action.LevelUpInfo {
	st := c.Stats
	if st == nil {
		return action.LevelUpInfo{Level: c.Level}
	}
	return action.LevelUpInfo{
		Level:               c.Level,
		LevelUpPoints:       st.LevelUpPoints,
		MaximumHealth:       st.MaximumHealth,
		MaximumMana:         st.MaximumMana,
		MaximumShield:       st.MaximumShield,
		MaximumAbility:      st.MaximumAbility,
		FruitPoints:         st.UsedFruitPoints,
		MaximumFruitPoints:  st.MaxFruitPoints,
		NegativeFruitPoints: st.UsedNegFruit,
		// 原版把 GetMaximumFruitPoints() 同时填进这两个字段（上游即如此）。
		MaximumNegativeFruitPoints: st.MaxFruitPoints,
	}
}

// showLevelUpEffect 对照原版
// `ForEachWorldObserverAsync<IShowEffectPlugIn>(p => p.ShowEffectAsync(player, LevelUp), true)`：
// sendToSelf = true → 自己收 PlayerId = 0x200，观察者收真实对象 ID。
func (s *Server) showLevelUpEffect(wp *world.Player, view action.PlayerView) {
	if view != nil {
		_ = view.ShowEffect(constantPlayerID, action.EffectLevelUp)
	}
	if wp == nil {
		return
	}
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.ID == wp.ID || o.View == nil {
			continue
		}
		_ = o.View.ShowEffect(wp.ID, action.EffectLevelUp)
	}
}
