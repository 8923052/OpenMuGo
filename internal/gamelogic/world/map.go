package world

import (
	"sync"

	"mugo/internal/util"
)

// Map 是单张地图的玩家注册表（M6 只有 Lorencia，结构上支持多图）。
// 所有公开方法均在 mu 下串行化；返回的玩家切片为快照副本，
// 调用方可在锁外通过 Player.Send 下发，慢连接不会阻塞注册表。
type Map struct {
	number  uint16
	mu      sync.Mutex
	players map[uint16]*Player
	terrain *Terrain // 可空：nil 时全部可走（原版 DefaultTerrain）
	grid    bucketGrid
}

func newMap(number uint16) *Map {
	return &Map{number: number, players: make(map[uint16]*Player)}
}

// SetTerrain 挂载地形（碰撞判定用；重复设置覆盖）。
// Terrain 返回地图地形（未挂载为 nil）。
func (m *Map) Terrain() *Terrain { return m.terrain }

func (m *Map) SetTerrain(t *Terrain) {
	m.mu.Lock()
	m.terrain = t
	m.mu.Unlock()
}

// Walkable 报告坐标是否可走（无地形时恒可走）。
func (m *Map) Walkable(x, y byte) bool {
	m.mu.Lock()
	t := m.terrain
	m.mu.Unlock()
	return t == nil || t.Walkable(x, y)
}

// PlayersInRangeFor 返回 (x,y) 周围 InfoRange 内的玩家（怪物 AI 索敌；T2-1）。
func (m *Map) PlayersInRangeFor(x, y byte) []*Player {
	m.mu.Lock()
	out := m.grid.inRange(x, y)
	m.mu.Unlock()
	return out
}

// RandomCoordinateNear 对应原版 GameMapTerrain.GetRandomCoordinate：在 (x,y) 的
// [±radius] 方框内随机取点，不可走就重掷、最多 20 次。边界钳 0..255（原版不按地图
// 宽高钳），且第 20 次重掷即使可走也回原点——两处怪癖都照抄。
func (m *Map) RandomCoordinateNear(x, y byte, radius byte, rng *util.Rand) (byte, byte) {
	px, py, r := int(x), int(y), int(radius)
	roll := func(lo, hi int) int { return rng.Next(lo, hi+1) }
	loX, hiX := max(0, px-r), min(255, px+r)
	loY, hiY := max(0, py-r), min(255, py+r)
	tx, ty := roll(loX, hiX), roll(loY, hiY)
	i := 0
	for !m.Walkable(byte(tx), byte(ty)) && i < 20 {
		tx, ty = roll(loX, hiX), roll(loY, hiY)
		i++
	}
	if i == 20 {
		return x, y
	}
	return byte(tx), byte(ty)
}

// Number 返回地图编号。
func (m *Map) Number() uint16 { return m.number }

// Enter 让 p 进入地图（坐标取 p.X/p.Y）。返回值 others 为进入前已在**视野范围内**
// 的玩家（AoI 分桶，InfoRange=12 切比雪夫距离；T1-4），供新玩家视野与互相打照面。
// 同一 ID 重复 Enter 视为幂等刷新（先摘旧桶再挂新桶），others 仍返回当前范围内其他人。
func (m *Map) Enter(p *Player) (others []*Player) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.players[p.ID]; ok {
		m.grid.remove(old)
	}
	others = m.grid.inRange(p.X, p.Y)
	m.players[p.ID] = p
	m.grid.add(p)
	return filterSelf(others, p.ID)
}

// Leave 让玩家离开地图。返回离开前在**其视野范围内**的玩家（需收到离场通知）。
// 玩家不在地图上时返回 nil。
func (m *Map) Leave(id uint16) (remaining []*Player) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[id]
	if !ok {
		return nil
	}
	remaining = filterSelf(m.grid.inRange(p.X, p.Y), id)
	m.grid.remove(p)
	delete(m.players, id)
	return remaining
}

// Player 按 ID 取玩家快照指针（调用方不得在未持锁时长期依赖可变字段）。
func (m *Map) Player(id uint16) *Player {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.players[id]
}

// Walk 更新 id 的坐标与朝向，返回更新后的玩家指针与三组 AoI 集合（T1-4）：
//   - others：移动后仍在视野内的其他玩家（收行走包）；
//   - entered：本次移动**新进入**视野的玩家（需收到移动者的入视野包）；
//   - left：本次移动**离开**视野的玩家（需收到移出视野包；离场包在 T2-n 接）。
//
// 玩家不存在时 ok=false。坐标钳制由调用方在组包前完成。
func (m *Map) Walk(id uint16, x, y, rotation byte) (p *Player, others, entered, left []*Player, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok = m.players[id]
	if !ok {
		return nil, nil, nil, nil, false
	}
	before := m.grid.inRange(p.X, p.Y)
	// 桶维护：跨桶移动时先摘旧桶再挂新桶（原版 MoveObjectOnMapAsync 的
	// differentBucket 语义）。**必须先 remove 再更新坐标**——remove 按对象当前
	// 坐标定位桶，先改坐标会从新桶摘（no-op），旧桶条目残留成幽灵（真机事故）。
	m.grid.remove(p)
	p.X, p.Y, p.Rotation = x, y, rotation
	m.grid.add(p)
	after := m.grid.inRange(p.X, p.Y)

	beforeIDs := make(map[uint16]bool, len(before))
	for _, o := range before {
		if o.ID != id {
			beforeIDs[o.ID] = true
		}
	}
	for _, o := range after {
		if o.ID == id {
			continue
		}
		others = append(others, o)
		if !beforeIDs[o.ID] {
			entered = append(entered, o)
		} else {
			beforeIDs[o.ID] = false
		}
	}
	for _, o := range before {
		if o.ID != id && beforeIDs[o.ID] {
			left = append(left, o)
		}
	}
	return p, others, entered, left, true
}

// filterSelf 从列表中剔除指定玩家（ID 相等）。
func filterSelf(players []*Player, id uint16) []*Player {
	out := players[:0]
	for _, p := range players {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return out
}

// Safezone 报告坐标是否安全区（无地形时 false）。
func (m *Map) Safezone(x, y byte) bool {
	m.mu.Lock()
	t := m.terrain
	m.mu.Unlock()
	return t != nil && t.Safezone(x, y)
}

// Count 返回当前在场玩家数。
func (m *Map) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.players)
}
