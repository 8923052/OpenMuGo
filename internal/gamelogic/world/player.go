// Package world 是内存态世界模型：地图、玩家注册表、进出场与移动。
// 不依赖协议层：出站通过 View（动作层视图接口，T0-b）或 Send 回调下发。
package world

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
)

// Player 是地图上的一名玩家（世界态，与连接会话解耦）。
type Player struct {
	ID        uint16
	MapNumber uint16
	Name      string
	Class     byte // CharacterClassNumber 原始值（DK=4）
	X, Y      byte // 当前坐标
	Rotation  byte // 线上朝向（0..7）
	// Appearance 为 27 字节进图外观（含效果位由调用方补齐）。
	Appearance []byte
	// View 由 gameserver 注入（T0-b）：广播/视野事件走视图接口。nil 时回退 Send。
	View action.PlayerView
	// Send 由 gameserver 注入的兜底出站通道（View 为 nil 时使用）。
	Send func([]byte)
	// DefenseRatePvM / DefensePvM 由 gameserver 注入（T2-2：怪物攻击的命中判定与减伤）。
	DefenseRatePvM float32
	DefensePvM     float32
	// DamageReceiveMultiplier 由 gameserver 注入（本角色"Damage Receive Multiplier"，
	// 守护天使/黑王马/蓝 Fenrir 等宠物/翅膀的受伤减免倍率，基值 1）。
	DamageReceiveMultiplier float32
	// ArmorDamageDecrease / DefenseDecrement / GreaterDefenseBonus 由 gameserver 注入，
	// 供怪物→玩家伤害按完整防御减伤公式结算（卓越/和谐/镶嵌减伤、减防、格挡加成）。
	ArmorDamageDecrease float32
	DefenseDecrement    float32
	GreaterDefenseBonus float32
	// SoulBarrierReduction / SoulBarrierToll 由 gameserver 注入（TRIM-06）：
	// 怪物打玩家时同样走 Soul Barrier 抵扣，只要玩家法力够付每次受击的费用。
	SoulBarrierReduction float32
	SoulBarrierToll      float32
	// IsAlive 由 gameserver 维护（死亡后置 false——原版 OnDeathAsync 的 IsAlive=false，
	// 怪物 AI 不得以尸体为攻击目标）。
	IsAlive bool
	// Stats 为角色属性引用（受击血/盾条计算用；enterWorld 注入，与 Character.Stats 同源）。
	Stats *entity.CharStats
	// OnDeath 由 gameserver 注入（死亡后的重生流程：原版 3s 后回出生门满血重生）。
	OnDeath func()
	// ApplyDamage 由 gameserver 注入（T2-2：怪物攻击扣玩家血；返回剩余血量与是否致死）。
	ApplyDamage func(dmg uint16) (remained uint32, died bool)
	// OnHit 由 gameserver 注入（受击后回调：扣减宠物/防御装备耐久，对照
	// Player.DecreaseDefenseItemDurabilityAsync）。入参为本次玩家承受伤害。
	OnHit func(damage int)
}
