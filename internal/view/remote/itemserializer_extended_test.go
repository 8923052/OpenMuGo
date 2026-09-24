// itemserializer_extended_test.go —— 扩展物品编码（5~15B）的回归网。
//
// 对拍策略：本测试**不复用被测代码的判断**，而是把 MuMain 客户端的
//
//	WSclient.cpp  CalcItemLength()   （定长推导）
//	NewUIItemMng.cpp ParseItemData() （字段解析）
//
// 逐行转写成 clientCalcItemLength / clientParseItemData，再用它做
// encode → 客户端语义解码 → 字段比对 的往返断言。
//
// 这样"编码正确"由客户端语义独立背书，而不是自己验证自己。
// 字节序/位序依据同样来自客户端与 OpenMU 两端一致的事实：
//   - 客户端 PITEM_EXTENDED_BASE = {WORD GroupAndNumber; BYTE Level; BYTE Durability;
//     ItemOptionFlags OptionFlags;}，#pragma pack(1)，枚举底层类型 :BYTE → base 恒 5B；
//   - Group 在高 4bit / Number 在低 12bit（客户端 ParseItemData:21-22 与 OpenMU
//     ItemStruct.Group/Number 双向一致）；
//   - 可选字节顺序 Opt → Exc → Anc → Harmony → Sockets（客户端 CalcItemLength 链）；
//     HasLuck / HasSkill / HasGuardian 是纯标志位，**不占字节**。
package remote

import (
	"bytes"
	"testing"

	"mugo/internal/gamelogic/entity/item"
)

// ---- 客户端语义转写（MuMain） ----

// clientOptionFlags 对应客户端 _enum.h `enum ItemOptionFlags : BYTE`。
const (
	clientHasOption    byte = 0x01
	clientHasLuck      byte = 0x02
	clientHasSkill     byte = 0x04
	clientHasExcellent byte = 0x08
	clientHasAncient   byte = 0x10
	clientHasHarmony   byte = 0x20
	clientHasGuardian  byte = 0x40
	clientHasSockets   byte = 0x80
)

// clientCalcItemLength 是 MuMain WSclient.cpp CalcItemLength() 的逐行转写。
func clientCalcItemLength(d []byte) int {
	size := 5
	flags := d[4]
	if flags&clientHasOption != 0 {
		size++
	}
	if flags&clientHasExcellent != 0 {
		size++
	}
	if flags&clientHasAncient != 0 {
		size++
	}
	if flags&clientHasHarmony != 0 {
		size++
	}
	if flags&clientHasSockets != 0 {
		socketCount := int(d[size] & 0x0F)
		size++
		size += socketCount
	}
	return size
}

// clientItemParams 是客户端 ItemCreationParams 的字段子集。
type clientItemParams struct {
	Group, Number                 int
	Level, Durability             byte
	WithLuck, WithSkill           bool
	OptionLevel, OptionType       int
	ExcellentFlags                byte
	AncientDiscriminator          int
	AncientBonusOption            int
	HasHarmonyOption              bool
	HarmonyOptionLevel, HarmonyOp int
	SocketBonusOption, SocketCnt  int
	SocketOptions                 [5]byte
}

// clientParseItemData 是 MuMain NewUIItemMng.cpp ParseItemData() 的逐行转写。
func clientParseItemData(d []byte) clientItemParams {
	var p clientItemParams
	if len(d) < 5 {
		return p
	}
	p.Group = int((d[0] >> 4) & 0xF)
	// 注意：必须先把 byte 提升为 int 再左移 8；byte<<8 在 Go 里恒为 0。
	p.Number = int(d[0]&0xF)<<8 + int(d[1])
	p.Level = d[2]
	p.Durability = d[3]
	flags := d[4]
	p.WithLuck = flags&clientHasLuck != 0
	p.WithSkill = flags&clientHasSkill != 0

	offset := 0
	if flags&clientHasOption != 0 {
		p.OptionLevel = int(d[5] & 0xF)
		p.OptionType = int((d[5] >> 4) & 0xF)
		offset++
	}
	if flags&clientHasExcellent != 0 {
		p.ExcellentFlags = d[5+offset]
		offset++
	}
	if flags&clientHasAncient != 0 {
		p.AncientDiscriminator = int(d[5+offset] & 0xF)
		p.AncientBonusOption = int((d[5+offset] >> 4) & 0xF)
		offset++
	}
	if flags&clientHasHarmony != 0 {
		p.HasHarmonyOption = true
		p.HarmonyOptionLevel = int(d[5+offset] & 0xF)
		p.HarmonyOp = int((d[5+offset] >> 4) & 0xF)
		offset++
	}
	if flags&clientHasSockets != 0 {
		p.SocketBonusOption = int((d[5+offset] >> 4) & 0xF)
		p.SocketCnt = int(d[5+offset] & 0xF)
		for i := 0; i < p.SocketCnt; i++ {
			p.SocketOptions[i] = d[6+offset+i]
		}
	}
	return p
}

// ---- 用例 ----

// extCase 是一个编码用例：物品 + 期望长度 + 期望字节（nil 表示只校验长度/往返）。
type extCase struct {
	name string
	it   *item.Item
	want int    // 期望编码长度
	hex  string // 期望字节（空格分隔的 hex，可选）
}

func extCases() []extCase {
	sock3 := &item.Item{
		Number: 20, Group: 8, Level: 13, Durability: 250,
		// SocketBonus 是 4bit 字段（OpenMU setter=(value&0xF)<<4），故用 4 而非 >15 的值。
		SocketCount: 3, HasSocketBonus: true, SocketBonus: 4,
	}
	sock3.SocketFilled[0], sock3.SocketSlots[0] = true, 1
	sock3.SocketFilled[1], sock3.SocketSlots[1] = true, 11
	sock3.SocketFilled[2], sock3.SocketSlots[2] = true, 21

	sockEmpty := &item.Item{
		Number: 21, Group: 8, Durability: 250, SocketCount: 2,
	}

	all := &item.Item{
		Number: 0x123, Group: 15, Level: 11, Durability: 200,
		Luck: true, HasSkill: true,
		OptionLevel: 7, WingOptionNumber: 3,
		ExcellentBits: 0x3F, FenrirBits: 0x02,
		AncientDiscriminator: 2, AncientBonusLevel: 3,
		HarmonyNumber: 9, HarmonyLevel: 12,
		GuardianOption: true,
		SocketCount:    2, HasSocketBonus: true, SocketBonus: 5,
	}
	all.SocketFilled[0], all.SocketSlots[0] = true, 100
	all.SocketFilled[1] = false

	return []extCase{
		{name: "plain", it: &item.Item{Group: 2, Number: 4, Level: 5, Durability: 28},
			want: 5, hex: "20 04 05 1C 00"},
		{name: "option_only", it: &item.Item{Group: 0, Number: 6, Level: 3, Durability: 50, OptionLevel: 7},
			want: 6, hex: "00 06 03 32 01 07"},
		{name: "wing_option", it: &item.Item{Group: 12, Number: 37, Level: 13, Durability: 255, OptionLevel: 4, WingOptionNumber: 3},
			want: 6, hex: "C0 25 0D FF 01 34"},
		{name: "luck_skill_excellent", it: &item.Item{Group: 4, Number: 8, Level: 11, Durability: 200, Luck: true, HasSkill: true, ExcellentBits: 0x3F},
			want: 6, hex: "40 08 0B C8 0E 3F"},
		{name: "fenrir_only", it: &item.Item{Group: 13, Number: 37, Durability: 255, FenrirBits: 0x02},
			want: 6, hex: "D0 25 00 FF 08 02"},
		{name: "ancient", it: &item.Item{Group: 7, Number: 10, Level: 6, Durability: 80, AncientDiscriminator: 2, AncientBonusLevel: 3},
			want: 6, hex: "70 0A 06 50 10 32"},
		{name: "guardian_no_extra_byte", it: &item.Item{Group: 5, Number: 12, Durability: 120, GuardianOption: true},
			want: 5, hex: "50 0C 00 78 40"},
		{name: "harmony", it: &item.Item{Group: 3, Number: 14, Level: 7, Durability: 60, HarmonyNumber: 9, HarmonyLevel: 12},
			want: 6, hex: "30 0E 07 3C 20 9C"},
		{name: "sockets_filled", it: sock3,
			want: 9, hex: "80 14 0D FA 80 43 01 0B 15"},
		{name: "sockets_empty", it: sockEmpty,
			want: 8, hex: "80 15 00 FA 80 F2 FE FE"},
		{name: "all_flags", it: all,
			want: 12}, // 5 + opt + exc + anc + harmony + sockets(1+2)
	}
}

func TestEncodeItemExtendedGoldenVectors(t *testing.T) {
	for _, tc := range extCases() {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, ItemExtendedMaxSize)
			n := EncodeItemExtended(tc.it, buf)
			if n != tc.want {
				t.Fatalf("长度=%d, want %d（字节 %X）", n, tc.want, buf[:n])
			}
			if n < ItemExtendedMinSize || n > ItemExtendedMaxSize {
				t.Fatalf("长度 %d 越出 5~15", n)
			}
			// 客户端 CalcItemLength 必须推出同一长度——这是"客户端不会错位"的直接证据。
			if got := clientCalcItemLength(buf); got != n {
				t.Fatalf("客户端 CalcItemLength=%d, 编码长度=%d（字节 %X）", got, n, buf[:n])
			}
			if tc.hex != "" {
				want := mustHex(t, spaceStripped(tc.hex))
				if !bytes.Equal(buf[:n], want) {
					t.Fatalf("字节不一致\n got: %X\nwant: %X", buf[:n], want)
				}
			}
		})
	}
}

// TestEncodeItemExtendedRoundTrip 用客户端语义解码回读，逐字段核对。
func TestEncodeItemExtendedRoundTrip(t *testing.T) {
	for _, tc := range extCases() {
		t.Run(tc.name, func(t *testing.T) {
			it := tc.it
			buf := make([]byte, ItemExtendedMaxSize)
			n := EncodeItemExtended(it, buf)

			p := clientParseItemData(buf[:n])
			if p.Group != int(it.Group) || p.Number != it.Number {
				t.Fatalf("Group/Number=(%d,%d), want (%d,%d)", p.Group, p.Number, it.Group, it.Number)
			}
			if p.Level != it.Level || p.Durability != it.Durability {
				t.Fatalf("Level/Dura=(%d,%d), want (%d,%d)", p.Level, p.Durability, it.Level, it.Durability)
			}
			if p.WithLuck != it.Luck || p.WithSkill != it.HasSkill {
				t.Fatalf("Luck/Skill=(%v,%v), want (%v,%v)", p.WithLuck, p.WithSkill, it.Luck, it.HasSkill)
			}

			// 只有真的写了该可选字节，客户端才应解出对应字段。
			if it.OptionLevel != 0 || it.WingOptionNumber > 0 {
				if p.OptionLevel != it.OptionLevel&0x0F {
					t.Fatalf("OptionLevel=%d, want %d", p.OptionLevel, it.OptionLevel&0x0F)
				}
				if p.OptionType != it.WingOptionNumber&0x0F {
					t.Fatalf("OptionType(=翅膀编号)=%d, want %d", p.OptionType, it.WingOptionNumber&0x0F)
				}
			} else if buf[4]&clientHasOption != 0 {
				t.Fatalf("未设置选项却置了 HasOption 位: flags=%02X", buf[4])
			}

			if it.ExcellentBits != 0 || it.WingOptionNumber > 0 || it.FenrirBits != 0 {
				want := (it.ExcellentBits & 0x3F) | (it.FenrirBits & 0x07)
				if p.ExcellentFlags != want {
					t.Fatalf("Excellent=%02X, want %02X", p.ExcellentFlags, want)
				}
			}
			if it.AncientDiscriminator != 0 {
				if p.AncientDiscriminator != int(it.AncientDiscriminator) {
					t.Fatalf("AncientDisc=%d, want %d", p.AncientDiscriminator, it.AncientDiscriminator)
				}
				if p.AncientBonusOption != int(it.AncientBonusLevel) {
					t.Fatalf("AncientBonus=%d, want %d", p.AncientBonusOption, it.AncientBonusLevel)
				}
			}
			if it.HarmonyNumber != 0 || it.HarmonyLevel != 0 {
				if !p.HasHarmonyOption {
					t.Fatal("HasHarmonyOption 未解出")
				}
				if p.HarmonyOp != int(it.HarmonyNumber) || p.HarmonyOptionLevel != int(it.HarmonyLevel) {
					t.Fatalf("Harmony=(%d,%d), want (%d,%d)", p.HarmonyOp, p.HarmonyOptionLevel, it.HarmonyNumber, it.HarmonyLevel)
				}
			}
			if it.SocketCount > 0 {
				if p.SocketCnt != it.SocketCount {
					t.Fatalf("SocketCount=%d, want %d", p.SocketCnt, it.SocketCount)
				}
				if it.HasSocketBonus && p.SocketBonusOption != int(it.SocketBonus) {
					t.Fatalf("SocketBonus=%d, want %d", p.SocketBonusOption, it.SocketBonus)
				}
				if !it.HasSocketBonus && p.SocketBonusOption != 0x0F {
					t.Fatalf("无奖励时应为 0x0F(=0xFF 写入后被 &0xF), got %X", p.SocketBonusOption)
				}
				for i := 0; i < it.SocketCount; i++ {
					want := byte(socketEmptySocket)
					if it.SocketFilled[i] {
						want = it.SocketSlots[i]
					}
					if p.SocketOptions[i] != want {
						t.Fatalf("Socket[%d]=%02X, want %02X", i, p.SocketOptions[i], want)
					}
				}
			}
		})
	}
}

// TestEncodeItemExtendedBufferGuard 校验容量不足时不写任何字节（调用方按 0 判定失败）。
func TestEncodeItemExtendedBufferGuard(t *testing.T) {
	it := &item.Item{Group: 1, Number: 1, Durability: 9}
	for size := 0; size < ItemExtendedMaxSize; size++ {
		buf := bytes.Repeat([]byte{0xAA}, size)
		before := append([]byte(nil), buf...)
		if n := EncodeItemExtended(it, buf); n != 0 {
			t.Fatalf("len=%d 时应返回 0, got %d", size, n)
		}
		if !bytes.Equal(buf, before) {
			t.Fatalf("len=%d 时不应写字节", size)
		}
	}
}

// spaceStripped 去掉十六进制串中的空格，便于按字节书写向量。
func spaceStripped(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
