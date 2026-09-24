package action

// npcshop.go —— T2-9 NPC 商店（原版 GameLogic/ItemPriceCalculator.cs +
// PlayerActions/Items/BuyNpcItemAction.cs / SellItemToNpcAction.cs）。
//
// 本文件只做**纯决策**：价格计算（买/卖）、买/卖两类的可行性判定与金额/容器增量。
// 出站包、会话/商店上下文由 gameserver 装配（handler_npc.go），与 T2-5 的
// DecideMove/Apply* 分层一致。

// ShopItemDef 是商店计算所需的物品定义投影（原版 ItemDefinition 的价格相关字段；
// 由调用方从 config.GameConfig.Item 投影，action 包不依赖 config）。
type ShopItemDef struct {
	Group              int
	Number             int
	Width              int
	Height             int
	DropLevel          int
	MaximumItemLevel   int
	Durability         int
	Value              int
	HasSkill           bool
	IsWearable         bool // 原 definition.ItemSlot 非空（Slots 非空）
	IsBoundToCharacter bool
}

// shopSpecialID 对照原版 ItemPriceCalculator.GetId(group, id) = (id << 8) + group。
func shopSpecialID(group, number int) int { return number<<8 + group }

// shopSpecialItems 对照原版 SpecialItemDictionary 里商店/卖店会触碰到的条目。
// f 入参为 (itemLevel, durability, 定义耐久)。键 = GetId。
var shopSpecialItems = map[int]func(itemLevel int, durability int, defDurability int) int{
	0x0704: func(l int, d int, dd int) int { // Bolt (4,7)
		base := map[int]int{1: 1400, 2: 2200, 3: 3000}[l]
		if base == 0 {
			base = 100
		}
		if d > 0 {
			return base * d / dd // 原版 baseprice×Durability/definition.Durability
		}
		return 0
	},
	0x0F04: func(l int, d int, dd int) int { // Arrow (4,15)
		base := map[int]int{1: 1200, 2: 2000, 3: 2800}[l]
		if base == 0 {
			base = 70
		}
		if d > 0 {
			return base * d / dd
		}
		return 0
	},
	0x0D0E: func(_ int, _ int, _ int) int { return 9000000 },  // Bless (14,13)
	0x0E0E: func(_ int, _ int, _ int) int { return 6000000 },  // Soul (14,14)
	0x0F0C: func(_ int, _ int, _ int) int { return 810000 },   // Chaos (12,15)
	0x100E: func(_ int, _ int, _ int) int { return 45000000 }, // Life (14,16)
	0x160E: func(_ int, _ int, _ int) int { return 36000000 }, // Creation (14,22)
	0x1F0E: func(_ int, _ int, _ int) int { return 60000000 }, // Guardian (14,31)
	0x090E: func(_ int, _ int, _ int) int { return 750 },      // Ale (14,9)
	0x1D0D: func(_ int, _ int, _ int) int { return 5000 },     // Armor of Guardsman (13,29)
	0x100D: func(l int, _ int, _ int) int { // Scroll of Archangel (13,16)
		return map[int]int{1: 10000, 2: 50000, 3: 100000, 4: 300000, 5: 500000, 6: 800000, 7: 1000000, 8: 1200000}[l]
	},
	0x110D: func(l int, _ int, _ int) int { // Blood Bone (13,17)
		return map[int]int{1: 10000, 2: 50000, 3: 100000, 4: 300000, 5: 500000, 6: 800000, 7: 1000000, 8: 1200000}[l]
	},
	0x110E: func(l int, _ int, _ int) int { // Devil Eye (14,17)
		return map[int]int{1: 10000, 2: 50000, 3: 100000, 4: 300000, 5: 500000, 6: 800000, 7: 1000000}[l]
	},
	0x120E: func(l int, _ int, _ int) int { // Devil Key (14,18)
		return map[int]int{1: 15000, 2: 75000, 3: 150000, 4: 450000, 5: 750000, 6: 1200000, 7: 1500000}[l]
	},
	0x130E: func(l int, _ int, _ int) int { // Devil Invitation (14,19)
		switch l {
		case 1:
			return 60000
		case 2:
			return 84000
		default:
			return (l - 1) * 60000
		}
	},
	0x310D: func(l int, _ int, _ int) int { // Old Scroll (13,49)
		if l == 1 {
			return 500000
		}
		return (l + 1) * 200000
	},
	0x320D: func(l int, _ int, _ int) int { // Illusion Sorcerer Covenant (13,50)
		if l == 1 {
			return 500000
		}
		return (l + 1) * 200000
	},
	0x140D: func(l int, _ int, _ int) int { // Wizard's Ring (13,20)
		if l == 0 {
			return 30000
		}
		return 0
	},
}

// shopDefaultSpecial 让缺省条目命中后返回 0（未登记的等级按 0 价，原版同形）。
func shopSpecialPrice(group, number, itemLevel, durability, defDurability int) (int, bool) {
	if f, ok := shopSpecialItems[shopSpecialID(group, number)]; ok {
		return f(itemLevel, durability, defDurability), true
	}
	return 0, false
}

// additionalDurabilityPerLevel 对照原版 ItemExtensions.AdditionalDurabilityPerLevel。
var additionalDurabilityPerLevel = [16]int{0, 1, 2, 3, 4, 6, 8, 10, 12, 14, 17, 21, 26, 32, 39, 47}

// shopDropLevelIncreaseByLevel 对照原版 ItemPriceCalculator.DropLevelIncreaseByLevel。
var shopDropLevelIncreaseByLevel = map[int]int{5: 4, 6: 10, 7: 25, 8: 45, 9: 65, 10: 95, 11: 135, 12: 185, 13: 245, 14: 305, 15: 365}

// shopMaxPrice 对照原版 MaximumPrice = 3_000_000_000。
const shopMaxPrice = 3_000_000_000

// shopRoundPrice 对照原版 RoundPrice。
func shopRoundPrice(price int) int {
	switch {
	case price >= 1000:
		return price / 100 * 100
	case price >= 100:
		return price / 10 * 10
	default:
		return price
	}
}

// isWingDef 对照原版 ItemDefinition.IsWing()。
func isWingDef(def *ShopItemDef) bool {
	return (def.Group == 12 || def.Group == 13) &&
		def.Width >= 2 && def.Height >= 2 && def.MaximumItemLevel >= 11 && def.Durability >= 200 &&
		def.IsWearable
}

// IsTrainablePet 对照原版 ItemDefinition.IsTrainablePet()（PetExperienceFormula 非空）。
// 导出件不含该字段；S6 初始化里只有 Dark Horse (13,4) 与 Dark Raven (13,5)。
func IsTrainablePet(group, number int) bool {
	return group == 13 && (number == 4 || number == 5)
}

// MaximumDurabilityOfOnePiece 对照原版 ItemExtensions.GetMaximumDurabilityOfOnePiece：
// 不可穿戴 = 1（耐久即堆叠数）；训练宠 = 255；否则定义耐久 + 等级加成
// （远古 +20 / 卓越 +15）。isAncient/isExcellent 为物品实例态。
func MaximumDurabilityOfOnePiece(def *ShopItemDef, itemLevel byte, isAncient, isExcellent bool) int {
	if !def.IsWearable {
		return 1
	}
	if IsTrainablePet(def.Group, def.Number) {
		return 255
	}
	add := 0
	if int(itemLevel) < len(additionalDurabilityPerLevel) {
		add = additionalDurabilityPerLevel[itemLevel]
	}
	result := def.Durability + add
	if isAncient {
		result += 20
	} else if isExcellent {
		result += 15
	}
	if result > 255 {
		result = 255
	}
	return result
}

// CalculateBuyingPrice 对照原版 ItemPriceCalculator.CalculateBuyingPrice（Go 子集）。
// itemOptions 只投影出本仓商店/实体模型携带的三类：幸运、普通选项等级、卓越/守护标志。
func CalculateBuyingPrice(def *ShopItemDef, itemLevel, durability byte, hasSkill, luck bool, optionLevel int, excellent, guardian bool) int {
	if def == nil {
		return 0
	}
	group, number := def.Group, def.Number

	// 原版首分支：卷轴(15)/宝石(12) 且定义有价 → 直接定义价。
	if def.Value > 0 && (group == 15 || group == 12) {
		return def.Value
	}

	if IsTrainablePet(group, number) {
		// Dark Raven (13,5) ×1,000,000/级；其余（Dark Horse）×2,000,000/级。
		if group == 13 && number == 5 {
			return int(itemLevel) * 1_000_000
		}
		return int(itemLevel) * 2_000_000
	}

	price := 0
	dropLevel := def.DropLevel + int(itemLevel)*3
	if excellent {
		dropLevel += 25
	}

	if sp, ok := shopSpecialPrice(group, number, int(itemLevel), int(durability), def.Durability); ok {
		price = sp
	} else if def.Value > 0 {
		price += def.Value * def.Value * 10 / 12
		if group == 14 && number <= 8 {
			// 药水/解毒草：等级翻倍 + 取整到 10 位 + 按堆叠数倍乘（原版提前返回）。
			if itemLevel > 0 {
				mul := 1
				for i := 0; i < int(itemLevel); i++ {
					mul *= 2
				}
				price *= mul
			}
			price = price / 10 * 10
			price *= int(durability)
			return price
		}
	} else if (group == 12 && ((number > 6 && number < 36) || (number > 43 && number != 50))) ||
		group == 13 || group == 15 {
		// 披风/戒指/项链/杂货（定义无价）：dropLevel³ + 100。
		price = dropLevel*dropLevel*dropLevel + 100
		// HealthRecoveryMultiplier 选项加成：本仓实体模型未携带选项目标属性，
		// 商店与掉落物均未触及（差异登记 doc/15 T2-9）。
	} else {
		if inc, ok := shopDropLevelIncreaseByLevel[int(itemLevel)]; ok {
			dropLevel += inc
		}
		if isWingDef(def) {
			price = ((dropLevel+40)*dropLevel*dropLevel*11 + 40000000)
		} else {
			price = ((dropLevel+40)*dropLevel*dropLevel)/8 + 100
		}
		isOneHandedWeapon := group < 6 && def.Width < 2
		isShield := group == 6
		if isOneHandedWeapon || isShield {
			price = price * 80 / 100
		}
		// WorthlessSkills（66/223/224/225）需要"定义→技能号"链，导出件未含；
		// 商店武器均不在该名单（差异登记 doc/15 T2-9）。
		if hasSkill {
			price += price * 3 / 2
		}
		if luck {
			price += price * 25 / 100
		}
		switch {
		case optionLevel == 0:
		case optionLevel == 1:
			price += int(float64(price) * 0.6)
		default:
			pow := 1
			for i := 1; i < optionLevel; i++ {
				pow *= 2
			}
			price += int(float64(price) * 0.7 * float64(pow))
		}
		// 卓越选项翻倍：模型里卓越是标志位（无逐条卓越），按单条翻倍。
		if excellent {
			price += price
		}
	}

	if guardian {
		price += price * 16 / 100
	}
	if price > shopMaxPrice {
		price = shopMaxPrice
	}
	return price
}

// CalculateFinalBuyingPrice 对照原版 CalculateFinalBuyingPrice。
func CalculateFinalBuyingPrice(def *ShopItemDef, itemLevel, durability byte, hasSkill, luck bool, optionLevel int, excellent, guardian bool) int {
	return shopRoundPrice(CalculateBuyingPrice(def, itemLevel, durability, hasSkill, luck, optionLevel, excellent, guardian))
}

// CalculateSellingPrice 对照原版 CalculateSellingPrice(item, durability)：
// 买价 / 3；药水(14, ≤8)取整到 10 位；其余按耐久损耗折减（非训练宠）。
func CalculateSellingPrice(def *ShopItemDef, itemLevel, durability byte, hasSkill, luck bool, optionLevel int, excellent, guardian bool) int {
	sellingPrice := CalculateBuyingPrice(def, itemLevel, durability, hasSkill, luck, optionLevel, excellent, guardian) / 3
	if def.Group == 14 && def.Number <= 8 {
		return sellingPrice / 10 * 10
	}
	if !IsTrainablePet(def.Group, def.Number) {
		maxDurability := MaximumDurabilityOfOnePiece(def, itemLevel, false, excellent)
		if maxDurability > 1 && maxDurability > int(durability) {
			multiplier := 1.0 - float64(durability)/float64(maxDurability)
			loss := int(float64(sellingPrice) * 0.6 * multiplier)
			sellingPrice -= loss
		}
	}
	return shopRoundPrice(sellingPrice)
}

// ShopOutcome 是商店交互结果（原版各失败分支 → 客户端表现）。
type ShopOutcome byte

const (
	// ShopBought 买入成功（原版 INpcItemBoughtPlugIn + IUpdateMoneyPlugIn）。
	ShopBought ShopOutcome = iota
	// ShopBoughtStacked 买入成功且完全堆叠到既有物品（原版同分支还回一帧
	// NpcItemBuyFailed 让客户端结束手势——照抄）。
	ShopBoughtStacked
	// ShopBuyNoMoney 金额不足（原版 NotEnoughMoney 蓝字 + BuyNpcItemFailed）。
	ShopBuyNoMoney
	// ShopBuyInventoryFull 背包放不下（原版 InventoryFull 蓝字 + BuyNpcItemFailed）。
	ShopBuyInventoryFull
	// ShopBuyUnknownItem 商店里没有该槽位的商品（原版 ItemUnknown + 失败包）。
	ShopBuyUnknownItem
	// ShopBuyNoStore 没有打开的商店（原版 BuyNpcItemFailed）。
	ShopBuyNoStore
	// ShopSold 卖出成功（原版 NpcItemSellResult success + 金币）。
	ShopSold
	// ShopSellRejected 卖出被拒（原版 NpcItemSellResult fail；含无商店/无物品/绑定）。
	ShopSellRejected
	// ShopSellMoneyOverflow 卖出所得放不下（金币超上限，原版 TryAddMoney false）。
	ShopSellMoneyOverflow
)

// ShopItem 是商店里的一件商品（原版 MerchantStore.Items 的元素投影）。
type ShopItem struct {
	Slot        byte
	Def         *ShopItemDef
	Level       byte
	Durability  byte
	HasSkill    bool
	Luck        bool
	OptionLevel int
	Excellent   bool
	Guardian    bool
}

// IsStackable 对照原版 ItemExtensions.IsStackable：不可穿戴且定义耐久 > 1。
func (s *ShopItem) IsStackable() bool {
	return s.Def != nil && !s.Def.IsWearable && s.Def.Durability > 1
}

// BuyingPrice 商品买入价（CalculateFinalBuyingPrice）。
func (s *ShopItem) BuyingPrice() int {
	return CalculateFinalBuyingPrice(s.Def, s.Level, s.Durability, s.HasSkill, s.Luck, s.OptionLevel, s.Excellent, s.Guardian)
}

// BuyDecision 按 BuyNpcItemAction.BuyItemAsync 的顺序做一次买入判定：
// 无商店 → 找商品 → 堆叠分支 → 背包空间 → 金币。纯判定：扣款由调用方
// 按 Result.Price 落账（Outcome 为 ShopBought/ShopBoughtStacked 时）。
//
//	stackTarget: 可完全堆叠的目标槽回调（无则 ok=false，原版 CanCompletelyStackOn）。
//	invFree:     CheckInvSpace 等价回调（w×h 物品在背包是否有落位）。
func BuyDecision(storeOpen bool, items []ShopItem, money uint32, slot byte,
	stackTarget func(*ShopItem) (byte, bool), invFree func(w, h byte) bool) BuyResult {
	if !storeOpen {
		return BuyResult{Outcome: ShopBuyNoStore}
	}
	var storeItem *ShopItem
	for i := range items {
		if items[i].Slot == slot {
			storeItem = &items[i]
			break
		}
	}
	if storeItem == nil || storeItem.Def == nil {
		return BuyResult{Outcome: ShopBuyUnknownItem}
	}

	// 堆叠分支：可堆叠且背包有同物可完全堆上 → 目标耐久 += 商品耐久（调用方落袋）。
	if stackTarget != nil && storeItem.IsStackable() {
		if targetSlot, ok := stackTarget(storeItem); ok {
			return BuyResult{Outcome: ShopBoughtStacked, Price: uint32(storeItem.BuyingPrice()), StackTarget: targetSlot}
		}
	}

	// 非堆叠：先查背包空间（原版 CheckInvSpace → InventoryFull）。
	w, h := byte(1), byte(1)
	if storeItem.Def.Width > 0 {
		w = byte(storeItem.Def.Width)
	}
	if storeItem.Def.Height > 0 {
		h = byte(storeItem.Def.Height)
	}
	if invFree != nil && !invFree(w, h) {
		return BuyResult{Outcome: ShopBuyInventoryFull}
	}

	// 金币：原版空间检查在前（先 InventoryFull 后 NotEnoughMoney，顺序照抄）。
	price := uint32(storeItem.BuyingPrice())
	if money < price {
		return BuyResult{Outcome: ShopBuyNoMoney}
	}
	return BuyResult{Outcome: ShopBought, Price: price, NewItem: storeItem}
}

// BuyResult 是 BuyDecision 的产出（handler 直接消费）。
type BuyResult struct {
	Outcome     ShopOutcome
	Price       uint32
	StackTarget byte      // Outcome == ShopBoughtStacked 时有效
	NewItem     *ShopItem // Outcome == ShopBought 时有效（商品投影，供入包）
}

// SellDecision 按 SellItemToNpcAction.SellItemAsync 的顺序做一次卖出判定：
// 无商店 → 无物品 → 绑定物品 → 金额上限。返回结果、卖价与入账后的金额。
func SellDecision(storeOpen bool, money uint32, item *ShopItem) (ShopOutcome, int, uint32) {
	if !storeOpen || item == nil || item.Def == nil {
		return ShopSellRejected, 0, money
	}
	// 原版：IsBoundToCharacter && (Durability == 0 || item.Durability > 0) → 拒。
	if item.Def.IsBoundToCharacter && (item.Def.Durability == 0 || item.Durability > 0) {
		return ShopSellRejected, 0, money
	}
	sellingPrice := CalculateSellingPrice(item.Def, item.Level, item.Durability, item.HasSkill, item.Luck, item.OptionLevel, item.Excellent, item.Guardian)
	// 原版 TryAddMoney：Money + price > MaximumInventoryMoney → 拒（金币放不下）。
	if uint64(money)+uint64(sellingPrice) > MaxInventoryMoney {
		return ShopSellMoneyOverflow, sellingPrice, money
	}
	return ShopSold, sellingPrice, money + uint32(sellingPrice)
}

// MaxInventoryMoney 对照原版 GameConfiguration.MaximumInventoryMoney
// （GameConfigurationInitializerBase 固定 int.MaxValue）——金币上限。
const MaxInventoryMoney = 2147483647
