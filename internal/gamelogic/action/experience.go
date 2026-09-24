// experience.go —— 击杀经验结算与升级决策（纯函数）。
//
// 对照原版：
//   - GameLogic/AttackableExtensions.cs `CalculateBaseExperience`（基础经验公式）
//   - GameLogic/PlayerExperience.cs `CalculateAfterKillAsync`（倍率链）
//     / `AddExperienceCoreAsync`（逐级吃经验 + 升级 + 多级连环）
//
// 倍率链（`CalculateAfterKillAsync`）：
//
//	exp = base × GameContext.ExperienceRate × (attr[Experience Rate] + attr[Bonus Experience Rate])
//	         × map.ExpMultiplier
//
// 本仓数据下三项均为 1：`GameConfiguration.ExperienceRate` 由初始化固定 1.0f、
// 地图 `ExpMultiplier` 由 BaseMapInitializer 固定 1、`Experience Rate` 职业基值 1
// 且 `Bonus Experience Rate` 无基值（=0）。随机区间（Random Experience Min/Max
// Multiplier）在初始化里没有基值 → 原版随机分支不触发，故经验是确定值。
package action

// ExperienceRateMultiplier 汇总击杀经验的三个倍率（原版三项相乘）。
// 参数由调用方从属性系统/配置读出——**不要写死在调用点**，方便将来接印章/活动。
type ExperienceRateMultiplier struct {
	ServerRate float64 // GameContext.ExperienceRate（本仓固定 1.0）
	Character  float64 // attr[Experience Rate] + attr[Bonus Experience Rate]
	Map        float64 // map.ExpMultiplier（本仓固定 1.0）
}

// Value 返回三项倍率的乘积（原版逐项相乘的等价写法）。
func (m ExperienceRateMultiplier) Value() float64 {
	v := 1.0
	for _, f := range []float64{m.ServerRate, m.Character, m.Map} {
		if f != 0 {
			v *= f
		}
	}
	return v
}

// CalculateBaseExperience 对照原版 `AttackableExtensions.CalculateBaseExperience`。
//
// 逐行复刻（float32 中间量与原版 C# `float` 语义一致，避免与 golden 向量出现末位差）：
//
//	tempExperience = (targetLevel + 25) × targetLevel / 3.0
//	if killerLevel > targetLevel + 10: tempExperience ×= (targetLevel + 10) / killerLevel
//	if targetLevel >= 65: tempExperience += (targetLevel - 64) × (targetLevel / 4)
//	return max(tempExperience, 0) × 1.25
//
// 裁剪登记：原版开头有 `IsSummonedMonster → 0`（召唤怪不给经验）——本仓尚无召唤物标记，
// 怪物定义里也没有"召唤"位，故不引入该判据。
func CalculateBaseExperience(targetLevel, killerLevel float32) float64 {
	if targetLevel <= 0 {
		return 0
	}
	temp := float64((targetLevel+25)*targetLevel) / 3.0
	if killerLevel > targetLevel+10 {
		temp *= float64((targetLevel + 10) / killerLevel)
	}
	if targetLevel >= 65 {
		temp += float64((targetLevel - 64) * (targetLevel / 4))
	}
	if temp < 0 {
		temp = 0
	}
	return temp * 1.25
}

// ExperienceStep 是结算序列中的一步，顺序与原版 `AddExperienceCoreAsync` 的循环体完全一致：
//
//	下发经验段 → （若引发升级）等级 +1 / 加升级点 / 当前值补满 / 下发等级更新 / 升级光效
//
// 也就是说 `LeveledUp` 为 true 的步，其等级更新与光效必须**紧跟在本步的经验包之后**下发；
// 跨级击杀（一次涨多级）会产生多组这样的步，不可合并成一次下发。
type ExperienceStep struct {
	// Amount 是本步下发的经验量（MaxLevelReached 步固定为 0）。
	Amount int64
	// Type 是本步的下发类型（ExperienceNormal / ExperienceMaxLevelReached）。
	Type ExperienceResult
	// LeveledUp 为 true 表示本步之后发生了升级。
	LeveledUp bool
	// Level 是升级后的等级（仅 LeveledUp 为 true 时有意义）。
	Level int
}

// ExperienceGain 是一次"加经验"的结算结果。
//
// Steps 对应原版**每级一包**的下发序列（`InvokeViewPlugInAsync<IAddExperiencePlugIn>` 在
// 循环里每次调一次）：单级内加满时不升级 → 1 步；跨级时每升一级各有自己的步，
// 最后一段是剩余经验。`MaxLevelReached` 为 true 时原版会发一个 `AddedExperience = 0`
// 的包（`ExperienceType.MaxLevelReached`）并**丢弃剩余经验**，调用方据此把 0 也下发。
type ExperienceGain struct {
	LevelsGained    int
	NewLevel        int
	NewExperience   int64
	Steps           []ExperienceStep // 按发生顺序的下发序列（含跨级拆分与升级标记）
	MaxLevelReached bool
}

// ApplyExperience 对照原版 `PlayerExperience.AddExperienceCoreAsync`。
//
// expTable 为**累计**经验表（`ExperienceTable[level]`，即该等级起点的累计经验；
// 导出件 20_experience.json 的 table 与之同源）。maxLevel 为等级上限；
// preventOverflow 对应 `GameConfiguration.PreventExperienceOverflow`（本仓默认 false）。
//
// loop 语义与原版逐行一致：先判等级上限 → 取本级所需经验 → 不足则直接返回（不升级），
// 够则**截断**到本级上限、升级、把剩余经验继续投进 while（因此一次击杀可连升多级）。
func ApplyExperience(level int, currentExp, gained int64, expTable []int64, maxLevel int, preventOverflow bool) ExperienceGain {
	g := ExperienceGain{NewLevel: level, NewExperience: currentExp}
	remaining := gained
	for remaining > 0 {
		if g.NewLevel >= maxLevel {
			// 原版：已达上限 → 只发一个 AddedExperience=0 的 MaxLevelReached 包，
			// **剩余经验直接丢弃**（不是延后入账）。
			g.MaxLevelReached = true
			g.Steps = append(g.Steps, ExperienceStep{Amount: 0, Type: ExperienceMaxLevelReached})
			return g
		}
		next := expTableFor(expTable, g.NewLevel+1)
		chunk := remaining
		levelUp := false
		if next-g.NewExperience < chunk {
			chunk = next - g.NewExperience
			levelUp = true
		}
		g.NewExperience += chunk
		step := ExperienceStep{Amount: chunk, Type: ExperienceNormal}
		if levelUp {
			g.NewLevel++
			g.LevelsGained++
			step.LeveledUp = true
			step.Level = g.NewLevel
		}
		g.Steps = append(g.Steps, step)
		if !levelUp {
			return g
		}
		remaining -= chunk
		if remaining <= 0 || g.NewLevel >= maxLevel || preventOverflow {
			return g
		}
	}
	return g
}

// expTableFor 安全取表（越界返回 0，避免调用方被迫做长度判断）。
func expTableFor(table []int64, level int) int64 {
	if level < 0 || level >= len(table) {
		return 0
	}
	return table[level]
}
