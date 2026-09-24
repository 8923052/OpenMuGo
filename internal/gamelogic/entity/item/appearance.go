// Package item 的外观领域模型，对照 OpenMU GameLogic/PlayerAppearanceData 与
// DataModel 的 AppearanceData。只描述"穿什么"，不含 18B/27B 线上字节布局——
// 编码在 view/remote（对照 GameServer/RemoteView/AppearanceSerializer*.cs）。
package item

// 装备槽位常量（对照 OpenMU InventoryConstants）。
const (
	SlotLeftHand  = 0
	SlotRightHand = 1
	SlotHelm      = 2
	SlotArmor     = 3
	SlotPants     = 4
	SlotGloves    = 5
	SlotBoots     = 6
	SlotWings     = 7
	SlotPet       = 8
)

// 外观在封包中的固定字节数（供 view/remote 与上层预分配缓冲）。
const (
	// AppearanceSize 是角色列表预览外观的字节数。
	AppearanceSize = 18
	// AppearanceExtSize 是进图视野外观的字节数（106.3+ 扩展）。
	AppearanceExtSize = 27
)

// Equip 是一件参与外观显示的装备。
type Equip struct {
	Number    int
	Group     byte
	Level     byte
	Excellent bool
	Ancient   bool
	// 宠物（槽 8）专用
	BlackFenrir bool
	BlueFenrir  bool
	GoldFenrir  bool
}

// Appearance 是角色外观的输入模型。
type Appearance struct {
	ClassNumber            int // CharacterClass.Number
	Pose                   byte
	FullAncientSetEquipped bool
	// GameMaster 仅 27B 扩展外观使用（[1] |= 0x20）；18B 预览忽略。
	GameMaster bool
	// Equipment 按槽位索引，nil 表示该槽无装备。长度至少 9（使用 0..8）。
	Equipment []*Equip
}
