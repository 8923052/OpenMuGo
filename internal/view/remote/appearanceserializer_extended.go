// Package remote 的进图视野外观序列化（27 字节），对照 OpenMU AppearanceSerializerExtended
// （S6E3，106.3+）。与角色列表 18B 预览是两套编码：
//
//	[0]    职业编号原始值（DK=4，不左移）
//	[1]    Pose | 0x10 远古全套 | 0x20 GM
//	[2..22] 7 件闪光装备各 3B（左右手/头/铠/裤/手/鞋）：
//	       byte0 高nibble=group(4) 低nibble=number 高4位
//	       byte1 = number 低8位
//	       byte2 高nibble=GlowLevel，0x08=卓越 0x04=远古
//	[23..24] 翅膀 2B（group 高nibble + number 12bit）
//	[25..26] 宠物 2B（同上；Fenrir 可见选项改写 byte25：0x02黑 0x04蓝 0x06金）
//
// 空槽：闪光 FF FF 00；非闪光 FF FF。
//
// 本文件即原版 AppearanceSerializerExtended.cs 的 Go 侧落位，故不带 _extended 后缀；
// 将来若补齐 075/095 变体，以 _075/_095 后缀另起文件。
package remote

import "mugo/internal/gamelogic/entity/item"

// AppearanceExtSize 是扩展外观固定字节数。
const AppearanceExtSize = item.AppearanceExtSize

// EncodeAppearanceExt 将进图外观写入 27 字节缓冲，对照 WritePreviewCharSet（Extended 版）。
func EncodeAppearanceExt(a *item.Appearance, t []byte) {
	for i := 0; i < AppearanceExtSize; i++ {
		t[i] = 0
	}
	eq := func(slot int) *item.Equip {
		if slot < len(a.Equipment) {
			return a.Equipment[slot]
		}
		return nil
	}

	t[0] = byte(a.ClassNumber)
	t[1] = a.Pose
	if a.FullAncientSetEquipped {
		t[1] |= 0x10
	}
	if a.GameMaster {
		t[1] |= 0x20
	}

	// 7 件闪光装备：槽位 i 对应偏移 2+i*3。
	shinySlots := []int{item.SlotLeftHand, item.SlotRightHand, item.SlotHelm, item.SlotArmor, item.SlotPants, item.SlotGloves, item.SlotBoots}
	for i, slot := range shinySlots {
		setShinyItem(t[2+i*3:], eq(slot))
	}
	setUnshinyItem(t[23:], eq(item.SlotWings))
	setUnshinyItem(t[25:], eq(item.SlotPet))
	if pet := eq(item.SlotPet); pet != nil {
		// 逐行对照 AppearanceSerializerExtended 的 Fenrir 分支：OR 在 items[23] =
		// t[25] = **宠物段 d[0]**（items = target[2..]，items[23..25] 即 t[25..27]）。
		// 金芬里尔 OR 0b110 → 0xD0|0x06=0xD6，与 golden 向量 D6 25 一致。
		if pet.BlackFenrir {
			t[25] |= 0x02
		}
		if pet.BlueFenrir {
			t[25] |= 0x04
		}
		if pet.GoldFenrir {
			t[25] |= 0x06
		}
	}
}

func setShinyItem(d []byte, e *item.Equip) {
	if e == nil {
		// group=0xF number=0xFFF glow=0 flags=0
		d[0], d[1], d[2] = 0xFF, 0xFF, 0x00
		return
	}
	d[0] = byte((e.Group&0x0F)<<4 | byte((e.Number>>8)&0x0F))
	d[1] = byte(e.Number)
	glow := byte(0)
	if e.Level >= 1 {
		glow = (e.Level - 1) / 2
	}
	d[2] = (glow & 0x0F) << 4
	if e.Excellent {
		d[2] |= 0x08
	}
	if e.Ancient {
		d[2] |= 0x04
	}
}

func setUnshinyItem(d []byte, e *item.Equip) {
	if e == nil {
		// group=0xF number=0xFFF
		d[0], d[1] = 0xFF, 0xFF
		return
	}
	d[0] = byte((e.Group&0x0F)<<4 | byte((e.Number>>8)&0x0F))
	d[1] = byte(e.Number)
}
