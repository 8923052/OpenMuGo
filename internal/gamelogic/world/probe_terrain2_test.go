package world

import (
	"testing"

	"mugo/internal/gamelogic/config"
)

func TestProbeLorenciaTerrain2(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mp, _ := cfg.Map(0)
	raw, _ := mp.TerrainBytes()
	tt := ParseTerrain(raw)

	// 转置读法：cell (x,y) 的真实属性 = 存储里的 (y,x)。
	transWalk := func(x, y byte) bool { return tt.walk[y][x] }
	transSafe := func(x, y byte) bool { return tt.safezone[y][x] }

	cells := [][2]byte{
		{139, 127}, {140, 127}, {143, 127}, {161, 127},
		{140, 131}, {145, 135}, {150, 130}, {139, 130},
	}
	for _, c := range cells {
		t.Logf("(%d,%d) 直接: walk=%v safe=%v | 转置: walk=%v safe=%v",
			c[0], c[1], tt.Walkable(c[0], c[1]), tt.Safezone(c[0], c[1]),
			transWalk(c[0], c[1]), transSafe(c[0], c[1]))
	}

	// 安全区包围盒（两种读法）。
	minX, maxX, minY, maxY := 255, 0, 255, 0
	tminX, tmaxX, tminY, tmaxY := 255, 0, 255, 0
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			if tt.safezone[x][y] {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
			if tt.safezone[y][x] {
				if x < tminX {
					tminX = x
				}
				if x > tmaxX {
					tmaxX = x
				}
				if y < tminY {
					tminY = y
				}
				if y > tmaxY {
					tmaxY = y
				}
			}
		}
	}
	t.Logf("直接读法 safezone bbox: x[%d..%d] y[%d..%d]", minX, maxX, minY, maxY)
	t.Logf("转置读法 safezone bbox: x[%d..%d] y[%d..%d]", tminX, tmaxX, tminY, tmaxY)
}
