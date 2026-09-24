package action

// consume_strategy_test.go —— TRIM-01 策略决策层的确定性单测。
//
// 随机性处理：World.RNG() 无注种入口，所以"掷骰敏感"的判定全部在这一层用
// util.NewRand(seed) 锁定——seed 流是 System.Random 复刻（T0-d），跨平台稳定。
// 辅助 seedFor 在线性扫描里找一个"前几次抽取落在指定区间"的种子（纯计算，无 I/O），
// 找到即确定；因此这些测试不依赖运气。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/util"
)

// seedFor 扫描出一个种子，使 pred 在其前几次抽取上为真。返回 seed 本身：
// 每个用例都要 util.NewRand(seed) 起一个**全新**实例（复用同一实例会推进抽取流，
// 后续用例就不再满足 pred）。
func seedFor(t *testing.T, pred func(*util.Rand) bool) int {
	t.Helper()
	for seed := 1; seed < 2_000_000; seed++ {
		if pred(util.NewRand(seed)) {
			return seed
		}
	}
	t.Fatal("在 2,000,000 个种子内找不到满足条件的抽取序列")
	return 0
}

// ---------------------------------------------------------------------------
// 注册表
// ---------------------------------------------------------------------------

func TestConsumeStrategyForKeys(t *testing.T) {
	for _, tc := range []struct {
		group, number int
		want          ConsumeStrategy
	}{
		{13, 15, ConsumeStrategyFruit},
		{14, 8, ConsumeStrategyAntidote},
		{14, 10, ConsumeStrategyTownPortal},
		{14, 13, ConsumeStrategyBlessJewel},
		{14, 14, ConsumeStrategySoulJewel},
		{14, 16, ConsumeStrategyLifeJewel},
		{14, 42, ConsumeStrategyHarmonyJewel},
		{14, 43, ConsumeStrategyLowerRefineStone},
		{14, 44, ConsumeStrategyHigherRefineStone},
	} {
		s, ok := ConsumeStrategyFor(tc.group, tc.number)
		if !ok || s != tc.want {
			t.Fatalf("(%d,%d) 应命中策略 %d, got %d ok=%v", tc.group, tc.number, tc.want, s, ok)
		}
	}
	// 药水走配方表；创造宝石(14,22) 原版也没有消耗插件（ItemConsumeActions 目录里
	// 没有 CreationJewel*）→ 都必须 miss（否则分发顺序被破坏）。
	for _, k := range [][2]int{{14, 1}, {14, 22}, {12, 5}, {14, 15}} {
		if _, ok := ConsumeStrategyFor(k[0], k[1]); ok {
			t.Fatalf("(%d,%d) 不应注册精确策略", k[0], k[1])
		}
	}
}

// ---------------------------------------------------------------------------
// 果实
// ---------------------------------------------------------------------------

// baseFruit 是一个"全部门槛都通过"的加点输入（上限 100、已用 0 → 100% 档）。
func baseFruit(r *util.Rand) FruitInput {
	return FruitInput{
		Usage: FruitUsageAddPoints, PlayerLevel: 100, ItemLevel: 0,
		StatAllowed: true, FruitPointCap: 100,
		StatValue: 50, StatBaseValue: 30,
		Rand: r,
	}
}

func TestPlanFruitPreventedGates(t *testing.T) {
	rng := util.NewRand(1) // 门槛路径不消耗抽取
	cases := []struct {
		name    string
		mutate  func(*FruitInput)
		usage   FruitUsage
		outcome int
	}{
		{"等级不足", func(in *FruitInput) { in.PlayerLevel = 9 }, FruitUsageAddPoints, FruitOutcomePlusPrevented},
		{"果等级非法", func(in *FruitInput) { in.ItemLevel = 5 }, FruitUsageAddPoints, FruitOutcomePlusPrevented},
		{"职业不可增", func(in *FruitInput) { in.StatAllowed = false }, FruitUsageAddPoints, FruitOutcomePlusPrevented},
		{"穿戴装备", func(in *FruitInput) { in.HasEquipped = true }, FruitUsageAddPoints, FruitOutcomePreventedByEquipped},
		{"加点满上限", func(in *FruitInput) { in.UsedAddPoints = 100 }, FruitUsageAddPoints, FruitOutcomePlusPreventedByMaximum},
		{"洗点满上限", func(in *FruitInput) { in.UsedNegPoints = 100 }, FruitUsageRemovePoints, FruitOutcomeMinusPreventedByMaximum},
		{"洗点跌破初始", func(in *FruitInput) { in.StatValue = 30 }, FruitUsageRemovePoints, FruitOutcomeMinusPreventedByDefault},
		{"等级不足(洗点)", func(in *FruitInput) { in.PlayerLevel = 9 }, FruitUsageRemovePoints, FruitOutcomeMinusPrevented},
	}
	for _, tc := range cases {
		in := baseFruit(rng)
		in.Usage = tc.usage
		if tc.usage == FruitUsageRemovePoints {
			in.StatValue = 50 // 跌破初始用例自己覆盖
		}
		tc.mutate(&in)
		got := PlanFruit(in)
		if !got.Prevented || got.Outcome != tc.outcome || got.Applied || got.Consumed {
			t.Fatalf("%s: got %+v, want prevented outcome=%d", tc.name, got, tc.outcome)
		}
	}
}

// TestPlanFruitSuccessPercentageTiers 锁定成功率分档边界（原版 GetSuccessPercentage
// 按**本用途已用点数** over=used−10 与 cap 比例 0.1/0.3/0.5/0.8 降档 90/80/70/60/50）。
// 每档用一对 used 值 + 一个恰好在两档之间命中的掷骰来钉死边界。
func TestPlanFruitSuccessPercentageTiers(t *testing.T) {
	// 掷骰 a：NextRandomBool 判 a <= percent（percent=100 短路无抽取）。
	cases := []struct {
		roll [2]int // a ∈ [lo,hi] 使高档成功、低档失败
		ok   int    // used：落在成功档
		bad  int    // used：落在下一（更低成功率）档
	}{
		{[2]int{81, 90}, 19, 20}, // 90 vs 80
		{[2]int{71, 80}, 39, 40}, // 80 vs 70
		{[2]int{61, 70}, 59, 60}, // 70 vs 60
		{[2]int{51, 60}, 89, 90}, // 60 vs 50
	}
	for _, tc := range cases {
		seed := seedFor(t, func(r *util.Rand) bool {
			a := r.Next(0, 100)
			return a >= tc.roll[0] && a <= tc.roll[1]
		})
		in := baseFruit(util.NewRand(seed))
		in.FruitPointCap = 100
		in.UsedAddPoints = tc.ok
		if got := PlanFruit(in); !got.Applied || got.Outcome != FruitOutcomePlusSuccess {
			t.Fatalf("used=%d 应成功(掷骰 %v): %+v", tc.ok, tc.roll, got)
		}
		in.UsedAddPoints = tc.bad
		in.Rand = util.NewRand(seed)
		got := PlanFruit(in)
		if got.Applied || got.Outcome != FruitOutcomePlusFailed || !got.Consumed {
			t.Fatalf("used=%d 应失败但仍消耗(掷骰 %v): %+v", tc.bad, tc.roll, got)
		}
	}
}

// TestPlanFruitFailedRollStillConsumes 锁定原版关键语义：掷骰失败**果实照被消耗**
// （返回 true → 基类扣耐久），应答点数 0。
func TestPlanFruitFailedRollStillConsumes(t *testing.T) {
	failRoll := func(r *util.Rand) bool { return r.Next(0, 100) > 50 }
	seed := seedFor(t, failRoll)
	in := baseFruit(util.NewRand(seed))
	in.FruitPointCap = 100
	in.UsedAddPoints = 90 // over=80 → 50% 档
	got := PlanFruit(in)
	if got.Outcome != FruitOutcomePlusFailed || !got.Consumed || got.Applied || got.Points != 0 {
		t.Fatalf("加点失败: %+v", got)
	}

	in = baseFruit(util.NewRand(seed))
	in.Usage = FruitUsageRemovePoints
	in.FruitPointCap = 100
	in.UsedNegPoints = 90
	got = PlanFruit(in)
	if got.Outcome != FruitOutcomeMinusFailed || !got.Consumed || got.Applied {
		t.Fatalf("洗点失败: %+v", got)
	}
}

// TestPlanFruitClampsPointsToRemaining 锁定"成功点数被剩余上限截断"。
func TestPlanFruitClampsPointsToRemaining(t *testing.T) {
	// 100% 档（used≤10）必成功；剩余 1 → 无论点数掷骰如何都只加 1。
	in := baseFruit(util.NewRand(7))
	in.FruitPointCap = 11
	in.UsedAddPoints = 10
	got := PlanFruit(in)
	if !got.Applied || got.Points != 1 {
		t.Fatalf("剩余 1 点应被截断为 1: %+v", got)
	}
	// 洗点方向同理用负点已用计数算剩余。
	in = baseFruit(util.NewRand(8))
	in.Usage = FruitUsageRemovePoints
	in.FruitPointCap = 11
	in.UsedNegPoints = 10
	if got := PlanFruit(in); !got.Applied || got.Points != 1 {
		t.Fatalf("洗点截断失败: %+v", got)
	}
}

// TestFruitRandomPointsDistribution 锁定点数映射（原版 GetRandomPoints 的掷骰表）。
func TestFruitRandomPointsDistribution(t *testing.T) {
	addTable := func(v int) int {
		switch {
		case v < 70:
			return 1
		case v < 95:
			return 2
		default:
			return 3
		}
	}
	minusTable := func(v int) int {
		switch {
		case v < 50:
			return 1
		case v < 75:
			return 3
		case v < 91:
			return 5
		case v < 98:
			return 7
		default:
			return 9
		}
	}
	// 遍历全部 101 个可命中值：用同一 seed 流的孪生实例预测，再对照纯函数输出。
	seen := map[bool]map[int]bool{true: {}, false: {}}
	for seed := 1; seed < 30000 && (len(seen[true]) < 3 || len(seen[false]) < 5); seed++ {
		r := util.NewRand(seed)
		v := r.Next(0, 101)
		if got := fruitRandomPoints(util.NewRand(seed), true); got != addTable(v) {
			t.Fatalf("seed %d 加点掷骰 %d → %d, want %d", seed, v, got, addTable(v))
		}
		if got := fruitRandomPoints(util.NewRand(seed), false); got != minusTable(v) {
			t.Fatalf("seed %d 洗点掷骰 %d → %d, want %d", seed, v, got, minusTable(v))
		}
		seen[true][addTable(v)] = true
		seen[false][minusTable(v)] = true
	}
	if len(seen[true]) != 3 || len(seen[false]) != 5 {
		t.Fatalf("30000 个种子应能覆盖全部档位: add=%v minus=%v", seen[true], seen[false])
	}
}

func TestMaxFruitPointsTable(t *testing.T) {
	// 锚定值按原版 GetFruitPoints 公式（current=2；逢 10 级 current += 3*(i+11)/divisor + 2）
	// 独立算出；三种 divisor 400/700/500。
	for _, tc := range []struct {
		level, calc, want int
	}{
		{1, 0, 2}, {9, 0, 2}, {10, 0, 4}, {11, 0, 4}, {20, 0, 6},
		{100, 0, 22}, {129, 0, 26}, {130, 0, 29}, {400, 0, 127},
		{130, 1, 28}, {200, 1, 42}, {400, 1, 100},
		{130, 2, 28}, {200, 2, 47}, {400, 2, 115},
	} {
		if got := MaxFruitPoints(tc.level, tc.calc); got != tc.want {
			t.Fatalf("MaxFruitPoints(%d, calc=%d)=%d, want %d", tc.level, tc.calc, got, tc.want)
		}
	}
	// 越界钳制（原版 >400 越界异常，我们钳到末档；<1 钳到首档）。
	if MaxFruitPoints(0, 0) != MaxFruitPoints(1, 0) || MaxFruitPoints(401, 0) != MaxFruitPoints(400, 0) {
		t.Fatal("等级钳制失败")
	}
}

// ---------------------------------------------------------------------------
// Bless / Soul
// ---------------------------------------------------------------------------

func TestPlanItemLevelUpgradeBless(t *testing.T) {
	rule, ok := LevelRuleForStrategy(ConsumeStrategyBlessJewel)
	if !ok || rule.MaximumLevel != 5 || rule.SuccessRate != 100 || rule.LevelAmount != 1 {
		t.Fatalf("Bless 默认规则不符: %+v", rule)
	}
	def := &config.Item{Group: 6, Number: 0, Slots: []int{1}, MaximumItemLevel: 15}
	rng := util.NewRand(1)

	for lvl := 0; lvl <= 5; lvl++ {
		got := PlanItemLevelUpgrade(rng, rule, &item.Item{Level: byte(lvl)}, def)
		if !got.Modified || !got.Success || got.NewLevel != byte(lvl+1) {
			t.Fatalf("+%d 应必成 +1: %+v", lvl, got)
		}
	}
	// 原版 MaximumLevel=5 的口径是"+5 仍可用（升到 6）"，+6 起 amount<=0 → 不消耗。
	if got := PlanItemLevelUpgrade(rng, rule, &item.Item{Level: 6}, def); got.Modified {
		t.Fatalf("+6 后 Bless 应拒绝: %+v", got)
	}
	// 定义上限优先：maximumAllowed=min(6, defMax=3)=3。
	capped := &config.Item{Group: 6, Number: 0, Slots: []int{1}, MaximumItemLevel: 3}
	if got := PlanItemLevelUpgrade(rng, rule, &item.Item{Level: 2}, capped); !got.Success || got.NewLevel != 3 {
		t.Fatalf("defMax=3 时 +2 应升到 3: %+v", got)
	}
	if got := PlanItemLevelUpgrade(rng, rule, &item.Item{Level: 3}, capped); got.Modified {
		t.Fatalf("已达定义上限应拒绝: %+v", got)
	}
	// 不可穿戴 / 无低槽（宠物槽 8）→ 拒绝。
	if got := PlanItemLevelUpgrade(rng, rule, &item.Item{}, &config.Item{MaximumItemLevel: 15}); got.Modified {
		t.Fatal("非穿戴件应拒绝")
	}
	if got := PlanItemLevelUpgrade(rng, rule, &item.Item{}, &config.Item{Slots: []int{8}, MaximumItemLevel: 15}); got.Modified {
		t.Fatal("仅翅膀以上槽位（宠物/坐骑）应拒绝")
	}
}

func TestPlanItemLevelUpgradeSoul(t *testing.T) {
	rule, ok := LevelRuleForStrategy(ConsumeStrategySoulJewel)
	if !ok || rule.MaximumLevel != 8 || rule.SuccessRate != 50 || rule.LuckBonus != 25 ||
		rule.ResetToZeroFromLevel != 7 {
		t.Fatalf("Soul 默认规则不符: %+v", rule)
	}
	def := &config.Item{Group: 6, Number: 0, Slots: []int{1}, MaximumItemLevel: 15}

	// 成功档（a≤50）：+7 → +8；+8 → +9（原版 maximumAllowed=MaximumLevel+1 口径）。
	seedOK := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) <= 50 })
	if got := PlanItemLevelUpgrade(util.NewRand(seedOK), rule, &item.Item{Level: 7}, def); !got.Success || got.NewLevel != 8 {
		t.Fatalf("+7 成功应到 8: %+v", got)
	}
	if got := PlanItemLevelUpgrade(util.NewRand(seedOK), rule, &item.Item{Level: 8}, def); !got.Success || got.NewLevel != 9 {
		t.Fatalf("+8 成功应到 9（原版口径）: %+v", got)
	}
	if got := PlanItemLevelUpgrade(util.NewRand(seedOK), rule, &item.Item{Level: 9}, def); got.Modified {
		t.Fatalf("+9 后 Soul 应拒绝: %+v", got)
	}

	// 失败档：a∈[51,99]。≥+7 清零，否则 -1（+0 失败仍是 0）。
	seedFail := seedFor(t, func(r *util.Rand) bool { a := r.Next(0, 100); return a > 50 })
	for _, tc := range []struct {
		from byte
		want byte
	}{{0, 0}, {3, 2}, {6, 5}, {7, 0}, {9, 0}} {
		got := PlanItemLevelUpgrade(util.NewRand(seedFail), rule, &item.Item{Level: tc.from}, def)
		if got.Modified && got.Success {
			t.Fatalf("掷骰 >50 不该成功: %+v", got)
		}
		if got.NewLevel != tc.want {
			t.Fatalf("失败 +%d → +%d, got +%d", tc.from, tc.want, got.NewLevel)
		}
	}

	// 幸运加成 +25 → 75%：a∈[51,75] 时"无幸运失败、有幸运成功"。
	seedLuck := seedFor(t, func(r *util.Rand) bool { a := r.Next(0, 100); return a >= 51 && a <= 75 })
	if got := PlanItemLevelUpgrade(util.NewRand(seedLuck), rule, &item.Item{Level: 3}, def); got.Success {
		t.Fatal("无幸运时该掷骰应失败")
	}
	if got := PlanItemLevelUpgrade(util.NewRand(seedLuck), rule, &item.Item{Level: 3, Luck: true}, def); !got.Success || got.NewLevel != 4 {
		t.Fatalf("幸运宝石应吃到 +25 加成: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Life / Harmony（用真实 season6 数据：(6,0) 小盾 = 穿戴件 + 一条蓝选项 + 和谐池）
// ---------------------------------------------------------------------------

func season6(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入 season6: %v", err)
	}
	return cfg
}

func shieldDef(t *testing.T, cfg *config.GameConfig) *config.Item {
	t.Helper()
	def, ok := cfg.Item(6, 0)
	if !ok {
		t.Fatal("season6 缺少 (6,0)")
	}
	return def
}

func TestPlanLifeJewel(t *testing.T) {
	cfg := season6(t)
	def := shieldDef(t, cfg)

	// 1) 首次添加成功（a≤50，池只有编号 0 一条 → Level 恒 1）：消耗。
	seedOK := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) <= 50 })
	got := PlanLifeJewel(util.NewRand(seedOK), cfg, &item.Item{}, def)
	if !got.Modified || got.NewOptionLevel != 1 || got.HarmonyAdd != nil {
		t.Fatalf("Life 添加成功应写 OptionLevel=1: %+v", got)
	}
	// 2) 首次添加失败：仍消耗（原版 TryAddItemOption 失败 return true），物品不变。
	seedFail := seedFor(t, func(r *util.Rand) bool { a := r.Next(0, 100); return a > 50 })
	got = PlanLifeJewel(util.NewRand(seedFail), cfg, &item.Item{}, def)
	if !got.Modified || got.NewOptionLevel != -1 || got.HarmonyAdd != nil {
		t.Fatalf("Life 添加失败应消耗且不变: %+v", got)
	}
	// 3) 升级路径（OptionLevel 1 → 有 ldo 2..4）：成功 +1、失败移除（=0）。
	got = PlanLifeJewel(util.NewRand(seedOK), cfg, &item.Item{OptionLevel: 1}, def)
	if !got.Modified || got.NewOptionLevel != 2 {
		t.Fatalf("Life 升级成功应到 2: %+v", got)
	}
	got = PlanLifeJewel(util.NewRand(seedFail), cfg, &item.Item{OptionLevel: 1}, def)
	if !got.Modified || got.NewOptionLevel != 0 {
		t.Fatalf("Life 升级失败应移除(0): %+v", got)
	}
	// 4) 已是最高档（ldo 最高 4）→ 无可升 → **不消耗**。
	if got := PlanLifeJewel(util.NewRand(seedOK), cfg, &item.Item{OptionLevel: 4}, def); got.Modified {
		t.Fatalf("Life 满档应拒绝消耗: %+v", got)
	}
	// 5) 定义无蓝选项候选（药水）→ 不消耗。
	potion, _ := cfg.Item(14, 1)
	if got := PlanLifeJewel(util.NewRand(seedOK), cfg, &item.Item{}, potion); got.Modified {
		t.Fatalf("无候选定义应不消耗: %+v", got)
	}
}

func TestPlanHarmonyJewel(t *testing.T) {
	cfg := season6(t)
	def := shieldDef(t, cfg)

	// 和谐只"首次添加"（increasesOption=false）。
	seedHas := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) <= 60 })
	if got := PlanHarmonyJewel(util.NewRand(seedHas), cfg, &item.Item{HarmonyNumber: 1}, def); got.Modified {
		t.Fatalf("已有和谐选项时升档属 Higher Harmony，本宝石应不消耗: %+v", got)
	}
	// 远古件直接拒绝（原版 ItemCanHaveOption 的 IsAncient）。
	if got := PlanHarmonyJewel(util.NewRand(seedHas), cfg, &item.Item{AncientDiscriminator: 1}, def); got.Modified {
		t.Fatalf("远古件应拒绝和谐宝石: %+v", got)
	}
	// 位域里的编号不在候选定义（数据异常）→ 保守不消耗。
	if got := PlanHarmonyJewel(util.NewRand(seedHas), cfg, &item.Item{HarmonyNumber: 99}, def); got.Modified {
		t.Fatalf("未知和谐编号应保守拒绝: %+v", got)
	}

	// 物品等级 0：池只剩编号 1（其余档 required_item_level≥3），等级=其最小档 0。
	got := PlanHarmonyJewel(util.NewRand(seedHas), cfg, &item.Item{}, def)
	if !got.Modified || got.NewOptionLevel != -1 || got.HarmonyAdd == nil ||
		got.HarmonyAdd.Number != 1 || got.HarmonyAdd.Level != 0 {
		t.Fatalf("lv0 和谐添加应为 {1,0}: %+v", got)
	}
	// 掷骰失败（a>60）→ 消耗但无添加。
	seedFail := seedFor(t, func(r *util.Rand) bool { a := r.Next(0, 100); return a > 60 })
	got = PlanHarmonyJewel(util.NewRand(seedFail), cfg, &item.Item{}, def)
	if !got.Modified || got.HarmonyAdd != nil || got.NewOptionLevel != -1 {
		t.Fatalf("和谐掷骰失败应只消耗: %+v", got)
	}

	// 物品等级 3：候选 {1:w50, 2:w40, 3:w40}，第二次抽取（加权 roll）决定编号。
	// 抽取顺序：先 Next(0,100) 成败掷骰，后 Next(0,130) 权重roll。
	for _, want := range []int{1, 2, 3} {
		num := want
		seed := seedFor(t, func(r *util.Rand) bool {
			if a := r.Next(0, 100); a > 60 {
				return false
			}
			roll := r.Next(0, 130)
			predicted := 1
			switch {
			case roll >= 50 && roll < 90:
				predicted = 2
			case roll >= 90:
				predicted = 3
			}
			return predicted == num
		})
		got := PlanHarmonyJewel(util.NewRand(seed), cfg, &item.Item{Level: 3}, def)
		if got.HarmonyAdd == nil || got.HarmonyAdd.Number != want {
			t.Fatalf("等级 3 加权 roll 应得编号 %d: %+v", want, got)
		}
		// 等级取该候选自身档位的**最小 Level**（原版 Min()??0）：编号 2/3 起始档为 3。
		wantLevel := 0
		if want >= 2 {
			wantLevel = 3
		}
		if got.HarmonyAdd.Level != wantLevel {
			t.Fatalf("编号 %d 的初始和谐等级=%d, got %d", want, wantLevel, got.HarmonyAdd.Level)
		}
	}
}

// ---------------------------------------------------------------------------
// 私有辅助函数的直接锁定
// ---------------------------------------------------------------------------

func TestOptionHelpers(t *testing.T) {
	if _, ok := LevelRuleForStrategy(ConsumeStrategyFruit); ok {
		t.Fatal("果实不是升等级宝石")
	}

	// minOptionLevel：无档 0；有档取最小。
	if minOptionLevel(&config.IncreasableItemOptionExport{}) != 0 {
		t.Fatal("无档应为 0")
	}
	o := &config.IncreasableItemOptionExport{LevelDependentOptions: []config.ItemOptionOfLevelExport{
		{Level: 5}, {Level: 3},
	}}
	if minOptionLevel(o) != 3 {
		t.Fatal("应取最小档")
	}

	// selectWeighted：全零权重退化为第一条（原版同行为）。
	pool := []*config.IncreasableItemOptionExport{{Number: 7}, {Number: 8}}
	if selectWeighted(util.NewRand(3), pool).Number != 7 {
		t.Fatal("全零权重应取第一条")
	}

	// dropFirstTargeting 只剔第一条命中的；filterRequirementReductions 按定义需求把关。
	mk := func(n int, target string) *config.IncreasableItemOptionExport {
		return &config.IncreasableItemOptionExport{Number: n,
			LevelDependentOptions: []config.ItemOptionOfLevelExport{
				{PowerUp: &config.PowerUpDef{Target: target}},
			}}
	}
	withRed := []*config.IncreasableItemOptionExport{
		mk(1, "Strength Requirement reduction"),
		mk(2, "Strength Requirement reduction"),
		mk(3, "Base Defense"),
	}
	got := filterRequirementReductions(withRed, &config.Item{})
	if len(got) != 2 || got[0].Number != 2 || got[1].Number != 3 {
		t.Fatalf("无力量需求时应只剔第一条力量削减候选: %d 条", len(got))
	}
	// 定义带力量需求 → 保留。
	withReq := filterRequirementReductions(withRed,
		&config.Item{Requirements: []config.ItemRequirement{{Attribute: "Total Strength Requirement Value"}}})
	if len(withReq) != 3 {
		t.Fatalf("带力量需求的定义不应剔除削减候选: %d", len(withReq))
	}

	// optionPool：RequiredItemLevel 门槛过滤（无档恒可选）。
	d := &config.ItemOptionDefinitionExport{PossibleOptions: []config.IncreasableItemOptionExport{
		{Number: 1},
		{Number: 2, LevelDependentOptions: []config.ItemOptionOfLevelExport{{Level: 6, RequiredItemLevel: 6}}},
	}}
	if p := optionPool(d, 3); len(p) != 1 || p[0].Number != 1 {
		t.Fatalf("lv3 池应只剩编号 1: %+v", p)
	}
	if p := optionPool(d, 6); len(p) != 2 {
		t.Fatalf("lv6 池应有 2 条: %+v", p)
	}
}

// harmonyFloor 取某物品定义里和谐选项 #1 的最低档（失败回档的落点）。
func harmonyFloor(t *testing.T, cfg *config.GameConfig, def *config.Item) int {
	t.Helper()
	d := cfg.DefinitionOfOptionType(cfg.ItemOptionDefinitionsFor(def.Group, def.Number), config.OptionKindHarmony)
	if d == nil {
		t.Fatalf("(组%d/号%d) 没有和谐选项定义", def.Group, def.Number)
	}
	for _, p := range d.PossibleOptions {
		if p.Number == 1 && len(p.LevelDependentOptions) > 0 {
			min := p.LevelDependentOptions[0].Level
			for _, l := range p.LevelDependentOptions[1:] {
				if l.Level < min {
					min = l.Level
				}
			}
			return min
		}
	}
	t.Fatal("和谐选项 #1 不存在")
	return 0
}

// TestPlanRefineStones 覆盖两档精炼石：只升已有和谐选项、成功 +1、失败回最低档、
// 满档不消耗，以及武器最小伤害守卫（Kris 6..11：档位再往上会把 min 抬过 max-1）。
func TestPlanRefineStones(t *testing.T) {
	cfg := season6(t)
	shield := shieldDef(t, cfg)
	floor := harmonyFloor(t, cfg, shield)
	ok20 := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) <= 20 })
	no20 := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) > 20 })
	ok80 := seedFor(t, func(r *util.Rand) bool { return r.Next(0, 100) <= 80 })

	// 1) 没有和谐选项 → adds=false 的两档都不消耗（宝石留着）。
	if got := PlanLowerRefineStone(util.NewRand(ok20), cfg, &item.Item{Level: 6}, shield); got.Modified {
		t.Fatalf("无和谐选项时下阶精炼石不应消耗: %+v", got)
	}
	if got := PlanHigherRefineStone(util.NewRand(ok80), cfg, &item.Item{Level: 6}, shield); got.Modified {
		t.Fatalf("无和谐选项时上阶精炼石不应消耗: %+v", got)
	}
	// 2) 成功 +1 / 失败回最低档。
	if got := PlanLowerRefineStone(util.NewRand(ok20), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 1}, shield); !got.Modified || got.NewOptionLevel != 2 {
		t.Fatalf("下阶成功应升到 2: %+v", got)
	}
	if got := PlanLowerRefineStone(util.NewRand(no20), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 1}, shield); !got.Modified || got.NewOptionLevel != floor {
		t.Fatalf("下阶失败应回最低档 %d: %+v", floor, got)
	}
	if got := PlanHigherRefineStone(util.NewRand(ok80), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 3}, shield); !got.Modified || got.NewOptionLevel != 4 {
		t.Fatalf("上阶成功应升到 4: %+v", got)
	}
	// 3) 满档 → 无可升 → 不消耗。
	full := &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 13}
	if got := PlanHigherRefineStone(util.NewRand(ok80), cfg, full, shield); got.Modified {
		t.Fatalf("满档不应消耗精炼石: %+v", got)
	}
	// 4) 武器最小伤害守卫：Kris min=6/max=11，和谐 #1 的 L2 boost=4（11-10=1 放行）、
	//    L3 boost=5（11-11=0 <1 拦截）。
	kris, ok := cfg.Item(0, 0)
	if !ok {
		t.Fatal("season6 缺少 Kris (0,0)")
	}
	if got := PlanHigherRefineStone(util.NewRand(ok80), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 1}, kris); !got.Modified || got.NewOptionLevel != 2 {
		t.Fatalf("Kris 升到 2 档不该被守卫拦: %+v", got)
	}
	if got := PlanHigherRefineStone(util.NewRand(ok80), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 2}, kris); got.Modified {
		t.Fatalf("Kris 升到 3 档会把最小伤害抬过最大伤害，应拒绝消耗: %+v", got)
	}
	// 5) 位域口径：三档和谐族都写 HarmonyLevel，Life 写 OptionLevel。
	for _, s := range []ConsumeStrategy{ConsumeStrategyHarmonyJewel, ConsumeStrategyLowerRefineStone, ConsumeStrategyHigherRefineStone} {
		if !StrategyWritesHarmonyLevel(s) {
			t.Fatalf("策略 %d 应写 HarmonyLevel", s)
		}
	}
	if StrategyWritesHarmonyLevel(ConsumeStrategyLifeJewel) {
		t.Fatal("Life 应写 OptionLevel")
	}
	// 6) 分派器与专用入口一致。
	a := PlanOptionJewel(ConsumeStrategyLowerRefineStone, util.NewRand(no20), cfg,
		&item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 1}, shield)
	b := PlanLowerRefineStone(util.NewRand(no20), cfg, &item.Item{Level: 6, HarmonyNumber: 1, HarmonyLevel: 1}, shield)
	if a != b {
		t.Fatalf("分派器与专用入口结果不一致: %+v vs %+v", a, b)
	}
}
