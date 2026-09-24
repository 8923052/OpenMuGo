package world

import "mugo/internal/pathfinding"

// terrain.go —— 地形解析与碰撞（doc/15 §10 T1-1，原版 GameLogic/GameMapTerrain.cs）。
//
// .att 格式（原版 ReadTerrainData）：前 3 字节为头（跳过），其后为 256×256 逐格属性字节，
// i 字节格 x = i&0xFF，y = (i>>8)&0xFF；值 0=普通可走，1=可走且安全区，其余=阻挡。
// TerrainData 为 nil 时全部可走（原版 DefaultTerrain 全 0）。

// Terrain 是一张地图的解析后地形。
type Terrain struct {
	walk     [256][256]bool
	safezone [256][256]bool
	// aiGrid 是寻路代价网格（原版 AIgrid：低 7 位代价、最高位安全区旗标），
	// 在 ParseTerrain 时构建一次并缓存，供 internal/pathfinding 消费（T1-f）。
	aiGrid *pathfinding.Grid
}

// AIGrid 返回地图的寻路代价网格（对照原版 GameMapTerrain.AIgrid，索引 [x][y]）。
// 可走格代价 1、安全区格额外置 0x80 旗标；不可走格为 0（不可达）。
func (t *Terrain) AIGrid() *pathfinding.Grid { return t.aiGrid }

// buildAIGrid 由 walk/safezone 生成 AIgrid（原版 UpdateAiGridValue 语义）。
func (t *Terrain) buildAIGrid() *pathfinding.Grid {
	g := &pathfinding.Grid{}
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			var v byte
			if t.walk[x][y] {
				v = 1
			}
			if t.safezone[x][y] {
				v |= 0b1000_0000
			}
			g[x][y] = v
		}
	}
	return g
}

// ParseTerrain 解析原始 .att 字节（含 3 字节头）。
func ParseTerrain(data []byte) *Terrain {
	t := &Terrain{}
	if len(data) == 0 {
		// 原版 DefaultTerrain：全 0 → 全部可走、无安全区。
		for x := 0; x < 256; x++ {
			for y := 0; y < 256; y++ {
				t.walk[x][y] = true
			}
		}
		t.aiGrid = t.buildAIGrid()
		return t
	}
	body := data[3:]
	for i := range body {
		x := i & 0xFF
		y := (i >> 8) & 0xFF
		v := body[i]
		t.walk[x][y] = v == 0 || v == 1
		t.safezone[x][y] = v == 1
	}
	t.aiGrid = t.buildAIGrid()
	return t
}

// Walkable 报告坐标是否可走。
func (t *Terrain) Walkable(x, y byte) bool { return t.walk[x][y] }

// Safezone 报告坐标是否安全区。
func (t *Terrain) Safezone(x, y byte) bool { return t.safezone[x][y] }

// WalkableCount 返回可走格数（测试与诊断用）。
func (t *Terrain) WalkableCount() int {
	n := 0
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			if t.walk[x][y] {
				n++
			}
		}
	}
	return n
}

// SafezoneCount 返回安全区格数（测试与诊断用）。
func (t *Terrain) SafezoneCount() int {
	n := 0
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			if t.safezone[x][y] {
				n++
			}
		}
	}
	return n
}
