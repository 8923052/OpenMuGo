// Package entity 是业务实体层，对应 OpenMU 的 DataModel/Entities 与 GameLogic 中的领域对象。
// 只承载数据与纯值语义，不 import server / view / proto / persistence。
package entity

import "mugo/internal/gamelogic/storage"

// AccountState 对应 C# AccountState（登录结果中封禁判定所需）。
type AccountState byte

const (
	AccountStateNormal            AccountState = 0
	AccountStateBanned            AccountState = 1
	AccountStateGameMaster        AccountState = 2
	AccountStateTemporarilyBanned AccountState = 3
)

// LearnedSkill 是角色已学技能（对照 OpenMU Character.SkillList / SkillEntry）。
// SkillNumber 与 config.Skill.Number 一致，即 S6 客户端 SkillListUpdate 的 SkillNumber；
// Level 为技能等级（技能书携带的物品等级）。
type LearnedSkill struct {
	SkillNumber uint16
	Level       byte
}

// Character 是角色选择列表中的一个角色（M4 只填 CharacterList 需要的字段）。
type Character struct {
	Slot   byte
	Name   string
	Level  uint16
	Status CharacterStatus
	// GuildPosition 为公会职位；未加入公会时为 NormalMember。
	GuildPosition byte
	// Appearance 为 18 字节外观（由 item.EncodeAppearance 生成，M5 起使用）。
	// 为 nil 时角色列表写入全零外观。
	Appearance []byte

	// M6：进图所需的世界属性。
	// ClassNumber 为职业编号（CharacterClassNumber：DW=0 DK=4 Elf=8 DL=16，原始值不左移）。
	ClassNumber byte
	// MapNumber 为出生地图号（Lorencia=0，Noria=3）。
	MapNumber uint16
	// X/Y/Rotation 为出生坐标与朝向。
	X, Y, Rotation byte
	// AppearanceExt 为 27 字节进图外观（item.EncodeAppearanceExt）；为 nil 时不进图。
	AppearanceExt []byte
	// Stats 为进图属性（92B CharacterInformationExtended）；种子数据提供，nil 视为零值。
	Stats *CharStats
	// Inventory 为角色背包（T1-3 storage；拾取/丢弃时由 GS 惰性创建）。
	Inventory *storage.Inventory
	// LearnedSkills 为已学技能（对照 OpenMU Character.SkillList）；技能书右键学习写入，
	// 进图/学习后由 GS 经 SkillListUpdate（C1 F3 11）下发，施放判定（skillKnown）据此查询。
	LearnedSkills []LearnedSkill
	// MuHelperConfiguration 是 MU Helper 程序存档（不透明的客户端字节 blob，原样回显；
	// 对照 Character.MuHelperConfiguration）。
	MuHelperConfiguration []byte
	// KeyConfiguration 是客户端键位设置（不透明 blob；对照 Character.KeyConfiguration，
	// 由 C3 F3 30 写入，原版只在 075/097 形态的进图属性包后回发）。
	KeyConfiguration []byte
	// QuestStates 是按任务组持久化的任务进度（对照 OpenMU Character.QuestStates）。
	QuestStates []QuestState
	// AttributeBonuses 是角色级属性加成（对照 Character.Attributes 里的 StatAttribute：
	// 任务"属性奖励"往该属性的元素表里追加一个常量元素）。按属性定义 id 累加。
	AttributeBonuses []AttributeBonus
	// MasterExperience 是大师经验（原版 Character.MasterExperience，long）。
	MasterExperience int64
	// MasterLevelUpPoints 是**剩余**大师点数（原版同名 int）：大师升级时按
	// attr[Master points per master Level up] 累加，加点按花费扣减。
	MasterLevelUpPoints int
	// MasterLevel 是大师等级。原版存在 character.Attributes 的 StatAttribute
	// "Master Level"（可回收属性）里，本仓角色态属性一律放实体字段，
	// 由 player.charStatOverrides 灌回属性系统（见 doc/16 TRIM-09）。
	MasterLevel uint16
	// 已学大师技能不另立表：原版就是 LearnedSkills 条目的 Level（普通技与大师技共用）。
}

// AttributeBonus 是一条角色级属性加成（原版 Character.Attributes 的一项）。
type AttributeBonus struct {
	// AttributeID 是属性定义 GUID（与 80_attributes.json 的 id 逐字一致）。
	AttributeID string
	// Designation 供人读与日志；解析一律走 AttributeID。
	Designation string
	Value       float64
}

// QuestState 是某任务组的运行时状态（对照 DataModel/Entities/CharacterQuestState.cs：
// 一个组同时只有一个进行中任务 + 一个上次完成任务 + 逐击杀需求的计数）。
// 任务以**配置下标**引用（原版是 QuestDefinition 引用）：同一 (group, number) 会按职业
// 有多条定义，只有下标能无歧义地指回那一条（转职后按号/职业反查会落到另一条）。
type QuestState struct {
	Group int
	// ActiveQuestIndex / LastFinishedQuestIndex 是 config.GameConfig.Quests 的下标，-1=无。
	ActiveQuestIndex       int
	LastFinishedQuestIndex int
	// KillProgress 是进行中任务逐击杀需求的已击杀数：RequiredKills 下标 -> count
	// （对照 QuestMonsterKillRequirementState）。
	KillProgress     map[int]int
	ClientActionDone bool
}

// NewQuestState 建一个空的组状态（两个引用都是"无"）。
func NewQuestState(group int) QuestState {
	return QuestState{Group: group, ActiveQuestIndex: -1, LastFinishedQuestIndex: -1}
}

// CharStats 是进图时下发的角色属性集合（M6 简化版，无完整属性系统）。
// 字段顺序对照 S2C CharacterInformationExtended。
type CharStats struct {
	Experience          uint64
	ExperienceNext      uint64
	LevelUpPoints       uint16
	Strength            uint16
	Agility             uint16
	Vitality            uint16
	Energy              uint16
	Leadership          uint16
	CurrentHealth       uint32
	MaximumHealth       uint32
	CurrentMana         uint32
	MaximumMana         uint32
	CurrentShield       uint32
	MaximumShield       uint32
	CurrentAbility      uint32
	MaximumAbility      uint32
	Money               uint32
	HeroState           byte // s2c.CharacterHeroState（Normal=3）
	UsedFruitPoints     uint16
	MaxFruitPoints      uint16
	UsedNegFruit        uint16
	MaxNegFruit         uint16
	AttackSpeed         uint16
	MagicSpeed          uint16
	MaximumAttackSpeed  uint16
	InventoryExtensions byte
	Resets              uint16
}

// CharacterStatus 对应枚举 CharacterStatus（低 4 位）。
type CharacterStatus byte

const (
	CharacterStatusNormal     CharacterStatus = 0
	CharacterStatusBanned     CharacterStatus = 1
	CharacterStatusGameMaster CharacterStatus = 32
)

// VaultSlots 是仓库格子数（原版 InventoryConstants.WarehouseSize = 15 行 × 8 列）。
const VaultSlots = 120

// Vault 是账号仓库（对照原版 Account.Vault：ItemStorage 含 Items 与 Money）。
// 仓库物品与金钱随**账号**走，不随角色。
type Vault struct {
	Items *storage.Storage
	Money uint32
}

// Account 是登录账号及其角色集合。
type Account struct {
	Name       string
	Password   string
	State      AccountState
	Characters []Character
	// Vault 为账号仓库；与仓库 NPC 对话时惰性创建（对照原版 Account.Vault）。
	Vault *Vault
	// VaultPassword 是仓库 PIN（4 位数字的字符串形态；空=未保护。对照 Account.VaultPassword，
	// 非空即"该账号的仓库上着锁"，解锁状态是会话内的可变态）。
	VaultPassword string
	// UnlockedClasses 是账号已解锁的可创建职业号（对照 Account.UnlockedCharacterClasses，
	// 由角色升级按 action.ClassUnlockRules 追加；在角色列表里以 UnlockFlags/DE 00 下发）。
	UnlockedClasses []int
	IsTemplate      bool // 模板账号（OpenMU 测试/离线系统用），会话管理放宽
}
