package action

// master_experience.go —— 大师经验入账与大师升级（纯函数，对照
// GameLogic/PlayerExperience.cs `AddMasterExperienceCoreAsync` :151-195）。
//
// 与普通升级的两处根本差别（勿"顺手统一"）：
//   - **一次击杀最多升一级**：原版没有 while 连环，够门槛就把经验截断到门槛值；
//   - 两个前置门（大师等级已满 / 被杀怪物等级过低）各自下发一个 Amount=0 的
//     特殊结果包（C3 16 的 32 / 33），而不是静默吞掉。

// MasterExperienceGain 是一次大师经验入账的结果。
type MasterExperienceGain struct {
	// Amount 是实际入账的经验（被截断时为门槛差额；两个门为 0）。
	Amount int64
	// Kind 是下发类型（ExperienceMaxMasterReached / ExperienceMonsterTooLow / ExperienceMaster）。
	Kind ExperienceResult
	// LeveledUp 为 true 时调用方需执行大师升级（等级 +1、加点、补满、下发 F3 51 与光效）。
	LeveledUp bool
	// NewExperience / NewMasterLevel 是落地后的值（未变更时与入参相同）。
	NewExperience  int64
	NewMasterLevel uint16
}

// ApplyMasterExperience 对应 AddMasterExperienceCoreAsync。
//
// masterTable 是**累计**大师经验表（导出件 20_experience.json 的 master_table，
// 与原版 GameContext.CreateExpTable(MasterExperienceFormula, MaximumMasterLevel) 同源）；
// maxMasterLevel 对应 GameConfiguration.MaximumMasterLevel（S6=200）；
// monsterLevel 是被杀对象的等级，minMonsterLevel 对应
// MinimumMonsterLevelForMasterExperience（S6=95）；monsterLevel < 0 表示"无击杀对象"
// （原版 `killedObject is not null` 分支，此时跳过等级门）。
func ApplyMasterExperience(masterLevel uint16, currentExp, gained int64, masterTable []int64,
	maxMasterLevel, monsterLevel, minMonsterLevel int) MasterExperienceGain {
	g := MasterExperienceGain{NewExperience: currentExp, NewMasterLevel: masterLevel}
	if int(masterLevel) >= maxMasterLevel {
		g.Kind = ExperienceMaxMasterReached
		return g
	}
	if monsterLevel >= 0 && monsterLevel < minMonsterLevel {
		g.Kind = ExperienceMonsterTooLow
		return g
	}
	amount := gained
	if next := expTableFor(masterTable, int(masterLevel)+1); next-currentExp < amount {
		amount = next - currentExp
		g.LeveledUp = true
	}
	g.Amount = amount
	g.Kind = ExperienceMaster
	g.NewExperience = currentExp + amount
	if g.LeveledUp {
		g.NewMasterLevel = masterLevel + 1
	}
	return g
}
