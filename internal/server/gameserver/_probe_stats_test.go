package gameserver

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
)

func TestProbeStats(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	for _, cls := range []byte{0, 1, 2, 3, 4, 5, 6, 7} {
		for _, lv := range []uint16{1, 91, 200, 400} {
			c := &entity.Character{Name: "p", ClassNumber: cls, Level: lv}
			real, err := player.ResolveCharStats(cfg, c)
			if err != nil {
				t.Fatalf("class=%d lv=%d: %v", cls, lv, err)
			}
			old := player.NewCharStats(cls, lv)
			t.Logf("class=%d lv=%d REAL hp=%d mp=%d ag=%d sd=%d | OLD hp=%d mp=%d ag=%d sd=%d",
				cls, lv,
				real.MaximumHealth, real.MaximumMana, real.MaximumAbility, real.MaximumShield,
				old.MaximumHealth, old.MaximumMana, old.MaximumAbility, old.MaximumShield)
		}
	}
}
