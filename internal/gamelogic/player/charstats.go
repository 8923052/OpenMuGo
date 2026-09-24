// Package player 是玩家角色的属性推导与成长（原 gamelogic/attribute 包）。
//
// 之所以不叫 attribute：OpenMU 的 `AttributeSystem` 是一个独立顶层项目（21 个 .cs，
// 被 GameLogic / GameServer / Persistence / DataModel / Web 五处依赖），Go 侧必须有
// 一个同名顶层包 `mugo/internal/attribute` 与之对应。若这里继续占用 `attribute` 包名，
// 真正的属性系统就无处落位。
//
// 当前为 M6 简化占位实现（职业基准 + 等级公式），属性系统落地后本包改为**只做数据装配**：
// 从 GameConfiguration + 装备构造 AttributeSystem，不再自己算公式。
package player

import "mugo/internal/gamelogic/entity"

// 经验公式逐行复刻 GameConfigurationInitializerBase.CalculateNeededExperience（level<256 分支）。
// OpenMU 由完整 AttributeSystem 按装备/果实/组队等动态计算；
// Go 端暂无该系统，这里集中用职业基准+等级公式生成进图展示数值。
// 不影响移动/视野等核心链路；后续接入属性系统时整体替换本文件即可。

// 职业编号（CommonEnums CharacterClassNumber）。
const (
	ClassDarkWizard     byte = 0
	ClassDarkKnight     byte = 4
	ClassFairyElf       byte = 8
	ClassMagicGladiator byte = 12
	ClassDarkLord       byte = 16
	ClassSummoner       byte = 20
	ClassRageFighter    byte = 24
)

// HeroStateNormal 对应 s2c.CharacterHeroState_Normal（进图固定普通状态）。
const HeroStateNormal byte = 3

// MaxAttackSpeed 是攻速/魔速属性的上限（Stats.cs 的 Stats.AttackSpeed.MaximumValue = 200）。
// 客户端把这两个值直接喂给 SetAttackSpeed()：PlaySpeed = 基准 + AttackSpeed * 0.004
// （ZzzCharacter.cpp，RGZ_FIX_ATTACK_SPEED 分支下非 509~750 区间按 0.004），
// 所以它们是"0~200 的小数值"，**不是**客户端动画倍速本身。
const MaxAttackSpeed = 200

// attackSpeedsFor 按 OpenMU 的属性推导攻速/魔速（当前只含职业敏捷关系）。
//
// 对照 Persistence/Initialization/Updates/FixAttackSpeedCalculationUpdate.cs：
//   - AttackSpeed/MagicSpeed 都先累加 AttackSpeedByWeapon（武器）与 AttackSpeedAny；
//   - 各职业再加 TotalAgility 的线性关系：
//     DK/BK/BM 魔速 += 敏捷/20；DL 攻速+魔速各 += 敏捷/10；
//     DW/SM/GM 攻速 += 敏捷/20、魔速 += 敏捷/10；精灵系 攻速+魔速各 += 敏捷/50；
//     MG/DM 魔速 += 敏捷/20；RF/FM 攻速+魔速各 += 敏捷/9；召唤系 魔速 += 敏捷/20；
//   - 手套再各加 4~10（AttackSpeedByGloveNumber）。
//
// 裁剪登记：装备系统未落地，武器/手套/魔法效果(potion/set)的加成暂缺——接装备后在本函数
// 补 AttackSpeedByWeapon/手套表即可，职业关系不用改。
func attackSpeedsFor(class byte, agility uint16) (attackSpeed, magicSpeed uint16) {
	agi := float32(agility)
	switch class {
	case ClassDarkKnight:
		magicSpeed = uint16(agi / 20)
	case ClassDarkLord:
		attackSpeed = uint16(agi / 10)
		magicSpeed = uint16(agi / 10)
	case ClassDarkWizard:
		attackSpeed = uint16(agi / 20)
		magicSpeed = uint16(agi / 10)
	case ClassFairyElf:
		attackSpeed = uint16(agi / 50)
		magicSpeed = uint16(agi / 50)
	case ClassMagicGladiator:
		magicSpeed = uint16(agi / 20)
	case ClassRageFighter:
		attackSpeed = uint16(agi / 9)
		magicSpeed = uint16(agi / 9)
	case ClassSummoner:
		magicSpeed = uint16(agi / 20)
	}
	// 属性系统对最终值按 MaximumValue 截断。
	if attackSpeed > MaxAttackSpeed {
		attackSpeed = MaxAttackSpeed
	}
	if magicSpeed > MaxAttackSpeed {
		magicSpeed = MaxAttackSpeed
	}
	return attackSpeed, magicSpeed
}

// ExperienceForLevel 复刻 OpenMU CalculateNeededExperience。
func ExperienceForLevel(level uint16) uint64 {
	if level == 0 {
		return 0
	}
	l := uint64(level)
	if level < 256 {
		return 10 * (l + 8) * (l - 1) * (l - 1)
	}
	return 10*(l+8)*(l-1)*(l-1) + 1000*(l-247)*(l-256)*(l-256)
}

type classBase struct {
	strength   uint16
	agility    uint16
	vitality   uint16
	energy     uint16
	leadership uint16
	pointsPer  uint16 // 每级可分配点数
}

// 初始四维取 OpenMU CharacterClasses 配置的基准值。
var classBases = map[byte]classBase{
	ClassDarkWizard:     {18, 18, 15, 30, 0, 5},
	ClassDarkKnight:     {28, 20, 25, 10, 0, 5},
	ClassFairyElf:       {22, 25, 20, 20, 0, 5},
	ClassMagicGladiator: {26, 26, 26, 26, 0, 7},
	ClassDarkLord:       {26, 20, 20, 15, 26, 7},
	ClassSummoner:       {20, 20, 20, 30, 0, 5},
	ClassRageFighter:    {32, 27, 25, 20, 0, 7},
}

// AttackSpeedsForClass 按职业基准敏捷返回缺省攻速/魔速，供"没有 Stats 时"的视图兜底
// （对照原版进图/入视野时由 AttributeSystem 供值；此处退化为职业基准敏捷）。
func AttackSpeedsForClass(class byte) (attackSpeed, magicSpeed uint16) {
	return attackSpeedsFor(class, classBases[class].agility)
}

// NewCharStats 按职业与等级生成进图属性（近似公式，展示用）。
func NewCharStats(class byte, level uint16) *entity.CharStats {
	base := classBases[class]
	lv := uint32(level)
	if lv == 0 {
		lv = 1
	}
	vit, ene := uint32(base.vitality), uint32(base.energy)

	// 生命/法力为经典季简化线性公式（整型截断与原版方向一致）。
	maxHP := 12 + 2*lv + 2*vit
	maxMP := 7 + lv + ene
	switch class {
	case ClassDarkWizard, ClassSummoner:
		maxHP = 8 + lv + vit
		maxMP = 12 + 2*lv + 2*ene
	case ClassFairyElf:
		maxHP = 10 + lv + vit + vit/2
		maxMP = 9 + lv + ene + ene/2
	case ClassDarkLord:
		maxHP = 10 + lv + lv/2 + vit + vit/2
		maxMP = 9 + lv + lv/2 + ene
	case ClassMagicGladiator:
		maxHP = 11 + lv + lv/2 + vit
		maxMP = 8 + lv + ene
	}

	// 攻速/魔速按职业敏捷关系推导（原版 AttributeSystem 的子集）。
	// **不可硬编码 200**：客户端把该值直接算成动画倍速
	// （PlaySpeed = 基准 + AttackSpeed*0.004，ZzzCharacter.cpp SetAttackSpeed），
	// 恒 200 = 属性上限 → 所有职业/所有武器都按最高速播放攻击动作，帧序看起来发抖。
	atkSpeed, magSpeed := attackSpeedsFor(class, base.agility)

	// **当前值一律为 0，不得用公式伪造**：原版测试账号（AccountInitializerBase.
	// CreateCharacter）只持久化 Level/LevelUpPoints，Current 四项由属性系统的
	// 类 StatAttribute 基值提供（DK=110/20/1/1），真实最大值也由属性系统派生。
	// 有配置时走 ResolveCharStats 回落类基值；无配置降级路径由调用方钳满血。
	return &entity.CharStats{
		Experience:         ExperienceForLevel(level),
		ExperienceNext:     ExperienceForLevel(level + 1),
		LevelUpPoints:      base.pointsPer * uint16(lv-1),
		Strength:           base.strength,
		Agility:            base.agility,
		Vitality:           base.vitality,
		Energy:             base.energy,
		Leadership:         base.leadership,
		CurrentHealth:      0,
		MaximumHealth:      maxHP,
		CurrentMana:        0,
		MaximumMana:        maxMP,
		CurrentShield:      0,
		MaximumShield:      0,
		CurrentAbility:     0,
		MaximumAbility:     0,
		HeroState:          HeroStateNormal,
		AttackSpeed:        atkSpeed,
		MagicSpeed:         magSpeed,
		MaximumAttackSpeed: MaxAttackSpeed,
	}
}
