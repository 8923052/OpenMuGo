package action

// crafting_tickets.go —— 活动券合成，对应 PlayerActions/Craftings/BaseEventTicketCrafting.cs
// 与三个子类（恶魔广场 / 血Castle / 幻术神庙）。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity/item"
)

const chaosItemName = "Jewel of Chaos"

// ticketSpec 是子类差异部分（两件同名活动物 + 混沌 → 一张等级同输入物的券）。
type ticketSpec struct {
	item1, item2, result string
	priceByLevel         map[int]int
	defaultPrice         int
	rateByLevel          int
	rateBelowLevel       int // >0 时按等级分档取率（低于该档用 rateUnder）
	rateUnder            int
	reject               int
}

var (
	devilSquareTicket = ticketSpec{
		item1: "Devil's Eye", item2: "Devil's Key", result: "Devil's Invitation",
		priceByLevel: map[int]int{2: 200000, 3: 400000, 4: 700000, 5: 1100000, 6: 1600000, 7: 2000000},
		defaultPrice: 100000, rateBelowLevel: 5, rateByLevel: 70, rateUnder: 80,
	}
	bloodCastleTicket = ticketSpec{
		item1: "Scroll of Archangel", item2: "Blood Bone", result: "Invisibility Cloak",
		priceByLevel: map[int]int{2: 80000, 3: 150000, 4: 250000, 5: 400000, 6: 600000, 7: 850000, 8: 1050000},
		defaultPrice: 50000, rateByLevel: 80, reject: CraftIncorrectBloodCastle,
	}
	illusionTempleTicket = ticketSpec{
		item1: "Old Scroll", item2: "Illusion Sorcerer Covenant", result: "Scroll of Blood",
		priceByLevel: map[int]int{2: 5000000, 3: 7000000, 4: 9000000, 5: 11000000, 6: 13000000},
		defaultPrice: 3000000, rateByLevel: 70,
	}
)

type ticketCrafter struct{ spec ticketSpec }

// required 对照 BaseEventTicketCrafting.TryGetRequiredItems：两件活动物必须同等级，且都要在容器里。
func (t ticketCrafter) required(ctx *craftCtx, storage []MixCandidate) ([]itemLink, byte, int) {
	c1 := findByName(ctx, storage, t.spec.item1)
	c2 := findByName(ctx, storage, t.spec.item2)
	chaos := findByName(ctx, storage, chaosItemName)
	reject := t.spec.reject
	if reject == 0 {
		reject = CraftIncorrectMix
	}
	if c1 == nil || c2 == nil || chaos == nil || c1.It.Level != c2.It.Level {
		return nil, 0, reject
	}
	links := []itemLink{
		singleLink(*c1), singleLink(*c2), singleLink(*chaos),
	}
	rate := t.eventRate(int(c1.It.Level))
	return links, byte(rate), craftNoReject
}

func (t ticketCrafter) price(ctx *craftCtx, rate byte, links []itemLink) int64 {
	return int64(t.priceByLevel(eventLevelOf(links, ctx, t.spec.item1)))
}

func (t ticketCrafter) results(ctx *craftCtx, links []itemLink, rate byte) []*item.Item {
	def := findDefByName(ctx.cfg, t.spec.result)
	if def == nil {
		return nil
	}
	created := &item.Item{Group: byte(def.Group), Number: def.Number}
	created.Level = byte(eventLevelOf(links, ctx, t.spec.item1))
	created.Durability = 1
	return []*item.Item{created}
}

// eventRate 对照各子类的 GetSuccessRate（恶魔广场按等级分档，其余固定）。
func (t ticketCrafter) eventRate(level int) int {
	if t.spec.rateBelowLevel > 0 {
		if level < t.spec.rateBelowLevel {
			return t.spec.rateUnder
		}
		return t.spec.rateByLevel
	}
	return t.spec.rateByLevel
}

func (t ticketCrafter) priceByLevel(level int) int {
	if p, ok := t.spec.priceByLevel[level]; ok {
		return p
	}
	return t.spec.defaultPrice
}

// singleLink 将一件候选包成"必消"的投入链接（原版 TransientItemCraftingRequiredItem 默认值）。
func singleLink(c MixCandidate) itemLink {
	return itemLink{
		items: []*item.Item{c.It},
		req:   config.CraftingRequired{MinAmount: 1, MaxAmount: 1, SuccessResult: config.MixDisappear, FailResult: config.MixDisappear},
	}
}

func findByName(ctx *craftCtx, storage []MixCandidate, name string) *MixCandidate {
	for i := range storage {
		if storage[i].Def != nil && storage[i].Def.Name == name {
			return &storage[i]
		}
	}
	return nil
}

func findDefByName(cfg *config.GameConfig, name string) *config.Item {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Items {
		if cfg.Items[i].Name == name {
			return &cfg.Items[i]
		}
	}
	return nil
}

// eventLevelOf 取活动物等级的参照实现：按名字找回对应链接里的第一件（原版 First(ri => ...Name == item1)）。
func eventLevelOf(links []itemLink, ctx *craftCtx, name string) int {
	for _, l := range links {
		for _, it := range l.items {
			if def := itemDef(ctx.cfg, it); def != nil && def.Name == name {
				return int(it.Level)
			}
		}
	}
	return 0
}
