package player

// combat.go —— T2-2 战斗结算所需的角色属性查询（对照原版在
// AttackableExtensions 里经 IAttributeSystem 按 designation 取值的方式）。
// 无装备时 "Minimum/Maximum Physical Base Damage" 为 0——与原版空手行为一致，
// 伤害由 minLevelDmg = max(1, level/10) 下限兜底。

import (
	"fmt"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/combat"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// CombatValues 是一次战斗结算需要的角色属性快照。
type CombatValues struct {
	Level          int
	AttackRatePvM  float32
	DefenseRatePvM float32
	DefensePvM     float32
	PhysDmgMin     int
	PhysDmgMax     int
	// WizDmgMin/Max 对应原版 Stats.Minimum/MaximumWizBaseDmg（T2-11 魔法技能伤害）。
	WizDmgMin int
	WizDmgMax int
	MaxHealth float32
	// 伤害乘区（对照 AttackableExtensions.CalculateDamageAsync 末尾两个乘子）：
	// AttackDamageIncrease = 攻方"Attack Damage Increase Multiplier"（小恶魔/Dinorant/
	// 黑 Fenrir/翅膀，基值 1）；DamageReceiveMultiplier = 本角色"Damage Receive
	// Multiplier"（守护天使/守护之魂/蓝 Fenrir/黑王马，基值 1，<1 即减伤）。
	AttackDamageIncrease    float32
	DamageReceiveMultiplier float32
	// MovementSpeed 为本角色"Movement Speed"（坐骑 15/17/19）；供需要服务端速度处取用。
	MovementSpeed float32
	// MoneyAmountRate 为"Money Drop Amount Rate"（独角兽 1.5x）；未装配为 0，掉落处按 1。
	MoneyAmountRate float32
	// 伤害公式掷骰/乘区（对照 CalculateDamageAsync 攻方属性；缺省 0/基值 1）。
	CriticalChance      float32 // Critical Damage Chance
	ExcellentChance     float32 // Excellent Damage Chance
	DefenseIgnoreChance float32 // Defense Ignore Chance
	DoubleDamageChance  float32 // Double Damage Chance
	CriticalBonus       float32 // Critical Damage Bonus
	ExcellentBonus      float32 // Excellent Damage Bonus
	GreaterDamageBonus  float32 // Greater Damage Bonus
	FinalDamageBonus    float32 // Late Damage Bonus (any type)
	WizardryIncrease    float32 // Wizardry Attack Damage Increase Multiplier（基值 1）
	SkillDamageMult     float32 // Skill Damage Multiplier（职业基值，DK=2/DW=1）
	HasDoubleWield      bool    // Has Double Wield
	IsTwoHanded         bool    // Is Two-Handed Weapon Equipped
	TwoHandedIncrease   float32 // Two-Handed Weapon Physical Damage Increase
	IsDinorant          bool    // Is Dinorant Equipped（无技能普攻 ×1.3）
	// 守方减伤（本角色被击时用，对照 Calculate 防御段）。
	ArmorDamageDecrease float32 // Armor Damage Decrease（卓越/和谐/镶嵌合计）
	DefenseDecrement    float32 // Defense Decrement（基值 1）
	GreaterDefenseBonus float32 // Greater Defense Bonus
	// 恢复参数（原版 Stats.IntervalRegenerationAttributes：倍率×最大值+绝对值，
	// 按各自周期累加——HP 7s、MP 3s、AG 3s、护盾 1s，RecoveryInterval=3s 触发）。
	HealthRegenMult  float32
	ManaRegenMult    float32
	AbilityRegenMult float32
	ShieldRegenMult  float32
	HealthRegenAbs   float32
	ManaRegenAbs     float32
	AbilityRegenAbs  float32
	ShieldRegenAbs   float32
	// ShieldRecoveryEverywhere 对应 ShieldRecoveryActiveEverywhere（380 守护 option）：
	// 非 0 时护盾在安全区外也恢复（原版 ShieldRecoveryActive 的第二个加项）。
	ShieldRecoveryEverywhere float32
	// ShieldRampFactor 是静置 0s 时的护盾 ramp 因子（数据基值 4/3；随 hiatus
	// 线性上升，见 action.ShieldRampFactorAt）。
	ShieldRampFactor float32
	// 击杀怪物后恢复（原版 Stats.AfterMonsterKillRegenerationAttributes）：
	// 卓越/镶嵌/大师技能才给值，缺省 0 → 击杀不回血蓝（与原版一致）。
	ManaAfterKillMult float32
	// MuHelperActive 是属性系统里 IsMuHelperActive 的当前值（原版 MuHelper 运行时为 1）。
	MuHelperActive       float32
	ManaAfterKillAbs     float32
	HealthAfterKillMult  float32
	HealthAfterKillAbs   float32
	AbilityAfterKillMult float32
	AbilityAfterKillAbs  float32
	ShieldAfterKillMult  float32
	ShieldAfterKillAbs   float32

	// === TRIM-06：PvP / 大师 / 连击 / Soul Barrier 乘区 ===
	// PvP 选型三件套（原版只在"攻守都是玩家"时换：AttackableExtensions.cs:698-707、718-726）。
	AttackRatePvP  float32
	DefenseRatePvP float32
	DefensePvP     float32
	// Curse/Fenrir 基伤与增幅（:851-862）。本仓此前把 Curse 当 Wiz 的同一条，现分开。
	CurseDmgMin   int
	CurseDmgMax   int
	CurseIncrease float32
	FenrirBase    int
	// Berserker（狂暴）：物理/巫术/诅咒上下限加成 + 精通乘区（:108-119、:191、:818-825）。
	BerserkerMinPhys     float32
	BerserkerMaxPhys     float32
	BerserkerMinWiz      float32
	BerserkerMaxWiz      float32
	BerserkerMinCurse    float32
	BerserkerMaxCurse    float32
	BerserkerProficiency float32
	// WeaknessPhysDecrement 是"弱点"对物理伤害的减免（:195）。
	WeaknessPhysDecrement float32
	// FinalDamageIncreasePvP 只在 PvP 末段生效（:272；S6 唯一产出是守护石选项 2）。
	FinalDamageIncreasePvP float32
	// ComboBonus 是连击终结一击的加值（:288）；开关是属性 Is Skill Combo Available。
	ComboBonus     float32
	ComboAvailable bool
	// SoulBarrier* 是受击侧抵扣与每次受击的法力费（:258-266，够付才抵扣）。
	SoulBarrierReduction float32
	SoulBarrierToll      float32
	CurrentMana          float32
	// 大师树两块"按当前武器位选属性"的开关板（:958-1041），在这里解析成整数。
	MasterPhysPreBuffs  int
	MasterPhysPostBuffs int
	MasterMasteryPvP    int
}

// ResolveCombatValues 构建角色属性系统并查询战斗相关属性。
func ResolveCombatValues(cfg *config.GameConfig, c *entity.Character) (CombatValues, error) {
	return resolveCombatValues(cfg, c, nil, nil, false)
}

// DesIsResting 是"是否休息中"状态的 designation（原版 Stats.IsResting，
// 由 0x18 动画包驱动；经类关系图加成 Health/Mana 恢复倍率 0.03×IsResting）。
const DesIsResting = "Flag, if the character is resting (sitting, leaning or hanging)"

// StatIsMuHelperActive 与 muHelperActiveElement 对照原版 MuHelper.cs:23/84/105：
// 助手运行时把 ConstantElement(1) 挂到 Stats.IsMuHelperActive（GUID 见 Stats.cs:1479）上，
// 停止时移除。本仓无常驻属性系统，故与 resting/effects 一样作为快照的动态输入注入。
var (
	StatIsMuHelperActive = &attribute.AttributeDefinition{
		ID: "1FBD3CC0-DFDC-4A19-9B73-5B2DC0E12983", Designation: "Is MU Helper active"}
	muHelperActiveElement = attribute.NewConstantElement(1, attribute.AggregateAddRaw)
)

// ResolveCombatValuesFlags 在休息态与 buff 之外再注入 IsMuHelperActive 元素。
func ResolveCombatValuesFlags(cfg *config.GameConfig, c *entity.Character, resting, muHelperActive bool, effects []action.MagicEffect) (CombatValues, error) {
	var extra map[string]float32
	if resting {
		extra = map[string]float32{DesIsResting: 1}
	}
	return resolveCombatValues(cfg, c, extra, effects, muHelperActive)
}

// ResolveCombatValuesResting 按"休息中"状态构建战斗/恢复属性快照
// （原版 IsResting 是属性系统的动态输入，坐下/起身改变恢复倍率）。
func ResolveCombatValuesResting(cfg *config.GameConfig, c *entity.Character, resting bool) (CombatValues, error) {
	var extra map[string]float32
	if resting {
		extra = map[string]float32{DesIsResting: 1}
	}
	return resolveCombatValues(cfg, c, extra, nil, false)
}

// ResolveCombatValuesWithEffects 构建含活动 buff（及休息态）的战斗属性快照：
// 武器/防具/攻速等装备加成与生命之光等 buff 都进入战斗数值。
func ResolveCombatValuesWithEffects(cfg *config.GameConfig, c *entity.Character, resting bool, effects []action.MagicEffect) (CombatValues, error) {
	var extra map[string]float32
	if resting {
		extra = map[string]float32{DesIsResting: 1}
	}
	return resolveCombatValues(cfg, c, extra, effects, false)
}

func resolveCombatValues(cfg *config.GameConfig, c *entity.Character, extraOverrides map[string]float32, effects []action.MagicEffect, muHelperActive bool) (CombatValues, error) {
	if cfg == nil {
		return CombatValues{}, fmt.Errorf("player: 未注入游戏配置（GameConfig）")
	}
	overrides := charStatOverrides(c)
	for k, v := range extraOverrides {
		overrides[k] = v
	}
	// 统一系统：基底 overrides + 装备 + 活动 buff（不再直接 BuildCharacterAttributeSystem
	// 跳过装备/效果——旧路径导致武器伤害、buff 不进战斗）。
	system, err := buildWorldSystemWithOverrides(cfg, c, int(c.ClassNumber), overrides, effects)
	if err != nil {
		return CombatValues{}, err
	}
	valueOf := func(designation string) float32 {
		def, ok := cfg.AttributeByName(designation)
		if !ok {
			return 0
		}
		probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
		return system.GetValueOfAttribute(probe)
	}

	if muHelperActive {
		system.AddElement(muHelperActiveElement, StatIsMuHelperActive)
	}
	v := CombatValues{
		MuHelperActive: system.GetValueOfAttribute(StatIsMuHelperActive),
		Level:          int(valueOf("Total Level")),
		AttackRatePvM:  valueOf("Attack Rate (PvM)"),
		// 原版 GetDefenseRatePvm(:683-686) 把"格挡加成"并进防御速率，Overrates 判定也用它。
		DefenseRatePvM:          valueOf("Defense Rate (PvM)") + valueOf("Increase Block Bonus"),
		DefensePvM:              valueOf("Defense (PvM)"),
		PhysDmgMin:              int(valueOf("Minimum Physical Base Damage")),
		PhysDmgMax:              int(valueOf("Maximum Physical Base Damage")),
		WizDmgMin:               int(valueOf("Minimum Wizardry Base Damage")),
		WizDmgMax:               int(valueOf("Maximum Wizardry Base Damage")),
		MaxHealth:               valueOf("Maximum Health"),
		AttackDamageIncrease:    valueOf("Attack Damage Increase Multiplier"),
		DamageReceiveMultiplier: valueOf("Damage Receive Multiplier"),
		MovementSpeed:           valueOf("Movement Speed"),
		MoneyAmountRate:         valueOf("Money Drop Amount Rate"),
		CriticalChance:          valueOf("Critical Damage Chance"),
		ExcellentChance:         valueOf("Excellent Damage Chance"),
		DefenseIgnoreChance:     valueOf("Defense Ignore Chance"),
		DoubleDamageChance:      valueOf("Double Damage Chance"),
		CriticalBonus:           valueOf("Critical Damage Bonus"),
		ExcellentBonus:          valueOf("Excellent Damage Bonus"),
		GreaterDamageBonus:      valueOf("Greater Damage Bonus"),
		FinalDamageBonus:        valueOf("Late Damage Bonus (any type)"),
		WizardryIncrease:        valueOf("Wizardry Attack Damage Increase Multiplier"),
		SkillDamageMult:         valueOf("Skill Damage Multiplier"),
		HasDoubleWield:          valueOf("Has Double Wield") > 0,
		IsTwoHanded:             valueOf("Is Two-Handed Weapon Equipped") > 0,
		TwoHandedIncrease:       valueOf("Two-Handed Weapon Physical Damage Increase (Ancient Option)"),
		IsDinorant:              valueOf("Is Dinorant Equipped") > 0,
		ArmorDamageDecrease:     valueOf("Armor Damage Decrease"),
		DefenseDecrement:        valueOf("Defense Decrement"),
		GreaterDefenseBonus:     valueOf("Greater Defense Bonus"),
		HealthRegenMult:         valueOf("Health Recovery Multiplier"),
		ManaRegenMult:           valueOf("Mana Recovery Multiplier"),
		AbilityRegenMult:        valueOf("Ability Recovery Multiplier"),
		ShieldRegenMult:         valueOf("Shield Recovery Multiplier"),
		HealthRegenAbs:          valueOf("Health Recovery Absolute Increase"),
		ManaRegenAbs:            valueOf("Mana Recovery Absolute Increase"),
		AbilityRegenAbs:         valueOf("Ability Recovery Absolute Increase"),
		ShieldRegenAbs:          valueOf("Shield Recovery Absolute"),

		ShieldRecoveryEverywhere: valueOf("Shield Recovery Active Everywhere"),
		ShieldRampFactor:         valueOf("Shield Recovery Ramp Factor"),

		ManaAfterKillMult:    valueOf("Mana recover after Monster kill, multiplier of max mana"),
		ManaAfterKillAbs:     valueOf("Mana recover after Monster kill, absolute"),
		HealthAfterKillMult:  valueOf("Health recover after Monster kill, multiplier of max health"),
		HealthAfterKillAbs:   valueOf("Health recover after Monster kill, absolute"),
		AbilityAfterKillMult: valueOf("Ability recover after Monster kill, multiplier of max ability"),
		AbilityAfterKillAbs:  valueOf("Ability recover after Monster kill, absolute"),
		ShieldAfterKillMult:  valueOf("Shield recover after Monster kill, multiplier of max shield"),
		ShieldAfterKillAbs:   valueOf("Shield recover after Monster kill, absolute"),

		AttackRatePvP:  valueOf("Attack Rate (PvP)"),
		DefenseRatePvP: valueOf("Defense Rate (PvP)"),
		DefensePvP:     valueOf("Defense (PvP)"),
		CurseDmgMin:    int(valueOf("Minimum Curse Base Damage")),
		CurseDmgMax:    int(valueOf("Maximum Curse Base Damage")),
		CurseIncrease:  valueOf("Curse Attack Damage Increase Multiplier"),
		FenrirBase:     int(valueOf("Fenrir Base Damage")),

		BerserkerMinPhys:     valueOf("Berserker Minimum Physical Damage Bonus"),
		BerserkerMaxPhys:     valueOf("Berserker Maximum Physical Damage Bonus"),
		BerserkerMinWiz:      valueOf("Berserker Minimum Wizardry Damage Bonus"),
		BerserkerMaxWiz:      valueOf("Berserker Maximum Wizardry Damage Bonus"),
		BerserkerMinCurse:    valueOf("Berserker Minimum Curse Damage Bonus"),
		BerserkerMaxCurse:    valueOf("Berserker Maximum Curse Damage Bonus"),
		BerserkerProficiency: valueOf("Berserker Proficiency Multiplier (MST)"),

		WeaknessPhysDecrement:  valueOf("Weakness Physical Damage Decrement"),
		FinalDamageIncreasePvP: valueOf("Final Damage Increase (PvP)"),
		ComboBonus:             valueOf("Combo Bonus"),
		ComboAvailable:         valueOf("Is Skill Combo Available") > 0,
		SoulBarrierReduction:   valueOf("Soul Barrier Damage Receive Decrement"),
		SoulBarrierToll:        valueOf("Soul Barrier Mana Toll Per Received Hit"),
		CurrentMana:            valueOf("Current Mana"),
	}
	// 大师树的两块开关板（原版在伤害式里按"当前装备的武器位"现选，这里同样只读属性）。
	v.MasterPhysPreBuffs = masterPhysicalPreBuffs(valueOf)
	v.MasterPhysPostBuffs = masterPhysicalPostBuffs(valueOf)
	v.MasterMasteryPvP = masterMasteryPvPBonus(valueOf)
	if v.Level == 0 {
		v.Level = int(c.Level)
	}
	return v, nil
}

// AttackerStats 把本角色的进攻属性投影为 combat 公式入参（普攻/技能通用）。
func (v CombatValues) AttackerStats() combat.AttackerStats {
	return combat.AttackerStats{
		Level:                  v.Level,
		MinPhys:                v.PhysDmgMin,
		MaxPhys:                v.PhysDmgMax,
		MinWiz:                 v.WizDmgMin,
		MaxWiz:                 v.WizDmgMax,
		MinCurse:               v.CurseDmgMin,
		MaxCurse:               v.CurseDmgMax,
		FenrirBase:             v.FenrirBase,
		AttackRate:             v.AttackRatePvM,
		AttackRatePvP:          v.AttackRatePvP,
		CurseIncrease:          v.CurseIncrease,
		BerserkerMinPhys:       v.BerserkerMinPhys,
		BerserkerMaxPhys:       v.BerserkerMaxPhys,
		BerserkerMinWiz:        v.BerserkerMinWiz,
		BerserkerMaxWiz:        v.BerserkerMaxWiz,
		BerserkerMinCurse:      v.BerserkerMinCurse,
		BerserkerMaxCurse:      v.BerserkerMaxCurse,
		BerserkerProficiency:   v.BerserkerProficiency,
		WeaknessPhysDecrement:  v.WeaknessPhysDecrement,
		FinalDamageIncreasePvP: v.FinalDamageIncreasePvP,
		ComboBonus:             v.ComboBonus,
		MasterPhysPreBuffs:     v.MasterPhysPreBuffs,
		MasterPhysPostBuffs:    v.MasterPhysPostBuffs,
		MasterMasteryPvP:       v.MasterMasteryPvP,
		CriticalChance:         v.CriticalChance,
		ExcellentChance:        v.ExcellentChance,
		DefenseIgnoreChance:    v.DefenseIgnoreChance,
		DoubleDamageChance:     v.DoubleDamageChance,
		CriticalBonus:          v.CriticalBonus,
		ExcellentBonus:         v.ExcellentBonus,
		GreaterDamageBonus:     v.GreaterDamageBonus,
		FinalDamageBonus:       v.FinalDamageBonus,
		HasDoubleWield:         v.HasDoubleWield,
		IsTwoHanded:            v.IsTwoHanded,
		TwoHandedIncrease:      v.TwoHandedIncrease,
		WizardryIncrease:       v.WizardryIncrease,
		AttackDamageIncrease:   v.AttackDamageIncrease,
		SkillDamageMultiplier:  v.SkillDamageMult,
		IsDinorant:             v.IsDinorant,
	}
}

// DefenderStats 把本角色的防守属性投影为 combat 公式入参（被击时用）。
func (v CombatValues) DefenderStats() combat.DefenderStats {
	return combat.DefenderStats{
		Defense:                v.DefensePvM,
		DefensePvP:             v.DefensePvP,
		GreaterDefenseBonus:    v.GreaterDefenseBonus,
		DefenseDecrement:       v.DefenseDecrement,
		ArmorDamageDecrease:    v.ArmorDamageDecrease,
		DamageReceiveDecrement: v.DamageReceiveMultiplier,
		DefenseRate:            v.DefenseRatePvM,
		DefenseRatePvP:         v.DefenseRatePvP,
		SoulBarrierReduction:   v.SoulBarrierReduction,
		SoulBarrierManaToll:    v.SoulBarrierToll,
		CurrentMana:            v.CurrentMana,
	}
}
