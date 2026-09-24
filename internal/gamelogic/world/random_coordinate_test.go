// random_coordinate_test.go —— 掉落脚距：对照原版 GameMapTerrain.GetRandomCoordinate
// （方框内随机、不可走重掷最多 20 次、用尽则回原点、边界只钳 0..255）。
package world

import (
	"testing"

	"mugo/internal/util"
)

// allBlocked 是全阻挡地形（3 字节头 + 256×256 全 2）。
func allBlocked() *Terrain {
	body := make([]byte, 3+256*256)
	for i := 3; i < len(body); i++ {
		body[i] = 2
	}
	return ParseTerrain(body)
}

func TestRandomCoordinateNearOpenTerrain(t *testing.T) {
	m := newMap(0)
	m.SetTerrain(ParseTerrain(nil)) // 全可走
	rng := util.NewRand(7)

	for i := 0; i < 200; i++ {
		x, y := m.RandomCoordinateNear(100, 100, 2, rng)
		if dx := int(x) - 100; dx < -2 || dx > 2 {
			t.Fatalf("落点超出半径: (%d,%d)", x, y)
		}
		if dy := int(y) - 100; dy < -2 || dy > 2 {
			t.Fatalf("落点超出半径: (%d,%d)", x, y)
		}
	}
	// 至少出现过一次"不是原点"，证明真的散布了。
	rng2 := util.NewRand(11)
	spread := false
	for i := 0; i < 50; i++ {
		if x, y := m.RandomCoordinateNear(100, 100, 2, rng2); x != 100 || y != 100 {
			spread = true
			break
		}
	}
	if !spread {
		t.Fatal("250 次取样应至少散布到一个非原点点位")
	}
}

func TestRandomCoordinateNearBlockedFallsBackToSource(t *testing.T) {
	m := newMap(0)
	m.SetTerrain(allBlocked())
	if x, y := m.RandomCoordinateNear(120, 130, 2, util.NewRand(3)); x != 120 || y != 130 {
		t.Fatalf("20 次重掷皆不可走应回原点, got (%d,%d)", x, y)
	}
}

// TestRandomCoordinateNearClampsTo255 锁原版边界语义：只钳 0..255，不按地图真实宽高。
func TestRandomCoordinateNearClampsTo255(t *testing.T) {
	m := newMap(0)
	m.SetTerrain(ParseTerrain(nil))
	rng := util.NewRand(5)
	for i := 0; i < 100; i++ {
		x, y := m.RandomCoordinateNear(255, 0, 2, rng)
		if x > 255 || y > 255 {
			t.Fatalf("越界: (%d,%d)", x, y)
		}
		if int(x) < 253 || y > 2 {
			t.Fatalf("角落取样应落在 [253,255]×[0,2], got (%d,%d)", x, y)
		}
	}
}
