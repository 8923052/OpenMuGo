package gameserver

// trade_manager.go —— S3 交易的服务端状态与结算，对照原版 PlayerActions/Trade/*。
// 交易双方各持一个临时容器（temp，对应 TemporaryStorage 4×8）；金额从背包即时扣出为
// tradingMoney（与 OpenMU TradeMoneyAction 的 TryAddMoney 语义一致）。任一侧的确认在
// 双方都按下时原子交换物品与金额；失败/取消则各自回退。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/storage"
)

// tradeRows 对照 InventoryConstants.TemporaryStorageRows（=4，×8 列 = 32 槽）。
const tradeRows = 4

type tradeSide struct {
	sess   *session
	c      *entity.Character
	name   string
	temp   *storage.Storage
	money  uint32 // 已摆出的金额（已从背包扣出）
	button bool   // 已按下确认
}

type trade struct {
	a, b *tradeSide
}

func newTradeStorage() *storage.Storage {
	return storage.New("trade", tradeRows*storage.RowSize, 0, 0)
}

func (t *trade) other(side *tradeSide) *tradeSide {
	if side == t.a {
		return t.b
	}
	return t.a
}

// sideOf 返回该会话在本交易中的侧（不匹配返回 nil）。
func (t *trade) sideOf(sess *session) *tradeSide {
	switch sess {
	case t.a.sess:
		return t.a
	case t.b.sess:
		return t.b
	}
	return nil
}

// tradeView 返回某侧的出站视图。
func (s *Server) tradeView(side *tradeSide) action.PlayerView {
	return s.viewFor(side.sess)
}

// openTrade 建立双方交易（对照 TradeAcceptAction.OpenTradeAsync）：清空临时容器与金额。
func (s *Server) openTrade(a, b *session) (*trade, bool) {
	ac, bc := a.getSelected(), b.getSelected()
	if ac == nil || bc == nil {
		return nil, false
	}
	t := &trade{
		a: &tradeSide{sess: a, c: ac, name: ac.Name, temp: newTradeStorage()},
		b: &tradeSide{sess: b, c: bc, name: bc.Name, temp: newTradeStorage()},
	}
	a.trade = t
	b.trade = t
	return t, true
}

// setTradeMoney 落实一次金额设置（对照 TradeMoneyAction）：背包回退旧额、扣新额，
// 双方按钮重置为红；自己收 InventoryMoneyUpdate + MoneySetResponse，对方收 MoneyUpdate。
func (s *Server) setTradeMoney(t *trade, side *tradeSide, amount uint32) {
	partner := t.other(side)
	if side.c.Stats == nil {
		return
	}
	side.c.Stats.Money += side.money // 回退旧额
	if side.c.Stats.Money < amount {
		side.c.Stats.Money -= side.money // 余额不足：还原，不改交易态
		return
	}
	side.c.Stats.Money -= amount
	side.money = amount

	if v := s.tradeView(side); v != nil {
		_ = v.ShowInventoryMoneyUpdate(side.c.Stats.Money)
		_ = v.ShowTradeMoneySetResponse()
		_ = v.ShowTradeButtonState(action.TradeButtonRed)
	}
	side.button = false
	if pv := s.tradeView(partner); pv != nil {
		_ = pv.ShowTradeMoneyUpdate(amount)
		_ = pv.ShowTradeButtonState(action.TradeButtonRed)
	}
	partner.button = false
}

// tradeButtonPressed 处理一次确认按钮变化（对照 TradeButtonAction）。
func (s *Server) tradeButtonPressed(t *trade, side *tradeSide, checked bool) {
	partner := t.other(side)
	if !checked {
		side.button = false
		if pv := s.tradeView(partner); pv != nil {
			_ = pv.ShowTradeButtonState(action.TradeButtonUnchecked)
		}
		return
	}
	side.button = true
	if !partner.button {
		if pv := s.tradeView(partner); pv != nil {
			_ = pv.ShowTradeButtonState(action.TradeButtonChecked)
		}
		return
	}
	s.finishTrade(t)
}

// finishTrade 双方确认后原子交换物品与金额（对照 InternalFinishTradeAsync）；
// 放不下则全部回退，结果 FailedByFullInventory。
func (s *Server) finishTrade(t *trade) {
	a, b := t.a, t.b
	if !s.tryTakeAll(a, b) || !s.tryTakeAll(b, a) {
		s.restoreSide(a)
		s.restoreSide(b)
		s.closeTrade(t)
		s.notifyBoth(a.sess, b.sess, action.TradeFailedFullInventory)
		return
	}
	a.c.Stats.Money += b.money // 各自背包已扣己方出价，此处加对方出价
	b.c.Stats.Money += a.money
	s.closeTrade(t)
	s.notifyBoth(a.sess, b.sess, action.TradeSuccess)
	s.refreshTradeInventory(a)
	s.refreshTradeInventory(b)
}

// tryTakeAll 把 from.temp 的全部物品搬到 to 的背包（对照 Inventory.TryTakeAll）。
func (s *Server) tryTakeAll(to, from *tradeSide) bool {
	inv := s.ensureInventory(to.c)
	var moved []*storage.SlottedItem
	for _, it := range from.temp.Items() {
		if it == nil {
			continue
		}
		if !from.temp.Remove(it) {
			continue
		}
		if !inv.AddToFree(it) {
			for _, m := range moved {
				inv.Grid().Remove(m) // 回滚本次已搬入 to 的物品
			}
			return false
		}
		moved = append(moved, it)
	}
	return true
}

// restoreSide 交易失败/取消时把某侧 temp 物品退回其自己背包并归还出价金额。
func (s *Server) restoreSide(side *tradeSide) {
	inv := s.ensureInventory(side.c)
	for _, it := range side.temp.Items() {
		if it == nil {
			continue
		}
		side.temp.Remove(it)
		_ = inv.AddToFree(it)
	}
	if side.c.Stats != nil {
		side.c.Stats.Money += side.money
	}
	side.money = 0
	side.button = false
}

// cancelTrade 单方取消：双方各自回退，收 TradeFinished(Cancelled)。
func (s *Server) cancelTrade(t *trade) {
	sa, sb := t.a.sess, t.b.sess
	s.restoreSide(t.a)
	s.restoreSide(t.b)
	s.closeTrade(t)
	s.notifyBoth(sa, sb, action.TradeCancelled)
}

// closeTrade 断开双方会话上的交易引用。
func (s *Server) closeTrade(t *trade) {
	t.a.sess.trade = nil
	t.a.sess.tradeRequestFrom = ""
	t.b.sess.trade = nil
	t.b.sess.tradeRequestFrom = ""
}

// notifyBoth 向双方当前视图下发交易结束帧（closeTrade 后仍可安全取视图）。
func (s *Server) notifyBoth(a, b *session, result action.TradeResult) {
	_ = s.viewFor(a).ShowTradeFinished(result)
	_ = s.viewFor(b).ShowTradeFinished(result)
}

// refreshTradeInventory 交易成功后刷新某侧背包清单（客户端需看到换进来的物品）。
func (s *Server) refreshTradeInventory(side *tradeSide) {
	s.sendInventoryList(side.sess)
}
