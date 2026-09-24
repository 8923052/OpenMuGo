// DecodeItem 将 12 字节物品数据解码（EncodeItem 的逆变换，逐位对照）。
// 调用方保证 len(data) >= item.Size；返回写入字节数（item.Size）。
//
// 布局见 EncodeItem 注释；wings 编号在 [3] 高 nibble、选项 bit2 在 [3] 0x40——
// 两者都只有在该 nibble 区域非零时才回读（与 Encode 的"有值才写"对称）。
//
// 注意：卓越位（0x3F）与 Fenrir 位（0x07）在原版 12B 线格式里与翅膀/其他位共享字节 3，
// 语义歧义由上层按物品类型（Group）消歧；本解码保证 encode(decode(x)) == x 字节级往返。
package remote

import "mugo/internal/gamelogic/entity/item"

func DecodeItem(data []byte, it *item.Item) int {
	*it = item.Item{}
	it.Number = int(data[0])
	it.Level = (data[1] & levelMask) >> 3
	it.Durability = data[2]

	it.Luck = data[1]&luckFlag != 0
	it.HasSkill = data[1]&skillFlag != 0

	excWing := data[3] & 0x3F
	it.ExcellentBits = excWing & 0x3F

	optionBits := int(data[1] & 0x03)
	if data[3]&0x40 != 0 {
		optionBits |= 4
	}
	wingNumber := int(data[3]&0x30) >> 4
	it.OptionLevel = optionBits
	it.WingOptionNumber = wingNumber

	if data[3]&flag512 != 0 {
		it.Number |= 0x100
	}
	it.FenrirBits = data[3] & 0x07

	it.AncientDiscriminator = data[4] & ancientDiscrimMask
	it.AncientBonusLevel = (data[4] & ancientBonusMask) >> 2

	it.Group = data[5] >> 4
	it.GuardianOption = data[5]&guardianOptionFlag != 0

	if it.SocketCount = socketCountOf(data); it.SocketCount > 0 {
		it.HasSocketBonus = data[6] != socketNoSocket
		it.SocketBonus = data[6]
	} else {
		it.HarmonyNumber = data[6] >> 4
		it.HarmonyLevel = data[6] & 0x0F
	}

	for i := 0; i < maxSockets; i++ {
		b := data[7+i]
		switch {
		case b == socketNoSocket:
			it.SocketFilled[i] = false
		case b == socketEmptySocket:
			it.SocketFilled[i] = false
		default:
			it.SocketFilled[i] = true
			it.SocketSlots[i] = b
		}
	}
	return item.Size
}

// socketCountOf 从五个槽字节推断孔数：0xFF=无孔（其后必然全是 0xFF）。
func socketCountOf(data []byte) int {
	count := 0
	for i := 0; i < maxSockets; i++ {
		if data[7+i] == socketNoSocket {
			break
		}
		count++
	}
	return count
}
