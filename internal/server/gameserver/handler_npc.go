package gameserver

// handler_npc.go —— T2-9 NPC 对话（C3 30）/关闭（C1 31）/买（C3 32）/卖（C3 33），
// 对照原版：
//   - GameServer/MessageHandler/TalkNpcHandlerPlugIn（0x30）→
//     GameLogic/PlayerActions/TalkNpcAction.TalkToNpcAsync：商店 NPC →
//     IOpenNpcWindowPlugIn（C3 30）+ IShowMerchantStoreItemListPlugIn（C2 31）；
//   - MessageHandler/CloseNpcDialogHandlerPlugIn（0x31）→ CloseNpcDialogAction
//     （清 OpenedNpc；本仓无仓库/混沌哥布林钩子）；
//   - MessageHandler/Items/BuyNpcItemHandlerPlugIn（0x32）→ BuyNpcItemAction
//     （堆叠/背包/金币三道门，判定在 action.BuyDecision；成功 C1 32 + C3 22 FE，
//     堆叠成功后原版还会回一帧 C1 32 FF 让客户端结束手势——照抄）；
//   - MessageHandler/Items/SellItemToNpcHandlerPlugIn（0x33）→ SellItemToNpcAction
//     （绑定拒绝/金币上限，判定在 action.SellDecision；C3 33 结果帧）。
//
// 裁剪登记（doc/15 T2-9）：
//   - 原版商店打开前 Task.Delay(500)（等客户端对话动画）未复刻；
//   - 攻城税率（CastleSiegeTaxProvider）按 0 处理（本仓无攻城）；
//   - 买入拒绝按原版发蓝字（ItemUnknown/InventoryFull/NotEnoughMoney）后再回 C1 32 FF。

import (
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/view/remote"
)

// findNpcByID 在地图实例列表里按对象 ID 找 NPC（与 handleHit 同一查询语义）。
func (s *Server) findNpcByID(mapNumber uint16, id uint16) *npc.Npc {
	if s.deps.cfg.NPCs == nil {
		return nil
	}
	for _, n := range s.deps.cfg.NPCs.ByMap(mapNumber) {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// handleNpcTalk 处理 C3 30：与 NPC 对话。分支次序严格对照 TalkNpcAction.TalkToNpcAsync：
// 商店分支（先 500ms 再开窗 + 商品清单，然后**直接 return**）→ 按 NpcWindow 的 switch →
// 最后（非商店路径）"该 NPC 挂有合成配方"才建临时容器（原版 BackupInventory，:141-144）。
func (s *Server) handleNpcTalk(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	if wp == nil {
		return
	}
	if len(frame) < c2s.TalkToNpcRequestLength {
		return
	}
	req := c2s.AsTalkToNpcRequest(frame)
	target := s.findNpcByID(wp.MapNumber, req.NpcId())
	if target == nil {
		return // 原版：地图里取不到该对象就什么也不做（无距离校验、无提示）
	}
	sess.setOpenedNpc(target)
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	// 可接任务清单**不**在此下发：原版由 C1 F6 30 请求驱动（ShowAvailableQuestsPlugIn
	// 只读 Player.OpenedNpc），故 setOpenedNpc 之后由 handleQuestPacket 应答。

	gc := s.deps.cfg.GameConfig
	window := 0
	if target.Def != nil {
		window = int(target.Def.NpcWindow)
	}
	if gc != nil {
		if store, ok := gc.MerchantStore(int(target.Number)); ok && len(store.Items) > 0 {
			s.sleepNpcDialogDelay()
			// 原版：NpcWindow 未定义 → 按 Merchant 开窗（`NpcWindow != Undefined ? … : Merchant`）。
			_ = view.ShowNpcWindow(merchantWindowOf(window))
			_ = view.ShowMerchantStoreItemList(action.StoreKindNormal, s.storeItemViews(store.Items))
			return
		}
	}
	s.showNpcWindowFor(sess, target, window)
	if gc != nil && gc.HostsCraftings(int(target.Number)) {
		s.openCraft(sess) // 原版 player.BackupInventory = new BackupItemStorage(...)
	}
}

// showNpcWindowFor 是 TalkNpcAction 的 NpcWindow switch：三类"有专门应答"的窗口各走自己的
// 发包序列，其余交给视图层的映射表；映射不到（原版 throw）只记日志。
func (s *Server) showNpcWindowFor(sess *session, target *npc.Npc, window int) {
	view := s.viewFor(sess)
	switch window {
	case remote.NpcWindowUndefined:
		// 原版：IPlayerTalkToNpcPlugIn 无人接管 → 蓝字 TalkingNotImplementedFormat 并退回
		// EnteredWorld（= 清掉对话上下文）。攻城/失落地图/事件类 NPC 的接管插件属各自域。
		s.showLocalizedMessage(sess, player.MsgTalkingNotImplementedFormat, target.Number, target.Name)
		sess.setOpenedNpc(nil)
	case remote.NpcWindowVaultStorage:
		s.openVault(sess) // 开窗 + 仓库清单 + 金钱 + 锁状态（ShowVaultPlugIn 的四连发）
	case remote.NpcWindowNpcDialog:
		_ = view.ShowNpcDialog(uint16(target.Number))
	case remote.NpcWindowLegacyQuest:
		s.showLegacyQuestDialog(sess, target)
	case remote.NpcWindowGuildMaster:
		s.showGuildMasterDialog(sess, target)
	default:
		// ChaosMachine / RemoveJohOption 与其余可映射窗口一样：**只开窗**，
		// 合成清单只在一次合成之后才发（ItemCraftAction.cs:55）。
		if err := view.ShowNpcWindow(window); err != nil {
			s.deps.logger.Printf("gameserver: NPC %d(%s) 的窗口 %d 无协议对应值: %v",
				target.Number, target.Name, window, err)
		}
	}
}

// merchantWindowOf 把 NPC 的窗口换成"该发哪个窗包"：商店分支里原版用
// `NpcWindow != Undefined ? NpcWindow : Merchant` —— 有专属窗口的商店（如炼金术师）
// 仍发自己的窗口，只有 Undefined 才退化成 Merchant。
func merchantWindowOf(window int) int {
	if window != remote.NpcWindowUndefined {
		return window
	}
	return remote.NpcWindowMerchant
}

// sleepNpcDialogDelay 复刻开窗前的 Task.Delay(500)（测试把 npcDialogDelay 置 0 跳过）。
func (s *Server) sleepNpcDialogDelay() {
	if s.npcDialogDelay > 0 {
		time.Sleep(s.npcDialogDelay)
	}
}

// storeItemViews 把导出件商品投影为视图条目（扩展物品编码由本层序列化）。
func (s *Server) storeItemViews(items []config.MerchantStoreItem) []action.MerchantItemView {
	out := make([]action.MerchantItemView, 0, len(items))
	for _, si := range items {
		it := &item.Item{
			Group:       si.Group,
			Number:      int(si.Number),
			Level:       si.Level,
			Durability:  si.Durability,
			HasSkill:    si.HasSkill,
			Luck:        si.Luck,
			OptionLevel: int(si.OptionLevel),
		}
		out = append(out, action.MerchantItemView{Slot: si.Slot, Data: encodeItemForClient(it)})
	}
	return out
}

// handleNpcClose 处理 C1 31：关闭 NPC 对话（对照 CloseNpcDialogAction：
// 仅在有打开的 NPC 时清上下文）。
func (s *Server) handleNpcClose(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	if n := sess.getOpenedNpc(); n != nil {
		s.closeCraftIfNeeded(sess) // S8：关混沌锅退回临时容器物品
		sess.setOpenedNpc(nil)
	}
}

// handleNpcBuy 处理 C3 32：从打开的商店买入（BuyNpcItemAction 全分支）。
func (s *Server) handleNpcBuy(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	if c.Stats == nil {
		resolved, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 买入前解析角色属性失败 %s: %v", c.Name, err)
			return
		}
		c.Stats = resolved
		if wp2 := sess.getWorldPlayer(); wp2 != nil {
			wp2.Stats = c.Stats
		}
	}
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	fail := func() {
		_ = view.ShowNpcItemBuyFailed()
	}
	opened := sess.getOpenedNpc()
	storeOpen := opened != nil
	gc := s.deps.cfg.GameConfig
	var store *config.MerchantStoreEntry
	if storeOpen && gc != nil {
		store, _ = gc.MerchantStore(int(opened.Number))
	}
	items := s.shopItems(store)
	if len(frame) < c2s.BuyItemFromNpcRequestLength {
		fail()
		return
	}
	slot := c2s.AsBuyItemFromNpcRequest(frame).ItemSlot()

	inv := s.ensureInventory(c)
	res := action.BuyDecision(storeOpen, items, c.Stats.Money, slot,
		func(si *action.ShopItem) (byte, bool) { return s.stackTargetSlot(c, si) },
		func(w, h byte) bool { return inventoryHasSpace(inv, w, h) })

	switch res.Outcome {
	case action.ShopBought:
		// 入包（原版 AssignValues + CheckInvSpace 定槽 → AddItemAsync）。
		it := &item.Item{
			Group:       byte(res.NewItem.Def.Group),
			Number:      res.NewItem.Def.Number,
			Level:       res.NewItem.Level,
			Durability:  res.NewItem.Durability,
			HasSkill:    res.NewItem.HasSkill,
			Luck:        res.NewItem.Luck,
			OptionLevel: res.NewItem.OptionLevel,
		}
		if res.NewItem.Excellent {
			it.ExcellentBits = 0x3F // 精确位随掉落系统（商店货无卓越，防御性置位）
		}
		si := s.newSlottedItem(it)
		if !inv.AddToFree(si) {
			fail()
			return
		}
		c.Stats.Money -= res.Price
		_ = view.ShowNpcItemBought(si.Slot, encodeItemForClient(it))
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)

	case action.ShopBoughtStacked:
		target := inv.GetItem(res.StackTarget)
		if target == nil {
			fail()
			return
		}
		target.It.Durability += res.NewItem.Durability
		c.Stats.Money -= res.Price
		// 原版顺序：耐久更新 → 失败包（结束手势）→ 金币更新。
		_ = view.ShowItemDurabilityChanged(target.Slot, target.It.Durability, false)
		_ = view.ShowNpcItemBuyFailed()
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)

	case action.ShopBuyInventoryFull, action.ShopBuyNoMoney, action.ShopBuyUnknownItem, action.ShopBuyNoStore:
		// 原版 BuyNpcItemAction：三种具体原因各出一条蓝字，随后才是 C1 32 FF；
		// 未开商店（NoStore）原版只回失败包、无文字。
		switch res.Outcome {
		case action.ShopBuyUnknownItem:
			s.showLocalizedMessage(sess, player.MsgItemUnknown)
		case action.ShopBuyInventoryFull:
			s.showLocalizedMessage(sess, player.MsgInventoryFull)
		case action.ShopBuyNoMoney:
			s.showLocalizedMessage(sess, player.MsgNotEnoughMoney)
		}
		fail()
	}
}

// handleNpcSell 处理 C3 33：向打开的商店卖出（SellItemToNpcAction 全分支）。
func (s *Server) handleNpcSell(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	if c.Stats == nil {
		resolved, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 卖出前解析角色属性失败 %s: %v", c.Name, err)
			return
		}
		c.Stats = resolved
		if wp2 := sess.getWorldPlayer(); wp2 != nil {
			wp2.Stats = c.Stats
		}
	}
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	opened := sess.getOpenedNpc()
	storeOpen := opened != nil && s.deps.cfg.GameConfig != nil
	if storeOpen {
		if _, ok := s.deps.cfg.GameConfig.MerchantStore(int(opened.Number)); !ok {
			storeOpen = false // 有对话但该 NPC 无商店 → 原版 MerchantStore == null 分支
		}
	}
	if len(frame) < c2s.SellItemToNpcRequestLength {
		_ = view.ShowNpcItemSellResult(false, c.Stats.Money)
		return
	}
	slot := c2s.AsSellItemToNpcRequest(frame).ItemSlot()
	inv := s.ensureInventory(c)
	si := inv.GetItem(slot)

	var shopItem *action.ShopItem
	if si != nil && si.It != nil {
		shopItem = &action.ShopItem{
			Def:         s.shopItemDef(int(si.It.Group), si.It.Number),
			Level:       si.It.Level,
			Durability:  si.It.Durability,
			HasSkill:    si.It.HasSkill,
			Luck:        si.It.Luck,
			OptionLevel: si.It.OptionLevel,
			Excellent:   si.It.ExcellentBits != 0,
			Guardian:    si.It.GuardianOption,
		}
	}

	outcome, _, money := action.SellDecision(storeOpen, c.Stats.Money, shopItem)
	switch outcome {
	case action.ShopSold:
		if !inv.Remove(si) {
			_ = view.ShowNpcItemSellResult(false, c.Stats.Money)
			return
		}
		c.Stats.Money = money
		_ = view.ShowNpcItemSellResult(true, c.Stats.Money)
	default:
		// 拒绝/金币放不下：原版一律回 fail 帧（带上当前金币）。
		_ = view.ShowNpcItemSellResult(false, c.Stats.Money)
	}
}

// shopItemDef 把导出件物品定义投影为商店计算所需（缺定义返回 nil → 决策按拒绝处理）。
func (s *Server) shopItemDef(group, number int) *action.ShopItemDef {
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return nil
	}
	def, ok := gc.Item(group, number)
	if !ok {
		return nil
	}
	return &action.ShopItemDef{
		Group:              def.Group,
		Number:             def.Number,
		Width:              def.Width,
		Height:             def.Height,
		DropLevel:          def.DropLevel,
		MaximumItemLevel:   def.MaximumItemLevel,
		Durability:         def.Durability,
		Value:              def.Value,
		HasSkill:           def.HasSkill,
		IsWearable:         def.IsWearable(),
		IsBoundToCharacter: def.IsBoundToCharacter,
	}
}

// shopItems 把商店导出件投影为判定条目（store 为 nil → 空清单）。
func (s *Server) shopItems(store *config.MerchantStoreEntry) []action.ShopItem {
	if store == nil {
		return nil
	}
	out := make([]action.ShopItem, 0, len(store.Items))
	for _, si := range store.Items {
		out = append(out, action.ShopItem{
			Slot:        si.Slot,
			Def:         s.shopItemDef(int(si.Group), int(si.Number)),
			Level:       si.Level,
			Durability:  si.Durability,
			HasSkill:    si.HasSkill,
			Luck:        si.Luck,
			OptionLevel: int(si.OptionLevel),
		})
	}
	return out
}

// stackTargetSlot 找"可完全堆上"的背包物品（原版 CanCompletelyStackOn：
// 同 group/number 且 store.Durability + target.Durability ≤ 定义耐久）。
func (s *Server) stackTargetSlot(c *entity.Character, store *action.ShopItem) (byte, bool) {
	if c.Inventory == nil || !store.IsStackable() {
		return 0, false
	}
	grid := c.Inventory.Grid()
	for idx, si := range grid.Items() {
		if si == nil || si.It == nil {
			continue
		}
		if int(si.It.Group) != store.Def.Group || si.It.Number != store.Def.Number {
			continue
		}
		if int(si.It.Durability)+int(store.Durability) <= store.Def.Durability {
			return byte(idx), true
		}
	}
	return 0, false
}

// inventoryHasSpace 判断 w×h 物品在背包网格是否有落位（原版 CheckInvSpace 的
// 纯检查部分；装备区不参与商店货摆放）。
func inventoryHasSpace(inv *storage.Inventory, w, h byte) bool {
	return inv.Grid().HasSpaceFor(int(w), int(h))
}
