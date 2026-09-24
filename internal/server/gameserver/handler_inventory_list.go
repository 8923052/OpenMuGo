package gameserver

// handler_inventory_list.go —— F3 10 背包清单下发（真机修复 11），
// 对照原版：
//   - 出站 UpdateInventoryListPlugIn（C1 F3 10 CharacterInventory）：选角流程在 92B 之后
//     由 SelectCharacterAsync 调用（Player.cs 1748）；按槽位排序 + 同槽去重 +
//     actualSize 收缩（本包在视图层实现收缩）。
//   - 入站 MuMain 收 F3 10 时 UnequipAllItems + DeleteAllItems 全清再逐件装——
//     **不下发则客户端装备栏/背包为空**，同时外观靠入视野包独立表达（两个数据源）。
//
// 实测症状（test300 装备栏空 + 外观乱堆）的根因即本包缺失：装备栏空 = F3 10 没发；
// 外观乱 = 角色实体的 AppearanceExt 预编码与背包实际装备不一致是**两个独立数据源**，
// 而种子此前只编码了"职业号"，装备外观来自入视野包（正常），但空装备栏 + 网格散件
// 会让客户端用默认外观叠加渲染。修复后两者同源。
//
// 触发点：enterWorld（选角/换图重入共用，对照原版 ClientReadyAfterMapChangeAsync 后
// 的视图刷新序列——原版在 SelectCharacterAsync 与 ClientReadyAfterMapChangeAsync
// 两条路都会触发属性/背包/技能下发）。

import (
	"sort"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/storage"
)

// handleInventoryRequest 处理 C3 F3 10：下发当前角色背包清单。
func (s *Server) handleInventoryRequest(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	if c == nil || c.Inventory == nil {
		return
	}
	_ = frame
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowInventoryList(s.inventoryViews(c))
	}
}

// inventoryViews 把角色背包投影为清单条目（按槽位升序，同槽去重——原版同）。
func (s *Server) inventoryViews(c *entity.Character) []action.MerchantItemView {
	inv := c.Inventory
	views := make([]action.MerchantItemView, 0, inv.Count())
	seen := map[byte]bool{}
	grid := inv.Grid()
	type slotItem struct {
		slot byte
		si   *storage.SlottedItem
	}
	var pairs []slotItem
	for idx, si := range grid.Items() {
		if si == nil || si.It == nil {
			continue
		}
		slot := byte(grid.SlotOffset() + idx)
		if seen[slot] {
			continue
		}
		seen[slot] = true
		pairs = append(pairs, slotItem{slot: slot, si: si})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].slot < pairs[j].slot })
	for _, p := range pairs {
		views = append(views, action.MerchantItemView{Slot: p.slot, Data: encodeItemForClient(p.si.It)})
	}
	return views
}

// sendInventoryList 在进图（enterWorld）后主动下发背包清单（原版 SelectCharacterAsync
// 在 92B 之后同步调用 UpdateInventoryListAsync；换图重入同）。
func (s *Server) sendInventoryList(sess *session) {
	c := sess.getSelected()
	if c == nil {
		return
	}
	if c.Inventory == nil {
		s.ensureInventory(c)
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowInventoryList(s.inventoryViews(c))
	}
}
