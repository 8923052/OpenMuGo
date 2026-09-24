package pet

// pet.go —— 宠物行为/攻击类型枚举与命令号映射（对照原版
// GameLogic/Pet/IPetCommandManager.cs 的 PetBehaviour、Views/Pet/PetAttackType.cs、
// GameServer 侧 PetCommandMode/PetSkillType 协议枚举）。
//
// 数值与 s2c/c2s 的 ClientToServerPetCommandMode、PetSkillType 逐字一致，
// 便于行为↔命令号无损往返（Idle=Normal=0、AttackRandom=1、AttackWithOwner=2、AttackTarget=3）。

// Behaviour 是宠物的攻击行为。
type Behaviour byte

const (
	// BehaviourIdle 空闲，不攻击。
	BehaviourIdle Behaviour = 0
	// BehaviourAttackRandom 随机攻击视野内怪物。
	BehaviourAttackRandom Behaviour = 1
	// BehaviourAttackWithOwner 跟随主人目标攻击。
	BehaviourAttackWithOwner Behaviour = 2
	// BehaviourAttackTarget 锁定单一目标打到死。
	BehaviourAttackTarget Behaviour = 3
)

// CommandMode 返回下发给客户端的 PetMode 命令字节（数值与 Behaviour 相同）。
func (b Behaviour) CommandMode() byte { return byte(b) }

// BehaviourFromCommandMode 把客户端命令号（已 %120）映射为行为；未知返回 false。
func BehaviourFromCommandMode(mode byte) (Behaviour, bool) {
	if mode > byte(BehaviourAttackTarget) {
		return BehaviourIdle, false
	}
	return Behaviour(mode), true
}

// AttackType 是一次宠物攻击的动画类型。
type AttackType byte

const (
	// AttackSingleTarget 单体攻击（暴击/卓越用）。
	AttackSingleTarget AttackType = 0
	// AttackRange 范围攻击（随后各目标各补一发单体）。
	AttackRange AttackType = 1
)

// SkillTypeByte 返回 PetAttack 的 skill type 字节。
func (a AttackType) SkillTypeByte() byte { return byte(a) }

// 黑暗渡鸦攻击参数（对照 RavenCommandManager 常量）。
const (
	// AttackRangeTiles 是渡鸦索敌半径（切比雪夫）。
	AttackRangeTiles = 7
	// RangeAttackExtraTargets 是范围攻击附加目标数（主目标外最多 2 个，共 3 段）。
	RangeAttackExtraTargets = 2
	// RangeAttackHitRadius 是范围攻击二次索敌半径。
	RangeAttackHitRadius = 3
	// rangeAttackChancePercent 是普通攻击掷为范围攻击的概率（原版 Rand.NextRandomBool(0.3)）。
	rangeAttackChancePercent = 30
)

// RangeAttackProbability 暴露范围攻击概率（整数百分比，供 NextRandomBool）。
func RangeAttackProbability() int { return rangeAttackChancePercent }

// PetTypeByte 返回宠物类型字节（s2c ClientToServerPetType：渡鸦=0，马=1）。
func PetTypeByte(darkHorse bool) byte {
	if darkHorse {
		return 1
	}
	return 0
}
