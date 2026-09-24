package remote

// npcshop_view.go —— T2-8/T2-9 出站实现：
//   - C1 F3 06 CharacterStatIncreaseResponseExtended（StatIncreaseResultExtendedPlugIn，
//     [MinimumClient(106,3)] → S6 客户端恒发扩展 24B 小端）；
//   - C3 30 NpcWindowResponse / C2 31 StoreItemList（ShowMerchantStoreItemListPlugIn）；
//   - C1 32 ItemBought（NpcItemBoughtPlugIn）/ C1 32 FF NpcItemBuyFailed
//     （BuyNpcItemFailedPlugIn）/ C3 33 NpcItemSellResult（ItemSoldToNpcPlugIn）。

import (
	"fmt"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
)

// ShowStatIncreaseResult 实现 action.PlayerView（C1 F3 06 扩展 24B）。
func (v *PlayerView) ShowStatIncreaseResult(stat action.StatType, added uint16, maximums action.MaximumStats) error {
	p := s2c.NewCharacterStatIncreaseResponseExtended()
	p.SetAttribute(s2c.CharacterStatAttribute(stat))
	p.SetAddedAmount(added)
	p.SetUpdatedMaximumHealth(maximums.Health)
	p.SetUpdatedMaximumMana(maximums.Mana)
	p.SetUpdatedMaximumShield(maximums.Shield)
	p.SetUpdatedMaximumAbility(maximums.Ability)
	return v.send.Send(p.Bytes())
}

// ShowMerchantStoreItemList 实现 action.PlayerView（C2 31）。
// 每个 StoredItem = 1B 槽位 + 物品扩展编码（5~15B 动态，CalcItemLength 定长）；
// 帧长按各物品实际长度累计（对照原版 ShowMerchantStoreItemListPlugIn 的 actualSize
// 收缩语义——stride 只用于预分配，长度字段必须等于实写字节数）。
func (v *PlayerView) ShowMerchantStoreItemList(storeKind byte, items []action.MerchantItemView) error {
	if len(items) == 0 {
		return nil
	}
	size := s2c.StoreItemListRequiredSize(len(items), 0) // 6B 头
	for _, it := range items {
		if len(it.Data) < ItemExtendedMinSize || len(it.Data) > ItemExtendedMaxSize {
			return fmt.Errorf("remote: 商店物品槽 %d 数据长度 %d 超出扩展布局 5~%d", it.Slot, len(it.Data), ItemExtendedMaxSize)
		}
		size += 1 + len(it.Data)
	}
	p := s2c.NewStoreItemList(size)
	p.SetType(s2c.ItemWindow(storeKind))
	p.SetItemCount(byte(len(items)))
	off := 6
	for _, it := range items {
		stride := 1 + len(it.Data)
		si := s2c.AsStoredItem(p.Bytes()[off : off+stride])
		si.SetItemSlot(it.Slot)
		copy(si.ItemData(), it.Data)
		off += stride
	}
	return v.send.Send(p.Bytes())
}

// ShowNpcItemBought 实现 action.PlayerView（C1 32 ItemBought）。
func (v *PlayerView) ShowNpcItemBought(slot byte, itemData []byte) error {
	if len(itemData) < ItemExtendedMinSize || len(itemData) > ItemExtendedMaxSize {
		return fmt.Errorf("remote: 买入物品数据长度 %d 超出扩展布局 5~%d", len(itemData), ItemExtendedMaxSize)
	}
	p := s2c.NewItemBought(s2c.ItemBoughtRequiredSize(len(itemData)))
	p.SetInventorySlot(slot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

// ShowNpcItemBuyFailed 实现 action.PlayerView（C1 32 FF）。
func (v *PlayerView) ShowNpcItemBuyFailed() error {
	p := s2c.NewNpcItemBuyFailed()
	return v.send.Send(p.Bytes())
}

// ShowNpcItemSellResult 实现 action.PlayerView（C3 33）。
func (v *PlayerView) ShowNpcItemSellResult(success bool, money uint32) error {
	p := s2c.NewNpcItemSellResult()
	p.SetSuccess(success)
	p.SetMoney(money)
	return v.send.Send(p.Bytes())
}
