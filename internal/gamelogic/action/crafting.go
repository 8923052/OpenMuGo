package action

// crafting.go —— 合成执行引擎，对应 OpenMU PlayerActions/Items/BaseItemCraftingHandler.cs
// （流程与投入物处理）与 SimpleItemCraftingHandler.cs（数据驱动的判定/产出）。
// 特殊配方按原版类名分派到本包的 crafting_*.go；出站与容器操作在 gameserver。

import (
	"sort"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/pricing"
)

// 结果码，数值对齐线上枚举 s2c.CraftingResult（注意 5 不在线上使用）。
const (
	CraftFailed               = 0
	CraftSuccess              = 1
	CraftNotEnoughMoney       = 2
	CraftTooManyItems         = 3
	CraftLackingMixItems      = 6
	CraftIncorrectMix         = 7
	CraftIncorrectBloodCastle = 10
)

// craftNoReject 是"未被拒绝"的哨兵值（0 已被 CraftFailed 占用）。
const craftNoReject = -1

// Roll 注入随机源（返回 [0,n)），语义与 util.Rand.Next 一致。
type Roll func(n int) int

// MixCandidate 是临时容器里的一件候选物品（携带定义）。
type MixCandidate struct {
	It  *item.Item
	Def *config.Item
}

// MixPlan 是一次合成的执行计划：调用方据此扣款、移除 Consume、放入 Created，
// Kept 是就地改造的原有物品（仍在容器内，只需刷新编码）。
type MixPlan struct {
	Code    int
	Rate    byte
	Price   int64
	Consume []*item.Item
	Created []*item.Item
	Kept    []*item.Item
	// Result 是回给客户端的那一件（原版 resultItems.LastOrDefault()）。
	Result *item.Item
}

// CraftRequest 是评估一次合成的输入。
type CraftRequest struct {
	Craft      *config.Crafting
	Storage    []MixCandidate
	Cfg        *config.GameConfig
	Roll       Roll
	Money      uint64
	SocketSlot byte
}

// craftCtx 是执行期的上下文（对应原版 handler 构造期注入的 settings + 每次调用的 player）。
type craftCtx struct {
	cfg        *config.GameConfig
	craft      *config.Crafting
	roll       Roll
	socketSlot byte
}

// itemLink 是一条投入要求与其匹配到的物品（对照 CraftingRequiredItemLink）。
type itemLink struct {
	items []*item.Item
	req   config.CraftingRequired
}

// crafter 是配方执行器的三个抽象点（对照 BaseItemCraftingHandler 的三个抽象方法）。
type crafter interface {
	required(ctx *craftCtx, storage []MixCandidate) (links []itemLink, rate byte, reject int)
	price(ctx *craftCtx, rate byte, links []itemLink) int64
	results(ctx *craftCtx, links []itemLink, rate byte) []*item.Item
}

// DecideCraft 复刻 DoMixAsync 的顺序：匹配投入 → 定价 → 金额 → 掷骰 → 处理投入物 → 产出。
// 金额不足必须在掷骰与任何物品改动之前返回（原版即如此，失败时钱照扣、掷骰后才动物）。
func DecideCraft(req CraftRequest) MixPlan {
	plan := MixPlan{Code: CraftFailed}
	h := crafterFor(req.Craft)
	if h == nil {
		return plan
	}
	ctx := &craftCtx{cfg: req.Cfg, craft: req.Craft, roll: req.Roll, socketSlot: req.SocketSlot}
	links, rate, reject := h.required(ctx, append([]MixCandidate(nil), req.Storage...))
	if reject != craftNoReject {
		plan.Code = reject
		return plan
	}
	plan.Rate, plan.Price = rate, h.price(ctx, rate, links)
	if plan.Price > 0 && req.Money < uint64(plan.Price) {
		plan.Code = CraftNotEnoughMoney
		return plan
	}
	if !nextRandomBool(req.Roll, int(rate)) {
		plan.Consume = applyMixResults(ctx, links, false)
		return plan
	}
	plan.Code = CraftSuccess
	plan.Consume = applyMixResults(ctx, links, true)
	created := h.results(ctx, links, rate)
	if len(created) > 0 {
		plan.Result = created[len(created)-1]
	}
	plan.Created, plan.Kept = splitResults(created, links)
	return plan
}

// splitResults 把产出分为"新建"与"就地改造"两类（后者本就在容器里，不能重复放入）。
func splitResults(items []*item.Item, links []itemLink) (created, kept []*item.Item) {
	inStorage := make(map[*item.Item]bool)
	for _, l := range links {
		for _, it := range l.items {
			inStorage[it] = true
		}
	}
	for _, it := range items {
		if it == nil {
			continue
		}
		if inStorage[it] {
			kept = append(kept, it)
		} else {
			created = append(created, it)
		}
	}
	return created, kept
}

// applyMixResults 按 MixResult 处理每条链接的物品（对照 RequiredItemChangeAsync），返回消失的件。
func applyMixResults(ctx *craftCtx, links []itemLink, success bool) []*item.Item {
	var gone []*item.Item
	for _, l := range links {
		kind := l.req.FailResult
		if success {
			kind = l.req.SuccessResult
		}
		for _, it := range l.items {
			switch kind {
			case config.MixDisappear:
				gone = append(gone, it)
			case config.MixChaosDowngrade:
				downgradeChaos(ctx, it)
			case config.MixWingsDowngrade:
				downgradeWings(ctx, it)
			}
		}
	}
	return gone
}

// downgradeChaos 对照 ChaosWeaponAndFirstWingsDowngradedRandom：等级随机降到 [0, 原等级)，
// 非卓越且有技能时 50% 掉技能，普通选项 50% 降一档（1 档则清掉），耐久按比例重算。
func downgradeChaos(ctx *craftCtx, it *item.Item) {
	prevLevel := int(it.Level)
	prevMax := int(maxDurabilityOfOnePiece(ctx.cfg, it))
	def := itemDef(ctx.cfg, it)
	newLevel := nextInt(ctx.roll, 0, prevLevel+1)
	if newLevel > prevLevel {
		newLevel = prevLevel
	}
	it.Level = byte(newLevel)
	if it.HasSkill && !isExcellentItem(it) && nextRandomBool(ctx.roll, 50) {
		it.HasSkill = false
	}
	if it.OptionLevel > 0 && nextRandomBool(ctx.roll, 50) {
		it.OptionLevel--
	}
	it.Durability = byte(rescaleDurability(ctx.cfg, def, it, prevMax))
	_ = def
}

// downgradeWings 对照 ThirdWingsDowngradedRandom：等级 -2 或 -3（原版是 byte 运算，会回绕），
// 普通选项**无条件**清除，耐久取满。
func downgradeWings(ctx *craftCtx, it *item.Item) {
	dec := 3
	if nextRandomBool(ctx.roll, 50) {
		dec = 2
	}
	it.Level -= byte(dec) // 原版是 byte 运算，低等级会回绕
	it.OptionLevel = 0
	it.Durability = maxDurabilityOfOnePiece(ctx.cfg, it)
}

// rescaleDurability 按"新满值 × 现耐久 / 旧满值"重算（原版整数除法）。
func rescaleDurability(cfg *config.GameConfig, def *config.Item, it *item.Item, prevMax int) int {
	newMax := int(maxDurabilityOfOnePiece(cfg, it))
	if prevMax <= 0 {
		return int(it.Durability)
	}
	return newMax * int(it.Durability) / prevMax
}

func maxDurabilityOfOnePiece(cfg *config.GameConfig, it *item.Item) byte {
	if def := itemDef(cfg, it); def != nil {
		return pricing.MaximumDurability(def, it)
	}
	return it.Durability
}

func itemDef(cfg *config.GameConfig, it *item.Item) *config.Item {
	if cfg == nil || it == nil {
		return nil
	}
	def, _ := cfg.Item(int(it.Group), it.Number)
	return def
}

func isExcellentItem(it *item.Item) bool {
	return it != nil && (it.ExcellentBits != 0 || it.FenrirBits != 0)
}

func isAncientItem(it *item.Item) bool {
	return it != nil && it.AncientDiscriminator != 0
}

func isGuardianItem(it *item.Item) bool {
	return it != nil && it.GuardianOption
}

func nextRandomBool(roll Roll, percent int) bool {
	if roll == nil {
		return false
	}
	switch percent {
	case 0:
		return false
	case 100:
		return true
	}
	return roll(100) <= percent
}

func nextInt(roll Roll, min, maxPlus1 int) int {
	if roll == nil || maxPlus1 <= min {
		return min
	}
	return min + roll(maxPlus1-min)
}

// pickRandom 等价原版 SelectRandom()：空集返回 0,false。
func pickRandom(roll Roll, n int) (int, bool) {
	if n <= 0 {
		return 0, false
	}
	return nextInt(roll, 0, n), true
}

// sortRequiredByMinAmountDesc 对应 OrderByDescending(MinimumAmount)（稳定排序，保持数据序）。
func sortRequiredByMinAmountDesc(reqs []config.CraftingRequired) []config.CraftingRequired {
	out := append([]config.CraftingRequired(nil), reqs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].MinAmount > out[j].MinAmount })
	return out
}
