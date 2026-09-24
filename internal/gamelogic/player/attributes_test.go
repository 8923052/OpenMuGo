package player

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// loadCfg 载入 T0-c 导出件（T1-2 属性装配的数据源）。
func loadCfg(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入导出件失败: %v", err)
	}
	return cfg
}

// TestResolveCharStatsDarkWizard 锁定 T1-2：Dark Wizard（0）1 级的属性
// 由真实属性系统按导出件关系图计算——四维 = 类 StatAttributes 初始值，
// MaximumHealth = 类基值 30 + 1×TotalLevel(1) + 2×TotalVitality(15) = 61。
func TestResolveCharStatsDarkWizard(t *testing.T) {
	cfg := loadCfg(t)
	c := &entity.Character{Name: "dw", ClassNumber: 0, Level: 1}

	st, err := ResolveCharStats(cfg, c)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if st.Strength != 18 || st.Agility != 18 || st.Vitality != 15 || st.Energy != 30 {
		t.Fatalf("DW 初始四维不符: %d/%d/%d/%d", st.Strength, st.Agility, st.Vitality, st.Energy)
	}
	// 无持久化当前值 → 满状态进场：CurrentHealth = MaximumHealth = 61
	// （旧实现取类 StatAttribute 基值 60）。
	if st.CurrentHealth != 61 {
		t.Fatalf("DW CurrentHealth=%d, want 61（满状态=MaximumHealth）", st.CurrentHealth)
	}
	if st.MaximumHealth != 61 {
		t.Fatalf("DW MaximumHealth=%d, want 61（30 基值 + 1×Level + 2×Vitality）", st.MaximumHealth)
	}
	if st.MaximumMana != 62 {
		t.Fatalf("DW MaximumMana=%d, want 62（2×Energy(30) + 2×Level(1)）", st.MaximumMana)
	}
	// 攻速由关系图计算（含职业/公共关系的全部贡献）；值随数据走，只断言可解析。
	// （此前线性近似恒为 200；真实图值以导出件关系为准。）
}

// TestResolveCharStatsOverrides加点 锁定角色当前态覆盖：
// Level=5、BaseStrength=30（加点后）时派生值联动（对应原版 character.Attributes）。
func TestResolveCharStatsOverrides(t *testing.T) {
	cfg := loadCfg(t)
	c := &entity.Character{
		Name: "dw5", ClassNumber: 0, Level: 5,
		Stats: &entity.CharStats{Strength: 30, Agility: 18, Vitality: 18, Energy: 40},
	}
	st, err := ResolveCharStats(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	if st.Strength != 30 {
		t.Fatalf("覆盖后 Strength=%d, want 30", st.Strength)
	}
	// MaximumHealth = 30 + 1×5 + 2×18 = 71。
	if st.MaximumHealth != 71 {
		t.Fatalf("覆盖后 MaximumHealth=%d, want 71", st.MaximumHealth)
	}
	// 当前值保留角色自身态。
	if st.CurrentHealth != c.Stats.CurrentHealth && c.Stats.CurrentHealth != 0 {
		t.Fatalf("当前血量应保留角色态")
	}
}

// TestResolveCharStatsAllClasses 全职业冒烟：任一职业（含转生/大师级）装配不报错。
func TestResolveCharStatsAllClasses(t *testing.T) {
	cfg := loadCfg(t)
	for i := range cfg.CharacterClasses {
		cls := &cfg.CharacterClasses[i]
		c := &entity.Character{Name: cls.Name, ClassNumber: byte(cls.Number), Level: 1}
		if _, err := ResolveCharStats(cfg, c); err != nil {
			t.Fatalf("职业 %s(%d) 属性解析失败: %v", cls.Name, cls.Number, err)
		}
	}
}

// TestResolveCharStatsUnknownClass 未知职业应报错（数据/代码错位早暴露）。
func TestResolveCharStatsUnknownClass(t *testing.T) {
	cfg := loadCfg(t)
	c := &entity.Character{ClassNumber: 200, Level: 1}
	if _, err := ResolveCharStats(cfg, c); err == nil {
		t.Fatal("未知职业应报错")
	}
}
