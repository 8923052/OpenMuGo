// Package remote 的角色外观序列化（18 字节），对照 OpenMU GameServer/RemoteView/AppearanceSerializer.cs。
// 用于角色列表预览。原版另有 AppearanceSerializer075/095/Extended 变体，
// 后续按 _075/_095/_extended 后缀在本包拆分，并以 version.Constraint 标注适用区间。
package remote

import "mugo/internal/gamelogic/entity/item"

// EquipSlotsCount 是外观涉及的装备槽数量。
const EquipSlotsCount = 12

// AppearanceSize 是外观在封包中的固定字节数。
const AppearanceSize = item.AppearanceSize

// EncodeAppearance 将角色外观写入 18 字节缓冲，对照 AppearanceSerializer.WritePreviewCharSet。
func EncodeAppearance(a *item.Appearance, t []byte) {
	for i := 0; i < AppearanceSize; i++ {
		t[i] = 0
	}
	eq := func(slot int) *item.Equip {
		if slot < len(a.Equipment) {
			return a.Equipment[slot]
		}
		return nil
	}

	t[0] = byte(a.ClassNumber<<3) & 0xF8
	t[0] |= a.Pose

	setHand(t, eq(item.SlotLeftHand), 1, 12)
	setHand(t, eq(item.SlotRightHand), 2, 13)
	setArmorPiece(t, eq(item.SlotHelm), 3, true, 0x80, 13, false)
	setArmorPiece(t, eq(item.SlotArmor), 3, false, 0x40, 14, true)
	setArmorPiece(t, eq(item.SlotPants), 4, true, 0x20, 14, false)
	setArmorPiece(t, eq(item.SlotGloves), 4, false, 0x10, 15, true)
	setArmorPiece(t, eq(item.SlotBoots), 5, true, 0x08, 15, false)

	setItemLevels(t, a.Equipment)

	if a.FullAncientSetEquipped {
		t[11] |= 0x01
	}

	addWing(t, eq(item.SlotWings))
	addPet(t, eq(item.SlotPet))
}

func highNibble(v int) byte { return byte((v << 4) & 0xF0) }
func lowNibble(v int) byte  { return byte(v & 0x0F) }

func setHand(p []byte, e *item.Equip, indexIndex, groupIndex int) {
	if e != nil {
		p[indexIndex] = byte(e.Number)
		p[groupIndex] |= e.Group << 5
	} else {
		p[indexIndex] = 0xFF
		p[groupIndex] |= 0xF0
	}
}

func setEmptyArmor(p []byte, firstIndex int, firstHigh bool, secondMask, thirdIndex byte, thirdHigh bool) {
	if firstHigh {
		p[firstIndex] |= highNibble(0x0F)
	} else {
		p[firstIndex] |= lowNibble(0x0F)
	}
	p[9] |= secondMask
	if thirdHigh {
		p[thirdIndex] |= highNibble(0x0F)
	} else {
		p[thirdIndex] |= lowNibble(0x0F)
	}
}

func setArmorItemIndex(p []byte, e *item.Equip, firstIndex int, firstHigh bool, secondMask, thirdIndex byte, thirdHigh bool) {
	if firstHigh {
		p[firstIndex] |= highNibble(e.Number)
	} else {
		p[firstIndex] |= lowNibble(e.Number)
	}
	multi := e.Number / 16
	if multi > 0 {
		if multi%2 == 1 {
			p[9] |= secondMask
		}
		if b2 := multi / 2; b2 > 0 {
			if thirdHigh {
				p[thirdIndex] |= highNibble(b2)
			} else {
				p[thirdIndex] |= lowNibble(b2)
			}
		}
	}
}

func setArmorPiece(p []byte, e *item.Equip, firstIndex int, firstHigh bool, secondMask, thirdIndex byte, thirdHigh bool) {
	if e != nil {
		setArmorItemIndex(p, e, firstIndex, firstHigh, secondMask, thirdIndex, thirdHigh)
		if e.Excellent {
			p[10] |= secondMask
		}
		if e.Ancient {
			p[11] |= secondMask
		}
	} else {
		setEmptyArmor(p, firstIndex, firstHigh, secondMask, thirdIndex, thirdHigh)
	}
}

// setItemLevels 把 7 件可见装备（手*2 + 防具*5）的 glow level 压入 21 位。
func setItemLevels(p []byte, equipment []*item.Equip) {
	var levelIndex int
	for i := 0; i < 7; i++ {
		if i < len(equipment) && equipment[i] != nil {
			lv := int(equipment[i].Level)
			glow := byte((lv - 1) / 2)
			levelIndex |= int(glow) << (i * 3)
		}
	}
	p[6] = byte((levelIndex >> 16) & 0xFF)
	p[7] = byte((levelIndex >> 8) & 0xFF)
	p[8] = byte(levelIndex & 0xFF)
}

// addWing 编码翅膀外观（编号映射自 AppearanceSerializer.WingIndex）。
func addWing(p []byte, w *item.Equip) {
	if w == nil {
		return
	}
	n := w.Number
	switch n {
	case 0, 1, 2, 41:
		p[5] |= 0x04
	case 3, 4, 5, 6, 30, 42, 49:
		p[5] |= 0x08
	case 36, 37, 38, 39, 40, 43, 50, 130, 131, 132, 133, 134, 135:
		p[5] |= 0x0C
	}
	switch n {
	case 0, 3, 36:
		p[9] |= 0x01
	case 1, 4, 37:
		p[9] |= 0x02
	case 2, 5, 38:
		p[9] |= 0x03
	case 41, 6, 39:
		p[9] |= 0x04
	case 30, 40:
		p[9] |= 0x05
	case 42, 43:
		p[9] |= 0x06
	case 49, 50:
		p[9] |= 0x07
	case 130:
		p[17] |= 0x20
	case 131:
		p[17] |= 0x40
	case 132:
		p[17] |= 0x60
	case 133:
		p[17] |= 0x80
	case 134:
		p[17] |= 0xA0
	case 135:
		p[17] |= 0xC0
	}
}

// addPet 编码宠物外观（编号映射自 AppearanceSerializer.PetIndex）。
func addPet(p []byte, pet *item.Equip) {
	if pet == nil {
		p[5] |= 0x03
		return
	}
	n := pet.Number
	switch n {
	case 0, 1, 2:
		p[5] |= byte(n)
	case 3:
		p[5] |= 0x03
		p[10] |= 0x01
	case 4:
		p[5] |= 0x03
		p[12] |= 0x01
	case 37:
		p[5] |= 0x03
		p[10] &= 0xFE
		p[12] &= 0xFE
		p[12] |= 0x04
		p[16] = 0x00
		if pet.BlackFenrir {
			p[16] |= 0x01
		}
		if pet.BlueFenrir {
			p[16] |= 0x02
		}
		if pet.GoldFenrir {
			p[17] |= 0x01
		}
	default:
		p[5] |= 0x03
	}
	switch n {
	case 80:
		p[16] |= 0xE0
	case 106:
		p[16] |= 0xA0
	case 123:
		p[16] |= 0x60
	case 66:
		p[16] |= 0x80
	case 65:
		p[16] |= 0x40
	case 64:
		p[16] |= 0x20
	}
}
