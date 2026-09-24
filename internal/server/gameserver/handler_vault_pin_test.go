package gameserver

// handler_vault_pin_test.go —— C1 83 仓库 PIN 全族（00 解锁 / 01 设 PIN / 02 撤销）
// 与上锁时的取物禁令（doc/17 S-3 批次 4，结清 doc/15 的 TRIM-05 剩余半）。
// 出站统一是 C1 83 VaultProtectionInformation；原版的三条怪癖按原样锁定在断言里。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

const vaultTestPassword = "secret"

// pinScaffold 打开仓库对话，并给账号一个可校验的密码。
func pinScaffold(t *testing.T) *vaultScaffold {
	t.Helper()
	sc := newVaultScaffold(t, 1000)
	sc.sess.getAccount().Password = vaultTestPassword
	sc.open(t)
	return sc
}

// lastVaultState 取最后一条 C1 83 出站的锁状态。
func lastVaultState(t *testing.T, sc *vaultScaffold) byte {
	t.Helper()
	f := findFrame(sc.rec, 0xC1, 0x83)
	if f == nil {
		t.Fatal("未下发 C1 83 VaultProtectionInformation")
	}
	return byte(s2c.AsVaultProtectionInformation(f).ProtectionState())
}

func (sc *vaultScaffold) setPin(pin uint16, password string) {
	r := c2s.NewSetVaultPin()
	r.SetPin(pin)
	r.SetPassword(password)
	sc.srv.handleVaultLockSub(sc.sess, c2s.SetVaultPinSubCode, r.Bytes())
}

func (sc *vaultScaffold) unlock(pin uint16) {
	r := c2s.NewUnlockVault()
	r.SetPin(pin)
	sc.srv.handleVaultLockSub(sc.sess, c2s.UnlockVaultSubCode, r.Bytes())
}

func (sc *vaultScaffold) removePin(password string) {
	r := c2s.NewRemoveVaultPin()
	r.SetPassword(password)
	sc.srv.handleVaultLockSub(sc.sess, c2s.RemoveVaultPinSubCode, r.Bytes())
}

// TestVaultSetPinWithCorrectPassword 验证未上锁时设 PIN：写入账号、回 Unlocked(12)，
// 且**本次会话仍然不锁**（原版 SetVaultPinAction 不置 IsVaultLocked，下次登录才锁）。
func TestVaultSetPinWithCorrectPassword(t *testing.T) {
	sc := pinScaffold(t)

	sc.setPin(1234, vaultTestPassword)

	if got := sc.sess.getAccount().VaultPassword; got != "1234" {
		t.Fatalf("PIN=%q，期望 \"1234\"", got)
	}
	if got := lastVaultState(t, sc); got != action.VaultStateUnlocked {
		t.Fatalf("状态=%d，期望 Unlocked(%d)", got, action.VaultStateUnlocked)
	}
	if sc.sess.vaultLocked {
		t.Fatal("原版设 PIN 后本会话不立即上锁")
	}
}

// TestVaultSetPinRejectedWhenWrongPassword 验证原版的错标：账号密码不对时回的也是
// SetPinFailedBecauseLock(11)（不是"密码错误"），且 PIN 未被写入。
func TestVaultSetPinRejectedWhenWrongPassword(t *testing.T) {
	sc := pinScaffold(t)

	sc.setPin(1234, "wrong-once")

	if got := lastVaultState(t, sc); got != action.VaultStateSetPinFailedBecauseLock {
		t.Fatalf("状态=%d，期望 SetPinFailedBecauseLock(%d)", got, action.VaultStateSetPinFailedBecauseLock)
	}
	if sc.sess.getAccount().VaultPassword != "" {
		t.Fatal("密码不对不应写入 PIN")
	}
}

// TestVaultSetPinRejectedWhileLocked 验证已有 PIN 且仍上锁时不能直接改（须先撤销）。
func TestVaultSetPinRejectedWhileLocked(t *testing.T) {
	sc := pinScaffold(t)
	acc := sc.sess.getAccount()
	acc.VaultPassword = "9999"
	sc.sess.vaultLocked = true

	sc.setPin(1234, vaultTestPassword)

	if got := lastVaultState(t, sc); got != action.VaultStateSetPinFailedBecauseLock {
		t.Fatalf("状态=%d，期望 SetPinFailedBecauseLock", got)
	}
	if acc.VaultPassword != "9999" {
		t.Fatalf("上锁中不应改 PIN，现在=%q", acc.VaultPassword)
	}
}

// TestVaultUnlockWrongThenRight 验证 83 00：错 PIN 回 UnlockFailedByWrongPin(10) 且仍锁，
// 对 PIN 回 Unlocked(12) 并解开会话锁。
func TestVaultUnlockWrongThenRight(t *testing.T) {
	sc := pinScaffold(t)
	acc := sc.sess.getAccount()
	acc.VaultPassword = "1234"
	sc.sess.vaultLocked = true

	sc.unlock(4321)
	if got := lastVaultState(t, sc); got != action.VaultStateUnlockFailedByWrongPin {
		t.Fatalf("错 PIN 状态=%d，期望 10", got)
	}
	if !sc.sess.vaultLocked {
		t.Fatal("错 PIN 不应解锁")
	}

	sc.unlock(1234)
	if got := lastVaultState(t, sc); got != action.VaultStateUnlocked {
		t.Fatalf("对 PIN 状态=%d，期望 12", got)
	}
	if sc.sess.vaultLocked {
		t.Fatal("对 PIN 后应解开本会话的锁")
	}
}

// TestVaultRemovePin 验证 83 02：密码对 → 清 PIN + 解锁 + 重新计算状态（Unprotected(0)）；
// 密码错 → RemovePinFailedByWrongPassword(13) 且 PIN 保留。
func TestVaultRemovePin(t *testing.T) {
	sc := pinScaffold(t)
	acc := sc.sess.getAccount()
	acc.VaultPassword = "1234"
	sc.sess.vaultLocked = true

	sc.removePin("nope")
	if got := lastVaultState(t, sc); got != action.VaultStateRemovePinFailedByPassword {
		t.Fatalf("错密码状态=%d，期望 13", got)
	}
	if acc.VaultPassword != "1234" || !sc.sess.vaultLocked {
		t.Fatal("错密码不应清 PIN")
	}

	sc.removePin(vaultTestPassword)
	if got := lastVaultState(t, sc); got != action.VaultStateUnprotected {
		t.Fatalf("撤销后状态=%d，期望 Unprotected(0)", got)
	}
	if acc.VaultPassword != "" || sc.sess.vaultLocked {
		t.Fatalf("撤销后 PIN=%q locked=%v", acc.VaultPassword, sc.sess.vaultLocked)
	}
}

// TestVaultLockedBlocksWithdraw 验证上锁时"仓库→背包"被拒（对照 MoveItemAction.cs:241-245：
// 只发蓝字 TheVaultIsLocked、不发移动失败包），解锁后同一笔搬运才成功。
func TestVaultLockedBlocksWithdraw(t *testing.T) {
	sc := pinScaffold(t)
	acc := sc.sess.getAccount()
	acc.VaultPassword = "1234"
	sc.sess.vaultLocked = true
	si := sc.srv.newSlottedItem(&item.Item{Group: 14, Number: 1, Level: 0, Durability: 5})
	if !acc.Vault.Items.AddToSlot(0, si) {
		t.Fatal("前置：物品放进仓库失败")
	}

	sc.srv.handleItemMove(sc.sess, vaultMoveFrame(c2s.ItemStorageKind_Vault, 0,
		c2s.ItemStorageKind_Inventory, 12))

	if acc.Vault.Items.GetItem(0) == nil {
		t.Fatal("上锁时不应把物品取出仓库")
	}
	if findFrame(sc.rec, 0xC1, 0x0D) == nil {
		t.Fatal("上锁取物应发一条蓝字（TheVaultIsLocked）")
	}
	if findFrameSub(sc.rec, 0xC3, 0x24, 0xFF) != nil {
		t.Fatal("原版这条分支不发移动失败包（24 FF）")
	}

	sc.unlock(1234)
	sc.srv.handleItemMove(sc.sess, vaultMoveFrame(c2s.ItemStorageKind_Vault, 0,
		c2s.ItemStorageKind_Inventory, 12))
	if acc.Vault.Items.GetItem(0) != nil {
		t.Fatal("解锁后同一笔取物应成功")
	}
	if sc.sess.getWorldPlayer() != nil && sc.srv.ensureInventory(sc.sess.getSelected()).GetItem(12) == nil {
		t.Fatal("取出的物品应落在背包第 12 格")
	}
}

// TestVaultLockedAtSelectCharacter 验证锁的来源是账号 PIN：设过 PIN 的账号在选角那一刻
// 会话即为上锁（对照 Player.cs:289 在 Account 赋值时算 IsVaultLocked）。
func TestVaultLockedAtSelectCharacter(t *testing.T) {
	srv, sess, _, _, _ := newExpScaffold(t, 20)
	acc := sess.getAccount()
	acc.VaultPassword = "4321"
	acc.Characters = []entity.Character{{
		Name: "leveler", Slot: 0, Level: 20, ClassNumber: 4,
		MapNumber: 0, X: 139, Y: 127, AppearanceExt: make([]byte, 27),
	}}
	sess.setSelected(nil)
	sess.setWorldPlayer(nil)
	sess.setState(entity.StateAuthenticated)

	req := c2s.NewSelectCharacter()
	req.SetName("leveler")
	srv.handleSelectCharacter(sess, req.Bytes())

	if !sess.vaultLocked {
		t.Fatal("账号设有 PIN 时，选角后会话应为上锁状态")
	}
}

// TestVaultUnlockedAtSelectCharacterWithoutPin 验证没设 PIN 的账号选角后不受锁。
func TestVaultUnlockedAtSelectCharacterWithoutPin(t *testing.T) {
	srv, sess, _, _, _ := newExpScaffold(t, 20)
	acc := sess.getAccount()
	acc.Characters = []entity.Character{{
		Name: "leveler", Slot: 0, Level: 20, ClassNumber: 4,
		MapNumber: 0, X: 139, Y: 127, AppearanceExt: make([]byte, 27),
	}}
	sess.setSelected(nil)
	sess.setWorldPlayer(nil)
	sess.setState(entity.StateAuthenticated)

	req := c2s.NewSelectCharacter()
	req.SetName("leveler")
	srv.handleSelectCharacter(sess, req.Bytes())

	if sess.vaultLocked {
		t.Fatal("未设 PIN 的账号不应上锁")
	}
}
