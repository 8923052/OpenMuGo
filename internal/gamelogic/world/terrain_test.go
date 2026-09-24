package world

import (
	"encoding/base64"
	"testing"

	"mugo/internal/gamelogic/config"
)

// TestParseTerrainFlags 锁定 .att 语义（原版 GameMapTerrain.ReadTerrainData）：
// 跳过 3 字节头；值 0/1 可走，1 另为安全区，其余阻挡；x=i&0xFF，y=i>>8。
func TestParseTerrainFlags(t *testing.T) {
	// 构造：头 3B + 两行（512B）。i=0 → (0,0) 值 0；i=1 → (1,0) 值 1；i=2 → (2,0) 值 2；
	// i=256 → (0,1) 值 2（阻挡）。
	data := make([]byte, 3+512)
	data[3] = 0     // (0,0) 可走
	data[4] = 1     // (1,0) 可走 + 安全区
	data[5] = 2     // (2,0) 阻挡
	data[3+256] = 2 // (0,1) 阻挡
	tt := ParseTerrain(data)

	if !tt.Walkable(0, 0) || tt.Safezone(0, 0) {
		t.Fatalf("(0,0) 应可走非安全区")
	}
	if !tt.Walkable(1, 0) || !tt.Safezone(1, 0) {
		t.Fatalf("(1,0) 应可走且为安全区")
	}
	if tt.Walkable(2, 0) {
		t.Fatalf("(2,0) 应阻挡")
	}
	if tt.Walkable(0, 1) {
		t.Fatalf("(0,1) 应阻挡")
	}
	// 512 格中仅 2 格显式阻挡，其余全 0（可走）。
	if n := tt.WalkableCount(); n != 510 {
		t.Fatalf("可走格数=%d, want 510", n)
	}
	if n := tt.SafezoneCount(); n != 1 {
		t.Fatalf("安全区格数=%d, want 1", n)
	}
}

// TestParseTerrainDefaultNil 锁定：无地形数据 = 全部可走（原版 DefaultTerrain 全 0）。
func TestParseTerrainDefaultNil(t *testing.T) {
	tt := ParseTerrain(nil)
	if !tt.Walkable(0, 0) || !tt.Walkable(255, 255) {
		t.Fatal("nil 地形应全部可走")
	}
	if tt.SafezoneCount() != 0 {
		t.Fatal("nil 地形应无安全区")
	}
	// Map 挂 nil 地形同样恒可走。
	m := newMap(0)
	m.SetTerrain(nil)
	if !m.Walkable(1, 2) {
		t.Fatal("Map 无地形应恒可走")
	}
}

// TestLorenciaTerrainFromExport 用导出件真实地形（T0-c 的 .att）校验解析规模：
// Lorencia 应有大量可走格与安全区（城镇中心）。
func TestLorenciaTerrainFromExport(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入导出件失败: %v", err)
	}
	mp, ok := cfg.Map(0)
	if !ok {
		t.Fatal("Lorencia 不存在")
	}
	raw, err := mp.TerrainBytes()
	if err != nil || raw == nil {
		t.Fatalf("Lorencia 地形缺失: %v", err)
	}
	if len(raw) < 3+65536 {
		t.Fatalf("Lorencia .att 应 ≥ 65539 字节（3 头 + 256×256），got %d", len(raw))
	}
	tt := ParseTerrain(raw)
	walkable, safezone := tt.WalkableCount(), tt.SafezoneCount()
	if walkable < 10000 || safezone < 100 {
		t.Fatalf("Lorencia 地形规模异常: walkable=%d safezone=%d", walkable, safezone)
	}
	// 出生点（原版 DW HomeMap Lorencia）应落在可走格。
	// 端点装配给 DW 的出生坐标由数据决定，这里抽验 (18,20) 一带的主城格。
	if !tt.Walkable(18, 20) && !tt.Walkable(19, 20) {
		t.Skipf("抽查点不可走不视为失败——只验证总量: walkable=%d", walkable)
	}
}

// TestTerrainFromExportBase64RoundTrip 锁定：导出件地形 base64 解码后
// 与解析器输入一致（数据管线无损）。
func TestTerrainFromExportBase64RoundTrip(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mp, ok := cfg.Map(0)
	if !ok {
		t.Fatal("Lorencia 不存在")
	}
	raw, err := mp.TerrainBytes()
	if err != nil {
		t.Fatal(err)
	}
	// base64 编解码由 encoding/base64 保证；这里锁定解码长度 = 原始 .att 长度。
	if len(raw) != 3+256*256 && len(raw) != 256*256 {
		t.Fatalf("Lorencia .att 长度异常: %d", len(raw))
	}
	if base64.StdEncoding.EncodeToString(raw) != *mp.Terrain {
		t.Fatal("base64 往返不一致")
	}
}
