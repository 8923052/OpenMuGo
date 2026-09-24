// consume.go —— 消耗品使用（C3 0x26）的决策纯函数（T2-6）。
//
// 对照原版：
//   - PlayerActions/ItemConsumeActions/ItemConsumeAction.cs（策略查找 + 耐久 + 销毁）
//   - ItemConsumeActions/RecoverConsumeHandlerPlugIn.cs（恢复公式与分段）
//   - ItemConsumeActions/{Apple,Small|Medium|Large}{Health,Mana,Shield,Complex}Potion*.cs
//   - GameLogic/ItemConstants.cs（(Number, Group) 键；注意构造参数序是 (Number, Group)）
//
// 本层只做"算什么"，不做"何时发"：分段恢复的时间轴由 handler 驱动，出站由 view 层映射。
package action

// PotionKind 是药水种类（原版各 *ConsumeHandlerPlugIn 的 Key 分类）。
type PotionKind byte

const (
	PotionNone PotionKind = iota
	PotionApple
	PotionHealth
	PotionMana
	PotionShield
	PotionComplex
)

// RecoverTarget 是被恢复的属性（原版 RecoverConsumeHandlerPlugIn.CurrentAttribute）。
type RecoverTarget byte

const (
	RecoverNoTarget RecoverTarget = iota
	RecoverHealth                 // Stats.CurrentHealth
	RecoverMana                   // Stats.CurrentMana
	RecoverShield                 // Stats.CurrentShield
)

// RecoverStepSpec 是一段分段恢复（对照原版 RecoverStep）。
type RecoverStepSpec struct {
	DelayMs    int
	Percentage int // 占 totalRecoverAmount 的百分比；各段之和应为 100
}

// RecoverPart 是"某一项属性的恢复参数"（对照原版一份 RecoverConsumeHandlerConfiguration
// + 该 handler 的 Current/MaximumAttribute）。复合药水由**两个** Part 组成。
type RecoverPart struct {
	Target                                 RecoverTarget
	TotalRecoverPercentage                 float64
	AdditionalRecoverMinusCharacterLevel   float64
	RecoverPercentageIncreaseByPotionLevel float64
	RecoverDelayReductionByPotionLevel     float64
	Steps                                  []RecoverStepSpec
}

// PotionRecipe 是一个药水类物品的完整恢复配方。
type PotionRecipe struct {
	Kind PotionKind
	// Parts 的顺序 = 原版调用顺序（ComplexPotion 先 Health 后 Shield，两者**并发**推进）。
	Parts []RecoverPart
	// CooldownMs 对应原版 RecoverConsumeHandlerConfiguration.CooldownTime（0.5s），
	// 落在 player.PotionCooldownUntil 上；冷却期内再次使用直接失败。
	CooldownMs int
}

// potionSteps 是 ManaHealth / Shield 两类默认配置共用的三段恢复（20/60/20 @ 200/600/200ms）。
var potionSteps = []RecoverStepSpec{
	{DelayMs: 200, Percentage: 20},
	{DelayMs: 600, Percentage: 60},
	{DelayMs: 200, Percentage: 20},
}

// 所有药水的冷却时间都是 0.5s（两类默认配置一致）。
const potionCooldownMs = 500

// healthPart 按 ManaHealthConsumeHandlerPlugIn 的默认配置生成一项生命/法力恢复段。
//
//	TotalRecoverPercentage = Multiplier * 10
//	AdditionalRecoverMinusCharacterLevel = (Multiplier + 1) * 50
//	RecoverPercentageIncreaseByPotionLevel = 1；RecoverDelayReductionByPotionLevel = 1/16
func healthPart(target RecoverTarget, multiplier int) RecoverPart {
	return RecoverPart{
		Target:                                 target,
		TotalRecoverPercentage:                 float64(multiplier * 10),
		AdditionalRecoverMinusCharacterLevel:   float64((multiplier + 1) * 50),
		RecoverPercentageIncreaseByPotionLevel: 1,
		RecoverDelayReductionByPotionLevel:     1.0 / 16.0,
		Steps:                                  potionSteps,
	}
}

// shieldPart 按 ShieldPotionConsumeHandlerPlugIn 的默认配置生成一项护盾恢复段。
// 注意它**没有** AdditionalRecoverMinusCharacterLevel（=0，故额外恢复恒为 0）。
func shieldPart(percentage float64) RecoverPart {
	return RecoverPart{
		Target:                                 RecoverShield,
		TotalRecoverPercentage:                 percentage,
		RecoverPercentageIncreaseByPotionLevel: 1,
		RecoverDelayReductionByPotionLevel:     1.0 / 16.0,
		Steps:                                  potionSteps,
	}
}

// potionRecipes 是 (Group, Number) → 配方的静态表（对照 ItemConstants 与各 handler）。
//
// 键序是 (Group, Number)——原版 ItemIdentifier 的构造参数序是 (Number, Group)，
// 极易写反；这里统一按"输出 JSON 里的 group/number 顺序"书写以免误读。
var potionRecipes = map[[2]int]PotionRecipe{
	{14, 0}:  {Kind: PotionApple, Parts: []RecoverPart{healthPart(RecoverHealth, 0)}, CooldownMs: potionCooldownMs},
	{14, 1}:  {Kind: PotionHealth, Parts: []RecoverPart{healthPart(RecoverHealth, 1)}, CooldownMs: potionCooldownMs},
	{14, 2}:  {Kind: PotionHealth, Parts: []RecoverPart{healthPart(RecoverHealth, 2)}, CooldownMs: potionCooldownMs},
	{14, 3}:  {Kind: PotionHealth, Parts: []RecoverPart{healthPart(RecoverHealth, 3)}, CooldownMs: potionCooldownMs},
	{14, 4}:  {Kind: PotionMana, Parts: []RecoverPart{healthPart(RecoverMana, 1)}, CooldownMs: potionCooldownMs},
	{14, 5}:  {Kind: PotionMana, Parts: []RecoverPart{healthPart(RecoverMana, 2)}, CooldownMs: potionCooldownMs},
	{14, 6}:  {Kind: PotionMana, Parts: []RecoverPart{healthPart(RecoverMana, 3)}, CooldownMs: potionCooldownMs},
	{14, 35}: {Kind: PotionShield, Parts: []RecoverPart{shieldPart(20)}, CooldownMs: potionCooldownMs},
	{14, 36}: {Kind: PotionShield, Parts: []RecoverPart{shieldPart(40)}, CooldownMs: potionCooldownMs},
	{14, 37}: {Kind: PotionShield, Parts: []RecoverPart{shieldPart(100)}, CooldownMs: potionCooldownMs},
	{14, 38}: {Kind: PotionComplex, Parts: []RecoverPart{healthPart(RecoverHealth, 1), shieldPart(20)}, CooldownMs: potionCooldownMs},
	{14, 39}: {Kind: PotionComplex, Parts: []RecoverPart{healthPart(RecoverHealth, 2), shieldPart(40)}, CooldownMs: potionCooldownMs},
	{14, 40}: {Kind: PotionComplex, Parts: []RecoverPart{healthPart(RecoverHealth, 3), shieldPart(100)}, CooldownMs: potionCooldownMs},
}

// PotionRecipeFor 按物品 (group, number) 查药水配方。
//
// 对照原版 ItemConsumeAction 的策略查找顺序：先精确 (Number, Group)，再回退 (null, Group)。
// 本表只覆盖 group 14 的瓶类药水；果实、宝石、解毒剂、传送卷轴等其它消耗策略登记在
// consume_strategy.go（TRIM-01），技能书/卷轴与 ConsumeEffect 由 handler 按物品定义分派。
func PotionRecipeFor(group, number int) (PotionRecipe, bool) {
	r, ok := potionRecipes[[2]int{group, number}]
	return r, ok
}

// RecoverAmount 是一段恢复：等待 DelayMs 后施加 Amount（DelayMs=0 表示立即）。
type RecoverAmount struct {
	DelayMs int
	Amount  float64
}

// PlanRecovery 对照原版 RecoverConsumeHandlerPlugIn.RecoverAsync：
//
//	recoverPercentage = TotalRecoverPercentage + itemLevel × IncreaseByPotionLevel
//	additional        = max(0, AdditionalRecoverMinusCharacterLevel − charLevel)
//	total             = maxValue × recoverPercentage / 100 + additional
//
// 分段判据与原版逐字一致：`RecoverSteps.Count == 0 || delayReduction >= 1` → 一次性；
// 其中 delayReduction = DelayReductionByPotionLevel × itemLevel。药水等级 0..15 时
// delayReduction < 1，因此**默认走三段**；等级 ≥16 才是一次性。
//
// maxValue 为该项属性的最大值（Maximum Health / Mana / Shield）。
func PlanRecovery(p RecoverPart, maxValue float64, itemLevel int, charLevel float64) []RecoverAmount {
	percentage := p.TotalRecoverPercentage + float64(itemLevel)*p.RecoverPercentageIncreaseByPotionLevel
	additional := p.AdditionalRecoverMinusCharacterLevel - charLevel
	if additional < 0 {
		additional = 0
	}
	total := maxValue*percentage/100.0 + additional

	delayReduction := p.RecoverDelayReductionByPotionLevel * float64(itemLevel)
	if len(p.Steps) == 0 || delayReduction >= 1 {
		return []RecoverAmount{{DelayMs: 0, Amount: total}}
	}
	out := make([]RecoverAmount, 0, len(p.Steps))
	for _, s := range p.Steps {
		out = append(out, RecoverAmount{
			DelayMs: int(float64(s.DelayMs) * (1.0 - delayReduction)),
			Amount:  total * float64(s.Percentage) / 100.0,
		})
	}
	return out
}

// RecoverBatch 是"同一时刻施加的一组恢复"（按目标属性聚合）。
//
// 为什么要合并：原版把四项当前属性（血/盾/法力/BP）与攻速/魔速归到**同一个** UpdateAction，
// 由 UpdateStatsBasePlugIn 以 16ms 窗口合并发送——同一时刻的多项变化只出一包 C1 26 FF。
// 复合药水的生命段与护盾段是并发推进的，于是每"步"也只出一包（而不是两包）。
type RecoverBatch struct {
	DelayMs int
	// Amounts 为本次要施加到各目标属性的增量。用 map 以便同目标多段相加。
	Amounts map[RecoverTarget]float64
}

// PlanPotionRecovery 把配方的全部 Part 合并到统一时间轴上（见 RecoverBatch 的说明）。
//
// maxima 为各目标属性的最大值（Maximum Health/Mana/Shield）。返回的批次按时间升序；
// 单个 Part 的一次性恢复（药水等级 ≥16）会退化成唯一一批（DelayMs=0）。
func PlanPotionRecovery(r PotionRecipe, maxima map[RecoverTarget]float64, itemLevel int, charLevel float64) []RecoverBatch {
	if len(r.Parts) == 0 {
		return nil
	}
	plans := make([][]RecoverAmount, len(r.Parts))
	maxSteps := 0
	for i, p := range r.Parts {
		plans[i] = PlanRecovery(p, maxima[p.Target], itemLevel, charLevel)
		if len(plans[i]) > maxSteps {
			maxSteps = len(plans[i])
		}
	}
	out := make([]RecoverBatch, 0, maxSteps)
	for step := 0; step < maxSteps; step++ {
		batch := RecoverBatch{Amounts: map[RecoverTarget]float64{}}
		for i, pl := range plans {
			if step >= len(pl) {
				continue
			}
			if pl[step].DelayMs > batch.DelayMs {
				batch.DelayMs = pl[step].DelayMs
			}
			batch.Amounts[r.Parts[i].Target] += pl[step].Amount
		}
		out = append(out, batch)
	}
	return out
}

// ApplyRecover 对照原版 `player.Attributes[Current] = (uint)Math.Min(Maximum, Current + amount)`。
//
// 与原版的唯一差异是负值钳到 0：C# 的 `(uint)` 转换遇到负 double 是未定义行为（unchecked
// 下回绕成巨大值），而入参 amount ≥ 0、current ≥ 0 使该分支不可达——这里取保守值。
func ApplyRecover(current, maximum, amount float64) uint32 {
	v := current + amount
	if v > maximum {
		v = maximum
	}
	if v < 0 {
		v = 0
	}
	return uint32(v)
}

// ItemsNeedingSkill 报告该物品是否属于"技能书/卷轴"分支（原版 ItemConsumeAction 的
// `item.Definition.Skill != null && !item.IsWearable()` → ItemConstants.AllScrolls）。
// 已接入（TRIM-01）：handler 会把命中此判据的物品派给 handleLearnSkillScroll
// （技能书 group15、技能石 group12——后者技能号 = 定义技能号 + 物品等级）。
func ItemsNeedingSkill(hasSkill, wearable bool) bool { return hasSkill && !wearable }
