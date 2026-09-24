package gameserver

// handler_item.go —— T2-4 拾取（C3 22）/ 丢弃（C3 23）与 T2-5 背包搬运（C3 24），
// 对照原版 PickupItemAction / DropItemAction / MoveItemAction 与对应 view 插件：
//   - 拾取：距离 ≤3（原版 CanPickup）→ 金币入账 / 物品入背包（AddToFree）；
//     成功后拾取者收 C3 22 物品或金币更新，观察者收 C2 21 移除；
//     失败回 C1 23 失败响应（原版 ItemPickUpFailed 语义）。
//   - 丢弃：目标格可走（原版 DropItemAction 的唯一地形校验）→ 移出背包 → 地面物 +
//     观察者（含自己）收 C2 20，自己收 C1 23 丢弃结果（成功后客户端移除背包条目）。
//   - 搬运：决策在 action 包（DecideMove/Apply*，三道门 + 堆叠），本层只做
//     "取帧字段 → 装配环境 → 调动作 → 出站"，以及装备槽变化时的外观广播。

import (
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/view/remote"
)

// storageKindInventory 是客户端 STORAGE_TYPE::INVENTORY（= 0，与原版 ItemStorageKind 一致）。
const storageKindInventory byte = byte(c2s.ItemStorageKind_Inventory)

// handlePickupItem 处理 C3 22：拾取地面物。
func (s *Server) handlePickupItem(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil || s.deps.cfg.Drops == nil {
		return
	}
	req := c2s.AsPickupItemRequest(frame)
	dropID := req.ItemId() & 0x7FFF

	// 金币：入账 + 金币更新 + 移除广播（原版 TryPickupMoneyAsync）。
	if dm, ok := s.deps.cfg.Drops.PeekMoney(dropID); ok {
		if chebyshevByte(wp.X, wp.Y, dm.X, dm.Y) > 3 {
			s.pickUpFailed(sess, action.ItemPickFailGeneral)
			return
		}
		if _, ok := s.deps.cfg.Drops.TakeMoney(dropID); !ok {
			s.pickUpFailed(sess, action.ItemPickFailGeneral)
			return
		}
		if c.Stats == nil {
			c.Stats = &entity.CharStats{}
		}
		c.Stats.Money += dm.Amount
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
		}
		s.broadcastDropRemoved(wp.MapNumber, dm.X, dm.Y, dropID)
		return
	}

	// 物品：距离校验 → 入背包（AddToFree = 原版 CheckInvSpace + Add）→ 移除广播。
	if di, ok := s.deps.cfg.Drops.PeekItem(dropID); ok {
		if chebyshevByte(wp.X, wp.Y, di.X, di.Y) > 3 {
			s.pickUpFailed(sess, action.ItemPickFailGeneral)
			return
		}
		// 扩展页数取自角色态（对照原版 PlayerStorages 每登录按
		// character.InventoryExtensions 重建 InventoryStorage）。
		inv := s.ensureInventory(c)
		si := s.newSlottedItem(di.It)
		if !inv.AddToFree(si) {
			// 背包满：地面物保留，回 C3 22 General（原版 PickupItemAction:53）。
			s.pickUpFailed(sess, action.ItemPickFailGeneral)
			return
		}
		if _, ok := s.deps.cfg.Drops.TakeItem(dropID); !ok {
			// 竞态兜底：地面物已被他人拾取——回滚背包并报失败（原版同口径）。
			_ = inv.Remove(si)
			s.pickUpFailed(sess, action.ItemPickFailGeneral)
			return
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemAddedToInventory(si.Slot, encodeItemForClient(di.It))
		}
		s.broadcastDropRemoved(wp.MapNumber, di.X, di.Y, dropID)
		return
	}
	// 原版 switch 的 default 分支：既不是掉落物也不是金币（ID 不存在/已被拿走）→ General。
	s.pickUpFailed(sess, action.ItemPickFailGeneral)
}

// pickUpFailed 下发 C3 22 拾取失败原因（对照 IItemPickUpFailedPlugIn）。
func (s *Server) pickUpFailed(sess *session, reason action.ItemPickFailReason) {
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemPickUpFailed(reason)
	}
}

// handleDropItem 处理 C3 23：从背包丢弃物品到地面。
func (s *Server) handleDropItem(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	req := c2s.AsDropItemRequest(frame)
	slot := req.ItemSlot()
	tx, ty := req.TargetX(), req.TargetY()

	// 目标格可走（原版 DropItemAction 的唯一地形校验）。
	fail := func() {
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemDropResponse(false, slot)
		}
	}
	if !s.world.Map(wp.MapNumber).Walkable(tx, ty) {
		fail()
		return
	}
	// 背包：MVP 只有主网格无装备位（装备系统接入后 slot 语义对齐原版）。
	if c.Inventory == nil {
		fail()
		return
	}
	si := c.Inventory.Grid().GetItem(slot)
	if si == nil {
		fail()
		return
	}
	if !c.Inventory.Remove(si) {
		fail()
		return
	}

	// 丢弃结果回自己（成功后客户端移除背包 UI 条目）。
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemDropResponse(true, slot)
	}
	// 地面物 + 观察者（含自己）收 C2 20。
	if s.deps.cfg.Drops != nil {
		id := s.deps.cfg.Drops.AddItem(wp.MapNumber, tx, ty, si.It, time.Now())
		entry := action.DropEntry{ID: id, X: tx, Y: ty, Data: encodeItemForClient(si.It), FreshDrop: true}
		for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(tx, ty) {
			if o.View != nil {
				_ = o.View.ShowDropsInScope([]action.DropEntry{entry}, nil)
			}
		}
	}
}

// broadcastDropRemoved 对地面物位置覆盖集内的玩家广播 C2 21 移除。
func (s *Server) broadcastDropRemoved(mapNumber uint16, x, y byte, dropID uint16) {
	for _, o := range s.world.Map(mapNumber).PlayersInRangeFor(x, y) {
		if o.View != nil {
			_ = o.View.ShowItemDropRemoved([]uint16{dropID})
		}
	}
}

// newSlottedItem 用物品定义的占位尺寸构造背包条目（缺省 1×1）。
func (s *Server) newSlottedItem(it *item.Item) *storage.SlottedItem {
	w, h := byte(1), byte(1)
	if def, ok := s.deps.cfg.GameConfig.Item(int(it.Group), int(it.Number)); ok {
		w, h = byte(def.Width), byte(def.Height)
	}
	return &storage.SlottedItem{It: it, Width: w, Height: h}
}

// handleItemMove 处理 C3 24 ItemMoveRequestExtended：背包内搬运 + 装备穿脱（T2-5）。
//
// 对照原版 MoveItemAction.MoveItemAsync 的骨架：
//
//	IsMoveAllowed → 取源物品 → CanMoveAsync（三道门）→ 按 Movement 分派
//	→ 失败/成功各自出站 → 装备槽变化触发外观广播。
func (s *Server) handleItemMove(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	// 帧长防御：扩展形态固定 7B（AsItemMoveRequestExtended 直接读 d[3..6]，短帧会越界）。
	if len(frame) < int(c2s.ItemMoveRequestExtendedLength) {
		s.replyItemMoveFailed(sess, nil)
		return
	}
	req := c2s.AsItemMoveRequestExtended(frame)
	fromStorage, toStorage := byte(req.FromStorage()), byte(req.ToStorage())
	fromSlot, toSlot := req.FromSlot(), req.ToSlot()

	// 背包↔仓库搬运：独立路径（跨容器不堆叠、装备区三道门），不改动背包内搬运逻辑。
	storageKindVault := byte(c2s.ItemStorageKind_Vault)
	if fromStorage == storageKindVault || toStorage == storageKindVault {
		s.handleItemMoveVault(sess, fromStorage, toStorage, fromSlot, toSlot)
		return
	}

	// 背包↔交易临时容器（S3）：由交易处理器接管，成功回 ItemMoved、并通知对方。
	if storageKind := byte(c2s.ItemStorageKind_Trade); fromStorage == storageKind || toStorage == storageKind {
		if !s.handleItemMoveTrade(sess, fromStorage, toStorage, fromSlot, toSlot) {
			s.replyItemMoveFailed(sess, nil)
		}
		return
	}

	// 背包混沌锅临时容器（S8）：由合成处理器接管。
	if ck := byte(c2s.ItemStorageKind_ChaosMachine); fromStorage == ck || toStorage == ck {
		if !s.handleItemMoveCraft(sess, fromStorage, toStorage, fromSlot, toSlot) {
			s.replyItemMoveFailed(sess, nil)
		}
		return
	}

	// 原版 IsStorageContextAllowed 的 Go 子集：T2-5 只接受**背包内部**搬运
	// （Inventory ↔ Inventory，状态在上面已限定 EnteredWorld）。仓库/交易/混沌盒
	// 随后续任务接入；跨容器判据（原版 CanMoveAsync 的 IsBoundToCharacter 分支）
	// 在引入仓库前恒不触发，故此处不预先添加。
	if fromStorage != storageKindInventory || toStorage != storageKindInventory {
		s.replyItemMoveFailed(sess, nil)
		return
	}

	if c.Stats == nil {
		st, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 搬运前解析角色属性失败 %s: %v", c.Name, err)
			s.replyItemMoveFailed(sess, nil)
			return
		}
		c.Stats = st
	}
	inv := s.ensureInventory(c)
	si := inv.GetItem(fromSlot)
	if si == nil {
		// 原版：fromItemStorage.GetItem(fromSlot) 为 null → ItemMoveFailed(null)。
		s.replyItemMoveFailed(sess, nil)
		return
	}

	env := s.moveItemEnv(c)
	def := env.DefOf(si.It)
	kind := action.DecideMove(inv, fromSlot, toSlot, si.It, def, env)
	srcData := encodeItemForClient(si.It)
	srcSlot := fromSlot

	switch kind {
	case action.MoveNormal:
		if !action.ApplyNormalMove(inv, fromSlot, toSlot, si) {
			// 落位失败（原版 MoveNormalAsync 的回滚分支）：容器已尽力还原，报失败。
			s.replyItemMoveFailed(sess, si.It)
			return
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemMoved(storageKindInventory, toSlot, srcData)
		}

	case action.MoveCompleteStack:
		target := inv.GetItem(toSlot)
		if target == nil || target == si {
			s.replyItemMoveFailed(sess, si.It)
			return
		}
		dstSlot := toSlot
		action.ApplyFullStack(inv, si, target)
		// 原版 FullStackAsync：先回失败包（客户端把手上那件放回**源槽**），
		// 再让客户端删掉源槽条目，最后刷新目标槽数量。
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemMoveFailed(srcData)
			_ = view.ShowItemRemoved(srcSlot)
			_ = view.ShowItemDurabilityChanged(dstSlot, target.It.Durability, false)
		}

	case action.MovePartiallyStack:
		target := inv.GetItem(toSlot)
		if target == nil || target == si {
			s.replyItemMoveFailed(sess, si.It)
			return
		}
		dstSlot := toSlot
		action.ApplyPartialStack(inv, si, target, def)
		// 原版 PartiallyStackAsync：失败包（归位）+ 两端各刷新一次数量。
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemMoveFailed(encodeItemForClient(si.It))
			_ = view.ShowItemDurabilityChanged(srcSlot, si.It.Durability, false)
			_ = view.ShowItemDurabilityChanged(dstSlot, target.It.Durability, false)
		}

	default:
		// Movement.None：原版回 ItemMoveFailed(item) 让客户端恢复原位。
		s.replyItemMoveFailed(sess, si.It)
		return
	}

	// 装备槽变化 → 外观广播。顺序与 MoveNormalAsync 的 RemoveItemAsync → AddItemAsync
	// 一致：先"卸下"后"穿上"（原版两个事件分别由 Remove/Add 钩子触发）。
	if inv.WearsSlot(fromSlot) {
		s.deps.logger.Printf("[equip] 卸下 slot=%d item=(%d,%d)", fromSlot, si.It.Group, si.It.Number)
		s.broadcastAppearanceChange(wp, c, fromSlot, si.It, false)
	}
	if inv.WearsSlot(toSlot) {
		s.deps.logger.Printf("[equip] 穿上 slot=%d item=(%d,%d)", toSlot, si.It.Group, si.It.Number)
		s.broadcastAppearanceChange(wp, c, toSlot, si.It, true)
	}
	// 装备槽变化 → 重算战斗属性：攻击伤害/命中/防御率随装备即时更新
	// （对照原版 UpdateItemsOnChangeAsync 触发属性重算；否则缓存的 combat
	// values 仍是穿装备前的值，要等移动才刷新）。堆叠不涉及装备槽，自然跳过。
	if inv.WearsSlot(fromSlot) || inv.WearsSlot(toSlot) {
		s.refreshCombatValues(sess, c, false)
		// 上下可训练攻击宠（黑暗渡鸦）随穿脱挂/摘命令管理器（对照 UpdatePetCommandManagerOnItemMovePlugIn）。
		s.syncPetManager(sess, c, wp)
		// 装备（含物品选项）加成同步：重算角色属性并下发 C1 26 FE/FF，
		// 属性面板的血/蓝/盾/AG/攻速随穿脱即时变化（否则客户端一直显示穿前的值）。
		s.refreshEquipmentStats(sess, c)
		// 装备自带技能随穿脱进出技能栏（原版 Inventory.EquippedItemsChanged →
		// AddItemSkillAsync / RemoveItemSkillAsync → 技能列表视图刷新）。
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowSkillList(s.skillListViewOf(c))
		}
	}
}

// refreshEquipmentStats 在穿脱装备后用含活动 buff 的属性系统重算角色属性，
// 并下发 C1 26 FE/FF（对照原版 ItemAwareAttributeSystem 中装备元素增减 →
// AttributeValueChanged → UpdateStatsExtendedPlugIn：上限变化发 FE、当前值与
// 攻速变化发 FF）。物品选项加成（幸运/普通/卓越/和谐/远古/套装/组合奖励）
// 全部经属性系统重算——属性面板与后续装备需求判定（moveAttributeValue 读
// c.Stats）都即时读到新值。当前血/蓝仍按 ResolveCharStatsWithEffects 的语义
// 保留持久化值（脱下降上限时当前值保留，与原版一致）。
func (s *Server) refreshEquipmentStats(sess *session, c *entity.Character) {
	if c == nil || c.Stats == nil {
		return
	}
	var effects []action.MagicEffect
	if el := sess.getEffects(); el.Len() > 0 {
		effects = el.Snapshot()
	}
	st, err := player.ResolveCharStatsWithEffects(s.deps.cfg.GameConfig, c, effects)
	if err != nil {
		s.deps.logger.Printf("gameserver: 穿脱装备重算属性失败 %s: %v", c.Name, err)
		return
	}
	// 脱下加属性的装备使上限下降时，把当前值钳到新上限（对照原版
	// OnAttributeValueChanged 的 LimitCurrentAttribute），保证 FE 与 FF 自洽。
	clampStat := func(cur, max uint32) uint32 {
		if cur > max {
			return max
		}
		return cur
	}
	st.CurrentHealth = clampStat(st.CurrentHealth, st.MaximumHealth)
	st.CurrentMana = clampStat(st.CurrentMana, st.MaximumMana)
	st.CurrentShield = clampStat(st.CurrentShield, st.MaximumShield)
	st.CurrentAbility = clampStat(st.CurrentAbility, st.MaximumAbility)
	c.Stats = st
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowMaximumStatsExtended(maximumStatsOf(st))
		_ = view.ShowCurrentStatsExtended(currentStatsOf(st))
	}
}

// replyItemMoveFailed 回 C3 24 FF（it 为 nil 时视图层按原版 NeededSpace 补零）。
func (s *Server) replyItemMoveFailed(sess *session, it *item.Item) {
	var data []byte
	if it != nil {
		data = encodeItemForClient(it)
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemMoveFailed(data)
	}
}

// ensureInventory 惰性创建背包（扩展页数取角色属性；与拾取路径同一语义）。
func (s *Server) ensureInventory(c *entity.Character) *storage.Inventory {
	if c.Inventory == nil {
		ext := 0
		if c.Stats != nil {
			ext = int(c.Stats.InventoryExtensions)
		}
		c.Inventory = storage.NewInventory(ext)
	}
	return c.Inventory
}

// moveItemEnv 装配搬运决策环境（T2-5）。
//
// DefOf 把导出件里的物品定义投影成装备判定所需的字段（含 T2-5 新导出的
// slots / requirements / qualified_classes / is_ammunition / has_skill）；
// AttributeOf 读角色当前的 Total* 值（entity.CharStats 即属性系统的产出，
// 见 player.ResolveCharStats），ClassNumber 取角色职业编号。
func (s *Server) moveItemEnv(c *entity.Character) *action.MoveItemEnv {
	gc := s.deps.cfg.GameConfig
	return &action.MoveItemEnv{
		ClassNumber: int(c.ClassNumber),
		DefOf: func(it *item.Item) *action.MoveItemDef {
			if gc == nil || it == nil {
				return nil
			}
			def, ok := gc.Item(int(it.Group), int(it.Number))
			if !ok {
				return nil
			}
			reqs := make([]action.MoveRequirement, 0, len(def.Requirements))
			for _, r := range def.Requirements {
				reqs = append(reqs, action.MoveRequirement{Attribute: r.Attribute, Value: r.Value})
			}
			return &action.MoveItemDef{
				Group:              def.Group,
				Width:              def.Width,
				Height:             def.Height,
				DropLevel:          def.DropLevel,
				Durability:         def.Durability,
				Slots:              def.Slots,
				IsAmmunition:       def.IsAmmunition,
				IsWearable:         def.IsWearable(),
				HasSkill:           def.HasSkill,
				IsBoundToCharacter: def.IsBoundToCharacter,
				Requirements:       reqs,
				QualifiedClasses:   def.QualifiedClasses,
			}
		},
		AttributeOf: func(designation string) float32 {
			return moveAttributeValue(c, designation)
		},
	}
}

// moveAttributeValue 按 designation 取角色当前属性值（装备需求比对用）。
//
// 覆盖导出件里实际出现的全部需求属性：五维 "Total *"（映射后的比对属性）、
// "Level"，以及少量物品**原样直出**的 "Total Strength/Agility/Energy"。
// 未知 designation（如某件物品的 "Is '...' Quest completed?"）返回 0 →
// 需求不满足 → 拒绝穿戴（保守方向，与原版"未满足即拒绝"一致）。
func moveAttributeValue(c *entity.Character, designation string) float32 {
	if c == nil {
		return 0
	}
	switch designation {
	case "Level":
		return float32(c.Level)
	}
	if c.Stats == nil {
		return 0
	}
	switch designation {
	case "Total Strength":
		return float32(c.Stats.Strength)
	case "Total Agility":
		return float32(c.Stats.Agility)
	case "Total Vitality":
		return float32(c.Stats.Vitality)
	case "Total Energy":
		return float32(c.Stats.Energy)
	case "Total Leadership":
		return float32(c.Stats.Leadership)
	}
	return 0
}

// broadcastAppearanceChange 向世界观察者广播一次装备外观变化（C1 25；T2-5）。
//
// 对照原版 InventoryStorage.UpdateItemsOnChangeAsync → AppearanceChangedExtendedPlugIn：
//   - 只发给**其他**观察者（sendToSelf = false：自己的外观客户端自己知道）；
//   - isEquipped 为 false（卸下）时 ItemGroup 下 0xFF，客户端据此清空该槽模型；
//   - ExcellentFlags = GetExcellentByte | GetFenrirByte（本模型即 ExcellentBits|FenrirBits）；
//   - IsAncientSetComplete 用"整套远古"近似（原版 HasFullAncientSetEquipped 需要
//     ItemSetGroups 的套装成员表，导出件暂未带——差异登记在 doc/15 T2-5）。
//
// 另外同时对操作者自己的 27B 进图外观打补丁：否则"走出视野再回来"的观察者会
// 从 ScopeEntry.Appearance 拿到**旧的**装备外观（OpenMU 每次由 EquippedItems 现算，
// 我们这里是增量维护，必须自己保持同步）。
func (s *Server) broadcastAppearanceChange(wp *world.Player, c *entity.Character, slot byte, it *item.Item, isEquipped bool) {
	if wp == nil || it == nil || int(slot) > item.SlotPet {
		return
	}
	group := byte(0xFF)
	if isEquipped {
		group = it.Group
	}
	change := action.AppearanceChange{
		ChangedPlayerID:      wp.ID,
		ItemSlot:             slot,
		ItemGroup:            group,
		ItemNumber:           uint16(it.Number),
		ItemLevel:            it.Level,
		ExcellentFlags:       it.ExcellentBits | it.FenrirBits,
		AncientDiscriminator: it.AncientDiscriminator,
		IsAncientSetComplete: isAncientSetComplete(c),
	}
	if slot == item.SlotPet {
		s.deps.logger.Printf("[equip] C1 25 slot=8 广播 equipped=%v group=0x%02X number=%d excFlags=0x%02X", isEquipped, group, it.Number, change.ExcellentFlags)
	}
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.ID == wp.ID || o.View == nil {
			continue
		}
		_ = o.View.ShowAppearanceChanged(change)
	}
	if len(wp.Appearance) >= remote.AppearanceExtSize {
		remote.PatchAppearanceSlot(wp.Appearance, int(slot), appearanceEquip(it, isEquipped))
	}
}

// appearanceEquip 把领域物品投影成外观编码输入；isEquipped 为 false → nil（空槽编码）。
func appearanceEquip(it *item.Item, isEquipped bool) *item.Equip {
	if !isEquipped || it == nil {
		return nil
	}
	return &item.Equip{
		Number:      it.Number,
		Group:       it.Group,
		Level:       it.Level,
		Excellent:   it.ExcellentBits != 0,
		Ancient:     it.AncientDiscriminator != 0,
		BlackFenrir: it.FenrirBits == 1,
		BlueFenrir:  it.FenrirBits == 2,
		GoldFenrir:  it.FenrirBits == 4,
	}
}

// isAncientSetComplete 是原版 Character.HasFullAncientSetEquipped 的**近似**：
// 五件防具槽（头/铠/裤/手/鞋）全部穿着远古装备、且判别号一致 → 视为整套远古
// （客户端据此点亮紫色光环 c->ExtendState）。
//
// 精确实现要判"某个远古套装的**全部**成员是否都穿在身上"，需要 ItemSetGroups 的
// 成员表（导出件目前只有每件物品自己的 AncientSetDiscriminator）——差异已登记。
func isAncientSetComplete(c *entity.Character) bool {
	if c == nil || c.Inventory == nil {
		return false
	}
	disc := byte(0)
	for slot := item.SlotHelm; slot <= item.SlotBoots; slot++ {
		si := c.Inventory.GetItem(byte(slot))
		if si == nil || si.It == nil || si.It.AncientDiscriminator == 0 {
			return false
		}
		if disc == 0 {
			disc = si.It.AncientDiscriminator
		} else if disc != si.It.AncientDiscriminator {
			return false
		}
	}
	return true
}
