package npc

// intelligence_kinds.go —— 决策器装配所需的两组标识：原版 `MonsterDefinition.IntelligenceTypeName`
// 的完整类型名字符串，与 `NpcObjectKind` 的导出件字符串。
// 二者都由 tools/goldenconfig 从 OpenMU 初始化直接导出，本文件只做"字符串 → 决策器"的对表，
// 不在数据里发明任何新值。

// 导出件 ObjectKind（对照 DataModel MonsterDefinition.cs:183-224 的 NpcObjectKind）。
const (
	kindMonster     = "Monster"
	kindPassiveNpc  = "PassiveNpc"
	kindGuard       = "Guard"
	kindTrap        = "Trap"
	kindGate        = "Gate"
	kindStatue      = "Statue"
	kindSoccerBall  = "SoccerBall"
	kindDestructive = "Destructible"
)

// 配置里的 AI 类型全名（对照 MapInitializer.TryCreateConfiguredNpcIntelligence 反射的目标类型）。
const (
	typeBasicMonsterIntelligence = "MUnique.OpenMU.GameLogic.NPC.BasicMonsterIntelligence"
	typeGuardIntelligence        = "MUnique.OpenMU.GameLogic.NPC.GuardIntelligence"
	typeNullMonsterIntelligence  = "MUnique.OpenMU.GameLogic.NPC.NullMonsterIntelligence"
	typeRandomAttackInRangeTrap  = "MUnique.OpenMU.GameLogic.NPC.RandomAttackInRangeTrapIntelligence"
	typeTrapSinglePressed        = "MUnique.OpenMU.GameLogic.NPC.AttackSingleWhenPressedTrapIntelligence"
	typeTrapAreaPressed          = "MUnique.OpenMU.GameLogic.NPC.AttackAreaWhenPressedTrapIntelligence"
	typeTrapAreaDirection        = "MUnique.OpenMU.GameLogic.NPC.AttackAreaTargetInDirectionTrapIntelligence"
)

// trapKind 是四种陷阱行为变体。
type trapKind int

const (
	trapNone trapKind = iota
	// trapRandomInRange 视野内取最近可打目标即攻击（S6 的kanturu/城堡陷阱）。
	trapRandomInRange
	// trapSinglePressed 只踩在陷阱那一格上的那个玩家。
	trapSinglePressed
	// trapAreaPressed 有人踩上时，打到范围内所有目标。
	trapAreaPressed
	// trapAreaDirection 沿陷阱朝向那一方向打到范围内所有目标。
	trapAreaDirection
)

// ObjectKindOf 暴露导出件里的对象类别（GS 侧攻击可打性判定要用它，避免各处再比字符串）。
func (n *Npc) ObjectKindOf() string {
	if n == nil || n.Def == nil {
		return ""
	}
	return n.Def.ObjectKind
}

// typeNameOf 取配置的 AI 类型名（未配置为空串）。
func typeNameOf(n *Npc) string {
	if n == nil || n.Def == nil || n.Def.Intelligence == nil {
		return ""
	}
	return *n.Def.Intelligence
}

// IsAttackableByPlayer 报告玩家能否打这个对象（对照原版的两层门槛）：
//   - 只有继承 AttackableNpcBase 的类别是 IAttackable（Monster/Guard/Destructible/SoccerBall）；
//     Merchant、Statue、Gate、PassiveNpc、Trap 都是 NonPlayerCharacter，客户端根本选不到；
//   - 守卫再被单独拒绝：AttackableNpcBase.AttackByAsync 在 ObjectKind == Guard 时直接返回 null
//     （AttackableNpcBase.cs:107-112），所以守卫只能打人、不能被打。
func (n *Npc) IsAttackableByPlayer() bool {
	switch n.ObjectKindOf() {
	case kindMonster, kindDestructive, kindSoccerBall:
		return true
	}
	return false
}
