// Package vault 对应 OpenMU `src/GameServer/MessageHandler/Vault/`（6 个 .cs）。
//
// 入站：移除仓库密码、设置仓库密码、开锁仓库、关闭仓库、仓库锁组包、仓库金币。
//
// 逻辑在 gamelogic/action（PlayerActions/Vault/RemoveVaultPinAction/SetVaultPinAction/
// UnlockVaultAction），出站在 view/remote（Vault 相关 5 个插件 —— 原版这几个直接放在
// RemoteView 根下，未建 Vault 子目录，Go 侧按域归入 view/remote/vault）。
package vault
