// world/drop.go —— 地面掉落物实体与超时清理（doc/15 §10 T1-6，
// 对应原版 DroppedItem.cs / DroppedMoney.cs；地面物 ID 段 0..0x7FFF 由玩家 ID 之外分配）。
package world

import (
	"sync"
	"time"

	"mugo/internal/gamelogic/entity/item"
)

// DefaultDropDuration 是掉落物在地面的存活时长（原版 SystemConfiguration.ItemDropDuration = 1 分钟）。
const DefaultDropDuration = time.Minute

// DroppedItem 是地面上的一件掉落物品。
type DroppedItem struct {
	ID        uint16
	MapNumber uint16
	X, Y      byte
	It        *item.Item
	DroppedAt time.Time
}

// DroppedMoney 是地面上的一堆金币。
type DroppedMoney struct {
	ID        uint16
	MapNumber uint16
	X, Y      byte
	Amount    uint32
	DroppedAt time.Time
}

// DropRegistry 管理一张（或多张）地图的地面掉落物。
type DropRegistry struct {
	mu    sync.Mutex
	next  uint16
	items map[uint16]*DroppedItem
	money map[uint16]*DroppedMoney
}

// NewDropRegistry 构造（掉落物 ID 0..ConstantPlayerID-1 = 0..0x1FF，
// 对应原版 GameMap._dropIdGenerator = new IdGenerator(0, ConstantPlayerId-1)）。
func NewDropRegistry() *DropRegistry {
	return &DropRegistry{
		next:  0,
		items: make(map[uint16]*DroppedItem),
		money: make(map[uint16]*DroppedMoney),
	}
}

// allocID 环形分配掉落物 ID（0..0x1FF）。
func (r *DropRegistry) allocID() uint16 {
	id := r.next
	r.next++
	if r.next >= ConstantPlayerID {
		r.next = 0
	}
	return id
}

// AddItem 放置一件掉落物品，返回其 ID。
func (r *DropRegistry) AddItem(mapNumber uint16, x, y byte, it *item.Item, now time.Time) uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.allocID()
	r.items[id] = &DroppedItem{ID: id, MapNumber: mapNumber, X: x, Y: y, It: it, DroppedAt: now}
	return id
}

// AddMoney 放置一堆金币，返回其 ID。
func (r *DropRegistry) AddMoney(mapNumber uint16, x, y byte, amount uint32, now time.Time) uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.allocID()
	r.money[id] = &DroppedMoney{ID: id, MapNumber: mapNumber, X: x, Y: y, Amount: amount, DroppedAt: now}
	return id
}

// TakeItem 拾取物品（成功即从地面移除）。
func (r *DropRegistry) TakeItem(id uint16) (*DroppedItem, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return nil, false
	}
	delete(r.items, id)
	return d, true
}

// PeekItem 只读查找地面物品（拾取校验用；不移除）。
func (r *DropRegistry) PeekItem(id uint16) (*DroppedItem, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	return d, ok
}

// PeekMoney 只读查找地面金币（拾取校验用；不移除）。
func (r *DropRegistry) PeekMoney(id uint16) (*DroppedMoney, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.money[id]
	return d, ok
}

// TakeMoney 拾取金币。
func (r *DropRegistry) TakeMoney(id uint16) (*DroppedMoney, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.money[id]
	if !ok {
		return nil, false
	}
	delete(r.money, id)
	return d, true
}

// ExpiredDrop 是一次过期清理的记录（供 GS 广播移出视野包）。
type ExpiredDrop struct {
	ID        uint16
	MapNumber uint16
	X, Y      byte
}

// ExpireBefore 清理 dropTime 早于 cutoff 的掉落物，返回被清理的明细
// （原版 DroppedItem 生命周期：过期即从地图移除，观察者收移出包）。
func (r *DropRegistry) ExpireBefore(cutoff time.Time) []ExpiredDrop {
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []ExpiredDrop
	for id, d := range r.items {
		if d.DroppedAt.Before(cutoff) {
			expired = append(expired, ExpiredDrop{ID: id, MapNumber: d.MapNumber, X: d.X, Y: d.Y})
			delete(r.items, id)
		}
	}
	for id, d := range r.money {
		if d.DroppedAt.Before(cutoff) {
			expired = append(expired, ExpiredDrop{ID: id, MapNumber: d.MapNumber, X: d.X, Y: d.Y})
			delete(r.money, id)
		}
	}
	return expired
}

// ItemsOnMap 返回某地图的地面物品快照（供视野下发/拾取列表）。
func (r *DropRegistry) ItemsOnMap(mapNumber uint16) []*DroppedItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*DroppedItem
	for _, d := range r.items {
		if d.MapNumber == mapNumber {
			out = append(out, d)
		}
	}
	return out
}

// MoneyOnMap 返回某地图的地面金币快照（供视野下发；T2-1）。
func (r *DropRegistry) MoneyOnMap(mapNumber uint16) []*DroppedMoney {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*DroppedMoney
	for _, d := range r.money {
		if d.MapNumber == mapNumber {
			out = append(out, d)
		}
	}
	return out
}
