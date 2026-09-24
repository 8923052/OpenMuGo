package player

// attributes.go —— 角色属性装配（doc/15 §10 T1-2）。
//
// 对照原版 GameLogic/Attributes/ItemAwareAttributeSystem.cs：
//   stat = character.Attributes（角色当前态）∪ class.StatAttributes（类初始值）
//   base = class.BaseAttributeValues ∪ gameConfiguration.GlobalBaseAttributeValues
//   relationships = class.AttributeCombinations ∪ gameConfiguration.GlobalAttributeCombinations
// Go 侧：类数据来自 T0-c 导出件（config.GameConfig），角色当前态来自 entity.CharStats
// （Base Strength 等＝已分配点数后的总值），派生（Total * → Maximum Health/Mana、
// Attack Speed…）由 internal/attribute 按导出的关系图计算。
//
// CharStats 从此是**视图适配层**：数值全部读自属性系统（按 designation），
// 不再自带任何近似公式（原 NewCharStats 的线性公式已废弃）。

import (
	"fmt"

	"mugo/internal/attribute"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// 本系统消费/产出的 designation（与导出件逐字一致）。
const (
	desLevel        = "Level"
	desBaseStrength = "Base Strength"
	desBaseAgility  = "Base Agility"
	desBaseVitality = "Base Vitality"
	desBaseEnergy   = "Base Energy"
	desBaseLeader   = "Base Leadership"
	desCurHealth    = "Current Health"
	desCurMana      = "Current Mana"
	desCurAbility   = "Current Ability"
	desCurShield    = "Current Shield"
	desResets       = "Resets"
	desTotalStr     = "Total Strength"
	desTotalAgi     = "Total Agility"
	desTotalVit     = "Total Vitality"
	desTotalEne     = "Total Energy"
	desTotalLeader  = "Total Leadership"
	desMaxHealth    = "Maximum Health"
	desMaxMana      = "Maximum Mana"
	desMaxShield    = "Maximum Shield"
	desMaxAbility   = "Maximum Ability"
	desAttackSpeed  = "Attack Speed"
	desMagicSpeed   = "Magic Speed"
	desPointsPerLvl = "Points per Level up"
	desMasterLevel  = "Master Level"
)

// charStatOverrides 把角色当前态映射为 designation 覆盖
// （对应原版 character.Attributes：等级与加点后的五维/当前值）。
func charStatOverrides(c *entity.Character) map[string]float32 {
	overrides := map[string]float32{
		desLevel: float32(c.Level),
	}
	if c.Stats == nil {
		return overrides
	}
	st := c.Stats
	set := func(designation string, v uint16) {
		if v > 0 {
			overrides[designation] = float32(v)
		}
	}
	set(desBaseStrength, st.Strength)
	set(desBaseAgility, st.Agility)
	set(desBaseVitality, st.Vitality)
	set(desBaseEnergy, st.Energy)
	set(desBaseLeader, st.Leadership)
	// 大师等级是可回收属性（原版 Stats.MasterLevel，由大师经验分支 ++）：
	// 角色态存在 Character.MasterLevel，这里灌回系统，保证面板与系统同源。
	if c.MasterLevel > 0 {
		overrides[desMasterLevel] = float32(c.MasterLevel)
	}
	if st.CurrentHealth > 0 {
		overrides[desCurHealth] = float32(st.CurrentHealth)
	}
	if st.CurrentMana > 0 {
		overrides[desCurMana] = float32(st.CurrentMana)
	}
	if st.CurrentAbility > 0 {
		overrides[desCurAbility] = float32(st.CurrentAbility)
	}
	if st.Resets > 0 {
		overrides[desResets] = float32(st.Resets)
	}
	return overrides
}

// ResolveCharStats 用真实属性系统解析角色的 92B 属性集合
// （替换原 NewCharStats 的线性近似公式）。cfg 为 T0-c 载入的导出件。
func ResolveCharStats(cfg *config.GameConfig, c *entity.Character) (*entity.CharStats, error) {
	return ResolveCharStatsWithEffects(cfg, c, nil)
}

// ResolveCharStatsWithEffects 解析角色属性，并纳入活动 buff 效果
// （buff 改变 AttackSpeed/MaximumHealth 等派生值时，重算 92B 全字段）。
func ResolveCharStatsWithEffects(cfg *config.GameConfig, c *entity.Character, effects []action.MagicEffect) (*entity.CharStats, error) {
	if cfg == nil {
		return nil, fmt.Errorf("player: 未注入游戏配置（GameConfig）")
	}
	system, err := buildWorldSystem(cfg, c, effects)
	if err != nil {
		return nil, err
	}

	valueOf := func(designation string) float32 {
		def, ok := cfg.AttributeByName(designation)
		if !ok {
			return 0
		}
		probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
		return system.GetValueOfAttribute(probe)
	}

	cls, ok := cfg.Class(int(c.ClassNumber))
	if !ok {
		return nil, fmt.Errorf("player: 职业 %d 不存在于导出件", c.ClassNumber)
	}

	// 上限先算（当前值初始化与钳制需要）。
	maxHealth := uint32(valueOf(desMaxHealth))
	maxMana := uint32(valueOf(desMaxMana))
	maxAbility := uint32(valueOf(desMaxAbility))
	maxShield := uint32(valueOf(desMaxShield))

	// 当前值：角色有持久化 current（战斗扣血/消耗后）保留；
	// 无持久化（首次进图/重连无存档）按**满状态**进场——职业 stat 基值
	// （mana=20 等）是 1 级初始基数，直接用会让高等级角色残蓝、放不出技能。
	curHealth, curMana, curAbility, curShield := maxHealth, maxMana, maxAbility, maxShield
	if c.Stats != nil {
		if c.Stats.CurrentHealth > 0 {
			curHealth = c.Stats.CurrentHealth
		}
		if c.Stats.CurrentMana > 0 {
			curMana = c.Stats.CurrentMana
		}
		if c.Stats.CurrentAbility > 0 {
			curAbility = c.Stats.CurrentAbility
		}
		if c.Stats.CurrentShield > 0 {
			curShield = c.Stats.CurrentShield
		}
	}

	u16 := func(v float32) uint16 { return uint16(v + 0.5) }
	st := &entity.CharStats{
		// 经验表来自导出件（T0-c，与原版公式同源）。
		Experience:     expAt(cfg, int(c.Level)),
		ExperienceNext: expAt(cfg, int(c.Level)+1),
		Strength:       u16(valueOf(desTotalStr)),
		Agility:        u16(valueOf(desTotalAgi)),
		Vitality:       u16(valueOf(desTotalVit)),
		Energy:         u16(valueOf(desTotalEne)),
		Leadership:     u16(valueOf(desTotalLeader)),
		CurrentHealth:  curHealth,
		MaximumHealth:  maxHealth,
		CurrentMana:    curMana,
		MaximumMana:    maxMana,
		CurrentShield:  curShield,
		MaximumShield:  maxShield,
		CurrentAbility: curAbility,
		MaximumAbility: maxAbility,
		HeroState:      HeroStateNormal,
		AttackSpeed:    u16(valueOf(desAttackSpeed)),
		MagicSpeed:     u16(valueOf(desMagicSpeed)),
		// 攻速上限原版=同一属性（MaximumAttackSpeed 无独立派生）。
		MaximumAttackSpeed: u16(valueOf(desAttackSpeed)),
	}
	if c.Stats != nil {
		st.Resets = c.Stats.Resets
	}
	// 升级点数（T2-8）：角色态优先——加点会**扣减**剩余点数，重新解析不得回满
	// （原版 Character.LevelUpPoints 是持久化的"剩余值"，升级时 += PointsPerLevelUp）。
	// 无角色态（首次进图）按 每级点数 × (等级-1) 派生（初始即全额）。
	if c.Stats != nil {
		st.LevelUpPoints = c.Stats.LevelUpPoints
	} else if per := valueOf(desPointsPerLvl); per > 0 && c.Level > 1 {
		st.LevelUpPoints = u16(per * float32(c.Level-1))
	}
	// 钱与果实用量：角色自身态保留（内存种子数据）。
	if c.Stats != nil {
		st.Money = c.Stats.Money
		st.UsedFruitPoints = c.Stats.UsedFruitPoints
		st.UsedNegFruit = c.Stats.UsedNegFruit
		st.InventoryExtensions = c.Stats.InventoryExtensions
	}
	// 果实上限按等级现算（TRIM-11d，对照 CharacterExtensions.GetMaximumFruitPoints:26-35
	// + GetFruitPoints:142-154 的三张表）：原版把**同一个**上限同时填进正/负两侧，与消耗果实时的
	// 判定同源，所以两处都取这里而不是角色存量。
	st.MaxFruitPoints = uint16(action.MaxFruitPoints(int(c.Level), cls.FruitCalculation))
	st.MaxNegFruit = st.MaxFruitPoints
	return st, nil
}

func expAt(cfg *config.GameConfig, level int) uint64 {
	if v := cfg.ExperienceForLevel(level); v >= 0 {
		return uint64(v)
	}
	return ExperienceForLevel(uint16(level))
}

// AttributeValue 按 designation 读取角色属性系统中的值（对应原版 `attributes[Stats.X]`）。
//
// 与 ResolveCharStats 共用同一套覆盖装配（charStatOverrides），因此结果与 92B 属性集合自洽。
// 配置未注入、职业未知或属性未导出时返回 0——调用方据此回落默认值（如倍率 0 → 视为 1）。
func AttributeValue(cfg *config.GameConfig, c *entity.Character, designation string) float64 {
	if cfg == nil || c == nil {
		return 0
	}
	def, ok := cfg.AttributeByName(designation)
	if !ok {
		return 0
	}
	system, err := buildCharacterSystem(cfg, c)
	if err != nil {
		return 0
	}
	probe := &attribute.AttributeDefinition{ID: def.ID, Designation: def.Designation}
	return float64(system.GetValueOfAttribute(probe))
}
