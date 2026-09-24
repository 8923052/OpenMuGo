package combat

// damage.go —— CalculateDamageAsync 的完整 PvM 移植。输入是攻/守双方"结算后的属性值"
// （由调用方从角色属性系统 / 怪物属性读出后填入），本函数只做纯计算与掷骰，不碰世界/背包。

import "mugo/internal/util"

// DamageType 与 action 层常量一致（原版 DataModel/Configuration/Skill.cs:14-45 的 DamageType）。
const (
	TypeNone            = -1 // 原版 DamageType.None（导出件里 139 条技能是它）
	TypePhysical        = 0
	TypeWizardry        = 1
	TypeCurse           = 2
	TypeSummonedMonster = 3
	TypeFenrir          = 4
)

// AttackerStats 是攻方参与伤害公式的属性。倍率类字段（AttackDamageIncrease、
// SkillDamageMultiplier、WizardryIncrease、CurseIncrease）为 0 时按 1 处理（属性未装配不改变结果）。
type AttackerStats struct {
	Level         int
	MinPhys       int
	MaxPhys       int
	MinWiz        int
	MaxWiz        int
	MinCurse      int // 原版 Curse 分支读 MinimumCurseBaseDmg（不是 Wiz 的那对）
	MaxCurse      int
	FenrirBase    int     // Fenrir 伤害类型唯一的那对基伤（:859-862）
	AttackRate    float32 // PvM
	AttackRatePvP float32 // 玩家打玩家时才用（GetHitChanceTo :698-707）

	CriticalChance      float32
	ExcellentChance     float32
	DefenseIgnoreChance float32
	DoubleDamageChance  float32

	CriticalBonus      float32
	ExcellentBonus     float32
	GreaterDamageBonus float32
	FinalDamageBonus   float32

	HasDoubleWield    bool
	TwoHandedIncrease float32 // 双手武器加伤（IsTwoHandedWeaponEquipped>0 时生效）
	IsTwoHanded       bool

	WizardryIncrease      float32 // 魔法基础伤害增幅（默认 1）
	CurseIncrease         float32 // 诅咒基础伤害增幅（默认 1；原版与 Wiz 不同属性）
	AttackDamageIncrease  float32 // 末尾增伤乘区（默认 1）
	SkillDamageMultiplier float32 // 技能伤害倍率（默认 1）
	IsDinorant            bool    // 无技能时 Dinorant ×1.3

	// 渡鸦（AttackerSurrogate）专属：物理末段 +=RavenBonusDamage，且非暴非卓越 ÷1.5。
	IsRaven          bool
	RavenBonusDamage float32

	// Berserker（狂暴效果自身提供的上下限加成与巫术/诅咒精通乘区）。
	// 原版在 :108/113/118-119 加物理、:191 给巫术与诅咒乘区、:818-825 给召唤系基伤。
	BerserkerMinPhys     float32
	BerserkerMaxPhys     float32
	BerserkerMinWiz      float32
	BerserkerMaxWiz      float32
	BerserkerMinCurse    float32
	BerserkerMaxCurse    float32
	BerserkerProficiency float32

	// WeaknessPhysDecrement 是"弱点"效果对物理伤害的减免（:195）。
	WeaknessPhysDecrement float32

	// 大师树的两块开关板由调用方按"当前武器位"算好后传入
	// （原版 GetMasterSkillTreePhysicalPassiveDamageBonus :958-1013 与
	// GetMasterSkillTreeMasteryPvpDamageBonus :1015-1041；属性读取见 player 包）。
	MasterPhysPreBuffs  int // :138 之前那段（狂暴/卓越/暴击加成的"前置阶段"）
	MasterPhysPostBuffs int // :196 之后那段（含 Master Skill Physical Bonus Damage (MST)）
	MasterMasteryPvP    int // :273，仅 PvP 且非渡鸦

	// FinalDamageIncreasePvP 是 PvP 专属的最终增伤（:272；S6 唯一产出点是守护石选项 2）。
	FinalDamageIncreasePvP float32

	// ComboBonus 是连击终结一击的加值（:288；由 ComboStateMachine 判定 isCombo 后才加）。
	ComboBonus float32
}

// DefenderStats 是守方（怪物为主）参与公式的属性。DefenseDecrement/DamageReceiveDecrement
// 为 0 时按 1 处理。
type DefenderStats struct {
	Defense                float32 // 防御属性原值（PvM）
	DefensePvP             float32 // 玩家防守时才选这条（GetDefenseAttribute :718-726）
	GreaterDefenseBonus    float32
	DefenseDecrement       float32
	ArmorDamageDecrease    float32 // 卓越/和谐/镶嵌减伤合计
	DamageReceiveDecrement float32
	DefenseRate            float32 // 用于 Overrates 判定（含 Increase Block Bonus，由调用方合并）
	DefenseRatePvP         float32

	// Soul Barrier：只对"有法力可付"的目标生效（:258-266），抵扣量钳在 0..0.9。
	SoulBarrierReduction float32
	SoulBarrierManaToll  float32
	CurrentMana          float32
}

// Match 描述这一击的对抗关系。原版从对象种类在函数内部判断（:75-79），
// 本仓由调用方（知道攻/守各是谁）填好，保持 combat 层"数字进数字出"。
type Match struct {
	// IsPvP 对应 `attacker 是玩家 && defender 是玩家`。
	IsPvP bool
	// DuelDecrement 是原版的 duelDmgDec：只在"与对手的古典 PvP（对方无盾）"时为 0.6。
	// 本仓没有决斗房间（DuelRoom 属 doc/13 进阶子系统），故调用方目前恒传 1；
	// 保留这个乘区是为了公式位置与原版逐行对得上（:137/160/166/181）。
	DuelDecrement float64
	// IsCombo 对应原版的 isCombo 入参：连击状态机判定这一手是终结击时才置位，
	// 效果是 dmg += ComboBonus（:286-290）。
	IsCombo bool
}

// Skill 是本次攻击携带的技能参数（Used=false 表示普通攻击）。
type Skill struct {
	Used            bool
	DamageType      int
	ExtraMin        int // GetSkillDmg：技能附加最小伤害（= AttackDamage）
	ExtraMax        int // GetSkillDmg：技能附加最大伤害（= AttackDamage×1.5）
	FinalBonus      float32
	FinalMultiplier float32 // >0 时覆盖 AttackerStats.SkillDamageMultiplier
	DamageFactor    float64 // 多段/区域伤害系数（默认 1）
	// Number 是技能号；265（Dragon Slasher）在 PvM 下倍率 ×3（:240-243）。
	Number int
}

// Calculate 逐行复刻 CalculateDamageAsync（:68-307），返回最终伤害与颜色类型。
// duel 传入的 DuelDecrement 参与 :137/160/166/181 那一乘区。
func Calculate(rng *util.Rand, m Match, atk AttackerStats, def DefenderStats, sk Skill) DamageResult {
	// 1. 命中判定：PvP 与 PvM 用**不同的**攻/防速率（:694-716）。
	if !rollHit(rng, HitChance(m.IsPvP, atk, def)) {
		return DamageResult{Miss: true}
	}

	// 2. 暴击 / 卓越 / 破防 掷骰（:82-84）。
	isCrit := rollChance(rng, atk.CriticalChance)
	isExc := rollChance(rng, atk.ExcellentChance)
	isIgnore := rollChance(rng, atk.DefenseIgnoreChance)

	// 3. 防御（含破防、GreaterDefenseBonus、DefenseDecrement）；属性按 PvP/PvM 选型（:86-99）。
	defense := 0
	if !isIgnore {
		base := def.Defense
		if m.IsPvP {
			base = def.DefensePvP
		}
		dec := def.DefenseDecrement
		if dec <= 0 {
			dec = 1
		}
		defense = int((base + def.GreaterDefenseBonus) * dec)
		if defense < 0 {
			defense = 0
		}
	}

	kind := KindNormal
	if isIgnore {
		kind = KindIgnoreDefense
	}

	duelDec := m.DuelDecrement
	if duelDec == 0 {
		duelDec = 1
	}

	// 4. 基础伤害区间（GetBaseDmg :837-873）：Curse 用自己的那对基伤与自己的增幅；
	// None/SummonedMonster 走原版 switch 的 default（基伤保持 0）。
	damageType := TypePhysical
	skillMin, skillMax := 0, 0
	if sk.Used {
		damageType = sk.DamageType
		skillMin, skillMax = sk.ExtraMin, sk.ExtraMax
	}
	var baseMin, baseMax int
	switch damageType {
	case TypePhysical:
		baseMin, baseMax = atk.MinPhys+skillMin, atk.MaxPhys+skillMax
	case TypeWizardry:
		wiz := atk.WizardryIncrease
		if wiz <= 0 {
			wiz = 1
		}
		baseMin = int(float32(atk.MinWiz+skillMin) * wiz)
		baseMax = int(float32(atk.MaxWiz+skillMax) * wiz)
	case TypeCurse:
		curse := atk.CurseIncrease
		if curse <= 0 {
			curse = 1
		}
		baseMin = int(float32(atk.MinCurse+skillMin) * curse)
		baseMax = int(float32(atk.MaxCurse+skillMax) * curse)
	case TypeFenrir:
		baseMin = atk.FenrirBase + skillMin
		baseMax = atk.FenrirBase + skillMax
	}

	// 5. 按伤害类型与暴击/卓越选值并减防。物理与巫术的**运算顺序不同**（:103-183），
	// 尤其非物理卓越是"先减防再 ×1.2"。
	var dmg int
	if damageType == TypePhysical {
		switch {
		case isExc:
			dmg = int(float32(baseMax)*1.2+atk.ExcellentBonus) +
				int(atk.CriticalBonus+atk.BerserkerMaxPhys)
			kind = KindExcellent
		case isCrit:
			dmg = baseMax + int(atk.CriticalBonus+atk.BerserkerMaxPhys)
			kind = KindCritical
		default:
			lo, hi := baseMin+int(atk.BerserkerMinPhys), baseMax+int(atk.BerserkerMaxPhys)
			dmg = randInt(rng, lo, hi)
		}
		dmg += atk.MasterPhysPreBuffs // :138 的前半段（开关板按武器位）
		if atk.HasDoubleWield {
			dmg += dmg // 双持 = 200%（:131-135，注释写的 110% 是错的）
		}
		dmg = int(float64(dmg)*duelDec) - defense
		if atk.IsTwoHanded {
			dmg += int(float32(dmg) * atk.TwoHandedIncrease)
		}
		if atk.IsRaven {
			dmg += int(atk.RavenBonusDamage)
			if kind != KindExcellent && kind != KindCritical {
				dmg = int(float32(dmg) / 1.5)
			}
		}
	} else {
		// Wizardry / Curse。
		switch {
		case isExc:
			dmg = int(float64(baseMax)*duelDec) - defense
			dmg = int(float32(dmg)*1.2) + int(atk.ExcellentBonus)
			kind = KindExcellent
		case isCrit:
			dmg = int(float64(baseMax)*duelDec) - defense
			dmg += int(atk.CriticalBonus)
			kind = KindCritical
		default:
			dmg = randInt(rng, baseMin, baseMax)
			dmg = int(float64(dmg)*duelDec) - defense
		}
	}

	// 6. GreaterDamageBonus（:185）与各类型的精通/弱点乘区（:189-197）。
	dmg += int(atk.GreaterDamageBonus)
	switch damageType {
	case TypeWizardry, TypeCurse:
		dmg += int(float32(dmg) * atk.BerserkerProficiency)
	default:
		dmg -= int(float32(dmg) * atk.WeaknessPhysDecrement)
		dmg += atk.MasterPhysPostBuffs
	}

	// 7. Overrates：仅 PvM；守方防御速率 > 攻方攻击速率 → ×0.3（:205-208）。
	if !m.IsPvP && def.DefenseRate > atk.AttackRate {
		dmg = int(float32(dmg) * 0.3)
	}

	// 8. 护甲减伤（:210）。
	if def.ArmorDamageDecrease > 0 {
		dmg -= int(float32(dmg) * def.ArmorDamageDecrease)
	}

	// 9. minLevelDmg = max(1, 等级/10)（:212-218；玩家的"等级"是 Total Level）。
	minLevelDmg := atk.Level / 10
	if minLevelDmg < 1 {
		minLevelDmg = 1
	}
	if dmg < minLevelDmg {
		dmg = minLevelDmg
	}

	// 10. 增伤 / 受伤减免乘区（:220-224）。
	dmg = ApplyDamageMultipliers(dmg, atk.AttackDamageIncrease, def.DamageReceiveDecrement)

	// 11. 技能倍率 / Dinorant。原版的 SkillFinalDamageBonus 在**乘倍率之前**加（:232→:247）。
	if sk.Used {
		mult := atk.SkillDamageMultiplier
		if mult <= 0 {
			mult = 1
		}
		dmg += int(sk.FinalBonus)
		if sk.FinalMultiplier > 0 {
			mult = sk.FinalMultiplier
			// Dragon Slasher(265) 只对怪物 ×3（:240-243）。
			if sk.Number == skillDragonSlasher && !m.IsPvP {
				mult *= 3
			}
		}
		factor := sk.DamageFactor
		if factor == 0 {
			factor = 1
		}
		dmg = int(float64(dmg) * float64(mult) * factor)
	} else if atk.IsDinorant {
		dmg = int(float32(dmg) * 1.3)
	}

	// 12. Soul Barrier：只有"法力够付费"时才抵扣（:258-266）。
	if def.SoulBarrierManaToll > 0 && def.CurrentMana > def.SoulBarrierManaToll {
		reduction := def.SoulBarrierReduction
		if reduction > 0.9 {
			reduction = 0.9
		}
		if reduction > 0 {
			dmg -= int(float32(dmg) * reduction)
		}
	}

	// 13. 最终伤害加成（:268，无 dmg>0 门）。
	dmg += int(atk.FinalDamageBonus)

	// 14. PvP 专属末段（:270-280）：FinalDamageIncreasePvp + 大师"精通"PvP 开关板。
	// 混沌城堡的两处 ÷2 未接：需要 CurrentMiniGame 实例（小游戏整域未起，见 doc/16）。
	if m.IsPvP && !atk.IsRaven {
		dmg += int(atk.FinalDamageIncreasePvP)
		dmg += atk.MasterMasteryPvP
	}

	// 15. 连击终结加成与双倍伤害（:282-304；两者都要求 dmg>0）。
	if dmg > 0 {
		if m.IsCombo {
			dmg += int(atk.ComboBonus)
		}
		if rollChance(rng, atk.DoubleDamageChance) {
			dmg *= 2
		}
	}

	return DamageResult{Damage: dmg, Kind: kind}
}

// skillDragonSlasher 是原版硬编码在伤害式里的技能号（:240）。
const skillDragonSlasher = 265
