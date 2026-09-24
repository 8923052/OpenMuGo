package seedtest

// seed_test.go —— 测试账号全集的形状与行为一致性验证：
// 账号清单/角色名/等级/职业/出生门/GM 状态/背包落袋/外观编码，全部对照
// OpenMU TestAccountsInitialization + AccountInitializerBase*。

import (
	"fmt"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
)

func loadAccounts(t *testing.T) []*entity.Account {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	return Accounts(cfg)
}

// TestFullAccountSet 锁定全集账号清单（对照 TestAccountsInitialization.Initialize）。
func TestFullAccountSet(t *testing.T) {
	accounts := loadAccounts(t)
	want := map[string]bool{}
	for i := 0; i < 10; i++ {
		want[fmt.Sprintf("test%d", i)] = true
	}
	for _, n := range []string{"test300", "test400", "ancient", "socket", "invext", "quest1", "quest2", "quest3", "testgm", "testgm2", "testunlock"} {
		want[n] = true
	}
	got := map[string]*entity.Account{}
	for _, a := range accounts {
		got[a.Name] = a
		if a.Name != a.Password {
			t.Fatalf("%s 密码应与账号名相同（OpenMU 种子语义）", a.Name)
		}
	}
	for n := range want {
		if got[n] == nil {
			t.Fatalf("缺账号 %s", n)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("账号数=%d want %d", len(got), len(want))
	}
}

// TestLowLevelAccounts 锁定 test0..9：等级 1/11/…/91、4 基础职业、出生门、外观。
func TestLowLevelAccounts(t *testing.T) {
	accounts := loadAccounts(t)
	byName := map[string]*entity.Account{}
	for _, a := range accounts {
		byName[a.Name] = a
	}
	for i := 0; i < 10; i++ {
		acc := byName[fmt.Sprintf("test%d", i)]
		if acc == nil {
			t.Fatalf("缺 test%d", i)
		}
		wantLevel := uint16(i*10 + 1)
		wantNames := []string{acc.Name + "Dk", acc.Name + "Dw", acc.Name + "Elf", acc.Name + "Dl"}
		wantClass := []byte{player.ClassDarkKnight, player.ClassDarkWizard, player.ClassFairyElf, player.ClassDarkLord}
		wantMap := []uint16{0, 0, 3, 0}
		if len(acc.Characters) != 4 {
			t.Fatalf("%s 角色数=%d，应为4", acc.Name, len(acc.Characters))
		}
		for ci, c := range acc.Characters {
			if c.Name != wantNames[ci] || c.Level != wantLevel {
				t.Fatalf("%s 角色%d 名称/等级不符: %s lv%d", acc.Name, ci, c.Name, c.Level)
			}
			if c.ClassNumber != wantClass[ci] || c.MapNumber != wantMap[ci] {
				t.Fatalf("%s 角色%d 职业/地图不符", acc.Name, ci)
			}
			if c.Slot != byte(ci) || c.Stats == nil || c.Stats.Money != 10_000_000 {
				t.Fatalf("%s 角色%d 槽位/属性/金币不符", acc.Name, ci)
			}
			if c.AppearanceExt[0] != c.ClassNumber {
				t.Fatalf("%s 扩展外观职业字节=%d want %d", c.Name, c.AppearanceExt[0], c.ClassNumber)
			}
			if c.MapNumber == 0 && (c.X < lorenciaX1 || c.X > lorenciaX2 || c.Y < lorenciaY1 || c.Y > lorenciaY2) {
				t.Fatalf("%s Lorencia 出生点越界: (%d,%d)", c.Name, c.X, c.Y)
			}
			if c.MapNumber == 3 && (c.X < noriaX1 || c.X > noriaX2 || c.Y < noriaY1 || c.Y > noriaY2) {
				t.Fatalf("%s Noria 出生点越界: (%d,%d)", c.Name, c.X, c.Y)
			}
			// 武器已入背包 0 槽（Small Axe / Skull Staff / Short Bow 区分职业）。
			if c.Inventory == nil || c.Inventory.GetItem(0) == nil {
				t.Fatalf("%s 武器位应有物品", c.Name)
			}
		}
	}
}

// TestGMAccounts 锁定 GM 账号状态与点数（AccountState.GameMaster + 20000 点 + GM 角色态）。
func TestGMAccounts(t *testing.T) {
	accounts := loadAccounts(t)
	for _, a := range accounts {
		isGM := a.Name == "testgm" || a.Name == "testgm2" || a.Name == "testunlock"
		if isGM && a.State != entity.AccountStateGameMaster {
			t.Fatalf("%s 应为 GM 态", a.Name)
		}
		if !isGM && a.State == entity.AccountStateGameMaster {
			t.Fatalf("%s 不应为 GM", a.Name)
		}
		if !isGM {
			continue
		}
		for _, c := range a.Characters {
			if c.Status != entity.CharacterStatusGameMaster {
				t.Fatalf("%s 角色状态应为 GM", c.Name)
			}
			if c.Stats == nil || c.Stats.LevelUpPoints != 20000 {
				t.Fatalf("%s 升级点应为 20000", c.Name)
			}
		}
	}
}

// TestTest400Inventory 锁定 test400：5 职业（DK/DW/Elf/MG/DL）、+15 主手武器、
// 宝石药水齐全（对照 Level400.cs）。
func TestTest400Inventory(t *testing.T) {
	accounts := loadAccounts(t)
	var acc *entity.Account
	for _, a := range accounts {
		if a.Name == "test400" {
			acc = a
		}
	}
	if acc == nil {
		t.Fatal("缺 test400")
	}
	byClass := map[byte]entity.Character{}
	for _, c := range acc.Characters {
		byClass[c.ClassNumber] = c
	}
	for _, cls := range []byte{7, 3, 11, 13, 17} { // BladeMaster/GrandMaster/HighElf/DuelMaster/LordEmperor
		c, ok := byClass[cls]
		if !ok {
			t.Fatalf("test400 缺职业 %d", cls)
		}
		// Elf 的弓在右手（slot1，原版 RightHandSlot）且为 +13（Level400.CreateElf 字面量）；
		// 其余职业主手 slot0 +15。
		mainSlot, wantLvl := byte(0), byte(15)
		if cls == 11 {
			mainSlot, wantLvl = 1, 13
		}
		w := c.Inventory.GetItem(mainSlot)
		if w == nil || w.It.Level != wantLvl {
			t.Fatalf("%s 槽 %d 应为 +%d 武器", c.Name, mainSlot, wantLvl)
		}
		bless := 0
		for s := 12; s < 76; s++ {
			if it := c.Inventory.GetItem(byte(s)); it != nil && it.It.Group == 14 && it.It.Number == jewelBless {
				bless++
			}
		}
		if bless < 8 {
			t.Fatalf("%s Bless 应≥8, got %d", c.Name, bless)
		}
		if c.Stats.LevelUpPoints == 0 || c.Stats.LevelUpPoints >= 20000 {
			t.Fatalf("%s LevelUpPoints 异常: %d", c.Name, c.Stats.LevelUpPoints)
		}
	}
}

// TestAccountsMatchRealAccountBehavior 行为一致性：种子账号与真实账号走同一实体/
// 同一 Store/同一属性解析路径，锁定关键不变量（当前值 ≤ 上限、外观已编码、有背包）。
func TestAccountsMatchRealAccountBehavior(t *testing.T) {
	accounts := loadAccounts(t)
	for _, a := range accounts {
		for _, c := range a.Characters {
			if c.Stats == nil {
				t.Fatalf("%s Stats 不能为 nil", c.Name)
			}
			if c.Stats.CurrentHealth > c.Stats.MaximumHealth || c.Stats.CurrentMana > c.Stats.MaximumMana {
				t.Fatalf("%s 当前值越界", c.Name)
			}
			if len(c.AppearanceExt) != 27 {
				t.Fatalf("%s 外观长度=%d", c.Name, len(c.AppearanceExt))
			}
			if c.Inventory == nil {
				t.Fatalf("%s Inventory 不能为 nil（真实账号进图即有容器）", c.Name)
			}
			// 外观↔背包一致性：装备区每件物品必须出现在 27B 外观的对应槽位
			// （真机修复 11：Equipment 曾按 append 压缩导致槽位错位、外观乱堆）。
			for slot := byte(0); slot < 12; slot++ {
				si := c.Inventory.GetItem(slot)
				if si == nil {
					continue
				}
				if si.It.Group == 12 && (si.It.Number == 3 || si.It.Number == 4 || si.It.Number == 5 || (si.It.Number >= 30 && si.It.Number <= 43)) {
					// 翅膀在 [23..24]；d[23] 低 nibble 与 Fenrir 可见选项位共用
					// （原版 items[23] |= 0b10/0b100/0b110），断言时屏蔽。
					w := (uint16(c.AppearanceExt[23]&0xF0&0x0F) << 8) | uint16(c.AppearanceExt[24])
					if w != uint16(si.It.Number) {
						t.Fatalf("%s 槽 %d 翅膀 %d 未表达于外观（w=%d）", c.Name, slot, si.It.Number, w)
					}
					continue
				}
				if si.It.Group == 13 && (si.It.Number <= 5 || si.It.Number == 37) {
					// 宠物在 [25..26]；d[25] 低 nibble 与 Fenrir 可见选项位共用
					// （原版 items[23] |= 0b10/0b100/0b110），断言时屏蔽 0x07。
					p := (uint16(c.AppearanceExt[25]&0x08) << 8) | uint16(c.AppearanceExt[26])
					if p != uint16(si.It.Number) {
						t.Fatalf("%s 槽 %d 宠物 %d 未表达于外观（p=%d）", c.Name, slot, si.It.Number, p)
					}
					continue
				}
				shinyIdx := map[byte]int{0: 0, 1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6}[slot]
				if shinyIdx == 0 && slot != 0 {
					continue // 非外观槽（9/10/11 首饰不进 27B 闪光段）
				}
				off := 2 + shinyIdx*3
				d0 := c.AppearanceExt[off]
				if d0 == 0xFF {
					t.Fatalf("%s 槽 %d 物品 (%d,%d) 外观应为非空", c.Name, slot, si.It.Group, si.It.Number)
				}
				g := d0 >> 4
				if int(g) != int(si.It.Group)&0xF {
					t.Fatalf("%s 槽 %d 外观组 %d ≠ 物品组 %d（槽位错位）", c.Name, slot, g, si.It.Group)
				}
			}
		}
	}
}

// TestExperienceFormula 保留既有公式锚点。
func TestExperienceFormula(t *testing.T) {
	if got := player.ExperienceForLevel(10); got != 14580 {
		t.Fatalf("10级经验=%d，应为14580", got)
	}
	if got := player.ExperienceForLevel(1); got != 0 {
		t.Fatalf("1级经验=%d，应为0", got)
	}
}

// TestInventoryExtensionFixture 锁定 invext 账号：2 页扩展的**容器容量**与**上报页数**
// 必须同源（真机据此画扩展页；只有容器有页而 F3 03 报 0，页上物品在客户端不可见）。
func TestInventoryExtensionFixture(t *testing.T) {
	accounts := loadAccounts(t)
	var char *entity.Character
	for _, a := range accounts {
		if a.Name != "invext" {
			continue
		}
		if len(a.Characters) != 1 {
			t.Fatalf("invext 应只有 1 个角色, got %d", len(a.Characters))
		}
		char = &a.Characters[0]
	}
	if char == nil {
		t.Fatal("缺 invext 账号")
	}
	if char.Name != "invextDk" {
		t.Fatalf("角色名=%s", char.Name)
	}
	if char.Stats == nil || char.Stats.InventoryExtensions != 2 {
		t.Fatalf("上报页数应为 2, got %+v", char.Stats)
	}
	if got := char.Inventory.Grid().SlotCount(); got != storage.EquippedSlotsCount+storage.GetInventorySize(2) {
		t.Fatalf("容器槽数应按 2 页计, got %d", got)
	}
	// 页首末格各一件（14 组宝石），且必须落在页内而不是被挤到主网格。
	for _, slot := range []byte{76, 107, 108, 139} {
		si := char.Inventory.GetItem(slot)
		if si == nil || si.It.Group != 14 {
			t.Fatalf("页内槽 %d 应有 (14,x) 宝石, got %+v", slot, si)
		}
	}
}
