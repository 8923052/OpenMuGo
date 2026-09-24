// handler_attack_drop_test.go —— TRIM-04 击杀掉落的时序：原版 OnDeathAsync 里
// `_ = DropItemDelayedAsync(...)`（Task.Delay(1000) 后才生成并落地，且不等待完成），
// 因此击杀帧（0x17/经验包）先到、掉落物后到。
package gameserver

import (
	"testing"
	"time"

	c2s "mugo/internal/proto/c2s"
)

// groundCount 返回地图 0 上的地面物品 + 金币堆数。
func groundCount(srv *Server) int {
	reg := srv.deps.cfg.Drops
	return len(reg.ItemsOnMap(0)) + len(reg.MoneyOnMap(0))
}

func TestKillLootLandsOnlyAfterDelay(t *testing.T) {
	srv, sess, wp, c := newKillTestScaffold(t)
	const delay = 800 * time.Millisecond
	srv.dropDelay = delay

	before := groundCount(srv)
	killed := 0
	for _, target := range srv.deps.cfg.NPCs.ByMap(0) {
		if !target.Alive() || killed >= 8 {
			continue
		}
		wp.X, wp.Y = target.X-2, target.Y
		c.X, c.Y = wp.X, wp.Y
		for i := 0; i < 60 && target.Alive(); i++ {
			req := c2s.NewHitRequest()
			req.SetTargetId(target.ID)
			srv.handleHit(sess, req.Bytes())
		}
		if !target.Alive() {
			killed++
		}
	}
	if killed < 8 {
		t.Fatalf("击杀数不足: %d", killed)
	}
	if got := groundCount(srv); got != before {
		t.Fatalf("延时期内地面不应出现掉落（原版整体延后 1s）: before=%d got=%d", before, got)
	}

	deadline := time.Now().Add(8 * delay)
	for time.Now().Before(deadline) && groundCount(srv) == before {
		time.Sleep(20 * time.Millisecond)
	}
	if groundCount(srv) == before {
		t.Fatal("延迟到期后地面应出现掉落")
	}
}
