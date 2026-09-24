package gameserver

// handler_pet.go —— 宠物命令与信息（对照 MessageHandler/Pet/PetCommandRequestHandlerPlugIn.cs
// + PetInfoRequestHandlerPlugIn.cs、PlayerActions/Items/SetPetBehaviourRequestAction.cs
// + PetInfoRequestAction.cs）。
//
//   - C1 A7 PetCommandRequest → 切换黑暗渡鸦行为（Idle/随机/跟随/锁定）；
//   - C1 A9 PetInfoRequest    → 回 C1 A9 PetInfoResponse（等级/经验/耐久）。
//
// 命令号映射与 C# 一致：mode % 120 → behaviour（Normal=0/AttackRandom=1/
// AttackWithOwner=2/AttackTarget=3）。可训练攻击宠仅黑暗渡鸦，装备在右手槽（1）。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/pet"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// petStorageInventoryPetSlot 对应 s2c.ClientToServerStorageType_InventoryPetSlot=254
// （原版宠物升级时用该存储类型下发 PetInfoResponse）。
const petStorageInventoryPetSlot byte = 254

// handlePetCommand 处理 C1 A7：切换渡鸦攻击行为。
func (s *Server) handlePetCommand(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	// 必须有可训练攻击宠在右手槽（原版 SetPetBehaviourRequestAction 的前置判定）。
	mgr := sess.getPetManager()
	if mgr == nil {
		return
	}
	req := c2s.AsPetCommandRequest(frame)
	mode := byte(req.CommandMode()) % 120
	b, ok := pet.BehaviourFromCommandMode(mode)
	if !ok {
		return
	}
	var target *npc.Npc
	if b == pet.BehaviourAttackTarget {
		target = s.findNpcByID(wp.MapNumber, req.TargetId()&0x7FFF)
	}
	mgr.setBehaviour(b, target)
}

// handlePetInfo 处理 C1 A9：下发可训练宠物的信息窗口。
func (s *Server) handlePetInfo(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		s.deps.logger.Printf("[petinfo] 忽略：状态=%d 非 EnteredWorld", sess.getState())
		return
	}
	c := sess.getSelected()
	if c == nil || c.Inventory == nil {
		s.deps.logger.Printf("[petinfo] 忽略：角色/背包未就绪")
		return
	}
	if len(frame) < c2s.PetInfoRequestLength {
		s.deps.logger.Printf("[petinfo] 忽略：帧长 %d < %d", len(frame), c2s.PetInfoRequestLength)
		return
	}
	req := c2s.AsPetInfoRequest(frame)
	slot := req.ItemSlot()
	s.deps.logger.Printf("[petinfo] 收到 petType=%d storage=%d slot=%d", req.Pet(), req.Storage(), slot)
	slotted := c.Inventory.GetItem(slot)
	if slotted == nil || slotted.It == nil {
		s.deps.logger.Printf("[petinfo] 不回包：槽 %d 无物品", slot)
		return
	}
	it := slotted.It
	def, ok := s.deps.cfg.GameConfig.Item(int(it.Group), it.Number)
	if !ok || !def.IsTrainablePet() {
		s.deps.logger.Printf("[petinfo] 不回包：槽 %d 物品 (%d,%d) 非可训练宠 ok=%v", slot, it.Group, it.Number, ok)
		return
	}
	if view := s.viewFor(sess); view != nil {
		petType := pet.PetTypeByte(def.IsDarkHorse())
		s.deps.logger.Printf("[petinfo] 回包 petType=%d lv=%d exp=%d dur=%d", petType, it.Level, it.PetExperience, it.Durability)
		_ = view.ShowPetInfoResponse(petType, byte(req.Storage()), slot, it.Level,
			uint32(it.PetExperience), it.Durability)
	}
}

// syncPetManager 依据右手槽是否装备可训练攻击宠（黑暗渡鸦），创建或摘除命令管理器。
// 进图与装备穿脱后调用（对照 UpdatePetCommandManagerOnItemMovePlugIn）。
func (s *Server) syncPetManager(sess *session, c *entity.Character, wp *world.Player) {
	if c == nil || wp == nil {
		return
	}
	hasRaven := false
	if c.Inventory != nil {
		if sl := c.Inventory.GetItem(item.SlotRightHand); sl != nil && sl.It != nil {
			if def, ok := s.deps.cfg.GameConfig.Item(int(sl.It.Group), sl.It.Number); ok {
				hasRaven = def.IsTrainablePet() && sl.It.Durability > 0
			}
		}
	}
	mgr := sess.getPetManager()
	switch {
	case !hasRaven:
		if mgr != nil {
			mgr.stop()
			sess.setPetManager(nil)
		}
	case hasRaven && (mgr == nil || mgr.wp != wp):
		// 无管理器，或换图导致世界玩家指针变化 → 重建。
		// 新管理器的 c/wp 创建后不再变更（不可变），与后台循环的读无竞态。
		if mgr != nil {
			mgr.stop()
		}
		sess.setPetManager(newPetManager(s, sess, c, wp))
	}
	// 同一 wp 且已有管理器（仅装备刷新）：保持运行，不动。
}

// decreasePetDurability 玩家受击后扣减宠物耐久（对照 Player.DecreaseDefenseItemDurabilityAsync）。
// 本仓配置未导出 DamagePerOnePetDurability/PetDurationIncrease，用"每 100 承伤扣 1 耐久、
// 至少扣 1"的近似（裁剪登记）；渡鸦耐久归零 → 转 Idle。
// 由 AI 攻击钩子（非 opMu 线程）经 wp.OnHit 调用，故在此自持 sess.opMu 串行化背包改动。
func (s *Server) decreasePetDurability(sess *session, c *entity.Character, damage int) {
	if c == nil || c.Inventory == nil || damage <= 0 {
		return
	}
	dec := damage / 100
	if dec < 1 {
		dec = 1
	}
	sess.opMu.Lock()
	defer sess.opMu.Unlock()
	for _, slot := range []byte{item.SlotPet, item.SlotRightHand} {
		sl := c.Inventory.GetItem(slot)
		if sl == nil || sl.It == nil || sl.It.Group != 13 || sl.It.Durability == 0 {
			continue
		}
		if int(sl.It.Durability) <= dec {
			sl.It.Durability = 0
		} else {
			sl.It.Durability -= byte(dec)
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowItemDurabilityChanged(slot, sl.It.Durability, false)
		}
		if sl.It.Durability == 0 && slot == item.SlotRightHand {
			if m := sess.getPetManager(); m != nil {
				m.setBehaviour(pet.BehaviourIdle, nil)
			}
		}
	}
}

// grantPetExperience 把玩家击杀经验的一部分分给可训练宠物并升级
// （对照 PetExperiencePlugIn.PlayerGainedExperienceAsync：PetShare=0.2，骑/攻双宠各半）。
func (s *Server) grantPetExperience(sess *session, c *entity.Character, playerExp int64) {
	gc := s.deps.cfg.GameConfig
	if playerExp <= 0 || c == nil || c.Inventory == nil || gc == nil {
		return
	}
	movePet := trainablePetAt(gc, c, item.SlotPet)
	attackPet := trainablePetAt(gc, c, item.SlotRightHand)
	if movePet == nil && attackPet == nil {
		return
	}
	petExp := playerExp * 20 / 100
	if movePet != nil && attackPet != nil {
		petExp /= 2
	}
	if petExp < 1 {
		return
	}
	leveled := false
	if movePet != nil {
		leveled = s.addPetExp(sess, c, movePet, item.SlotPet, petExp) || leveled
	}
	if attackPet != nil {
		leveled = s.addPetExp(sess, c, attackPet, item.SlotRightHand, petExp) || leveled
	}
	// 宠物等级变化影响 HorseLevel/RavenLevel 派生属性（防御/减伤/渡鸦伤害）→ 重算。
	if leveled {
		s.refreshCombatValues(sess, c, false)
		s.refreshEquipmentStats(sess, c)
	}
}

// trainablePetAt 返回指定槽上"可升级且未坏"的可训练宠物实例（否则 nil）。
func trainablePetAt(gc *config.GameConfig, c *entity.Character, slot byte) *item.Item {
	sl := c.Inventory.GetItem(slot)
	if sl == nil || sl.It == nil || sl.It.Durability == 0 {
		return nil
	}
	def, ok := gc.Item(int(sl.It.Group), sl.It.Number)
	if !ok || !def.IsTrainablePet() || int(sl.It.Level) >= def.MaximumPetLevel() {
		return nil
	}
	return sl.It
}

// addPetExp 给宠物加经验并按阈值升级，每升一级下发 PetInfoResponse；返回是否发生了升级。
func (s *Server) addPetExp(sess *session, c *entity.Character, it *item.Item, slot byte, exp int64) bool {
	gc := s.deps.cfg.GameConfig
	def, ok := gc.Item(int(it.Group), it.Number)
	if !ok {
		return false
	}
	maxLevel := def.MaximumPetLevel()
	if int64(it.PetExperience)+exp > int64(^uint32(0)>>1) {
		it.PetExperience = int32(^uint32(0) >> 1) // 防溢出钳制
	} else {
		it.PetExperience += int32(exp)
	}
	leveled := false
	for int(it.Level) < maxLevel {
		next := config.PetExperienceForLevel(int(it.Level)+1, maxLevel)
		if uint64(it.PetExperience) < next {
			break
		}
		// 渡鸦升级受统率门槛约束（对照 GetDarkRavenLeadershipRequirement）。
		if def.IsDarkRaven() {
			need := config.DarkRavenLeadershipRequirement(int(it.Level) + 1)
			if float64(need) > player.AttributeValue(gc, c, "Total Leadership") {
				break
			}
		}
		it.Level++
		leveled = true
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowPetInfoResponse(pet.PetTypeByte(def.IsDarkHorse()), petStorageInventoryPetSlot,
				slot, it.Level, uint32(it.PetExperience), it.Durability)
		}
	}
	return leveled
}
