// itemserializer_extended.go —— 扩展物品序列化，逐行对照 OpenMU
// GameServer/RemoteView/ItemSerializerExtended.cs（[MinimumClient(106,3)]）。
//
// 为什么必须区分两套：OpenMU 有多个 IItemSerializer 插件，按客户端版本择一：
//   - ItemSerializer        固定 12B   （[MinimumClient(5, 0)]  = 0.90 世代及更早）
//   - ItemSerializerExtended 动态 5~15B（[MinimumClient(106, 3)] = 1.06.03 及更新）
//
// 本项目的客户端（MuMain）属于后者，证据在客户端源码里是硬编码事实：
//   - WSclient.h PITEM_EXTENDED_BASE = {GroupAndNumber(WORD), Level, Durability, OptionFlags}
//     注释 "Total: 5 ~ 15 bytes"；
//   - WSclient.cpp CalcItemLength() 由 OptionFlags 位推导长度，base size = 5；
//   - ReceiveCreateItemViewportExtended / ReceiveInventoryExtended 都走 CalcItemLength。
//
// 因此向该客户端发 12B 固定布局会被按扩展布局解析（Group/Number 取错、长度算成 5），
// 物品整条错位 → 地面/背包物品不显示。EncodeItem（12B）保留为低版本变体，仍在 golden
// 测试里逐字节锚定 OpenMU ItemSerializer。
//
// 布局（ItemSerializerExtended.ItemStruct，逐字段对照）：
//
//	[0]  Group<<4 | ((Number>>8)&0x0F)   // Group 4bit + Number 高 4bit
//	[1]  Number & 0xFF                   // Number 低 8bit
//	[2]  Level
//	[3]  Durability
//	[4]  OptionFlags
//	[5..] 可选字节，按 flags 出现，顺序固定：
//	       HasOption    → 1B: OptionType<<4 | (OptionLevel & 0x0F)
//	       HasExcellent → 1B: ExcellentBits | FenrirBits
//	       HasAncient   → 1B: AncientBonusLevel<<4 | (AncientDiscriminator & 0x0F)
//	       HasHarmony   → 1B: HarmonyNumber<<4 | (HarmonyLevel & 0x0F)
//	       HasSockets   → 1B: SocketBonus<<4 | (SocketCount & 0x0F)，后接 SocketCount 个槽字节
//
// 注意 HasLuck / HasSkill **不再**混进 Level 字节（那是 12B 变体的做法），
// 而是走 OptionFlags；客户端 PITEM_EXTENDED_BASE.Level 是纯等级。
package remote

import "mugo/internal/gamelogic/entity/item"

// 扩展物品编码的长度区间（对照客户端 CalcItemLength 的 base size 5 与 NeededSpace 15）。
const (
	ItemExtendedMinSize = 5
	ItemExtendedMaxSize = 15
)

// OptionFlags（对照 ItemSerializerExtended.OptionFlags 与客户端 _enum.h ItemOptionFlags）。
const (
	optHasOption    byte = 0x01
	optHasLuck      byte = 0x02
	optHasSkill     byte = 0x04
	optHasExcellent byte = 0x08
	optHasAncient   byte = 0x10
	optHasHarmony   byte = 0x20
	optHasGuardian  byte = 0x40
	optHasSockets   byte = 0x80
)

// optionFlags 由领域模型推导 OptionFlags（对照 ItemSerializerExtended.GetOptionFlags）。
//
// 两个容易混淆的点，都以 OpenMU 为准：
//
//  1. HasExcellent 的触发条件是**选项类型**属于
//     {Excellent, Wing, BlackFenrir, BlueFenrir, GoldFenrir}，与"编号"无关。
//     本模型的 ExcellentBits 文档即"卓越/翅膀选项位"，已包含 Wing 类型选项的位
//     （对照 ItemSerializerHelper.GetExcellentByte：它把 Excellent 与 Wing 两类
//     选项 OR 进同一字节），因此这里只看 ExcellentBits/FenrirBits。
//     **不可**因为 WingOptionNumber > 0 就置 HasExcellent——那个字段是下面第 2 点。
//
//  2. "翅膀档位"在 OpenMU 里是 OptionType == Option 的选项，它的 Number（1..3）
//     分别落到：12B 变体 → t[3] 高 nibble（ItemSerializer 的 `(Number & 0b11) << 4`）；
//     扩展变体 → OptionType 高 nibble。本模型用 WingOptionNumber 承载它。
//     它与 HasOption 同源（翅膀确实带一个普通"+选项"），但与 HasExcellent 无关。
func optionFlags(it *item.Item) byte {
	var f byte
	if it.OptionLevel != 0 || it.WingOptionNumber > 0 {
		f |= optHasOption
	}
	if it.Luck {
		f |= optHasLuck
	}
	if it.HasSkill {
		f |= optHasSkill
	}
	if it.ExcellentBits != 0 || it.FenrirBits != 0 {
		f |= optHasExcellent
	}
	if it.AncientDiscriminator != 0 {
		f |= optHasAncient
	}
	if it.HarmonyNumber != 0 || it.HarmonyLevel != 0 {
		f |= optHasHarmony
	}
	if it.GuardianOption {
		f |= optHasGuardian
	}
	if it.SocketCount > 0 {
		f |= optHasSockets
	}
	return f
}

// EncodeItemExtended 将物品写入扩展布局，返回实际写入长度（5~15）。
// 调用方必须保证 len(t) >= ItemExtendedMaxSize；容量不足返回 0（不写任何字节）。
func EncodeItemExtended(it *item.Item, t []byte) int {
	if len(t) < ItemExtendedMaxSize {
		return 0
	}
	for i := 0; i < ItemExtendedMaxSize; i++ {
		t[i] = 0
	}

	// Group 4bit + Number 12bit（对照 ItemStruct.Group / ItemStruct.Number）。
	t[0] = it.Group<<4 | byte((it.Number>>8)&0x0F)
	t[1] = byte(it.Number & 0xFF)
	t[2] = it.Level
	t[3] = it.Durability

	flags := optionFlags(it)
	t[4] = flags

	n := 5
	if flags&optHasOption != 0 {
		// OptionType 高 4bit + OptionLevel 低 4bit（对照 ItemStruct.OptionByte 组合语义）。
		t[n] = byte((it.WingOptionNumber&0x0F)<<4) | byte(it.OptionLevel&0x0F)
		n++
	}
	if flags&optHasExcellent != 0 {
		t[n] = (it.ExcellentBits & 0x3F) | (it.FenrirBits & 0x07)
		n++
	}
	if flags&optHasAncient != 0 {
		t[n] = (it.AncientBonusLevel&0x0F)<<4 | (it.AncientDiscriminator & 0x0F)
		n++
	}
	if flags&optHasHarmony != 0 {
		t[n] = (it.HarmonyNumber&0x0F)<<4 | (it.HarmonyLevel & 0x0F)
		n++
	}
	if flags&optHasSockets != 0 {
		// SocketBonus 高 4bit + SocketCount 低 4bit。
		// 注意奖励编号是 **4 bit 字段**：OpenMU 的 SocketBonus setter 做 (value & 0xF) << 4，
		// 客户端 ParseItemData 也只读 (byte >> 4) & 0xF，故编号 > 15 会被截断（两边一致）。
		// 无奖励时 OpenMU 的 GetSocketBonusByte 返回 0xFF，经 &0xF 即高 nibble = 0xF。
		bonus := socketNoSocket
		if it.HasSocketBonus {
			bonus = it.SocketBonus
		}
		t[n] = (bonus&0x0F)<<4 | byte(it.SocketCount&0x0F)
		n++
		for i := 0; i < it.SocketCount && i < maxSockets; i++ {
			t[n] = socketByte(it, i)
			n++
		}
	}
	return n
}
