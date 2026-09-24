package gameserver

// handler_repair.go —— S9 修理（C1 34 RepairItemRequest），对照原版
// ItemRepairHandlerPlugIn + ItemRepairAction：slot 0xFF 修全部已装备（跳过宠物槽），
// 否则修指定槽。价格 = pricing.RepairPrice（npcDiscount = 是否开着 NPC 对话框），
// 成功则耐久回满 + C1 2A ItemDurabilityChanged + 金币更新；拒绝按原版只发蓝字、不回失败包。

import (
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/pricing"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

func (s *Server) handleRepair(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	if c == nil {
		return
	}
	req := c2s.AsRepairItemRequest(frame)
	slot := req.ItemSlot()
	npcDiscount := sess.getOpenedNpc() != nil
	if slot == 0xFF {
		s.repairAll(sess, c, npcDiscount)
		return
	}
	s.repairOne(sess, c, slot, npcDiscount)
}

// repairOne 修理指定槽的物品（对照 ItemRepairAction.RepairItemAsync）。
func (s *Server) repairOne(sess *session, c *entity.Character, slot byte, npcDiscount bool) {
	inv := s.ensureInventory(c)
	si := inv.GetItem(slot)
	if si == nil {
		s.deps.logger.Printf("gameserver: 修理失败 槽 %d 空", slot)
		s.showLocalizedMessage(sess, player.MsgNoItemToRepair)
		return
	}
	def, ok := s.deps.cfg.GameConfig.Item(int(si.It.Group), si.It.Number)
	if !ok {
		return
	}
	maxDur := pricing.MaximumDurability(def, si.It)
	if si.It.Durability == maxDur {
		return // 已满耐久，无操作（对照原版直接 return）
	}
	price := pricing.RepairPrice(def, si.It, npcDiscount)
	if c.Stats == nil || uint64(c.Stats.Money) < uint64(price) {
		s.deps.logger.Printf("gameserver: 修理金币不足 %s 槽 %d 需 %d", c.Name, slot, price)
		s.showLocalizedMessage(sess, player.MsgNotEnoughMoneyToRepair)
		return
	}
	c.Stats.Money -= uint32(price)
	si.It.Durability = maxDur
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemDurabilityChanged(slot, maxDur, false)
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
	}
}

// repairAll 修理全部已装备（0..11），跳过宠物槽（对照 RepairAllItemsAsync）。
func (s *Server) repairAll(sess *session, c *entity.Character, npcDiscount bool) {
	inv := s.ensureInventory(c)
	for slot := byte(0); slot < storage.EquippedSlotsCount; slot++ {
		if slot == item.SlotPet {
			continue // 宠物由驯兽师 NPC 修理
		}
		if inv.GetItem(slot) != nil {
			s.repairOne(sess, c, slot, npcDiscount)
		}
	}
}
