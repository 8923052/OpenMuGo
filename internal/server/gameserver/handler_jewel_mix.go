package gameserver

// handler_jewel_mix.go —— C1 BC Lahap 宝石升档合成 / 降档拆分
// （对照 MessageHandler/Items/JewelMixHandlerPlugIn.cs + PlayerActions/Items/ItemStackAction.cs）。
//
// 判定全在 action.PlanJewelStack / PlanJewelUnstack；本文件只做"开没开 Lahap 窗"、
// 容器变更与出站序列。出站次序按原版：逐件 ItemRemoved → 新件 ItemAddedToInventory →
// 金币更新（InventoryMoneyUpdate）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/view/remote"

	c2s "mugo/internal/proto/c2s"
)

// handleJewelMix 处理 C1 BC（7B）：Operation 0=合成 1=拆分。
func (s *Server) handleJewelMix(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.LahapJewelMixRequestLength {
		return
	}
	c := sess.getSelected()
	gc := s.deps.cfg.GameConfig
	if c == nil || gc == nil {
		return
	}
	req := c2s.AsLahapJewelMixRequest(frame)
	op := req.Operation()
	// 原版 IsCorrectNpcOpened：没开着 Lahap 窗口就发这条，被当作刷物品尝试——
	// 合成只记警告，拆分**直接断开连接**（ItemStackAction.cs:92）。
	if !s.lahapWindowOpen(sess) {
		s.deps.logger.Printf("gameserver: 未开 Lahap 却发 BC（疑似刷物品）%s op=%d", c.Name, op)
		if op == c2s.MixType_Unmix {
			s.closeSession(sess)
		}
		return
	}
	mix, ok := gc.JewelMixByNumber(int(req.Item()))
	if !ok {
		s.deps.logger.Printf("gameserver: BC 未知宝石合成序号 %d（%s）", req.Item(), c.Name)
		return
	}
	switch op {
	case c2s.MixType_Mix:
		s.jewelMixStack(sess, c, mix, req.MixingStackSize())
	case c2s.MixType_Unmix:
		s.jewelMixUnstack(sess, c, mix, req.UnmixingSourceSlot())
	default:
		// 原版是 throw InvalidEnumArgumentException；本仓按"记日志不动作"处理。
		s.deps.logger.Printf("gameserver: BC 未知操作 %d（%s）", op, c.Name)
	}
}

// jewelMixStack 执行合成：吃 stackSize 件散宝石 → 产出 1 件打包宝石（等级 = 档数-1）。
func (s *Server) jewelMixStack(sess *session, c *entity.Character, mix *config.JewelMix, stackSizeCode c2s.StackSize) {
	stackSize, ok := action.JewelStackSize(byte(stackSizeCode))
	if !ok {
		s.deps.logger.Printf("gameserver: BC 未知堆叠档 %d（%s）", stackSizeCode, c.Name)
		return
	}
	inv := s.ensureInventory(c)
	if c.Stats == nil {
		c.Stats = &entity.CharStats{}
	}
	plan := action.PlanJewelStack(mix, stackSize, inv, c.Stats.Money)
	switch plan.Reject {
	case action.JewelMixLackOfJewels:
		s.showLocalizedMessage(sess, player.MsgLackingJewels)
		return
	case action.JewelMixNotEnoughMoney:
		s.showLocalizedMessage(sess, player.MsgNotEnoughMoney)
		return
	}
	c.Stats.Money -= plan.Fee
	view := s.viewFor(sess)
	for _, slot := range plan.Sources {
		si := inv.GetItem(slot)
		if si == nil {
			continue
		}
		inv.Remove(si)
		if view != nil {
			_ = view.ShowItemRemoved(slot)
		}
	}
	added := s.newSlottedItem(plan.Result)
	if !inv.AddToFree(added) {
		// 原版不加失败分支（AddItemAsync 满包时返回 false 并被忽略）——宝石已吃掉、
		// 产物落地失败即原版既有的丢失口径，这里同样只记日志。
		s.deps.logger.Printf("gameserver: BC 合成产物无空位（%s），物品丢失（原版同口径）", c.Name)
	} else if view != nil {
		_ = view.ShowItemAddedToInventory(added.Slot, encodeItemForClient(added.It))
	}
	if view != nil {
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
	}
	s.deps.logger.Printf("gameserver: BC 合成 %s 档%d×%d → 组%d/号%d 等级%d",
		c.Name, stackSize, mix.Number, mix.Mixed.Group, mix.Mixed.Number, plan.Result.Level)
}

// jewelMixUnstack 执行拆分：1 件打包宝石 → (等级+1)×10 件散宝石，费用 1_000_000。
func (s *Server) jewelMixUnstack(sess *session, c *entity.Character, mix *config.JewelMix, slot byte) {
	inv := s.ensureInventory(c)
	if c.Stats == nil {
		c.Stats = &entity.CharStats{}
	}
	plan := action.PlanJewelUnstack(mix, inv.GetItem(slot), inv, c.Stats.Money)
	switch plan.Reject {
	case action.JewelMixStackedNotFound:
		s.showLocalizedMessage(sess, player.MsgStackedJewelNotFound)
		return
	case action.JewelMixNotStackedJewel:
		s.showLocalizedMessage(sess, player.MsgNotStackedJewel)
		return
	case action.JewelMixNotEnoughMoney:
		s.showLocalizedMessage(sess, player.MsgNotEnoughMoney)
		return
	case action.JewelMixNoInventorySpace:
		// 原版先 TryRemoveMoney 再查空槽，且失败**不退款也不发金币帧**（照抄，见 doc/16）。
		c.Stats.Money -= plan.Fee
		s.showLocalizedMessage(sess, player.MsgNoInventorySpace)
		return
	}
	c.Stats.Money -= plan.Fee
	view := s.viewFor(sess)
	si := inv.GetItem(slot)
	if si != nil {
		inv.Remove(si)
		if view != nil {
			_ = view.ShowItemRemoved(slot)
		}
	}
	for _, free := range plan.Targets {
		jewel := s.newSlottedItem(&item.Item{
			Group: byte(mix.Single.Group), Number: mix.Single.Number, Durability: 1,
		})
		if !inv.AddToSlot(free, jewel) {
			continue
		}
		if view != nil {
			_ = view.ShowItemAddedToInventory(free, encodeItemForClient(jewel.It))
		}
	}
	if view != nil {
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
	}
	s.deps.logger.Printf("gameserver: BC 拆分 %s 槽%d → %d 件组%d/号%d",
		c.Name, slot, plan.Pieces, mix.Single.Group, mix.Single.Number)
}

// lahapWindowOpen 报告当前开着的 NPC 对话是不是宝石合成师 Lahap 的窗口
// （原版 ItemStackAction.IsCorrectNpcOpened 比对 NpcWindow.Lahap）。
func (s *Server) lahapWindowOpen(sess *session) bool {
	n := sess.getOpenedNpc()
	return n != nil && n.Def != nil && int(n.Def.NpcWindow) == remote.NpcWindowLahap
}

// closeSession 关闭会话连接（断连清理由读循环的 EOF 分支完成）。
func (s *Server) closeSession(sess *session) {
	if sess.conn != nil {
		_ = sess.conn.Close()
	}
}
