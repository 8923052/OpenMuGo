package world

import "testing"

// terrain_aigrid_test.go —— 锁定 AIgrid 与 walk/safezone 的一致性（供寻路消费）：
// 可走→代价 1；安全区→代价 1 | 0x80；阻挡→0（不可达）。对照原版 UpdateAiGridValue。

func TestAIGridReflectsWalkAndSafezone(t *testing.T) {
	// 构造一张最小地形：只需覆盖被测坐标附近。
	data := make([]byte, 3+256*256) // 3 字节头 + 全 0（默认全可走非安全）
	// (5,5)=阻挡；(6,6)=安全区(raw 1)；(7,7)=普通可走(raw 0)。
	idx := func(x, y int) int { return 3 + x + (y << 8) }
	data[idx(5, 5)] = 2 // 阻挡
	data[idx(6, 6)] = 1 // 可走 + 安全区
	tt := ParseTerrain(data)
	g := tt.AIGrid()
	if g == nil {
		t.Fatal("AIGrid 不应为 nil")
	}
	if g[5][5] != 0 {
		t.Fatalf("阻挡格 AIgrid 应为 0，得 %d", g[5][5])
	}
	if g[6][6] != 0x81 { // 可走(1) + 安全区(0x80)
		t.Fatalf("安全区格 AIgrid 应为 0x81，得 0x%02X", g[6][6])
	}
	if g[7][7] != 1 {
		t.Fatalf("普通可走格 AIgrid 应为 1，得 %d", g[7][7])
	}
	if g[100][100] != 1 { // 未显式设置→默认 raw 0→可走
		t.Fatalf("默认格 AIgrid 应为 1，得 %d", g[100][100])
	}
}

func TestAIGridDefaultTerrainAllWalkable(t *testing.T) {
	tt := ParseTerrain(nil) // 无数据 → 全可走
	g := tt.AIGrid()
	if g == nil || g[0][0] != 1 || g[255][255] != 1 {
		t.Fatal("nil 地形的 AIgrid 应全为代价 1")
	}
}
