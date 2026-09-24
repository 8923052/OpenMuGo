package remote

// playershop_view.go —— S4 个人商店出站，对应 OpenMU RemoteView/PlayerShop/* 与
// RemoteView/Inventory/PlayerShopBuyRequestResultExtendedPlugIn 等。商店清单条目变长
// 打包（stride 仅用于预分配，帧长 = 实写字节数），与 ShowMerchantStoreItemList 同一手法。

import (
	"fmt"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
)

// shopItemFixedLen 是 PlayerShopItemExtended 除物品数据外的定长（价格4+类型2+数量2+槽1）。
const shopItemFixedLen = 9

// shopListHeaderLen 是 PlayerShopItemListExtended 头部到 ItemCount 的长度。
const shopListHeaderLen = 55

func (v *PlayerView) ShowPlayerShopOpenResult(success bool) error {
	p := s2c.NewPlayerShopOpenSuccessful()
	p.SetSuccess(success)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowItemPriceSetResult(inventorySlot byte, result action.ItemPriceResult) error {
	p := s2c.NewPlayerShopSetItemPriceResponse()
	p.SetInventorySlot(inventorySlot)
	p.SetResult(s2c.ItemPriceSetResult(result))
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopClosed(success bool, playerID uint16) error {
	p := s2c.NewPlayerShopClosed()
	p.SetSuccess(success)
	p.SetPlayerId(playerID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopClosedNotice(playerID uint16) error {
	p := s2c.NewPlayerShopClosed()
	p.SetSuccess(true)
	p.SetPlayerId(playerID)
	return v.send.Send(p.Bytes())
}

// ShowPlayerShops 实现 action.PlayerView（C2 3F 00 PlayerShops）。
// 对照 ShowShopsOfPlayersPlugIn.Write：6B 头 + 每条 38B（ID 2B + 店名 36B）；
// count 必须与实际写入条数一致（客户端按 count 索引）。
func (v *PlayerView) ShowPlayerShops(shops []action.PlayerShopEntry) error {
	p := s2c.NewPlayerShops(s2c.PlayerShopsRequiredSize(len(shops)))
	p.SetShopCount(byte(len(shops)))
	for i, s := range shops {
		blk := p.Shops(i)
		if blk == nil {
			return fmt.Errorf("remote: 3F 00 第 %d 条越界（count=%d）", i, len(shops))
		}
		blk.SetPlayerId(s.ObjectID)
		blk.SetStoreName(s.StoreName)
	}
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopCloseDialog(playerID uint16) error {
	p := s2c.NewClosePlayerShopDialog()
	p.SetPlayerId(playerID)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopItemList(sellerID uint16, sellerName, shopName string, items []action.PlayerShopItemEntry) error {
	size := shopListHeaderLen
	for _, it := range items {
		if len(it.Data) < ItemExtendedMinSize || len(it.Data) > ItemExtendedMaxSize {
			return fmt.Errorf("remote: 商店物品 %d 数据长度 %d 超出扩展布局", it.Slot, len(it.Data))
		}
		size += shopItemFixedLen + len(it.Data)
	}
	p := s2c.NewPlayerShopItemListExtended(size)
	p.SetAction(s2c.ActionKind2(0))
	p.SetSuccess(true)
	p.SetPlayerId(sellerID)
	p.SetPlayerName(sellerName)
	p.SetShopName(shopName)
	p.SetItemCount(byte(len(items)))
	off := shopListHeaderLen
	for _, it := range items {
		stride := shopItemFixedLen + len(it.Data)
		e := s2c.AsPlayerShopItemExtended(p.Bytes()[off : off+stride])
		e.SetMoneyPrice(it.Price)
		e.SetPriceItemType(0)
		e.SetRequiredItemAmount(0)
		e.SetItemSlot(it.Slot)
		copy(e.ItemData(), it.Data)
		off += stride
	}
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopBuyResult(sellerID uint16, result action.ShopBuyResult, itemSlot byte, itemData []byte) error {
	if len(itemData) > ItemExtendedMaxSize {
		itemData = itemData[:ItemExtendedMaxSize]
	}
	p := s2c.NewPlayerShopBuyResultExtended(s2c.PlayerShopBuyResultExtendedRequiredSize(len(itemData)))
	p.SetSellerId(sellerID)
	p.SetResult(s2c.ResultKind2(result))
	p.SetItemSlot(itemSlot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowPlayerShopItemSold(itemSlot byte, buyerName string) error {
	p := s2c.NewPlayerShopItemSoldToPlayer()
	p.SetInventorySlot(itemSlot)
	p.SetBuyerName(buyerName)
	return v.send.Send(p.Bytes())
}
