package player

import (
	"math"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// minPhysByWeapon 是短剑的直接加成目标（与导出件 designation 一致）。
const minPhysByWeapon = "Minimum Physical Base Damage By Weapon"

func equipmentChar() (*config.GameConfig, *entity.Character) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		panic(err)
	}
	c := &entity.Character{
		ClassNumber: 4, // Dark Knight（可装备短剑）
		Level:       1,
		Inventory:   storage.NewInventory(0),
	}
	return cfg, c
}

// TestEquippedWeaponPowerUp 验证装备短剑后物攻基础加成回灌：
// +0 短剑加 3；+5 短剑再叠加武器等级表 12，共 15。
func TestEquippedWeaponPowerUp(t *testing.T) {
	cfg, c := equipmentChar()

	before := AttributeValue(cfg, c, minPhysByWeapon)

	// +0 短剑（group0,number1；占 1×3）→ min phys base +3。
	sword0 := &item.Item{Group: 0, Number: 1}
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: sword0, Width: 1, Height: 3}) {
		t.Fatal("装备短剑失败")
	}
	after0 := AttributeValue(cfg, c, minPhysByWeapon)
	if d := after0 - before; d != 3 {
		t.Fatalf("+0 短剑加成=%v，期望 3", d)
	}

	// 换成 +5 短剑：基础 3 + 武器等级表 level5=12 → 15。
	c.Inventory.Remove(c.Inventory.GetItem(0))
	sword5 := &item.Item{Group: 0, Number: 1, Level: 5}
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: sword5, Width: 1, Height: 3}) {
		t.Fatal("装备+5短剑失败")
	}
	after5 := AttributeValue(cfg, c, minPhysByWeapon)
	if d := after5 - before; d != 18 {
		t.Fatalf("+5 短剑加成=%v，期望 18（基础3+武器等级表15）", d)
	}

	// 脱下 → 加成消失。
	c.Inventory.Remove(c.Inventory.GetItem(0))
	if got := AttributeValue(cfg, c, minPhysByWeapon); got != before {
		t.Fatalf("脱下后加成未移除: got=%v before=%v", got, before)
	}
}

// TestEquippedWeaponAttackSpeed 验证短剑攻速（目标 "Attack Speed by Weapons"）：
// 装备短剑（攻速20，单持归一化乘数1）→ 20。
// 注：空手该属性为双持归一化乘数（0.5^0=1，原版公式），非武器攻速，故不断言空手。
func TestEquippedWeaponAttackSpeed(t *testing.T) {
	cfg, c := equipmentChar()
	c.Inventory.AddToSlot(0, &storage.SlottedItem{
		It: &item.Item{Group: 0, Number: 1}, Width: 1, Height: 3,
	})
	if got := AttributeValue(cfg, c, "Attack Speed by Weapons"); got != 20 {
		t.Fatalf("短剑武器攻速=%v，期望 20", got)
	}
}

// TestEquippedItemOptionPowerUps 锁定装备选项加成回灌（对照 ItemPowerUpFactory.
// GetPowerUpsOfItemOptions）：短剑(0,1) 的候选定义含幸运/普通/卓越/和谐四类，
// 逐项按位域还原后应各自命中导出件里的加成值：
//
//	幸运          → Critical Damage Chance +0.05
//	普通选项 L1   → Physical Base Damage (min and max) +4（Level n = n×4）
//	卓越位 3      → Attack Speed Any +7
//	和谐 Number 1 L0 → Minimum Physical Base Damage +2
func TestEquippedItemOptionPowerUps(t *testing.T) {
	cfg, c := equipmentChar()

	const (
		desCrit    = "Critical Damage Chance"
		desPhys    = "Physical Base Damage (min and max)"
		desAtkSpd  = "Attack Speed Any"
		desMinPhys = "Minimum Physical Base Damage"
	)
	// 基线：先装备"无任何选项"的同款短剑——武器自身的基础加成（如
	// Attack Speed by Weapons 20）必须排除在外，差值才是选项贡献。
	plain := &item.Item{Group: 0, Number: 1, Durability: 20}
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: plain, Width: 1, Height: 3}) {
		t.Fatal("装备短剑失败")
	}
	baseCrit := AttributeValue(cfg, c, desCrit)
	basePhys := AttributeValue(cfg, c, desPhys)
	baseSpd := AttributeValue(cfg, c, desAtkSpd)
	baseMin := AttributeValue(cfg, c, desMinPhys)
	c.Inventory.Remove(c.Inventory.GetItem(0))

	// ExcellentBits 位 3（1<<2）↔ 卓越物攻定义 Number 3（Attack Speed Any +7）。
	sword := &item.Item{
		Group: 0, Number: 1, Durability: 20,
		Luck: true, OptionLevel: 1, ExcellentBits: 1 << 2,
	}
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: sword, Width: 1, Height: 3}) {
		t.Fatal("装备短剑失败")
	}

	// 属性值内部是 float32，比较留 1e-6 容差。
	if d := AttributeValue(cfg, c, desCrit) - baseCrit; math.Abs(d-0.05) > 1e-6 {
		t.Fatalf("幸运应给 Critical Damage Chance +0.05, got %v", d)
	}
	if d := AttributeValue(cfg, c, desPhys) - basePhys; d != 4 {
		t.Fatalf("普通选项 L1 应给 Physical Base Damage (min and max) +4, got %v", d)
	}
	if d := AttributeValue(cfg, c, desAtkSpd) - baseSpd; d != 7 {
		t.Fatalf("卓越位 3 应给 Attack Speed Any +7, got %v", d)
	}

	// 和谐单独验证：Minimum Physical Base Damage 同时吃普通选项的
	// "(min and max)" 与和谐的直接加成，混在一起看不出归属，故换成只带和谐的短剑。
	c.Inventory.Remove(c.Inventory.GetItem(0))
	harmonyOnly := &item.Item{
		Group: 0, Number: 1, Durability: 20, HarmonyNumber: 1, HarmonyLevel: 0,
	}
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: harmonyOnly, Width: 1, Height: 3}) {
		t.Fatal("装备和谐短剑失败")
	}
	if d := AttributeValue(cfg, c, desMinPhys) - baseMin; d != 2 {
		t.Fatalf("和谐 Number1 L0 应给 Minimum Physical Base Damage +2, got %v", d)
	}

	// 换成无选项短剑 → 选项加成随之消失（属性系统每次重建）。
	c.Inventory.Remove(c.Inventory.GetItem(0))
	if !c.Inventory.AddToSlot(0, &storage.SlottedItem{It: plain, Width: 1, Height: 3}) {
		t.Fatal("重新装备无选项短剑失败")
	}
	if got := AttributeValue(cfg, c, desCrit); got != baseCrit {
		t.Fatalf("换下后幸运加成未移除: got=%v base=%v", got, baseCrit)
	}
	if got := AttributeValue(cfg, c, desPhys); got != basePhys {
		t.Fatalf("换下后普通选项加成未移除: got=%v base=%v", got, basePhys)
	}
	if got := AttributeValue(cfg, c, desAtkSpd); got != baseSpd {
		t.Fatalf("换下后卓越加成未移除: got=%v base=%v", got, baseSpd)
	}
	if got := AttributeValue(cfg, c, desMinPhys); got != baseMin {
		t.Fatalf("换下后和谐加成未移除: got=%v base=%v", got, baseMin)
	}
}

// TestEquippedAncientSetBonus 锁定远古套装（对照 ItemPowerUpFactory.GetSetPowerUps 与
// ItemOfItemSet.BonusOption）：装备 Warrior 套装 2 件（套装共 7 件、MinimumItemCount=2）→
// 未集齐取套装选项第 1 条（Total Strength +10）；每件远古物品再按 AncientBonusLevel=1
// 取额外远古属性（Total Vitality +5）。
func TestEquippedAncientSetBonus(t *testing.T) {
	cfg, c := equipmentChar()
	baseStr := AttributeValue(cfg, c, "Total Strength")
	baseVit := AttributeValue(cfg, c, "Total Vitality")

	// Warrior 套装成员 (10,5)/(11,5)，远古判别值 1。
	members := []struct {
		slot  byte
		group byte
	}{{7, 10}, {8, 11}}
	for _, m := range members {
		it := &item.Item{
			Group: m.group, Number: 5, Durability: 20,
			AncientDiscriminator: 1, AncientBonusLevel: 1,
		}
		if !c.Inventory.AddToSlot(m.slot, &storage.SlottedItem{It: it, Width: 1, Height: 1}) {
			t.Fatalf("装备 (%d,5) 失败", m.group)
		}
	}

	if d := AttributeValue(cfg, c, "Total Strength") - baseStr; d != 10 {
		t.Fatalf("Warrior 套装(2 件)应给 Total Strength +10, got %v", d)
	}
	if d := AttributeValue(cfg, c, "Total Vitality") - baseVit; d != 10 {
		t.Fatalf("两件远古的额外 Total Vitality 应为 +10（各 +5）, got %v", d)
	}
}
