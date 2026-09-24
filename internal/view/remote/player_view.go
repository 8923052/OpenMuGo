// player_view.go 是 action.PlayerView 的协议实现（doc/10 T0-b 出站轴）：
// 按"发生了什么"组装封包——形态选择（扩展/紧凑）与字段编码全部收拢在这一层，
// handler 与 action 不再出现任何 s2c 构造。
package remote

import (
	"fmt"
	"log"
	"unicode/utf8"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// scopeExtContentLen 为外观27B + 效果计数1B。
const scopeExtContentLen = AppearanceExtSize + 1

// PacketSender 是视图层需要的最小出站通道（会话连接的抽象）。
type PacketSender interface {
	Send([]byte) error
}

// PlayerView 单玩家出站视图。
type PlayerView struct {
	send     PacketSender
	extended bool // UsesExtendedCharacterList（扩展角色列表形态）
	// clientVersion 对应原版 RemotePlayer.ClientVersion：铁律 —— 同一语义的包存在版本变体时
	// （如任务 0x1B/0x0C 的 C1 与 C2 Extended），在视图层按版本轴挑选。
	clientVersion version.ClientVersion
	logger        *log.Logger
}

// NewPlayerView 构造。extended 决定角色列表形态选择
// （对应原版 IShowCharacterListPlugIn 的版本选择）；clientVersion 对应 RemotePlayer.ClientVersion。
func NewPlayerView(sender PacketSender, usesExtendedCharacterList bool, clientVersion version.ClientVersion, logger *log.Logger) *PlayerView {
	if logger == nil {
		logger = log.Default()
	}
	return &PlayerView{send: sender, extended: usesExtendedCharacterList, clientVersion: clientVersion, logger: logger}
}

// ShowLoginResult 实现 action.PlayerView（F1 01；语义码 → 协议码映射在此）。
func (v *PlayerView) ShowLoginResult(result action.LoginResult) error {
	p := s2c.NewLoginResponse()
	p.SetSuccess(loginResultCode(result))
	return v.send.Send(p.Bytes())
}

func loginResultCode(r action.LoginResult) s2c.LoginResult {
	switch r {
	case action.LoginOkay:
		return s2c.LoginResult_Okay
	case action.LoginInvalidPassword:
		return s2c.LoginResult_InvalidPassword
	case action.LoginAccountAlreadyConnected:
		return s2c.LoginResult_AccountAlreadyConnected
	case action.LoginAccountBlocked:
		return s2c.LoginResult_AccountBlocked
	case action.LoginTemporaryBlocked:
		return s2c.LoginResult_TemporaryBlocked
	case action.LoginWrongVersion:
		return s2c.LoginResult_WrongVersion
	default:
		return s2c.LoginResult_ConnectionError
	}
}

// ShowLogoutResponse 实现 action.PlayerView（C3 F1 02；回显客户端请求的登出方式）。
// 对照 OpenMU LogoutAction：CloseGame/BackToServerSelection 发送后断开；
// BackToCharacterSelection 发送后保持连接并回退到 Authenticated（由 handler 控制）。
func (v *PlayerView) ShowLogoutResponse(t action.LogoutType) error {
	p := s2c.NewLogoutResponse()
	p.SetType(s2c.LogOutType(t))
	return v.send.Send(p.Bytes())
}

// ShowSkillList 实现 action.PlayerView（C1 F3 11 SkillListUpdate；进图/学习后刷新技能列表）。
// 对照 OpenMU UpdateSkillListPlugIn：SkillEntry 顺序即客户端技能栏顺序，SkillIndex 从 0 递增；
// SkillNumber 小端（S6 扩展形态），与客户端 ReceiveSkillList 解析一致。
func (v *PlayerView) ShowSkillList(skills []action.SkillListView) error {
	p := s2c.NewSkillListUpdate(s2c.SkillListUpdateRequiredSize(len(skills)))
	p.SetCount(byte(len(skills)))
	for i, s := range skills {
		e := p.Skills(i)
		if e == nil {
			break
		}
		e.SetSkillIndex(byte(i))
		e.SetSkillNumber(s.SkillNumber)
		e.SetSkillLevel(s.Level)
	}
	return v.send.Send(p.Bytes())
}

// ShowCharacterList 实现 action.PlayerView。
// extended → CharacterListExtended（8B + N×44B，27B 扩展外观）；
// 否则 → CharacterList（8B + N×34B，18B 预览外观）。
// 两者不能混用：MuMain 的 ReceiveCharacterListExtended 默认对齐 sizeof=44，
// 收到 34B 紧凑版会整体错位（角色选择界面错乱/卡住）。
func (v *PlayerView) ShowCharacterList(entries []action.CharacterListEntry, classUnlockFlags byte) error {
	if v.extended {
		p := s2c.NewCharacterListExtended(s2c.CharacterListExtendedRequiredSize(len(entries)))
		p.SetUnlockFlags(s2c.CharacterCreationUnlockFlags(classUnlockFlags))
		p.SetMoveCnt(0) // OpenMU 序列化器不显式设置该字节
		p.SetCharacterCount(byte(len(entries)))
		p.SetIsVaultExtended(false)
		for i, e := range entries {
			block := p.Characters(i)
			if block == nil {
				return nil
			}
			block.SetSlotIndex(e.Slot)
			block.SetName(e.Name)
			block.SetLevel(e.Level)
			block.SetStatus(s2c.CharacterStatus(e.Status))
			block.SetIsItemBlockActive(false)
			block.SetGuildPosition(s2c.GuildMemberRole(e.GuildPosition))
			copy(block.Appearance(), e.AppearanceExt) // 27B 扩展外观
		}
		return v.send.Send(p.Bytes())
	}

	p := s2c.NewCharacterList(s2c.CharacterListRequiredSize(len(entries)))
	p.SetUnlockFlags(s2c.CharacterCreationUnlockFlags(classUnlockFlags))
	p.SetMoveCnt(0)
	p.SetCharacterCount(byte(len(entries)))
	p.SetIsVaultExtended(false)
	for i, e := range entries {
		block := p.Characters(i)
		if block == nil {
			return nil
		}
		block.SetSlotIndex(e.Slot)
		block.SetName(e.Name)
		block.SetLevel(e.Level)
		block.SetStatus(s2c.CharacterStatus(e.Status))
		block.SetIsItemBlockActive(false)
		block.SetGuildPosition(s2c.GuildMemberRole(e.GuildPosition))
		copy(block.Appearance(), e.Appearance) // 18B 预览外观
	}
	return v.send.Send(p.Bytes())
}

// ShowCharacterClassCreationUnlock 实现 action.PlayerView（C1 DE 00，5B）。
// 对照 ShowCharacterListPlugIn:46-49：标记为 0 时原版根本不发这条。
func (v *PlayerView) ShowCharacterClassCreationUnlock(flags byte) error {
	p := s2c.NewCharacterClassCreationUnlock()
	p.SetUnlockFlags(s2c.CharacterCreationUnlockFlags(flags))
	return v.send.Send(p.Bytes())
}

// ShowCharacterCreationSuccess 实现 action.PlayerView（F3 01，42B；对照
// ShowCreatedCharacterPlugIn）。PreviewData 填 0xFF。
func (v *PlayerView) ShowCharacterCreationSuccess(c action.CreatedCharacterView) error {
	p := s2c.NewCharacterCreationSuccessful()
	p.SetSuccess(true)
	p.SetCharacterName(c.Name)
	p.SetCharacterSlot(c.Slot)
	p.SetLevel(c.Level)
	p.SetClass(s2c.CharacterClassNumber(c.Class))
	p.SetCharacterStatus(c.Status)
	preview := p.PreviewData()
	for i := range preview {
		preview[i] = 0xFF
	}
	return v.send.Send(p.Bytes())
}

// ShowCharacterCreationFailed 实现 action.PlayerView（F3 01，5B；对照
// ShowCharacterCreationFailedPlugIn）。
func (v *PlayerView) ShowCharacterCreationFailed() error {
	return v.send.Send(s2c.NewCharacterCreationFailed().Bytes())
}

// ShowCharacterDeleteResponse 实现 action.PlayerView（F3 02，5B；对照
// ShowCharacterDeleteResponsePlugIn）。
func (v *PlayerView) ShowCharacterDeleteResponse(result action.CharacterDeleteResponseResult) error {
	p := s2c.NewCharacterDeleteResponse()
	p.SetResult(s2c.CharacterDeleteResult(result))
	return v.send.Send(p.Bytes())
}

// ShowCharacterFocused 实现 action.PlayerView（C1 F3 15，15B；对照 CharacterFocusedPlugIn
// 的 SendCharacterFocusedAsync(character.Name)——只回名字，不带属性）。
func (v *PlayerView) ShowCharacterFocused(name string) error {
	p := s2c.NewCharacterFocused()
	p.SetCharacterName(name)
	return v.send.Send(p.Bytes())
}

// ShowCharacterInformation 实现 action.PlayerView（C3 F3 03 92B Extended，MuMain 唯一接受版本）。
func (v *PlayerView) ShowCharacterInformation(info action.CharacterInformation) error {
	c := info.Character
	st := info.Stats
	if st == nil {
		st = defaultStats(c)
	}
	p := s2c.NewCharacterInformationExtended()
	p.SetX(c.X)
	p.SetY(c.Y)
	p.SetMapId(c.MapNumber)
	p.SetCurrentExperience(st.Experience)
	p.SetExperienceForNextLevel(st.ExperienceNext)
	p.SetLevelUpPoints(st.LevelUpPoints)
	p.SetStrength(st.Strength)
	p.SetAgility(st.Agility)
	p.SetVitality(st.Vitality)
	p.SetEnergy(st.Energy)
	p.SetLeadership(st.Leadership)
	p.SetCurrentHealth(st.CurrentHealth)
	p.SetMaximumHealth(st.MaximumHealth)
	p.SetCurrentMana(st.CurrentMana)
	p.SetMaximumMana(st.MaximumMana)
	p.SetCurrentShield(st.CurrentShield)
	p.SetMaximumShield(st.MaximumShield)
	p.SetCurrentAbility(st.CurrentAbility)
	p.SetMaximumAbility(st.MaximumAbility)
	p.SetMoney(st.Money)
	p.SetHeroState(s2c.CharacterHeroState(st.HeroState))
	p.SetStatus(s2c.CharacterStatus(c.Status))
	p.SetUsedFruitPoints(st.UsedFruitPoints)
	p.SetMaxFruitPoints(st.MaxFruitPoints)
	p.SetUsedNegativeFruitPoints(st.UsedNegFruit)
	p.SetMaxNegativeFruitPoints(st.MaxNegFruit)
	p.SetAttackSpeed(st.AttackSpeed)
	p.SetMagicSpeed(st.MagicSpeed)
	p.SetMaximumAttackSpeed(st.MaximumAttackSpeed)
	p.SetInventoryExtensions(st.InventoryExtensions)
	p.SetResets(st.Resets)
	return v.send.Send(p.Bytes())
}

// ShowCharacterInScope 实现 action.PlayerView（C2 12 单角色；spawned 时 Id 置 0x8000）。
func (v *PlayerView) ShowCharacterInScope(entry action.ScopeEntry) error {
	p := s2c.NewAddCharacterToScopeExtended(s2c.AddCharacterToScopeExtendedRequiredSize(scopeExtContentLen))
	id := entry.ID
	if entry.Spawned {
		id |= 0x8000
	}
	p.SetId(id)
	p.SetCurrentPositionX(entry.X)
	p.SetCurrentPositionY(entry.Y)
	p.SetTargetPositionX(entry.X)
	p.SetTargetPositionY(entry.Y)
	p.SetRotation(entry.Rotation)
	if entry.Stats != nil {
		p.SetHeroState(s2c.CharacterHeroState(entry.Stats.HeroState))
		p.SetAttackSpeed(entry.Stats.AttackSpeed)
		p.SetMagicSpeed(entry.Stats.MagicSpeed)
	} else {
		p.SetHeroState(s2c.CharacterHeroState_Normal)
		// 攻速/魔速由职业敏捷关系推导（原版 AttributeSystem 子集）——**不可写死 200**：
		// 客户端直接把它当动画倍速用，恒上限会让所有攻击动作都按最高速播放。
		atkSpeed, magSpeed := player.AttackSpeedsForClass(entry.ClassNumber)
		p.SetAttackSpeed(atkSpeed)
		p.SetMagicSpeed(magSpeed)
	}
	p.SetName(entry.Name)
	payload := p.AppearanceAndEffects()
	if len(entry.Appearance) == AppearanceExtSize {
		copy(payload[:AppearanceExtSize], entry.Appearance)
	} else {
		EncodeAppearanceExt(&item.Appearance{ClassNumber: int(entry.ClassNumber)}, payload[:AppearanceExtSize])
	}
	payload[AppearanceExtSize] = 0 // 可见效果数（M6 无魔法效果系统）
	return v.send.Send(p.Bytes())
}

// ShowNpcsInScope 实现 action.PlayerView（C2 13 怪物/NPC 入视野，批量分包）。
// 对照原版 NewNpcsInScopePlugIn.WriteNpcsInScope：入视野即出生（isSpawned=true），
// **Id 必须置 0x8000 出生位**——0x8000 是旗标不是 ID 段，客户端按 id&0x7FFF 索引对象。
func (v *PlayerView) ShowNpcsInScope(entries []action.NpcScopeEntry) error {
	const chunk = 32 // 每包 ≤32 只（NpcData 10B → 325B，远小于帧上限）
	for start := 0; start < len(entries); start += chunk {
		end := start + chunk
		if end > len(entries) {
			end = len(entries)
		}
		part := entries[start:end]
		p := s2c.NewAddNpcsToScope(s2c.AddNpcsToScopeRequiredSize(len(part)))
		p.SetNpcCount(byte(len(part)))
		for i, e := range part {
			nd := p.NPCs(i)
			if nd == nil {
				break
			}
			nd.SetId(e.ID | 0x8000)
			nd.SetTypeNumber(e.TypeNumber)
			nd.SetCurrentPositionX(e.X)
			nd.SetCurrentPositionY(e.Y)
			nd.SetTargetPositionX(e.X)
			nd.SetTargetPositionY(e.Y)
			nd.SetRotation(e.Rotation)
			nd.SetEffectCount(0)
		}
		if err := v.send.Send(p.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// ShowDropsInScope 实现 action.PlayerView（地面掉落物；T2-1）。
//
// 两条出站码按**客户端版本各自的插件选择**（OpenMU 对同一 S6 客户端即如此）：
//   - 物品 → ItemsDropped（C2 0x20），DroppedItem 内嵌 ItemSerializerExtended 的
//     5~15B 动态物品数据，故 stride 由该件物品的实际长度决定，逐件独立一包；
//   - 金币 → MoneyDroppedExtended（**C1 0x2F**）。
//
// 金币**不能**用 MoneyDropped（C2 0x20）：MuMain 的 WSclient.cpp 顶层 switch 把
// 0x20 派发给 ReceiveCreateItemViewportExtended（物品），0x2F 才派发给
// ReceiveCreateMoney；发 0x20 的金币会被当物品解析 → 地面永远看不到金币。
// 对照 OpenMU：ShowDroppedItemsPlugIn（0x20）+ ShowMoneyDropExtendedPlugIn
// （C1 0x2F，[MinimumClient(106,3)]，与客户端的 0x2F 分派一致）。
func (v *PlayerView) ShowDropsInScope(items []action.DropEntry, money []action.MoneyEntry) error {
	for _, e := range items {
		if len(e.Data) < ItemExtendedMinSize || len(e.Data) > ItemExtendedMaxSize {
			return fmt.Errorf("remote: 掉落物数据长度 %d 超出扩展布局 5~%d", len(e.Data), ItemExtendedMaxSize)
		}
		stride := s2c.ObjectIdLength + 2 + len(e.Data) // id(2) + x + y + 物品数据(N)
		p := s2c.NewItemsDropped(s2c.ItemsDroppedRequiredSize(1, stride))
		p.SetItemCount(1)
		di := p.Items(0, stride)
		if di == nil {
			return fmt.Errorf("remote: ItemsDropped 布局异常")
		}
		di.SetId(e.ID)
		if e.FreshDrop {
			di.SetIsFreshDrop(true)
		}
		di.SetPositionX(e.X)
		di.SetPositionY(e.Y)
		copy(di.ItemData(), e.Data)
		if err := v.send.Send(p.Bytes()); err != nil {
			return err
		}
	}
	for _, m := range money {
		p := s2c.NewMoneyDroppedExtended()
		p.SetId(m.ID)
		if m.FreshDrop {
			p.SetIsFreshDrop(true)
		}
		p.SetPositionX(m.X)
		p.SetPositionY(m.Y)
		p.SetAmount(m.Amount)
		if err := v.send.Send(p.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// ShowObjectAnimation 实现 action.PlayerView（C1 18 ObjectAnimation）。
//
// 对照 OpenMU ShowAnimationPlugIn.ShowMonsterAttackAnimationAsync（animation = 120）
// 经 SendObjectAnimationAsync(animatingId, direction.ToPacketByte(), animation, targetId) 出站，
// 以及客户端 ReceiveAction(0x18)：
//   - ObjectId 为动画发起者，按接收者视角解析（自己 = 0x200）；
//   - Direction 用 OpenMU Direction 枚举的 ToPacketByte（= Direction - 1），
//     与行走 nibble 同一套编码——客户端按 Angle = (方向字节 - 1) * 45 还原朝向；
//   - animation = AT_ATTACK1(120) 时客户端 SetPlayerAttack 并吸附到 TargetX/TargetY；
//   - TargetId 是被攻击的目标（客户端据此设定朝向/目标）。
func (v *PlayerView) ShowObjectAnimation(animatingID uint16, direction, animation byte, targetID uint16) error {
	p := s2c.NewObjectAnimation()
	p.SetObjectId(animatingID)
	p.SetDirection(direction)
	p.SetAnimation(animation)
	p.SetTargetId(targetID)
	return v.send.Send(p.Bytes())
}

// ShowObjectsOutOfScope 实现 action.PlayerView（C1 14 移出视野，批量对象 ID；T2-1）。
func (v *PlayerView) ShowObjectsOutOfScope(ids []uint16) error {
	if len(ids) == 0 {
		return nil
	}
	p := s2c.NewMapObjectOutOfScope(s2c.MapObjectOutOfScopeRequiredSize(len(ids)))
	p.SetObjectCount(byte(len(ids)))
	for i, id := range ids {
		p.Objects(i).SetId(id)
	}
	return v.send.Send(p.Bytes())
}

// ShowObjectHit 实现 action.PlayerView（C1 11 **ObjectHitExtended** 16B；
// 对照 ShowHitExtendedPlugIn：S6 客户端用扩展形态——基础 10B 会被按 16B
// 错位解析，导致无伤害数字 + SD/AG/MP 乱变。healthStatus = 受击者血量条
// current/max*250（0xFF=无该资源），ID 按 GetId(playerOfView) 语义）。
func (v *PlayerView) ShowObjectHit(targetID uint16, healthStatus, shieldStatus byte, damage, shieldDamage uint32, kind action.DamageKind) error {
	p := s2c.NewObjectHitExtended()
	p.SetKind(s2c.DamageKind(kind))
	p.SetObjectId(targetID)
	p.SetHealthStatus(healthStatus)
	p.SetShieldStatus(shieldStatus)
	p.SetHealthDamage(damage)
	p.SetShieldDamage(shieldDamage)
	return v.send.Send(p.Bytes())
}

// ShowObjectGotKilled 实现 action.PlayerView（C1 17 ObjectGotKilled）。
func (v *PlayerView) ShowObjectGotKilled(victimID uint16) error {
	p := s2c.NewObjectGotKilled()
	p.SetKilledId(victimID)
	p.SetSkillId(0xFFFF) // 普通攻击无技能（原版 skillId = 0xFFFF/0 占位）
	return v.send.Send(p.Bytes())
}

// ShowHealthUpdate 实现 action.PlayerView（C1 26 FF CurrentHealthAndShield）。
func (v *PlayerView) ShowHealthUpdate(health, shield uint16) error {
	p := s2c.NewCurrentHealthAndShield()
	p.SetHealth(health)
	p.SetShield(shield)
	return v.send.Send(p.Bytes())
}

// ShowItemAddedToInventory 实现 action.PlayerView（C3 22：拾取成功，物品进背包）。
// itemData 为 ItemSerializerExtended 的 5~15B 动态编码——客户端
// ReceiveGetItem/ReceiveInventoryExtended 同样用 CalcItemLength 定长，长度必须精确。
func (v *PlayerView) ShowItemAddedToInventory(slot byte, itemData []byte) error {
	if len(itemData) < ItemExtendedMinSize || len(itemData) > ItemExtendedMaxSize {
		return fmt.Errorf("remote: 背包物品数据长度 %d 超出扩展布局 5~%d", len(itemData), ItemExtendedMaxSize)
	}
	p := s2c.NewItemAddedToInventory(s2c.ItemAddedToInventoryRequiredSize(len(itemData)))
	p.SetInventorySlot(slot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

// ShowItemPickUpFailed 实现 action.PlayerView（C3 22 ItemPickUpRequestFailed，4B）。
// 对照 ItemPickUpFailedPlugIn + EnumExtensions.Convert(:79-88)：动作层枚举逐值映射到
// 线上 253/254/255（原版 Undefined 从不发送）。
func (v *PlayerView) ShowItemPickUpFailed(reason action.ItemPickFailReason) error {
	p := s2c.NewItemPickUpRequestFailed()
	switch reason {
	case action.ItemPickFailStacked:
		p.SetFailReason(s2c.ItemPickUpFailReason_ItemStacked)
	case action.ItemPickFailMaximumMoney:
		p.SetFailReason(s2c.ItemPickUpFailReason___MaximumInventoryMoneyReached)
	default:
		p.SetFailReason(s2c.ItemPickUpFailReason_General)
	}
	return v.send.Send(p.Bytes())
}

// ShowInventoryMoneyUpdate 实现 action.PlayerView（C3 22 FE：金币变化）。
func (v *PlayerView) ShowInventoryMoneyUpdate(money uint32) error {
	p := s2c.NewInventoryMoneyUpdate()
	p.SetMoney(money)
	return v.send.Send(p.Bytes())
}

// ShowItemDropRemoved 实现 action.PlayerView（C2 21：地面物移除，批量）。
func (v *PlayerView) ShowItemDropRemoved(ids []uint16) error {
	if len(ids) == 0 {
		return nil
	}
	p := s2c.NewItemDropRemoved(s2c.ItemDropRemovedRequiredSize(len(ids)))
	p.SetItemCount(byte(len(ids)))
	for i, id := range ids {
		p.ItemData(i).SetId(id)
	}
	return v.send.Send(p.Bytes())
}

// ShowItemDropResponse 实现 action.PlayerView（C1 23：丢弃结果；成功则客户端移除背包物品）。
func (v *PlayerView) ShowItemDropResponse(success bool, slot byte) error {
	p := s2c.NewItemDropResponse()
	p.SetSuccess(success)
	p.SetInventorySlot(slot)
	return v.send.Send(p.Bytes())
}

// ShowObjectMovedInstant 实现 action.PlayerView（C1 15：瞬移/位置回拉同步，
// 对照 ObjectMovedPlugIn.ObjectMovedAsync 的 MoveType.Instant 分支）。
func (v *PlayerView) ShowObjectMovedInstant(objectID uint16, x, y byte) error {
	p := s2c.NewObjectMoved()
	p.SetHeaderCode(s2c.ObjectMovedCode)
	p.SetObjectId(objectID)
	p.SetPositionX(x)
	p.SetPositionY(y)
	return v.send.Send(p.Bytes())
}

// ShowMapChanged 实现 action.PlayerView（C3 1C：换图/重生确认，客户端回 F3 12）。
//
// 出站直接用生成器产物 NewMapChanged()（15B，与 OpenMU MapChangedRef 逐字节一致）：
//	d[3]=SubCode 0x0F  d[4]=IsMapChange bit0  d[5..6]=MapNumber u16BE  d[7..9]=X/Y/Rot
//
// MuMain 按 PRECEIVE_TELEPORT_POSITION（WSclient.h，**非 pack(1)** 自然对齐，sizeof=10）
// 消费：Flag.lo=d[4]、Flag.hi=d[5]、Map=d[6]、X/Y/Angle=d[7..9]。代入生成帧逐字段成立：
// Flag.lo=IsMapChange=1、Flag.hi=地图号高字节（MU 地图号恒 ≤255 → 恒 0）→ Flag=1≠0 走
// ClearItems+LoadWorld 重载分支并回 F3 12、Map=d[6]=低字节。
// ⚠️ 兼容性事实：C# 声明偏移（MapNumber u16BE@5..6）与客户端消费偏移（Map@6）不一致，
// 线上兼容依赖"地图号 ≤255 → 高字节恒 0"——OpenMU 同款巧合，非本仓引入；
// 由下方字节 golden 断言（d[5]==0）把该前提锁死，地图号超 255 时此断言会立刻暴露。
func (v *PlayerView) ShowMapChanged(mapNumber uint16, x, y, rotation byte) error {
	p := s2c.NewMapChanged()
	p.SetMapNumber(mapNumber)
	p.SetPositionX(x)
	p.SetPositionY(y)
	p.SetRotation(rotation)
	return v.send.Send(p.Bytes())
}

// ShowMapChangeFailed 实现 action.PlayerView（C3 1C，Flag=0）。
// 对照 MapChangePlugIn.MapChangeFailedAsync → SendMessageAsync(success:false)：
// 地图号/坐标取玩家**当前**值，客户端 ReceiveTeleport 见 Flag==0 只做传送结束动画。
func (v *PlayerView) ShowMapChangeFailed(mapNumber uint16, x, y, rotation byte) error {
	p := s2c.NewMapChanged()
	p.SetIsMapChange(false)
	p.SetMapNumber(mapNumber)
	p.SetPositionX(x)
	p.SetPositionY(y)
	p.SetRotation(rotation)
	return v.send.Send(p.Bytes())
}

// ShowInventoryList 实现 action.PlayerView（C1 F3 10 CharacterInventory）。
// 对照 UpdateInventoryListPlugIn：先 F3 12 客户端清装备栏（ReceiveInventoryExtended 的
// UnequipAllItems+DeleteAllItems），再逐件写入 {1B 槽位 + 扩展物品编码 5~15B}；
// actualSize 收缩语义与 C2 31 相同（stride 只用于预分配）。
func (v *PlayerView) ShowInventoryList(items []action.MerchantItemView) error {
	size := s2c.CharacterInventoryRequiredSize(len(items), 0) // 6B 头
	for _, it := range items {
		if len(it.Data) < ItemExtendedMinSize || len(it.Data) > ItemExtendedMaxSize {
			return fmt.Errorf("remote: 背包物品槽 %d 数据长度 %d 超出扩展布局 5~%d", it.Slot, len(it.Data), ItemExtendedMaxSize)
		}
		size += 1 + len(it.Data)
	}
	p := s2c.NewCharacterInventory(size)
	p.SetItemCount(byte(len(items)))
	off := 6
	for _, it := range items {
		stride := 1 + len(it.Data)
		si := s2c.AsStoredItem(p.Bytes()[off : off+stride])
		si.SetItemSlot(it.Slot)
		copy(si.ItemData(), it.Data)
		off += stride
	}
	return v.send.Send(p.Bytes())
}

// ShowObjectWalked 实现 action.PlayerView（C1 D4；步点编码逐行复刻
// ObjectMovedPlugInExtended：steps=0 无步点；否则字节数=steps/2+2，首字节低 nibble 放字节数）。
func (v *PlayerView) ShowObjectWalked(move action.WalkedMove) error {
	steps := len(move.Steps)
	contentLen := 0
	if steps > 0 {
		contentLen = steps/2 + 2
	}
	p := s2c.NewObjectWalkedExtended(s2c.ObjectWalkedExtendedRequiredSize(contentLen))
	p.SetHeaderCode(s2c.ObjectWalkedExtendedCode)
	p.SetObjectId(move.ObjectID)
	p.SetSourceX(move.SourceX)
	p.SetSourceY(move.SourceY)
	p.SetTargetX(move.TargetX)
	p.SetTargetY(move.TargetY)
	p.SetTargetRotation(move.Rotation)
	p.SetStepCount(byte(steps))
	if steps > 0 {
		stepData := p.StepData()
		stepData[0] = move.Steps[0]<<4 | byte(contentLen)
		for i := 0; i < contentLen-1; i += 2 {
			b := move.Steps[i] << 4
			if len(move.Steps) > i+1 {
				b |= move.Steps[i+1]
			}
			stepData[1+i/2] = b
		}
	}
	return v.send.Send(p.Bytes())
}

// ShowItemMoved 实现 action.PlayerView（C3 24：背包内搬运成功；T2-5）。
//
// 帧布局（客户端 PHEADER_DEFAULT_SUBCODE_ITEM_EXTENDED：PBMSG_HEADER 3B + SubCode@3 +
// Index@4 + Item@5）与本包的 TargetStorageType@3/TargetSlot@4/ItemData@5 一一对应，
// 总长 = 5 + len(itemData)。客户端以 `Data->SubCode != 255` 判成功，并把它当 STORAGE_TYPE
// 分派（0 = INVENTORY → 走 EquipItem / InsertItem / 扩展页 InsertItem）。
func (v *PlayerView) ShowItemMoved(targetStorage, targetSlot byte, itemData []byte) error {
	if len(itemData) < ItemExtendedMinSize || len(itemData) > ItemExtendedMaxSize {
		return fmt.Errorf("remote: ItemMoved 物品数据长度 %d 超出扩展布局 5~%d", len(itemData), ItemExtendedMaxSize)
	}
	p := s2c.NewItemMoved(s2c.ItemMovedRequiredSize(len(itemData)))
	p.SetTargetStorageType(s2c.ItemStorageKind(targetStorage))
	p.SetTargetSlot(targetSlot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

// ShowItemMoveFailed 实现 action.PlayerView（C3 24 FF：搬运失败；T2-5）。
//
// 客户端对失败帧仍会先跑 `CalcItemLength(itemData)`，所以 ItemData 段不能缺——
// 原版 ItemMoveFailedPlugIn 在 item 为 null 时按 NeededSpace(15) 写零，这里照做。
// 注意 C3HeaderWithSubCode 是 4B（type/size/code/subcode），而 ItemData 从第 5 字节起，
// 中间的 Index 字节原版也不写（客户端失败分支不读它）——保持 0 即可。
func (v *PlayerView) ShowItemMoveFailed(itemData []byte) error {
	n := len(itemData)
	if n == 0 {
		n = ItemExtendedMaxSize // 15，对照 ItemSerializerExtended.NeededSpace
	}
	p := s2c.NewItemMoveRequestFailed(s2c.ItemMoveRequestFailedRequiredSize(n))
	if len(itemData) > 0 {
		copy(p.ItemData(), itemData)
	}
	return v.send.Send(p.Bytes())
}

// ShowItemRemoved 实现 action.PlayerView（C1 28：物品从背包消失；T2-5）。
//
// TrueFlag 必须保持构造器的默认 1（对照 OpenMU ItemRemoved 构造器 `this.TrueFlag = 1`）：
// 客户端 ReceiveDeleteInventory 用 `if (Data->Value) EnableUse = 0;` 解除"使用去抖"
// （ZzzInventory.cpp SendRequestUse 里 `if (EnableUse > 0) return;` 会吞掉后续所有使用请求，
// 且 EnableUse 只被清 0、从不递减）。写成 0 会让消耗品销毁后客户端永久卡在使用态。
func (v *PlayerView) ShowItemRemoved(inventorySlot byte) error {
	p := s2c.NewItemRemoved()
	p.SetInventorySlot(inventorySlot)
	return v.send.Send(p.Bytes())
}

// ShowItemDurabilityChanged 实现 action.PlayerView（C1 2A：耐久/堆叠数量变化；T2-5）。
func (v *PlayerView) ShowItemDurabilityChanged(inventorySlot, durability byte, byConsumption bool) error {
	p := s2c.NewItemDurabilityChanged()
	p.SetInventorySlot(inventorySlot)
	p.SetDurability(durability)
	p.SetByConsumption(byConsumption)
	return v.send.Send(p.Bytes())
}

// ShowAppearanceChanged 实现 action.PlayerView（C1 25 装备外观变化；T2-5）。
//
// 帧长固定 14B。客户端 PCHANGE_CHARACTER_EXTENDED 的 Key 是 WORD，编译器在
// PBMSG_HEADER(3B) 之后插入 1 字节对齐填充，因此 Key 落在偏移 4——与 OpenMU
// 的 ChangedPlayerId@4 完全吻合。**不可**按 13B"紧凑布局"发，那样后面所有字段错位。
func (v *PlayerView) ShowAppearanceChanged(c action.AppearanceChange) error {
	p := s2c.NewAppearanceChangedExtended()
	p.SetChangedPlayerId(c.ChangedPlayerID)
	p.SetItemSlot(c.ItemSlot)
	p.SetItemGroup(c.ItemGroup)
	p.SetItemNumber(c.ItemNumber)
	p.SetItemLevel(c.ItemLevel)
	p.SetExcellentFlags(c.ExcellentFlags)
	p.SetAncientDiscriminator(c.AncientDiscriminator)
	p.SetIsAncientSetComplete(c.IsAncientSetComplete)
	return v.send.Send(p.Bytes())
}

// ShowCurrentStatsExtended 实现 action.PlayerView（C1 26 FF **CurrentStatsExtended**，24B；T2-6）。
//
// 对照 UpdateStatsExtendedPlugIn.OnCurrentStatsChangedAsync → SendCurrentStatsExtendedAsync
// （Health, Shield, Mana, Ability, AttackSpeed, MagicSpeed）——字段全为**小端**，
// 与 C1 26 FE/24B 之外的其它 0x26 子码（BE）**不是**同一套字节序，混淆会让客户端血量乱跳。
// 只发给操作者自己（原版 InvokeViewPlugInAsync 语义）。
func (v *PlayerView) ShowCurrentStatsExtended(stats action.CurrentStats) error {
	p := s2c.NewCurrentStatsExtended()
	p.SetHealth(stats.Health)
	p.SetShield(stats.Shield)
	p.SetMana(stats.Mana)
	p.SetAbility(stats.Ability)
	p.SetAttackSpeed(stats.AttackSpeed)
	p.SetMagicSpeed(stats.MagicSpeed)
	return v.send.Send(p.Bytes())
}

// ShowMaximumStatsExtended 实现 action.PlayerView（C1 26 FE **MaximumStatsExtended**，20B；T2-6）。
// 对照 OnMaximumStatsChangedAsync → SendMaximumStatsExtendedAsync（Health, Shield, Mana, Ability）。
func (v *PlayerView) ShowMaximumStatsExtended(stats action.MaximumStats) error {
	p := s2c.NewMaximumStatsExtended()
	p.SetHealth(stats.Health)
	p.SetShield(stats.Shield)
	p.SetMana(stats.Mana)
	p.SetAbility(stats.Ability)
	return v.send.Send(p.Bytes())
}

// ShowItemConsumptionFailed 实现 action.PlayerView（C1 26 FD，12B；T2-6）。
//
// 用**扩展**形态（ItemConsumptionFailedExtended）：S6 客户端的 0x26 分派统一进
// ReceiveStatsExtended，按 SubCode 0xFD 置 `EnableUse = 0`；扩展形态多带的当下血/盾
// 让客户端把本地预测纠正回服务端真值（原版 RequestedItemConsumptionFailedExtendedPlugIn）。
func (v *PlayerView) ShowItemConsumptionFailed(health, shield uint32) error {
	p := s2c.NewItemConsumptionFailedExtended()
	p.SetHealth(health)
	p.SetShield(shield)
	return v.send.Send(p.Bytes())
}

// ShowMagicEffectStatus 实现 action.PlayerView（C1 07 MagicEffectStatus，7B；T2-6）。
// 对照 DeActivateMagicEffectPlugIn.SendMagicEffectStatusAsync：isActive 是 flag 位，
// objectId 按接收者视角解析（自己 = 0x200），EffectId 是效果编号的低字节。
func (v *PlayerView) ShowMagicEffectStatus(active bool, objectID uint16, effectNumber byte) error {
	p := s2c.NewMagicEffectStatus()
	p.SetIsActive(active)
	p.SetPlayerId(objectID)
	p.SetEffectId(effectNumber)
	return v.send.Send(p.Bytes())
}

// ShowFruitConsumptionResponse 实现 action.PlayerView（C1 2C FruitConsumptionResponse，7B；TRIM-01）。
// 对照 FruitConsumptionResponsePlugIn.ShowResponseAsync：result/statType 的线值与
// action 层 FruitOutcome* / FruitStat 数值一致（枚举定义逐值对齐 s2c），直接转换。
func (v *PlayerView) ShowFruitConsumptionResponse(result int, statPoints uint16, statType action.FruitStat) error {
	p := s2c.NewFruitConsumptionResponse()
	p.SetResult(s2c.FruitConsumptionResult(result))
	p.SetStatPoints(statPoints)
	p.SetStatType(s2c.FruitStatType(statType))
	return v.send.Send(p.Bytes())
}

// ShowItemUpgraded 实现 action.PlayerView（C1 F3 14 InventoryItemUpgraded；TRIM-01）。
// 对照 ItemUpgradedPlugIn.ItemUpgradedAsync：槽位 + ItemSerializerExtended 完整编码
// （帧长 = 5 + 物品编码长度，与 ShowItemAddedToInventory 同一 RequiredSize 语义）。
func (v *PlayerView) ShowItemUpgraded(inventorySlot byte, itemData []byte) error {
	if len(itemData) < ItemExtendedMinSize || len(itemData) > ItemExtendedMaxSize {
		return fmt.Errorf("remote: 强化物品数据长度 %d 超出扩展布局 5~%d", len(itemData), ItemExtendedMaxSize)
	}
	p := s2c.NewInventoryItemUpgraded(s2c.InventoryItemUpgradedRequiredSize(len(itemData)))
	p.SetInventorySlot(inventorySlot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

// ShowExperienceGained 实现 action.PlayerView（C3 16 **ExperienceGainedExtended**，16B；T2-7）。
//
// **必须发扩展形态**：MuMain WSclient.cpp 的 0x16 分派是
// `if (Size >= sizeof(PRECEIVE_EXP_EXTENDED)) ReceiveDieExpLarge else ReceiveDieExp`，
// 9B 紧凑形态会被当 16B 错位解析（经验数字乱跳、且不触发击杀表现）。
func (v *PlayerView) ShowExperienceGained(g action.ExperienceGainPacket) error {
	p := s2c.NewExperienceGainedExtended()
	p.SetType(s2c.AddResult(g.Result))
	p.SetAddedExperience(g.Experience)
	p.SetDamageOfLastHit(g.Damage)
	p.SetKilledObjectId(g.KilledObjectID)
	p.SetKillerObjectId(g.KillerObjectID)
	return v.send.Send(p.Bytes())
}

// ShowLevelUp 实现 action.PlayerView（C1 F3 05 **CharacterLevelUpdateExtended**，32B；T2-7）。
//
// 帧头是 C1+F3+子码 0x05（不是 C3）——对照 UpdateLevelExtendedPlugIn 的
// `SendCharacterLevelUpdateExtendedAsync`；客户端 ReceiveLevelUp 据此刷新等级与升级点数。
func (v *PlayerView) ShowLevelUp(info action.LevelUpInfo) error {
	p := s2c.NewCharacterLevelUpdateExtended()
	p.SetLevel(info.Level)
	p.SetLevelUpPoints(info.LevelUpPoints)
	p.SetMaximumHealth(info.MaximumHealth)
	p.SetMaximumMana(info.MaximumMana)
	p.SetMaximumShield(info.MaximumShield)
	p.SetMaximumAbility(info.MaximumAbility)
	p.SetFruitPoints(info.FruitPoints)
	p.SetMaximumFruitPoints(info.MaximumFruitPoints)
	p.SetNegativeFruitPoints(info.NegativeFruitPoints)
	p.SetMaximumNegativeFruitPoints(info.MaximumNegativeFruitPoints)
	return v.send.Send(p.Bytes())
}

// ShowEffect 实现 action.PlayerView（C1 48 ShowEffect，6B）。
// 对照 ShowEffectPlugIn：PlayerId 按接收者视角解析（自己 = 0x200），Effect 用枚举的低字节。
func (v *PlayerView) ShowEffect(targetID uint16, kind action.EffectKind) error {
	p := s2c.NewShowEffect()
	p.SetPlayerId(targetID)
	p.SetEffect(showEffectType(kind))
	return v.send.Send(p.Bytes())
}

// showEffectType 把动作层效果词汇映射成协议枚举（数值对照 s2c.EffectType2）。
func showEffectType(kind action.EffectKind) s2c.EffectType2 {
	switch kind {
	case action.EffectShieldPotion:
		return s2c.EffectType2_ShieldPotion
	case action.EffectLevelUp:
		return s2c.EffectType2_LevelUp
	case action.EffectShieldLost:
		return s2c.EffectType2_ShieldLost
	default:
		return s2c.EffectType2_LevelUp
	}
}

// PatchAppearanceSlot 就地更新 27B 进图外观里某个装备槽的编码，其余槽位与// Pose/GM 位保持不变（返回 false 表示该槽不参与外观编码）。
//
// 为什么是"补丁"而不是整体重编码：EncodeAppearanceExt 需要 Pose/GameMaster/职业等输入，
// 而这些在切换装备时并没有变化；整体重编码会把调用方（GS）并不知道的位抹掉。
// 槽位偏移与 EncodeAppearanceExt 的 shinySlots 顺序同源：
//
//	0..6 → 2 + slot*3（左手/右手/头/铠/裤/手/鞋），7 → 23（翅膀），8 → 25（宠物）。
func PatchAppearanceSlot(encoded []byte, slot int, e *item.Equip) bool {
	if len(encoded) < AppearanceExtSize {
		return false
	}
	switch {
	case slot >= 0 && slot <= item.SlotBoots:
		setShinyItem(encoded[2+slot*3:], e)
		return true
	case slot == item.SlotWings:
		setUnshinyItem(encoded[23:], e)
		return true
	case slot == item.SlotPet:
		setUnshinyItem(encoded[25:], e)
		// 宠物槽的 Fenrir 档位与 group/number 共用第 25 字节（高位 nibble 是 group），
		// setUnshinyItem 已经整字节重写，必须按 EncodeAppearanceExt 的同一分支重新置位。
		if e != nil {
			if e.BlackFenrir {
				encoded[25] |= 0x02
			}
			if e.BlueFenrir {
				encoded[25] |= 0x04
			}
			if e.GoldFenrir {
				encoded[25] |= 0x06
			}
		}
		return true
	}
	return false
}

// defaultStats 兜底角色属性（handler 原逻辑等价）。
func defaultStats(c *entity.Character) *entity.CharStats {
	return player.NewCharStats(c.ClassNumber, c.Level)
}

// ShowChatMessage 实现 action.PlayerView（C1 00 ChatMessage）。
// channel 字节位于 d[2]（与 code 同位）：Normal=0、Whisper=2。
func (v *PlayerView) ShowChatMessage(sender, message string, msgType action.ChatMessageType) error {
	const maxContent = 255 - 14 // ChatMessageRequiredSize = content + 14，C1 长度单字节 ≤ 255
	if len(message) > maxContent {
		message = message[:maxContent]
	}
	p := s2c.NewChatMessage(s2c.ChatMessageRequiredSize(len(message)))
	p.SetType(s2c.ChatMessageType(msgType))
	p.SetSender(sender)
	p.SetMessage(message)
	return v.send.Send(p.Bytes())
}

// ShowMessage 实现 action.PlayerView（C1 0D ServerMessage，对照 ShowMessagePlugIn）。
// 超过 241 UTF-8 字节按字符边界拆多包；Season>0 时前缀 9 个 0（客户端渲染需要）。
func (v *PlayerView) ShowMessage(message string, msgType action.MessageType) error {
	if message == "" {
		return nil
	}
	const maxMessageBytes = 241
	if len(message) > maxMessageBytes {
		part := utf8Prefix(message, maxMessageBytes)
		if err := v.ShowMessage(part, msgType); err != nil {
			return err
		}
		return v.ShowMessage(message[len(part):], msgType)
	}
	if v.clientVersion.Season > 0 {
		message = "000000000" + message
	}
	p := s2c.NewServerMessage(s2c.ServerMessageRequiredSize(len(message)))
	p.SetType(s2c.MessageType(msgType))
	p.SetMessage(message)
	return v.send.Send(p.Bytes())
}

// utf8Prefix 返回 s 中最长的、字节数不超过 max 的字符前缀。
func utf8Prefix(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// ShowPartyRequest 实现 action.PlayerView（C1 40 PartyRequest）。
func (v *PlayerView) ShowPartyRequest(requesterID uint16) error {
	p := s2c.NewPartyRequest()
	p.SetRequesterId(requesterID)
	return v.send.Send(p.Bytes())
}

// ShowPartyList 实现 action.PlayerView（C1 42 01 PartyList）。
// 自己所在块按 selfIndex 把 Id 置为哨兵 0x200（客户端据此识别自己）；
// PartyMember 块本身不携带 Id 字段，因此这里无需写入 ID，只传槽位。
func (v *PlayerView) ShowPartyList(selfIndex byte, members []action.PartyMemberView) error {
	p := s2c.NewPartyList(s2c.PartyListRequiredSize(len(members)))
	p.SetCount(byte(len(members)))
	for i := range members {
		m := p.Members(i)
		if m == nil {
			break
		}
		e := members[i]
		m.SetIndex(e.Index)
		m.SetName(e.Name)
		m.SetMapId(e.MapID)
		m.SetPositionX(e.X)
		m.SetPositionY(e.Y)
		m.SetCurrentHealth(e.CurrentHealth)
		m.SetMaximumHealth(e.MaximumHealth)
	}
	_ = selfIndex // 块不携带 Id，客户端按槽位对齐——保留形参以对齐接口语义
	return v.send.Send(p.Bytes())
}

// ShowPartyMemberRemoved 实现 action.PlayerView（C1 43 RemovePartyMember）。
func (v *PlayerView) ShowPartyMemberRemoved(index byte) error {
	p := s2c.NewRemovePartyMember()
	p.SetIndex(index)
	return v.send.Send(p.Bytes())
}

// ShowPartyHealth 实现 action.PlayerView（C1 44 PartyHealthUpdate）。
func (v *PlayerView) ShowPartyHealth(members []action.PartyHealthView) error {
	p := s2c.NewPartyHealthUpdate(s2c.PartyHealthUpdateRequiredSize(len(members)))
	p.SetCount(byte(len(members)))
	for i := range members {
		h := p.Members(i)
		if h == nil {
			break
		}
		h.SetIndex(members[i].Index)
		h.SetValue(members[i].Value)
	}
	return v.send.Send(p.Bytes())
}

// ShowTradeRequest 实现 action.PlayerView（C1 36 TradeRequest）。
func (v *PlayerView) ShowTradeRequest(requesterName string) error {
	p := s2c.NewTradeRequest()
	p.SetName(requesterName)
	return v.send.Send(p.Bytes())
}

// ShowTradeRequestAnswer 实现 action.PlayerView（C1 37 TradeRequestAnswer）。
func (v *PlayerView) ShowTradeRequestAnswer(accepted bool, partnerName string, partnerLevel uint16) error {
	p := s2c.NewTradeRequestAnswer()
	p.SetAccepted(accepted)
	p.SetName(partnerName)
	p.SetTradePartnerLevel(partnerLevel)
	p.SetGuildId(0)
	return v.send.Send(p.Bytes())
}

// ShowTradeItemAdded 实现 action.PlayerView（C1 39 TradeItemAdded）。
func (v *PlayerView) ShowTradeItemAdded(toSlot byte, itemData []byte) error {
	p := s2c.NewTradeItemAdded(s2c.TradeItemAddedRequiredSize(len(itemData)))
	p.SetToSlot(toSlot)
	copy(p.ItemData(), itemData)
	return v.send.Send(p.Bytes())
}

// ShowTradeItemRemoved 实现 action.PlayerView（C1 38 TradeItemRemoved）。
func (v *PlayerView) ShowTradeItemRemoved(slot byte) error {
	p := s2c.NewTradeItemRemoved()
	p.SetSlot(slot)
	return v.send.Send(p.Bytes())
}

// ShowTradeMoneyUpdate 实现 action.PlayerView（C1 3B TradeMoneyUpdate）。
func (v *PlayerView) ShowTradeMoneyUpdate(amount uint32) error {
	p := s2c.NewTradeMoneyUpdate()
	p.SetMoneyAmount(amount)
	return v.send.Send(p.Bytes())
}

// ShowTradeMoneySetResponse 实现 action.PlayerView（C1 3A 01 TradeMoneySetResponse）。
func (v *PlayerView) ShowTradeMoneySetResponse() error {
	return v.send.Send(s2c.NewTradeMoneySetResponse().Bytes())
}

// ShowTradeButtonState 实现 action.PlayerView（C1 3C TradeButtonStateChanged）。
func (v *PlayerView) ShowTradeButtonState(state action.TradeButtonState) error {
	p := s2c.NewTradeButtonStateChanged()
	p.SetState(s2c.TradeButtonState(state))
	return v.send.Send(p.Bytes())
}

// ShowTradeFinished 实现 action.PlayerView（C1 3D TradeFinished）。
func (v *PlayerView) ShowTradeFinished(result action.TradeResult) error {
	p := s2c.NewTradeFinished()
	p.SetResult(s2c.TradeResult(result))
	return v.send.Send(p.Bytes())
}
