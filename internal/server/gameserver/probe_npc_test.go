package gameserver

// probe_npc_test.go —— 实测排查：Lorencia 出生区（X133..151, Y118..135）切比雪夫
// 12 内到底有哪些 NPC/怪（用户实测"看不见 NPC"的第一嫌疑：出生点附近真空）。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/util"
)

func TestProbeNpcNearLorenciaSpawn(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	all := sp.ByMap(0)
	t.Logf("Lorencia NPC 总数: %d", len(all))

	// 出生区每个角的切比雪夫 12 邻域内 NPC 数（代表玩家可能出生点）。
	corners := [][2]byte{{133, 118}, {151, 118}, {133, 135}, {151, 135}, {142, 126}}
	for _, c := range corners {
		count := 0
		sample := ""
		for _, n := range all {
			d := chebyshevByte(n.X, n.Y, c[0], c[1])
			if d <= 12 {
				count++
				if count <= 5 {
					sample += " (" + n.Def.Name + "@" + itoa2(int(n.X)) + "," + itoa2(int(n.Y)) + " d=" + itoa2(d) + ")"
				}
			}
		}
		t.Logf("出生点(%d,%d) 12 格内 NPC=%d %s", c[0], c[1], count, sample)
	}

	// Lorencia 全图 NPC 采样（前 15 只），看坐标分布。
	shown := 0
	for _, n := range all {
		if n.X >= 100 && n.X <= 170 && n.Y >= 100 && n.Y <= 170 && shown < 15 {
			t.Logf("NPC %s #%d @(%d,%d)", n.Def.Name, n.Def.Number, n.X, n.Y)
			shown++
		}
	}
}

func itoa2(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}
