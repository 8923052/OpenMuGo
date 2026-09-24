// Package castlesiege 对应 OpenMU `src/GameLogic/CastleSiege`（约 80 个 .cs，
// 是 GameLogic 里最大的域，占 13%）。
//
// 内部按原版再分四层：
//
//	CastleSiege/            编排：CastleSiegePlugIn / CastleSiegeAdministration /
//	                        CastleSiegeContext(+Resolver) / CastleSiegeSchedule /
//	                        CastleSiegeStatePeriod / CastleSiegeParticipant(+Tracker) /
//	                        CastleSiegeGuildParticipant / CastleSiegeGuildSelector /
//	                        CastleSiegeCrownMechanics / CastleSiegeSwitchMechanics /
//	                        CastleSiegeTaxProvider / CastleSiegeRewardDelivery /
//	                        CastleSiegeConfigurationExtensions / CastleSiegeEconomyNotifier /
//	                        *Snapshot / *Error / *Result / *TalkPlugIn。
//	CastleSiege/Actions/    玩家动作（对照 PlayerActions 层）：GateOperate / MachineUse /
//	                        HuntZoneEnter|Toggle / NpcBuy|Repair|Upgrade / RegisterGuild /
//	                        RegisterMark / RegistrationState / SummonLifeStone /
//	                        TaxInfo / TaxRateChange / TributeWithdraw / UnregisterGuild。
//	CastleSiege/NPC/        可攻击 NPC：Gate / Statue / Crown / Lever / Machine / LifeStone / Switch。
//	CastleSiege/Intelligence/ 各 NPC 的 AI：Crown / Gate / Lever / Machine / Statue / Switch。
//
// 配套的入站 handler 在 server/gameserver/handler/castlesiege，出站插件在 view/remote/castlesiege。
// 三处必须同域同序落地，否则战盟攻城会出现"包收得到但不生效"。
package castlesiege
