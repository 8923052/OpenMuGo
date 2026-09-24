package gameserver

import (
	"sync"

	"mugo/internal/api"
)

// Container 是按 ServerID 索引的 GS 实例容器（doc/14 P3 = S-2，对应原版
// GameServerContainer 的 IDictionary<int, IGameServer> + IGameServerInstanceManager）。
// 并发安全：观察者/管理端与启动流程可能在不同 goroutine 增删。
type Container struct {
	mu      sync.RWMutex
	byID    map[int]*Server
	order   []int // 保持注册顺序（原版 List/Dictionary 并存：_servers + _gameServers）
	servers []api.ManageableServer
}

// NewContainer 创建空容器。
func NewContainer() *Container {
	return &Container{byID: make(map[int]*Server)}
}

// Add 注册一个 GS；同 ID 重复注册返回错误（原版 Dictionary 同样会炸）。
func (c *Container) Add(id int, s *Server) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byID[id]; ok {
		return errDuplicateServer(id)
	}
	c.byID[id] = s
	c.order = append(c.order, id)
	c.servers = append(c.servers, s)
	return nil
}

// Remove 按 ID 移除（原版 RemoveGameServerAsync 的容器侧职责）。
func (c *Container) Remove(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byID[id]; !ok {
		return false
	}
	delete(c.byID, id)
	for i, v := range c.order {
		if v == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	for i, s := range c.servers {
		if s.Id() == id {
			c.servers = append(c.servers[:i], c.servers[i+1:]...)
			break
		}
	}
	return true
}

// Get 按 ID 取实例。
func (c *Container) Get(id int) (*Server, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byID[id]
	return s, ok
}

// All 按注册顺序返回全部实例。
func (c *Container) All() []*Server {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Server, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.byID[id])
	}
	return out
}

// ManageableServers 按注册顺序返回契约层视图（喂 LocalServerProvider）。
func (c *Container) ManageableServers() []api.ManageableServer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]api.ManageableServer, len(c.servers))
	copy(out, c.servers)
	return out
}

// StartAll 按注册顺序启动全部 GS（原版 foreach _gameServers.Values StartAsync）。
func (c *Container) StartAll(ctx startCtx) error {
	for _, s := range c.All() {
		if err := s.StartAsync(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ShutdownAll 按注册顺序停全部 GS。
func (c *Container) ShutdownAll(ctx startCtx) error {
	for _, s := range c.All() {
		if err := s.ShutdownAsync(ctx); err != nil {
			return err
		}
	}
	return nil
}
