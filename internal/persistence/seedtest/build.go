package seedtest

// build.go —— 规格到实体的装配（BuildSpecs / buildCharacter / applyItems）。
// 物品按 (group,number) 查导出件定义取宽高/耐久；外观 Equipment 同步预编码。
// 装备区槽（0..11）单格直放；网格槽 FitsInside 检查失败时回退 AddToFree。

import (
	"fmt"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/view/remote"
)

// BuildSpecs 把全部账号规格装配为实体（cfg 提供物品定义与经验表）。
// cfg 为 nil 时物品宽高按 1×1、经验按 0（测试脚手架可传 nil）。
func BuildSpecs(cfg *config.GameConfig) []*entity.Account {
	specs := fullAccounts()
	accounts := make([]*entity.Account, 0, len(specs))
	for _, spec := range specs {
		acc := &entity.Account{
			Name:       spec.name,
			Password:   spec.name, // OpenMU：测试账号密码与账号名相同
			Characters: make([]entity.Character, 0, len(spec.classes)),
		}
		if spec.gm {
			acc.State = entity.AccountStateGameMaster
		}
		for _, cs := range spec.classes {
			acc.Characters = append(acc.Characters, buildCharacter(cfg, spec, cs))
		}
		accounts = append(accounts, acc)
	}
	return accounts
}

// buildCharacter 装配一个角色实体（名字/职业/槽位/坐标/外观/属性/背包）。
func buildCharacter(cfg *config.GameConfig, spec *seedSpec, cs seedClass) entity.Character {
	charName := spec.name + cs.suffix
	mapNumber := uint16(0)
	x1, y1, x2, y2 := lorenciaX1, lorenciaY1, lorenciaX2, lorenciaY2
	if cs.noria {
		mapNumber = noriaNumber
		x1, y1, x2, y2 = noriaX1, noriaY1, noriaX2, noriaY2
	}
	// OpenMU 在出生门矩形内随机落点；这里用确定性散列（同账号同职业可复现）。
	seed := len(spec.name)*7 + int(cs.slot)*3
	x := byte(x1 + seed%int(x2-x1+1))
	y := byte(y1 + (seed*3+5)%int(y2-y1+1))

	// 属性：等级点数公式与职业基值——真实链路里 ResolveCharStats 会按 Base* 覆盖
	// 重新解析，这里给基线；Base* 直设/增量按规格叠加。
	st := player.NewCharStats(cs.class, spec.level)
	st.Money = testMoney
	// 原版 LevelUpPoints = (Level-1) × PointsPerLevelUp；职业 7/13/17/25 每级 7 点。
	per := uint32(5)
	switch cs.class {
	case 13, 17, 25:
		per = 7
	}
	if spec.level > 1 {
		st.LevelUpPoints = uint16((uint32(spec.level) - 1) * per)
	}
	applyStat := func(cur *uint16, base, add uint16) {
		if base > 0 {
			*cur = base
		}
		*cur += add
	}
	applyStat(&st.Strength, cs.str, cs.addStr)
	applyStat(&st.Agility, cs.agi, cs.addAgi)
	applyStat(&st.Vitality, cs.vit, cs.addVit)
	applyStat(&st.Energy, cs.ene, cs.addEne)
	applyStat(&st.Leadership, cs.cmd, 0)
	if spec.gm {
		// GameMaster.CreateAccount：LevelUpPoints = 20000（覆盖上面公式值）。
		st.LevelUpPoints = 20000
	}
	// 当前值留 0：NewCharStats 的 Maximum 是旧线性公式产物，灌进 Current 会被
	// 属性系统解析出的真实 Maximum 覆盖前就"污染"Current（真机修复 11 复测发现
	// DK 92B Current=84=旧公式值）。enterWorld 对 Current==0 的角色统一置满
	// （真实属性系统 Maximum），语义 = "满状态进图"。

	// 扩展页数：容器容量与"报给客户端的页数"必须同源（原版二者都读
	// character.InventoryExtensions —— InventoryStorage 构造 + F3 03 的
	// CharacterInformation.InventoryExtensions 字节）。缺了后者，客户端不画扩展页。
	st.InventoryExtensions = cs.invExt
	inv := storage.NewInventory(int(cs.invExt))
	c := entity.Character{
		Slot:        cs.slot,
		Name:        charName,
		Level:       spec.level,
		ClassNumber: cs.class,
		MapNumber:   mapNumber,
		X:           x,
		Y:           y,
		Stats:       st,
		Inventory:   inv,
		// 大师点数（原版测试账号注释："To test master skill tree"）。
		MasterLevelUpPoints: cs.masterPoints,
	}
	if spec.gm {
		c.Status = entity.CharacterStatusGameMaster
	}

	// 外观：按装备清单预编码（与物品落袋同一来源）。
	// ⚠️ Equipment 必须按**槽位索引**展开（编码器 eq(slot) 按索引取值，
	// append 压缩排列会让"槽位 1 的物品"被当成"槽位 0"——真机修复 11 的
	// "外观乱堆"根因）：固定 12 槽，空槽 nil。
	ap := &item.Appearance{ClassNumber: int(cs.class), GameMaster: spec.gm}
	ap.Equipment = make([]*item.Equip, 12)
	for _, si := range cs.items {
		if si.slot <= slotPet {
			ap.Equipment[si.slot] = &item.Equip{
				Number: int(si.number), Group: si.group, Level: si.level,
				Excellent: si.excellentBits != 0 || si.fullOpt, Ancient: si.ancient != 0,
				BlackFenrir: si.fenrir&1 != 0, BlueFenrir: si.fenrir&2 != 0, GoldFenrir: si.fenrir&4 != 0,
			}
		}
	}
	preview := make([]byte, remote.AppearanceSize)
	remote.EncodeAppearance(ap, preview)
	ext := make([]byte, remote.AppearanceExtSize)
	remote.EncodeAppearanceExt(ap, ext)
	c.Appearance = preview
	c.AppearanceExt = ext

	// 物品落袋。
	applyItems(cfg, inv, cs.items)
	return c
}

// applyItems 把物品清单放进背包（装备区直放；网格 FitsInside 失败回退找空位）。
func applyItems(cfg *config.GameConfig, inv *storage.Inventory, items []seedItem) {
	for _, si := range items {
		it := &item.Item{
			Group:       si.group,
			Number:      int(si.number),
			Level:       si.level,
			HasSkill:    si.skill,
			Luck:        si.luck,
			OptionLevel: int(si.option),
		}
		switch {
		case si.fullOpt:
			it.ExcellentBits = 0x3F // CreateFullOptionJewellery：全部 PossibleOptions 逐条挂
		case si.excellentBits != 0:
			it.ExcellentBits = si.excellentBits // 单条卓越（原版 TargetExcellentOption→bit=Number-1）
		}
		switch {
		case si.fenrir&1 != 0:
			it.FenrirBits = 1
		case si.fenrir&2 != 0:
			it.FenrirBits = 2
		case si.fenrir&4 != 0:
			it.FenrirBits = 4
		}
		if si.ancient != 0 {
			// 原版 FullAncient：bonus option level 2 + 套件 discriminator（线上 Ancient 字节）。
			it.AncientBonusLevel = 2
			it.AncientDiscriminator = si.ancient
		}
		w, h := 1, 1
		dur := si.dur
		if cfg != nil {
			if def, ok := cfg.Item(int(si.group), int(si.number)); ok {
				w, h = def.Width, def.Height
				if dur == 0 {
					dur = byte(def.Durability)
				}
			} else {
				// 定义缺失：跳过并保底（种子不应引用不存在物品；定义缺失属数据漂移）。
				continue
			}
		}
		it.Durability = dur
		slotted := &storage.SlottedItem{It: it, Width: byte(w), Height: byte(h)}
		if !inv.AddToSlot(si.slot, slotted) {
			// 网格撞位：回退顺序找空位（OpenMU AddItemAsync 语义）。
			if !inv.AddToFree(slotted) {
				panic(fmt.Sprintf("seedtest: 物品 (%d,%d) 无处安放 slot=%d", si.group, si.number, si.slot))
			}
		}
	}
}
