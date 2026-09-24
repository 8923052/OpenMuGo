package npc

// spawner.go —— 实例生成（原版 MapInitializer 的运行时部分）：
// 从导出件的出生区（MonsterSpawnArea）按 Quantity 生成怪物/NPC 实例。

import (
	"fmt"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// Spawner 从导出件生成并持有全部怪物/NPC 实例。
type Spawner struct {
	cfg        *config.GameConfig
	rng        *util.Rand
	log        logger
	byMap      map[uint16][]*Npc
	all        []*Npc
	next       uint16                                            // NPC ID 分配器（0x201..0x7FFF，见 nextID 注释）
	attackHook func(n *Npc, target *world.Player, now time.Time) // 怪物攻击玩家回调（GS 装配）
}

// logger 是 npc 包需要的最小日志接口。
type logger interface {
	Printf(format string, args ...any)
}

// NewSpawner 构造（rng 传入固定种子实例以保证出生位置可复现）。
func NewSpawner(cfg *config.GameConfig, rng *util.Rand, log logger) *Spawner {
	if log == nil {
		log = nopLogger{}
	}
	return &Spawner{cfg: cfg, rng: rng, log: log, byMap: make(map[uint16][]*Npc)}
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...any) {}

// SpawnAll 按导出件生成全部怪物/NPC 实例（每个出生区 Quantity 只）。
// 出生点与朝向对照原版 NonPlayerCharacter.Initialize（矩形内随机 →
// IsValidSpawnPoint 校验 → 失败重试；点区域 1 次，区域最多 100 次）。
func (s *Spawner) SpawnAll() {
	spawned := 0
	terrains := make(map[uint16]*world.Terrain) // .att 惰性解析缓存
	terrainOf := func(mp *config.GameMap) *world.Terrain {
		if t, ok := terrains[uint16(mp.Number)]; ok {
			return t
		}
		var t *world.Terrain
		if raw, err := mp.TerrainBytes(); err == nil && raw != nil {
			t = world.ParseTerrain(raw)
		}
		terrains[uint16(mp.Number)] = t
		return t
	}
	for i := range s.cfg.Maps {
		mp := &s.cfg.Maps[i]
		if mp.Discriminator != 0 {
			continue // 只生成主变体
		}
		terr := terrainOf(mp)
		for j := range mp.Spawns {
			area := &mp.Spawns[j]
			if area.Monster == nil || area.Quantity <= 0 {
				continue
			}
			def, ok := s.cfg.Monster(*area.Monster)
			if !ok {
				//s.log.Printf("npc: 出生区引用了不存在的怪物 %d（地图 %d）", *area.Monster, mp.Number)
				continue
			}
			rot := spawnDirection(area.Direction, s.rng)
			for k := 0; k < int(area.Quantity); k++ {
				x, y, ok := s.validSpawnPoint(area, terr)
				if !ok {
					// 原版此处抛 InvalidOperationException（服务无法启动）；
					// Go 侧记录并跳过该实例（偏差登记：内存态启动更韧性）。
					//s.log.Printf("npc: 出生区 %s（地图 %d）无有效出生点，跳过实例", def.Name, mp.Number)
					continue
				}
				id := s.nextID()
				n := NewNpc(id, def, uint16(mp.Number), x, y, rot)
				s.byMap[uint16(mp.Number)] = append(s.byMap[uint16(mp.Number)], n)
				s.all = append(s.all, n)
				spawned++
			}
		}
	}
	s.log.Printf("npc: 已生成 %d 只怪物/NPC（来自 %d 张主变体地图）", spawned, len(s.byMap))
}

// validSpawnPoint 在出生区内随机取一个有效出生点（对照原版 GetNewSpawnPoint +
// IsValidSpawnPoint + Initialize 的重试策略）。
func (s *Spawner) validSpawnPoint(area *config.Spawn, terr *world.Terrain) (byte, byte, bool) {
	maxRetry := 100
	if area.X1 == area.X2 && area.Y1 == area.Y2 {
		maxRetry = 1 // 原版 IsPoint()：单点区域只试一次
	}
	var x, y byte
	for retry := 0; retry < maxRetry; retry++ {
		x, y = s.pointInArea(area)
		if s.isValidSpawnPoint(area, terr, x, y) {
			return x, y, true
		}
	}
	return x, y, false
}

// isValidSpawnPoint 对照原版 NonPlayerCharacter.IsValidSpawnPoint：
// 安全区：CanSpawnInSafezone = kind ∉ {Monster, Trap}（其余含 Guard/Npc 恒可）；
// 可走：npcCanWalk = kind ∈ {Monster, Guard} 时必须 WalkMap。
func (s *Spawner) isValidSpawnPoint(area *config.Spawn, terr *world.Terrain, x, y byte) bool {
	if terr == nil {
		return true // 无地形数据时退化为不校验（T1-1 的 DefaultTerrain 语义）
	}
	kind := ""
	if area.Monster != nil {
		if def, ok := s.cfg.Monster(*area.Monster); ok {
			kind = def.ObjectKind
		}
	}
	safezoneAllowed := kind != "Monster" && kind != "Trap"
	npcCanWalk := kind == "Monster" || kind == "Guard"
	return (safezoneAllowed || !terr.Safezone(x, y)) && (!npcCanWalk || terr.Walkable(x, y))
}

// spawnDirection 对照原版 NonPlayerCharacter.GetSpawnDirection + ToPacketByte：
// 配置 Undefined(0) → 随机 Direction 1..8；否则用配置值；封包值 = Direction-1。
func spawnDirection(configured int, rng *util.Rand) byte {
	if configured == 0 {
		configured = rng.Next(1, 9)
	}
	return byte(configured - 1)
}

// pointInArea 在出生区矩形内取确定性随机点（原版 MapInitializer 的随机出生语义）。
func (s *Spawner) pointInArea(area *config.Spawn) (byte, byte) {
	x1, x2 := int(area.X1), int(area.X2)
	y1, y2 := int(area.Y1), int(area.Y2)
	if x2 < x1 {
		x1, x2 = x2, x1
	}
	if y2 < y1 {
		y1, y2 = y2, y1
	}
	x := x1 + s.rng.Next(0, x2-x1+1)
	y := y1 + s.rng.Next(0, y2-y1+1)
	return byte(x), byte(y)
}

// nextID 分配 NPC ID：0x201..0x7FFF（原版 GameMap._objectIdGenerator =
// IdGenerator(ViewExtensions.ConstantPlayerId+1, 0x7FFF)。玩家区 1..0x200、
// 掉落物区 0..0x1FF 之外；**不得**用 0x8000 以上——那是出站包里的出生位旗标，
// 不是 ID 段，混用会让客户端对象索引互相覆盖）。
func (s *Spawner) nextID() uint16 {
	id := 0x201 + s.next
	s.next++
	if id >= 0x7FFF {
		s.next = 0
		id = 0x201
	}
	return id
}

// All 返回全部实例（生成顺序）。
func (s *Spawner) All() []*Npc { return s.all }

// SetAttackHook 注册怪物攻击玩家的回调（GS 装配：伤害结算 + 出站；T2-2）。
func (s *Spawner) SetAttackHook(hook func(n *Npc, target *world.Player, now time.Time)) {
	s.attackHook = hook
}

// ByMap 返回指定地图的实例列表。
func (s *Spawner) ByMap(mapNumber uint16) []*Npc { return s.byMap[mapNumber] }

// Count 返回实例总数。
func (s *Spawner) Count() int { return len(s.all) }

// TickAll 驱动怪物 AI 一次，返回本 tick 的移动事件与复活事件
// （供 GS 广播 NPC 移动/出生/移出视野包——原版 MoveObjectAsync 的观察者通知）。
// 只驱动**有玩家的地图**（原版无观察者时怪物不思考的等价优化）。
func (s *Spawner) TickAll(now time.Time, view WorldView, activeMaps map[uint16]bool) (moved []MovedNpc, revived []*Npc) {
	if view == nil {
		return nil, nil
	}
	for _, n := range s.all {
		if !activeMaps[n.MapNumber] {
			continue
		}
		oldX, oldY := n.X, n.Y
		wasDead := !n.Alive()
		s.deciderFor(n).Tick(n, now, view)
		if wasDead && n.Alive() {
			revived = append(revived, n)
			continue
		}
		if n.X != oldX || n.Y != oldY {
			moved = append(moved, MovedNpc{Npc: n, OldX: oldX, OldY: oldY})
		}
	}
	return moved, revived
}

// MovedNpc 是一次 AI 移动事件（旧坐标 → Npc.X/Y 新坐标）。
type MovedNpc struct {
	Npc        *Npc
	OldX, OldY byte
}

var _ = fmt.Sprintf

// deciderFor 按配置装配并缓存该 NPC 的决策器（TRIM-07）。
// 攻击回调每次取最新的 s.attackHook（GS 在 StartAsync 之后才注册）。
func (s *Spawner) deciderFor(n *Npc) Decider {
	if n.decider == nil {
		n.decider = IntelligenceFor(n, s.rng, s.aiAttackHook)
	}
	return n.decider
}

// aiAttackHook 是决策器回调到 Spawner 的稳定入口（hook 晚注册也能生效）。
func (s *Spawner) aiAttackHook(n *Npc, target *world.Player, now time.Time) {
	if s.attackHook != nil {
		s.attackHook(n, target, now)
	}
}
