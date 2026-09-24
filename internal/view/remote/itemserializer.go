// Package remote 的物品序列化，对照 OpenMU GameServer/RemoteView/ItemSerializer.cs。
// 12 字节布局：
//
//	[0] ItemNumber 低8位（512+ 标记在 [3] 0x80）
//	[1] Level<<3(0x78) | Luck(0x04) | Skill(0x80) | 普通选项低2位
//	[2] Durability
//	[3] 卓越/翅膀位(0x3F) | 512物品(0x80) | Fenrir(0x07) | 选项bit2(0x40) | 翅膀编号<<4
//	[4] 远古：bonusLevel<<2 | discriminator
//	[5] Group<<4 | 380守护(0x08)
//	[6] 无孔=Harmony(编号高4位+等级低4位)；有孔=SocketBonus
//	[7..11] 五个镶嵌槽（0xFF 无孔 / 0xFE 空孔 / 其他=球编码）
//
// 本文件是原 Item.Encode 方法的外移：领域模型留在 gamelogic/entity/item，
// 字节布局归 view/remote。原版另有 ItemSerializer075/095/Extended 变体，
// 后续按 _075/_095/_extended 后缀在本包拆分，并以 version.Constraint 标注适用区间。
package remote

import "mugo/internal/gamelogic/entity/item"

// Bit constants (对照 ItemSerializer.cs).
const (
	luckFlag           byte = 0x04
	skillFlag          byte = 0x80
	levelMask          byte = 0x78
	guardianOptionFlag byte = 0x08
	ancientBonusMask   byte = 0x0C
	ancientDiscrimMask byte = 0x03
	flag512            byte = 0x80
	socketNoSocket     byte = 0xFF
	socketEmptySocket  byte = 0xFE
	maxSockets         int  = 5
)

// EncodeItem 将物品写入 12 字节缓冲（调用方保证 len>=12），返回写入字节数。
func EncodeItem(it *item.Item, t []byte) int {
	for i := 0; i < item.Size; i++ {
		t[i] = 0
	}

	t[0] = byte(it.Number)
	t[1] = (it.Level << 3) & levelMask

	if it.OptionLevel != 0 || it.WingOptionNumber > 0 {
		t[1] |= byte(it.OptionLevel & 3)
		t[3] |= byte((it.OptionLevel & 4) << 4)
		if it.WingOptionNumber > 0 {
			t[3] |= byte((it.WingOptionNumber & 0b11) << 4)
		}
	}

	t[2] = it.Durability
	t[3] |= it.ExcellentBits & 0x3F

	if it.Number&0x100 == 0x100 {
		t[3] |= flag512
	}
	t[3] |= it.FenrirBits & 0x07

	if it.Luck {
		t[1] |= luckFlag
	}
	if it.HasSkill {
		t[1] |= skillFlag
	}

	if it.AncientDiscriminator != 0 || it.AncientBonusLevel != 0 {
		t[4] |= it.AncientDiscriminator & ancientDiscrimMask
		t[4] |= (it.AncientBonusLevel << 2) & ancientBonusMask
	}

	t[5] = it.Group << 4
	if it.GuardianOption {
		t[5] |= guardianOptionFlag
	}

	if it.SocketCount > 0 {
		if it.HasSocketBonus {
			t[6] = it.SocketBonus
		} else {
			t[6] = socketNoSocket
		}
	} else {
		t[6] = (it.HarmonyNumber << 4) | (it.HarmonyLevel & 0x0F)
	}

	for i := 0; i < maxSockets; i++ {
		t[7+i] = socketByte(it, i)
	}
	return item.Size
}

// socketByte 计算第 i 个镶嵌槽字节：已镶嵌=SocketSlots[i]；空孔=0xFE；无孔=0xFF。
func socketByte(it *item.Item, i int) byte {
	if i >= it.SocketCount {
		return socketNoSocket
	}
	if !it.SocketFilled[i] {
		return socketEmptySocket
	}
	return it.SocketSlots[i]
}
