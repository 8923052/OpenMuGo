package npc

// intelligence_select_test.go —— TRIM-07：决策器装配（按配置类型名 → ObjectKind 的次序）与
// 守卫 / 四种陷阱的行为。对照 GuardIntelligence.cs、TrapIntelligenceBase.cs 与四个陷阱子类。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

// stringOf 造 *string（配置里的类型名字段是可选的）。
func stringOf(s string) *string { return &s }

// mkKindDef 造一个带类别/类型名的对象定义。
func mkKindDef(kind, typeName string, viewRange, attackRange int) *config.Monster {
	def := mkMonsterDef()
	def.ObjectKind = kind
	def.Intelligence = stringOf(typeName)
	def.ViewRange = viewRange
	def.AttackRange = attackRange
	return def
}

func TestIntelligenceForSelection(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		typeName string
		want     string
	}{
		{"配置写明守卫", kindGuard, typeGuardIntelligence, "*npc.Intelligence"},
		{"配置写明随机陷阱", kindTrap, typeRandomAttackInRangeTrap, "npc.trapIntelligence"},
		{"配置写明单体踩压陷阱", kindTrap, typeTrapSinglePressed, "npc.trapIntelligence"},
		{"配置写明 Null", kindMonster, typeNullMonsterIntelligence, "npc.NullIntelligence"},
		{"类型名不认识（原版反射失败）", kindMonster, "MUnique.OpenMU.GameLogic.NPC.NotAThing", "npc.NullIntelligence"},
		{"无类型名 + Monster", kindMonster, "", "*npc.Intelligence"},
		{"无类型名 + Guard 默认守卫", kindGuard, "", "*npc.Intelligence"},
		{"无类型名 + Trap 默认随机陷阱", kindTrap, "", "npc.trapIntelligence"},
		{"无类型名 + 商人（原版无 AI）", kindPassiveNpc, "", "npc.NullIntelligence"},
		{"无类型名 + 雕像", kindStatue, "", "npc.NullIntelligence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := NewNpc(0x8001, mkKindDef(tc.kind, tc.typeName, 7, 1), 0, 50, 50, 0)
			got := IntelligenceFor(n, util.NewRand(1), nil)
			if typ := typeNameOfDecider(got); typ != tc.want {
				t.Fatalf("决策器类型 = %s, want %s", typ, tc.want)
			}
		})
	}
}

func typeNameOfDecider(d Decider) string {
	switch d.(type) {
	case NullIntelligence:
		return "npc.NullIntelligence"
	case trapIntelligence:
		return "npc.trapIntelligence"
	case *Intelligence:
		return "*npc.Intelligence"
	}
	return "?"
}

// TestGuardOnlyAttacksRedPlayers 对照 GuardIntelligence.cs:38-48：视野内只挑玩家，且要求
// 红名（≥ PlayerKiller1stStage）、存活、不在安全区，取最近的一个。
func TestGuardOnlyAttacksRedPlayers(t *testing.T) {
	def := mkKindDef(kindGuard, typeGuardIntelligence, 7, 2)
	n := NewNpc(0x8001, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	view.players = []*world.Player{
		{ID: 1, X: 52, Y: 50, IsAlive: true, Stats: &entity.CharStats{HeroState: 3}},  // 更近的白名 → 不该被打
		{ID: 2, X: 51, Y: 50, IsAlive: true, Stats: &entity.CharStats{HeroState: 5}},  // 红名且在 AttackRange 内 → 目标
		{ID: 3, X: 56, Y: 50, IsAlive: true, Stats: &entity.CharStats{HeroState: 6}},  // 更远的红名
		{ID: 4, X: 51, Y: 51, IsAlive: false, Stats: &entity.CharStats{HeroState: 6}}, // 尸体
	}
	view.safezone[[2]byte{51, 52}] = true
	view.players = append(view.players, &world.Player{ID: 5, X: 51, Y: 52, IsAlive: true, Stats: &entity.CharStats{HeroState: 6}}) // 安全区红名

	var hits []uint16
	ai := NewGuardIntelligence(util.NewRand(7), func(_ *Npc, target *world.Player, _ time.Time) {
		hits = append(hits, target.ID)
	})
	now := time.Now()
	ai.Tick(n, now, view)
	if len(hits) != 1 || hits[0] != 2 {
		t.Fatalf("守卫应只打最近的那个红名玩家(ID2), got %v", hits)
	}
	if n.State != StateAttack {
		t.Fatalf("应为 attack, got %d", n.State)
	}
	// 全白名时不打人。
	view.players = view.players[:1]
	hits = nil
	ai.Tick(n, now.Add(3*time.Second), view)
	if len(hits) != 0 {
		t.Fatalf("白名玩家不该被守卫攻击, got %v", hits)
	}
}

// TestGuardStaysNearHome 对照 TickWithoutTargetAsync：无目标时只有 20% 概率行动，
// 且离出生点 >7 时朝"出生点/自身中点"走回。
func TestGuardStaysNearHome(t *testing.T) {
	def := mkKindDef(kindGuard, typeGuardIntelligence, 7, 2)
	n := NewNpc(0x8001, def, 0, 70, 50, 0) // 出生点 (50,50) 之后由 OnStart 语义等价：HomeX/HomeY
	n.HomeX, n.HomeY = 50, 50
	view := testView().(*fakeWorld)
	ai := NewGuardIntelligence(util.NewRand(11), nil)

	before := dist(n.X, n.Y, 50, 50)
	moved := 0
	now := time.Now()
	for i := 0; i < 60; i++ {
		now = now.Add(time.Second)
		oldX, oldY := n.X, n.Y
		ai.Tick(n, now, view)
		if n.X != oldX || n.Y != oldY {
			moved++
		}
	}
	after := dist(n.X, n.Y, 50, 50)
	if moved == 0 {
		t.Fatal("60 个 tick 里守卫一次都没回走，20% 概率判定可疑")
	}
	if moved >= 60 {
		t.Fatalf("守卫不该每 tick 都动（原版 20%% 概率）, got %d/60", moved)
	}
	if after >= before {
		t.Fatalf("守卫应向出生点靠近：before=%d after=%d @(%d,%d)", before, after, n.X, n.Y)
	}
}

// TestTrapVariants 覆盖四种陷阱的触发/目标集合差异。
func TestTrapVariants(t *testing.T) {
	cases := []struct {
		name     string
		typeName string
		rotation byte
		players  []*world.Player
		wantIDs  []uint16
	}{
		{
			name: "随机射程内", typeName: typeRandomAttackInRangeTrap,
			players: []*world.Player{{ID: 1, X: 52, Y: 50, IsAlive: true}},
			wantIDs: []uint16{1},
		},
		{
			name: "单体踩压·没踩上", typeName: typeTrapSinglePressed,
			players: []*world.Player{{ID: 1, X: 51, Y: 50, IsAlive: true}},
			wantIDs: nil,
		},
		{
			name: "单体踩压·踩上", typeName: typeTrapSinglePressed,
			players: []*world.Player{{ID: 1, X: 50, Y: 50, IsAlive: true}, {ID: 2, X: 51, Y: 50, IsAlive: true}},
			wantIDs: []uint16{1},
		},
		{
			name: "面积踩压·踩上后打一圈", typeName: typeTrapAreaPressed,
			players: []*world.Player{{ID: 1, X: 50, Y: 50, IsAlive: true}, {ID: 2, X: 51, Y: 51, IsAlive: true}},
			wantIDs: []uint16{1, 2},
		},
		{
			name: "面积踩压·没人踩", typeName: typeTrapAreaPressed,
			players: []*world.Player{{ID: 2, X: 51, Y: 51, IsAlive: true}},
			wantIDs: nil,
		},
		{
			name: "朝向面积·只打正东", typeName: typeTrapAreaDirection, rotation: 3,
			players: []*world.Player{{ID: 1, X: 51, Y: 50, IsAlive: true}, {ID: 2, X: 50, Y: 51, IsAlive: true}},
			wantIDs: []uint16{1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := mkKindDef(kindTrap, tc.typeName, 2, 2)
			n := NewNpc(0x8001, def, 0, 50, 50, tc.rotation)
			n.HomeX, n.HomeY = 50, 50
			view := testView().(*fakeWorld)
			view.players = tc.players
			var hits []uint16
			ai := IntelligenceFor(n, util.NewRand(5), func(_ *Npc, p *world.Player, _ time.Time) {
				hits = append(hits, p.ID)
			})
			now := time.Now()
			ai.Tick(n, now, view)
			if !sameIDs(hits, tc.wantIDs) {
				t.Fatalf("陷阱目标 = %v, want %v", hits, tc.wantIDs)
			}
			if n.X != 50 || n.Y != 50 {
				t.Fatalf("陷阱不应移动, got (%d,%d)", n.X, n.Y)
			}
		})
	}
}

// TestTrapAttackDelayThrottles 锁定陷阱同样受 AttackDelay 限频（TrapIntelligenceBase.cs:84）。
func TestTrapAttackDelayThrottles(t *testing.T) {
	def := mkKindDef(kindTrap, typeRandomAttackInRangeTrap, 2, 2)
	def.AttackDelayMs = 1000
	n := NewNpc(0x8001, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	view.players = []*world.Player{{ID: 1, X: 51, Y: 50, IsAlive: true}}
	hits := 0
	ai := IntelligenceFor(n, util.NewRand(5), func(*Npc, *world.Player, time.Time) { hits++ })
	now := time.Now()
	ai.Tick(n, now, view)
	ai.Tick(n, now.Add(100*time.Millisecond), view)
	if hits != 1 {
		t.Fatalf("AttackDelay 未到应不重复攻击, got %d", hits)
	}
	ai.Tick(n, now.Add(2*time.Second), view)
	if hits != 2 {
		t.Fatalf("过了 AttackDelay 应再次攻击, got %d", hits)
	}
}

// TestNullIntelligenceDoesNothing 锁定"无 AI 静态物"什么都不做（原版 NullMonsterIntelligence）。
func TestNullIntelligenceDoesNothing(t *testing.T) {
	n := NewNpc(0x8001, mkKindDef(kindStatue, "", 7, 1), 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	view.players = []*world.Player{{ID: 1, X: 50, Y: 50, IsAlive: true}}
	hits := 0
	ai := IntelligenceFor(n, util.NewRand(5), func(*Npc, *world.Player, time.Time) { hits++ })
	ai.Tick(n, time.Now(), view)
	if hits != 0 || n.State != StateIdle {
		t.Fatalf("雕像不该有任何行为, hits=%d state=%d", hits, n.State)
	}
}

func sameIDs(got, want []uint16) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
