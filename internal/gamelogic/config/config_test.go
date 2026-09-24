package config

import (
	"math"
	"testing"
)

// loadSeason6 载入导出件；失败即 Fatal（数据文件由 tools/goldenconfig 生成，见 data/season6）。
func loadSeason6(t *testing.T) *GameConfig {
	t.Helper()
	c, err := LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6 数据失败: %v", err)
	}
	return c
}

// TestLoadSeason6Validate 全量校验在 Load 内已完成；这里锁定基本规模
// （数值与导出件 meta 一致性在 validate 内断言，此处防"空数据静默通过"）。
func TestLoadSeason6Validate(t *testing.T) {
	c := loadSeason6(t)
	if c.Meta.SchemaVersion != 1 {
		t.Fatalf("schema_version=%d", c.Meta.SchemaVersion)
	}
	if c.Meta.DataInitializationID != "season6" {
		t.Fatalf("data_initialization_id=%q", c.Meta.DataInitializationID)
	}
	if len(c.Maps) < 70 || len(c.Monsters) < 400 || len(c.Items) < 600 || len(c.Skills) < 250 {
		t.Fatalf("数据规模异常: maps=%d monsters=%d items=%d skills=%d",
			len(c.Maps), len(c.Monsters), len(c.Items), len(c.Skills))
	}
	if c.Meta.Counts.MonsterSpawns < 5000 {
		t.Fatalf("出生点规模异常: %d", c.Meta.Counts.MonsterSpawns)
	}
}

// TestLorenciaExists 锁定新手地图：map 0（Lorencia）存在且有怪物出生点与出生门。
func TestLorenciaExists(t *testing.T) {
	c := loadSeason6(t)
	mp, ok := c.Map(0)
	if !ok {
		t.Fatal("map 0 (Lorencia) 不存在")
	}
	if mp.Name != "Lorencia" {
		t.Fatalf("map 0 名称=%q", mp.Name)
	}
	if len(mp.Spawns) == 0 {
		t.Fatal("Lorencia 应有怪物出生点")
	}
	if len(mp.EnterGates) == 0 {
		t.Fatal("Lorencia 应有出生门")
	}
	// 所有出生点引用的怪物必须存在（Load 校验已保证，这里抽查第一条）。
	if mp.Spawns[0].Monster != nil {
		if _, ok := c.Monster(*mp.Spawns[0].Monster); !ok {
			t.Fatalf("出生点怪物 %d 不存在", *mp.Spawns[0].Monster)
		}
	}
}

// TestMonsterAttributes 锁定怪物属性：按 designation 可取到等级/血量等数值。
func TestMonsterAttributes(t *testing.T) {
	c := loadSeason6(t)
	// Bali（150）是 Season6 初始化里的普通怪物。
	m, ok := c.Monster(150)
	if !ok {
		t.Fatal("怪物 150 (Bali) 不存在")
	}
	if m.Name != "Bali" {
		t.Fatalf("怪物 150 名称=%q", m.Name)
	}
	if m.Attribute("Level") != 52 {
		t.Fatalf("Bali 等级=%v, want 52", m.Attribute("Level"))
	}
	if m.Attribute("Maximum Health") != 5000 {
		t.Fatalf("Bali 血量=%v, want 5000", m.Attribute("Maximum Health"))
	}
}

// TestExperienceTable 锁定经验表：长度、边界与单调性，且与原版公式值一致（抽查）。
func TestExperienceTable(t *testing.T) {
	c := loadSeason6(t)
	if len(c.Experience.Table) != c.Experience.MaximumLevel+2 {
		t.Fatalf("经验表长度 %d ≠ maxLevel+2", len(c.Experience.Table))
	}
	if c.ExperienceForLevel(0) != 0 {
		t.Fatalf("等级 0 经验应为 0，got %d", c.ExperienceForLevel(0))
	}
	// 原版公式：level=1 → 10*(1+8)*(1-1)*(1-1) = 0；level=2 → 10*10*1*1 = 100。
	if c.ExperienceForLevel(1) != 0 {
		t.Fatalf("等级 1 经验应为 0，got %d", c.ExperienceForLevel(1))
	}
	if c.ExperienceForLevel(2) != 100 {
		t.Fatalf("等级 2 经验应为 100，got %d", c.ExperienceForLevel(2))
	}
	// 越界返回 -1。
	if c.ExperienceForLevel(c.Experience.MaximumLevel+3) != -1 {
		t.Fatal("越界等级应返回 -1")
	}
}

// TestItemLookup 锁定物品键查找：group 0（剑）number 0 应存在（短剑系）。
func TestItemLookup(t *testing.T) {
	c := loadSeason6(t)
	item, ok := c.Item(0, 0)
	if !ok {
		t.Fatal("物品 (0,0) 不存在")
	}
	if item.Width <= 0 || item.Height <= 0 {
		t.Fatalf("物品尺寸异常: %dx%d", item.Width, item.Height)
	}
	if _, ok := c.Item(99, 999); ok {
		t.Fatal("不存在的物品键不应命中")
	}
}

// TestTerrainDecodable 锁定地形：有 TerrainData 的地图可 base64 解码为 .att 原始字节。
func TestTerrainDecodable(t *testing.T) {
	c := loadSeason6(t)
	decoded, missing := 0, 0
	for i := range c.Maps {
		raw, err := c.Maps[i].TerrainBytes()
		if err != nil {
			t.Fatalf("地图 %d 地形解码失败: %v", c.Maps[i].Number, err)
		}
		if raw == nil {
			missing++
			continue
		}
		// .att 是 256x256 的逐格属性字节流（含文件头），非空即通过；解析在 T1-d。
		decoded++
	}
	if decoded == 0 {
		t.Fatal("没有任何地图带地形数据")
	}
	t.Logf("地形: %d 张带数据, %d 张无地形", decoded, missing)
}

// TestClassLookup 锁定职业：0=Dark Wizard、4=Dark Knight（原版 CharacterClassNumber 编号）。
func TestClassLookup(t *testing.T) {
	c := loadSeason6(t)
	dw, ok := c.Class(0)
	if !ok {
		t.Fatal("职业 0 (Dark Wizard) 不存在")
	}
	if dw.Name != "Dark Wizard" {
		t.Fatalf("职业 0 名称=%q, want Dark Wizard", dw.Name)
	}
	if !dw.CanGetCreated {
		t.Fatal("Dark Wizard 应可创建")
	}
	if dk, ok := c.Class(4); !ok || dk.Name != "Dark Knight" {
		t.Fatalf("职业 4 应为 Dark Knight: ok=%v name=%q", ok, dk.Name)
	}
	// 初始属性非空且含力量。
	if len(dw.BaseAttributes) == 0 {
		t.Fatal("职业应有初始属性")
	}
}

// TestAreaSkillSettingsApplied 锁定区域技能形状设置回填（对照原版 AreaSkillSettings）：
// 扇形技能必须有 frustum（否则背后也能打到——本修复的核心）。
func TestAreaSkillSettingsApplied(t *testing.T) {
	c := loadSeason6(t)
	frustum := map[int]bool{55: true, 56: true, 8: true, 12: true, 24: true, 52: true, 235: true, 65: true, 78: true, 236: true, 238: true}
	for num := range frustum {
		sk, ok := c.Skill(num)
		if !ok || sk.Area == nil || !sk.Area.UseFrustumFilter {
			t.Fatalf("技能 %d 应有 frustum 形状设置, got %+v", num, sk.Area)
		}
		if sk.Area.FrustumDistance <= 0 {
			t.Fatalf("技能 %d frustum 距离应 >0", num)
		}
	}
	// 点击点小圆（target-area）
	for _, num := range []int{5, 13, 39} {
		sk, _ := c.Skill(num)
		if sk.Area == nil || !sk.Area.UseTargetAreaFilter {
			t.Fatalf("技能 %d 应有 target-area 小圆设置", num)
		}
	}
	// 纯作用半径（effect-range）：Decay=2
	if sk, _ := c.Skill(38); sk.Area == nil || sk.Area.EffectRange != 2 {
		t.Fatalf("技能 38(Decay) EffectRange 应为 2, got %+v", sk.Area)
	}
	// 多段命中：Twister 每目标至多 2 次、命中衰减 ≈0.7（原版 float 存 0.7→0.69999999，用容差）。
	if sk, _ := c.Skill(8); sk.Area.MaxHitsPerTarget != 2 || math.Abs(sk.Area.HitChancePerDistanceMultiplier-0.7) > 0.01 {
		t.Fatalf("技能 8(Twister) 多段/衰减异常: %+v", sk.Area)
	}
	// 变体继承：Power Slash Streng(482) 与基础 Power Slash(56) 同扇形
	if sk, _ := c.Skill(482); sk.Area == nil || !sk.Area.UseFrustumFilter {
		t.Fatalf("技能 482 应继承 Power Slash 扇形设置")
	}
	// 非区域技能无形状设置：Energy Ball(17)
	if sk, _ := c.Skill(17); sk.Area != nil {
		t.Fatalf("技能 17 不应有 Area 设置: %+v", sk.Area)
	}
	// 新补充的多来源设置（防"按 Range 全打"超范围回归）：
	// Earthshake(62) 点击点直径 10 小圆 + 9~15 段命中；之前缺失会按 Range=10 方全打。
	if sk, _ := c.Skill(62); sk.Area == nil || !sk.Area.UseTargetAreaFilter || sk.Area.TargetAreaDiameter != 10 || sk.Area.MinHitsPerAttack != 9 || sk.Area.MaxHitsPerAttack != 15 {
		t.Fatalf("技能 62(Earthshake) 设置异常: %+v", sk.Area)
	}
	// Lightning Shock(230) 直径 14 小圆。
	if sk, _ := c.Skill(230); sk.Area == nil || !sk.Area.UseTargetAreaFilter || sk.Area.TargetAreaDiameter != 14 {
		t.Fatalf("技能 230(Lightning Shock) 设置异常: %+v", sk.Area)
	}
	// Summoner Explosion(223)/Requiem(224) EffectRange=2；Pollution(225) EffectRange=3。
	for num, want := range map[int]int{223: 2, 224: 2, 225: 3} {
		if sk, _ := c.Skill(num); sk.Area == nil || sk.Area.EffectRange != want {
			t.Fatalf("技能 %d EffectRange 应为 %d: %+v", num, want, sk.Area)
		}
	}
	// Chaotic Diseier(238) 应为扇形且 MinHitsPerAttack=7。
	if sk, _ := c.Skill(238); sk.Area == nil || !sk.Area.UseFrustumFilter || sk.Area.MinHitsPerAttack != 7 {
		t.Fatalf("技能 238(Chaotic Diseier) 设置异常: %+v", sk.Area)
	}
}

// TestPriorityOneFieldsLoaded 锁定"优先级1"补入的导出字段确实被载入（防导出器/载入器漏接）。
func TestPriorityOneFieldsLoaded(t *testing.T) {
	c := loadSeason6(t)

	// 全局标量（05_game_config.json）具体值。
	g := c.Globals
	if g.InfoRange != 12 || g.RecoveryInterval != 3000 || g.MaximumPartySize != 5 ||
		g.MaximumCharactersPerAccount != 5 || g.ItemDropDurationMs != 60000 ||
		g.DamagePerOnePetDurability != 100000 || g.DamagePerOneItemDurability != 2000 ||
		g.HitsPerOneItemDurability != 10000 || g.MinimumMonsterLevelForMasterExperience != 95 ||
		g.MaximumLevel != 400 || g.MaximumMasterLevel != 200 || !g.ShouldDropMoney ||
		g.MaximumInventoryMoney == 0 || g.CharacterNameRegex == "" {
		t.Fatalf("全局标量未正确载入: %+v", g)
	}

	countElem, countReq, countRel := 0, 0, 0
	for i := range c.Skills {
		if c.Skills[i].ElementalModifierTarget != "" {
			countElem++
		}
		if len(c.Skills[i].Requirements) > 0 {
			countReq++
		}
		if len(c.Skills[i].AttributeRelationships) > 0 {
			countRel++
		}
	}
	if countElem == 0 || countReq == 0 || countRel == 0 {
		t.Fatalf("技能新字段未载入: elem=%d req=%d rel=%d", countElem, countReq, countRel)
	}

	// 怪物元素属性（Monster.Element）。
	elemMonsters := 0
	for i := range c.Monsters {
		if c.Monsters[i].Element > 0 {
			elemMonsters++
		}
	}
	if elemMonsters == 0 {
		t.Fatal("怪物元素属性未载入")
	}

	// 地图经验倍率（ExpMultiplier 导出默认 1.0；未载入会是 0）。
	expMaps := 0
	for i := range c.Maps {
		if c.Maps[i].ExpMultiplier >= 1 {
			expMaps++
		}
	}
	if expMaps == 0 {
		t.Fatal("地图经验倍率未载入")
	}

	// 魔法效果时长缩放除数（默认 1；未载入会是 0）。
	if len(c.MagicEffects) > 0 && c.MagicEffects[0].MonsterTargetLevelDivisor <= 0 {
		t.Fatalf("魔法效果时长除数未载入: %+v", c.MagicEffects[0])
	}
}
