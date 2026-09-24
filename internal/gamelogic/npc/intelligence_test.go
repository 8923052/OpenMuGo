package npc

// intelligence_test.go —— T1-6 掉落生成器的固定种子序列验证辅助在此不做
// （掉落在 internal/gamelogic/drops 包）。本文件保留 AI 的补充场景。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// TestAIRoamWithNoPlayers 锁定：无观察者（视野内无玩家）时怪物不移动——
// 原版 IsObservedByAttacker 语义（M7 放宽为"有玩家在范围才动"）。
func TestAIRoamWithNoPlayers(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8001, def, 0, 50, 50, 0)
	view := testView()
	ai := NewIntelligence(util.NewRand(7))

	for i := 0; i < 5; i++ {
		ai.Tick(n, time.Now().Add(time.Duration(i)*time.Second), view)
	}
	if n.X != 50 || n.Y != 50 {
		t.Fatalf("无玩家时不应游走: (%d,%d)", n.X, n.Y)
	}
}

// TestChaseWalkTargetIsRandom 锁定修复：追击走位目标取自标周围 AttackRange 内的
// 随机可走点（原版 GetRandomCoordinate）——多只怪因此分散、不再叠到目标同一坐标。
func TestChaseWalkTargetIsRandom(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x9003, def, 0, 50, 50, 0)
	view := testView()
	ai := NewIntelligence(util.NewRand(42))
	const cx, cy byte = 60, 50
	seen := map[[2]byte]bool{}
	for k := 0; k < 40; k++ {
		x, y := ai.randomCoordinate(n, view, cx, cy, int(def.AttackRange))
		if dist(x, y, cx, cy) > int(def.AttackRange) {
			t.Fatalf("随机走位点超出 AttackRange: (%d,%d)", x, y)
		}
		seen[[2]byte{x, y}] = true
	}
	if len(seen) < 2 {
		t.Fatalf("走位目标未随机分散（全部叠到同一坐标）: %v", seen)
	}
	// AttackRange=0（贴身怪）→ 回落目标点本身。
	if x, y := ai.randomCoordinate(n, view, cx, cy, 0); x != cx || y != cy {
		t.Fatalf("radius=0 应回落中心点，得 (%d,%d)", x, y)
	}
}

// TestRoamWithinMoveRange 锁定：游走步幅不超过 MoveRange（相对出生点）。
func TestRoamWithinMoveRange(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8004, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	view.players = append(view.players, &world.Player{ID: 1, X: 51, Y: 51})
	ai := NewIntelligence(util.NewRand(7))

	for i := 0; i < 10; i++ {
		ai.Tick(n, time.Now().Add(time.Duration(i)*time.Second), view)
		if d := dist(n.X, n.Y, 50, 50); d > def.MoveRange {
			t.Fatalf("游走超出 MoveRange=%d: (%d,%d) 距 %d", def.MoveRange, n.X, n.Y, d)
		}
	}
}
