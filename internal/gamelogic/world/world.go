package world

import (
	"sync"

	"mugo/internal/util"
)

// ConstantPlayerID 是哨兵值（原版 ViewExtensions.ConstantPlayerId = 0x200，
// F1 00 等无实际对象上下文的封包用它占位）。完整 ID 布局：
// 掉落物 0..0x1FF（dropIdGenerator）、对象（玩家+怪物/NPC）0x201..0x7FFF（objectIdGenerator）。
// 0x8000 及以上**不是** ID——那是出站视野包的"出生位"旗标位。
const ConstantPlayerID uint16 = 0x200

// World 持有所有地图与玩家 ID 分配（M6 单进程内存态）。
type World struct {
	mu     sync.Mutex
	maps   map[uint16]*Map
	nextID uint16
	usedID map[uint16]struct{}
	rng    *util.Rand // 战斗掷点（固定种子可复现；T2-2）
}

// New 创建空世界（对象 ID 游标从对象池起点 0x201 起）。
func New() *World {
	return &World{
		maps:   make(map[uint16]*Map),
		nextID: 0x201,
		usedID: make(map[uint16]struct{}),
		rng:    util.NewRand(0xC01DB4E),
	}
}

// RNG 返回战斗掷点随机源（固定种子；命中/伤害与原版同构可回放）。
func (w *World) RNG() *util.Rand { return w.rng }

// RngInt 是线程安全的全局随机区间取值（含 min、不含 max）。
// util.Rand 非并发安全，跨 goroutine 使用时必须经此方法加锁。
func (w *World) RngInt(minValue, maxValue int) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rng.Next(minValue, maxValue)
}

// Map 返回编号对应的地图（惰性创建）。
func (w *World) Map(number uint16) *Map {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.maps[number]
	if !ok {
		m = newMap(number)
		w.maps[number] = m
	}
	return m
}

// AllocID 分配一个未占用的**对象 ID**（玩家进图时分配，对应原版 GameMap.AddAsync
// 里 player 分支的 objectIdGenerator.GenerateId()；怪物/NPC 同池——Spawner 的 ID
// 经 ReserveIDs 预占）。原版为每地图一个生成器（0x201..0x7FFF，耗尽即异常，不回绕）；
// 这里为全局单池 + 回收复用（偏差登记：单进程内存态，跨地图唯一性与客户端可见性等价）。
func (w *World) AllocID() uint16 {
	w.mu.Lock()
	defer w.mu.Unlock()
	const minID = 0x201
	const maxID = 0x7FFF
	for i := minID; i <= maxID; i++ {
		id := w.nextID
		w.nextID++
		if w.nextID > maxID {
			w.nextID = minID
		}
		if _, taken := w.usedID[id]; !taken {
			w.usedID[id] = struct{}{}
			return id
		}
	}
	return 0
}

// ReserveIDs 预占用一批对象 ID（Spawner 启动期生成的怪物/NPC 实例注册进同一池，
// 保证玩家 ID 不与其冲突——对应原版"怪物先建、同池分配"的效果）。
func (w *World) ReserveIDs(ids []uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, id := range ids {
		w.usedID[id] = struct{}{}
	}
}

// FreeID 释放玩家 ID（连接结束时调用）。
func (w *World) FreeID(id uint16) {
	if id == 0 {
		return
	}
	w.mu.Lock()
	delete(w.usedID, id)
	w.mu.Unlock()
}

// SetTerrain 为指定地图挂载解析后的地形（碰撞判定；T1-1）。
func (w *World) SetTerrain(number uint16, t *Terrain) {
	w.Map(number).SetTerrain(t)
}

// PlayersInRange 返回指定坐标 InfoRange 内的玩家（怪物 AI 索敌用；T2-1）。
func (w *World) PlayersInRange(mapNumber uint16, x, y byte) []*Player {
	return w.Map(mapNumber).PlayersInRangeFor(x, y)
}

// WalkableForAI 对应原版 CanWalkOn：AIgrid==1 → 可走且非安全区（怪物移动可行域）。
func (w *World) WalkableForAI(mapNumber uint16, x, y byte) bool {
	return w.Map(mapNumber).Walkable(x, y) && !w.Map(mapNumber).Safezone(x, y)
}

// SafezoneAt 报告坐标是否安全区（怪物 AI 的目标守卫）。
func (w *World) SafezoneAt(mapNumber uint16, x, y byte) bool {
	return w.Map(mapNumber).Safezone(x, y)
}

// PlayerCount 返回全部地图的在场玩家总数（GS 上报 CurrentConnections 用，
// 对应原版 GameContext.PlayerCount = _playerList.Count）。
func (w *World) PlayerCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	total := 0
	for _, m := range w.maps {
		m.mu.Lock()
		total += len(m.players)
		m.mu.Unlock()
	}
	return total
}
