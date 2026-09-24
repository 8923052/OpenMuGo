// Package action 是动作层骨架（doc/10 T0-b）：玩家的每一个"意图"在这里实现，
// 对应原版 GameLogic.PlayerActions（194 个）。铁律（doc/10 §4.2）：
//   - 本包不 import proto / server / view / version / transport（layoutcheck 强制）；
//   - 出站一律通过 PlayerView 接口（消费者侧定义，view/remote 按客户端版本实现）；
//   - handler 只做三件事：取帧字段 → 构造入参 → 调用动作；不允许在 handler 里拼出站包。
package action

import (
	"mugo/internal/gamelogic/entity"
)

// LoginResult 是登录结果的**语义**码（动作层词汇；协议映射在视图层）。
type LoginResult int

const (
	LoginOkay LoginResult = iota
	LoginInvalidPassword
	LoginAccountAlreadyConnected
	LoginAccountBlocked
	LoginTemporaryBlocked
	LoginConnectionError
	LoginWrongVersion
)

// LogoutType 是登出方式的语义码（动作层词汇；协议映射在视图层）。
// 数值与 s2c.LogOutType 一致（对照 OpenMU LogOutType 枚举）。
type LogoutType byte

const (
	LogoutCloseGame                LogoutType = 0 // 关闭游戏
	LogoutBackToCharacterSelection LogoutType = 1 // 返回角色选择
	LogoutBackToServerSelection    LogoutType = 2 // 返回服务器选择
)

// CharacterListEntry 是角色列表条目的领域视图（不含协议形态）。
// 两个外观字段分别是**同一角色的两种序列化产物**（紧凑 18B / 扩展 27B，
// 由 view/remote 的 EncodeAppearance / EncodeAppearanceExt 产出）——
// 不是前缀关系，形态选择由视图层按客户端版本做。
type CharacterListEntry struct {
	Slot          byte
	Name          string
	Level         uint16
	Status        uint8  // 原版 CharacterStatus 枚举值
	GuildPosition uint8  // 原版 GuildMemberRole 枚举值
	Appearance    []byte // 18B 预览外观
	AppearanceExt []byte // 27B 扩展外观
}

// CharacterInformation 是选角成功后的完整角色信息（原版 CharacterInformationExtended 内容）。
// Stats 允许为 nil（视图层按职业/等级兜底）。
type CharacterInformation struct {
	Character *entity.Character
	Stats     *entity.CharStats
}

// CreatedCharacterView 是创角成功帧的内容（对照原版 CharacterCreationSuccessfulRef）。
// 预览数据由视图层填 0xFF（原版 ShowCreatedCharacterPlugIn 行为）。
type CreatedCharacterView struct {
	Name   string
	Slot   byte
	Level  uint16
	Class  byte // CharacterClassNumber
	Status byte // CharacterStatus
}

// CharacterDeleteResponseResult 是删角结果语义码（数值与 s2c.CharacterDeleteResult 一致）。
type CharacterDeleteResponseResult byte

const (
	CharacterDeleteUnsuccessful CharacterDeleteResponseResult = 0
	CharacterDeleteSuccessful   CharacterDeleteResponseResult = 1
	CharacterDeleteWrongCode    CharacterDeleteResponseResult = 2
)

// SkillListView 是一条已学技能（对照 SkillListUpdate.SkillEntry）。
// SkillNumber 与 config.Skill.Number 一致（S6 客户端 SkillListUpdate 的 SkillNumber）。
type SkillListView struct {
	SkillNumber uint16
	Level       byte
}

// ScopeEntry 是"进入视野的角色"（原版 AddCharacterToScopeExtended 内容）。
type ScopeEntry struct {
	ID          uint16
	Spawned     bool // true：Id 置出生位 0x8000（自己出生）
	X, Y        byte
	Rotation    byte
	Name        string
	ClassNumber byte
	Appearance  []byte            // 27B；长度不符时视图层按职业编码缺省外观
	Stats       *entity.CharStats // nil → 视图层按职业基准敏捷推导攻速（Hero 常态）
}

// NpcScopeEntry 是"进入视野的怪物/NPC"（原版 AddNpcsToScopeExtended 的 NpcData）。
type NpcScopeEntry struct {
	ID         uint16
	TypeNumber uint16 // 怪物定义编号（客户端据此渲染外观）
	X, Y       byte
	Rotation   byte
}

// WalkedMove 是一次行走确认（原版 ObjectWalkedExtended 内容）。
type WalkedMove struct {
	ObjectID uint16
	SourceX  byte
	SourceY  byte
	TargetX  byte
	TargetY  byte
	Rotation byte
	Steps    []byte // 方向序列（每步一个 nibble 值 1..8）
}

// DropEntry 是"视野内的地面物品"（原版 ItemsDropped 的 DroppedItem 内容）。
type DropEntry struct {
	ID   uint16
	X, Y byte
	// Data 是该物品的线上编码（5~15B 动态，由 view/remote.EncodeItemExtended 产出；
	// 视图层原样搬运并按实际长度定 stride）。领域模型本身不持有字节布局。
	Data      []byte
	FreshDrop bool // 新鲜掉落位（bit7）
}

// MoneyEntry 是"视野内的地面金币"（视图层发 C1 2F MoneyDroppedExtended）。
type MoneyEntry struct {
	ID        uint16
	X, Y      byte
	Amount    uint32
	FreshDrop bool
}

// ShowObjectAnimation 的动画号（对照客户端 _enum.h：AT_ATTACK1 = 120）。
// 原版 ShowAnimationPlugIn.MonsterAttackAnimation 同为 120。
const AnimationAttack1 byte = 120

// AppearanceChange 是"某角色的某个装备槽外观变化"（对照原版
// AppearanceChangedExtendedPlugIn.AppearanceChangedAsync 的入参）。
//
// ItemGroup == 0xFF 表示该槽已被**卸下**：客户端 ReceiveChangePlayer 按 ItemSlot 分支
// 把对应模型（Weapon[0]/Weapon[1]/Helm/…）置 -1（清空），因此卸下时 Group 必须下 0xFF，
// 不能下物品真实 Group——否则客户端会"穿上一件卸掉的东西"。
//
// IsAncientSetComplete 对应 OpenMU `HasFullAncientSetEquipped()`：非零时客户端置
// `c->ExtendState`（远古全套的紫色光环）。
type AppearanceChange struct {
	// ChangedPlayerID 按观察者视角解析（自己恒为 0x200）；本包**不发给自己**。
	ChangedPlayerID uint16
	ItemSlot        byte
	ItemGroup       byte // 0xFF = 卸下
	ItemNumber      uint16
	ItemLevel       byte
	ExcellentFlags  byte
	// AncientDiscriminator 为该物品的远古套装判别号（无远古为 0）。
	AncientDiscriminator byte
	IsAncientSetComplete bool
}

// ItemPickFailReason 对照 GameLogic/Views/Inventory/ItemPickFailReason.cs（含 Undefined=0
// 的空位；线值转换在 view/remote，对应原版 EnumExtensions.Convert）。
type ItemPickFailReason int

const (
	ItemPickFailUndefined ItemPickFailReason = iota
	ItemPickFailGeneral
	ItemPickFailStacked
	ItemPickFailMaximumMoney
)

// PlayerView 是单个玩家连接的出站视图（消费者侧接口；实现在 view/remote）。
type PlayerView interface {
	// ShowLoginResult 下发 F1 01 登录结果。
	ShowLoginResult(result LoginResult) error
	// ShowLogoutResponse 下发 C3 F1 02 登出响应（回显客户端请求的登出方式；T3 登出）。
	ShowLogoutResponse(t LogoutType) error
	// ShowSkillList 下发 C1 F3 11 SkillListUpdate（进图/学习后刷新技能列表；T3 技能学习）。
	ShowSkillList(skills []SkillListView) error
	// ShowCharacterList 下发 F3 00 角色列表（扩展/紧凑形态由视图层按客户端版本选）。
	// classUnlockFlags 是账号已解锁职业的 CreationAllowedFlag 聚合（原版同一个值也另发 DE 00）。
	ShowCharacterList(entries []CharacterListEntry, classUnlockFlags byte) error
	// ShowCharacterClassCreationUnlock 下发 C1 DE 00（仅当聚合标记 >0 时，对照
	// ShowCharacterListPlugIn:46-49）。
	ShowCharacterClassCreationUnlock(flags byte) error
	// ShowCharacterInformation 下发 F3 03 角色信息（92B Extended）。
	ShowCharacterInformation(info CharacterInformation) error
	// ShowCharacterCreationSuccess 下发 F3 01 创角成功（CharacterCreationSuccessful，42B）。
	ShowCharacterCreationSuccess(v CreatedCharacterView) error
	// ShowCharacterCreationFailed 下发 F3 01 创角失败（CharacterCreationFailed，5B）。
	ShowCharacterCreationFailed() error
	// ShowCharacterDeleteResponse 下发 F3 02 删角结果（CharacterDeleteResponse，5B）。
	ShowCharacterDeleteResponse(result CharacterDeleteResponseResult) error
	// ShowCharacterFocused 下发 F3 15 聚焦结果（CharacterFocused，15B，仅回角色名）。
	ShowCharacterFocused(name string) error
	// ShowCharacterInScope 下发 C2 12 视野进入（单角色）。
	ShowCharacterInScope(entry ScopeEntry) error
	// ShowNpcsInScope 下发 C2 13 怪物/NPC 入视野（批量；T2-1）。
	ShowNpcsInScope(entries []NpcScopeEntry) error
	// ShowDropsInScope 下发 C2 20 地面掉落物（物品/金币；T2-1）。
	ShowDropsInScope(items []DropEntry, money []MoneyEntry) error
	// ShowObjectsOutOfScope 下发 C1 14 移出视野（批量对象 ID；T2-1）。
	ShowObjectsOutOfScope(ids []uint16) error
	// ShowObjectHit 下发 C1 11 **ObjectHitExtended**（16B，S6 客户端专用；基础 10B 形态
	// 会被扩展客户端按 16B 错位解析 → 伤害不显示 + SD/AG/MP 乱变，真机事故）。
	// 对照 ShowHitExtendedPlugIn：targetId/healthStatus/shieldStatus 按接收者视角
	// 解析（自己是目标 → 0x200）；healthStatus = 受击者血量条 0..250（0xFF=N/A）。
	ShowObjectHit(targetID uint16, healthStatus, shieldStatus byte, damage, shieldDamage uint32, kind DamageKind) error
	// ShowObjectGotKilled 下发 C1 17 ObjectGotKilled（victimID 同上按接收者视角解析）。
	ShowObjectGotKilled(victimID uint16) error
	// ShowObjectAnimation 下发 C1 18 ObjectAnimation（对象动作动画）。
	// 对照 OpenMU Monster.AttackAsync → ShowMonsterAttackAnimationAsync(animation=120)：
	// **怪物攻击必须显式发这一包**——S6 客户端的怪物攻击动画只由 0x18 触发，
	// 缺失时客户端不会播放近身挥击，玩家只看到"隔空掉血"，近战怪看起来像远程。
	// animatingID 按接收者视角解析（自己 = 0x200）；direction 为 OpenMU Direction
	// 枚举的 packet byte（= Direction - 1，与行走 nibble 同一套编码）；
	// targetID 是被攻击目标（无目标传 0）。
	ShowObjectAnimation(animatingID uint16, direction, animation byte, targetID uint16) error
	// ShowItemAddedToInventory 下发 C3 22（拾取成功：物品进背包，T2-4）。
	ShowItemAddedToInventory(slot byte, itemData []byte) error
	// ShowItemPickUpFailed 下发 C3 22 ItemPickUpRequestFailed（拾取失败原因；原版
	// PickupItemAction 的三条失败出口与 ItemStacked 分支都用它）。
	ShowItemPickUpFailed(reason ItemPickFailReason) error
	// ShowInventoryMoneyUpdate 下发 C3 22 FE（金币变化，T2-4）。
	ShowInventoryMoneyUpdate(money uint32) error
	// ShowItemDropRemoved 下发 C2 21 地面物移除（拾取/过期，T2-4）。
	ShowItemDropRemoved(ids []uint16) error
	// ShowItemDropResponse 下发 C1 23 丢弃结果（丢弃者自己收，T2-4）。
	ShowItemDropResponse(success bool, slot byte) error
	// ShowMapChanged 下发 C3 1C 0F 换图/重生确认（客户端回 F3 12 重入）。
	ShowMapChanged(mapNumber uint16, x, y, rotation byte) error
	// ShowMapChangeFailed 下发同一包的 Flag=0 形态（原版 IMapChangePlugIn.MapChangeFailedAsync）：
	// 客户端只做落点与传送结束动画，不切图。
	ShowMapChangeFailed(mapNumber uint16, x, y, rotation byte) error
	// ShowObjectMovedInstant 下发 C1 15 瞬移/位置回拉（行走拒绝时的同步；T2-4 联调）。
	// objectID 按接收者视角解析（自己 = 0x200）。
	ShowObjectMovedInstant(objectID uint16, x, y byte) error
	// ShowObjectWalked 下发 C1 D4 行走确认（含步点编码）。
	ShowObjectWalked(move WalkedMove) error
	// ShowItemMoved 下发 C3 24 背包内搬运成功（T2-5）。
	// 对照 ItemMovedPlugIn：TargetStorageType + TargetSlot + ItemData（扩展 5~15B 动态）。
	// 客户端 ReceiveEquipmentItemExtended 用 CalcItemLength 定长，再按 TargetSlot 落位
	// （< MAX_EQUIPMENT_INDEX 走 EquipItem，主网格/扩展页走各自 InsertItem）。
	ShowItemMoved(targetStorage, targetSlot byte, itemData []byte) error
	// ShowItemMoveFailed 下发 C3 24 FF 搬运失败（T2-5）：客户端把手上物品恢复原位。
	// itemData 为被拒物品的扩展编码；传 nil 时视图层补 15B 零
	// （对照原版 ItemMoveFailedPlugIn 在 item==null 时按 ItemSerializerExtended.NeededSpace=15
	// 写零——客户端仍会对该段调 CalcItemLength，长度必须够）。
	ShowItemMoveFailed(itemData []byte) error
	// ShowItemRemoved 下发 C1 28 物品从背包消失（T2-5：完整堆叠后源件销毁）。
	ShowItemRemoved(inventorySlot byte) error
	// ShowItemDurabilityChanged 下发 C1 2A 耐久/堆叠数量变化（T2-5：堆叠后两端各自更新）。
	ShowItemDurabilityChanged(inventorySlot, durability byte, byConsumption bool) error
	// ShowAppearanceChanged 下发 C1 25 装备外观变化（T2-5）。
	// 只发给**其他观察者**（对照 InventoryStorage.UpdateItemsOnChangeAsync 的
	// ForEachWorldObserverAsync(..., sendToSelf: false)）。
	ShowAppearanceChanged(change AppearanceChange) error

	// ShowCurrentStatsExtended 下发 C1 26 FF **CurrentStatsExtended**（24B，T2-6）。
	// 对照 UpdateStatsExtendedPlugIn.OnCurrentStatsChangedAsync：只发给**自己**
	// （原版由属性变更事件驱动；本仓在药水恢复/周期恢复/击杀恢复/升级后显式调用）。
	// 客户端 ReceiveStatsExtended 的 Index=0xFF 分支写 Life/Shield/Mana/BP/攻速/魔速。
	// ⚠️ 这是当前值四项的**唯一**出口：S6 客户端 case 0x26 只有 ReceiveStatsExtended
	// （24B 结构体），发 9B 的 CurrentHealthAndShield 会被当 24B 读成越界垃圾。
	ShowCurrentStatsExtended(stats CurrentStats) error
	// ShowMaximumStatsExtended 下发 C1 26 FE **MaximumStatsExtended**（20B，T2-6）。
	// 只发给自己；升级导致上限变化时下发（对照 OnMaximumStatsChangedAsync）。
	ShowMaximumStatsExtended(stats MaximumStats) error
	// ShowItemConsumptionFailed 下发 C1 26 FD **ItemConsumptionFailedExtended**（12B，T2-6）。
	// 对照 RequestedItemConsumptionFailedExtendedPlugIn：带上**当前**血/盾，客户端据此
	// 纠正本地预测（客户端把 SubCode 0xFD 视作 EnableUse=0）。
	ShowItemConsumptionFailed(health, shield uint32) error
	// ShowMagicEffectStatus 下发 C1 07 MagicEffectStatus（T2-6 MagicEffect 基础）。
	// 对照 DeActivateMagicEffectPlugIn：objectID 按接收者视角解析（自己 = 0x200）；
	// active=false 表示去激活。仅当效果编号可见（>0 且 <200）时才应调用。
	ShowMagicEffectStatus(active bool, objectID uint16, effectNumber byte) error
	// ShowFruitConsumptionResponse 下发 C1 2C FruitConsumptionResponse（TRIM-01 果实）。
	// 对照 FruitConsumptionResponsePlugIn：result 为 FruitOutcome* 线值（0/1/2/3/4/5/16/33/37/38），
	// statPoints 为本次实际增减点数（失败/拒绝为 0），statType 为果实影响属性（线值 0..4）。
	ShowFruitConsumptionResponse(result int, statPoints uint16, statType FruitStat) error
	// ShowItemUpgraded 下发 C1 F3 14 InventoryItemUpgraded（TRIM-01 宝石强化）。
	// 对照 ItemUpgradedPlugIn.ItemUpgradedAsync：槽位 + 物品的**完整扩展序列化**
	// （客户端按该包整体刷新背包里的那件物品——等级/选项变化只有它能同步）。
	ShowItemUpgraded(inventorySlot byte, itemData []byte) error
	// ShowExperienceGained 下发 C3 16 **ExperienceGainedExtended**（16B，T2-7）。
	// 对照 AddExperienceExtendedPlugIn：S6 客户端按帧长分派（>=16B 走 Large），
	// 故必须发扩展形态——9B 紧凑形态会被当 16B 解析。
	ShowExperienceGained(g ExperienceGainPacket) error
	// ShowLevelUp 下发 C1 F3 05 **CharacterLevelUpdateExtended**（32B，T2-7）。
	// 对照 UpdateLevelExtendedPlugIn：只发给自己；含等级/升级点数/四项上限/果实点数。
	ShowLevelUp(info LevelUpInfo) error
	// ShowMasterStats 下发 F3 50 MasterStatsUpdate（TRIM-09）。对照 UpdateMasterStatsPlugIn /
	// 其 Extended 变体（[MinimumClient(106,3)]）：(106,3) 及以上为 40B（四项上限 u32），
	// 其余为 32B（u16）。原版在发完这一包后紧接着发大师技能列表（UpdateMasterSkillsAsync）。
	ShowMasterStats(info MasterStatsView) error
	// ShowMasterSkillList 下发 C2 F3 53 MasterSkillList（TRIM-09）：已学大师技的
	// 槽位/等级/展示值。原版只发**已学**条目——客户端自己知道整棵树。
	ShowMasterSkillList(entries []MasterSkillEntryView) error
	// ShowMasterCharacterLevel 下发 F3 51 MasterCharacterLevelUpdate（TRIM-09 大师升级）。
	// 同 50 有 Extended 变体（28B / 20B），下限也是 (106,3)。
	ShowMasterCharacterLevel(info MasterLevelView) error
	// ShowMasterSkillLevelUpdate 下发 C1 F3 52 MasterSkillLevelUpdate（TRIM-09 加点成功）。
	// 原版只在**成功**后调用（MasterSkillLevelChangedPlugIn.cs:39 恒传 success=true）。
	ShowMasterSkillLevelUpdate(info MasterSkillUpdateView) error
	// ShowEffect 下发 C1 48 ShowEffect（T2-7 升级光效；T2-6 基础）。
	// 对照 ShowEffectPlugIn：targetID 按接收者视角解析；升级光效发给**自己 + 观察者**
	// （原版 ForEachWorldObserverAsync(..., sendToSelf: true)）。
	ShowEffect(targetID uint16, kind EffectKind) error

	// ShowStatIncreaseResult 下发 C1 F3 06 **CharacterStatIncreaseResponseExtended**
	// （24B，T2-8；S6 客户端 [MinimumClient(106,3)] 恒发扩展形态，对照
	// StatIncreaseResultExtendedPlugIn）：属性类型 + 本次加点数 + 四项新上限。
	ShowStatIncreaseResult(stat StatType, added uint16, maximums MaximumStats) error

	// ShowNpcWindow 下发 C3 30 NpcWindowResponse（T2-9）。
	// configWindow 取 DataModel 的 NpcWindow 枚举序号（MonsterDefinition.NpcWindow），
	// 到线协议号的映射在视图层（对照 OpenNpcWindowPlugIn.Convert）；无对应值时返回错误、不发包。
	ShowNpcWindow(configWindow int) error
	// ShowNpcDialog 下发 C3 F9 01 OpenNpcDialog（NpcWindow.NpcDialog 专用窗口）。
	ShowNpcDialog(npcNumber uint16) error

	// ShowMerchantStoreItemList 下发 C2 31 StoreItemList（T2-9）。
	// storeKind 取 s2c.ItemWindow_*（Normal=0 等）；items 为 {槽位, 扩展物品编码 5~15B}。
	ShowMerchantStoreItemList(storeKind byte, items []MerchantItemView) error

	// ShowVaultMoneyUpdate 下发 C1 81 VaultMoneyUpdate（12B，仓库金钱同步）。
	// 对照 UpdateVaultMoneyPlugIn：success=false 时金额字段为 0（客户端回滚输入）。
	ShowVaultMoneyUpdate(success bool, vaultMoney, inventoryMoney uint32) error
	// ShowVaultClosed 下发 C1 82 VaultClosed（3B；客户端关闭仓库窗口）。
	ShowVaultClosed() error
	// ShowVaultProtectionState 下发 C1 83 VaultProtectionInformation（4B；锁状态）。
	// state 取 s2c.VaultProtectionState_*（Unprotected=0）。
	ShowVaultProtectionState(state byte) error

	// ShowNpcItemBought 下发 C1 32 ItemBought（T2-9 买入成功：槽位 + 扩展物品编码）。
	ShowNpcItemBought(slot byte, itemData []byte) error

	// ShowNpcItemBuyFailed 下发 C1 32 FF NpcItemBuyFailed（T2-9 买入失败）。
	ShowNpcItemBuyFailed() error

	// ShowNpcItemSellResult 下发 C3 33 NpcItemSellResult（T2-9 卖出结果 + 当前金币）。
	ShowNpcItemSellResult(success bool, money uint32) error

	// ShowInventoryList 下发 C1 F3 10 CharacterInventory（选角/进图背包清单，真机修复 11）。
	// items 为 {槽位, 扩展物品编码 5~15B}；对照 UpdateInventoryListPlugIn：
	// 按槽位排序、同槽去重、actualSize 收缩语义与 C2 31 相同。
	ShowInventoryList(items []MerchantItemView) error

	// ShowSkillAnimation 下发 C3 19 SkillAnimation（T2-11 定向技能动画）。
	// 对照 ShowSkillAnimationPlugIn：playerID 按接收者视角解析（自己 = 哨兵 0x200），
	// targetID 为真实目标 ID；effectApplied=false 时原版不发——本签名由调用方决定。
	ShowSkillAnimation(playerID, skillID, targetID uint16) error

	// ShowAreaSkillAnimation 下发 C3 1E AreaSkillAnimation（T2-11 区域技能动画）。
	// 对照 ShowAreaSkillAnimationPlugIn：playerID 按接收者视角解析。
	ShowAreaSkillAnimation(playerID, skillID uint16, x, y, rotation byte) error

	// ShowPetMode 下发 C1 A7 PetMode（宠物行为变化，对照 PetBehaviourChangedViewPlugIn）。
	// petType 取 s2c.ClientToServerPetType（渡鸦=0/马=1）；mode 取 PetCommandMode（0..3）；
	// targetID 为 Lock Target 目标对象 ID（无目标传 0xFFFF）。
	ShowPetMode(petType, mode byte, targetID uint16) error
	// ShowPetAttack 下发 C1 A8 PetAttack（宠物攻击动画，对照 PetAttackViewPlugIn）。
	// attackType 取 PetAttack.SkillType（单体=0/范围=1）；ownerID 按接收者视角解析。
	ShowPetAttack(petType, attackType byte, ownerID, targetID uint16) error
	// ShowPetInfoResponse 下发 C1 A9 PetInfoResponse（宠物信息窗口，对照 PetInfoViewPlugIn）。
	// storage 取 s2c.StorageType（Inventory=0/PetSlot 等）；health = 宠物当前耐久。
	ShowPetInfoResponse(petType, storage, slot, level byte, experience uint32, health byte) error

	// ShowChatMessage 下发 C1 00 ChatMessage（S1 聊天）：channel 字节在 d[2]，
	// 与封包 code 同位（Normal=0/Whisper=2，对照 ChatViewPlugIn.ChatMessageAsync）。
	// sender 是发言者角色名；公共聊天发给发言者 + 视野内玩家（sendToSelf:true），
	// 私聊只发给接收者。
	ShowChatMessage(sender, message string, msgType ChatMessageType) error

	// ShowMessage 下发 C1 0D ServerMessage（蓝字/金字系统提示，对照 IShowMessagePlugIn）。
	// message 由调用方按 player.LocalizedMessage 取好；空文本不发（原版提前返回）。
	ShowMessage(message string, msgType MessageType) error

	// ShowAvailableChatCommands 下发 C2 F5 01 AvailableChatCommand（TRIM-08，对照
	// ChatCommandListViewPlugIn.ShowChatCommandListAsync）：**每条命令一包**，
	// Index/Count 让客户端知道收齐没有。列表已由编排层按角色状态与插件激活过滤。
	ShowAvailableChatCommands(commands []ChatCommandView) error

	// ShowPartyRequest 下发 C1 40 PartyRequest（S2）：requesterID 是邀请者的世界对象 ID
	// （对照 ShowPartyRequestPlugIn 直接用 requester.Id，非 GetId 视角）。
	ShowPartyRequest(requesterID uint16) error
	// ShowPartyList 下发 C1 42 PartyList（S2）：selfIndex 告诉客户端哪个条目是自己
	// （PartyMember 块不携带 ID，客户端按槽位对齐已渲染的对象）。
	ShowPartyList(selfIndex byte, members []PartyMemberView) error
	// ShowPartyMemberRemoved 下发 C1 43 RemovePartyMember（S2）。
	ShowPartyMemberRemoved(index byte) error
	// ShowPartyHealth 下发 C1 44 PartyHealthUpdate（S2）：每位 1 字节高 nibble=索引、低 nibble=血量 0..10。
	ShowPartyHealth(members []PartyHealthView) error

	// —— S3 交易（对照 RemoteView/Trade/*）——
	// ShowTradeRequest 下发 C1 36 TradeRequest（发给被邀请者，携带邀请者名）。
	ShowTradeRequest(requesterName string) error
	// ShowTradeRequestAnswer 下发 C1 37 TradeRequestAnswer（双方都收；partner 为对方）。
	ShowTradeRequestAnswer(accepted bool, partnerName string, partnerLevel uint16) error
	// ShowTradeItemAdded 下发 C1 39 TradeItemAdded（只发对方：自己那侧客户端本地已渲染）。
	ShowTradeItemAdded(toSlot byte, itemData []byte) error
	// ShowTradeItemRemoved 下发 C1 38 TradeItemRemoved（只发对方，slot 是移出方的交易槽）。
	ShowTradeItemRemoved(slot byte) error
	// ShowTradeMoneyUpdate 下发 C1 3B TradeMoneyUpdate（只发对方：对方摆出的金额）。
	ShowTradeMoneyUpdate(amount uint32) error
	// ShowTradeMoneySetResponse 下发 C1 3A 01 TradeMoneySetResponse（自己设金额的确认）。
	ShowTradeMoneySetResponse() error
	// ShowTradeButtonState 下发 C1 3C TradeButtonStateChanged（红=重置对方按钮）。
	ShowTradeButtonState(state TradeButtonState) error
	// ShowTradeFinished 下发 C1 3D TradeFinished。
	ShowTradeFinished(result TradeResult) error

	// —— S4 个人商店 PlayerStore（均走 C1/C2/C3 的 0x3F 组，按 sub 区分）——
	// ShowPlayerShopOpenResult 下发 C1 3F 02 PlayerShopOpenSuccessful。
	ShowPlayerShopOpenResult(success bool) error
	// ShowItemPriceSetResult 下发 C3 3F 01 PlayerShopSetItemPriceResponse。
	ShowItemPriceSetResult(inventorySlot byte, result ItemPriceResult) error
	// ShowPlayerShopClosed 下发 C1 3F 03 PlayerShopClosed（店主自己收）。
	ShowPlayerShopClosed(success bool, playerID uint16) error
	// ShowPlayerShopClosedNotice 下发 C1 3F 03 PlayerShopClosed（告知观察者店主已闭店）。
	ShowPlayerShopClosedNotice(playerID uint16) error
	// ShowPlayerShopCloseDialog 下发 C1 3F 12 ClosePlayerShopDialog（买家侧关窗）。
	ShowPlayerShopCloseDialog(playerID uint16) error
	// ShowPlayerShopItemList 下发 C2 3F 05 PlayerShopItemListExtended（回应买家拉清单）。
	ShowPlayerShopItemList(sellerID uint16, sellerName, shopName string, items []PlayerShopItemEntry) error
	// ShowPlayerShops 下发 C2 3F 00 PlayerShops（视野内"正在开店"的店名清单；对照
	// ShowShopsOfPlayersPlugIn——不发这条，客户端走近也看不见别人的摊位）。
	ShowPlayerShops(shops []PlayerShopEntry) error
	// ShowPlayerShopBuyResult 下发 C1 3F 06 PlayerShopBuyResultExtended（买家拉取购买结果）。
	ShowPlayerShopBuyResult(sellerID uint16, result ShopBuyResult, itemSlot byte, itemData []byte) error
	// ShowPlayerShopItemSold 下发 C1 3F 08 PlayerShopItemSoldToPlayer（告知店主某格已售出）。
	ShowPlayerShopItemSold(itemSlot byte, buyerName string) error

	// ShowItemCraftingResult 下发 C1 86 ItemCraftingResult（S8）：code 为 action.Craft* 语义码，
	// itemData 非空时附产物编码（单一产物）。
	ShowItemCraftingResult(code int, itemData []byte) error

	// —— P2 好友系统（Messenger）——
	// SendMessengerInitialization 下发 C2 C0 MessengerInitialization（进图时的信使初始化：好友名单+计数）。
	SendMessengerInitialization(friends []FriendEntry, letterCount, maxLetterCount byte) error
	// SendFriendAdded 下发 C1 C1 01 FriendAdded（新好友入列，携其服务器号）。
	SendFriendAdded(friendName string, serverID byte) error
	// SendFriendDeleted 下发 C1 C3 01 FriendDeleted。
	SendFriendDeleted(friendName string) error
	// SendFriendRequest 下发 C1 C2 FriendRequest（有人请求加你为好友）。
	SendFriendRequest(requesterName string) error
	// SendFriendOnlineStateUpdate 下发 C1 C4 FriendOnlineStateUpdate（好友在线服务器号变化）。
	SendFriendOnlineStateUpdate(friendName string, serverID byte) error
	// SendChatRoomConnectionInfo 下发 C3 CA ChatRoomConnectionInfo（进聊天室的地址/房号/口令）。
	SendChatRoomConnectionInfo(chatServerIP string, roomID uint16, authPassword uint32, clientIndex byte, friendName string, success bool) error
	// SendFriendInvitationResult 下发 C3 CB FriendInvitationResult（邀请对方进房的结果）。
	SendFriendInvitationResult(success bool, requestID uint32) error
	// SendAddLetter 下发 C3 C6 AddLetter（新信/信头列表：下标+发件人+时间+主题+状态）。
	SendAddLetter(index uint16, senderName, timestamp, subject string, state byte) error
	// SendOpenLetter 下发 C4 C7 OpenLetterExtended（读信：发件人外观+正文）。
	SendOpenLetter(index uint16, senderAppearance []byte, rotation, animation byte, message string) error
	// SendLetterSendResult 下发 C1 C5 LetterSendResponse（发信结果码 + 客户端 letterId）。
	SendLetterSendResult(result byte, letterID uint32) error
	// SendRemoveLetter 下发 C1 C8 RemoveLetter（删信结果）。
	SendRemoveLetter(success bool, index uint16) error
	// —— S6 战盟（Guild）——
	// SendGuildList 下发 C2 52 GuildList（成员列表 + 总分）。
	SendGuildList(members []GuildMemberView, totalScore uint32) error
	// SendGuildCreationResult 下发 C1 56 GuildCreationResult。
	SendGuildCreationResult(success bool, errorType byte) error
	// SendGuildKickResult 下发 C1 53 GuildKickResponse。
	SendGuildKickResult(result byte) error
	// SendGuildJoinRequest 下发 C1 50 GuildJoinRequest（向团长：申请者世界对象ID）。
	SendGuildJoinRequest(requesterObjectID uint16) error
	// SendGuildJoinResult 下发 C1 51 GuildJoinResponse（向申请者：接受/拒绝等结果码）。
	SendGuildJoinResult(result byte) error
	// SendShowGuildCreationDialog 下发 C1 55 ShowGuildCreationDialog（团长确认创建时弹窗）。
	SendShowGuildCreationDialog() error
	// SendGuildRelationshipRequest 下发 C2 E5（把某团长收到的关系变更请求转给他看）。
	SendGuildRelationshipRequest(relationshipType, requestType byte, requesterID uint16) error
	// SendGuildRelationshipResult 下发 C1 E6（关系变更结果；guildMasterID 是对方团长 ID）。
	SendGuildRelationshipResult(relationshipType, requestType, result byte, guildMasterID uint16) error
	// SendAllianceList 下发 C2 E9（同盟内战盟清单）。
	SendAllianceList(entries []AllianceListEntry) error
	// SendRemoveAllianceGuildResult 下发 C1 EB 01（把某战盟移出同盟的结果）。
	SendRemoveAllianceGuildResult(success bool) error

	// SendGuildInformation 下发 C1 66 GuildInformation（战盟详情）。
	SendGuildInformation(guildID uint32, guildType byte, allianceName, guildName string, logo []byte) error
	// SendAssignCharacterToGuild 下发 C2 65 AssignCharacterToGuild（成员角色变更广播）。
	SendAssignCharacterToGuild(members []GuildAssignEntry) error
	// —— S10 任务（Quest，0xF6 现代任务流 + 0xA3 奖励公告）——
	// ShowQuestStepInfo 下发 C1 F6 0B QuestStepInfo（选中起始步骤 / 拒绝时按 RefuseNumber 发）。
	ShowQuestStepInfo(group, stepNumber uint16) error
	// ShowQuestCompletionResponse 下发 C1 F6 0D QuestCompletionResponse。
	ShowQuestCompletionResponse(group, number uint16, completed bool) error
	// ShowQuestCancelled 下发 C1 F6 0F QuestCancelled。
	ShowQuestCancelled(group, number uint16) error
	// ShowQuestState 下发 F6 1B QuestState（组 0 以外的进行中任务详情）。
	// 客户端不低于 (106,3) 时该视图实现改发 C2 Extended 变体（对照 MinimumClient 属性）。
	ShowQuestState(group, number uint16, conds []QuestConditionView, rewards []QuestRewardView) error
	// ShowQuestProgress 下发 F6 0C QuestProgress（与 QuestState 同构；开始确认与"已在进行中"时发）。
	ShowQuestProgress(group, number uint16, conds []QuestConditionView, rewards []QuestRewardView) error
	// ShowActiveQuests 下发 C1 F6 1A QuestStateList（进行中清单，原版最多 62 条、空也发）。
	ShowActiveQuests(quests []QuestIDView) error
	// ShowAvailableQuests 下发 C1 F6 0A AvailableQuests（0x30 请求时对当前对话 NPC 应答）。
	ShowAvailableQuests(npcNumber uint16, quests []QuestIDView) error
	// ShowActiveEventQuests 下发 C1 F6 03 QuestEventResponse（0x21 事件任务清单，原版是两条常量）。
	ShowActiveEventQuests() error
	// ShowQuestRewardAnnouncement 下发 C1 A3 LegacyQuestReward：加点/转职/连击技等
	// 不进 0x1B 奖励表的奖励由它可见（对照 LegacyQuestRewardPlugIn）。receiverID 是自己时
	// 传 constantPlayerID 语义的 0x200，观察者广播时传对象号。
	ShowQuestRewardAnnouncement(receiverID uint16, reward QuestLegacyReward, count byte) error
	// —— 组 0 legacy 任务对话族（TRIM-07，对照 RemoteView/Quest/LegacyQuest*）——
	// ShowLegacyQuestStateList 下发 C1 A0（7 槽任务状态表；进图与 0xA0 请求时）。
	// states 下标 = legacy 任务号 0..6，取值见 quests.LegacyState（与协议同值）。
	ShowLegacyQuestStateList(states [7]byte) error
	// ShowLegacyQuestStateDialog 下发 C1 A1（任务号 + 4 任务状态字节）。
	ShowLegacyQuestStateDialog(questIndex byte, state byte) error
	// ShowLegacySetQuestStateResponse 下发 C1 A2（接/交 legacy 任务的应答）。
	ShowLegacySetQuestStateResponse(questIndex, result, state byte) error
	// ShowLegacyQuestMonsterKillInfo 下发 C1 A4 00（对话时附带击杀进度）。
	ShowLegacyQuestMonsterKillInfo(questIndex byte, kills []LegacyKillView) error
	// ShowObjectMessage 下发 C1 01（NPC 头顶气泡；legacy 任务与会话类提示共用）。
	ShowObjectMessage(objectID uint16, message string) error
	// ShowGuildMasterDialog 下发 C1 54（会长 NPC 允许创建公会时的对话框）。
	ShowGuildMasterDialog() error

	// ShowMuHelperConfiguration 下发 C2 AE MuHelperConfigurationData（保存后原样回显程序 blob）。
	ShowMuHelperConfiguration(data []byte) error
	// ShowMuHelperStatus 下发 C1 BF 51 MuHelperStatusUpdate（consumeMoney/money/pauseStatus）。
	ShowMuHelperStatus(consumeMoney bool, money uint32, paused bool) error
}

// StoreKind 是商店清单类型（动作层词汇，数值与 s2c.ItemWindow 一致）。
const (
	StoreKindNormal    byte = 0 // 原版 StoreKind.Normal
	StoreKindChaos     byte = 3 // 原版 StoreKind.ChaosMachine
	StoreKindResurrect byte = 5 // 原版 StoreKind.ResurrectionFailed
)

// MerchantItemView 是商店清单条目（槽位 + 物品扩展编码；字节布局由视图层消费）。
type MerchantItemView struct {
	Slot byte
	Data []byte
}

// CurrentStats 是 C1 26 FF 的**当前**属性集合（对照原版 Stats.Current* + 攻速/魔速）。
// 字段顺序与 SendCurrentStatsExtendedAsync 入参一致（Health, Shield, Mana, Ability,
// AttackSpeed, MagicSpeed）——客户端按同一顺序写入 CharacterAttribute。
type CurrentStats struct {
	Health      uint32
	Shield      uint32
	Mana        uint32
	Ability     uint32 // 客户端 SkillMana（BP）
	AttackSpeed uint16
	MagicSpeed  uint16
}

// MaximumStats 是 C1 26 FE 的**上限**属性集合（对照原版 Stats.Maximum*）。
type MaximumStats struct {
	Health  uint32
	Shield  uint32
	Mana    uint32
	Ability uint32
}

// ExperienceResult 对应 C3 16 的 AddResult 枚举（数值与 s2c.AddResult 一致）。
type ExperienceResult byte

const (
	ExperienceUndefined        ExperienceResult = 0
	ExperienceNormal           ExperienceResult = 1
	ExperienceMaster           ExperienceResult = 2
	ExperienceMaxLevelReached  ExperienceResult = 16
	ExperienceMaxMasterReached ExperienceResult = 32
	ExperienceMonsterTooLow    ExperienceResult = 33
)

// ExperienceGainPacket 是一次经验下发的内容（对照原版 AddExperienceExtendedPlugIn 入参）。
//
// KilledObjectID / KillerObjectID 按**接收者视角**解析：KillerObjectID 是自己时取
// 哨兵 0x200（原版 ViewExtensions.ConstantPlayerId）。DamageOfLastHit 只对队友非零，
// 单人场景恒 0（原版 `player.Id != obj.LastDeath.KillerId` 才带伤害）。
type ExperienceGainPacket struct {
	Result         ExperienceResult
	Experience     uint32
	Damage         uint32
	KilledObjectID uint16
	KillerObjectID uint16
}

// LevelUpInfo 是 C1 F3 05 的内容（对照 UpdateLevelExtendedPlugIn 的入参顺序）。
type LevelUpInfo struct {
	Level                      uint16
	LevelUpPoints              uint16
	MaximumHealth              uint32
	MaximumMana                uint32
	MaximumShield              uint32
	MaximumAbility             uint32
	FruitPoints                uint16
	MaximumFruitPoints         uint16
	NegativeFruitPoints        uint16
	MaximumNegativeFruitPoints uint16
}

// MasterStatsView 是 F3 50 MasterStatsUpdate 的内容（TRIM-09）。
// 对照 UpdateMasterStatsExtendedPlugIn：等级/经验/下一级经验/剩余点数 + 四项上限。
type MasterStatsView struct {
	MasterLevel           uint16
	MasterExperience      uint64
	ExperienceOfNextLevel uint64
	MasterLevelUpPoints   uint16
	MaximumHealth         uint32
	MaximumMana           uint32
	MaximumShield         uint32
	MaximumAbility        uint32
}

// MasterSkillEntryView 是 C2 F3 53 的一条已学大师技能。
// Index 是**客户端树槽位号**（由编排层从配置表取，0 表示该职业表里没有这一项）。
type MasterSkillEntryView struct {
	Index                 byte
	Level                 byte
	DisplayValue          float32
	DisplayValueOfNextLev float32
}

// MasterLevelView 是 F3 51 MasterCharacterLevelUpdate 的内容（大师升级）。
// GainedPoints 是"每级给几点"（attr[Master points per master Level up]），
// MaxPoints 是配置的最大大师等级（原版把 MaximumMasterLevel 填进这个槽位）。
type MasterLevelView struct {
	MasterLevel    uint16
	GainedPoints   uint16
	CurrentPoints  uint16
	MaximumPoints  uint16
	MaximumHealth  uint32
	MaximumMana    uint32
	MaximumShield  uint32
	MaximumAbility uint32
}

// MasterSkillUpdateView 是 C1 F3 52 MasterSkillLevelUpdate 的内容（一次加点成功）。
type MasterSkillUpdateView struct {
	Points              uint16
	SkillIndex          byte
	SkillNumber         uint16
	Level               byte
	DisplayValue        float32
	DisplayValueOfNextL float32
}

// EffectKind 是 C1 48 ShowEffect 的效果类型（动作层词汇；协议映射在视图层）。
// 对照原版 IShowEffectPlugIn.EffectType 的已实现分支。
type EffectKind byte

const (
	EffectShieldPotion EffectKind = iota + 1 // 原版 ShieldPotion
	EffectLevelUp                            // 原版 LevelUp
	EffectShieldLost                         // 原版 ShieldLost
)

// ChatMessageType 是聊天频道语义（动作层词汇；数值与 s2c.ChatMessageType 一致）。
type ChatMessageType byte

const (
	ChatNormal  ChatMessageType = 0 // 公共/视野内
	ChatWhisper ChatMessageType = 2 // 私聊
	ChatParty   ChatMessageType = 0 // 组队（沿用 Normal channel，客户端按前缀着色）
)

// MessageType 是系统提示通道（动作层词汇；数值与 s2c.MessageType 一致，
// 对照 IGameServer.MessageType）。
type MessageType byte

// ChatCommandParameterView 是命令的一个参数（C2 F5 01 的参数块）。
// TypeName 传原版的 CLR 类型名，到 wire 的 ChatCommandParameterType 映射在视图层
// （对照 ChatCommandListViewPlugIn.GetParameterType:88-99）。
type ChatCommandParameterView struct {
	Name        string
	ShortName   string
	IsRequired  bool
	TypeName    string
	ValidValues string
}

// ChatCommandView 是一条可用命令（C2 F5 01 的一条）。
type ChatCommandView struct {
	Command                string
	Name                   string
	Description            string
	MinimumCharacterStatus byte
	Parameters             []ChatCommandParameterView
}

const (
	MessageGoldenCenter MessageType = 0 // 屏幕中央金字
	MessageBlueNormal   MessageType = 1 // 蓝字提示
	MessageGuildNotice  MessageType = 2 // 居中绿字（公会通知）
)

// PartyMemberView 是组队列表条目（对照 UpdatePartyListPlugIn 写入的 PartyMember 块）。
type PartyMemberView struct {
	Index         byte
	Name          string
	MapID         byte
	X, Y          byte
	CurrentHealth uint32
	MaximumHealth uint32
}

// PartyHealthView 是组队血条更新条目（1 字节打包：索引高 nibble + 值低 nibble）。
type PartyHealthView struct {
	Index byte
	Value byte
}

// TradeButtonState 与 s2c.TradeButtonState 数值一致。
type TradeButtonState byte

const (
	TradeButtonUnchecked TradeButtonState = 0
	TradeButtonChecked   TradeButtonState = 1
	TradeButtonRed       TradeButtonState = 2 // 仅下发：让客户端重置确认按钮
)

// TradeResult 与 s2c.TradeResult 数值一致。
type TradeResult byte

const (
	TradeCancelled           TradeResult = 0
	TradeSuccess             TradeResult = 1
	TradeFailedFullInventory TradeResult = 2
)

// ItemPriceResult 与 s2c.ItemPriceSetResult 数值一致。
type ItemPriceResult byte

const (
	ItemPriceFailed       ItemPriceResult = 0
	ItemPriceSuccess      ItemPriceResult = 1
	ItemPriceSlotOutRange ItemPriceResult = 2
	ItemPriceNotFound     ItemPriceResult = 3
	ItemPriceNegative     ItemPriceResult = 4
	ItemPriceBlocked      ItemPriceResult = 5
	ItemPriceLevelTooLow  ItemPriceResult = 6
)

// ShopBuyResult 与 s2c.ResultKind2 数值一致。
type ShopBuyResult byte

const (
	ShopBuyUndefined          ShopBuyResult = 0
	ShopBuySuccess            ShopBuyResult = 1
	ShopBuyNotAvailable       ShopBuyResult = 2
	ShopBuyNotOpened          ShopBuyResult = 3
	ShopBuyInvalidSlot        ShopBuyResult = 5
	ShopBuyNameOrPriceMissing ShopBuyResult = 6
	ShopBuyLackOfMoney        ShopBuyResult = 7
	ShopBuyNoSpaceOrOverflow  ShopBuyResult = 8
)

// PlayerShopItemEntry 是商店清单条目（背包格 + 价格 + 扩展物品编码）。
type PlayerShopItemEntry struct {
	Slot  byte
	Price uint32
	Data  []byte
}

// PlayerShopEntry 是 C2 3F 00 里的一个"附近开着店"的摊位（对照 ShowShopsOfPlayersPlugIn
// 的每个 shopBlock：店主对象 ID + 店名 36B）。
type PlayerShopEntry struct {
	ObjectID  uint16
	StoreName string
}

// FriendEntry 是信使初始化里的一个好友条目（名字 + 在线服务器号，0xFF=离线/0xFE=隐身）。
type FriendEntry struct {
	Name     string
	ServerID byte
}

// GuildMemberView 是 GuildList 的一名成员（名字 + 在线服务器号 + 职位）。
type GuildMemberView struct {
	Name     string
	ServerID byte
	Role     byte // 对齐 s2c.GuildMemberRole（普通/团长等）
}

// GuildAssignEntry 是 AssignCharacterToGuild 的一条成员→战盟归属（在线成员：携世界对象ID）。
type GuildAssignEntry struct {
	GuildID  uint32
	Role     byte // 对齐 s2c.GuildMemberRole
	PlayerID uint16
}

// QuestConditionView 是一条任务条件（对齐 s2c.QuestCondition：类型+需求ID+当前/需求数+物品预览）。
type QuestConditionView struct {
	Type          byte // 对齐 s2c.ConditionType（MonsterKills=1、Item=4）
	RequirementID uint16
	Required      uint32
	Current       uint32
	ItemData      []byte // 物品条件：12B 预览编码
}

// QuestRewardView 是一条任务奖励（对齐 s2c.QuestReward）。
type QuestRewardView struct {
	Type     byte // 对齐 s2c.RewardType（Experience=1、Money=2、Item=4）
	RewardID uint16
	Count    uint32
	ItemData []byte // 物品奖励：12B 预览编码
}

// QuestIDView 是一个任务标识（组+号），用于任务清单包。
type QuestIDView struct {
	Group  uint16
	Number uint16
}

// LegacyKillView 是一条 legacy 任务的击杀进度（C1 A4 的 8 字节条目）。
type LegacyKillView struct {
	MonsterNumber int
	Count         int
}

// QuestLegacyReward 是 C1 A3 LegacyQuestReward 的奖励语义（数值 200~204 属协议层，
// 映射写在 view/remote；对照 LegacyQuestReward.QuestRewardType 与 LegacyQuestRewardPlugIn 的分支）。
type QuestLegacyReward byte

const (
	// QuestLegacyRewardLevelUpPoints 对应 reward type 200（count = 加的点数）。
	QuestLegacyRewardLevelUpPoints QuestLegacyReward = iota
	// QuestLegacyRewardEvolutionFirstToSecond 对应 201（count = 新职业号 <<3）。
	QuestLegacyRewardEvolutionFirstToSecond
	// QuestLegacyRewardPointsPerLevel 对应 202（count = (等级-任务最低等级) × 每级点数）。
	QuestLegacyRewardPointsPerLevel
	// QuestLegacyRewardComboSkill 对应 203（Is Skill Combo Available 属性奖励）。
	QuestLegacyRewardComboSkill
	// QuestLegacyRewardEvolutionSecondToThird 对应 204（count = 新职业号 <<3）。
	QuestLegacyRewardEvolutionSecondToThird
)

// DamageKind 与 s2c.DamageKind 数值一致（伤害颜色语义）。
type DamageKind byte

const (
	KindNormal    DamageKind = 0 // NormalRed
	KindCritical             = 3 // CriticalBlue
	KindExcellent            = 2
)

// PathWalkable 逐步校验路径上每一格（含目标格）是否可走（T1-1 碰撞）。
// walkable 为地形查询回调；任一格不可走即 false，动作层据此忽略行走包。
func PathWalkable(p WalkPlan, walkable func(x, y byte) bool) bool {
	if walkable == nil {
		return true
	}
	if !walkable(p.SourceX, p.SourceY) || !walkable(p.TargetX, p.TargetY) {
		return false
	}
	x, y := int(p.SourceX), int(p.SourceY)
	for _, d := range p.Directions {
		if d == 0 || int(d) >= len(DirectionDeltas) {
			continue
		}
		x += DirectionDeltas[d][0]
		y += DirectionDeltas[d][1]
		if x < 0 || x > 255 || y < 0 || y > 255 {
			continue
		}
		if !walkable(byte(x), byte(y)) {
			return false
		}
	}
	return true
}

// MaxWalkSteps 是单次行走包的最大步数（原版 WalkRequest 守卫）。
const MaxWalkSteps = 15

// DirectionDeltas 是线上方向增量（Direction 枚举序，对照 DirectionExtensions.CalculateTargetPoint）：
// 1 West(-1,-1) 2 SouthWest(0,-1) 3 South(+1,-1) 4 SouthEast(+1,0)
// 5 East(+1,+1) 6 NorthEast(0,+1) 7 North(-1,+1) 8 NorthWest(-1,0)
var DirectionDeltas = [9][2]int{
	// 索引 = Direction 枚举值（原版 CalculateTargetPoint，Direction.cs：1=W...8=NW）。
	// 注意：客户端行走 nibble 是 0..7，需 +1 转枚举（原版 ParseAsDirection；
	// 客户端 0=West——Direction.cs remarks）。**不可直接拿 nibble 当索引**。
	0: {0, 0},
	1: {-1, -1}, // West
	2: {0, -1},  // SouthWest
	3: {1, -1},  // South
	4: {1, 0},   // SouthEast
	5: {1, 1},   // East
	6: {0, 1},   // NorthEast
	7: {-1, 1},  // North
	8: {-1, 0},  // NorthWest
}

// WalkRequestHeaderSize 是 WalkRequest（C1 D4）**不含方向字节**时的帧长：
// C1 + Len + Code + SourceX + SourceY + (低 nibble StepCount | 高 nibble TargetRotation) = 6。
// 对照原版判据 `request.Header.Length > 6`（只有 > 6 才存在方向字节）。
const WalkRequestHeaderSize = 6

// IsNonMovingWalk 判断行走请求是否不带任何步点（只表达朝向，不改坐标）。
// 两个原版锚点：CharacterWalkBaseHandlerPlugIn.WalkAsync 的 `Header.Length > 6` 判据、
// PlayerMovement.WalkToAsync 的 `if (steps.IsEmpty) return;`。事故复盘见 doc/08 B11。
func IsNonMovingWalk(stepCount byte, frameLen int) bool {
	return stepCount == 0 || frameLen <= WalkRequestHeaderSize
}

// WalkPlan 是行走请求的规划结果（纯数据，目标坐标已按方向串累加并钳制）。
type WalkPlan struct {
	SourceX, SourceY byte
	TargetX, TargetY byte
	Rotation         byte
	Directions       []byte // 逐 nibble 解出的方向序列
}

// PlanWalk 解析行走请求（纯函数，原版 CharacterWalkHandlerPlugIn 的坐标部分）：
// 步数钳制到 MaxWalkSteps，方向串每步 4 bit，非法方向（0 或 >8）原地踏步。
// payload 不足以覆盖步数时 ok=false（客户端包损坏）。
func PlanWalk(sourceX, sourceY byte, directionsPayload []byte, stepCount, rotation byte) (plan WalkPlan, ok bool) {
	steps := int(stepCount)
	if steps > MaxWalkSteps {
		steps = MaxWalkSteps
	}
	if len(directionsPayload) < (steps+1)/2 {
		return plan, false
	}
	x, y := int(sourceX), int(sourceY)
	dirs := make([]byte, steps)
	for i := 0; i < steps; i++ {
		b := directionsPayload[i/2]
		var d byte
		if i%2 == 0 {
			d = b >> 4
		} else {
			d = b & 0x0F
		}
		dirs[i] = d
		// 原版 DecodePayload：nibble → ParseAsDirection（+1）→ Direction 枚举 1..8；
		// 客户端 nibble 0 = West（枚举 1）。**不可把 nibble 直接当枚举/增量索引**。
		enum := int(d) + 1
		if enum >= len(DirectionDeltas) {
			continue // >8 的非法 nibble：防御性忽略该步（原版会抛异常）
		}
		x += DirectionDeltas[enum][0]
		y += DirectionDeltas[enum][1]
	}
	if x < 0 {
		x = 0
	} else if x > 255 {
		x = 255
	}
	if y < 0 {
		y = 0
	} else if y > 255 {
		y = 255
	}
	return WalkPlan{
		SourceX: sourceX, SourceY: sourceY,
		TargetX: byte(x), TargetY: byte(y),
		Rotation: rotation, Directions: dirs,
	}, true
}

// StepTarget 返回前 n 步累加后的坐标（n ≥ 1；对应原版 steps[n-1].To）。
func (p WalkPlan) StepTarget(n int) (byte, byte) {
	x, y := int(p.SourceX), int(p.SourceY)
	for i := 0; i < n && i < len(p.Directions); i++ {
		enum := int(p.Directions[i]) + 1
		if enum >= len(DirectionDeltas) {
			continue
		}
		x += DirectionDeltas[enum][0]
		y += DirectionDeltas[enum][1]
	}
	return byte(x), byte(y)
}

// WalkableStepCount 对应原版 GetWalkableStepCount：逐步做地形检查，
// 在**第一个阻挡步**处截断（返回其前缀步数）；全部可走返回总步数。
// 原版语义是"截断"而非"整包拒绝"——玩家走到阻挡步之前为止。
func (p WalkPlan) WalkableStepCount(walkable func(x, y byte) bool) int {
	x, y := int(p.SourceX), int(p.SourceY)
	for i, d := range p.Directions {
		enum := int(d) + 1
		if enum >= len(DirectionDeltas) {
			return i
		}
		x += DirectionDeltas[enum][0]
		y += DirectionDeltas[enum][1]
		if !walkable(byte(x), byte(y)) {
			return i
		}
	}
	return len(p.Directions)
}
