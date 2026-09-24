// Package items 对应 OpenMU `src/GameServer/MessageHandler/Items/`（13 个 .cs）
// 以及 `MessageHandler/` 根下与物品/攻击相关、未分域的 handler。
//
// 入站：NPC 购买、混沌合成、消耗（含 075 变体）、丢弃、金弓窗口/退出、物品移动（含扩展版）、
// 修理、宝石合成、拾取（含 075 变体）、卖给 NPC；
// 根下未分域但属本域的：HitHandlerPlugIn(+075, Base)、TargetedSkillHandlerPlugIn(+075,+095)、
// AreaSkillAttackHandlerPlugIn(+075,+095)、AreaSkillHitHandlerPlugIn(+075, Base)、
// RageSkillAttackHandlerPlugIn、MagicEffectCancelHandlerPlugIn。
//
// 逻辑在 gamelogic/action（PlayerActions/Items/ + ItemConsumeActions/ + Skills/）+ gamelogic/combat，
// 出站在 view/remote/inventory。
package items
