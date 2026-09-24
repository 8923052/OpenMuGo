package action

// npcshop_test.go —— T2-9 价格黄金对拍 + 买/卖决策分支。
// 黄金值由 C# 公式（ItemPriceCalculator.cs + SpecialItemDictionary）独立脚本
// 按导出件数据逐条算出，锁死回归。

import (
	"testing"
)

// goldenBuying 是从原版 C# 公式独立对拍出的黄金值
// （数据源：data/season6/40_items.json + 85_merchant_stores.json 实际条目）。
func goldenBuying() []struct {
	name                string
	def                 ShopItemDef
	level               byte
	durability          byte
	hasSkill, luck      bool
	optionLevel         int
	excellent, guardian bool
	wantBuying          int // CalculateBuyingPrice
	wantFinal           int // CalculateFinalBuyingPrice
	wantSelling         int // CalculateSellingPrice
} {
	return []struct {
		name                string
		def                 ShopItemDef
		level               byte
		durability          byte
		hasSkill, luck      bool
		optionLevel         int
		excellent, guardian bool
		wantBuying          int
		wantFinal           int
		wantSelling         int
	}{
		// Apple (14,0)：value=5 → 5*5*10/12=20；药水不折损但取整 10 位 → 卖 0。
		{"apple", ShopItemDef{Group: 14, Number: 0, Value: 5, DropLevel: 1, Durability: 1, Width: 1, Height: 1}, 0, 1, false, false, 0, false, false, 20, 20, 0},
		// Apple +1：药水等级翻倍 → 40。
		{"apple+1", ShopItemDef{Group: 14, Number: 0, Value: 5, DropLevel: 1, Durability: 1, Width: 1, Height: 1}, 1, 1, false, false, 0, false, false, 40, 40, 10},
		// Apple ×3（durability 即堆叠数）：20*3=60。
		{"apple_x3", ShopItemDef{Group: 14, Number: 0, Value: 5, DropLevel: 1, Durability: 1, Width: 1, Height: 1}, 0, 3, false, false, 0, false, false, 60, 60, 20},
		// Town Portal Scroll (14,10)：value=30 → 30*30*10/12=750；非药水（n>8）。
		{"townportal", ShopItemDef{Group: 14, Number: 10, Value: 30, DropLevel: 30, Durability: 1, Width: 1, Height: 1}, 0, 1, false, false, 0, false, false, 750, 750, 250},
		// Bolt (4,7)：特殊表 100*255/255=100。
		{"bolt", ShopItemDef{Group: 4, Number: 7, DropLevel: 0, Durability: 255, Width: 1, Height: 2}, 0, 255, false, false, 0, false, false, 100, 100, 33},
		// Arrow (4,15) +2：特殊表 2000*190/255=1490。
		{"arrow+2", ShopItemDef{Group: 4, Number: 15, DropLevel: 0, Durability: 255, Width: 1, Height: 2}, 2, 190, false, false, 0, false, false, 1490, 1400, 490},
		// Ale (14,9)：特殊表 750。
		{"ale", ShopItemDef{Group: 14, Number: 9, Value: 30, DropLevel: 15, Durability: 1, Width: 1, Height: 1}, 0, 1, false, false, 0, false, false, 750, 750, 250},
		// Armor of Guardsman (13,29)：特殊表 5000。
		{"guardsman", ShopItemDef{Group: 13, Number: 29, DropLevel: 0, Durability: 1, Width: 2, Height: 2}, 0, 1, false, false, 0, false, false, 5000, 5000, 1600},
		// Bless (14,13)：特殊表 9_000_000。
		{"bless", ShopItemDef{Group: 14, Number: 13, DropLevel: 0, Durability: 1, Width: 1, Height: 1}, 0, 1, false, false, 0, false, false, 9000000, 9000000, 3000000},
		// Double Poleaxe (3,5) +3+L+4（双手 w=2 无 8 折）：
		// dl=13+9=22 → ((22+40)*484)/8+100=3851 → luck+25%=4813 → opt1+60%=7700。
		{"poleaxe", ShopItemDef{Group: 3, Number: 5, DropLevel: 13, Durability: 38, Width: 2, Height: 3, MaximumItemLevel: 15, IsWearable: true}, 3, 38, false, true, 1, false, false, 7700, 7700, 2400},
		// Berdysh (3,7) +3+skill+L+4：
		// dl=37+9=46 → (86*2116)/8+100=22847 → skill+150%=57117 → luck+25%=71396 → opt1+60%=114233。
		{"berdysh", ShopItemDef{Group: 3, Number: 7, DropLevel: 37, Durability: 54, Width: 2, Height: 4, MaximumItemLevel: 15, IsWearable: true}, 3, 54, true, true, 1, false, false, 114233, 114200, 36800},
		// Kris (0,0) +0+L+4（单手 8 折）：
		// dl=4 → (44*16)/8+100=188 → 8折=150 → luck=187 → opt1=299。
		{"kris", ShopItemDef{Group: 0, Number: 0, DropLevel: 4, Durability: 20, Width: 1, Height: 2, MaximumItemLevel: 15, IsWearable: true}, 0, 20, false, true, 1, false, false, 299, 290, 99},
		// Small Shield (6,1) +0（盾 8 折）：dl=6 → (46*36)/8+100=307 → 8折=245。
		{"smallshield", ShopItemDef{Group: 6, Number: 1, DropLevel: 6, Durability: 22, Width: 2, Height: 2, MaximumItemLevel: 15, IsWearable: true}, 0, 22, false, false, 0, false, false, 245, 240, 81},
		// Wings of Fairy (12,4) +15：dl2=58+365=423 → 翼公式 951_285_397。
		{"wing-fairy", ShopItemDef{Group: 12, Number: 4, DropLevel: 13, Durability: 200, Width: 2, Height: 2, MaximumItemLevel: 15, IsWearable: true}, 15, 200, false, false, 0, false, false, 951285397, 951285300, 280892300},
		// Dark Horse (13,4) +3：训练宠 ×2_000_000。
		{"darkhorse", ShopItemDef{Group: 13, Number: 4, DropLevel: 0, Durability: 100, Width: 2, Height: 2, IsWearable: true}, 3, 50, false, false, 0, false, false, 6000000, 6000000, 2000000},
		// Dark Raven (13,5) +3：×1_000_000。
		{"darkraven", ShopItemDef{Group: 13, Number: 5, DropLevel: 0, Durability: 100, Width: 2, Height: 2, IsWearable: true}, 3, 50, false, false, 0, false, false, 3000000, 3000000, 1000000},
	}
}

func TestCalculatePricesGolden(t *testing.T) {
	for _, g := range goldenBuying() {
		def := g.def
		got := CalculateBuyingPrice(&def, g.level, g.durability, g.hasSkill, g.luck, g.optionLevel, g.excellent, g.guardian)
		if got != g.wantBuying {
			t.Errorf("%s: CalculateBuyingPrice = %d, want %d", g.name, got, g.wantBuying)
		}
		if fin := CalculateFinalBuyingPrice(&def, g.level, g.durability, g.hasSkill, g.luck, g.optionLevel, g.excellent, g.guardian); fin != g.wantFinal {
			t.Errorf("%s: CalculateFinalBuyingPrice = %d, want %d", g.name, fin, g.wantFinal)
		}
		if sell := CalculateSellingPrice(&def, g.level, g.durability, g.hasSkill, g.luck, g.optionLevel, g.excellent, g.guardian); sell != g.wantSelling {
			t.Errorf("%s: CalculateSellingPrice = %d, want %d", g.name, sell, g.wantSelling)
		}
	}
}

// TestCalculateSellingPriceDurabilityLoss 锁耐久折减：半耐久 Kriss 卖价显著低于满耐久。
// 无幸运/选项的 Kriss：买价 150 → 满 50；半耐久 loss=int(50*0.6*0.5)=15 → 35。
func TestCalculateSellingPriceDurabilityLoss(t *testing.T) {
	def := ShopItemDef{Group: 0, Number: 0, DropLevel: 4, Durability: 20, Width: 1, Height: 2, MaximumItemLevel: 15, IsWearable: true}
	full := CalculateSellingPrice(&def, 0, 20, false, false, 0, false, false)
	half := CalculateSellingPrice(&def, 0, 10, false, false, 0, false, false)
	if full != 50 || half != 35 {
		t.Fatalf("full=%d half=%d, want 50/35", full, half)
	}
	if full-half != int(float64(full)*0.6*0.5) {
		t.Fatalf("折减量不符原版 0.6×mult: full=%d half=%d", full, half)
	}
}

func TestMaximumDurabilityOfOnePiece(t *testing.T) {
	wearable := &ShopItemDef{Group: 0, Number: 0, Durability: 20, IsWearable: true}
	if got := MaximumDurabilityOfOnePiece(wearable, 0, false, false); got != 20 {
		t.Fatalf("lv0 = %d, want 20", got)
	}
	if got := MaximumDurabilityOfOnePiece(wearable, 3, false, false); got != 23 { // +3
		t.Fatalf("lv3 = %d, want 23", got)
	}
	if got := MaximumDurabilityOfOnePiece(wearable, 0, false, true); got != 35 { // +15 卓越
		t.Fatalf("excellent = %d, want 35", got)
	}
	nonWearable := &ShopItemDef{Group: 14, Number: 0, Durability: 1}
	if got := MaximumDurabilityOfOnePiece(nonWearable, 0, false, false); got != 1 {
		t.Fatalf("不可穿戴 = %d, want 1", got)
	}
	pet := &ShopItemDef{Group: 13, Number: 4, Durability: 100, IsWearable: true}
	if got := MaximumDurabilityOfOnePiece(pet, 0, false, false); got != 255 {
		t.Fatalf("训练宠 = %d, want 255", got)
	}
}

func TestShopRoundPrice(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, {99, 99}, {100, 100}, {123, 120}, {999, 990},
		{1000, 1000}, {1234, 1200}, {9999, 9900},
	}
	for _, c := range cases {
		if got := shopRoundPrice(c.in); got != c.want {
			t.Errorf("shopRoundPrice(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBuyDecisionBranches(t *testing.T) {
	boltDef := &ShopItemDef{Group: 4, Number: 7, Durability: 255, Height: 2} // 可堆叠（不可穿戴 & 定义耐久>1）
	appleDef := &ShopItemDef{Group: 14, Number: 0, Value: 5, Durability: 1, Width: 1, Height: 1}
	store := []ShopItem{
		{Slot: 0, Def: appleDef, Level: 0, Durability: 1},
		{Slot: 10, Def: boltDef, Level: 0, Durability: 100},
	}

	t.Run("无商店", func(t *testing.T) {
		res := BuyDecision(false, store, 1000, 0, nil, nil)
		if res.Outcome != ShopBuyNoStore {
			t.Fatalf("outcome = %d, want ShopBuyNoStore", res.Outcome)
		}
	})
	t.Run("未知槽位", func(t *testing.T) {
		res := BuyDecision(true, store, 1000, 99, nil, nil)
		if res.Outcome != ShopBuyUnknownItem {
			t.Fatalf("outcome = %d, want ShopBuyUnknownItem", res.Outcome)
		}
	})
	t.Run("金币不足", func(t *testing.T) {
		res := BuyDecision(true, store, 19, 0, nil, func(w, h byte) bool { return true })
		if res.Outcome != ShopBuyNoMoney {
			t.Fatalf("outcome = %d, want ShopBuyNoMoney", res.Outcome)
		}
	})
	t.Run("背包满", func(t *testing.T) {
		// 空间检查在金币之前（原版顺序）——即便金币不足也先报背包满。
		res := BuyDecision(true, store, 0, 0, nil, func(w, h byte) bool { return false })
		if res.Outcome != ShopBuyInventoryFull {
			t.Fatalf("outcome = %d, want ShopBuyInventoryFull", res.Outcome)
		}
	})
	t.Run("普通买入", func(t *testing.T) {
		res := BuyDecision(true, store, 1000, 0, nil, func(w, h byte) bool { return true })
		if res.Outcome != ShopBought || res.Price != 20 || res.NewItem == nil {
			t.Fatalf("outcome=%d price=%d item=%v", res.Outcome, res.Price, res.NewItem)
		}
	})
	t.Run("堆叠买入", func(t *testing.T) {
		res := BuyDecision(true, store, 1000, 10,
			func(*ShopItem) (byte, bool) { return 12, true },
			func(w, h byte) bool { return true })
		if res.Outcome != ShopBoughtStacked || res.StackTarget != 12 || res.Price != 39 {
			t.Fatalf("outcome=%d stack=%d price=%d", res.Outcome, res.StackTarget, res.Price)
		}
	})
	t.Run("不可堆叠无目标", func(t *testing.T) {
		// 苹果（可穿戴判定外：IsWearable=false 但定义耐久=1 → 不可堆叠）走普通买入。
		res := BuyDecision(true, store, 1000, 0,
			func(*ShopItem) (byte, bool) { return 5, true },
			func(w, h byte) bool { return true })
		if res.Outcome != ShopBought {
			t.Fatalf("outcome = %d, want ShopBought（耐久=1 不可堆叠）", res.Outcome)
		}
	})
}

func TestSellDecisionBranches(t *testing.T) {
	appleDef := &ShopItemDef{Group: 14, Number: 0, Value: 5, Durability: 1}
	t.Run("无商店", func(t *testing.T) {
		outcome, _, money := SellDecision(false, 100, &ShopItem{Def: appleDef})
		if outcome != ShopSellRejected || money != 100 {
			t.Fatalf("outcome=%d money=%d", outcome, money)
		}
	})
	t.Run("无物品", func(t *testing.T) {
		if outcome, _, _ := SellDecision(true, 100, nil); outcome != ShopSellRejected {
			t.Fatalf("outcome = %d", outcome)
		}
	})
	t.Run("绑定物品拒卖", func(t *testing.T) {
		bound := &ShopItemDef{Group: 13, Number: 29, Durability: 1, IsBoundToCharacter: true}
		// 原版分支：IsBound && (Durability == 0 || item.Durability > 0)——实例耐久 >0 命中。
		if outcome, _, _ := SellDecision(true, 100, &ShopItem{Def: bound, Durability: 1}); outcome != ShopSellRejected {
			t.Fatalf("outcome = %d, want ShopSellRejected", outcome)
		}
	})
	t.Run("金币上限", func(t *testing.T) {
		jewel := &ShopItemDef{Group: 14, Number: 13, Durability: 1}
		outcome, _, money := SellDecision(true, MaxInventoryMoney, &ShopItem{Def: jewel})
		if outcome != ShopSellMoneyOverflow || money != MaxInventoryMoney {
			t.Fatalf("outcome=%d money=%d", outcome, money)
		}
	})
	t.Run("成功卖出", func(t *testing.T) {
		ale := &ShopItemDef{Group: 14, Number: 9, Value: 30, Durability: 1}
		outcome, price, money := SellDecision(true, 100, &ShopItem{Def: ale})
		if outcome != ShopSold || price != 250 || money != 350 {
			t.Fatalf("outcome=%d price=%d money=%d", outcome, price, money)
		}
	})
}
