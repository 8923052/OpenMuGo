// Package container 实现服务容器与启动顺序（doc/14 P4，对应原版
// Startup/ServerContainerBase 与 Program.cs 的三段启动循环）。
//
// 两段分离（原版语义，不能合并）：
//   - Start（原版 StartAsync）：**建服务，不开端口**；
//   - StartListeners（原版 StartListenersAsync）：才开监听端口。
//
// 这个分离是 P5 社交服务（ChatServer 有独立端口）复用同一套容器的前提，
// 也是 RestartAll（Stop → Start → StartListeners）的组成步骤。
//
// 顺序表达：**不用排序函数**，用显式阶段划分（原版 Program.cs:160-176 就是三段 for）：
// Chat → Game → Connect。停止时逆序。
package container

import (
	"context"
	"log"
	"sync"

	"mugo/internal/api"
)

// Managed 是容器可托管的服务：契约层可管理服务 + 开监听端口的能力。
type Managed interface {
	api.ManageableServer
	// StartListeners 开监听端口（原版 StartListenersAsync）。
	// 已开时实现应幂等（原版注释 "listeners are always started"）。
	StartListeners(ctx context.Context) error
}

// phases 是启动阶段（顺序即语义，不得重排）。
var phases = []api.ServerType{
	api.ServerTypeChatServer,
	api.ServerTypeGameServer,
	api.ServerTypeConnectServer,
}

// Container 托管全部服务，按阶段启动/停止。
type Container struct {
	mu      sync.RWMutex
	servers []Managed
	logger  *log.Logger
}

// New 创建空容器。
func New(logger *log.Logger) *Container {
	if logger == nil {
		logger = log.Default()
	}
	return &Container{logger: logger}
}

// Add 注册服务；同 id 重复注册报错（对应原版 _servers 列表 + 字典双结构的去重语义）。
func (c *Container) Add(s Managed) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.servers {
		if e.Id() == s.Id() {
			return errDuplicate{e}
		}
	}
	c.servers = append(c.servers, s)
	return nil
}

// Remove 移除服务。
func (c *Container) Remove(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.servers {
		if e.Id() == id {
			c.servers = append(c.servers[:i], c.servers[i+1:]...)
			return true
		}
	}
	return false
}

// All 返回注册顺序快照。
func (c *Container) All() []Managed {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Managed, len(c.servers))
	copy(out, c.servers)
	return out
}

// StartAll 按三阶段建全部服务（不开端口）。阶段内按注册顺序；
// 某服务失败即中止（后续阶段不启动），已启动的不回滚——由调用方决定 StopAll。
func (c *Container) StartAll(ctx context.Context) error {
	return c.forEachPhase(func(s Managed) error { return s.StartAsync(ctx) }, "启动")
}

// StartAllListeners 按三阶段开全部监听端口。
func (c *Container) StartAllListeners(ctx context.Context) error {
	return c.forEachPhase(func(s Managed) error { return s.StartListeners(ctx) }, "开监听")
}

// StopAll 逆阶段停止全部服务（Connect → Game → Chat），阶段内逆注册顺序。
func (c *Container) StopAll(ctx context.Context) error {
	c.mu.RLock()
	servers := make([]Managed, len(c.servers))
	copy(servers, c.servers)
	c.mu.RUnlock()

	for i := len(phases) - 1; i >= 0; i-- {
		phase := phases[i]
		for j := len(servers) - 1; j >= 0; j-- {
			if servers[j].Type() == phase {
				c.logger.Printf("container: 停止 %s（id=%d）", phaseName(phase), servers[j].Id())
				if err := servers[j].ShutdownAsync(ctx); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RestartAll 对应原版 RestartAllAsync：Stop → Start → StartListeners。
// 原版的 BeforeStart/DB 事件订阅（S-15）在纯内存态永不触发，不实现（doc/14 明示）。
func (c *Container) RestartAll(ctx context.Context) error {
	if err := c.StopAll(ctx); err != nil {
		return err
	}
	if err := c.StartAll(ctx); err != nil {
		return err
	}
	return c.StartAllListeners(ctx)
}

// forEachPhase 按阶段遍历执行动作。
func (c *Container) forEachPhase(action func(Managed) error, verb string) error {
	c.mu.RLock()
	servers := make([]Managed, len(c.servers))
	copy(servers, c.servers)
	c.mu.RUnlock()

	for _, phase := range phases {
		c.logger.Printf("container: %s阶段 %s", verb, phaseName(phase))
		for _, s := range servers {
			if s.Type() != phase {
				continue
			}
			if err := action(s); err != nil {
				return err
			}
		}
	}
	return nil
}

func phaseName(t api.ServerType) string {
	switch t {
	case api.ServerTypeGameServer:
		return "GameServer"
	case api.ServerTypeConnectServer:
		return "ConnectServer"
	case api.ServerTypeChatServer:
		return "ChatServer"
	default:
		return "Undefined"
	}
}

// errDuplicate 表示同 id 服务重复注册。
type errDuplicate struct{ existing Managed }

func (e errDuplicate) Error() string {
	return "container: 重复注册的 id=" + itoa(e.existing.Id())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
