package remote

// vault_view.go —— 仓库 Vault 出站实现：
//   - C1 81 VaultMoneyUpdate（UpdateVaultMoneyPlugIn，12B）；
//   - C1 82 VaultClosed（CloseVaultPlugIn，3B）；
//   - C1 83 VaultProtectionInformation（UpdateVaultStatePlugIn，4B）。

import s2c "mugo/internal/proto/s2c"

// ShowVaultMoneyUpdate 实现 action.PlayerView（C1 81）。
// success=false 时按原版 UpdateVaultMoneyPlugIn 金额字段写 0。
func (v *PlayerView) ShowVaultMoneyUpdate(success bool, vaultMoney, inventoryMoney uint32) error {
	p := s2c.NewVaultMoneyUpdate()
	p.SetSuccess(success)
	if success {
		p.SetVaultMoney(vaultMoney)
		p.SetInventoryMoney(inventoryMoney)
	}
	return v.send.Send(p.Bytes())
}

// ShowVaultClosed 实现 action.PlayerView（C1 82）。
func (v *PlayerView) ShowVaultClosed() error {
	p := s2c.NewVaultClosed()
	return v.send.Send(p.Bytes())
}

// ShowVaultProtectionState 实现 action.PlayerView（C1 83）。
func (v *PlayerView) ShowVaultProtectionState(state byte) error {
	p := s2c.NewVaultProtectionInformation()
	p.SetProtectionState(s2c.VaultProtectionState(state))
	return v.send.Send(p.Bytes())
}
