package gameserver

// handler_trade.go —— S3 交易的入站处理，对照 MessageHandler/Trade/* 与 Items 的
// MoveItem→Trade 分支。入站 code：0x36 请求、0x37 应答、0x3A 金额、0x3C 按钮、0x3D 取消。
// 交易物品搬运走 0x24（toStorage/fromStorage = Trade）分派到 handleItemMoveTrade。

import (
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

const (
	tradeInvKind   = byte(c2s.ItemStorageKind_Inventory) // 0
	tradeTradeKind = byte(c2s.ItemStorageKind_Trade)     // 1
)

// tradeRange 是发起交易所需的最大切比雪夫距离（对照原版须在视野内）。
const tradeRange = 12

// handleTradeRequest 处理 C1 36（对照 TradeRequestAction）：同图、双方空闲 → 记待决、发请求帧。
func (s *Server) handleTradeRequest(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil || sess.trade != nil {
		return
	}
	targetID := c2s.AsTradeRequest(frame).PlayerId() & 0x7FFF
	tp := s.world.Map(wp.MapNumber).Player(targetID)
	if tp == nil || tp.Name == wp.Name || chebyshevByte(wp.X, wp.Y, tp.X, tp.Y) > tradeRange {
		return
	}
	tsess := s.sessionOfCharacter(tp.Name)
	if tsess == nil || tsess.trade != nil || tsess.tradeRequestFrom != "" {
		return
	}
	tsess.tradeRequestFrom = wp.Name
	if tp.View != nil {
		_ = tp.View.ShowTradeRequest(wp.Name)
	}
}

// handleTradeResponse 处理 C1 37（对照 TradeAcceptAction）：接受则建交易并向双方发确认应答。
func (s *Server) handleTradeResponse(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	inviter := sess.tradeRequestFrom
	sess.tradeRequestFrom = ""
	if inviter == "" || sess.trade != nil {
		return
	}
	isess := s.sessionOfCharacter(inviter)
	if isess == nil {
		return
	}
	myChar := sess.getSelected()
	if !c2s.AsTradeRequestResponse(frame).TradeAccepted() {
		if v := s.viewFor(isess); v != nil {
			_ = v.ShowTradeRequestAnswer(false, wp.Name, levelOfChar(myChar))
		}
		return
	}
	theirChar := isess.getSelected()
	if isess.trade != nil || theirChar == nil {
		// 邀请者已另开交易/角色丢失：应答失效
		if v := s.viewFor(sess); v != nil {
			_ = v.ShowTradeRequestAnswer(false, inviter, levelOfChar(theirChar))
		}
		return
	}
	if _, ok := s.openTrade(isess, sess); !ok {
		return
	}
	_ = s.viewFor(isess).ShowTradeRequestAnswer(true, myChar.Name, levelOfChar(myChar))
	_ = s.viewFor(sess).ShowTradeRequestAnswer(true, theirChar.Name, levelOfChar(theirChar))
}

// handleTradeMoney 处理 C1 3A（对照 TradeMoneyHandlerPlugIn）。
func (s *Server) handleTradeMoney(sess *session, frame []byte) {
	t := sess.trade
	if t == nil {
		return
	}
	side := t.sideOf(sess)
	s.setTradeMoney(t, side, c2s.AsSetTradeMoney(frame).Amount())
}

// handleTradeButton 处理 C1 3C（对照 TradeButtonHandlerPlugIn）。
func (s *Server) handleTradeButton(sess *session, frame []byte) {
	t := sess.trade
	if t == nil {
		return
	}
	side := t.sideOf(sess)
	checked := c2s.AsTradeButtonStateChange(frame).NewState() == c2s.TradeButtonState_Checked
	s.tradeButtonPressed(t, side, checked)
}

// handleTradeCancel 处理 C1 3D（对照 TradeCancelHandlerPlugIn）。
func (s *Server) handleTradeCancel(sess *session) {
	if sess.trade != nil {
		s.cancelTrade(sess.trade)
	}
}

// handleItemMoveTrade 处理 0x24 中源/目标为 Trade 的搬运（对照 MoveNormalAsync 的 Trade 分支）：
// 背包↔自己的临时容器，成功回自己 ItemMoved、并通知对方 TradeItemAdded/Removed。
func (s *Server) handleItemMoveTrade(sess *session, from, to, fromSlot, toSlot byte) bool {
	t := sess.trade
	if t == nil {
		return false
	}
	side := t.sideOf(sess)
	if side == nil {
		return false
	}
	partner := t.other(side)
	if side.button || partner.button {
		return false // 确认中禁止改动物品（对照 TradeButtonPressed 态不放行）
	}
	inv := s.ensureInventory(side.c)

	var src *storage.SlottedItem
	if from == tradeTradeKind {
		src = side.temp.GetItem(fromSlot)
	} else {
		src = inv.GetItem(fromSlot)
	}
	if src == nil {
		return false
	}
	data := encodeItemForClient(src.It)
	if from == tradeTradeKind {
		side.temp.Remove(src)
	} else {
		inv.Grid().Remove(src)
	}
	rollback := func() {
		if from == tradeTradeKind {
			side.temp.AddToSlot(fromSlot, src)
		} else {
			inv.Grid().AddToSlot(fromSlot, src)
		}
	}

	switch {
	case to == tradeTradeKind:
		if !side.temp.AddToSlot(toSlot, src) {
			rollback()
			return false
		}
		_ = s.tradeView(side).ShowItemMoved(tradeTradeKind, toSlot, data)
		_ = s.tradeView(partner).ShowTradeItemAdded(toSlot, data)
		if from == tradeTradeKind {
			_ = s.tradeView(partner).ShowTradeItemRemoved(fromSlot)
		}
	case from == tradeTradeKind:
		if !inv.AddToSlot(toSlot, src) && !inv.AddToFree(src) {
			side.temp.AddToSlot(fromSlot, src) // 背包放不下：退回临时容器
			return false
		}
		_ = s.tradeView(side).ShowItemMoved(tradeInvKind, src.Slot, data)
		_ = s.tradeView(partner).ShowTradeItemRemoved(fromSlot)
	default:
		rollback()
		return false
	}
	return true
}

// levelOfChar 返回角色等级（无角色 0）。
func levelOfChar(c *entity.Character) uint16 {
	if c == nil {
		return 0
	}
	return c.Level
}
