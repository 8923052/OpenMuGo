package npc

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// pathfinding_integration_test.go —— 锁定"追击遇障 → 用寻路绕行"这一接线：
// Intelligence 在贪心一步被挡时，若 WorldView 同时实现 NextStepFinder，则采用其给出的
// 绕行下一步。这是 AI（npc）与 server 层寻路（internal/pathfinding 经 worldAIView）的
// 契约测试；npc 包本身不 import pathfinding，只用可选接口。

// wallWorld 是一个"某格被墙挡 + 提供寻路下一步"的假世界。
type wallWorld struct {
	players   []*world.Player
	blocked   map[[2]byte]bool
	detours   map[[2]byte][2]byte // key: from(x,y) → 绕行下一步（追击目标已随机化，按起点查）
	detourHit *int
}

func (w *wallWorld) PlayersInRange(uint16, byte, byte) []*world.Player { return w.players }

func (w *wallWorld) WalkableForAI(_ uint16, x, y byte) bool { return !w.blocked[[2]byte{x, y}] }

func (w *wallWorld) Safezone(uint16, byte, byte) bool { return false }

// NextStep 使 wallWorld 满足 NextStepFinder。
func (w *wallWorld) NextStep(_ uint16, fx, fy, _, _ byte) (byte, byte, bool) {
	if w.detourHit != nil {
		*w.detourHit++
	}
	if d, ok := w.detours[[2]byte{fx, fy}]; ok {
		return d[0], d[1], true
	}
	return 0, 0, false
}

func TestChaseUsesPathfinderWhenBlocked(t *testing.T) {
	def := mkMonsterDef() // MoveRange 3 / AttackRange 1 / ViewRange 7
	n := NewNpc(0x9001, def, 0, 50, 50, 0)
	hits := 0
	// 追击走位目标现为目标(52,50)周围 AttackRange=1 的随机格，贪心一步必落在 x=51 列，
	// 故把整列 (51,49..51) 都堵上，稳定触发"贪心被挡 → 寻路绕行"。
	view := &wallWorld{
		players: []*world.Player{{ID: 1, X: 52, Y: 50, IsAlive: true}}, // 距 2，触发 chase
		blocked: map[[2]byte]bool{{51, 49}: true, {51, 50}: true, {51, 51}: true},
		// 寻路给出绕行下一步 (50,51)（可走），按起点 (50,50) 查。
		detours:   map[[2]byte][2]byte{{50, 50}: {50, 51}},
		detourHit: &hits,
	}

	NewIntelligence(util.NewRand(1)).Tick(n, time.Now().Add(time.Second), view)

	if n.State != StateChase {
		t.Fatalf("应进入 chase，得 %d", n.State)
	}
	if hits == 0 {
		t.Fatal("贪心被挡时应调用寻路 NextStep")
	}
	if n.X != 50 || n.Y != 51 {
		t.Fatalf("应采用绕行下一步 (50,51)，实际 (%d,%d)", n.X, n.Y)
	}
}

func TestChaseNoDetourKeepsPosition(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x9002, def, 0, 50, 50, 0)
	view := &wallWorld{
		players: []*world.Player{{ID: 1, X: 52, Y: 50, IsAlive: true}},
		blocked: map[[2]byte]bool{{51, 49}: true, {51, 50}: true, {51, 51}: true},
		detours: nil, // 寻路也无解
	}

	NewIntelligence(util.NewRand(1)).Tick(n, time.Now().Add(time.Second), view)

	if n.X != 50 || n.Y != 50 {
		t.Fatalf("无可行绕行时应原地不动，实际 (%d,%d)", n.X, n.Y)
	}
}
