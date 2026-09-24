package remote

// player_view_stats_test.go —— 角色属性包（F3 03）里"扩展背包页数"的字节锚定。
//
// 对照 OpenMU：CharacterInformationExtended（[MinimumClient(106,3)] 的 S6E3 形态）把
// InventoryExtensions 放在字节 [88]，紧凑形态 CharacterInformation 放在 [68]
// （ServerToClientPackets.cs:16901/17415）。客户端按这个字节画扩展页 —— 容器有页而
// 该字节为 0 时页上物品不可见，故必须钉住。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	s2c "mugo/internal/proto/s2c"
)

func TestCharacterInformationCarriesInventoryExtensions(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	c := &entity.Character{Name: "invextDk", Level: 300, ClassNumber: 7, X: 100, Y: 100}
	st := &entity.CharStats{InventoryExtensions: 2, Money: 1000}
	if err := view.ShowCharacterInformation(action.CharacterInformation{Character: c, Stats: st}); err != nil {
		t.Fatal(err)
	}
	if len(rec.frames) != 1 {
		t.Fatalf("应发 1 帧, got %d", len(rec.frames))
	}
	f := rec.frames[0]
	p := s2c.AsCharacterInformationExtended(f)
	if p.InventoryExtensions() != 2 {
		t.Fatalf("扩展页数应为 2, got %d", p.InventoryExtensions())
	}
	if f[88] != 2 {
		t.Fatalf("InventoryExtensions 应写在字节 [88], got %d", f[88])
	}
}
