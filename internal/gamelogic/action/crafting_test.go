package action

// crafting_test.go —— 合成引擎对拍：逐条钉住原版 BaseItemCraftingHandler / SimpleItemCraftingHandler
// 与各 handler 子类的判定与产出（含 (byte) 回绕、旧买入价基准、券的等级一致性与失败消耗）。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/pricing"
)

// scripted 是可控随机源：按序给值，并记录被抽取的次数（用来钉掷骰顺序）。
type scripted struct {
	vals  []int
	i     int
	calls int
}

func (s *scripted) roll(n int) int {
	s.calls++
	if n <= 0 {
		return 0
	}
	v := 0
	if s.i < len(s.vals) {
		v = s.vals[s.i]
		s.i++
	}
	return v % n
}

func cand(t *testing.T, cfg *config.GameConfig, name string, level int, tweak func(*item.Item)) MixCandidate {
	t.Helper()
	def, ok := cfg.ItemByName(name)
	if !ok {
		t.Fatalf("导出件里没有物品 %q", name)
	}
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: byte(level),
		Durability: maxDurabilityOfOnePiece(cfg, &item.Item{Group: byte(def.Group), Number: def.Number, Level: byte(level)})}
	if tweak != nil {
		tweak(it)
	}
	return MixCandidate{It: it, Def: def}
}

func loadCraftCfg(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func synth(handler string, st *config.CraftingSettings) *config.Crafting {
	return &config.Crafting{Number: 999, Handler: handler, Settings: st,
		Hosts: []config.CraftingHost{{Monster: 238, NpcWindow: "ChaosMachine"}}}
}

func run(cfg *config.GameConfig, craft *config.Crafting, storage []MixCandidate, r *scripted, money uint64, socket byte) MixPlan {
	return DecideCraft(CraftRequest{Craft: craft, Storage: storage, Cfg: cfg,
		Roll: r.roll, Money: money, SocketSlot: socket})
}

// TestDevilSquareTicket 锁定券类：两件活动物同等级 + 混沌 → 券等级=输入等级、价格按等级查表、
// 成功率先档 80（等级<5）否则 70。
func TestDevilSquareTicket(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(2)
	if !ok {
		t.Fatal("配方 2 不存在")
	}
	storage := []MixCandidate{
		cand(t, cfg, "Devil's Eye", 4, nil),
		cand(t, cfg, "Devil's Key", 4, nil),
		cand(t, cfg, "Jewel of Chaos", 0, nil),
	}
	// 一次抽取：NextRandomBool(80) → roll(100)=50 → 命中。
	r := &scripted{vals: []int{50}}
	plan := run(cfg, craft, storage, r, 10_000_000, 0)
	if plan.Code != CraftSuccess {
		t.Fatalf("应成功, got code=%d rate=%d", plan.Code, plan.Rate)
	}
	if plan.Rate != 80 || plan.Price != 700000 {
		t.Fatalf("等级4 应为率 80 / 价 700000, got %d / %d", plan.Rate, plan.Price)
	}
	if plan.Result == nil || plan.Result.Level != 4 || plan.Result.Durability != 1 {
		t.Fatalf("产出应为 4 级耐久 1 的券, got %+v", plan.Result)
	}
	def, _ := cfg.ItemByName("Devil's Invitation")
	if plan.Result.Number != def.Number || plan.Result.Group != byte(def.Group) {
		t.Fatalf("产出定义不符: %+v", plan.Result)
	}
}

// TestTicketRejects 锁定两条拒绝：两件活动物等级不等 → IncorrectMix；血 Castle 用专属码 10。
func TestTicketRejects(t *testing.T) {
	cfg := loadCraftCfg(t)
	t.Run("等级不等", func(t *testing.T) {
		craft, _ := cfg.CraftingByNumber(2)
		storage := []MixCandidate{
			cand(t, cfg, "Devil's Eye", 3, nil), cand(t, cfg, "Devil's Key", 4, nil),
			cand(t, cfg, "Jewel of Chaos", 0, nil),
		}
		plan := run(cfg, craft, storage, &scripted{}, 10_000_000, 0)
		if plan.Code != CraftIncorrectMix {
			t.Fatalf("应回 IncorrectMix, got %d", plan.Code)
		}
	})
	t.Run("血Castle专属码", func(t *testing.T) {
		craft, _ := cfg.CraftingByNumber(8)
		storage := []MixCandidate{
			cand(t, cfg, "Scroll of Archangel", 1, nil), cand(t, cfg, "Blood Bone", 2, nil),
			cand(t, cfg, "Jewel of Chaos", 0, nil),
		}
		plan := run(cfg, craft, storage, &scripted{}, 10_000_000, 0)
		if plan.Code != CraftIncorrectBloodCastle {
			t.Fatalf("血 Castle 应回码 10, got %d", plan.Code)
		}
	})
	t.Run("等级相同才谈成功率", func(t *testing.T) {
		craft, _ := cfg.CraftingByNumber(8)
		storage := []MixCandidate{
			cand(t, cfg, "Scroll of Archangel", 2, nil), cand(t, cfg, "Blood Bone", 2, nil),
			cand(t, cfg, "Jewel of Chaos", 0, nil),
		}
		plan := run(cfg, craft, storage, &scripted{vals: []int{0}}, 10_000_000, 0)
		if plan.Rate != 80 || plan.Code != CraftSuccess {
			t.Fatalf("血 Castle 成功率应恒 80 且命中, got rate=%d code=%d", plan.Rate, plan.Code)
		}
		if plan.Price != 80000 {
			t.Fatalf("等级2 价格应为 80000, got %d", plan.Price)
		}
	})
}

// TestLevelUpInPlace 锁定 +N 升级：目标等级 +1、耐久按"新满值×现耐久/旧满值"重算，
// 且失败时目标连同宝石一起消失（原版 FailResult=Disappear，这是"哪颗宝石必耗"的验收点）。
func TestLevelUpInPlace(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(3) // +10
	if !ok {
		t.Fatal("配方 3 不存在")
	}
	if craft.Settings.Required[0].FailResult != config.MixDisappear {
		t.Fatalf("导出数据：+10 的目标失败时应消失, got %s", craft.Settings.Required[0].FailResult)
	}
	base := func() []MixCandidate {
		return []MixCandidate{
			cand(t, cfg, "Sword of Destruction", 9, nil),
			cand(t, cfg, "Jewel of Chaos", 0, nil),
			cand(t, cfg, "Jewel of Bless", 0, nil),
			cand(t, cfg, "Jewel of Soul", 0, nil),
		}
	}
	target := base()[0]
	def := target.Def

	t.Run("成功", func(t *testing.T) {
		storage := base()
		plan := run(cfg, craft, storage, &scripted{vals: []int{10}}, 50_000_000, 0)
		if plan.Code != CraftSuccess {
			t.Fatalf("roll=10 <= 60 应成功, got code=%d rate=%d", plan.Code, plan.Rate)
		}
		if plan.Rate != 60 || plan.Price != 2_000_000 {
			t.Fatalf("+10 应为率 60 / 价 2000000, got %d / %d", plan.Rate, plan.Price)
		}
		if storage[0].It.Level != 10 {
			t.Fatalf("目标应升到 10, got %d", storage[0].It.Level)
		}
		wantMax := int(pricing.MaximumDurability(def, storage[0].It))
		prevMax := int(pricing.MaximumDurability(def, &item.Item{Group: byte(def.Group), Number: def.Number, Level: 9}))
		if wantMax <= 0 || prevMax <= 0 {
			t.Fatal("满值计算为 0，用例前提不成立")
		}
		if int(storage[0].It.Durability) != wantMax*prevMax/prevMax {
			t.Fatalf("耐久重算异常: %d", storage[0].It.Durability)
		}
		if len(plan.Kept) != 1 || len(plan.Created) != 0 {
			t.Fatalf("升级是就地改造：kept=%d created=%d", len(plan.Kept), len(plan.Created))
		}
	})

	t.Run("失败全耗", func(t *testing.T) {
		storage := base()
		plan := run(cfg, craft, storage, &scripted{vals: []int{99}}, 50_000_000, 0)
		if plan.Code != CraftFailed {
			t.Fatalf("roll=99 > 60 应失败, got %d", plan.Code)
		}
		if len(plan.Consume) != 4 {
			t.Fatalf("失败时目标与三颗宝石都该消失, got %d", len(plan.Consume))
		}
		if plan.Result != nil || len(plan.Created) != 0 {
			t.Fatal("失败不得有产出")
		}
	})
}

// TestRateWrapsAsByte 锁定原版逐次 (byte) 截断：5% 配方遇上镶嵌目标（-20）会回绕成 241，
// 再被夹到 100 —— 于是这次合成必然成功。这是"看似 bug 但必须照抄"的行为。
func TestRateWrapsAsByte(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft := synth("", &config.CraftingSettings{
		SuccessPercent: 5, AddSocket: -20,
		Required: []config.CraftingRequired{{MinAmount: 1, MaxAmount: 1,
			FailResult: config.MixDisappear, SuccessResult: config.MixDisappear}},
	})
	target := cand(t, cfg, "Sword of Destruction", 0, func(it *item.Item) { it.SocketCount = 3 })
	r := &scripted{}
	plan := run(cfg, craft, []MixCandidate{target}, r, 0, 0)
	if plan.Rate != 100 {
		t.Fatalf("回绕后应被夹到 100, got %d", plan.Rate)
	}
	if plan.Code != CraftSuccess {
		t.Fatalf("率 100 应必然成功（NextRandomBool 不抽随机数）, code=%d calls=%d", plan.Code, r.calls)
	}
	if r.calls != 0 {
		t.Fatalf("percent==100 不应消耗随机数, got %d 次", r.calls)
	}
}

// TestChaosWeaponUsesOldJewelPrices 锁定全局 NpcPriceDivisor 分支：成功率用"旧买入价"合计除以 20000，
// 混沌宝石按旧表 40000 计价（现价高一个量级，混用会把概率抬到天上去）。
func TestChaosWeaponUsesOldJewelPrices(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(1)
	if !ok {
		t.Fatal("配方 1 不存在")
	}
	weapon := cand(t, cfg, "Sword of Destruction", 4, func(it *item.Item) { it.OptionLevel = 4 })
	chaos := cand(t, cfg, "Jewel of Chaos", 0, nil)
	plan := run(cfg, craft, []MixCandidate{weapon, chaos}, &scripted{vals: []int{0}}, 10_000_000, 0)

	wantOld := pricing.FinalOldBuyingPrice(weapon.Def, weapon.It) + pricing.FinalOldBuyingPrice(chaos.Def, chaos.It)
	if int(plan.Rate) != int(toByte(int(wantOld/20000))) {
		t.Fatalf("成功率应按旧价合计/20000 = %d, got %d", wantOld/20000, plan.Rate)
	}
	if newPrice := pricing.FinalBuyingPrice(chaos.Def, chaos.It); newPrice == 40_000 {
		t.Fatal("用例前提失效：混沌宝石新旧价相同")
	}
	if plan.Price != int64(10_000)*int64(plan.Rate) {
		t.Fatalf("价格应为 10000×率, got %d (rate %d)", plan.Price, plan.Rate)
	}
}

// TestLeftoverItemRejected 锁定"容器里有未被任何要求消化的物品 → IncorrectMix"。
func TestLeftoverItemRejected(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft := synth("", &config.CraftingSettings{SuccessPercent: 100,
		Required: []config.CraftingRequired{{MinAmount: 1, MaxAmount: 1,
			PossibleItems: []config.ItemRef{{Group: 12, Number: 15}}}}})
	storage := []MixCandidate{
		cand(t, cfg, "Jewel of Chaos", 0, nil),
		cand(t, cfg, "Jewel of Bless", 0, nil), // 没人要它
	}
	if plan := run(cfg, craft, storage, &scripted{}, 0, 0); plan.Code != CraftIncorrectMix {
		t.Fatalf("多余物品应致 IncorrectMix, got %d", plan.Code)
	}
}

// TestNotEnoughMoneyKeepsItems 锁定金额检查在掷骰与任何改动之前（原版 TryPayChaosCost 失败即返回）。
func TestNotEnoughMoneyKeepsItems(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, _ := cfg.CraftingByNumber(3)
	storage := []MixCandidate{
		cand(t, cfg, "Sword of Destruction", 9, nil),
		cand(t, cfg, "Jewel of Chaos", 0, nil),
		cand(t, cfg, "Jewel of Bless", 0, nil),
		cand(t, cfg, "Jewel of Soul", 0, nil),
	}
	before := *storage[0].It
	plan := run(cfg, craft, storage, &scripted{vals: []int{0}}, 1000, 0)
	if plan.Code != CraftNotEnoughMoney || plan.Price != 2_000_000 {
		t.Fatalf("应回 NotEnoughMoney 并报出价格, got code=%d price=%d", plan.Code, plan.Price)
	}
	if storage[0].It.Level != before.Level || storage[0].It.Durability != before.Durability {
		t.Fatal("钱不够时不得改动物品")
	}
	if len(plan.Consume) != 0 {
		t.Fatal("钱不够时不得有消耗")
	}
}

// TestSeedSphereNumberFormula 锁定 (12,60+seedIdx) + (12,70+sphereIdx) → (12,100+seedIdx+6*sphereIdx)，
// 产物等级取种子等级（等级即选项种类）。
func TestSeedSphereNumberFormula(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft := synth("MUnique.OpenMU.GameLogic.PlayerActions.Craftings.SeedSphereCrafting",
		&config.CraftingSettings{SuccessPercent: 100, ResultItems: []config.CraftingResult{{}}})
	seed := cand(t, cfg, "Seed (Water)", 3, nil)
	sphere := cand(t, cfg, "Sphere (Tri)", 0, nil)
	craft.Settings.Required = []config.CraftingRequired{
		{Reference: 119, MinAmount: 1, MaxAmount: 1, MaxLevel: 15, PossibleItems: []config.ItemRef{{Group: 12, Number: 61}}},
		{Reference: 102, MinAmount: 1, MaxAmount: 1, MaxLevel: 15, PossibleItems: []config.ItemRef{{Group: 12, Number: 72}}},
	}
	plan := run(cfg, craft, []MixCandidate{seed, sphere}, &scripted{}, 0, 0)
	if plan.Code != CraftSuccess {
		t.Fatalf("应成功, got %d", plan.Code)
	}
	if plan.Result == nil || plan.Result.Group != 12 || plan.Result.Number != 113 || plan.Result.Level != 3 {
		t.Fatalf("球种应为 (12,113) 等级 3, got %+v", plan.Result)
	}
}

// TestMountSeedSphereWritesSlotByte 锁定槽位字节 = (球等级-1)*50 + 子选项偏移 + 选项号，
// 以及原版的"元素相克"门：护甲不接受火球（武器才是火/雷/冰）。
func TestMountSeedSphereWritesSlotByte(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft := synth("MUnique.OpenMU.GameLogic.PlayerActions.Craftings.MountSeedSphereCrafting",
		&config.CraftingSettings{SuccessPercent: 100, ResultItems: []config.CraftingResult{{}}})
	water := candNum(t, cfg, 12, 101, 1, nil) // Seed Sphere (Water) (1)
	fire := candNum(t, cfg, 12, 100, 1, nil)  // Seed Sphere (Fire) (1)
	shield := candNum(t, cfg, 6, 17, 0, func(it *item.Item) { it.SocketCount = 3 })
	craft.Settings.Required = []config.CraftingRequired{
		{Reference: 119, MinAmount: 1, MaxAmount: 1, MaxLevel: 15, PossibleItems: []config.ItemRef{{Group: 12, Number: water.Def.Number}}},
		{Reference: 136, MinAmount: 1, MaxAmount: 1, MaxLevel: 15, PossibleItems: []config.ItemRef{{Group: shield.Def.Group, Number: shield.Def.Number}}},
	}

	t.Run("元素不相容", func(t *testing.T) {
		fireReq := append([]config.CraftingRequired(nil), craft.Settings.Required...)
		fireReq[0].PossibleItems = []config.ItemRef{{Group: 12, Number: fire.Def.Number}}
		burned := *craft
		burned.Settings = &config.CraftingSettings{SuccessPercent: 100, Required: fireReq}
		plan := run(cfg, &burned, []MixCandidate{fire, candNum(t, cfg, 6, 17, 0, func(it *item.Item) { it.SocketCount = 3 })},
			&scripted{}, 0, 0)
		if plan.Code != CraftIncorrectMix {
			t.Fatalf("护甲镶火球应 IncorrectMix, got %d", plan.Code)
		}
	})

	plan := run(cfg, craft, []MixCandidate{water, shield}, &scripted{}, 0, 1)
	if plan.Code != CraftSuccess {
		t.Fatalf("水球应可镶护甲, got %d", plan.Code)
	}
	if !shield.It.SocketFilled[1] {
		t.Fatal("应写入 1 号槽")
	}
	// Water 的子选项偏移是 10，选项号 1（等级 1）→ (1-1)*50 + 10 + 1。
	if got, want := shield.It.SocketSlots[1], byte(11); got != want {
		t.Fatalf("槽位字节应为 %d, got %d", want, got)
	}
	if len(plan.Kept) != 1 || len(plan.Created) != 0 {
		t.Fatalf("镶嵌是就地改造, kept=%d created=%d", len(plan.Kept), len(plan.Created))
	}
}

// TestGuardianOptionTargetRules 锁定守护者选项：目标必须"能挂且尚未挂"；两件目标 → TooManyItems。
func TestGuardianOptionTargetRules(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(36)
	if !ok {
		t.Fatal("配方 36 不存在")
	}
	name := guardianBearer(t, cfg)
	target := cand(t, cfg, name, 5, func(it *item.Item) { it.OptionLevel = 4 })
	stone := cand(t, cfg, "Jewel of Guardian", 0, nil)
	harmony := cand(t, cfg, "Jewel of Harmony", 0, nil)

	plan := run(cfg, craft, []MixCandidate{target, stone, harmony}, &scripted{}, 20_000_000, 0)
	if plan.Code != CraftSuccess || !target.It.GuardianOption {
		t.Fatalf("成功应挂守护者选项, code=%d guardian=%v", plan.Code, target.It.GuardianOption)
	}
	if plan.Price != 10_000_000 {
		t.Fatalf("价格应为 10000000, got %d", plan.Price)
	}
	// 已挂过的目标不再被接受（matcher 的"尚未挂"条件）。
	again := cand(t, cfg, name, 5, func(it *item.Item) { it.OptionLevel = 4; it.GuardianOption = true })
	plan1 := run(cfg, craft, []MixCandidate{again, stone, harmony}, &scripted{}, 20_000_000, 0)
	if plan1.Code != CraftIncorrectMix {
		t.Fatalf("已挂守护者的装备不该被当目标（无人认领 → IncorrectMix）, got %d", plan1.Code)
	}
	if again.It.GuardianOption != true {
		t.Fatal("被拒的这次不得改动物品")
	}
	// 重新造一批：上一例已把 GuardianOption 写进 target，复用会让本例走成"无人认领"。
	plan2 := run(cfg, craft, []MixCandidate{
		cand(t, cfg, name, 5, func(it *item.Item) { it.OptionLevel = 4 }),
		cand(t, cfg, name, 8, func(it *item.Item) { it.OptionLevel = 4 }),
		cand(t, cfg, "Jewel of Guardian", 0, nil),
		cand(t, cfg, "Jewel of Harmony", 0, nil),
	}, &scripted{}, 20_000_000, 0)
	if plan2.Code != CraftTooManyItems {
		t.Fatalf("两件目标应回 TooManyItems, got %d", plan2.Code)
	}
}

// TestRestoreItemPriceAndClear 锁定还原：价格按和谐选项档位查表，产出即去掉和谐项的同一件装备。
func TestRestoreItemPriceAndClear(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(35)
	if !ok {
		t.Fatal("配方 35 不存在")
	}
	items := []MixCandidate{cand(t, cfg, "Sword of Destruction", 0, func(it *item.Item) {
		it.HarmonyNumber = 1
		it.HarmonyLevel = 3
	})}
	if items[0].Def == nil {
		t.Skip("导出件里没有该装备")
	}
	plan := run(cfg, craft, items, &scripted{}, 1_000_000, 0)
	if plan.Price != restorePricePerOptionLevel[3] {
		t.Fatalf("价格应为和谐等级 3 档 %d, got %d", restorePricePerOptionLevel[3], plan.Price)
	}
	if plan.Code != CraftSuccess {
		t.Fatalf("率 100 必然成功, got %d", plan.Code)
	}
	if items[0].It.HarmonyNumber != 0 || items[0].It.HarmonyLevel != 0 {
		t.Fatalf("和谐选项应被清除: %+v", items[0].It)
	}
}

// TestFenrirUpgradeRejectsMixedTargets 锁定非泪升级：武器与防具同时出现 → IncorrectMix；
// 两者皆无 → LackingMixItems。
func TestFenrirUpgradeRejectsMixedTargets(t *testing.T) {
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(28)
	if !ok {
		t.Fatal("配方 28 不存在")
	}
	weapon := cand(t, cfg, "Sword of Destruction", 4, func(it *item.Item) { it.OptionLevel = 4 })
	armor := cand(t, cfg, "Plate Armor", 4, func(it *item.Item) { it.OptionLevel = 4 })
	horn := cand(t, cfg, "Horn of Fenrir", 0, nil)
	chaos := cand(t, cfg, "Jewel of Chaos", 0, nil)
	life := cand(t, cfg, "Jewel of Life", 0, nil)
	base := []MixCandidate{horn, chaos}
	for i := 0; i < 5; i++ {
		base = append(base, cand(t, cfg, "Jewel of Life", 0, nil))
	}
	if weapon.Def == nil || armor.Def == nil || horn.Def == nil {
		t.Skip("导出件缺少用例所需物品")
	}
	_ = life
	if plan := run(cfg, craft, append(append([]MixCandidate{}, base...), weapon, armor), &scripted{}, 20_000_000, 0); plan.Code != CraftIncorrectMix {
		t.Fatalf("武器+防具同时出现应 IncorrectMix, got %d", plan.Code)
	}
	if plan := run(cfg, craft, append([]MixCandidate{}, base...), &scripted{}, 20_000_000, 0); plan.Code != CraftLackingMixItems {
		t.Fatalf("没有武器/防具应 LackingMixItems, got %d", plan.Code)
	}
}

// TestFruitsUseWeightedLevel 锁定果实产出等级走加权表 [30,25,20,20,5]（其余物品走结果表区间）。
func TestFruitsUseWeightedLevel(t *testing.T) {
	for target, want := range map[int]int{0: 0, 29: 0, 30: 1, 54: 1, 55: 2, 74: 2, 75: 3, 94: 3, 95: 4, 99: 4} {
		r := &scripted{vals: []int{target}}
		if got := weightedLevel(r.roll, []int{30, 25, 20, 20, 5}); got != want {
			t.Errorf("抽取值 %d 应落在等级 %d, got %d", target, want, got)
		}
	}
	cfg := loadCraftCfg(t)
	craft, ok := cfg.CraftingByNumber(6)
	if !ok {
		t.Fatal("配方 6 不存在")
	}
	storage := []MixCandidate{
		cand(t, cfg, "Jewel of Chaos", 0, nil),
		cand(t, cfg, "Jewel of Creation", 0, nil),
	}
	plan := run(cfg, craft, storage, &scripted{vals: []int{10, 0, 20}}, 10_000_000, 0)
	if plan.Code != CraftSuccess {
		t.Fatalf("roll=10 应命中 90%%, got %d", plan.Code)
	}
	if plan.Result == nil || plan.Result.Group != 13 || plan.Result.Number != 15 {
		t.Fatalf("产出应为果实, got %+v", plan.Result)
	}
	if plan.Result.Level > 4 {
		t.Fatalf("果实等级应落在 0..4, got %d", plan.Result.Level)
	}
}

// candNum 按 (group, number) 造候选（球种名字带档位，按名取不稳）。
func candNum(t *testing.T, cfg *config.GameConfig, group, number, level int, tweak func(*item.Item)) MixCandidate {
	t.Helper()
	def, ok := cfg.Item(group, number)
	if !ok {
		t.Fatalf("导出件里没有物品 (%d,%d)", group, number)
	}
	it := &item.Item{Group: byte(group), Number: number, Level: byte(level)}
	it.Durability = maxDurabilityOfOnePiece(cfg, it)
	if tweak != nil {
		tweak(it)
	}
	return MixCandidate{It: it, Def: def}
}

// guardianBearer 返回第一件"可挂守护者选项"的可穿戴装备（Level380 套）。
func guardianBearer(t *testing.T, cfg *config.GameConfig) string {
	t.Helper()
	for i := range cfg.Items {
		def := &cfg.Items[i]
		if !def.IsWearable() || def.MaximumItemLevel < 6 {
			continue
		}
		if definitionOfKind(cfg, def, config.OptionKindGuardian) == nil {
			continue
		}
		return def.Name
	}
	t.Skip("导出件里没有可挂守护者选项的装备")
	return ""
}

// TestEveryCraftingHasImplementation 是"全量 39 条"的守卫：导出件里每条配方都必须能被
// 引擎接住（handler 名可分派，且空 handler 必带 settings）——防止再出现"静默不接受"。
func TestEveryCraftingHasImplementation(t *testing.T) {
	cfg := loadCraftCfg(t)
	if len(cfg.Craftings) != 39 {
		t.Fatalf("S6 配方应为 39 条, got %d", len(cfg.Craftings))
	}
	byHandler := map[string]int{}
	for i := range cfg.Craftings {
		craft := &cfg.Craftings[i]
		if crafterFor(craft) == nil {
			t.Errorf("配方 %d(%s) 的 handler %q 无实现", craft.Number, craft.Name, craft.Handler)
			continue
		}
		if shortHandlerName(craft.Handler) == "" && craft.Settings == nil {
			t.Errorf("配方 %d 无 handler 也无 settings", craft.Number)
		}
		byHandler[shortHandlerName(craft.Handler)]++
	}
	if byHandler[""] != 22 {
		t.Errorf("数据驱动配方应为 22 条, got %d", byHandler[""])
	}
	if len(byHandler) != 16 {
		t.Errorf("handler 种类应为 16 种（含空）, got %d: %v", len(byHandler), byHandler)
	}
}
