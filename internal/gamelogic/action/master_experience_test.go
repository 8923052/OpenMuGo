package action

import (
	"testing"

	"mugo/internal/gamelogic/config"
)

// master_experience_test.go —— 大师经验分支（TRIM-09c），对照
// PlayerExperience.AddMasterExperienceCoreAsync：两个门 + **一次击杀最多升一级**。

func masterTable(t *testing.T) ([]int64, int, int) {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入导出件失败: %v", err)
	}
	return cfg.Experience.MasterTable, cfg.Experience.MaximumMasterLevel,
		cfg.Globals.MinimumMonsterLevelForMasterExperience
}

func TestApplyMasterExperienceGates(t *testing.T) {
	table, maxMaster, minMonster := masterTable(t)
	// 门 1：大师等级已满 → Amount=0 的 32 号结果包（原版仍下发）。
	g := ApplyMasterExperience(uint16(maxMaster), table[maxMaster], 5000, table, maxMaster, 200, minMonster)
	if g.Kind != ExperienceMaxMasterReached || g.Amount != 0 || g.LeveledUp {
		t.Fatalf("满级门 got %+v", g)
	}
	// 门 2：被杀怪物等级低于 95 → 33 号结果包。
	g = ApplyMasterExperience(3, table[3], 5000, table, maxMaster, minMonster-1, minMonster)
	if g.Kind != ExperienceMonsterTooLow || g.Amount != 0 || g.NewExperience != table[3] {
		t.Fatalf("低等级怪门 got %+v", g)
	}
	// 恰好等于门槛的怪可给大师经验（原版是 < 判定）。
	g = ApplyMasterExperience(3, table[3], 5000, table, maxMaster, minMonster, minMonster)
	if g.Kind != ExperienceMaster || g.Amount != 5000 {
		t.Fatalf("门槛等级的怪应正常入账, got %+v", g)
	}
	// 无击杀对象（monsterLevel<0）时跳过该门（原版 `killedObject is not null`）。
	g = ApplyMasterExperience(3, table[3], 5000, table, maxMaster, -1, minMonster)
	if g.Kind != ExperienceMaster {
		t.Fatalf("无对象时不该走低等级门, got %+v", g)
	}
}

func TestApplyMasterExperienceLevelUp(t *testing.T) {
	table, maxMaster, minMonster := masterTable(t)
	// 不够升级：全额入账、不升级。
	g := ApplyMasterExperience(0, 0, 1000, table, maxMaster, 200, minMonster)
	if g.Kind != ExperienceMaster || g.Amount != 1000 || g.LeveledUp || g.NewExperience != 1000 {
		t.Fatalf("不足门槛时 got %+v", g)
	}
	// 越过门槛：**截断**到门槛值并升一级，多出的部分丢弃（与 ApplyExperience 的连环升级不同）。
	gained := table[1] + 1
	g = ApplyMasterExperience(0, 0, gained, table, maxMaster, 200, minMonster)
	if !g.LeveledUp || g.NewMasterLevel != 1 || g.Amount != table[1] || g.NewExperience != table[1] {
		t.Fatalf("越门槛应截断并只升 1 级, got %+v (table[1]=%d)", g, table[1])
	}
	// 恰好等于门槛值：原版用 `<` 判定 → 不升级，经验正好满。
	g = ApplyMasterExperience(0, 0, table[1], table, maxMaster, 200, minMonster)
	if g.LeveledUp || g.Amount != table[1] {
		t.Fatalf("恰好等于门槛时不该升级（原版 < 判定）, got %+v", g)
	}
	// 满经验差 1 点即升级：与上一条形成对照。
	g = ApplyMasterExperience(0, 0, table[1]+1, table, maxMaster, 200, minMonster)
	if !g.LeveledUp {
		t.Fatalf("多 1 点应升级, got %+v", g)
	}
}
