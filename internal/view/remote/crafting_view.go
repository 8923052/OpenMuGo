package remote

// crafting_view.go —— S8 合成出站，对应 OpenMU RemoteView/NPC/ShowItemCraftingResultPlugIn
// （C1 86 ItemCraftingResult：结果码 + 可选单产物编码）。

import (
	s2c "mugo/internal/proto/s2c"
)

// ShowItemCraftingResult 实现 action.PlayerView。code 的数值与 s2c.CraftingResult 一致，直接映射。
func (v *PlayerView) ShowItemCraftingResult(code int, itemData []byte) error {
	p := s2c.NewItemCraftingResult(s2c.ItemCraftingResultRequiredSize(len(itemData)))
	p.SetResult(s2c.CraftingResult(code))
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}
