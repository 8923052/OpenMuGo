package gameserver

// handler_vault_test.go —— 仓库 Vault 端到端：打开（窗口+金钱+锁状态）、
// 物品存入/取出（C3 24 跨存储）、金钱存取（C1 81）、关闭（C1 82）、拒绝分支。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/util"
)

const testVaultNpcID uint16 = 0x2BB

type vaultScaffold struct {
	srv      *Server
	sess     *session
	wp       *world.Player
	c        *entity.Character
	rec      *packetRecorder
	vaultNpc *npc.Npc
}

func newVaultScaffold(t *testing.T, money uint32) *vaultScaffold {
	t.Helper()
	srv := newScopeTestSrv(t)
	gc := srv.deps.cfg.GameConfig
	sp := npc.NewSpawner(gc, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	srv.deps.cfg.NPCs = sp

	var vaultNpc *npc.Npc
	for _, n := range sp.ByMap(0) {
		if int(n.Def.NpcWindow) == npcWindowVaultStorage {
			vaultNpc = n
			break
		}
	}
	if vaultNpc == nil {
		t.Fatal("Lorencia 应有仓库 NPC（NpcWindow=VaultStorage）")
	}
	vaultNpc.ID = testVaultNpcID

	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "vaultuser", 120, 125, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: money}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return &vaultScaffold{srv: srv, sess: sess, wp: wp, c: c, rec: rec, vaultNpc: vaultNpc}
}

// open 与仓库 NPC 对话。
func (sc *vaultScaffold) open(t *testing.T) {
	t.Helper()
	req := c2s.NewTalkToNpcRequest()
	req.SetNpcId(testVaultNpcID)
	sc.srv.handleNpcTalk(sc.sess, req.Bytes())
	if sc.sess.getOpenedNpc() != sc.vaultNpc {
		t.Fatal("对话后应打开仓库 NPC")
	}
}

func vaultMoveFrame(fromStorage c2s.ItemStorageKind, fromSlot byte, toStorage c2s.ItemStorageKind, toSlot byte) []byte {
	p := c2s.NewItemMoveRequestExtended()
	p.SetFromStorage(fromStorage)
	p.SetFromSlot(fromSlot)
	p.SetToStorage(toStorage)
	p.SetToSlot(toSlot)
	return p.Bytes()
}

func vaultMoneyFrame(direction c2s.VaultMoneyMoveDirection, amount uint32) []byte {
	p := c2s.NewVaultMoveMoneyRequest()
	p.SetDirection(direction)
	p.SetAmount(amount)
	return p.Bytes()
}

// TestVaultOpen 锁定打开序列：窗口（VaultStorage）+ C1 81 金钱 + C1 83 状态；
// 空仓库不下发物品清单。
func TestVaultOpen(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)

	if n := countFrames(sc.rec, 0xC3, 0x30); n != 1 {
		t.Fatalf("应下发 1 帧 C3 30 窗口, got %d", n)
	}
	win := s2c.AsNpcWindowResponse(findFrame(sc.rec, 0xC3, 0x30))
	if win.Window() != s2c.NpcWindow_VaultStorage {
		t.Fatalf("窗口应为 VaultStorage(2), got %d", win.Window())
	}
	// 空仓库：无物品清单。
	if n := countFrames(sc.rec, 0xC2, 0x31); n != 0 {
		t.Fatalf("空仓库不应发物品清单, got %d", n)
	}
	// C1 81 金钱：success + vault(0) + inventory(1000)。
	money := s2c.AsVaultMoneyUpdate(findFrame(sc.rec, 0xC1, 0x81))
	if !money.Success() || money.VaultMoney() != 0 || money.InventoryMoney() != 1000 {
		t.Fatalf("仓库金钱帧异常: success=%v vault=%d inv=%d", money.Success(), money.VaultMoney(), money.InventoryMoney())
	}
	// C1 83 状态：Unprotected。
	state := s2c.AsVaultProtectionInformation(findFrame(sc.rec, 0xC1, 0x83))
	if state.ProtectionState() != s2c.VaultProtectionState_Unprotected {
		t.Fatalf("仓库状态应为 Unprotected(0), got %d", state.ProtectionState())
	}
}

// TestVaultStoreAndRetrieveItem 锁定物品存入/取出：背包槽12 ↔ 仓库槽0。
func TestVaultStoreAndRetrieveItem(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	// 祝福宝石(14,13) 1×1，放背包槽 12。
	gem := &item.Item{Group: 14, Number: 13, Level: 0, Durability: 1}
	inv := sc.srv.ensureInventory(sc.c)
	si := sc.srv.newSlottedItem(gem)
	if !inv.AddToSlot(12, si) {
		t.Fatal("放入背包槽 12 失败")
	}
	sc.open(t)
	sc.rec.frames = nil

	// 存入：inventory 槽12 → vault 槽0。
	f := vaultMoveFrame(c2s.ItemStorageKind_Inventory, 12, c2s.ItemStorageKind_Vault, 0)
	sc.srv.handleItemMove(sc.sess, f)
	acc := sc.sess.getAccount()
	if acc.Vault.Items.GetItem(0) == nil {
		t.Fatal("仓库槽 0 应有物品")
	}
	if sc.c.Inventory.GetItem(12) != nil {
		t.Fatal("背包槽 12 应已空")
	}
	if frame := findFrame(sc.rec, 0xC3, 0x24); frame[3] != byte(s2c.ItemStorageKind_Vault) || frame[4] != 0 {
		t.Fatalf("C3 24 响应目标应为 vault 槽0: %X", frame)
	}

	// 取出：vault 槽0 → inventory 槽12。
	f = vaultMoveFrame(c2s.ItemStorageKind_Vault, 0, c2s.ItemStorageKind_Inventory, 12)
	sc.srv.handleItemMove(sc.sess, f)
	if acc.Vault.Items.GetItem(0) != nil {
		t.Fatal("仓库槽 0 应已空")
	}
	if sc.c.Inventory.GetItem(12) == nil {
		t.Fatal("背包槽 12 应有物品")
	}
}

// TestVaultMoneyDepositAndWithdraw 锁定金钱存取与超额拒绝。
func TestVaultMoneyDepositAndWithdraw(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)
	sc.rec.frames = nil
	acc := sc.sess.getAccount()

	// 存入 400。
	sc.srv.handleVaultMoney(sc.sess, vaultMoneyFrame(c2s.VaultMoneyMoveDirection_InventoryToVault, 400))
	if sc.c.Stats.Money != 600 || acc.Vault.Money != 400 {
		t.Fatalf("存入后应 inv=600 vault=400, got inv=%d vault=%d", sc.c.Stats.Money, acc.Vault.Money)
	}
	upd := s2c.AsVaultMoneyUpdate(findFrame(sc.rec, 0xC1, 0x81))
	if !upd.Success() || upd.InventoryMoney() != 600 || upd.VaultMoney() != 400 {
		t.Fatal("存入后 C1 81 帧值异常")
	}

	// 取出 300。
	sc.srv.handleVaultMoney(sc.sess, vaultMoneyFrame(c2s.VaultMoneyMoveDirection_VaultToInventory, 300))
	if sc.c.Stats.Money != 900 || acc.Vault.Money != 100 {
		t.Fatalf("取出后应 inv=900 vault=100, got inv=%d vault=%d", sc.c.Stats.Money, acc.Vault.Money)
	}

	// 超额取出 500（vault 仅 100）→ 失败帧，金钱不变。
	nBefore := len(sc.rec.frames)
	sc.srv.handleVaultMoney(sc.sess, vaultMoneyFrame(c2s.VaultMoneyMoveDirection_VaultToInventory, 500))
	if sc.c.Stats.Money != 900 || acc.Vault.Money != 100 {
		t.Fatal("超额取出被拒后金钱不应变化")
	}
	failFrame := sc.rec.frames[nBefore]
	if s2c.AsVaultMoneyUpdate(failFrame).Success() {
		t.Fatal("超额取出应回 success=false")
	}
}

// TestVaultClose 锁定关闭：清对话上下文 + 回 C1 82。
func TestVaultClose(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)
	sc.rec.frames = nil

	sc.srv.handleVaultClose(sc.sess, c2s.NewVaultClosed().Bytes())
	if sc.sess.getOpenedNpc() != nil {
		t.Fatal("关闭后 openedNpc 应清空")
	}
	// C1 82 为 3B 帧（countFrames 仅认 ≥4B），直接逐字节断言。
	found := false
	for _, f := range sc.rec.frames {
		if len(f) == 3 && f[0] == 0xC1 && f[2] == 0x82 {
			found = true
		}
	}
	if !found {
		t.Fatal("应回 1 帧 C1 82（3B）")
	}
}

// vaultMoveSucceeded 判断 recorder 中是否出现成功的 C3 24（f[3] 为目标存储而非 FF 子码）。
func vaultMoveSucceeded(rec *packetRecorder) bool {
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC3 && frameCode(f) == 0x24 && f[3] != 0xFF {
			return true
		}
	}
	return false
}

// TestVaultMoveRejectedWithoutOpen 未打开仓库时搬运 → 失败帧。
func TestVaultMoveRejectedWithoutOpen(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	f := vaultMoveFrame(c2s.ItemStorageKind_Inventory, 12, c2s.ItemStorageKind_Vault, 0)
	sc.srv.handleItemMove(sc.sess, f)
	if vaultMoveSucceeded(sc.rec) {
		t.Fatal("未开仓库不应有成功帧")
	}
}

// TestVaultStoreOccupiedSlot 目标槽已有物品（跨容器不堆叠）→ 拒绝。
func TestVaultStoreOccupiedSlot(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)
	acc := sc.sess.getAccount()
	first := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 13, Durability: 1})
	if !acc.Vault.Items.AddToSlot(0, first) {
		t.Fatal("预置仓库槽 0 失败")
	}
	// 背包槽 12 放第二件，往已占用的仓库槽 0 存。
	gem := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 13, Durability: 1})
	if !sc.srv.ensureInventory(sc.c).AddToSlot(12, gem) {
		t.Fatal("放入背包槽 12 失败")
	}
	sc.rec.frames = nil
	f := vaultMoveFrame(c2s.ItemStorageKind_Inventory, 12, c2s.ItemStorageKind_Vault, 0)
	sc.srv.handleItemMove(sc.sess, f)
	if vaultMoveSucceeded(sc.rec) {
		t.Fatal("目标槽占用应拒绝成功帧")
	}
	if sc.c.Inventory.GetItem(12) == nil {
		t.Fatal("被拒后背包物品应保留")
	}
}

// TestVaultMoveWithin 仓库内部移动：槽0 → 槽8 成功落位，槽0 清空。
func TestVaultMoveWithin(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)
	acc := sc.sess.getAccount()
	gem := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 13, Durability: 1})
	if !acc.Vault.Items.AddToSlot(0, gem) {
		t.Fatal("预置仓库槽 0 失败")
	}
	sc.rec.frames = nil

	f := vaultMoveFrame(c2s.ItemStorageKind_Vault, 0, c2s.ItemStorageKind_Vault, 8)
	sc.srv.handleItemMove(sc.sess, f)
	if !vaultMoveSucceeded(sc.rec) {
		t.Fatal("仓内移动应成功")
	}
	if acc.Vault.Items.GetItem(0) != nil {
		t.Fatal("源槽 0 应清空")
	}
	if acc.Vault.Items.GetItem(8) == nil {
		t.Fatal("目标槽 8 应有物品")
	}
	frame := findFrame(sc.rec, 0xC3, 0x24)
	if frame[3] != byte(s2c.ItemStorageKind_Vault) || frame[4] != 8 {
		t.Fatalf("C3 24 响应目标应为 vault 槽8: %X", frame)
	}
}

// TestVaultMoveWithinOccupied 仓内移动目标格有物（同存储不堆叠）→ 拒绝且物品保留。
func TestVaultMoveWithinOccupied(t *testing.T) {
	sc := newVaultScaffold(t, 1000)
	sc.open(t)
	acc := sc.sess.getAccount()
	gemA := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 13, Durability: 1})
	gemB := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 13, Durability: 1})
	if !acc.Vault.Items.AddToSlot(0, gemA) || !acc.Vault.Items.AddToSlot(8, gemB) {
		t.Fatal("预置仓库槽 0/8 失败")
	}
	sc.rec.frames = nil

	f := vaultMoveFrame(c2s.ItemStorageKind_Vault, 8, c2s.ItemStorageKind_Vault, 0)
	sc.srv.handleItemMove(sc.sess, f)
	if vaultMoveSucceeded(sc.rec) {
		t.Fatal("目标槽占用应拒绝成功帧")
	}
	if acc.Vault.Items.GetItem(8) == nil || acc.Vault.Items.GetItem(0) == nil {
		t.Fatal("被拒后两槽物品均应保留")
	}
}
