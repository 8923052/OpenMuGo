package remote

// pet_view.go —— 宠物出站实现（对照 GameServer/RemoteView/Pet/*ViewPlugIn.cs）：
//   - C1 A7 PetMode（PetBehaviourChangedViewPlugIn）：宠物行为变化，只发给自己；
//   - C1 A8 PetAttack（PetAttackViewPlugIn）：宠物攻击动画，向主人 + 视野内观察者广播
//     （调用方按受众逐个 view 调用，ownerID 按接收者视角解析）；
//   - C1 A9 PetInfoResponse（PetInfoViewPlugIn）：宠物信息窗口（等级/经验/耐久）。

import (
	s2c "mugo/internal/proto/s2c"
)

// ShowPetMode 实现 action.PlayerView（C1 A7）。
func (v *PlayerView) ShowPetMode(petType, mode byte, targetID uint16) error {
	p := s2c.NewPetMode()
	p.SetPet(s2c.ClientToServerPetType(petType))
	p.SetPetCommandMode(s2c.ClientToServerPetCommandMode(mode))
	p.SetTargetId(targetID)
	return v.send.Send(p.Bytes())
}

// ShowPetAttack 实现 action.PlayerView（C1 A8）。
func (v *PlayerView) ShowPetAttack(petType, attackType byte, ownerID, targetID uint16) error {
	p := s2c.NewPetAttack()
	p.SetPet(s2c.ClientToServerPetType(petType))
	p.SetSkillType(s2c.PetSkillType(attackType))
	p.SetOwnerId(ownerID)
	p.SetTargetId(targetID)
	return v.send.Send(p.Bytes())
}

// ShowPetInfoResponse 实现 action.PlayerView（C1 A9）。
func (v *PlayerView) ShowPetInfoResponse(petType, storage, slot, level byte, experience uint32, health byte) error {
	p := s2c.NewPetInfoResponse()
	p.SetPet(s2c.ClientToServerPetType(petType))
	p.SetStorage(s2c.ClientToServerStorageType(storage))
	p.SetItemSlot(slot)
	p.SetLevel(level)
	p.SetExperience(experience)
	p.SetHealth(health)
	return v.send.Send(p.Bytes())
}
