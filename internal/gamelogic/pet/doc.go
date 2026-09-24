// Package pet 对应 OpenMU `src/GameLogic/Pet`，是宠物子系统的纯领域层。
//
// 本包只放与运行时无关的宠物语义（行为/攻击枚举、命令号映射、索敌参数），对照原版
// IPetCommandManager / RavenCommandManager 的枚举与常量。有状态、需要世界与并发的
// 部分按分层拆到别处：
//
//   - 可训练宠物等级注入 + 宠物选项（黑暗之马 / 三色 Fenrir）消费：gamelogic/player
//     （pet_level.go / item_option_powerup.go），对照 ItemPowerUpFactory.GetPetLevel；
//   - 渡鸦攻击属性解析：gamelogic/player（pet_raven.go），对照 RavenAttributeSystem；
//   - 经验曲线 / 升级门槛：gamelogic/config（pet.go），对照 PetLevelHelper、
//     ItemExtensions.GetDarkRavenLeadershipRequirement；
//   - 命令管理器（后台攻击循环，对照 RavenCommandManager）：server/gameserver（pet_manager.go）；
//   - 入站 handler：server/gameserver（handler_pet.go）；出站在 view/remote（pet_view.go）。
package pet
