package gameserver

// handler_vault.go —— 仓库 Vault 的服务端处理，对照原版：
//   - TalkNpcAction 的 NpcWindow.VaultStorage 分支 → ShowVaultPlugIn.ShowVaultAsync；
//   - VaultMoneyHandlerPlugIn（C1 81）→ TryDeposit/TryTakeVaultMoney；
//   - VaultCloseHandlerPlugIn（C1 82）→ CloseNpcDialogAction + CloseVaultPlugIn。
//
// 仓库容器与金钱挂在账号实体上（entity.Account.Vault），会话期间有效。

import (
	"strconv"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/view/remote"
)

// npcWindowVaultStorage 是 DataModel NpcWindow.VaultStorage 枚举值（=4）。
// 注意与线协议 s2c.NpcWindow_VaultStorage(=2) 不同，仅用于 NPC 定义判定。
const npcWindowVaultStorage = 4

// vaultOpen 校验仓库上下文：打开的 NPC 是仓库窗口且账号仓库已初始化
// （对照原版状态 NpcDialogOpened + openedWindow=VaultStorage）。
func (s *Server) vaultOpen(sess *session) (*entity.Vault, bool) {
	n := sess.getOpenedNpc()
	if n == nil || int(n.Def.NpcWindow) != npcWindowVaultStorage {
		return nil, false
	}
	a := sess.getAccount()
	if a == nil || a.Vault == nil {
		return nil, false
	}
	return a.Vault, true
}

// openVault 处理与仓库 NPC 的对话：建仓 + 窗口 + 物品清单 + 金钱 + 锁状态
// （对照 ShowVaultPlugIn.ShowVaultAsync 四步）。
func (s *Server) openVault(sess *session) {
	a := sess.getAccount()
	c := sess.getSelected()
	if a == nil || c == nil {
		return
	}
	if c.Stats == nil {
		st, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 打开仓库前解析属性失败 %s: %v", c.Name, err)
			return
		}
		c.Stats = st
	}
	if a.Vault == nil {
		a.Vault = &entity.Vault{Items: storage.New("vault", entity.VaultSlots, 0, 0)}
	}
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	_ = view.ShowNpcWindow(remote.NpcWindowVaultStorage)
	items := make([]action.MerchantItemView, 0, a.Vault.Items.Count())
	for _, si := range a.Vault.Items.Items() {
		if si == nil {
			continue
		}
		items = append(items, action.MerchantItemView{Slot: si.Slot, Data: encodeItemForClient(si.It)})
	}
	if len(items) > 0 {
		_ = view.ShowMerchantStoreItemList(action.StoreKindNormal, items)
	}
	_ = view.ShowVaultMoneyUpdate(true, a.Vault.Money, c.Stats.Money)
	_ = view.ShowVaultProtectionState(action.VaultProtectionState(a, sess.vaultLocked))
}

// handleVaultMoney 处理 C1 81：背包↔仓库金钱移动。
func (s *Server) handleVaultMoney(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	v, ok := s.vaultOpen(sess)
	c := sess.getSelected()
	if !ok || c == nil || c.Stats == nil || len(frame) < int(c2s.VaultMoveMoneyRequestLength) {
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowVaultMoneyUpdate(false, 0, 0)
		}
		return
	}
	req := c2s.AsVaultMoveMoneyRequest(frame)
	success, newInv, newVault := action.MoveVaultMoney(int(req.Direction()), req.Amount(), c.Stats.Money, v.Money)
	view := s.viewFor(sess)
	if !success {
		// 余额不足/越界：原版 UpdateVaultMoneyAsync(false)，客户端回滚输入。
		if view != nil {
			_ = view.ShowVaultMoneyUpdate(false, 0, 0)
		}
		return
	}
	c.Stats.Money = newInv
	v.Money = newVault
	if view != nil {
		_ = view.ShowVaultMoneyUpdate(true, newVault, newInv)
	}
}

// handleVaultClose 处理 C1 82：关闭仓库（对照 CloseNpcDialogAction：清对话上下文，
// CloseVaultPlugIn 回 C1 82 让客户端关窗）。
func (s *Server) handleVaultClose(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	n := sess.getOpenedNpc()
	if n != nil && int(n.Def.NpcWindow) == npcWindowVaultStorage {
		sess.setOpenedNpc(nil)
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowVaultClosed()
		}
		return
	}
	// 容错：打开的是其他窗口时仅清上下文（与 C1 31 关闭语义一致），不下发仓库包。
	if n != nil {
		sess.setOpenedNpc(nil)
	}
}

// handleItemMoveVault 处理背包↔仓库的 C3 24 物品搬运。
func (s *Server) handleItemMoveVault(sess *session, fromStorage, toStorage, fromSlot, toSlot byte) {
	kindVault := byte(c2s.ItemStorageKind_Vault)
	// 仓库窗口下只接受 Inventory/Vault 两端（原版 IsStorageContextAllowed：
	// 交易/混沌盒等临时存储在此状态不可用）。
	kindInventory := byte(c2s.ItemStorageKind_Inventory)
	if (fromStorage != kindVault && fromStorage != kindInventory) ||
		(toStorage != kindVault && toStorage != kindInventory) {
		s.replyItemMoveFailed(sess, nil)
		return
	}
	v, ok := s.vaultOpen(sess)
	c := sess.getSelected()
	if !ok || c == nil {
		s.replyItemMoveFailed(sess, nil)
		return
	}
	if c.Stats == nil {
		st, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 仓库搬运前解析属性失败 %s: %v", c.Name, err)
			s.replyItemMoveFailed(sess, nil)
			return
		}
		c.Stats = st
	}
	inv := s.ensureInventory(c)

	// 三种方向：Vault→Inventory、Inventory→Vault、Vault 内部搬运。
	// toInventory=true（Vault→Inventory）：源在仓库；否则源在背包。
	fromVault := fromStorage == kindVault
	toVault := toStorage == kindVault
	toInventory := !toVault && fromVault
	withinVault := fromVault && toVault
	if fromVault && toInventory && sess.vaultLocked {
		s.deps.logger.Printf("gameserver: 仓库上锁，拒绝取出 %s", c.Name)
		s.showLocalizedMessage(sess, player.MsgVaultIsLocked)
		return
	}
	var si *storage.SlottedItem
	if fromVault {
		si = v.Items.GetItem(fromSlot)
	} else {
		si = inv.Grid().GetItem(fromSlot)
	}
	if si == nil {
		s.replyItemMoveFailed(sess, nil)
		return
	}
	env := s.moveItemEnv(c)
	def := env.DefOf(si.It)
	var moveOK bool
	if withinVault {
		moveOK = action.DecideVaultMoveWithin(v.Items, fromSlot, toSlot, si.It, def) == action.MoveNormal
	} else {
		moveOK = action.DecideVaultMove(toInventory, inv, v.Items, fromSlot, toSlot, si.It, def, env) == action.MoveNormal
	}
	if !moveOK {
		s.replyItemMoveFailed(sess, si.It)
		return
	}
	srcData := encodeItemForClient(si.It)
	var applied bool
	if withinVault {
		applied = action.ApplyVaultMoveWithin(v.Items, fromSlot, toSlot, si)
	} else {
		applied = action.ApplyVaultMove(toInventory, inv, v.Items, fromSlot, toSlot, si)
	}
	if !applied {
		s.replyItemMoveFailed(sess, si.It)
		return
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowItemMoved(toStorage, toSlot, srcData)
	}

	// 装备槽变化（仓库取出直接穿上 / 穿着的装备直接存进仓库）→ 外观广播 + 属性重算 + 技能。
	// 仓库内部搬运不触及装备区，必须排除（vault 槽 0~11 与装备槽编号重号）。
	equipChanged := !withinVault &&
		((toInventory && inv.WearsSlot(toSlot)) || (!toInventory && inv.WearsSlot(fromSlot)))
	if equipChanged {
		wp := sess.getWorldPlayer()
		if toInventory {
			s.broadcastAppearanceChange(wp, c, toSlot, si.It, true)
		} else {
			s.broadcastAppearanceChange(wp, c, fromSlot, si.It, false)
		}
		s.refreshCombatValues(sess, c, false)
		s.refreshEquipmentStats(sess, c)
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowSkillList(s.skillListViewOf(c))
		}
	}
}

// handleVaultLockSub 处理 C1 83 族（按子码分派）：00 解锁、01 设 PIN、02 撤销 PIN。
// 对照 MessageHandler/Vault/{UnlockVaultPlugIn,SetVaultPinPlugIn,RemoveVaultPinPlugIn}，
// 三者都是 IsEncryptionExpected=false 的明文包。
func (s *Server) handleVaultLockSub(sess *session, sub byte, frame []byte) {
	switch sub {
	case c2s.UnlockVaultSubCode:
		s.handleVaultUnlock(sess, frame)
	case c2s.SetVaultPinSubCode:
		s.handleVaultSetPin(sess, frame)
	case c2s.RemoveVaultPinSubCode:
		s.handleVaultRemovePin(sess, frame)
	default:
		s.deps.logger.Printf("gameserver: 忽略仓库锁定子码 0x%02X", sub)
	}
}

// handleVaultUnlock 处理 83 00：PIN 对才解开本会话的锁（错 5 次暂时锁定原版并不存在，
// 见 doc/17 对旧台账的更正）。
func (s *Server) handleVaultUnlock(sess *session, frame []byte) {
	acc := sess.getAccount()
	if acc == nil || len(frame) < c2s.UnlockVaultLength {
		return
	}
	c := vaultPin(c2s.AsUnlockVault(frame).Pin())
	s.applyVaultChange(sess, action.VaultUnlock(acc, c))
}

// handleVaultSetPin 处理 83 01：账号密码 + 新 PIN（只在上锁之外可用，原版语义）。
func (s *Server) handleVaultSetPin(sess *session, frame []byte) {
	acc := sess.getAccount()
	if acc == nil || len(frame) < c2s.SetVaultPinLength {
		return
	}
	req := c2s.AsSetVaultPin(frame)
	s.applyVaultChange(sess, action.VaultSetPin(acc, sess.vaultLocked, vaultPin(req.Pin()), req.PasswordString()))
}

// handleVaultRemovePin 处理 83 02：账号密码对则清 PIN 并解锁。
func (s *Server) handleVaultRemovePin(sess *session, frame []byte) {
	acc := sess.getAccount()
	if acc == nil || len(frame) < c2s.RemoveVaultPinLength {
		return
	}
	s.applyVaultChange(sess, action.VaultRemovePin(acc, c2s.AsRemoveVaultPin(frame).PasswordString()))
}

// applyVaultChange 落地下锁状态变化并把状态字节回给客户端（一条 C1 83）。
func (s *Server) applyVaultChange(sess *session, change action.VaultLockChange) {
	if change.NowLocked != nil {
		sess.vaultLocked = *change.NowLocked
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowVaultProtectionState(change.State)
	}
}

// vaultPin 把线上 ushort 还原成原版用的字符串形态（Pin.ToString()；前导零与原版一样丢失）。
func vaultPin(pin uint16) string {
	return strconv.Itoa(int(pin))
}
