// Package pricing 移植 OpenMU `GameLogic/ItemPriceCalculator` 与 `ItemExtensions.GetMaximumDurabilityOfOnePiece`
// 中"修理"所需的那部分（耐久上限 + 修理价）。纯逻辑，只依赖 item 领域模型与 config 物品定义，版本无关。
//
// 忠实度边界（登记在 doc/15）：CalculateBuyingPrice 的 SpecialItemDictionary（宝石/特殊物）
// 与翅膀/披风的特例定价未完整移植——这些物品要么不可修理（耐久≤1），要么极少走修理；
// 对常规可修理装备（武器/防具/宠物）的价格与原版逐式一致。
package pricing

import (
	"math"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

// additionalDurabilityPerLevel 对照 ItemExtensions.AdditionalDurabilityPerLevel（按物品等级 0..15）。
var additionalDurabilityPerLevel = [16]int{0, 1, 2, 3, 4, 6, 8, 10, 12, 14, 17, 21, 26, 32, 39, 47}

// dropLevelIncreaseByLevel 对照 ItemPriceCalculator.DropLevelIncreaseByLevel（等级 5..15）。
var dropLevelIncreaseByLevel = map[int]int{5: 4, 6: 10, 7: 25, 8: 45, 9: 65, 10: 95, 11: 135, 12: 185, 13: 245, 14: 305, 15: 365}

const (
	maximumPrice         = int64(3_000_000_000)
	maximumBasePrice     = int64(400_000_000)
	destroyedPetPenalty  = 2.0
	destroyedItemPenalty = 1.4
)

// skillWorthless 对照 ItemPriceCalculator.WorthlessSkills（这些技能不加价）。
var skillWorthless = map[int]bool{66: true, 223: true, 224: true, 225: true}

// MaximumDurability 对照 GetMaximumDurabilityOfOnePiece。
func MaximumDurability(def *config.Item, it *item.Item) byte {
	if !isWearable(def) {
		return 1
	}
	if def.IsTrainablePet() {
		return 255
	}
	level := int(it.Level)
	if level >= len(additionalDurabilityPerLevel) {
		level = len(additionalDurabilityPerLevel) - 1
	}
	result := def.Durability + additionalDurabilityPerLevel[level]
	switch {
	case it.AncientDiscriminator > 0:
		result += 20
	case it.ExcellentBits != 0:
		result += 15
	}
	if result > 255 {
		result = 255
	}
	return byte(result)
}

// FinalBuyingPrice 对照 CalculateFinalBuyingPrice = RoundPrice(BuyingPrice)：合成按旧买入价推成功率用。
func FinalBuyingPrice(def *config.Item, it *item.Item) int64 {
	return roundPrice(buyingPrice(def, it))
}

// specialItemOldValue 对照 SpecialItemOldValueDictionary：早期版本的宝石基准价，
// 混沌武器/一阶翅膀的成功率按它算（不是现价）。键 = (number<<8)+group。
var specialItemOldValue = map[int]int64{
	itemID(14, 13): 100_000, // Bless
	itemID(14, 14): 70_000,  // Soul
	itemID(12, 15): 40_000,  // Chaos
	itemID(14, 16): 450_000, // Life
	itemID(14, 22): 450_000, // Creation
}

func itemID(group, number int) int { return (number << 8) + group }

// FinalOldBuyingPrice 对照 CalculateFinalOldBuyingPrice：宝石走旧基准价表，其余同 FinalBuyingPrice。
// 原版注释：这是"混沌武器/一阶翅膀"成功率沿用的历史价，不可改用现价。
func FinalOldBuyingPrice(def *config.Item, it *item.Item) int64 {
	if v, ok := specialItemOldValue[itemID(def.Group, def.Number)]; ok {
		return roundPrice(v)
	}
	return FinalBuyingPrice(def, it)
}

// SellingPrice 对照 CalculateSellingPrice：买入价的三分之一，按损耗折减后取整。
// 药水类（group14 且 number≤8）再向下取整到 10 的倍数；可训练宠物不折耐久。
func SellingPrice(def *config.Item, it *item.Item) int64 {
	price := buyingPrice(def, it) / 3
	if def.Group == 14 && def.Number <= 8 {
		return price / 10 * 10
	}
	if !def.IsTrainablePet() {
		maxDur := int64(MaximumDurability(def, it))
		if maxDur > 1 && maxDur > int64(it.Durability) {
			multiplier := 1.0 - float64(it.Durability)/float64(maxDur)
			price -= int64(float64(price) * 0.6 * multiplier)
		}
	}
	return roundPrice(price)
}

// RepairPrice 对照 CalculateRepairPrice：npcDiscount = 通过 NPC 修理（原版 player.OpenedNpc != null）。
func RepairPrice(def *config.Item, it *item.Item, npcDiscount bool) int64 {
	maxDur := int(MaximumDurability(def, it))
	if maxDur == 0 {
		return 0
	}
	isPet := def.IsTrainablePet()
	divisor := int64(3)
	if isPet {
		divisor = 1
	}
	base := roundPrice(buyingPrice(def, it)) / divisor
	if base > maximumBasePrice {
		base = maximumBasePrice
	}
	base = roundPrice(base)

	sqrt1 := math.Sqrt(float64(base))
	sqrt2 := math.Sqrt(sqrt1)
	missing := 1 - float64(int(it.Durability))/float64(maxDur)
	price := 3.0*sqrt1*sqrt2*missing + 1.0
	if it.Durability == 0 {
		if isPet {
			price *= destroyedPetPenalty
		} else {
			price *= destroyedItemPenalty
		}
	}
	if !npcDiscount {
		price *= 2.5
	}
	return roundPrice(int64(price))
}

// buyingPrice 移植 CalculateBuyingPrice 的可修理装备路径（见包注释忠实度边界）。
func buyingPrice(def *config.Item, it *item.Item) int64 {
	if def.Value > 0 && (def.Group == 15 || def.Group == 12) {
		return int64(def.Value)
	}
	if def.IsTrainablePet() {
		if def.Group == 13 {
			return int64(it.Level) * 1_000_000
		}
		return int64(it.Level) * 2_000_000
	}

	dropLevel := def.DropLevel + int(it.Level)*3
	if it.ExcellentBits != 0 {
		dropLevel += 25
	}

	var price int64
	switch {
	case def.Value > 0:
		price += int64(def.Value) * int64(def.Value) * 10 / 12
		if def.Group == 14 && def.Number <= 8 {
			if it.Level > 0 {
				price *= int64(math.Pow(2, float64(it.Level)))
			}
			price = price / 10 * 10
			price *= int64(it.Durability)
			return clampMax(price)
		}
	case (def.Group == 12 && ((def.Number > 6 && def.Number < 36) || (def.Number > 43 && def.Number != 50))) ||
		def.Group == 13 || def.Group == 15:
		price = int64(dropLevel*dropLevel*dropLevel) + 100
		return clampMax(price)
	default:
		if inc, ok := dropLevelIncreaseByLevel[int(it.Level)]; ok {
			dropLevel += inc
		}
		if isWing(def) {
			price = int64((dropLevel+40)*dropLevel*dropLevel*11) + 40_000_000
		} else {
			price = int64((dropLevel+40)*dropLevel*dropLevel/8) + 100
		}
		isOneHanded := def.Group < 6 && def.Width < 2
		isShield := def.Group == 6
		if isOneHanded || isShield {
			price = price * 80 / 100
		}
		if it.HasSkill && !skillWorthless[skillNumber(def)] {
			price += int64(float64(price) * 1.5)
		}
		if it.Luck {
			price += price * 25 / 100
		}
		switch it.OptionLevel {
		case 0:
		case 1:
			price += int64(float64(price) * 0.6)
		default:
			price += int64(float64(price) * 0.7 * math.Pow(2, float64(it.OptionLevel-1)))
		}
		for i := 0; i < wingOptionCount(it); i++ {
			price += int64(float64(price) * 0.25)
		}
		for _, bit := range excellentBits(it.ExcellentBits) {
			_ = bit
			price += price // 每个卓越选项翻倍
		}
	}
	if it.GuardianOption {
		price += price * 16 / 100
	}
	return clampMax(price)
}

func isWearable(def *config.Item) bool { return def.Slot != nil || len(def.Slots) > 0 }

func isWing(def *config.Item) bool { return def.Group == 6 && def.Width >= 2 && def.Height >= 2 }

func skillNumber(def *config.Item) int {
	if def.SkillNumber != nil {
		return *def.SkillNumber
	}
	return 0
}

// wingOptionCount 以 WingOptionNumber>0 近似"翅膀备选选项"数量（忠实度边界内）。
func wingOptionCount(it *item.Item) int {
	if it.WingOptionNumber > 0 {
		return 1
	}
	return 0
}

// excellentBits 返回 ExcellentBits 置位的个数（对照逐条 Excellent 选项）。
func excellentBits(bits byte) []byte {
	var out []byte
	for i := 0; i < 6; i++ {
		if bits&(1<<uint(i)) != 0 {
			out = append(out, byte(i))
		}
	}
	return out
}

func clampMax(price int64) int64 {
	if price > maximumPrice {
		return maximumPrice
	}
	return price
}

func roundPrice(price int64) int64 {
	switch {
	case price >= 1000:
		return price / 100 * 100
	case price >= 100:
		return price / 10 * 10
	default:
		return price
	}
}
