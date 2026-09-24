// Package loginserver 对应 OpenMU 的 MUnique.OpenMU.LoginServer：跟踪账号在哪个服务上登录。
// 单进程部署下简化为进程内注册表；将来分布式部署时替换为远端实现。
package loginserver

import (
	"errors"
	"sync"

	"mugo/internal/gamelogic/entity"
)

// ErrAlreadyConnected 表示该账号已在另一连接上登录。
var ErrAlreadyConnected = errors.New("loginserver: account already connected")

// SessionRegistry 跟踪已登录账号，防止重复登录（对应 LoginServer.TryLogin 的进程内简化版）。
//
// 与 OpenMU 的差异：原版按 serverId 记录"账号在哪台服务器上"，本项目当前单 GS
// （serverId 恒为 0），故只记账号名。多 GS 落地时须按 account → serverId 扩表。
type SessionRegistry struct {
	mu    sync.Mutex
	users map[string]struct{}
}

// NewSessionRegistry 创建会话注册表。
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{users: make(map[string]struct{})}
}

// TryLogin 尝试占用账号名；已在线则返回 ErrAlreadyConnected。
// 模板账号（IsTemplate）不占用名额，与 OpenMU 行为一致。
func (r *SessionRegistry) TryLogin(account *entity.Account) error {
	if account.IsTemplate {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[account.Name]; ok {
		return ErrAlreadyConnected
	}
	r.users[account.Name] = struct{}{}
	return nil
}

// Logoff 释放账号名。
func (r *SessionRegistry) Logoff(account *entity.Account) {
	if account.IsTemplate {
		return
	}
	r.mu.Lock()
	delete(r.users, account.Name)
	r.mu.Unlock()
}

// TryLoginAsync 是 api.LoginServer 的实现（按账号名，不经过实体）。
// 与 TryLogin 并存：TryLogin 走实体、保留 IsTemplate 语义；本方法供服务装配层按契约调用。
func (r *SessionRegistry) TryLoginAsync(accountName string, serverId int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[accountName]; ok {
		return false, nil
	}
	r.users[accountName] = struct{}{}
	return true, nil
}

// LogOffAsync 是 api.LoginServer 的实现。
func (r *SessionRegistry) LogOffAsync(accountName string, serverId int) error {
	r.mu.Lock()
	delete(r.users, accountName)
	r.mu.Unlock()
	return nil
}

// GetSnapshotAsync 是 api.LoginServer 的实现；serverId 当前恒为 0（单 GS）。
func (r *SessionRegistry) GetSnapshotAsync() (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.users))
	for name := range r.users {
		out[name] = 0
	}
	return out, nil
}
