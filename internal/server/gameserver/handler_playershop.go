package gameserver

// handler_playershop.go —— S4 个人商店的入站处理，对照 MessageHandler/PlayerShop/* 与
// PlayerActions/PlayerStore/*。全部走 C1/C3 的 0x3F 组按 sub 分派：
// 01 定价、02 开店、03 关店、05 拉清单、06 购买、07 买家关窗。
// 店主自己收 02/01/03 回执；买家收 05 清单、06 购买结果。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// playerStore 是店主侧开店状态：背包某格物品挂价（价格>0 即在售）。
type playerStore struct {
	open   bool
	name   string
	prices map[byte]uint32 // 背包槽 → 价格
}

func (s *Server) ensureStore(sess *session) *playerStore {
	if sess.store == nil {
		sess.store = &playerStore{prices: map[byte]uint32{}}
	}
	return sess.store
}

// handlePlayerShopSub 分派 0x3F 组子码。
func (s *Server) handlePlayerShopSub(sess *session, sub byte, frame []byte) {
	switch sub {
	case c2s.PlayerShopOpenSubCode:
		s.handleShopOpen(sess, frame)
	case c2s.PlayerShopSetItemPriceSubCode:
		s.handleShopSetPrice(sess, frame)
	case c2s.PlayerShopCloseSubCode:
		s.handleShopClose(sess)
	case c2s.PlayerShopItemListRequestSubCode:
		s.handleShopListRequest(sess, frame)
	case c2s.PlayerShopItemBuyRequestSubCode:
		s.handleShopBuy(sess, frame)
	case c2s.PlayerShopCloseOtherSubCode:
		// 买家关闭对面商店窗口：纯客户端态，服务端无动作。
	default:
		s.deps.logger.Printf("gameserver: 忽略玩家商店子码 0x%02X", sub)
	}
}

// handleShopOpen 处理 3F 02：开店并回执（对照 OpenStoreAction）。
func (s *Server) handleShopOpen(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	st := s.ensureStore(sess)
	if st.open {
		return
	}
	st.open = true
	st.name = c2s.AsPlayerShopOpen(frame).StoreNameString()
	_ = s.viewFor(sess).ShowPlayerShopOpenResult(true)
	s.announceShopOpened(wp, st)
}

// announceShopOpened 把"某人开店"通知给视野内每个观察者（含店主自己，ID 用其相对 ID），
// 对照原版 OpenStoreAction → 对周围 InvokeOnAroundAsync<IPlayerShopOpenedPlugIn>，
// 每个 viewer 收到一条只含该店主的 C2 3F 00。
func (s *Server) announceShopOpened(wp *world.Player, st *playerStore) {
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o == nil || o.View == nil {
			continue
		}
		id := wp.ID
		if id == o.ID {
			id = world.ConstantPlayerID // 店主看自己那条用哨兵 ID（视野包同规则）
		}
		_ = o.View.ShowPlayerShops([]action.PlayerShopEntry{{ObjectID: id, StoreName: st.name}})
	}
}

// shopsOf 收集给定玩家里"正在开店"的摊位（对照 NewPlayersInScopePlugIn 里
// 顺手攒出的 shopPlayers，一次性发给新看到他们的观察者）。
func (s *Server) shopsOf(viewerID uint16, players []*world.Player) []action.PlayerShopEntry {
	var out []action.PlayerShopEntry
	for _, p := range players {
		if p == nil || p.ID == viewerID {
			continue
		}
		sess := s.sessionOfCharacter(p.Name)
		if sess == nil || sess.store == nil || !sess.store.open {
			continue
		}
		out = append(out, action.PlayerShopEntry{ObjectID: p.ID, StoreName: sess.store.name})
	}
	return out
}

// handleShopSetPrice 处理 3F 01：给背包某格物品定价（对照 SetItemPriceAction）。
func (s *Server) handleShopSetPrice(sess *session, frame []byte) {
	c := sess.getSelected()
	if c == nil {
		return
	}
	req := c2s.AsPlayerShopSetItemPrice(frame)
	slot, price := req.ItemSlot(), req.Price()
	st := s.ensureStore(sess)

	result := action.ItemPriceSuccess
	switch {
	case st.open:
		result = action.ItemPriceFailed // 开店后禁止改价（须先关店）
	case c.Level < 6:
		result = action.ItemPriceLevelTooLow
	case s.ensureInventory(c).GetItem(slot) == nil:
		result = action.ItemPriceNotFound
	}
	if result == action.ItemPriceSuccess {
		if price == 0 {
			delete(st.prices, slot)
		} else {
			st.prices[slot] = price
		}
	}
	_ = s.viewFor(sess).ShowItemPriceSetResult(slot, result)
}

// handleShopClose 处理 3F 03：关店，回执店主并通知视野内玩家（对照 CloseStoreAction）。
func (s *Server) handleShopClose(sess *session) {
	s.closeStore(sess)
}

func (s *Server) closeStore(sess *session) {
	st := sess.store
	if st == nil || !st.open {
		return
	}
	st.open = false
	st.prices = map[byte]uint32{}
	wp := sess.getWorldPlayer()
	if wp == nil {
		return
	}
	_ = s.viewFor(sess).ShowPlayerShopClosed(true, wp.ID)
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.View != nil && o.ID != wp.ID {
			_ = o.View.ShowPlayerShopClosedNotice(wp.ID)
		}
	}
}

// handleShopListRequest 处理 3F 05：把店主的在售清单发给买家。
func (s *Server) handleShopListRequest(sess *session, frame []byte) {
	buyer := enteredPlayer(sess)
	if buyer == nil {
		return
	}
	req := c2s.AsPlayerShopItemListRequest(frame)
	seller, sellerSess := s.resolveShopSeller(buyer, req.PlayerId(), req.PlayerNameString())
	if seller == nil || sellerSess == nil {
		return
	}
	st := sellerSess.store
	if st == nil || !st.open {
		return
	}
	_ = s.viewFor(sess).ShowPlayerShopItemList(seller.ID, seller.Name, st.name, s.shopEntries(sellerSess.getSelected(), st))
}

// handleShopBuy 处理 3F 06：买家购买店主某格物品（对照 BuyRequestAction）。
func (s *Server) handleShopBuy(sess *session, frame []byte) {
	buyer := enteredPlayer(sess)
	buyerChar := sess.getSelected()
	if buyer == nil || buyerChar == nil {
		return
	}
	req := c2s.AsPlayerShopItemBuyRequest(frame)
	sellerID := req.PlayerId() & 0x7FFF
	seller, sellerSess := s.resolveShopSeller(buyer, req.PlayerId(), req.PlayerNameString())
	bv := s.viewFor(sess)
	if seller == nil || sellerSess == nil {
		_ = bv.ShowPlayerShopBuyResult(sellerID, action.ShopBuyNotAvailable, 0, nil)
		return
	}
	st := sellerSess.store
	if st == nil || !st.open {
		_ = bv.ShowPlayerShopBuyResult(seller.ID, action.ShopBuyNotOpened, 0, nil)
		return
	}
	slot := req.ItemSlot()
	price, priced := st.prices[slot]
	sellerChar := sellerSess.getSelected()
	si := s.ensureInventory(sellerChar).GetItem(slot)
	if !priced || si == nil {
		_ = bv.ShowPlayerShopBuyResult(seller.ID, action.ShopBuyNameOrPriceMissing, 0, nil)
		return
	}
	if buyerChar.Stats == nil || sellerChar.Stats == nil {
		return
	}
	if buyerChar.Stats.Money < price {
		_ = bv.ShowPlayerShopBuyResult(seller.ID, action.ShopBuyLackOfMoney, 0, nil)
		return
	}
	itemData := encodeItemForClient(si.It)
	buyerInv := s.ensureInventory(buyerChar)
	if !buyerInv.AddToFree(si) {
		_ = bv.ShowPlayerShopBuyResult(seller.ID, action.ShopBuyNoSpaceOrOverflow, 0, nil)
		return
	}
	// 落账：钱货两讫。
	s.ensureInventory(sellerChar).Grid().Remove(si)
	delete(st.prices, slot)
	buyerChar.Stats.Money -= price
	sellerChar.Stats.Money += price

	_ = bv.ShowPlayerShopBuyResult(seller.ID, action.ShopBuySuccess, slot, itemData)
	_ = bv.ShowInventoryMoneyUpdate(buyerChar.Stats.Money)

	if sv := s.viewFor(sellerSess); sv != nil {
		_ = sv.ShowPlayerShopItemSold(slot, buyer.Name)
		_ = sv.ShowItemRemoved(slot)
		_ = sv.ShowInventoryMoneyUpdate(sellerChar.Stats.Money)
	}
	if len(st.prices) == 0 {
		s.closeStore(sellerSess) // 售罄自动关店（对照 BuyRequestAction 尾部分支）
	}
}

// resolveShopSeller 在买家地图按对象 ID 找店主，并校验名字，返回其世界玩家与会话。
func (s *Server) resolveShopSeller(buyer *world.Player, playerID uint16, name string) (*world.Player, *session) {
	swp := s.world.Map(buyer.MapNumber).Player(playerID & 0x7FFF)
	if swp == nil || swp.Name != name {
		return nil, nil
	}
	return swp, s.sessionOfCharacter(swp.Name)
}

// shopEntries 把在售价格项解析成清单条目（跳过背包中已不存在的格）。
func (s *Server) shopEntries(c *entity.Character, st *playerStore) []action.PlayerShopItemEntry {
	inv := s.ensureInventory(c)
	var out []action.PlayerShopItemEntry
	for slot, price := range st.prices {
		si := inv.GetItem(slot)
		if si == nil {
			continue
		}
		out = append(out, action.PlayerShopItemEntry{Slot: slot, Price: price, Data: encodeItemForClient(si.It)})
	}
	return out
}
