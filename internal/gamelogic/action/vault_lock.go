package action

// vault_lock.go —— 仓库 PIN 的三个动作（对照 GameLogic/PlayerActions/Vault 的
// SetVaultPinAction / UnlockVaultAction / RemoveVaultPinAction）。
//
// 原版的三条怪癖一并照抄（不改）：
//   - 设 PIN 的前置是"**当前没上锁**"，所以已有 PIN 的账号必须先撤销才能改；
//   - 设 PIN 时**账号密码不对**也回 `SetPinFailedBecauseLock`（不是"密码错"）；
//   - 设 PIN 成功后 `IsVaultLocked` 保持 false —— 本次会话仍然开着，锁在下次登录生效。

import "mugo/internal/gamelogic/entity"

// 出站状态字节（与 s2c.VaultProtectionState_* 逐值一致；转换在 view/remote）。
const (
	VaultStateUnprotected               byte = 0
	VaultStateLocked                    byte = 1
	VaultStateUnlockFailedByWrongPin    byte = 10
	VaultStateSetPinFailedBecauseLock   byte = 11
	VaultStateUnlocked                  byte = 12
	VaultStateRemovePinFailedByPassword byte = 13
)

// VaultLockChange 是一次 PIN 动作的结果：要回给客户端的状态字节 + 会话锁的新值
// （nil 表示不改会话锁，对照原版只在解锁/撤销时写 IsVaultLocked）。
type VaultLockChange struct {
	State      byte
	VaultIsSet bool
	NowLocked  *bool
}

// VaultProtectionState 算当前该报的锁状态（对照 UpdateVaultStatePlugIn.GetVaultState）：
// 锁着 → Locked；没设 PIN → Unprotected；设了 PIN 但本次会话已解 → Unlocked。
func VaultProtectionState(acc *entity.Account, locked bool) byte {
	switch {
	case locked:
		return VaultStateLocked
	case acc == nil || acc.VaultPassword == "":
		return VaultStateUnprotected
	default:
		return VaultStateUnlocked
	}
}

// VaultSetPin 对照 SetVaultPinAction.SetPinAsync：上锁中或无账号 → 直接失败；
// 账号密码对 → 写入 PIN 并回 Unlocked；密码不对 → 也回 SetPinFailedBecauseLock。
func VaultSetPin(acc *entity.Account, locked bool, pin string, accountPassword string) VaultLockChange {
	if acc == nil || locked {
		return VaultLockChange{State: VaultStateSetPinFailedBecauseLock}
	}
	if !vaultPasswordMatches(acc, accountPassword) {
		return VaultLockChange{State: VaultStateSetPinFailedBecauseLock}
	}
	acc.VaultPassword = pin
	return VaultLockChange{State: VaultStateUnlocked, VaultIsSet: true}
}

// VaultUnlock 对照 UnlockVaultAction.UnlockVaultAsync：PIN 对才解开会话锁。
func VaultUnlock(acc *entity.Account, pin string) VaultLockChange {
	locked := false
	if acc != nil && acc.VaultPassword != pin {
		return VaultLockChange{State: VaultStateUnlockFailedByWrongPin}
	}
	return VaultLockChange{State: VaultStateUnlocked, NowLocked: &locked}
}

// VaultRemovePin 对照 RemoveVaultPinAction.RemovePinAsync：密码对则清空 PIN 并解锁，
// 出站走"重新计算状态"（此时必为 Unprotected）；密码不对回专门的失败状态。
func VaultRemovePin(acc *entity.Account, accountPassword string) VaultLockChange {
	if !vaultPasswordMatches(acc, accountPassword) {
		return VaultLockChange{State: VaultStateRemovePinFailedByPassword}
	}
	acc.VaultPassword = ""
	locked := false
	return VaultLockChange{State: VaultStateUnprotected, NowLocked: &locked}
}

// vaultPasswordMatches 校验账号密码（原版用 BCrypt.Verify；本仓账号密码目前是内存明文，
// 与登录用的 MemoryStore.Authenticate 同一口径，接 DB 时两处一起换成哈希）。
func vaultPasswordMatches(acc *entity.Account, accountPassword string) bool {
	return acc != nil && acc.Password != "" && acc.Password == accountPassword
}
