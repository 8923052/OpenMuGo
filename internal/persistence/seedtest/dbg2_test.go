package seedtest

// dbg2_test.go —— test300 装备与原版 OpenMU C# Level300.cs 逐件对照回归
// （真机修复 11/12 的防线：装备栏物品必须与原版同组/同号/同等级/同选项/同卓越位）。
//
// 对照源：OpenMU src/Persistence/Initialization/VersionSeasonSix/TestAccounts/Level300.cs
//   CreateKnight   → test300Dk  （BladeKnight，class 6）
//   CreateWizard   → test300Dw  （SoulMaster，  class 2）
//   CreateElf      → test300Elf （MuseElf，     class 10）
//   CreateDarkLord → test300Dl  （DarkLord，    class 16）
//
// C# 助手签名（已逐字核对，修复 12 前曾误读）：
//   CreateWeapon(slot, group, number, level, optionLevel, luck, skill, excOption)
//   CreateArmorItem(slot, setNumber, group, targetExcOption, level, optionLevel, luck)
//     —— 第二组参是"编号"、第三组参才是"组号"；甲=组8/头=组7/裤=组9/手=组10/靴=组11。
//   CreateWings(slot, number, level, group=12) —— Cape of Lord 第 4 参 group=13 → (13,30)。
//   CreateHorse(PetSlot) → CreatePet(PetSlot, 4) → (13,4)；CreateFenrir → (13,37)。

import (
	"testing"

	"mugo/internal/gamelogic/config"
)

// TestTest300EquipmentParity 锁定 test300 四职业装备与 C# Level300.cs 字面量逐件一致。
func TestTest300EquipmentParity(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("config.LoadSeason6: %v", err)
	}

	// 期望装备（仅登记主装备槽 0..7 的确定性字段；武器/护具的卓越位逐件单条，
	// 见修复 12：Helm=MaxHP(0x20)/Armor=ArmorDec(0x08)/Pants=MoneyRate(0x01)/
	// Gloves=MaxMana(0x10)/Boots=Reflection(0x04)；ExcDmg 武器 = 0x20（option 6））。
	type expItem struct {
		group       byte
		number      int
		level       byte
		option      int
		luck, skill bool
		exc         byte
	}
	want := map[string]map[int]expItem{
		"test300Dk": {
			0: {0, 0, 13, 4, true, false, 0x20},  // Kris（左手）ExcDmg
			1: {0, 5, 13, 4, true, true, 0x20},   // Blade（右手）ExcDmg + Skill
			2: {7, 6, 13, 4, true, false, 0x20},  // Scale Helm
			3: {8, 6, 13, 4, true, false, 0x08},  // Scale Armor
			4: {9, 6, 13, 4, true, false, 0x01},  // Scale Pants
			5: {10, 6, 13, 4, true, false, 0x10}, // Scale Gloves
			6: {11, 6, 13, 4, true, false, 0x04}, // Scale Boots
			7: {12, 5, 13, 0, false, false, 0},   // Dragon Wings（原版随机 wing tier，仅锁组号/号/级）
		},
		"test300Dw": {
			2: {7, 7, 15, 4, true, false, 0},
			3: {8, 7, 15, 4, true, false, 0},
			4: {9, 7, 15, 4, true, false, 0},
			5: {10, 7, 15, 4, true, false, 0},
			6: {11, 7, 15, 4, true, false, 0},
			7: {12, 4, 13, 0, false, false, 0}, // Wings of Soul
		},
		"test300Elf": {
			2: {7, 12, 15, 4, true, false, 0},
			3: {8, 12, 15, 4, true, false, 0},
			4: {9, 12, 15, 4, true, false, 0},
			5: {10, 12, 15, 4, true, false, 0},
			6: {11, 12, 15, 4, true, false, 0},
			7: {12, 3, 13, 0, false, false, 0}, // Wings of Spirits
		},
		"test300Dl": {
			0: {2, 12, 13, 4, true, true, 0x20},   // Great Lord Scepter（左手）ExcDmg + Skill
			2: {7, 26, 13, 4, true, false, 0x20},  // Ada Helm
			3: {8, 26, 13, 4, true, false, 0x08},  // Ada Armor
			4: {9, 26, 13, 4, true, false, 0x01},  // Ada Pants
			5: {10, 26, 13, 4, true, false, 0x10}, // Ada Gloves
			6: {11, 26, 13, 4, true, false, 0x04}, // Ada Boots
			7: {13, 30, 13, 0, false, false, 0},   // Cape of Lord（group 13, number 30）
		},
	}

	accounts := Accounts(cfg)
	found := false
	for _, a := range accounts {
		if a.Name != "test300" {
			continue
		}
		found = true
		for _, c := range a.Characters {
			exp, ok := want[c.Name]
			if !ok {
				t.Fatalf("未登记 %s 的期望装备表", c.Name)
			}
			for slot, e := range exp {
				si := c.Inventory.GetItem(byte(slot))
				if si == nil || si.It == nil {
					t.Fatalf("%s slot %d 装备缺失（want g=%d n=%d）", c.Name, slot, e.group, e.number)
				}
				it := si.It
				if it.Group != e.group || it.Number != e.number {
					t.Errorf("%s slot %d 组号/编号 = (%d,%d) want (%d,%d)",
						c.Name, slot, it.Group, it.Number, e.group, e.number)
				}
				if it.Level != e.level {
					t.Errorf("%s slot %d 等级 = %d want %d", c.Name, slot, it.Level, e.level)
				}
				// 翅膀 tier 在原版随机，仅锁选项等级（option 数据面）；护具/武器锁 option。
				if slot != 7 && it.OptionLevel != e.option {
					t.Errorf("%s slot %d 选项 = %d want %d", c.Name, slot, it.OptionLevel, e.option)
				}
				if it.Luck != e.luck {
					t.Errorf("%s slot %d luck = %v want %v", c.Name, slot, it.Luck, e.luck)
				}
				if it.HasSkill != e.skill {
					t.Errorf("%s slot %d skill = %v want %v", c.Name, slot, it.HasSkill, e.skill)
				}
				if it.ExcellentBits != e.exc {
					t.Errorf("%s slot %d 卓越位 = %02X want %02X", c.Name, slot, it.ExcellentBits, e.exc)
				}
			}
		}
	}
	if !found {
		t.Fatalf("未找到 test300 账号")
	}
}

// TestTest300PetParity 锁定 test300 宠物槽（8）组号/编号/等级——与 C# CreateFenrir/
// CreateHorse 一致（DK/DW/Elf=黑/蓝/无 Fenrir (13,37)；DL=Dark Horse (13,4)）。
func TestTest300PetParity(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("config.LoadSeason6: %v", err)
	}
	want := map[string]struct {
		group  byte
		number int
		level  byte
	}{
		"test300Dk":  {13, 37, 0},
		"test300Dw":  {13, 37, 0},
		"test300Elf": {13, 37, 0},
		"test300Dl":  {13, 4, 0},
	}
	accounts := Accounts(cfg)
	for _, a := range accounts {
		if a.Name != "test300" {
			continue
		}
		for _, c := range a.Characters {
			e, ok := want[c.Name]
			if !ok {
				t.Fatalf("未登记 %s 的期望宠物", c.Name)
			}
			si := c.Inventory.GetItem(8)
			if si == nil || si.It == nil {
				t.Fatalf("%s 宠物槽(8) 缺失（want g=%d n=%d）", c.Name, e.group, e.number)
			}
			it := si.It
			if it.Group != e.group || it.Number != e.number || it.Level != e.level {
				t.Errorf("%s 宠物 = (%d,%d,%d) want (%d,%d,%d)",
					c.Name, it.Group, it.Number, it.Level, e.group, e.number, e.level)
			}
		}
	}
}
