package gameserver

import (
	"testing"

	"mugo/internal/gamelogic/world"
	"mugo/internal/pathfinding"
)

// npc_ai_route_test.go —— 锁定 routeFinder（worldAIView.NextStep 的底座）：在真实
// world.Terrain 的 AIgrid 上绕墙给出下一步；无解时退回 ok=false。

// walledTerrain 在 x=3 竖一堵墙（y=0..8），仅 y≥9 开口；其余可走。
func walledTerrain() *world.Terrain {
	data := make([]byte, 3+256*256) // 全 0 → 可走
	idx := func(x, y int) int { return 3 + x + (y << 8) }
	for y := 0; y <= 8; y++ {
		data[idx(3, y)] = 2 // 阻挡
	}
	return world.ParseTerrain(data)
}

func TestRouteFinderGoesAroundWall(t *testing.T) {
	rf := newRouteFinder()
	grid := rf.gridFor(walledTerrain())
	x, y, ok := rf.next(pathfinding.Point{X: 0, Y: 0}, pathfinding.Point{X: 6, Y: 0}, grid)
	if !ok {
		t.Fatal("应找到绕行下一步")
	}
	if x == 0 && y == 0 {
		t.Fatal("下一步不应停在原地")
	}
	if grid[x][y]&0x7F == 0 {
		t.Fatalf("首步不应踏入墙格，得 (%d,%d)", x, y)
	}
	if absInt(int(x)-0) > 1 || absInt(int(y)-0) > 1 {
		t.Fatalf("首步应与起点相邻: (%d,%d)", x, y)
	}
}

func TestRouteFinderNoPath(t *testing.T) {
	rf := newRouteFinder()
	grid := rf.gridFor(walledTerrain())
	// 把 (5,5) 的 8 邻域全封死 → 从 (0,0) 不可达 (5,5)。
	for _, c := range [][2]int{{4, 5}, {6, 5}, {5, 4}, {5, 6}, {4, 4}, {4, 6}, {6, 4}, {6, 6}} {
		grid[c[0]][c[1]] = 0
	}
	if _, _, ok := rf.next(pathfinding.Point{X: 0, Y: 0}, pathfinding.Point{X: 5, Y: 5}, grid); ok {
		t.Fatal("被完全隔断时不应给出下一步")
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
