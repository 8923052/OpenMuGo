// Package persistence 是持久化抽象层，对应 OpenMU 的 MUnique.OpenMU.Persistence。
// 当前只有内存实现；接入数据库时替换 Store 实现即可，业务侧不感知。
package persistence

import (
	"errors"
	"sync"

	"mugo/internal/gamelogic/entity"
)

// ErrInvalidCredentials 表示用户名或密码错误。
var ErrInvalidCredentials = errors.New("persistence: invalid credentials")

// Store 是账号存储抽象。
type Store interface {
	Authenticate(username, password string) (*entity.Account, error)
}

// MemoryStore 是线程安全的内存账号存储（M4 默认实现）。
type MemoryStore struct {
	mu       sync.RWMutex
	accounts map[string]*entity.Account
}

// NewMemoryStore 创建空内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{accounts: make(map[string]*entity.Account)}
}

// Add 注册账号（重复名覆盖）。
func (s *MemoryStore) Add(a *entity.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.Name] = a
}

// Authenticate 校验用户名密码，返回账号副本指针。
func (s *MemoryStore) Authenticate(username, password string) (*entity.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[username]
	if !ok || a.Password != password {
		return nil, ErrInvalidCredentials
	}
	return a, nil
}
