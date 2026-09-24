package gameserver

// handler_item_consume.go —— T2-6 使用消耗品（C3 26）+ TRIM-01 消耗策略族补全。
// 对照原版：
//   - PlayerActions/ItemConsumeAction.cs（策略查找 → 耐久 → 销毁/刷新）
//   - ItemConsumeActions/RecoverConsumeHandlerPlugIn.cs（恢复公式 + 三段延迟）
//   - ItemConsumeActions/BaseConsumeHandlerPlugIn.cs（前置条件：EnteredWorld 且耐久 > 0）
//   - ItemConsumeActions/{FruitConsumeHandler,AntidoteConsumeHandler,
//     TownPortalScrollConsumeHandler,UpgradeItemLevelJewel*,ItemUpgradeConsumeHandler,
//     LearnablesConsumeHandler,SummoningOrbConsumeHandler}.cs（TRIM-01 各策略）
//
// 三层分工：配方与判定在 action（consume.go / consume_strategy.go，纯函数）；本层只做
// "取帧字段 → 装配 → 驱动时间轴 → 出站"；协议映射在 view/remote。
//
// 查找顺序逐字对照 ItemConsumeAction：①精确 (Group, Number) 策略（药水配方表 +
// ConsumeStrategyFor 注册表）→ ②不可穿戴且带 Skill → 学习（卷轴/技能石）→
// ③带 ConsumeEffect → 魔法效果（酒等）→ ④未注册 → 失败包。
// 精修石（14,43/44）、Higher Harmony、召唤石、攻城道具等仍走"未实现 → 失败包"
// （依赖尚未建模的领域，差异登记见 doc/16 TRIM-01）。

import (
	"sort"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/pricing"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// handleItemConsume 处理 C3 26 ConsumeItemRequest。
func (s *Server) handleItemConsume(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	// 帧长防御：AsConsumeItemRequest 直接读 d[3..5]，短帧会越界（原版反序列化会抛异常）。
	if len(frame) < int(c2s.ConsumeItemRequestLength) {
		return
	}
	req := c2s.AsConsumeItemRequest(frame)
	slot := req.ItemSlot()

	if c.Stats == nil {
		st, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 使用消耗品前解析角色属性失败 %s: %v", c.Name, err)
			s.replyItemConsumeFailed(sess, c)
			return
		}
		c.Stats = st
	}
	inv := s.ensureInventory(c)
	si := inv.GetItem(slot)
	if si == nil || si.It == nil {
		// 原版：`player.Inventory?.GetItem(slot)` 为 null → ItemConsumptionFailed。
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 原版 BaseConsumeHandlerPlugIn.CheckPreconditions：耐久为 0 不能使用。
	if si.It.Durability == 0 {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 取物品定义（后续分支共用）。
	def, hasDef := s.deps.cfg.GameConfig.Item(int(si.It.Group), int(si.It.Number))

	// ①精确策略：药水配方与 TRIM-01 策略注册表（键互斥，先后无歧义）。
	if recipe, ok := action.PotionRecipeFor(int(si.It.Group), si.It.Number); ok {
		s.handlePotionConsume(sess, c, wp, si, recipe)
		return
	}
	if strategy, ok := action.ConsumeStrategyFor(int(si.It.Group), si.It.Number); ok {
		switch strategy {
		case action.ConsumeStrategyFruit:
			s.handleFruitConsume(sess, c, wp, si, inv, action.FruitUsage(req.FruitConsumption()))
		case action.ConsumeStrategyAntidote:
			s.handleAntidoteConsume(sess, c, si)
		case action.ConsumeStrategyTownPortal:
			s.handleTownPortalConsume(sess, c, wp, si)
		case action.ConsumeStrategyBlessJewel, action.ConsumeStrategySoulJewel:
			s.handleLevelJewelConsume(sess, c, si, inv, req.TargetSlot(), strategy)
		case action.ConsumeStrategyLifeJewel, action.ConsumeStrategyHarmonyJewel,
			action.ConsumeStrategyLowerRefineStone, action.ConsumeStrategyHigherRefineStone:
			s.handleOptionJewelConsume(sess, c, si, inv, req.TargetSlot(), strategy)
		}
		return
	}
	// ②技能书（Scroll, group15）与技能石（Orb, group12）：不可穿戴且带技能 → 学习。
	// 对照原版 `item.Definition.Skill is {} && !item.IsWearable()` → AllScrolls handler。
	if hasDef && def.SkillNumber != nil && !def.IsWearable() {
		s.handleLearnSkillScroll(sess, c, wp, si)
		return
	}
	// ③物品带 ConsumeEffect（酒=201 等）→ 产生持续魔法效果。
	if hasDef && def.ConsumeEffectNumber != nil {
		s.handleConsumeEffectItem(sess, c, wp, si, *def.ConsumeEffectNumber)
		return
	}
	// ④未实现的消耗品：原版先发失败包、再发蓝字 UsingThisItemNotImplemented
	// （ItemConsumeAction.cs:49-50 顺序）。
	s.deps.logger.Printf("gameserver: 消耗品未实现 group=%d number=%d（回失败包）",
		si.It.Group, si.It.Number)
	s.replyItemConsumeFailed(sess, c)
	s.showLocalizedMessage(sess, player.MsgUsingThisItemNotImplemented)
}

// handlePotionConsume 处理瓶类药水：冷却 → 扣耐久 → 时间轴恢复 → 销毁/刷新
// （对照 RecoverConsumeHandlerPlugIn + ItemConsumeAction 的收尾）。
func (s *Server) handlePotionConsume(sess *session, c *entity.Character, wp *world.Player, si *storage.SlottedItem, recipe action.PotionRecipe) {
	// 冷却（原版 RecoverConsumeHandlerPlugIn.CheckPreconditions 的 PotionCooldownUntil）。
	now := time.Now()
	if ready, ok := sess.potionReadyAt(); ok && now.Before(ready) {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	sess.setPotionReadyAt(now.Add(time.Duration(recipe.CooldownMs) * time.Millisecond))

	// 1) 扣耐久（原版 ConsumeSourceItemAsync：仅当耐久 > 0 时 -1）。
	si.It.Durability--

	// 2) 恢复（原版 RecoverAsync：分段时**先返回**，真正的施加在定时器里）。
	s.startPotionRecovery(sess, c, wp, recipe, int(si.It.Level))

	// 3) 耐久归零 → 销毁（原版 DestroyInventoryItemAsync → C1 28）；
	//    否则刷新数量（ItemDurabilityChangedAsync(item, byConsumption: true) = C1 2A）。
	s.updateConsumedItemView(sess, c, si)
}

// consumeSourceItem 对照 BaseConsumeHandlerPlugIn.ConsumeSourceItemAsync +
// ItemConsumeAction 收尾：耐久 -1（>0 时），归零销毁（C1 28），否则 C1 2A 刷新。
func (s *Server) consumeSourceItem(sess *session, c *entity.Character, si *storage.SlottedItem) {
	if si.It.Durability > 0 {
		si.It.Durability--
	}
	s.updateConsumedItemView(sess, c, si)
}

// updateConsumedItemView 按当前耐久出"销毁或刷新"包（原版 ItemConsumeAction 收尾分支）。
func (s *Server) updateConsumedItemView(sess *session, c *entity.Character, si *storage.SlottedItem) {
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	if si.It.Durability == 0 {
		if inv := s.ensureInventory(c); inv.Remove(si) {
			_ = view.ShowItemRemoved(si.Slot)
		}
	} else {
		_ = view.ShowItemDurabilityChanged(si.Slot, si.It.Durability, true)
	}
}

// handleFruitConsume 处理果实（13,15）加点/洗点。
//
// 对照 FruitConsumeHandlerPlugIn：门槛判定全在 action.PlanFruit（纯函数）；被拒绝时
// 原版 return false → 除 C1 2C 应答外**还回使用失败包**（C1 26 FD）。
func (s *Server) handleFruitConsume(sess *session, c *entity.Character, wp *world.Player, si *storage.SlottedItem, inv *storage.Inventory, usage action.FruitUsage) {
	st := c.Stats
	gc := s.deps.cfg.GameConfig
	view := s.viewFor(sess)

	designation := fruitStatDesignation(action.FruitStat(si.It.Level))
	statAllowed := false
	statBaseValue := 0
	// 职业属性合格集（原版 StatAttributes.FirstOrDefault(a => a.IncreasableByPlayer &&
	// a.Attribute == statAttribute)；职业缺失按不合格——原版 null 传播同结果）。
	if cls, ok := gc.Class(int(c.ClassNumber)); ok {
		for _, sa := range cls.StatAttributes {
			if sa.Designation == designation && sa.IncreasableByPlayer {
				statAllowed = true
				statBaseValue = int(sa.BaseValue)
				break
			}
		}
	}
	// 身上有装备则拒绝（原版 `player.Inventory.EquippedItems.Any()`——装备区 0..11）。
	hasEquipped := false
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		if it := inv.GetItem(slot); it != nil && it.It != nil {
			hasEquipped = true
			break
		}
	}
	// 果实上限按等级查表（原版 CharacterExtensions.GetMaximumFruitPoints）。
	fruitCalc := 0
	if cls, ok := gc.Class(int(c.ClassNumber)); ok {
		fruitCalc = cls.FruitCalculation
	}

	stat := action.FruitStat(si.It.Level)
	res := action.PlanFruit(action.FruitInput{
		Usage:         usage,
		PlayerLevel:   c.Level,
		ItemLevel:     si.It.Level,
		StatAllowed:   statAllowed,
		HasEquipped:   hasEquipped,
		FruitPointCap: action.MaxFruitPoints(int(c.Level), fruitCalc),
		UsedAddPoints: int(st.UsedFruitPoints),
		UsedNegPoints: int(st.UsedNegFruit),
		StatValue:     int(fruitStatValue(st, stat)),
		StatBaseValue: statBaseValue,
		Rand:          s.world.RNG(),
	})
	// 被拒绝（门槛不满足）：应答包 + 失败包，果实不消耗。
	if res.Prevented {
		if view != nil {
			_ = view.ShowFruitConsumptionResponse(res.Outcome, 0, res.Stat)
		}
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 成功落账：基础属性 ±points、已用点数累加（原版 player.Attributes[stat] ±= points;
	// UsedFruitPoints/UsedNegFruitPoints += points），属性图重建后下发上限/当前值。
	if res.Applied {
		if usage == action.FruitUsageRemovePoints {
			fruitStatSub(st, res.Stat, int(res.Points))
			st.UsedNegFruit += uint16(res.Points)
		} else {
			fruitStatAdd(st, res.Stat, int(res.Points))
			st.UsedFruitPoints += uint16(res.Points)
		}
		if resolved, err := s.resolveCharStats(c); err == nil {
			c.Stats = resolved
		} else {
			s.deps.logger.Printf("gameserver: 果实后解析角色属性失败 %s: %v", c.Name, err)
		}
		if wp != nil {
			wp.Stats = c.Stats
		}
		if view != nil {
			_ = view.ShowMaximumStatsExtended(maximumStatsOf(c.Stats))
			_ = view.ShowCurrentStatsExtended(currentStatsOf(c.Stats))
		}
	}
	if view != nil {
		// 成功与"掷骰失败"都出应答（失败也消耗果实——原版 ConsumeSourceItemAsync 无条件执行）。
		_ = view.ShowFruitConsumptionResponse(res.Outcome, uint16(res.Points), res.Stat)
	}
	s.consumeSourceItem(sess, c, si)
}

// fruitStatDesignation 把果实目标属性映射到导出件 designation
// （原版 GetStatAttribute：Base Energy/Vitality/Agility/Strength/Leadership）。
func fruitStatDesignation(stat action.FruitStat) string {
	switch stat {
	case action.FruitStatEnergy:
		return "Base Energy"
	case action.FruitStatVitality:
		return "Base Vitality"
	case action.FruitStatAgility:
		return "Base Agility"
	case action.FruitStatStrength:
		return "Base Strength"
	case action.FruitStatLeadership:
		return "Base Leadership"
	}
	return ""
}

// fruitStatValue 读角色基础属性当前值（属性系统"角色态"侧，同 handler_stats 口径）。
func fruitStatValue(st *entity.CharStats, stat action.FruitStat) uint16 {
	switch stat {
	case action.FruitStatEnergy:
		return st.Energy
	case action.FruitStatVitality:
		return st.Vitality
	case action.FruitStatAgility:
		return st.Agility
	case action.FruitStatStrength:
		return st.Strength
	case action.FruitStatLeadership:
		return st.Leadership
	}
	return 0
}

// fruitStatAdd 增加基础属性（uint16 饱和到 65535 之前先钳 int 域）。
func fruitStatAdd(st *entity.CharStats, stat action.FruitStat, points int) {
	v := int(fruitStatValue(st, stat)) + points
	if v > 65535 {
		v = 65535
	}
	fruitStatSet(st, stat, uint16(v))
}

// fruitStatSub 减少基础属性（负值钳 0——原版 float 属性可为负，但下发是 uint16，取保守下界）。
func fruitStatSub(st *entity.CharStats, stat action.FruitStat, points int) {
	v := int(fruitStatValue(st, stat)) - points
	if v < 0 {
		v = 0
	}
	fruitStatSet(st, stat, uint16(v))
}

// fruitStatSet 写回基础属性。
func fruitStatSet(st *entity.CharStats, stat action.FruitStat, v uint16) {
	switch stat {
	case action.FruitStatEnergy:
		st.Energy = v
	case action.FruitStatVitality:
		st.Vitality = v
	case action.FruitStatAgility:
		st.Agility = v
	case action.FruitStatStrength:
		st.Strength = v
	case action.FruitStatLeadership:
		st.Leadership = v
	}
}

// handleAntidoteConsume 处理解毒剂（14,8）。
// 对照 AntidoteConsumeHandlerPlugIn：先常规消耗（耐久 -1 → 28/2A），再移除中毒效果
// （`ActiveEffects[0x37].Dispose()` → 去激活 C1 07）并重算战斗数值。
func (s *Server) handleAntidoteConsume(sess *session, c *entity.Character, si *storage.SlottedItem) {
	s.consumeSourceItem(sess, c, si)
	if old, removed := sess.getEffects().RemoveByNumber(action.PoisonEffectNumber); removed {
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(old.Definition.Number))
		}
		s.refreshCombatValues(sess, c, sess.getResting())
		s.refreshEffectStats(sess, c)
	}
}

// handleTownPortalConsume 处理瞬间传送卷轴（14,10）。
// 对照 TownPortalScrollConsumeHandlerPlugIn：目标图 = 当前图 SafezoneMap ?? 职业 HomeMap，
// 落点 = 目标图 IsSpawnGate 的出场门；warp 走 T1-7 换图链（C3 1C MapChanged）。
// 差异登记：原版先扣耐久再找图，图/门缺失时卷轴**白白损失**；本仓找到有效目标才消耗，
// 否则回失败包（物品不丢，行为对用户更友好，对客户端表现无差异）。
func (s *Server) handleTownPortalConsume(sess *session, c *entity.Character, wp *world.Player, si *storage.SlottedItem) {
	gc := s.deps.cfg.GameConfig
	Fail := func() {
		s.deps.logger.Printf("gameserver: 传送卷轴找不到目标地图/出生门 %s map=%d（回失败包）", c.Name, wp.MapNumber)
		s.replyItemConsumeFailed(sess, c)
	}
	mp, ok := gc.Map(int(wp.MapNumber))
	if !ok {
		Fail()
		return
	}
	targetNum := mp.SafezoneMap
	if targetNum == nil {
		if cls, ok := gc.Class(int(c.ClassNumber)); ok && cls.HomeMap != nil {
			targetNum = cls.HomeMap
		}
	}
	if targetNum == nil {
		Fail()
		return
	}
	tmap, ok := gc.Map(*targetNum)
	if !ok {
		Fail()
		return
	}
	// 原版 GameMapDefinition.SafeZoneSpawnGate：第一个 IsSpawnGate 的出场门。
	var gate *config.ExitGate
	for i := range tmap.ExitGates {
		if tmap.ExitGates[i].IsSpawnGate {
			gate = &tmap.ExitGates[i]
			break
		}
	}
	if gate == nil {
		Fail()
		return
	}
	s.consumeSourceItem(sess, c, si)
	s.warpThroughGate(sess, c, wp, &config.EnterGate{
		Target: &config.GateTarget{
			Map: targetNum,
			X1:  gate.X1, Y1: gate.Y1, X2: gate.X2, Y2: gate.Y2,
			Direction: gate.Direction, IsSpawnGate: true,
		},
	})
}

// handleLevelJewelConsume 处理祝福/灵魂宝石（14,13/14,14）升目标物等级。
// 对照 ItemModifyConsumeHandlerPlugIn.ConsumeItemAsync 的公共前置：
// 目标格无物 → false；目标在装备区（≤ LastEquippable）→ false（**禁止强化已装备物**）；
// ModifyItem false → false——三种都回失败包且宝石不消耗。
func (s *Server) handleLevelJewelConsume(sess *session, c *entity.Character, si *storage.SlottedItem, inv *storage.Inventory, targetSlot byte, strategy action.ConsumeStrategy) {
	if target := inv.GetItem(targetSlot); !jewelTargetUsable(target) || targetSlot < storage.EquippedSlotsCount {
		s.replyItemConsumeFailed(sess, c)
		return
	} else {
		rule, ok := action.LevelRuleForStrategy(strategy)
		if !ok {
			s.replyItemConsumeFailed(sess, c)
			return
		}
		def, ok := s.deps.cfg.GameConfig.Item(int(target.It.Group), target.It.Number)
		if !ok {
			s.replyItemConsumeFailed(sess, c)
			return
		}
		res := action.PlanItemLevelUpgrade(s.world.RNG(), rule, target.It, def)
		if !res.Modified {
			s.replyItemConsumeFailed(sess, c)
			return
		}
		// 落账（原版 ModifyItem 内）：等级写回；成功时耐久刷新为一件满值
		// （GetMaximumDurabilityOfOnePiece——升级不带走已损耗的耐久）。
		target.It.Level = res.NewLevel
		if res.Success {
			target.It.Durability = pricing.MaximumDurability(def, target.It)
		}
		s.consumeSourceItem(sess, c, si)
		s.showItemUpgraded(sess, target)
	}
}

// handleOptionJewelConsume 处理生命/和谐宝石（14,16/14,42）选项增与升。
// 前置同 handleLevelJewelConsume（目标存在、不在装备区）；判定在 action 层，
// Modified=false（含"无候选/不可升档/远古件"）→ 宝石不消耗 + 失败包。
func (s *Server) handleOptionJewelConsume(sess *session, c *entity.Character, si *storage.SlottedItem, inv *storage.Inventory, targetSlot byte, strategy action.ConsumeStrategy) {
	target := inv.GetItem(targetSlot)
	if !jewelTargetUsable(target) || targetSlot < storage.EquippedSlotsCount {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	def, ok := s.deps.cfg.GameConfig.Item(int(target.It.Group), target.It.Number)
	if !ok {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	res := action.PlanOptionJewel(strategy, s.world.RNG(), s.deps.cfg.GameConfig, target.It, def)
	if !res.Modified {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	if res.HarmonyAdd != nil {
		target.It.HarmonyNumber = byte(res.HarmonyAdd.Number)
		target.It.HarmonyLevel = byte(res.HarmonyAdd.Level)
	}
	if res.NewOptionLevel >= 0 {
		if action.StrategyWritesHarmonyLevel(strategy) {
			target.It.HarmonyLevel = byte(res.NewOptionLevel)
		} else {
			target.It.OptionLevel = res.NewOptionLevel
		}
	}
	s.consumeSourceItem(sess, c, si)
	s.showItemUpgraded(sess, target)
}

// jewelTargetUsable 报告目标格是否可作为强化对象（原版 targetItem is null 判据）。
func jewelTargetUsable(target *storage.SlottedItem) bool {
	return target != nil && target.It != nil
}

// showItemUpgraded 下发 C1 F3 14（对照 ItemUpgradedPlugIn.ItemUpgradedAsync：
// 槽位 + 完整扩展编码——宝石成功后目标物的等级/选项靠它刷新到客户端）。
func (s *Server) showItemUpgraded(sess *session, target *storage.SlottedItem) {
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemUpgraded(target.Slot, encodeItemForClient(target.It))
	}
}

// replyItemConsumeFailed 回 C1 26 FD（扩展形态带当前血/盾，客户端据此纠正本地预测）。
func (s *Server) replyItemConsumeFailed(sess *session, c *entity.Character) {
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	var health, shield uint32
	if c != nil && c.Stats != nil {
		health, shield = c.Stats.CurrentHealth, c.Stats.CurrentShield
	}
	_ = view.ShowItemConsumptionFailed(health, shield)
}

// startPotionRecovery 按配方时间轴施加恢复。
//
// 一次性（药水等级 ≥16）在本 goroutine 内同步施加；分段（默认）起一个定时 goroutine，
// 与原版 `_ = RecoverByStepsAsync(...)` 的"不等待、按 Delay 逐段施加"一致。
// 每段施加后出去一包 C1 26 FF（对照属性变更事件 → UpdateStatsExtendedPlugIn，
// 只发自己；原版 16ms 合并窗口已由 action.PlanPotionRecovery 按批聚合等价实现）。
func (s *Server) startPotionRecovery(sess *session, c *entity.Character, wp *world.Player, recipe action.PotionRecipe, itemLevel int) {
	maxima := map[action.RecoverTarget]float64{
		action.RecoverHealth: float64(c.Stats.MaximumHealth),
		action.RecoverMana:   float64(c.Stats.MaximumMana),
		action.RecoverShield: float64(c.Stats.MaximumShield),
	}
	batches := action.PlanPotionRecovery(recipe, maxima, itemLevel, float64(c.Level))
	if len(batches) == 0 {
		return
	}
	if len(batches) == 1 && batches[0].DelayMs <= 0 {
		s.applyRecoverBatch(sess, c, batches[0])
		return
	}
	go func() {
		for _, batch := range batches {
			if batch.DelayMs > 0 {
				time.Sleep(time.Duration(batch.DelayMs) * time.Millisecond)
			}
			// 原版 RecoverByStepsAsync 每步开头：`if (!player.IsAlive || Attributes is null) break;`
			// 再补两条会话级守卫——离场/换角色后继续加血是没有意义的。
			if !wp.IsAlive || sess.getState() != entity.StateEnteredWorld || sess.getSelected() != c {
				return
			}
			s.applyRecoverBatch(sess, c, batch)
		}
	}()
}

// applyRecoverBatch 施加一批恢复并回一包 C1 26 FF。
func (s *Server) applyRecoverBatch(sess *session, c *entity.Character, batch action.RecoverBatch) {
	st := c.Stats
	if st == nil {
		return
	}
	for target, amount := range batch.Amounts {
		switch target {
		case action.RecoverHealth:
			st.CurrentHealth = action.ApplyRecover(float64(st.CurrentHealth), float64(st.MaximumHealth), amount)
		case action.RecoverMana:
			st.CurrentMana = action.ApplyRecover(float64(st.CurrentMana), float64(st.MaximumMana), amount)
		case action.RecoverShield:
			st.CurrentShield = action.ApplyRecover(float64(st.CurrentShield), float64(st.MaximumShield), amount)
		}
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(currentStatsOf(st))
	}
}

// currentStatsOf 把角色属性投影为 C1 26 FF 的内容。
func currentStatsOf(st *entity.CharStats) action.CurrentStats {
	if st == nil {
		return action.CurrentStats{}
	}
	return action.CurrentStats{
		Health:      st.CurrentHealth,
		Shield:      st.CurrentShield,
		Mana:        st.CurrentMana,
		Ability:     st.CurrentAbility,
		AttackSpeed: st.AttackSpeed,
		MagicSpeed:  st.MagicSpeed,
	}
}

// maximumStatsOf 把角色属性投影为 C1 26 FE 的内容。
func maximumStatsOf(st *entity.CharStats) action.MaximumStats {
	if st == nil {
		return action.MaximumStats{}
	}
	return action.MaximumStats{
		Health:  st.MaximumHealth,
		Shield:  st.MaximumShield,
		Mana:    st.MaximumMana,
		Ability: st.MaximumAbility,
	}
}

// handleLearnSkillScroll 处理右键使用技能书（Scroll, group15）与技能石（Orb, group12）。
//
// 对照 OpenMU LearnablesConsumeHandlerPlugIn / SummoningOrbConsumeHandlerPlugIn：
//   - 技能号取物品定义的 Skill（item.Definition.Skill.Number）；**技能石例外**：
//     单一 orb 定义按物品等级学不同技能（targetSkillNumber = base + item.Level）；
//   - 技能不存在 → 失败；
//   - 已学 → 失败（原版 ConsumeItemAsync 前置 `SkillList.ContainsSkill` → 不消耗物品）；
//   - CompliesRequirements（等级/能量/职业，对照 player.CompliesRequirements(item)）→ 不满足失败；
//   - 入 LearnedSkills → 扣耐久/销毁（原版 ConsumeSourceItemAsync）→ 下发 SkillListUpdate。
func (s *Server) handleLearnSkillScroll(sess *session, c *entity.Character, wp *world.Player, si *storage.SlottedItem) {
	gc := s.deps.cfg.GameConfig
	itemDef, _ := gc.Item(int(si.It.Group), int(si.It.Number))
	if itemDef == nil || itemDef.SkillNumber == nil {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	skillNum := *itemDef.SkillNumber
	if si.It.Group == 12 {
		// Summoning Orb：技能号 = 定义技能号 + 物品等级。
		skillNum += int(si.It.Level)
	}
	if _, ok := gc.Skill(skillNum); !ok {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 已学则失败（原版：`player.SkillList.ContainsSkill(skill.Number)` → return false）。
	if learnedSkillContains(c, skillNum) {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 需求校验（原版 CheckPreconditions = base + player.CompliesRequirements(item)）。
	if !compliesItemRequirements(c, itemDef) {
		s.replyItemConsumeFailed(sess, c)
		return
	}
	// 入已学列表（原版 player.SkillList.AddLearnedSkillAsync(skill)）。
	c.LearnedSkills = append(c.LearnedSkills, entity.LearnedSkill{
		SkillNumber: uint16(skillNum),
		Level:       si.It.Level,
	})
	// 扣耐久/销毁（原版 ConsumeSourceItemAsync + ItemConsumeAction 收尾）。
	s.consumeSourceItem(sess, c, si)
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowSkillList(s.skillListViewOf(c))
	}
}

// learnedSkillContains 判断技能号是否已在角色已学列表中。
func learnedSkillContains(c *entity.Character, skillNumber int) bool {
	if c == nil {
		return false
	}
	for _, s := range c.LearnedSkills {
		if int(s.SkillNumber) == skillNumber {
			return true
		}
	}
	return false
}

// compliesItemRequirements 对照原版 player.CompliesRequirements(item)：
// 逐条 requirement 读取角色对应属性并比较；再校验职业合格集。
//
// 需求属性 → 角色实际属性的映射（对照 ItemExtensions.RequirementAttributeMapping）：
// "Total * Requirement Value" → 角色 Total *；"Level" → 角色等级。
func compliesItemRequirements(c *entity.Character, def *config.Item) bool {
	st := c.Stats
	for _, r := range def.Requirements {
		value := requiredCharacterValue(c, st, r.Attribute)
		if value < r.Value {
			return false
		}
	}
	if len(def.QualifiedClasses) > 0 {
		ok := false
		for _, cl := range def.QualifiedClasses {
			if cl == int(c.ClassNumber) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// requiredCharacterValue 按需求属性 designation 读角色当前值。
func requiredCharacterValue(c *entity.Character, st *entity.CharStats, attribute string) int {
	if st == nil {
		return 0
	}
	switch attribute {
	case "Level":
		return int(c.Level)
	case "Total Strength Requirement Value", "Total Strength":
		return int(st.Strength)
	case "Total Agility Requirement Value", "Total Agility":
		return int(st.Agility)
	case "Total Vitality Requirement Value", "Total Vitality":
		return int(st.Vitality)
	case "Total Energy Requirement Value", "Total Energy":
		return int(st.Energy)
	case "Total Leadership Requirement Value", "Total Leadership":
		return int(st.Leadership)
	default:
		// 未知需求属性：保守按 0（需求不满足）处理，与原版"取不到即拒绝"一致。
		return 0
	}
}

// skillListViewOf 把角色**可用**技能投影为视图层的 SkillListView 序列。
// SkillEntry 顺序即客户端技能栏顺序（原版 SkillListUpdate 按列表顺序下发）。
//
// 对照原版 SkillList 构造：可用技能 = 已学技能（LearnedSkills）+ **已装备物品自带技能**
// （`Inventory.EquippedItems.Where(item => item.HasSkill)` → AddItemSkillAsync，
// 且该技能必须对本职业合格）。只发已学列表会让"武器技能"在技能栏里缺失。
func (s *Server) skillListViewOf(c *entity.Character) []action.SkillListView {
	if c == nil {
		return nil
	}
	out := make([]action.SkillListView, 0, len(c.LearnedSkills))
	seen := make(map[uint16]bool, len(c.LearnedSkills))
	for _, sk := range c.LearnedSkills {
		if seen[sk.SkillNumber] {
			continue
		}
		seen[sk.SkillNumber] = true
		out = append(out, action.SkillListView{SkillNumber: sk.SkillNumber, Level: sk.Level})
	}
	// 装备自带技能（原版 AddItemSkillAsync：等级恒 0；已学同名技能时保留已学条目，
	// 因为 AddLearnedSkillAsync 会用已学条目顶替字典里的装备条目）。
	gc := s.deps.cfg.GameConfig
	if gc == nil || c.Inventory == nil {
		return filterSkillListViewForClient(gc, out)
	}
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		si := c.Inventory.GetItem(slot)
		if si == nil || si.It == nil {
			continue
		}
		def, ok := gc.Item(int(si.It.Group), si.It.Number)
		if !ok || def.SkillNumber == nil {
			continue
		}
		num := uint16(*def.SkillNumber)
		if seen[num] {
			continue
		}
		skill, ok := gc.Skill(*def.SkillNumber)
		if !ok || !skillQualifiedFor(skill, c) {
			continue
		}
		seen[num] = true
		out = append(out, action.SkillListView{SkillNumber: num, Level: 0})
	}
	return filterSkillListViewForClient(gc, out)
}

// filterSkillListViewForClient 施加原版 SkillListViewPlugIn.BuildSkillList（:148-167）
// 在组列表时的三条规则，并按技能号升序（原版 OrderBy(Number)，决定客户端的技能槽位号）：
//   - 被"已学大师技能"替换掉的低级技能不下发；
//   - PassiveBoost 的大师技能不下发（它们只出现在大师树里）；
//   - Force Wave(66) 恒不下发；学了 Force Wave Streng(509) 时连 Force(60) 也不发。
func filterSkillListViewForClient(gc *config.GameConfig, views []action.SkillListView) []action.SkillListView {
	if gc == nil {
		return views
	}
	replaced := make(map[uint16]bool)
	has := make(map[uint16]bool, len(views))
	for _, v := range views {
		has[v.SkillNumber] = true
		if m, ok := gc.MasterSkillByNumber(int(v.SkillNumber)); ok && m.ReplacedSkill != nil {
			replaced[uint16(*m.ReplacedSkill)] = true
		}
	}
	out := make([]action.SkillListView, 0, len(views))
	for _, v := range views {
		if m, ok := gc.MasterSkillByNumber(int(v.SkillNumber)); ok && m.PassiveBoost {
			continue
		}
		if replaced[v.SkillNumber] {
			continue
		}
		if v.SkillNumber == skillForceWave {
			continue
		}
		if v.SkillNumber == skillForce && has[skillForceWaveStreng] {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SkillNumber < out[j].SkillNumber })
	return out
}

// 原版 SkillListViewPlugIn 的三个硬编码技能号（Force 系替换关系只到 509 这一层）。
const (
	skillForce           = 60
	skillForceWave       = 66
	skillForceWaveStreng = 509
)

// skillQualifiedFor 判定技能是否对角色职业合格
// （原版 `skill.QualifiedCharacters.Contains(character.CharacterClass)`）。
func skillQualifiedFor(def *config.Skill, c *entity.Character) bool {
	if def == nil || c == nil {
		return false
	}
	for _, n := range def.QualifiedClasses {
		if n == int(c.ClassNumber) {
			return true
		}
	}
	return false
}
