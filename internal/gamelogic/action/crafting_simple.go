package action

// crafting_simple.go —— 数据驱动配方的执行器，对应 SimpleItemCraftingHandler.cs。
// 各特殊配方只覆写原版那几个 virtual 方法（见 optionHooks 与 crafting_*.go）。

import (
	"strings"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/pricing"
)

// optionHooks 对应原版 SimpleItemCraftingHandler 的 virtual 覆写点。
type optionHooks struct {
	itemOption   func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte)
	luck         func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte)
	skill        func(ctx *craftCtx, it *item.Item, def *config.Item, rate byte)
	excellent    func(ctx *craftCtx, it *item.Item, def *config.Item)
	matches      func(ctx *craftCtx, c MixCandidate, req config.CraftingRequired) bool
	afterRequire func(ctx *craftCtx, links []itemLink, rate byte) int
	price        func(ctx *craftCtx, links []itemLink) int64
	results      func(ctx *craftCtx, links []itemLink, rate byte) []*item.Item
}

type simpleCrafter struct{ hooks optionHooks }

// crafterFor 按原版 ItemCraftingHandlerClassName 分派（数据里是全限定名，取末段）。
func crafterFor(craft *config.Crafting) crafter {
	if craft == nil {
		return nil
	}
	switch shortHandlerName(craft.Handler) {
	case "":
		return simpleCrafter{}
	case "ChaosWeaponAndFirstWingsCrafting":
		return simpleCrafter{hooks: chaosWingsHooks}
	case "SecondWingsCrafting":
		return simpleCrafter{hooks: secondWingsHooks}
	case "ThirdWingsCrafting":
		return simpleCrafter{hooks: thirdWingsHooks}
	case "DarkHorseCrafting":
		return simpleCrafter{hooks: darkHorseHooks}
	case "DinorantCrafting":
		return simpleCrafter{hooks: dinorantHooks}
	case "GuardianOptionCrafting":
		return simpleCrafter{hooks: guardianHooks}
	case "RefineStoneCrafting":
		return simpleCrafter{hooks: refineHooks}
	case "RestoreItemCrafting":
		return simpleCrafter{hooks: restoreHooks}
	case "SeedSphereCrafting":
		return simpleCrafter{hooks: seedSphereHooks}
	case "MountSeedSphereCrafting":
		return simpleCrafter{hooks: mountSeedHooks}
	case "RemoveSeedSphereCrafting":
		return simpleCrafter{hooks: removeSeedHooks}
	case "DevilSquareTicketCrafting":
		return ticketCrafter{spec: devilSquareTicket}
	case "BloodCastleTicketCrafting":
		return ticketCrafter{spec: bloodCastleTicket}
	case "IllusionTempleTicketCrafting":
		return ticketCrafter{spec: illusionTempleTicket}
	case "FenrirUpgradeCrafting":
		return fenrirUpgradeCrafter{}
	}
	return nil
}

func shortHandlerName(full string) string {
	if i := strings.LastIndexByte(full, '.'); i >= 0 {
		return full[i+1:]
	}
	return full
}

// required 对应 TryGetRequiredItems：按 MinAmount 降序匹配，逐条累加成功率（**每步按 byte 回绕**，
// 原版的负加成在低费率配方上会溢出成高概率，这是要照抄的行为）。
func (s simpleCrafter) required(ctx *craftCtx, storage []MixCandidate) ([]itemLink, byte, int) {
	st := ctx.craft.Settings
	if st == nil {
		return nil, 0, CraftFailed
	}
	rate := st.SuccessPercent
	var totalOldPrice int64
	var links []itemLink
	left := storage
	for _, req := range sortRequiredByMinAmountDesc(st.Required) {
		found, rest := collectMatches(ctx, s.hooks.matches, left, req)
		count := 0
		for _, c := range found {
			count += candidateAmount(c)
		}
		if count < req.MinAmount {
			return nil, 0, CraftLackingMixItems
		}
		if req.MaxAmount > 0 && count > req.MaxAmount {
			return nil, 0, CraftTooManyItems
		}
		if st.NpcPriceDivisor > 0 {
			totalOldPrice += sumOldBuyingPrice(ctx.cfg, found)
		} else {
			rate += int(toByte(req.AddPercentage * (count - req.MinAmount)))
			if req.NpcPriceDivisor > 0 {
				rate += int(toByte(int(sumBuyingPrice(ctx.cfg, found) / int64(req.NpcPriceDivisor))))
			}
			for _, c := range found {
				rate = applyItemAdditions(st, c, rate)
			}
		}
		links = append(links, itemLink{items: itemsOf(found), req: req})
		left = rest
	}
	if len(left) > 0 {
		return nil, 0, CraftIncorrectMix
	}
	if totalOldPrice > 0 && st.NpcPriceDivisor > 0 {
		rate = int(toByte(int(totalOldPrice / int64(st.NpcPriceDivisor))))
	}
	if reject := s.afterRequire(ctx, links, byte(rate)); reject != craftNoReject {
		return nil, 0, reject
	}
	return links, clampRate(rate, st.MaxSuccessPercent), craftNoReject
}

// afterRequire 让子类在"匹配完成"后追加校验（原版 Dinorant/Guardian/Mount 的 override）。
func (s simpleCrafter) afterRequire(ctx *craftCtx, links []itemLink, rate byte) int {
	if s.hooks.afterRequire == nil {
		return craftNoReject
	}
	return s.hooks.afterRequire(ctx, links, rate)
}

func (s simpleCrafter) price(ctx *craftCtx, rate byte, links []itemLink) int64 {
	if s.hooks.price != nil {
		return s.hooks.price(ctx, links)
	}
	st := ctx.craft.Settings
	return int64(st.Money) + int64(st.MoneyPerSuccessPercent)*int64(rate)
}

// results 对应 CreateOrModifyResultItemsAsync：Any 时随机一条产出，All 时全出；
// Reference>0 就地升级（耐久按新满值重算），否则新建并逐个附加选项。
func (s simpleCrafter) results(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
	if s.hooks.results != nil {
		return s.hooks.results(ctx, links, rate)
	}
	st := ctx.craft.Settings
	picks := st.ResultItems
	if st.ResultSelect != "All" {
		if i, ok := pickRandom(ctx.roll, len(picks)); ok {
			picks = []config.CraftingResult{picks[i]}
		} else {
			return nil
		}
	}
	var out []*item.Item
	for _, res := range picks {
		if res.Reference > 0 {
			for _, it := range itemsOfLink(links, res.Reference) {
				out = append(out, upgradeInPlace(ctx.cfg, it, res.AddLevel))
			}
			continue
		}
		if res.Item == nil {
			continue
		}
		def, ok := ctx.cfg.Item(res.Item.Group, res.Item.Number)
		if !ok {
			continue
		}
		for _, it := range createResultItems(ctx, s.hooks, st, links, res, def, rate) {
			out = append(out, it)
		}
	}
	return out
}

// upgradeInPlace 对照 Reference>0 分支：先取旧满值 → 加等级 → 按比例重算耐久。
func upgradeInPlace(cfg *config.GameConfig, it *item.Item, addLevel int) *item.Item {
	prevMax := int(maxDurabilityOfOnePiece(cfg, it))
	it.Level += byte(addLevel) // 原版是 byte 加法（+15 之上会回绕，与 C# 一致）
	it.Durability = byte(rescaleDurability(cfg, itemDef(cfg, it), it, prevMax))
	return it
}

// createResultItems 对照 CreateResultItemsAsync：MultipleAllowed 时按引用件数产出多件。
func createResultItems(ctx *craftCtx, hooks optionHooks, st *config.CraftingSettings, links []itemLink,
	res config.CraftingResult, def *config.Item, rate byte) []*item.Item {
	count := 1
	if st.MultipleAllowed {
		if ref := firstReferenceAmount(links); ref > 0 {
			count = ref
		}
	}
	out := make([]*item.Item, 0, count)
	for i := 0; i < count; i++ {
		it := &item.Item{Group: byte(def.Group), Number: def.Number}
		if isFruit(def) {
			it.Level = byte(weightedLevel(ctx.roll, []int{30, 25, 20, 20, 5}))
		} else {
			it.Level = byte(nextInt(ctx.roll, res.MinLevel, res.MaxLevel+1))
		}
		if res.Durability != nil {
			it.Durability = byte(*res.Durability)
		} else {
			it.Durability = maxDurabilityOfOnePiece(ctx.cfg, it)
		}
		applyResultOptions(ctx, hooks, st, it, def, byte(rate))
		out = append(out, it)
	}
	return out
}

// applyResultOptions 按原版顺序附加：幸运 → 普通选项 → 技能 → 卓越（各步可被子类覆写）。
func applyResultOptions(ctx *craftCtx, hooks optionHooks, st *config.CraftingSettings, it *item.Item, def *config.Item, rate byte) {
	if hooks.luck != nil {
		hooks.luck(ctx, it, def, rate)
	} else if st.ResultLuckChance > 0 && nextRandomBool(ctx.roll, st.ResultLuckChance) &&
		firstOptionOfKind(ctx.cfg, def, config.OptionKindLuck) != nil {
		it.Luck = true
	}
	if hooks.itemOption != nil {
		hooks.itemOption(ctx, it, def, rate)
	}
	if hooks.skill != nil {
		hooks.skill(ctx, it, def, rate)
	} else if st.ResultSkillChance > 0 && nextRandomBool(ctx.roll, st.ResultSkillChance) {
		if def.SkillNumber != nil && !it.HasSkill {
			it.HasSkill = true
		}
	}
	if hooks.excellent != nil {
		hooks.excellent(ctx, it, def)
	} else {
		addRandomExcellent(ctx, st, it, def)
	}
}

// addRandomExcellent 对照 AddRandomExcellentOptions：第一条起每条都掷一次概率，
// 命中即取一个尚未拥有的卓越项；定义自带技能时卓越件必然带技能。
func addRandomExcellent(ctx *craftCtx, st *config.CraftingSettings, it *item.Item, def *config.Item) {
	if st.ResultExcChance <= 0 {
		return
	}
	optDef := excellentOptionDefinition(ctx.cfg, def)
	if optDef == nil {
		return
	}
	for j := 0; j < optDef.MaximumOptionsPerItem && nextRandomBool(ctx.roll, st.ResultExcChance); j++ {
		var candidates []config.IncreasableItemOptionExport
		for _, o := range optDef.PossibleOptions {
			if !itemHasOptionBit(ctx.cfg, it, &o) {
				candidates = append(candidates, o)
			}
		}
		i, ok := pickRandom(ctx.roll, len(candidates))
		if !ok {
			return
		}
		writeOptionBit(ctx.cfg, it, &candidates[i])
		if def.SkillNumber != nil {
			it.HasSkill = true // 卓越物品总是带技能（原版同注释）
		}
	}
}

// excellentOptionDefinition 取"含卓越或翅膀选项"的第一个选项定义（原版 FirstOrDefault）。
func excellentOptionDefinition(cfg *config.GameConfig, def *config.Item) *config.ItemOptionDefinitionExport {
	for _, id := range cfg.ItemOptionDefinitionsFor(def.Group, def.Number) {
		d, ok := cfg.ItemOptionDefinition(id)
		if !ok {
			continue
		}
		if d.HasKind(cfg, config.OptionKindExcellent) || d.HasKind(cfg, config.OptionKindWing) {
			return d
		}
	}
	return nil
}

// collectMatches 按要求从剩余候选里挑出匹配件，返回（匹配, 剩余）。
func collectMatches(ctx *craftCtx, matcher func(*craftCtx, MixCandidate, config.CraftingRequired) bool,
	storage []MixCandidate, req config.CraftingRequired) (matched, rest []MixCandidate) {
	for _, c := range storage {
		ok := requiredItemMatches(ctx.cfg, c, req)
		if ok && matcher != nil {
			ok = matcher(ctx, c, req)
		}
		if ok {
			matched = append(matched, c)
		} else {
			rest = append(rest, c)
		}
	}
	return matched, rest
}

// requiredItemMatches 对照 RequiredItemMatches：定义集（空=任意）+ 等级区间 + 必备选项。
func requiredItemMatches(cfg *config.GameConfig, c MixCandidate, req config.CraftingRequired) bool {
	if c.It == nil || c.Def == nil {
		return false
	}
	if len(req.PossibleItems) > 0 && !containsRef(req.PossibleItems, int(c.It.Group), c.It.Number) {
		return false
	}
	if int(c.It.Level) < req.MinLevel || int(c.It.Level) > req.MaxLevel {
		return false
	}
	for _, kind := range req.RequiredOptions {
		if !itemHasOptionKind(cfg, c.It, kind) {
			return false
		}
	}
	return true
}

func containsRef(refs []config.ItemRef, group, number int) bool {
	for _, r := range refs {
		if r.Group == group && r.Number == number {
			return true
		}
	}
	return false
}

// candidateAmount 对照 IsStackable() ? Durability : 1（堆叠物的"件数"就是耐久值）。
func candidateAmount(c MixCandidate) int {
	if c.Def != nil && !c.Def.IsWearable() && c.Def.Durability > 1 {
		return int(c.It.Durability)
	}
	return 1
}

func itemsOf(cands []MixCandidate) []*item.Item {
	out := make([]*item.Item, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.It)
	}
	return out
}

// itemsOfLink 取指定 reference 的投入物（原版按 ItemRequirement.Reference 查）。
// 注意 reference==0 表示"无引用"，不能当通配匹配。
func itemsOfLink(links []itemLink, reference int) []*item.Item {
	for _, l := range links {
		if l.req.Reference == reference && reference != 0 {
			return l.items
		}
	}
	return nil
}

func firstReferenceAmount(links []itemLink) int {
	for _, l := range links {
		if l.req.Reference > 0 {
			return len(l.items)
		}
	}
	return 0
}

func sumBuyingPrice(cfg *config.GameConfig, cands []MixCandidate) int64 {
	var sum int64
	for _, c := range cands {
		sum += pricing.FinalBuyingPrice(c.Def, c.It)
	}
	return sum
}

func sumOldBuyingPrice(cfg *config.GameConfig, cands []MixCandidate) int64 {
	var sum int64
	for _, c := range cands {
		sum += pricing.FinalOldBuyingPrice(c.Def, c.It)
	}
	return sum
}

// applyItemAdditions 逐条套用带符号的 add_*（原式每步都 (byte) 截断 → 会回绕）。
func applyItemAdditions(st *config.CraftingSettings, c MixCandidate, rate int) int {
	it := c.It
	if st.AddLuck != 0 && it.Luck {
		rate = int(toByte(rate + st.AddLuck))
	}
	if st.AddExcellent != 0 && isExcellentItem(it) {
		rate = int(toByte(rate + st.AddExcellent))
	}
	if st.AddAncient != 0 && isAncientItem(it) {
		rate = int(toByte(rate + st.AddAncient))
	}
	if st.AddGuardian != 0 && isGuardianItem(it) {
		rate = int(toByte(rate + st.AddGuardian))
	}
	if st.AddSocket != 0 && it.SocketCount > 0 {
		rate = int(toByte(rate + st.AddSocket))
	}
	return rate
}

func clampRate(rate, maxPercent int) byte {
	if maxPercent > 0 && rate > maxPercent {
		rate = maxPercent
	}
	if rate > 100 {
		rate = 100
	}
	return toByte(rate)
}

// toByte 复刻 C# 的显式 (byte) 转换：按 256 取模（负值同样回绕）。
func toByte(v int) byte { return byte(uint(v) & 0xFF) }

// weightedLevel 对照 SelectWeightedRandom(weights)：按权重累计在 [0,sum) 里取值取下标。
func weightedLevel(roll Roll, weights []int) int {
	sum := 0
	for _, w := range weights {
		sum += w
	}
	if sum <= 0 {
		return 0
	}
	target := nextInt(roll, 0, sum)
	for i, w := range weights {
		if target < w {
			return i
		}
		target -= w
	}
	return len(weights) - 1
}

// isFruit 对照 ItemConstants.Fruits（果实等级按权重掷 0..4，而非结果表的随机区间）。
func isFruit(def *config.Item) bool { return def.Group == 13 && def.Number == 15 }

// itemHasOptionBit 判定物品位域里是否已含该候选（卓越按位、翅膀按编号、其余按 kind）。
func itemHasOptionBit(cfg *config.GameConfig, it *item.Item, o *config.IncreasableItemOptionExport) bool {
	if it == nil || o == nil {
		return false
	}
	switch optionKind(cfg, o) {
	case config.OptionKindWing:
		return it.WingOptionNumber == o.Number && o.Number > 0
	case config.OptionKindExcellent:
		return o.Number >= 1 && o.Number <= 6 && it.ExcellentBits&(1<<uint(o.Number-1)) != 0
	}
	if o.Number >= 1 && o.Number <= 6 && it.ExcellentBits&(1<<uint(o.Number-1)) != 0 {
		return true
	}
	return it.WingOptionNumber == o.Number && it.WingOptionNumber > 0
}
