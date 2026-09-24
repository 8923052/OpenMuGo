package player

import (
	"math"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// petChar 构造可加载导出件的测试角色（classNumber 决定职业基值）。
func petChar(classNumber byte) (*config.GameConfig, *entity.Character) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		panic(err)
	}
	return cfg, &entity.Character{
		ClassNumber: classNumber,
		Level:       200,
		Inventory:   storage.NewInventory(0),
	}
}

// equip 把物品放入指定装备槽。
func equip(t *testing.T, c *entity.Character, slot byte, it *item.Item) {
	t.Helper()
	if !c.Inventory.AddToSlot(slot, &storage.SlottedItem{It: it, Width: 1, Height: 1}) {
		t.Fatalf("装备物品 (%d,%d) 到槽 %d 失败", it.Group, it.Number, slot)
	}
}

// TestPetTrainableLevelInjection 锁定可训练宠物等级注入：黑暗之马→"Dark Horse Level"
// （槽8），黑暗渡鸦→"Dark Raven Level"（槽1）；未装备时为 0（对照 GetPetLevel）。
func TestPetTrainableLevelInjection(t *testing.T) {
	cfg, c := petChar(16) // Dark Lord（可装备马/渡鸦，含渡鸦派生关系）
	if got := AttributeValue(cfg, c, "Dark Horse Level"); got != 0 {
		t.Fatalf("未装备马时 Dark Horse Level 应为 0，got %v", got)
	}
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 4, Level: 7, Durability: 255})
	if got := AttributeValue(cfg, c, "Dark Horse Level"); got != 7 {
		t.Fatalf("黑暗之马等级应注入 Dark Horse Level=7，got %v", got)
	}
	equip(t, c, item.SlotRightHand, &item.Item{Group: 13, Number: 5, Level: 12, Durability: 255})
	if got := AttributeValue(cfg, c, "Dark Raven Level"); got != 12 {
		t.Fatalf("黑暗渡鸦等级应注入 Dark Raven Level=12，got %v", got)
	}
}

// TestPetDarkHorseLevelOptionScaling 锁定黑暗之马等级选项：Base Defense 随
// 马等级线性 +2/级（opt#2 = 5 + HorseLevel*2 + TotalAgility*0.05），跨等级差恰为 20。
func TestPetDarkHorseLevelOptionScaling(t *testing.T) {
	cfg, c := petChar(16)
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 4, Level: 0, Durability: 255})
	def0 := AttributeValue(cfg, c, "Base Defense")
	// 换 +10 马（先卸后装：Remove 再 AddToSlot）。
	c.Inventory.Remove(c.Inventory.GetItem(item.SlotPet))
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 4, Level: 10, Durability: 255})
	def10 := AttributeValue(cfg, c, "Base Defense")
	if d := def10 - def0; math.Abs(d-20) > 1e-3 {
		t.Fatalf("黑暗之马 +10 级应给 Base Defense +20（10×2），got %v", d)
	}
}

// TestPetFenrirOptions 锁定 Fenrir 三色选项与移速组合奖励（对照 AddFenrirOptions +
// AddFenrirMovementSpeedCombinationBonus）：
//   - 裸 Fenrir（无位域）→ Movement Speed=17（基础 Maximum）；
//   - 黑 Fenrir(bit1) → 移速组合奖励升到 19，且 Attack Damage Increase Multiplier ×1.1；
//   - 蓝 Fenrir(bit2) → Damage Receive Multiplier ×0.9。
func TestPetFenrirOptions(t *testing.T) {
	cfg, c := petChar(4) // Dark Knight 也可装备 Fenrir（qual 含 4）
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 37, Durability: 255})
	if got := AttributeValue(cfg, c, "Movement Speed"); got != 17 {
		t.Fatalf("裸 Fenrir 移速应为 17，got %v", got)
	}

	// 黑 Fenrir。
	c.Inventory.Remove(c.Inventory.GetItem(item.SlotPet))
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 37, FenrirBits: 1, Durability: 255})
	if got := AttributeValue(cfg, c, "Movement Speed"); got != 19 {
		t.Fatalf("黑 Fenrir 移速组合奖励应为 19，got %v", got)
	}
	if got := AttributeValue(cfg, c, "Attack Damage Increase Multiplier"); math.Abs(got-1.1) > 1e-3 {
		t.Fatalf("黑 Fenrir 增伤倍率应为 1.1，got %v", got)
	}

	// 蓝 Fenrir。
	c.Inventory.Remove(c.Inventory.GetItem(item.SlotPet))
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 37, FenrirBits: 2, Durability: 255})
	if got := AttributeValue(cfg, c, "Damage Receive Multiplier"); math.Abs(got-0.9) > 1e-3 {
		t.Fatalf("蓝 Fenrir 减伤倍率应为 0.9，got %v", got)
	}
}

// TestPetGuardianAngelBase 锁定守护天使基础效果（对照 CreatePet(0)）：受伤减免 ×0.8、
// Maximum Health +50。
func TestPetGuardianAngelBase(t *testing.T) {
	cfg, c := petChar(4)
	baseHP := AttributeValue(cfg, c, "Maximum Health")
	equip(t, c, item.SlotPet, &item.Item{Group: 13, Number: 0, Durability: 255})
	if got := AttributeValue(cfg, c, "Damage Receive Multiplier"); math.Abs(got-0.8) > 1e-3 {
		t.Fatalf("守护天使减伤倍率应为 0.8，got %v", got)
	}
	if d := AttributeValue(cfg, c, "Maximum Health") - baseHP; d != 50 {
		t.Fatalf("守护天使应给 Maximum Health +50，got %v", d)
	}
}

// TestResolveRavenCombatValues 锁定渡鸦攻击属性由 "Dark Raven Level" 经职业关系图派生：
// min/max 伤害随等级 +15/级（不受统率影响的部分），跨等级差恰为 150。
func TestResolveRavenCombatValues(t *testing.T) {
	cfg, c := petChar(16)
	equip(t, c, item.SlotRightHand, &item.Item{Group: 13, Number: 5, Level: 10, Durability: 255})
	rav10, err := ResolveRavenCombatValues(cfg, c)
	if err != nil {
		t.Fatalf("渡鸦属性解析失败: %v", err)
	}
	c.Inventory.Remove(c.Inventory.GetItem(item.SlotRightHand))
	equip(t, c, item.SlotRightHand, &item.Item{Group: 13, Number: 5, Level: 20, Durability: 255})
	rav20, err := ResolveRavenCombatValues(cfg, c)
	if err != nil {
		t.Fatalf("渡鸦属性解析失败: %v", err)
	}
	if rav20.MinDamage-rav10.MinDamage != 150 || rav20.MaxDamage-rav10.MaxDamage != 150 {
		t.Fatalf("渡鸦 +10 级 min/max 伤害各应 +150，got minΔ=%d maxΔ=%d",
			rav20.MinDamage-rav10.MinDamage, rav20.MaxDamage-rav10.MaxDamage)
	}
	if rav10.MinDamage <= 0 {
		t.Fatalf("10 级渡鸦最小伤害应为正，got %d", rav10.MinDamage)
	}
}
