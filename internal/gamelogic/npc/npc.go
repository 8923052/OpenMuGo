// Package npc 是怪物/NPC 的生成与 AI（doc/15 §10 T1-5，对应原版
// GameLogic/MapInitializer.cs 的实例生成 + NPC/BasicMonsterIntelligence.cs 的决策逻辑）。
//
// 忠实复刻的语义：
//   - 出生：按导出件出生区（MonsterSpawnArea）与 Quantity 生成实例，位置在矩形内
//     随机（确定性：固定种子 util.Rand）；
//   - AI tick（原版 BasicMonsterIntelligence.TickAsync，间隔 = AttackDelay）：
//     死亡 → 等待重生（RespawnDelay）；索敌 → 最近目标（ViewRange 内、不在安全区）；
//     目标在 AttackRange 内 → 攻击（回调）；ViewRange+1 内 → 朝目标附近随机点走一步；
//     否则 → 随机游走（有观察者时才动，原版 IsObservedByAttacker 语义）；
//   - 移动受地形约束（AIgrid：可走且非安全区，原版 CanWalkOn）。
//
// 裁剪登记：攻击伤害结算、NPC 离场广播随 T2 战斗/视野包接入；本包只做
// 状态机与位置维护，出站包由视图层（T2-b）消费。
package npc

import (
	"fmt"
	"sync"
	"time"

	"mugo/internal/gamelogic/config"
)

// State 是怪物/NPC 的 AI 状态（原版以属性/位置推断，这里显式化便于验收）。
type State uint8

const (
	StateIdle State = iota
	StateRoam
	StateChase
	StateAttack
	StateDead
)

// Npc 是一只怪物/NPC 实例（原版 NonPlayerCharacter/Monster）。
type Npc struct {
	ID        uint16
	Number    int16
	Name      string
	MapNumber uint16
	X, Y      byte
	Rotation  byte
	HomeX     byte // 出生点（随机游走围绕它）
	HomeY     byte
	Def       *config.Monster
	State     State

	mu         sync.Mutex
	health     float32
	targetID   uint16 // 当前目标（玩家 ID；0 = 无）
	respawnAt  time.Time
	lastAttack time.Time
	lastMove   time.Time

	// decider 是本 NPC 的 AI 决策器（首次 tick 时按配置装配并缓存）。
	decider Decider
}

// LastMove 返回最近一次移动尝试时刻（测试用）。
func (n *Npc) LastMove() time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.lastMove
}

// NewNpc 构造实例（health = 导出件 "Maximum Health"）。
func NewNpc(id uint16, def *config.Monster, mapNumber uint16, x, y, rotation byte) *Npc {
	n := &Npc{
		ID:        id,
		Number:    int16(def.Number),
		Name:      def.Name,
		MapNumber: mapNumber,
		X:         x, Y: y, Rotation: rotation,
		HomeX: x, HomeY: y,
		Def: def,
	}
	n.health = n.Attribute("Maximum Health")
	return n
}

// Attribute 按 designation 读怪物属性值（缺失 0）。
func (n *Npc) Attribute(designation string) float32 {
	for _, a := range n.Def.Attributes {
		if a.Designation == designation {
			return float32(a.Value)
		}
	}
	return 0
}

// Health 当前血量。
func (n *Npc) Health() float32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.health
}

// Alive 是否存活。
func (n *Npc) Alive() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.health > 0
}

// ApplyDamage 扣血；死亡时进入死亡态并安排重生（原版受击→Kill 路径）。
// 返回剩余血量与是否本次致死。
func (n *Npc) ApplyDamage(dmg int, now time.Time) (remaining float32, died bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.health <= 0 {
		return 0, true
	}
	n.health -= float32(dmg)
	if n.health <= 0 {
		n.health = 0
		n.State = StateDead
		n.targetID = 0
		n.respawnAt = now.Add(time.Duration(n.Def.RespawnDelayMs) * time.Millisecond)
		return 0, true
	}
	return n.health, false
}

// Kill 击杀（T2 战斗接入时由伤害路径调用）：进入死亡态并安排重生。
func (n *Npc) Kill(now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.health = 0
	n.State = StateDead
	n.targetID = 0
	respawn := time.Duration(n.Def.RespawnDelayMs) * time.Millisecond
	n.respawnAt = now.Add(respawn)
}

// String 便于日志。
func (n *Npc) String() string {
	return fmt.Sprintf("npc %d(%s) @%d(%d,%d)", n.ID, n.Name, n.MapNumber, n.X, n.Y)
}

var _ = fmt.Sprintf
