package gameserver

// handler_crafting.go —— S8 混沌之锅合成。对照 OpenMU：
//   - TalkNpcAction 打开 ChaosMachine 窗口 → 建临时容器（TemporaryStorage）；
//   - MoveItemAction 到/自 ChaosMachine 存储（0x24，storage=3）；
//   - ChaosMixHandlerPlugIn（0x86）→ ItemCraftAction.MixItemsAsync → SimpleItemCraftingHandler
//     （判定在 action.DecideCraft），结果 C1 86 + 刷新商店清单 C2 31（ChaosMachine）。
// 特殊配方（翅膀/混沌武器等 ItemCraftingHandlerClassName）不在本引擎范围。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

// craftStorageRows 对照 InventoryConstants.TemporaryStorageRows（=4，×8=32 槽）。
const craftStorageRows = 4

const (
	npcWindowChaosMachine = 5                                      // DataModel NpcWindow.ChaosMachine
	craftStorageKind      = byte(c2s.ItemStorageKind_ChaosMachine) // 3
	inventoryStorageKind  = byte(c2s.ItemStorageKind_Inventory)    // 0
)

func newCraftStorage() *storage.Storage {
	return storage.New("chaos", craftStorageRows*storage.RowSize, 0, 0)
}

// openCraft 打开合成窗口：建临时容器（幂等）。原版 TemporaryStorage 与具体 NPC 窗口无关，
// 只要该 NPC 挂有配方就建（混沌锅、坐骑、镶嵌、Elphis 精炼等共用同一容器）。
func (s *Server) openCraft(sess *session) {
	if sess.craftStorage == nil {
		sess.craftStorage = newCraftStorage()
	}
}

// returnCraftItems 关窗/离场：把临时容器里的物品退回背包（放不下则丢弃并记录）。
func (s *Server) returnCraftItems(sess *session) {
	if sess.craftStorage == nil {
		return
	}
	c := sess.getSelected()
	if c != nil {
		inv := s.ensureInventory(c)
		for _, si := range sess.craftStorage.Items() {
			if si == nil {
				continue
			}
			sess.craftStorage.Remove(si)
			if !inv.AddToFree(si) {
				s.deps.logger.Printf("gameserver: 关合成窗退回背包失败（满），丢弃 %s (%d,%d)", c.Name, si.It.Group, si.It.Number)
			}
		}
	}
	sess.craftStorage = nil
}

// closeCraftIfNeeded 在关闭的 NPC 是混沌之锅时退回临时容器物品。
func (s *Server) closeCraftIfNeeded(sess *session) {
	n := sess.getOpenedNpc()
	if n != nil && int(n.Def.NpcWindow) == npcWindowChaosMachine {
		s.returnCraftItems(sess)
	}
}

// handleMix 处理 C1 86（对照 ChaosMixHandlerPlugIn + ItemCraftAction.MixItemsAsync）。
// 配方必须挂在当前打开的 NPC 上（原版按 OpenedNpc.Definition.ItemCraftings 查），
// 第 5 字节是镶嵌槽位（仅长帧携带，SeedSphere 三件套要用）。
func (s *Server) handleMix(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	gc := s.deps.cfg.GameConfig
	if c == nil || gc == nil || sess.craftStorage == nil {
		return
	}
	view := s.viewFor(sess)
	if len(frame) < c2s.ChaosMachineMixRequestLength {
		return
	}
	req := c2s.AsChaosMachineMixRequest(frame)
	npcNumber := -1
	if opened := sess.getOpenedNpc(); opened != nil {
		npcNumber = int(opened.Number)
	}
	craft, ok := gc.CraftingForNpc(int(req.MixType()), npcNumber)
	if !ok {
		_ = view.ShowItemCraftingResult(action.CraftIncorrectMix, nil)
		return
	}
	var socketSlot byte
	if len(frame) > 4 {
		socketSlot = req.SocketSlot()
	}
	var money uint64
	if c.Stats != nil {
		money = uint64(c.Stats.Money)
	}
	roll := func(n int) int {
		if n <= 0 {
			return 0
		}
		return s.world.RngInt(0, n)
	}
	plan := action.DecideCraft(action.CraftRequest{
		Craft: craft, Storage: craftCandidatesFrom(sess.craftStorage, gc), Cfg: gc,
		Roll: roll, Money: money, SocketSlot: socketSlot,
	})

	switch plan.Code {
	case action.CraftLackingMixItems, action.CraftTooManyItems,
		action.CraftIncorrectMix, action.CraftIncorrectBloodCastle:
		_ = view.ShowItemCraftingResult(plan.Code, nil)
		s.refreshCraftStore(sess)
		return
	case action.CraftNotEnoughMoney:
		_ = view.ShowItemCraftingResult(plan.Code, nil)
		return
	}

	if plan.Price > 0 && c.Stats != nil {
		c.Stats.Money -= uint32(plan.Price)
	}
	for _, it := range plan.Consume {
		if si := findSlotted(sess.craftStorage, it); si != nil {
			sess.craftStorage.Remove(si)
		}
	}
	for _, created := range plan.Created {
		_ = sess.craftStorage.AddToFree(s.newSlottedItem(created))
	}
	var resultData []byte
	if plan.Code == action.CraftSuccess && plan.Result != nil {
		resultData = encodeItemForClient(plan.Result)
	}
	_ = view.ShowItemCraftingResult(plan.Code, resultData)
	s.refreshCraftStore(sess)
	if plan.Price > 0 && c.Stats != nil {
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
	}
}

// handleItemMoveCraft 处理 0x24 中源/目标为 ChaosMachine 的搬运（背包↔临时容器）。
func (s *Server) handleItemMoveCraft(sess *session, from, to, fromSlot, toSlot byte) bool {
	if sess.craftStorage == nil {
		return false
	}
	c := sess.getSelected()
	if c == nil {
		return false
	}
	inv := s.ensureInventory(c)

	var src *storage.SlottedItem
	if from == craftStorageKind {
		src = sess.craftStorage.GetItem(fromSlot)
	} else {
		src = inv.GetItem(fromSlot)
	}
	if src == nil {
		return false
	}
	data := encodeItemForClient(src.It)
	if from == craftStorageKind {
		sess.craftStorage.Remove(src)
	} else {
		inv.Grid().Remove(src)
	}
	view := s.viewFor(sess)
	switch {
	case to == craftStorageKind:
		if !sess.craftStorage.AddToSlot(toSlot, src) {
			if from == craftStorageKind {
				sess.craftStorage.AddToSlot(fromSlot, src)
			} else {
				inv.Grid().AddToSlot(fromSlot, src)
			}
			return false
		}
		_ = view.ShowItemMoved(craftStorageKind, toSlot, data)
	case from == craftStorageKind:
		if !inv.AddToSlot(toSlot, src) && !inv.AddToFree(src) {
			sess.craftStorage.AddToSlot(fromSlot, src)
			return false
		}
		_ = view.ShowItemMoved(inventoryStorageKind, src.Slot, data)
	default:
		if from == craftStorageKind {
			sess.craftStorage.AddToSlot(fromSlot, src)
		} else {
			inv.Grid().AddToSlot(fromSlot, src)
		}
		return false
	}
	s.refreshCraftStore(sess)
	return true
}

// craftCandidatesFrom 把临时容器物品投影为合成候选（携带定义）。定义缺失时 Def 为 nil（引擎按不匹配处理）。
func craftCandidatesFrom(st *storage.Storage, gc *config.GameConfig) []action.MixCandidate {
	var out []action.MixCandidate
	for _, si := range st.Items() {
		if si == nil {
			continue
		}
		def, _ := gc.Item(int(si.It.Group), si.It.Number)
		out = append(out, action.MixCandidate{It: si.It, Def: def})
	}
	return out
}

func (s *Server) refreshCraftStore(sess *session) {
	if sess.craftStorage == nil {
		return
	}
	var views []action.MerchantItemView
	for _, si := range sess.craftStorage.Items() {
		if si == nil {
			continue
		}
		views = append(views, action.MerchantItemView{Slot: si.Slot, Data: encodeItemForClient(si.It)})
	}
	_ = s.viewFor(sess).ShowMerchantStoreItemList(action.StoreKindChaos, views)
}

func findSlotted(st *storage.Storage, it *item.Item) *storage.SlottedItem {
	for _, si := range st.Items() {
		if si != nil && si.It == it {
			return si
		}
	}
	return nil
}
