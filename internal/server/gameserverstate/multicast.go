// Package gameserverstate 实现 GS 状态观察者的多播与记忆层（doc/14 P3，对应 OpenMU Startup/）。
//
// 结构对应原版（Startup/MulticastConnectionServerStateObserver.cs + ConnectServerContainer.cs）：
//
//	MulticastConnectionServerStateObserver
//	  ├─ MemorizingObserver（恒为 _observers[0]）
//	  └─ List<IGameServerStateObserver>（每个 ConnectServer 一个）
//
// 记忆观察者的意义（不能省）：让**后加入的观察者**（后启动的 CS）也能拿到既有 GS 的注册——
// PullRegistrations 重放记忆内容。它是 P4 启动顺序（CS 先起、GS 后起亦可）的配套件。
package gameserverstate

import (
	"sync"

	"mugo/internal/api"
)

// MulticastObserver 是 api.GameServerStateObserver 的多播实现：
// 3 个接口方法全部循环转发给每个观察者，第 0 位恒为记忆观察者。
type MulticastObserver struct {
	mu        sync.RWMutex
	memo      *memorizing
	observers []api.GameServerStateObserver
}

// NewMulticastObserver 创建多播观察者（记忆观察者已内置在首位）。
func NewMulticastObserver() *MulticastObserver {
	m := &memorizing{infos: make(map[uint16]memoEntry)}
	return &MulticastObserver{memo: m, observers: []api.GameServerStateObserver{m}}
}

// AddObserver 追加一个观察者（原版 AddObserver）。
func (m *MulticastObserver) AddObserver(o api.GameServerStateObserver) {
	m.mu.Lock()
	m.observers = append(m.observers, o)
	m.mu.Unlock()
}

// PullRegistrations 把记忆中的全部注册重放给新观察者（原版 PullRegistrations）。
func (m *MulticastObserver) PullRegistrations(o api.GameServerStateObserver) {
	for _, e := range m.memo.all() {
		o.RegisterGameServer(e.info, e.endPoint)
	}
}

// RegisterGameServer 转发注册到全部观察者（含记忆）。
func (m *MulticastObserver) RegisterGameServer(gameServer api.ServerInfo, publicEndPoint api.EndPoint) {
	m.mu.RLock()
	observers := append([]api.GameServerStateObserver(nil), m.observers...)
	m.mu.RUnlock()
	for _, o := range observers {
		o.RegisterGameServer(gameServer, publicEndPoint)
	}
}

// UnregisterGameServer 转发注销到全部观察者（含记忆）。
func (m *MulticastObserver) UnregisterGameServer(gameServerId uint16) {
	m.mu.RLock()
	observers := append([]api.GameServerStateObserver(nil), m.observers...)
	m.mu.RUnlock()
	for _, o := range observers {
		o.UnregisterGameServer(gameServerId)
	}
}

// CurrentConnectionsChanged 转发连接数变化到全部观察者（含记忆）。
func (m *MulticastObserver) CurrentConnectionsChanged(serverId uint16, currentConnections int) {
	m.mu.RLock()
	observers := append([]api.GameServerStateObserver(nil), m.observers...)
	m.mu.RUnlock()
	for _, o := range observers {
		o.CurrentConnectionsChanged(serverId, currentConnections)
	}
}

// memoEntry 是记忆观察者的一条记录。
type memoEntry struct {
	info     api.ServerInfo
	endPoint api.EndPoint
}

// memorizing 对应原版 MemorizingObserver（嵌套私有类）：
// 记住注册表（含端点）并在连接数变化时就地更新 ServerInfo.CurrentConnections
// ——记忆条目的 ServerInfo 是值语义，更新要写回 map（原版 tuple 是引用类型，语义等价）。
type memorizing struct {
	mu    sync.Mutex
	infos map[uint16]memoEntry
}

func (m *memorizing) RegisterGameServer(gameServer api.ServerInfo, publicEndPoint api.EndPoint) {
	m.mu.Lock()
	// TryAdd 语义：已存在不覆盖（原版同）。重复注册先 Unregister 是 CS 侧行为（见 ConnectServer.RegisterGameServer）。
	if _, ok := m.infos[gameServer.Id]; !ok {
		m.infos[gameServer.Id] = memoEntry{info: gameServer, endPoint: publicEndPoint}
	}
	m.mu.Unlock()
}

func (m *memorizing) UnregisterGameServer(gameServerId uint16) {
	m.mu.Lock()
	delete(m.infos, gameServerId)
	m.mu.Unlock()
}

func (m *memorizing) CurrentConnectionsChanged(serverId uint16, currentConnections int) {
	m.mu.Lock()
	if e, ok := m.infos[serverId]; ok {
		e.info = e.info.WithCurrentConnections(currentConnections)
		m.infos[serverId] = e
	}
	m.mu.Unlock()
}

func (m *memorizing) all() []memoEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]memoEntry, 0, len(m.infos))
	for _, e := range m.infos {
		out = append(out, e)
	}
	return out
}
