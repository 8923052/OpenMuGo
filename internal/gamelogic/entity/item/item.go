// Package item 是物品的**领域模型**（对应 OpenMU GameLogic/ItemIdentifier、
// DataModel/Entities/Item 的属性面），只描述"这件物品是什么"，不含任何线上字节布局。
//
// 出站编码（12 字节）在 view/remote —— 对照 OpenMU GameServer/RemoteView/ItemSerializer.cs。
// 这样 gamelogic 不再持有出站序列化，也就不会因为序列化分版本而违反"gamelogic 版本无关"。
package item

// Size 是 S6E3 物品在封包中的固定字节数（供 view/remote 与上层预分配缓冲）。
const Size = 12

// Item 是物品的属性集合（与存储/配置模型解耦）。
type Item struct {
	Number     int
	Group      byte
	Level      byte
	Durability byte
	// PetExperience 是可训练宠物（黑暗之马 / 黑暗渡鸦）累计的经验；
	// 达到下一级阈值时 item.Level 递增（对照原版 Item.PetExperience）。
	// 非可训练宠物恒为 0。
	PetExperience        int32
	HasSkill             bool
	Luck                 bool
	OptionLevel          int  // 普通选项 level（0..7）
	WingOptionNumber     int  // 翅膀备选选项编号（>0 时写入）
	ExcellentBits        byte // 卓越/翅膀选项位（0..5）
	FenrirBits           byte // Black/Blue/Gold = 1/2/4
	AncientDiscriminator byte
	AncientBonusLevel    byte
	GuardianOption       bool
	// 非镶嵌物品
	HarmonyNumber byte
	HarmonyLevel  byte
	// 镶嵌物品
	SocketCount    int // 孔数 0..5
	HasSocketBonus bool
	SocketBonus    byte // 镶嵌奖励编号
	// SocketFilled[i] 为 true 时该孔已镶嵌（编码为 SocketSlots[i]，0 也是合法编码）；
	// 为 false 表示空孔（0xFE）。i >= SocketCount 为无孔（0xFF）。
	SocketFilled [5]bool
	SocketSlots  [5]byte
}
