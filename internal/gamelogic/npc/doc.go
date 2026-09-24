// Package npc 对应 OpenMU `src/GameLogic/NPC`（18 个 .cs）。
//
// 怪物、NPC 与其 AI：
//
//	NonPlayerCharacter / Monster / MerchantNpc / GateNpc / Destructible / Trap /
//	SoccerBall / AttackableNpcBase / ISummonable
//	BasicMonsterIntelligence / GuardIntelligence / SummonedMonsterIntelligence /
//	NullMonsterIntelligence
//	TrapIntelligenceBase / AttackAreaTargetInDirectionTrapIntelligence /
//	AttackAreaWhenPressedTrapIntelligence / AttackSingleWhenPressedTrapIntelligence /
//	RandomAttackInRangeTrapIntelligence
//
// 前置依赖：pathfinding（走位）、attribute（怪物属性）、combat（伤害结算）。
// 配套：GameMap 的刷怪（MapInitializer / GameServerMapInitializer）与视野播报（view/remote）。
//
// 这是"地图里有活物"的基础，优先级高于 Bots / CastleSiege / MiniGames。
package npc
