package npc

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/world"
	"mugo/internal/util"
)

func TestDebugChaseTrace(t *testing.T) {
	def := mkMonsterDef()
	n := NewNpc(0x8001, def, 0, 50, 50, 0)
	view := testView().(*fakeWorld)
	now := time.Now()
	ai := NewIntelligence(util.NewRand(1))

	p := &world.Player{ID: 1, X: 60, Y: 50}
	view.players = append(view.players, p)

	for i := 0; i < 6; i++ {
		ts := now.Add(time.Duration(i) * time.Second)
		ai.Tick(n, ts, view)
		t.Logf("tick%d: pos=(%d,%d) state=%d lastMove=%v", i, n.X, n.Y, n.State, n.LastMove())
		p.X, p.Y = 55, 50 // 从第 2 tick 起玩家站在 (55,50)
	}
}
