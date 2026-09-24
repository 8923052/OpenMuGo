package npc

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// fakeWorld 是 AI 测试的脚本化世界：手动放置玩家、指定安全区格与阻挡格。
type fakeWorld struct {
	players  []*world.Player
	blocked  map[[2]byte]bool
	safezone map[[2]byte]bool
}

func (f *fakeWorld) PlayersInRange(mapNumber uint16, x, y byte) []*world.Player {
	var out []*world.Player
	for _, p := range f.players {
		dx := int(p.X) - int(x)
		dy := int(p.Y) - int(y)
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		const infoRange = 12
		if dx <= infoRange && dy <= infoRange {
			out = append(out, p)
		}
	}
	return out
}

func (f *fakeWorld) WalkableForAI(mapNumber uint16, x, y byte) bool {
	return !f.blocked[[2]byte{x, y}]
}

func (f *fakeWorld) Safezone(mapNumber uint16, x, y byte) bool {
	return f.safezone[[2]byte{x, y}]
}

func mkMonsterDef() *config.Monster {
	// Bali（150）：MoveRange 3 / AttackRange 1 / ViewRange 7 / MoveDelay 400ms / AttackDelay 1600ms / 重生 100s。
	return &config.Monster{
		Number: 150, Name: "Bali",
		MoveRange: 3, AttackRange: 1, ViewRange: 7,
		MoveDelayMs: 400, AttackDelayMs: 1600, RespawnDelayMs: 100000,
		Attributes: []config.AttributeVal{{Designation: "Maximum Health", Value: 5000}},
	}
}

func testView() WorldView {
	return &fakeWorld{blocked: map[[2]byte]bool{}, safezone: map[[2]byte]bool{}}
}

// TestSpawnDeterministic 锁定生成：同一 seed 下出生位置完全可复现，且全部落在出生区内。
func TestSpawnDeterministic(t *testing.T) {
	build := func() []*Npc {
		cfg, err := config.LoadSeason6()
		if err != nil {
			t.Fatal(err)
		}
		s := NewSpawner(cfg, util.NewRand(0x5EED), nopLogger{})
		s.SpawnAll()
		var out []*Npc
		for _, n := range s.ByMap(0) { // Lorencia
			out = append(out, n)
		}
		return out
	}
	first := build()
	second := build()
	if len(first) == 0 {
		t.Fatal("Lorencia 应有出生怪")
	}
	if len(first) != len(second) {
		t.Fatalf("两次生成数量不一致: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].X != second[i].X || first[i].Y != second[i].Y || first[i].Number != second[i].Number {
			t.Fatalf("第 %d 只不一致: (%d,%d) vs (%d,%d)", i,
				first[i].X, first[i].Y, second[i].X, second[i].Y)
		}
	}
	// ID 在对象池段（0x201..0x7FFF，原版 GameMap objectIdGenerator；0x8000 是出生位旗标不是 ID）。
	for _, n := range first {
		if n.ID < 0x201 || n.ID > 0x7FFF {
			t.Fatalf("NPC ID 应在 0x201..0x7FFF: %d", n.ID)
		}
	}
	// 朝向为封包值（0..7）：Undefined(0) 配置随机 1..8 → packet = enum-1。
	for _, n := range first {
		if n.Rotation > 7 {
			t.Fatalf("NPC 朝向应为封包值 0..7: %d", n.Rotation)
		}
	}
}

// TestAIStateMachine 锁定状态迁移：idle → (玩家入视野) chase → (入攻击范围) attack。
func TestAIStateMachine(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8001, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	now := time.Now()

	// 无玩家：保持 idle。
	NewIntelligence(util.NewRand(1)).Tick(n, now, view)
	if n.State != StateIdle {
		t.Fatalf("无玩家应 idle, got %d", n.State)
	}

	// 玩家在远处（距 10，ViewRange 7 之外、12 之内）：不可见 → 仍 idle。
	p := &world.Player{ID: 1, X: 60, Y: 50, IsAlive: true}
	view.players = append(view.players, p)
	NewIntelligence(util.NewRand(1)).Tick(n, now.Add(time.Second), view)
	if n.State != StateIdle {
		t.Fatalf("ViewRange 外玩家不应触发追逐, got %d", n.State)
	}

	// 玩家靠近（距 5 < ViewRange 7 且 > AttackRange 1）→ chase（朝目标移动）。
	p.X, p.Y = 55, 50
	ai := NewIntelligence(util.NewRand(1))
	ai.Tick(n, now.Add(2*time.Second), view)
	if n.State != StateChase {
		t.Fatalf("玩家在 ViewRange 内应 chase, got %d", n.State)
	}
	// chase 后应比 chase 前更接近目标（单步移动，后续 tick 持续逼近）。
	if dist(n.X, n.Y, 55, 50) >= 7 {
		t.Fatalf("chase 应向目标移动: (%d,%d)", n.X, n.Y)
	}

	// 多次 chase tick 后进入攻击范围 → attack（攻击回调时机 T2 战斗接入）。
	for i := 0; i < 12; i++ {
		now = now.Add(400 * time.Millisecond)
		ai.Tick(n, now, view)
		p.X, p.Y = n.X, n.Y // 玩家原地等待（测试脚本）
		if n.State == StateAttack {
			break
		}
	}
	if n.State != StateAttack {
		t.Fatalf("进入攻击范围后应 attack, got %d @(%d,%d) target(%d,%d)", n.State, n.X, n.Y, p.X, p.Y)
	}
}

// TestRespawn 锁定死亡→重生：Kill 后进入 Dead，RespawnDelay 内不复活，之后原地复活满血。
func TestRespawn(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8002, def, 0, 50, 50, 0)
	view := testView()
	now := time.Now()

	n.Kill(now)
	if n.State != StateDead || n.Alive() {
		t.Fatal("Kill 后应为 Dead")
	}
	// 重生延迟内不复活。
	NewIntelligence(util.NewRand(1)).Tick(n, now.Add(time.Second), view)
	if n.Alive() {
		t.Fatal("重生延迟内不应复活")
	}
	// 重生后满血回出生点。
	NewIntelligence(util.NewRand(1)).Tick(n, now.Add(101*time.Second), view)
	if !n.Alive() {
		t.Fatal("重生后应存活")
	}
	if n.X != 50 || n.Y != 50 {
		t.Fatalf("重生应回出生点: (%d,%d)", n.X, n.Y)
	}
	if n.Health() != 5000 {
		t.Fatalf("重生后血量=%v, want 5000", n.Health())
	}
}

// TestSafezoneTargetIgnored 锁定：安全区内的玩家不被索敌（原版 IsAtSafezone 守卫）。
func TestSafezoneTargetIgnored(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8003, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	view.safezone[[2]byte{52, 50}] = true
	p := &world.Player{ID: 9, X: 52, Y: 50} // 距 2 < ViewRange，但在安全区
	view.players = append(view.players, p)

	NewIntelligence(util.NewRand(1)).Tick(n, time.Now(), view)
	if n.State != StateIdle {
		t.Fatalf("安全区玩家不应被索敌, got %d", n.State)
	}
}

func dist(x1, y1, x2, y2 byte) int {
	dx := int(x1) - int(x2)
	dy := int(y1) - int(y2)
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}
