package gameserver

import (
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	remoteMuHelper "mugo/internal/view/remote/muhelper"
)

// handleSelectCharacter 处理 C1 F3 03：校验角色归属后经视图下发 92B 角色信息，
// 客户端随后加载地图并回 F3 12（对照 SelectCharacterAction + UpdateCharacterStatsPlugIn）。
func (s *Server) handleSelectCharacter(sess *session, frame []byte) {
	d := s.deps
	if sess.getState() != entity.StateAuthenticated {
		// OpenMU：非 CharacterSelection 状态直接断开。
		d.logger.Printf("gameserver: 非法选角状态=%d，断开 %s", sess.getState(), sess.conn.RemoteAddr())
		_ = sess.conn.Close()
		return
	}
	account := sess.getAccount()
	if account == nil {
		_ = sess.conn.Close()
		return
	}

	name := c2s.AsSelectCharacter(frame).NameString()
	var chosen *entity.Character
	for i := range account.Characters {
		if account.Characters[i].Name == name {
			chosen = &account.Characters[i]
			break
		}
	}
	if chosen == nil {
		d.logger.Printf("gameserver: 选角不存在 user=%s name=%q，断开", account.Name, name)
		_ = sess.conn.Close()
		return
	}

	sess.setSelected(chosen)
	// 原版 Player.Account 赋值时就把 IsVaultLocked 算好（Player.cs:289）。
	sess.vaultLocked = account.VaultPassword != ""
	sess.setState(entity.StateEnteringMap)
	d.logger.Printf("gameserver: 选角成功 user=%s char=%s map=%d (%d,%d)",
		account.Name, chosen.Name, chosen.MapNumber, chosen.X, chosen.Y)
	st, err := s.resolveCharStats(chosen)
	if err != nil {
		d.logger.Printf("gameserver: 解析角色属性失败 %s: %v", chosen.Name, err)
		return
	}
	if err := s.viewFor(sess).ShowCharacterInformation(action.CharacterInformation{Character: chosen, Stats: st}); err != nil {
		d.logger.Printf("gameserver: 下发角色信息失败: %v", err)
	}
	// 大师职业紧跟 F3 50 + C2 F3 53（对照 UpdateCharacterStatsPlugIn.cs:71-74 的
	// IsMasterClass 门与 UpdateMasterStatsExtendedPlugIn.cs:52 的插件链）。
	s.sendMasterStats(sess, chosen, st)
	// 对照 OpenMU SelectCharacterAsync 尾部（Player.cs）：92B/背包/技能下发后
	// **直接** ClientReadyAfterMapChangeAsync 进世界——初始进图不等客户端 F3 12
	// （客户端加载地图期间缓存后续封包；F3 12 仅在换图/重生后才有意义）。
	s.enterWorld(sess, chosen)
}

// handleClientReady 处理 C1 F3 12：客户端地图加载完成的确认。
// 初始进图已在选角时完成（enterWorld，对照 OpenMU SelectCharacterAsync 尾部直接
// ClientReadyAfterMapChangeAsync）；F3 12 只在换图/重生（EnteringMap）后驱动重入，
// 已在图上时按 OpenMU 守卫忽略（PlayerMapTransitions.cs 的重复包守卫）。
func (s *Server) handleClientReady(sess *session) {
	switch sess.getState() {
	case entity.StateEnteringMap:
		c := sess.getSelected()
		if c == nil {
			_ = sess.conn.Close()
			return
		}
		s.enterWorld(sess, c)
	case entity.StateEnteredWorld:
		s.deps.logger.Printf("gameserver: 忽略重复 F3 12（已在图上）%s", sess.remoteAddr())
	default:
		s.deps.logger.Printf("gameserver: 忽略 F3 12（状态=%d）%s", sess.getState(), sess.remoteAddr())
	}
}

// enterWorld 把已选角玩家放进世界：AoI 进入 + 自己/他人视野互发 + 视野内
// 怪物与地面掉落物下发（对照 OpenMU ClientReadyAfterMapChangeAsync 的执行段）。
func (s *Server) enterWorld(sess *session, c *entity.Character) {
	if sess.getState() != entity.StateEnteringMap {
		return // 幂等守卫：非 EnteringMap 状态重复调用无副作用。
	}

	st, err := s.resolveCharStats(c)
	if err != nil {
		s.deps.logger.Printf("gameserver: 解析角色属性失败 %s: %v", c.Name, err)
		return
	}
	// T2-2：战斗属性（怪物命中判定的防守方防御率/减伤）。
	cv, cvErr := player.ResolveCombatValues(s.deps.cfg.GameConfig, c)
	if cvErr != nil {
		s.deps.logger.Printf("gameserver: 战斗属性解析失败 %s: %v", c.Name, cvErr)
	}
	view := s.viewFor(sess)
	// 对象 ID 进图时分配（原版 GameMap.AddAsync：玩家与怪物/NPC 同池 0x201..0x7FFF）；
	// 离场/换图时经 FreeID 回收。F1 00 的 PlayerId 是哨兵值 0x200，与本 ID 无关。
	wp := &world.Player{
		ID:         s.world.AllocID(),
		MapNumber:  c.MapNumber,
		Name:       c.Name,
		Class:      c.ClassNumber,
		X:          c.X,
		Y:          c.Y,
		Rotation:   c.Rotation,
		Appearance: c.AppearanceExt,
		View:       view,
	}
	if cvErr == nil {
		injectDefenderStats(wp, cv)
		sess.setCombatValues(&cv)
		sess.lastRegen = time.Now()
	}
	// T2-2：怪物攻击的扣血钩子（闭包持有角色实体；受击后回血量包由 AI hook 发）。
	// 进图**始终**用真实属性系统结果替换 c.Stats（ResolveCharStats 已保留持久化
	// 当前值/钱/果实点等；种子旧数据的占位最大值如 maxSD=0 不得覆盖真实后端逻辑）。
	// 原版语义：持久化 Current* 保留，Maximum* 由 AttributeSystem 派生。
	c.Stats = st
	// 当前值越界钳制（原版经属性系统/重生保证 current ≤ max；种子旧值可能超界）。
	clamp := func(cur, max *uint32) {
		if *cur > *max {
			*cur = *max
		}
	}
	clamp(&c.Stats.CurrentHealth, &c.Stats.MaximumHealth)
	clamp(&c.Stats.CurrentMana, &c.Stats.MaximumMana)
	clamp(&c.Stats.CurrentShield, &c.Stats.MaximumShield)
	clamp(&c.Stats.CurrentAbility, &c.Stats.MaximumAbility)
	if c.Stats.CurrentHealth == 0 {
		c.Stats.CurrentHealth = c.Stats.MaximumHealth
	}
	if cvErr == nil {
		s.deps.logger.Printf("[debug] combat-attrs %s: lvl=%d atkRatePvM=%.1f defRatePvM=%.1f defPvM=%.1f phys=[%d..%d] maxHP=%d maxMP=%d maxAG=%d maxSD=%d regenHP=%.4f regenMP=%.4f regenAG=%.4f+%.0f regenSD=%.6f ramp=%.4f",
			c.Name, cv.Level, cv.AttackRatePvM, cv.DefenseRatePvM, cv.DefensePvM,
			cv.PhysDmgMin, cv.PhysDmgMax, c.Stats.MaximumHealth, c.Stats.MaximumMana,
			c.Stats.MaximumAbility, c.Stats.MaximumShield,
			cv.HealthRegenMult, cv.ManaRegenMult, cv.AbilityRegenMult, cv.AbilityRegenAbs,
			cv.ShieldRegenMult, cv.ShieldRampFactor)
	}
	wp.Stats = c.Stats
	wp.IsAlive = true
	// 进图即下发最大/当前四项（原版属性初始化会触发 UpdateStatsExtendedPlugIn →
	// C1 26 FE + C1 26 FF，客户端据此刷血条/蓝条/AG/SD）。
	// 实测坑：不下发则**死亡重生后客户端仍停在死亡时的 0**（服务端已回满却没告诉客户端）。
	if view != nil {
		_ = view.ShowMaximumStatsExtended(maximumStatsOf(c.Stats))
		_ = view.ShowCurrentStatsExtended(currentStatsOf(c.Stats))
	}
	// MU Helper：进图回显程序 blob 并解析为会话设置
	// （原版 Player.SendMuHelperConfigurationAsync + MuHelperSettingsInitializationPlugIn）。
	sess.setMuHelperSettings(remoteMuHelper.TryDeserialize(c.MuHelperConfiguration))
	if view != nil && len(c.MuHelperConfiguration) > 0 {
		_ = view.ShowMuHelperConfiguration(c.MuHelperConfiguration)
	}
	wp.ApplyDamage = func(dmg uint16) (uint32, bool) {
		if !wp.IsAlive {
			return 0, false
		}
		if c.Stats.CurrentHealth > uint32(dmg) {
			c.Stats.CurrentHealth -= uint32(dmg)
		} else {
			c.Stats.CurrentHealth = 0
		}
		if c.Stats.CurrentHealth == 0 {
			wp.IsAlive = false
			return 0, true
		}
		return c.Stats.CurrentHealth, false
	}
	// 受击后扣减宠物耐久（渡鸦/黑王马等，对照 DecreaseDefenseItemDurabilityAsync）。
	wp.OnHit = func(damage int) { s.decreasePetDurability(sess, c, damage) }
	// T2-7 前置：死亡重生（原版 OnDeathAsync：3s 后 SetReclaimableAttributesToMaximum
	// + RespawnAtAsync(出生门) → 新客户端走 MapChanged → 客户端 F3 12 重入）。
	wp.OnDeath = func() {
		go func() {
			time.Sleep(3 * time.Second)
			s.respawnDeadPlayer(sess)
		}()
	}
	conn := sess.conn
	wp.Send = func(b []byte) { _ = conn.Send(b) }

	others := s.world.Map(c.MapNumber).Enter(wp)
	sess.setWorldPlayer(wp)
	sess.setState(entity.StateEnteredWorld)
	s.notifyConnectionsChanged() // 原版：连接数变化上报 stateObserver
	if !sess.publishedEnter {
		sess.publishedEnter = true
		s.publishEnteredGame(c.Name) // P5.1：进场事件 → 战盟服/好友服（换图重入不重复发布）
	}
	// 右手槽装备了可训练攻击宠（黑暗渡鸦）则挂上命令管理器（对照 UpdatePetCommandManagerOnItemMovePlugIn）。
	s.syncPetManager(sess, c, wp)

	self := action.ScopeEntry{
		// 自己的 Id 恒为哨兵值 0x200（原版 movedObject.GetId(playerOfView)：
		// 对象为查看者本人时返回 ConstantPlayerId）——客户端以此识别"自己"，
		// 与 F1 00 的 PlayerId 呼应；真实动态 ID 只存在于其他观察者的视角。
		ID: world.ConstantPlayerID, Spawned: true, X: wp.X, Y: wp.Y, Rotation: wp.Rotation,
		Name: wp.Name, ClassNumber: wp.Class, Appearance: wp.Appearance, Stats: st,
	}
	// 自己：出生位 0x8000（NewPlayersInScopeExtendedPlugIn）。
	_ = view.ShowCharacterInScope(self)
	// 在场其他人看到新玩家（无出生位，走各自视图）；新玩家看到在场者
	// （无出生位，沿用其进图属性缺省——攻速 200，对照原 handler）。
	for _, o := range others {
		if o.View != nil {
			_ = o.View.ShowCharacterInScope(action.ScopeEntry{
				ID: wp.ID, X: wp.X, Y: wp.Y, Rotation: wp.Rotation,
				Name: wp.Name, ClassNumber: wp.Class, Appearance: wp.Appearance,
			})
		} else if o.Send != nil {
			// 无视图的旧世界玩家：保持静默（M6 中不存在此形态）。
		}
		_ = view.ShowCharacterInScope(action.ScopeEntry{
			ID: o.ID, X: o.X, Y: o.Y, Rotation: o.Rotation,
			Name: o.Name, ClassNumber: o.Class, Appearance: o.Appearance,
		})
	}
	// 视野内已开着的摊位要一并补发（对照 NewPlayersInScopePlugIn 攒出的 shopPlayers）。
	if list := s.shopsOf(wp.ID, others); len(list) > 0 {
		_ = view.ShowPlayerShops(list)
	}

	// T2-1：视野内的怪物/NPC 入视野包（AddNpcsToScope，批量）。
	if npcs := s.deps.cfg.NPCs; npcs != nil {
		if entries := s.npcsInScope(npcs.ByMap(c.MapNumber), wp.X, wp.Y); len(entries) > 0 {
			_ = view.ShowNpcsInScope(entries)
		}
	}

	// 背包清单（真机修复 11，对照原版 SelectCharacterAsync 的视图刷新序列：
	// 92B → UpdateInventoryListAsync → UpdateSkillListAsync…；不下发则客户端
	// 装备栏/背包为空）。换图重入走 enterWorld 时同效。
	s.sendInventoryList(sess)

	// 技能列表（原版 SelectCharacterAsync 视图序列的 UpdateSkillListAsync 段：
	// 92B → 背包 → 技能）。不下发则客户端技能栏为空、无法释放任何技能。
	// 对照 OpenMU UpdateSkillListPlugIn（C1 F3 11，SkillEntry 顺序即技能栏顺序）。
	if view != nil {
		_ = view.ShowSkillList(s.skillListViewOf(c))
	}

	// S10：进图先回组 0 的 legacy 状态表，再发进行中清单
	// （对照 Player.cs:1752-1753 的 ShowQuestStateAsync(null) → ShowActiveQuestsAsync()）。
	s.sendLegacyQuestStateList(sess)
	s.pushActiveQuests(sess)

	// T2-1：视野覆盖集内的地面掉落物（C2 20 物品/金币）。
	if drops := s.deps.cfg.Drops; drops != nil {
		var items []action.DropEntry
		for _, d := range drops.ItemsOnMap(c.MapNumber) {
			if !world.InScope(wp.X, wp.Y, d.X, d.Y) {
				continue
			}
			items = append(items, action.DropEntry{
				ID: d.ID, X: d.X, Y: d.Y,
				Data: encodeItemForClient(d.It), FreshDrop: true,
			})
		}
		var money []action.MoneyEntry
		for _, d := range drops.MoneyOnMap(c.MapNumber) {
			if !world.InScope(wp.X, wp.Y, d.X, d.Y) {
				continue
			}
			money = append(money, action.MoneyEntry{ID: d.ID, X: d.X, Y: d.Y, Amount: d.Amount, FreshDrop: true})
		}
		if len(items) > 0 || len(money) > 0 {
			_ = view.ShowDropsInScope(items, money)
		}
	}

	s.deps.logger.Printf("gameserver: 玩家进图 id=%d char=%s map=%d (%d,%d) 可见=%d",
		wp.ID, c.Name, c.MapNumber, c.X, c.Y, len(others))
}

// chebyshevByte 切比雪夫距离（byte 入参防下溢）。
func chebyshevByte(x1, y1, x2, y2 byte) int {
	dx := int(x1) - int(x2)
	dy := int(y1) - int(y2)
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
