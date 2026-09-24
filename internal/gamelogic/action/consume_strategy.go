// consume_strategy.go —— 消耗品"策略族"的决策纯函数（TRIM-01，doc/16）。
//
// 对照原版 GameLogic/PlayerActions/ItemConsumeActions/：
//   - ItemConsumeAction.cs              （策略查找顺序 + 耐久销毁 + 失败回包）
//   - FruitConsumeHandlerPlugIn.cs      （果实加点/洗点：门槛 → 上限 → 概率 → 点数分布）
//   - UpgradeItemLevelJewel*.cs         （Bless/Soul 升目标物等级）
//   - ItemUpgradeConsumeHandlerPlugIn.cs（Life 蓝选项 / Harmony 和谐选项 增与升）
//   - BaseConsumeHandlerPlugIn.cs       （CheckPreconditions：EnteredWorld 且耐久>0；
//     ConsumeSourceItemAsync：耐久>0 时 -1）
//
// 与 PotionRecipeFor 同构：本层只回答"算什么"；"何时发、发什么包"由 gameserver 编排。
// 查找顺序保持原版：①精确 (Number,Group) 策略（含药水配方）→ ②按物品定义
// Skill/ConsumeEffect 的回退（handler 层）→ ③未注册 → 失败包。
package action

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/util"
)

// ConsumeStrategy 是精确键 (Group, Number) 命中的消耗策略。
type ConsumeStrategy byte

const (
	// ConsumeStrategyFruit 果实（原版 ItemConstants.Fruits 的 (Number=15, Group=13)；
	// Go 键序统一 (Group, Number)，见 consume.go 的 potionRecipes 注释）。
	ConsumeStrategyFruit ConsumeStrategy = iota
	// ConsumeStrategyAntidote 解毒剂（14,8）。
	ConsumeStrategyAntidote
	// ConsumeStrategyTownPortal 瞬间传送卷轴（14,10）。
	ConsumeStrategyTownPortal
	// ConsumeStrategyBlessJewel 祝福宝石强化（14,13）：+0..5、100%。
	ConsumeStrategyBlessJewel
	// ConsumeStrategySoulJewel 灵魂宝石强化（14,14）：+0..8、50%（幸运 +25）。
	ConsumeStrategySoulJewel
	// ConsumeStrategyLifeJewel 生命宝石（14,16）：添加/升级蓝选项，50%，失败移除。
	ConsumeStrategyLifeJewel
	// ConsumeStrategyHarmonyJewel 和谐宝石（14,42）：添加/升级和谐选项，60%，失败不降级。
	ConsumeStrategyHarmonyJewel
	// ConsumeStrategyLowerRefineStone 下阶精炼石（14,43）：只能升已存在的和谐选项，
	// 20%，失败回到该选项的最低档。
	ConsumeStrategyLowerRefineStone
	// ConsumeStrategyHigherRefineStone 上阶精炼石（14,44）：同上，80%。
	ConsumeStrategyHigherRefineStone
)

// consumeStrategyKeys 是 (Group, Number) → 策略的精确注册表。
var consumeStrategyKeys = map[[2]int]ConsumeStrategy{
	{13, 15}: ConsumeStrategyFruit,
	{14, 8}:  ConsumeStrategyAntidote,
	{14, 10}: ConsumeStrategyTownPortal,
	{14, 13}: ConsumeStrategyBlessJewel,
	{14, 14}: ConsumeStrategySoulJewel,
	{14, 16}: ConsumeStrategyLifeJewel,
	{14, 42}: ConsumeStrategyHarmonyJewel,
	{14, 43}: ConsumeStrategyLowerRefineStone,
	{14, 44}: ConsumeStrategyHigherRefineStone,
}

// ConsumeStrategyFor 按物品 (group, number) 查注册的精确策略。
// 对照原版 PlugInManager.GetStrategy(ItemIdentifier(Number, Group)) 的第一次查找。
func ConsumeStrategyFor(group, number int) (ConsumeStrategy, bool) {
	s, ok := consumeStrategyKeys[[2]int{group, number}]
	return s, ok
}

// PoisonEffectNumber 是中毒效果编号（原版 AntidoteConsumeHandlerPlugIn 私有常量 0x37）。
const PoisonEffectNumber = 0x37

// ---------------------------------------------------------------------------
// 果实（FruitConsumeHandlerPlugIn）
// ---------------------------------------------------------------------------

// FruitUsage 是果实用途。线值与 c2s.FruitUsage 逐一对齐（0=加、1=减；
// 原版 C# 的 Undefined/AddPoints/RemovePoints 在封包定义里从 0 起编号）。
type FruitUsage byte

const (
	FruitUsageAddPoints FruitUsage = iota
	FruitUsageRemovePoints
)

// FruitStat 是果实影响的属性（线协议 FruitStatType，与果实物品 Level 0..4 同序：
// 能量/体力/敏捷/力量/统率）。
type FruitStat byte

const (
	FruitStatEnergy FruitStat = iota
	FruitStatVitality
	FruitStatAgility
	FruitStatStrength
	FruitStatLeadership
)

// 果实应答码（C1 2C 的 Result 线值；对照 s2c FruitConsumptionResult 与原版视图映射表）。
const (
	FruitOutcomePlusSuccess             = 0
	FruitOutcomePlusFailed              = 1
	FruitOutcomePlusPrevented           = 2
	FruitOutcomeMinusSuccess            = 3
	FruitOutcomeMinusFailed             = 4
	FruitOutcomeMinusPrevented          = 5
	FruitOutcomePreventedByEquipped     = 16
	FruitOutcomePlusPreventedByMaximum  = 33
	FruitOutcomeMinusPreventedByMaximum = 37
	FruitOutcomeMinusPreventedByDefault = 38
)

// FruitInput 是果实判定所需的全部事实（编排层注入；随机源可换种子）。
type FruitInput struct {
	Usage         FruitUsage
	PlayerLevel   uint16
	ItemLevel     byte // 果实自身等级 0..4 → 目标属性（>4 原版抛异常，这里按拒绝）
	StatAllowed   bool // 职业 StatAttributes 有该基础属性且 IncreasableByPlayer
	HasEquipped   bool // 身上有装备（原版 EquippedItems.Any() → 拒绝）
	FruitPointCap int  // 原版 SelectedCharacter.GetMaximumFruitPoints()（见 MaxFruitPoints）
	UsedAddPoints int  // UsedFruitPoints
	UsedNegPoints int  // UsedNegFruitPoints
	StatValue     int  // 该属性当前基础值
	StatBaseValue int  // 该属性的职业初始值（洗点不得跌破）
	Rand          *util.Rand
}

// FruitResult 是判定结果。Applied → 属性已变化（调用方写回）；Consumed → 果实被消耗
// （扣耐久）；Prevented → 门槛拒绝（原版 return false：除应答包外还回使用失败包）。
type FruitResult struct {
	Outcome   int
	Points    byte
	Stat      FruitStat
	Applied   bool
	Consumed  bool
	Prevented bool
}

// PlanFruit 对照 FruitConsumeHandlerPlugIn.ConsumeItemAsync 的完整判定顺序：
// 等级/果等级 → 职业属性可增 → 无穿戴装备 → 剩余上限 →（减点：不跌破初始值）→
// 成功率掷骰 → 点数分布 → 落账。
func PlanFruit(in FruitInput) FruitResult {
	stat, statOK := fruitStatByLevel(in.ItemLevel)
	add := in.Usage != FruitUsageRemovePoints // 原版 isAdding = fruitUsage != RemovePoints
	prevented := FruitOutcomePlusPrevented
	if !add {
		prevented = FruitOutcomeMinusPrevented
	}
	fail := func(outcome int) FruitResult {
		return FruitResult{Outcome: outcome, Stat: stat, Prevented: true}
	}

	if in.PlayerLevel < 10 || !statOK {
		return fail(prevented)
	}
	if !in.StatAllowed {
		return fail(prevented)
	}
	if in.HasEquipped {
		return fail(FruitOutcomePreventedByEquipped)
	}
	used := in.UsedAddPoints
	if !add {
		used = in.UsedNegPoints
	}
	remaining := in.FruitPointCap - used
	if remaining <= 0 {
		if add {
			return fail(FruitOutcomePlusPreventedByMaximum)
		}
		return fail(FruitOutcomeMinusPreventedByMaximum)
	}
	if !add && in.StatValue <= in.StatBaseValue {
		return fail(FruitOutcomeMinusPreventedByDefault)
	}

	// 成功率按**本次用途已用点数**（原版 GetSuccessPercentage 的 currentUseCount）。
	percentage := 100
	if used > 10 {
		over := float64(used - 10)
		cap10 := float64(in.FruitPointCap)
		switch {
		case over >= cap10*0.8:
			percentage = 50
		case over >= cap10*0.5:
			percentage = 60
		case over >= cap10*0.3:
			percentage = 70
		case over >= cap10*0.1:
			percentage = 80
		default:
			percentage = 90
		}
	}

	if !in.Rand.NextRandomBool(percentage) {
		if add {
			return FruitResult{Outcome: FruitOutcomePlusFailed, Stat: stat, Consumed: true}
		}
		return FruitResult{Outcome: FruitOutcomeMinusFailed, Stat: stat, Consumed: true}
	}

	points := fruitRandomPoints(in.Rand, add)
	if points > remaining {
		points = remaining
	}
	outcome := FruitOutcomeMinusSuccess
	if add {
		outcome = FruitOutcomePlusSuccess
	}
	return FruitResult{Outcome: outcome, Points: byte(points), Stat: stat,
		Applied: true, Consumed: true}
}

// MaxFruitPoints 对照 CharacterExtensions.GetMaximumFruitPoints + GetFruitPoints：
// 按等级查表（表值 = 2 + 每 10 级递增 `3*(m+10)/divisor + 2`），divisor 由职业
// FruitCalculation 决定（Default=400、MagicGladiator=700、DarkLord=500——枚举值
// 1/2，与导出件 character_classes.fruit_calculation 同序）。
// 原版表长 400（等级 >400 会越界异常），这里钳到末档。
func MaxFruitPoints(level, fruitCalculation int) int {
	divisor := 400
	switch fruitCalculation {
	case 1: // FruitCalculationStrategy.MagicGladiator
		divisor = 700
	case 2: // FruitCalculationStrategy.DarkLord
		divisor = 500
	}
	if level < 1 {
		level = 1
	}
	if level > 400 {
		level = 400
	}
	current := 2
	for i := 0; i < level; i++ {
		if (i+1)%10 == 0 {
			current += 3*(i+11)/divisor + 2
		}
	}
	return current
}

// fruitStatByLevel 把果实等级映射到目标属性（与原版 GetStatAttribute 的 switch 一致：
// 0 能量 / 1 体力 / 2 敏捷 / 3 力量 / 4 统率——恰与线协议 FruitStatType 同值）。
func fruitStatByLevel(itemLevel byte) (FruitStat, bool) {
	if itemLevel > 4 {
		return 0, false
	}
	return FruitStat(itemLevel), true
}

// fruitRandomPoints 对照原版 GetRandomPoints：加点 1/2/3（70/25/5%），
// 洗点 1/3/5/7/9（50/25/16/7/2%）。random ∈ [0,100]（原版 NextInt(0, 101)）。
func fruitRandomPoints(r *util.Rand, add bool) int {
	random := r.Next(0, 101)
	if add {
		switch {
		case random < 70:
			return 1
		case random < 95:
			return 2
		}
		return 3
	}
	switch {
	case random < 50:
		return 1
	case random < 75:
		return 3
	case random < 91:
		return 5
	case random < 98:
		return 7
	}
	return 9
}

// ---------------------------------------------------------------------------
// Bless / Soul 宝石升等级（UpgradeItemLevelJewelConsumeHandlerPlugIn）
// ---------------------------------------------------------------------------

// JewelLevelRule 对照 UpgradeItemLevelConfiguration（原版 CreateDefaultConfig 数值）。
type JewelLevelRule struct {
	MinimumLevel         int
	MaximumLevel         int
	SuccessRate          int // 百分比
	LuckBonus            int // 带幸运选项时的加成
	ResetToZeroFromLevel int // 失败时 ≥ 该等级则清零，否则 -1
	// LevelAmount 对照原版 UpgradeItemLevelConfiguration.LevelAmount（默认 1）：
	// 每次成功只升这么多级，且不超过 maximumAllowed − 当前等级。
	LevelAmount int
}

var (
	// blessJewelRule：0→+5、100%、幸运加成 0、失败清零阈值 0（100% 成功故不可达）。
	// RepairTargetItems（受损装备直接修理）为默认空集，同原版缺省配置（裁剪登记）。
	blessJewelRule = JewelLevelRule{MaximumLevel: 5, SuccessRate: 100, LevelAmount: 1}
	// soulJewelRule：0→+8、50%（幸运 +25）、≥+7 失败清零，否则 -1。
	soulJewelRule = JewelLevelRule{MaximumLevel: 8, SuccessRate: 50, LuckBonus: 25, ResetToZeroFromLevel: 7, LevelAmount: 1}
)

// LevelRuleForStrategy 返回强化宝石策略的默认规则（非强化类返回 false）。
// 原版规则可被插件自定义配置覆盖（ISupportCustomConfiguration）；本仓无插件容器，
// 恒用 CreateDefaultConfig 的数值（Bless 的 RepairTargetItems/Allowed/Disallowed
// 均为默认空集，同原版缺省配置）。
func LevelRuleForStrategy(s ConsumeStrategy) (JewelLevelRule, bool) {
	switch s {
	case ConsumeStrategyBlessJewel:
		return blessJewelRule, true
	case ConsumeStrategySoulJewel:
		return soulJewelRule, true
	}
	return JewelLevelRule{}, false
}

// LevelUpgradeResult 是升等级判定。Modified=true → 掷骰已发生（宝石被消耗，等级按
// NewLevel 写回；Success 时耐久刷新为满值，由调用方用 pricing.MaximumDurability 计算）。
type LevelUpgradeResult struct {
	Modified bool
	Success  bool
	NewLevel byte
}

// PlanItemLevelUpgrade 对照 UpgradeItemLevelJewelConsumeHandlerPlugIn.ModifyItem：
// 可升级判定（穿戴件 && 槽位 ≤ WingsSlot(7) && Level < 定义上限）→ 规则区间 →
// LevelAmount(1) 与定义上限取小 → 幸运加成 → 掷骰 → 升级并满耐久 / 失败清零或 -1。
func PlanItemLevelUpgrade(rng *util.Rand, rule JewelLevelRule, target *item.Item, def *config.Item) LevelUpgradeResult {
	if !levelUpgradableByDef(def, target.Level) {
		return LevelUpgradeResult{}
	}
	if int(target.Level) < rule.MinimumLevel {
		return LevelUpgradeResult{}
	}
	maximumAllowed := rule.MaximumLevel + 1
	if def.MaximumItemLevel < maximumAllowed {
		maximumAllowed = def.MaximumItemLevel
	}
	// 原版 levelAmount = Math.Min(LevelAmount, maximumAllowedLevel - item.Level)。
	amount := maximumAllowed - int(target.Level)
	if rule.LevelAmount < amount {
		amount = rule.LevelAmount
	}
	if amount <= 0 {
		return LevelUpgradeResult{}
	}
	percent := rule.SuccessRate
	if target.Luck {
		percent += rule.LuckBonus
	}
	if rng.NextRandomBool(percent) {
		return LevelUpgradeResult{Modified: true, Success: true, NewLevel: byte(int(target.Level) + amount)}
	}
	if int(target.Level) >= rule.ResetToZeroFromLevel {
		return LevelUpgradeResult{Modified: true, NewLevel: 0}
	}
	nl := int(target.Level) - 1
	if nl < 0 {
		nl = 0
	}
	return LevelUpgradeResult{Modified: true, NewLevel: byte(nl)}
}

// levelUpgradableByDef 是原版 Item.CanLevelBeUpgraded 的"定义侧"判据
// （有 ≤ WingsSlot 的装备槽——排除宠物/坐骑槽；且未达定义等级上限）。
func levelUpgradableByDef(def *config.Item, level byte) bool {
	if def == nil || !def.IsWearable() || int(level) >= def.MaximumItemLevel {
		return false
	}
	for _, s := range def.Slots {
		if s <= wingsSlotThreshold {
			return true
		}
	}
	return false
}

// wingsSlotThreshold 对照原版 InventoryConstants.WingsSlot（=7）。
const wingsSlotThreshold = 7

// ---------------------------------------------------------------------------
// Life / Harmony 选项宝石（ItemUpgradeConsumeHandlerPlugIn）
// ---------------------------------------------------------------------------

// OptionUpgradeResult 是 Life/Harmony 共用的判定输出：
//   - Modified=false → 宝石**不被**消耗（原版 ModifyItem 返回 false）；
//   - NewOptionLevel：-1 = 不变；≥0 = 写回的新选项等级（Life 写 OptionLevel，
//     Harmony 升级路径写 HarmonyLevel；Life 失败移除 = 0）；
//   - HarmonyAdd ≠ nil → 首次添加和谐选项：写 HarmonyNumber/HarmonyLevel。
type OptionUpgradeResult struct {
	Modified       bool
	NewOptionLevel int
	HarmonyAdd     *HarmonyAdd
}

// HarmonyAdd 是"首次添加"的和谐选项（编号按候选 Weight 加权抽取；等级取 0——
// 原版 ItemOptionLink 创建后不显式设 Level，语义档由 RequiredItemLevel 门槛控制）。
type HarmonyAdd struct {
	Number int
	Level  int
}

// lifeUpgradeConfig / harmonyUpgradeConfig 对照原版两个插件构造的
// ItemUpgradeConfiguration（选项类型、add/increase、成功率、失败行为）。
// 注意原版 Harmony 的 increasesOption=**false**：和谐宝石只会"首次添加"，
// 升档是 Higher Harmony Jewel 的职责（未注册）——已持有和谐选项时宝石不消耗。
var (
	lifeUpgradeConfig    = optionJewelConfig{kind: config.OptionKindOption, adds: true, increases: true, chance: 50, fail: optionFailRemove}
	harmonyUpgradeConfig = optionJewelConfig{kind: config.OptionKindHarmony, adds: true, increases: false, chance: 60, fail: optionFailNone}
	// 两档精炼石：adds=false（没有和谐选项时宝石不消耗）、increases=true、
	// 失败回最低档，并带武器最小伤害守卫（RefineStoneUpgradeConsumeHandlerPlugIn）。
	lowerRefineConfig  = optionJewelConfig{kind: config.OptionKindHarmony, adds: false, increases: true, chance: 20, fail: optionFailBaseLevel, refineGuard: true}
	higherRefineConfig = optionJewelConfig{kind: config.OptionKindHarmony, adds: false, increases: true, chance: 80, fail: optionFailBaseLevel, refineGuard: true}
)

// optionFailResult 对照原版 ItemUpgradeConsumeHandlerPlugIn.ItemFailResult 三态。
type optionFailResult byte

const (
	optionFailNone optionFailResult = iota
	optionFailBaseLevel
	optionFailRemove
)

type optionJewelConfig struct {
	kind        string
	adds        bool // 无该选项时是否尝试添加（AddsOption）
	increases   bool // 已有该选项时是否升级（IncreasesOption）
	chance      int  // 成功百分比（原版 SuccessChance 0.5/0.6 的百分数口径）
	fail        optionFailResult
	refineGuard bool // 精炼石专属：武器最小伤害不得被抬过最大伤害
}

// PlanLifeJewel 对照 LifeJewelConsumeHandlerPlugIn（adds+increases、50%、失败 RemoveOption）：
//   - 定义无蓝选项候选 → 未消耗；
//   - 已有 → 有可升档（更高 ldo 且 RequiredItemLevel ≤ 物品等级）才消耗：
//     成功 Level+1，失败移除（Go 位域模型 OptionLevel=0）；
//   - 没有 → 掷骰即消耗：成功从候选池随机取一条写入 Level=1（原版
//     `optionLink.Level = 1` 恒为 1），失败仅消耗。
//
// 蓝选项候选恒取定义第一条（Number 0，口径同 player.item_option_powerup 注释）；
// 翅膀件（WingOptionNumber>0）与 OptionLevel 共位域，原版 Life 也按同类型升级，保持同构。
func PlanLifeJewel(rng *util.Rand, cfg *config.GameConfig, target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	return planOptionJewel(rng, cfg, lifeUpgradeConfig, target, targetDef)
}

// PlanHarmonyJewel 对照 HarmonyJewelConsumeHandlerPlugIn（60%、失败 FailResult=None）：
//   - 远古件不可有和谐选项（原版 ItemCanHaveOption 的 IsAncient 拒绝）；
//   - 定义无和谐候选 → 未消耗；
//   - 已有和谐选项 → **未消耗**（原版 increasesOption=false，升档属 Higher Harmony）；
//   - 没有：掷骰消耗；成功从候选池（RequiredItemLevel 过滤 + 力量/敏捷需求剔除 +
//     Weight 加权）抽一条写入；池空 → 未消耗。
func PlanHarmonyJewel(rng *util.Rand, cfg *config.GameConfig, target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	if target.AncientDiscriminator != 0 {
		return OptionUpgradeResult{}
	}
	return planOptionJewel(rng, cfg, harmonyUpgradeConfig, target, targetDef)
}

// planOptionJewel 是 Life/Harmony 的公共流程（差异全在 optionJewelConfig 与
// 取"当前等级/写回位置"的位域访问器上）。
func planOptionJewel(rng *util.Rand, cfg *config.GameConfig, jc optionJewelConfig, target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	ids := cfg.ItemOptionDefinitionsFor(targetDef.Group, targetDef.Number)
	d := cfg.DefinitionOfOptionType(ids, jc.kind)
	if d == nil || len(d.PossibleOptions) == 0 {
		return OptionUpgradeResult{}
	}
	has := optionJewelCurrent(cfg, jc, target, d)
	if has != nil {
		// 原版 TryUpgradeItemOption 的 !IncreasesOption 早退：宝石不被消耗。
		if !jc.increases {
			return OptionUpgradeResult{}
		}
		// 精炼石专属守卫：下一档最小物理伤害会把武器 min 抬过 max-1 → 不消耗。
		if jc.refineGuard && refineBlockedByWeaponMinDamage(has, currentLevelOf(jc, target), targetDef) {
			return OptionUpgradeResult{}
		}
		if !hasHigherOptionLevel(has, currentLevelOf(jc, target), int(target.Level)) {
			return OptionUpgradeResult{}
		}
		if rng.NextRandomBool(jc.chance) {
			return OptionUpgradeResult{Modified: true, NewOptionLevel: currentLevelOf(jc, target) + 1}
		}
		switch jc.fail {
		case optionFailRemove:
			return OptionUpgradeResult{Modified: true, NewOptionLevel: 0}
		case optionFailBaseLevel:
			return OptionUpgradeResult{Modified: true, NewOptionLevel: minOptionLevel(has)}
		default:
			return OptionUpgradeResult{Modified: true, NewOptionLevel: -1}
		}
	}
	if !jc.adds {
		return OptionUpgradeResult{}
	}
	if !rng.NextRandomBool(jc.chance) {
		// 原版 TryAddItemOption：掷骰失败仍 return true（宝石消耗，物品不变）。
		return OptionUpgradeResult{Modified: true, NewOptionLevel: -1}
	}
	// 候选池过滤对两种宝石共用（原版 Where 在分支之前）：无 ldo 或存在
	// RequiredItemLevel ≤ 物品等级的档。
	pool := optionPool(d, int(target.Level))
	if jc.kind == config.OptionKindHarmony {
		pool = filterRequirementReductions(pool, targetDef)
		if len(pool) == 0 {
			return OptionUpgradeResult{}
		}
		pick := selectWeighted(rng, pool)
		// 原版 optionLink.Level = 该选项各档 Level 的最小值（无档时 0）。
		return OptionUpgradeResult{Modified: true, NewOptionLevel: -1,
			HarmonyAdd: &HarmonyAdd{Number: pick.Number, Level: minOptionLevel(pick)}}
	}
	if len(pool) == 0 {
		return OptionUpgradeResult{}
	}
	// 原版 Life 分支：SelectRandom + Level 恒 1。
	if len(pool) > 1 {
		pool = []*config.IncreasableItemOptionExport{pool[rng.Next(0, len(pool))]}
	}
	return OptionUpgradeResult{Modified: true, NewOptionLevel: 1}
}

// optionJewelCurrent 返回目标物品当前的选项候选（未持有返回 nil）。
func optionJewelCurrent(cfg *config.GameConfig, jc optionJewelConfig, target *item.Item, d *config.ItemOptionDefinitionExport) *config.IncreasableItemOptionExport {
	if jc.kind == config.OptionKindHarmony {
		if target.HarmonyNumber == 0 {
			return nil
		}
		for i := range d.PossibleOptions {
			if d.PossibleOptions[i].Number == int(target.HarmonyNumber) {
				return &d.PossibleOptions[i]
			}
		}
		// 位域里的编号不在候选定义中（数据异常）：按"不可升档"处理（返回 nil 会让
		// 流程改走"添加"分支并覆盖旧值，与原版 First(...)==null 时的 NRE 语义都不同，
		// 这里保守拒绝消耗）。
		return &config.IncreasableItemOptionExport{}
	}
	if target.OptionLevel == 0 {
		return nil
	}
	return &d.PossibleOptions[0]
}

// currentLevelOf 读取位域模型里的当前选项等级。
func currentLevelOf(jc optionJewelConfig, target *item.Item) int {
	if jc.kind == config.OptionKindHarmony {
		return int(target.HarmonyLevel)
	}
	return target.OptionLevel
}

// hasHigherOptionLevel 报告是否存在"比当前等级更高且物品等级达标"的档位
// （原版 TryUpgradeItemOption 的 higherOptionPossible）。
func hasHigherOptionLevel(o *config.IncreasableItemOptionExport, level, itemLevel int) bool {
	for _, l := range o.LevelDependentOptions {
		if l.Level > level && l.RequiredItemLevel <= itemLevel {
			return true
		}
	}
	return false
}

// optionPool 是原版 TryAddItemOption 的候选过滤：无 LevelDependentOptions、或存在
// RequiredItemLevel ≤ 物品等级的档才可选。
func optionPool(d *config.ItemOptionDefinitionExport, itemLevel int) []*config.IncreasableItemOptionExport {
	var pool []*config.IncreasableItemOptionExport
	for i := range d.PossibleOptions {
		o := &d.PossibleOptions[i]
		if len(o.LevelDependentOptions) > 0 {
			ok := false
			for _, l := range o.LevelDependentOptions {
				if l.RequiredItemLevel <= itemLevel {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		pool = append(pool, o)
	}
	return pool
}

// filterRequirementReductions 剔除"力量/敏捷需求减少"候选（对照原版 Harmony 分支
// 的注释：Str and agi reduction options are not always applicable）——目标物品定义
// 不带对应 Total * Requirement Value 需求时，该候选不可用。
func filterRequirementReductions(pool []*config.IncreasableItemOptionExport, targetDef *config.Item) []*config.IncreasableItemOptionExport {
	out := pool
	if !defRequiresAttr(targetDef, "Total Strength Requirement Value") {
		out = dropFirstTargeting(out, "Strength Requirement reduction")
	}
	if !defRequiresAttr(targetDef, "Total Agility Requirement Value") {
		out = dropFirstTargeting(out, "Agility Requirement reduction")
	}
	return out
}

// dropFirstTargeting 移除**第一条**加成目标为该属性的候选（原版 FirstOrDefault+Remove，
// 一条规则至多剔一条）。
func dropFirstTargeting(pool []*config.IncreasableItemOptionExport, target string) []*config.IncreasableItemOptionExport {
	for i, o := range pool {
		if optionTargetsAttribute(o, target) {
			return append(pool[:i:i], pool[i+1:]...)
		}
	}
	return pool
}

// minOptionLevel 取选项各档 Level 的最小值（无档时 0，对照原版 `Min() ?? 0`）。
func minOptionLevel(o *config.IncreasableItemOptionExport) int {
	min := -1
	for _, l := range o.LevelDependentOptions {
		if min < 0 || l.Level < min {
			min = l.Level
		}
	}
	if min < 0 {
		return 0
	}
	return min
}

// defRequiresAttr 报告物品定义是否带指定属性的需求（Requirement.Attribute 按
// designation 匹配，口径同 handler_item_consume.requiredCharacterValue 的表）。
func defRequiresAttr(def *config.Item, designation string) bool {
	for _, r := range def.Requirements {
		if r.Attribute == designation {
			return true
		}
	}
	return false
}

// optionTargetsAttribute 报告选项（含各等级档）的加成目标是否为该属性。
func optionTargetsAttribute(o *config.IncreasableItemOptionExport, target string) bool {
	if o.PowerUp != nil && o.PowerUp.Target == target {
		return true
	}
	for _, l := range o.LevelDependentOptions {
		if l.PowerUp != nil && l.PowerUp.Target == target {
			return true
		}
	}
	return false
}

// selectWeighted 对照原版 possibleOptions.SelectWeightedRandom(Weight)：
// 权重和内均匀取点、按累进命中；全零权重退化为取第一条（原版行为一致）。
func selectWeighted(rng *util.Rand, pool []*config.IncreasableItemOptionExport) *config.IncreasableItemOptionExport {
	total := 0
	for _, o := range pool {
		if o.Weight > 0 {
			total += o.Weight
		}
	}
	if total == 0 {
		return pool[0]
	}
	roll := rng.Next(0, total)
	for _, o := range pool {
		if o.Weight <= 0 {
			continue
		}
		if roll < o.Weight {
			return o
		}
		roll -= o.Weight
	}
	return pool[len(pool)-1]
}

// ---------------------------------------------------------------------------
// 精炼石直强化（RefineStoneUpgradeConsumeHandlerPlugIn + Lower/Higher 两个子类）
// ---------------------------------------------------------------------------

// 三件属性的 designation（不用 GUID：导出件的属性 GUID 每次重导会变，见 doc/17 附录）。
const (
	attrMinPhysBaseDmg         = "Minimum Physical Base Damage"
	attrMinPhysBaseDmgByWeapon = "Minimum Physical Base Damage By Weapon"
	attrMaxPhysBaseDmgByWeapon = "Maximum Physical Base Damage By Weapon"
)

// PlanLowerRefineStone 对照 LowerRefineStoneConsumeHandlerPlugIn（20%、失败回最低档）。
func PlanLowerRefineStone(rng *util.Rand, cfg *config.GameConfig, target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	return planOptionJewel(rng, cfg, lowerRefineConfig, target, targetDef)
}

// PlanHigherRefineStone 对照 HigherRefineStoneConsumeHandlerPlugIn（80%、失败回最低档）。
func PlanHigherRefineStone(rng *util.Rand, cfg *config.GameConfig, target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	return planOptionJewel(rng, cfg, higherRefineConfig, target, targetDef)
}

// PlanOptionJewel 按策略分派"选项类宝石"的判定（Life / Harmony / 两档精炼石）。
func PlanOptionJewel(strategy ConsumeStrategy, rng *util.Rand, cfg *config.GameConfig,
	target *item.Item, targetDef *config.Item) OptionUpgradeResult {
	switch strategy {
	case ConsumeStrategyLifeJewel:
		return PlanLifeJewel(rng, cfg, target, targetDef)
	case ConsumeStrategyHarmonyJewel:
		return PlanHarmonyJewel(rng, cfg, target, targetDef)
	case ConsumeStrategyLowerRefineStone:
		return PlanLowerRefineStone(rng, cfg, target, targetDef)
	case ConsumeStrategyHigherRefineStone:
		return PlanHigherRefineStone(rng, cfg, target, targetDef)
	}
	return OptionUpgradeResult{}
}

// StrategyWritesHarmonyLevel 报告该策略的等级写回哪个位域（和谐族写 HarmonyLevel，
// Life 写 OptionLevel）。
func StrategyWritesHarmonyLevel(strategy ConsumeStrategy) bool {
	switch strategy {
	case ConsumeStrategyHarmonyJewel, ConsumeStrategyLowerRefineStone, ConsumeStrategyHigherRefineStone:
		return true
	}
	return false
}

// refineBlockedByWeaponMinDamage 对照 RefineStoneUpgradeConsumeHandlerPlugIn.cs:22-36：
// 该和谐选项的第一档加的是"最小物理伤害"时，下一档的加成不能把武器最小伤害抬到
// 超过最大伤害（原版判据 `max - (min + boost) < 1` → 拒绝，宝石不消耗）。
// 只作用于武器（非武器物品查不到 By Weapon 基础值 → 不拦，与原版 First(...) 取不到即 NRE
// 的区别见 doc/16 的登记）。
func refineBlockedByWeaponMinDamage(opt *config.IncreasableItemOptionExport, curLevel int,
	targetDef *config.Item) bool {
	if opt == nil || len(opt.LevelDependentOptions) == 0 || targetDef == nil {
		return false
	}
	first := opt.LevelDependentOptions[0].PowerUp
	if first == nil || first.Target != attrMinPhysBaseDmg {
		return false
	}
	var boost float64
	found := false
	for _, l := range opt.LevelDependentOptions {
		if l.Level == curLevel+1 && l.PowerUp != nil {
			boost = l.PowerUp.Boost.Constant
			found = true
			break
		}
	}
	if !found {
		return false
	}
	minDmg, okMin := weaponBaseDamage(*targetDef, attrMinPhysBaseDmgByWeapon)
	maxDmg, okMax := weaponBaseDamage(*targetDef, attrMaxPhysBaseDmgByWeapon)
	if !okMin || !okMax {
		return false
	}
	return maxDmg-(minDmg+boost) < 1
}

// weaponBaseDamage 取武器定义里某基础属性的裸值（不含等级加成表——原版读的也是
// BasePowerUpAttributes 的 BaseValue）。
func weaponBaseDamage(def config.Item, designation string) (float64, bool) {
	for _, a := range def.BasePowerUpAttributes {
		if a.Target == designation {
			return a.BaseValue, true
		}
	}
	return 0, false
}
