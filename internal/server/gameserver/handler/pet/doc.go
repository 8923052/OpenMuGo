// Package pet 对应 OpenMU `src/GameServer/MessageHandler/Pet/`（2 个 .cs）。
//
// 入站：宠物命令请求、宠物信息请求。
//
// 逻辑在 gamelogic/pet + gamelogic/action（PlayerActions/Items/SetPetBehaviourRequestAction、
// PetInfoRequestAction），出站在 view/remote/pet。
package pet
