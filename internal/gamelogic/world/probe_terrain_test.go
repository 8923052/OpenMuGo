package world

import (
	"testing"

	"mugo/internal/gamelogic/config"
)

func TestProbeLorenciaTerrain(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mp, _ := cfg.Map(0)
	raw, _ := mp.TerrainBytes()
	if raw == nil {
		t.Fatal("Lorencia 无地形")
	}
	tt := ParseTerrain(raw)
	t.Logf("walkable=%d safezone=%d", tt.WalkableCount(), tt.SafezoneCount())
	cells := [][2]byte{
		{139, 127}, {140, 127}, {141, 127}, {142, 127}, {143, 127},
		{143, 125}, {144, 125}, {145, 125}, {146, 126},
		{161, 127}, {162, 127}, {164, 127}, {170, 127},
		{155, 126}, {158, 125},
	}
	for _, c := range cells {
		t.Logf("(%d,%d) walk=%v safezone=%v", c[0], c[1], tt.Walkable(c[0], c[1]), tt.Safezone(c[0], c[1]))
	}
}
