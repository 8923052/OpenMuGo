package seedtest

// full.go —— OpenMU 测试账号全集（TestAccountsInitialization.cs 的对齐复刻）：
//
//	test0..test9   等级 1/11/…/91（LowLevel：4 基础职业 + 皮甲套/武器 + 宝石药水 + 宠物）
//	test300        等级 300（Level300：4 职业各 +13 套装/翅膀/芬里尔）
//	test400        等级 400（Level400：+15 套装/AA 武器/芬里尔）
//	ancient        等级 330（Ancient：全套远古）
//	socket         等级 380（Socket：镶嵌套装，装备精简为对照件）
//	quest1/2/3     等级 150/220/400（QuestBase：转职任务道具）
//	testgm / testgm2  GM 400（全部技能、GM 状态、20000 点）
//	testunlock     GM 0（解锁全部可创建职业）
//
// 行为一致性原则：测试账号与真实账号走**同一条链路**——同一 entity.Account/
// Character 实体、同一 MemoryStore 认证、同一进图属性解析（ResolveCharStats 从
// Base* 覆盖派生）。种子只填实体字段，不设任何特殊开关；唯一与 OpenMU 的差异是
// 属性由属性系统按 Base* 覆盖实时派生（等价于原版 StatAttribute 持久化）。
//
// 物品对照：AccountInitializerBase*.cs 的 Create* 字面量 → (group, number)。
// 裁剪登记：①转职任务状态（CharacterQuestState）随任务系统补；②AddAllSkills 的
// 技能列表落库待技能持久化（testgm 先以 GM 状态表示）。
// 大师点数（TRIM-09）已按原样落在角色实体上：test400 五个角色、testgm 的 Dark Lord、
// testgm2 的 Summoner 各 100 点（原版 GameMaster2 的另一条是 Fist Master，本仓未种）。

import (
	"fmt"

	"mugo/internal/gamelogic/player"
)

// 装备区槽位（InventoryConstants）。
const (
	slotLeftHand = 0
	slotHelm     = 2
	slotArmor    = 3
	slotPants    = 4
	slotGloves   = 5
	slotBoots    = 6
	slotWings    = 7
	slotPet      = 8
	// gridFirst 是背包网格首槽（装备区 12 槽之后）。
	gridFirst = 12
)

// 组号（ItemGroups / Misc1 / Misc2）。
const (
	groupWeapon = 0
	groupStaff  = 5
	groupOrb    = 12 // Orbs（含翅膀 30..43、Cape）
	groupMisc1  = 13 // 首饰/票/宠（Ring/Pendant/Fenrir 37/宠物 0..5）
	groupMisc2  = 14 // 宝石/药水/箭
)

// 常用定义号（AccountInitializerBase 私有字面量）。
const (
	jewelBless   = 13 // (14,13)
	jewelSoul    = 14 // (14,14)
	jewelLife    = 16 // (14,16)
	jewelCreate  = 22 // (14,22)
	chaosNumber  = 15 // Chaos Jewel = (12,15)
	aleNumber    = 9  // Alcohol = (14,9)
	arrowsNumber = 15 // (4,15)
	petDinorant  = 3
	petFenrir    = 37 // (13,37)
	petHorse     = 4  // (13,4)
	petRaven     = 5  // (13,5)
	bcTicketN    = 18 // (13,18) Blood Castle Ticket
	dsTicketN    = 19 // (14,19) Devil Square Ticket
	wingsCape    = 30 // (12,30) Cape of Lord
)

// seedItem 描述一件种子物品（对照 Create* 系列）。
type seedItem struct {
	slot          byte // 背包容器槽（装备区 0..11 或网格 12..）
	group         byte
	number        byte
	level         byte
	dur           byte // 0 = 取定义耐久
	skill         bool
	luck          bool
	option        byte // 普通选项等级（+4 → 4）
	excellentBits byte // 卓越位掩码（线上 Excellent 字节，位 = Option.Number-1；
	// 物理 ExcDmg=option6→bit5=0x20；防御套件按需另置）
	fenrir  byte // 0 无；1 黑 2 蓝 4 金（可或）
	ancient byte // 远古 discriminator（1=Agnis/Chrono 2=Broy/Semeden；0=非远古；bonus level 恒 2）
	fullOpt bool // 全选项首饰（CreateFullOptionJewellery）
}

// seedClass 一个角色的规格。
type seedClass struct {
	suffix string
	class  byte
	slot   byte
	noria  bool
	// base 直设（GM 系列）与增量（Level 系列）二选一；0 = 不动。
	str, agi, vit, ene, cmd uint16
	addStr, addAgi          uint16
	addVit, addEne          uint16
	invExt                  byte
	// masterPoints 是该角色的起始大师点数（对照原版测试账号里的
	// `character.MasterLevelUpPoints = 100; // To test master skill tree`：
	// Level400 五个角色、GameMaster 的 Dark Lord、GameMaster2 的两个角色）。
	masterPoints int
	items        []seedItem
}

// seedSpec 一个账号的规格。
type seedSpec struct {
	name     string
	level    uint16
	gm       bool
	unlocked bool
	classes  []seedClass
}

// ---------- 物品清单（对照 AccountInitializerBase）----------

// potionRow = AddTestJewelsAndPotions：Bless×8 Soul×8 Life×8 + HP×4 MP×3 Alcohol + 盾药×3。
func potionRow() []seedItem {
	out := make([]seedItem, 0, 35)
	slot := byte(gridFirst)
	add := func(n int, it seedItem) { _ = n }
	_ = add
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelBless, dur: 1})
		slot++
	}
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelSoul, dur: 1})
		slot++
	}
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelLife, dur: 1})
		slot++
	}
	// HP 0..3 → (14,0..3)；MP 0..2 → (14,4..6)；Alcohol (14,9)；盾药 (14,35..37)。
	for _, n := range []byte{0, 1, 2, 3, 4, 5, 6} {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: n, dur: 3})
		slot++
	}
	out = append(out, seedItem{slot: slot, group: groupMisc2, number: aleNumber, dur: 1})
	slot++
	for _, n := range []byte{35, 36, 37} {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: n, dur: 3})
		slot++
	}
	return out
}

// elfRow = AddElfItems：Bless×8 Soul×8 Life×8（槽 12..35）+ Creation×4（36..39）+ Chaos×8（40..47）。
func elfRow() []seedItem {
	out := make([]seedItem, 0, 28)
	slot := byte(gridFirst)
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelBless, dur: 1})
		slot++
	}
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelSoul, dur: 1})
		slot++
	}
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelLife, dur: 1})
		slot++
	}
	for i := 0; i < 4; i++ {
		out = append(out, seedItem{slot: slot, group: groupMisc2, number: jewelCreate, dur: 1})
		slot++
	}
	for i := 0; i < 8; i++ {
		out = append(out, seedItem{slot: slot, group: groupOrb, number: chaosNumber, dur: 1})
		slot++
	}
	return out
}

// dkRow = AddDarkKnightItems：Orb ×5 + 全满饰物 ×4。
func dkRow() []seedItem {
	return []seedItem{
		{slot: gridFirst + 35, group: groupOrb, number: 12, dur: 1},
		{slot: gridFirst + 36, group: groupOrb, number: 14, dur: 1},
		{slot: gridFirst + 37, group: groupOrb, number: 19, dur: 1},
		{slot: gridFirst + 38, group: groupOrb, number: 44, dur: 1},
		{slot: gridFirst + 44, group: groupOrb, number: 7, dur: 1},
		{slot: gridFirst + 40, group: groupMisc1, number: 20, fullOpt: true}, // Wizards Ring
		{slot: gridFirst + 41, group: groupMisc1, number: 8, fullOpt: true},  // Ring of Ice
		{slot: gridFirst + 42, group: groupMisc1, number: 9, fullOpt: true},  // Ring of Poison
		{slot: gridFirst + 43, group: groupMisc1, number: 12, fullOpt: true}, // Pendant of Lightning
	}
}

// dlRow = AddDarkLordItems：Orb/Scroll ×6 + 全满饰物 ×3 + Dark Raven。
func dlRow() []seedItem {
	out := []seedItem{
		{slot: gridFirst + 35, group: groupOrb, number: 21, dur: 1},
		{slot: gridFirst + 36, group: groupOrb, number: 22, dur: 1},
		{slot: gridFirst + 37, group: groupOrb, number: 23, dur: 1},
		{slot: gridFirst + 38, group: groupOrb, number: 24, dur: 1},
		{slot: gridFirst + 48, group: groupOrb, number: 35, dur: 1},
		{slot: gridFirst + 49, group: groupOrb, number: 48, dur: 1},
		{slot: gridFirst + 50, group: groupMisc1, number: 8, fullOpt: true},
		{slot: gridFirst + 51, group: groupMisc1, number: 9, fullOpt: true},
		{slot: gridFirst + 52, group: groupMisc1, number: 12, fullOpt: true},
		{slot: gridFirst + 53, group: groupMisc1, number: petRaven, dur: 255, skill: true},
	}
	out[9].level = 1 // darkRaven.Level = 1
	return out
}

// petRow = AddPets。
func petRow() []seedItem {
	return []seedItem{
		{slot: gridFirst + 50, group: groupMisc1, number: 0, dur: 255}, // Guardian Angel
		{slot: gridFirst + 48, group: groupMisc1, number: 1, dur: 255}, // Imp
		{slot: gridFirst + 58, group: groupMisc1, number: 2, dur: 255}, // Uniria
		{slot: gridFirst + 47, group: groupMisc1, number: petDinorant, dur: 255},
	}
}

// armorSet 一套防具（甲/头/裤/手/靴，槽 3/2/4/5/6）。
// armorSet 生成一套防具：组号由槽位决定（甲=8 头=7 裤=9 手=10 靴=11，原版
// ItemGroups.Armor/Helm/Pants/Gloves/Boots），编号统一 setNumber —— 对照原版
// CreateArmorItem(itemSlot, setNumber, group) 的实际语义（此前把 group/setNumber
// 写反，导致 (26,8)=None、甲胄显示成"None"占位——真机修复 11 第二轮）。
// exc 顺序 [甲, 头, 裤, 手, 靴]，逐件卓越位（0=无）；对照原版 CreateArmorItem 的
// targetExcellentOption：防御系位序 = option.Number-1（1=MoneyRate 0x01, 2=DefRate 0x02,
// 3=Reflection 0x04, 4=ArmorDec 0x08, 5=MaxMana 0x10, 6=MaxHP 0x20）。
func armorSet(setNumber byte, level, option byte, luck bool, exc [5]byte) []seedItem {
	return []seedItem{
		{slot: slotArmor, group: 8, number: setNumber, level: level, option: option, luck: luck, excellentBits: exc[0]},
		{slot: slotHelm, group: 7, number: setNumber, level: level, option: option, luck: luck, excellentBits: exc[1]},
		{slot: slotPants, group: 9, number: setNumber, level: level, option: option, luck: luck, excellentBits: exc[2]},
		{slot: slotGloves, group: 10, number: setNumber, level: level, option: option, luck: luck, excellentBits: exc[3]},
		{slot: slotBoots, group: 11, number: setNumber, level: level, option: option, luck: luck, excellentBits: exc[4]},
	}
}

// excPendantFire / excRingPoison 等全满饰物。
func fullOpt(group byte, number byte, slot byte) seedItem {
	return seedItem{slot: slot, group: group, number: number, fullOpt: true}
}

// ---------- 账号规格 ----------

// lowLevelClasses 生成 test0..9 的四职业（LowLevel.cs：皮甲/武器/宝石药水/宠物）。
func lowLevelClasses() []seedClass {
	return []seedClass{
		{suffix: "Dk", class: player.ClassDarkKnight, slot: 0,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 1, number: 0}, // Small Axe
			}, append(armorSet(5, 0, 0, false, [5]byte{}),
				potionRow()...)...),
		},
		{suffix: "Dw", class: player.ClassDarkWizard, slot: 1,
			items: append([]seedItem{
				{slot: slotLeftHand, group: groupStaff, number: 0}, // Skull Staff
			}, append(armorSet(2, 0, 0, false, [5]byte{}),
				potionRow()...)...),
		},
		{suffix: "Elf", class: player.ClassFairyElf, slot: 2, noria: true,
			items: append([]seedItem{
				{slot: 1, group: 4, number: 0}, // Short Bow
				{slot: slotLeftHand, group: 4, number: arrowsNumber, dur: 255},
			}, append(armorSet(10, 0, 0, false, [5]byte{}),
				append([]seedItem{
					{slot: gridFirst + 55, group: groupOrb, number: 8, dur: 1},  // Healing Orb @67
					{slot: gridFirst + 63, group: groupOrb, number: 9, dur: 1},  // Defense Orb @75
					{slot: gridFirst + 56, group: groupOrb, number: 10, dur: 1}, // Damage Orb @68
				}, elfRow()...)...)...),
		},
		{suffix: "Dl", class: player.ClassDarkLord, slot: 3,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 1, number: 0}, // Small Axe
			}, append(armorSet(5, 0, 0, false, [5]byte{}),
				potionRow()...)...),
		},
	}
}

// fullAccounts 按 TestAccountsInitialization.Initialize 的全集生成。
func fullAccounts() []*seedSpec {
	specs := make([]*seedSpec, 0, 19)
	// test0..9（LowLevel）
	for i := 0; i < 10; i++ {
		specs = append(specs, &seedSpec{
			name:    fmt.Sprintf("test%d", i),
			level:   uint16(i*10 + 1),
			classes: lowLevelClasses(),
		})
	}
	// test300（Level300）
	{
		dl := seedClass{suffix: "Dl", class: player.ClassDarkLord, slot: 3, addStr: 600, addAgi: 300, addEne: 200,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 2, number: 12, level: 13, option: 4, skill: true, luck: true, excellentBits: 0x20}, // Exc Great Lord Scepter+13
				{slot: slotWings, group: groupMisc1, number: wingsCape, level: 13},                                             // Cape of Lord +13（原版 CreateWings 第4参 group=13）
				{slot: slotPet, group: groupMisc1, number: petHorse, dur: 255, fullOpt: true},                                  // Horse
			}, append(armorSet(26, 13, 4, true, [5]byte{0x08, 0x20, 0x01, 0x10, 0x04}), append(dlRow(), potionRow()...)...)...)}
		dk := seedClass{suffix: "Dk", class: 6, slot: 0, addStr: 400, addAgi: 300,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 0, number: 0, level: 13, option: 4, luck: true, excellentBits: 0x20},   // Exc Kris+13
				{slot: 1, group: 0, number: 5, level: 13, option: 4, skill: true, luck: true, excellentBits: 0x20}, // Exc Blade+13
				{slot: slotWings, group: groupOrb, number: 5, level: 13},                                           // Dragon Wings
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255},                                    // Fenrir
			}, append(armorSet(6, 13, 4, true, [5]byte{0x08, 0x20, 0x01, 0x10, 0x04}), append(dkRow(), append(potionRow(), petRow()...)...)...)...)}
		elf := seedClass{suffix: "Elf", class: 10, slot: 2, noria: true, addStr: 350, addAgi: 350,
			items: append([]seedItem{
				{slot: slotWings, group: groupOrb, number: 3, level: 13},                   // Wings of Spirits
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 2}, // Blue Fenrir
			}, append(armorSet(12, 15, 4, true, [5]byte{}),
				append([]seedItem{
					{slot: gridFirst + 55, group: groupOrb, number: 8, dur: 1},
					{slot: gridFirst + 63, group: groupOrb, number: 9, dur: 1},
					{slot: gridFirst + 56, group: groupOrb, number: 10, dur: 1},
				}, elfRow()...)...)...)}
		dw := seedClass{suffix: "Dw", class: 2, slot: 1, addStr: 300, addAgi: 300, addEne: 800,
			items: append([]seedItem{
				{slot: slotWings, group: groupOrb, number: 4, level: 13},                   // Wings of Soul
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 1}, // Black Fenrir
			}, append(armorSet(7, 15, 4, true, [5]byte{}), potionRow()...)...)}
		specs = append(specs, &seedSpec{name: "test300", level: 300, classes: []seedClass{dk, dw, elf, dl}})
	}
	// test400（Level400）
	{
		dl := seedClass{masterPoints: 100, suffix: "Dl", class: 17, slot: 3, addStr: 1200, addAgi: 400, addEne: 400,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 2, number: 13, level: 15, option: 4, skill: true, luck: true}, // AA Scepter+15
				fullOpt(groupMisc1, 13, 9),                                // Exc Pendant of Fire @9
				{slot: slotWings, group: groupOrb, number: 40, level: 15}, // Cape of Emperor +15
				{slot: slotPet, group: groupMisc1, number: petHorse, dur: 255, fullOpt: true},
			}, append(armorSet(33, 15, 4, true, [5]byte{}), append(dlRow(), potionRow()...)...)...)}
		dk := seedClass{masterPoints: 100, suffix: "Dk", class: 7, slot: 0, addStr: 1200, addAgi: 400,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 0, number: 19, level: 15, option: 4, skill: true, luck: true}, // AA Sword+15
				{slot: 1, group: 0, number: 22, level: 15, option: 4, skill: true, luck: true},            // Bone Blade+15
				fullOpt(groupMisc1, 13, 9),
				{slot: slotWings, group: groupOrb, number: 36, level: 15},                  // Wing of Storm +15
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 4}, // Gold Fenrir
			}, append(armorSet(29, 15, 4, true, [5]byte{}), append(dkRow(), append(potionRow(), petRow()...)...)...)...)}
		mg := seedClass{masterPoints: 100, suffix: "Mg", class: 13, slot: 4, addStr: 1200, addAgi: 400,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 0, number: 23, level: 15, option: 4, skill: true, luck: true}, // Explosion Blade+15
				fullOpt(groupMisc1, 13, 9),
				{slot: slotWings, group: groupOrb, number: 39, level: 15}, // Wing of Ruin +15
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 4},
			}, append(armorSet(32, 15, 4, true, [5]byte{}), append(potionRow(), petRow()...)...)...)}
		elf := seedClass{masterPoints: 100, suffix: "Elf", class: 11, slot: 2, noria: true, addStr: 338, addAgi: 1457, addEne: 105,
			items: append([]seedItem{
				{slot: 1, group: 4, number: 20, level: 13, option: 4, skill: true, luck: true, excellentBits: 0x20}, // Exc Arrow Viper Bow+13（原版 Sylph 注释误，定义名 Arrow Viper）
				{slot: slotLeftHand, group: 4, number: arrowsNumber, dur: 255},
				{slot: slotWings, group: groupOrb, number: 38, level: 15}, // Wing of Illusion +15
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 2},
				{slot: gridFirst + 55, group: groupOrb, number: 8, dur: 1},
				{slot: gridFirst + 63, group: groupOrb, number: 9, dur: 1},
				{slot: gridFirst + 56, group: groupOrb, number: 10, dur: 1},
			}, append(armorSet(31, 15, 4, true, [5]byte{}), elfRow()...)...)}
		dw := seedClass{masterPoints: 100, suffix: "Dw", class: 3, slot: 1, addStr: 300, addAgi: 300, addEne: 1200,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 5, number: 9, level: 15, option: 4, skill: true, luck: true, excellentBits: 0x20}, // Exc Staff of Destruction+15
				//下面这行导致最新版客户端选择角色时闪退
				//{slot: 1, group: groupMisc2, number: 6, level: 15, option: 4, luck: true}, // Shield +15 (15,6)
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 1},
			}, append(armorSet(30, 15, 4, true, [5]byte{}), potionRow()...)...)}
		specs = append(specs, &seedSpec{name: "test400", level: 400, classes: []seedClass{dk, dw, elf, mg, dl}})
	}
	// ancient（Ancient，330）
	{
		dl := seedClass{suffix: "Dl", class: 16, slot: 3, addStr: 560, addAgi: 300, addEne: 200,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 2, number: 12, level: 13, option: 4, skill: true, luck: true, excellentBits: 0x20},
				{slot: slotArmor, group: 8, number: 26, level: 13, option: 4, luck: true, ancient: 1},
				{slot: slotHelm, group: 7, number: 26, level: 13, option: 4, luck: true, ancient: 1},
				{slot: slotPants, group: 9, number: 26, level: 13, option: 4, luck: true, ancient: 2},
				{slot: slotGloves, group: 10, number: 26, level: 13, option: 4, luck: true, ancient: 2},
				{slot: slotBoots, group: 11, number: 26, level: 13, option: 4, luck: true, ancient: 2},
				{slot: 9, group: groupMisc1, number: 25, ancient: 2}, // Broy Pendant of Ice
				{slot: 10, group: groupMisc1, number: 9, ancient: 2}, // Agnis Ring of Poison（原版该件 disc=2）
				{slot: slotWings, group: groupMisc1, number: wingsCape, level: 13},
				{slot: slotPet, group: groupMisc1, number: petHorse, dur: 255, fullOpt: true},
			}, append(dlRow(), potionRow()...)...)}
		specs = append(specs, &seedSpec{name: "ancient", level: 330, classes: []seedClass{dl}})
	}
	// socket（Socket，380）—— 装备精简为主手武器 + 套装（镶嵌位实体已含，逐件对照 Socket.cs）。
	{
		dk := seedClass{suffix: "Dk", class: 7, slot: 0, addStr: 1000, addAgi: 400,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 0, number: 19, level: 15, option: 4, skill: true, luck: true},
			}, append(armorSet(29, 15, 4, true, [5]byte{}), potionRow()...)...)}
		specs = append(specs, &seedSpec{name: "socket", level: 380, classes: []seedClass{dk}})
	}
	// invext（扩展背包页夹具，300）—— 带外授予 2 页扩展的对照账号。
	// 原版没有任何游戏内途径能加页（只有 BotGenerator.cs:494 给 bot 4 页、
	// Web 编辑器改 Character.InventoryExtensions），故本仓用种子规格当同一个带外杠杆。
	// 页槽窗口：主网格 12..75，页 0 = 76..107，页 1 = 108..139（InventoryConstants）。
	{
		dk := seedClass{suffix: "Dk", class: 7, slot: 0, addStr: 1000, addAgi: 400, invExt: 2,
			items: append([]seedItem{
				{slot: 76, group: groupMisc2, number: jewelBless, dur: 1},   // 页 0 首格
				{slot: 107, group: groupMisc2, number: jewelSoul, dur: 1},   // 页 0 末格
				{slot: 108, group: groupMisc2, number: jewelLife, dur: 1},   // 页 1 首格
				{slot: 139, group: groupMisc2, number: jewelCreate, dur: 1}, // 页 1 末格 = 背包末格
			}, potionRow()...)}
		specs = append(specs, &seedSpec{name: "invext", level: 300, classes: []seedClass{dk}})
	}
	// quest1/2/3（QuestBase：职业书页/任务道具随任务系统，先给标准角色）。
	for _, q := range []struct {
		name  string
		level uint16
	}{
		{"quest1", 150}, {"quest2", 220}, {"quest3", 400},
	} {
		specs = append(specs, &seedSpec{name: q.name, level: q.level, classes: lowLevelClasses()})
	}
	// testgm / testgm2（GameMaster：GM 状态 + 20000 点 + 全技能）。
	{
		dl := seedClass{masterPoints: 100, suffix: "Dl", class: 17, slot: 3, str: 10000, agi: 2000, ene: 2000, vit: 2000, cmd: 2000,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 2, number: 13, level: 15, option: 4, skill: true, luck: true},
				{slot: slotArmor, group: 8, number: 26, level: 15, option: 4, luck: true, ancient: 1},
				{slot: slotHelm, group: 7, number: 26, level: 15, option: 4, luck: true, ancient: 1},
				{slot: slotPants, group: 9, number: 26, level: 15, option: 4, luck: true, ancient: 2},
				{slot: slotGloves, group: 10, number: 26, level: 15, option: 4, luck: true, ancient: 2},
				{slot: slotBoots, group: 11, number: 26, level: 15, option: 4, luck: true, ancient: 2},
				{slot: 9, group: groupMisc1, number: 25, ancient: 2},
				{slot: 10, group: groupMisc1, number: 9, ancient: 2},
				fullOpt(groupMisc1, 13, 11),
				fullOpt(groupMisc1, 9, 12),
				{slot: slotWings, group: groupOrb, number: 40, level: 15},
				{slot: slotPet, group: groupMisc1, number: petHorse, dur: 255, fullOpt: true},
			}, append(dlRow(), potionRow()...)...)}
		dk := seedClass{suffix: "Dk", class: 7, slot: 0, str: 10000, agi: 8000, vit: 4000, ene: 1000,
			items: append([]seedItem{
				{slot: slotLeftHand, group: 0, number: 19, level: 15, option: 4, skill: true, luck: true},
				{slot: 1, group: 0, number: 22, level: 15, option: 4, skill: true, luck: true},
				{slot: slotWings, group: groupOrb, number: 36, level: 15},
				{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 4},
			}, append(armorSet(29, 15, 4, true, [5]byte{}), append(dkRow(), append(potionRow(), petRow()...)...)...)...)}
		specs = append(specs, &seedSpec{name: "testgm", level: 400, gm: true, classes: []seedClass{dk, dwGM(), elfGM(), dl}})
		specs = append(specs, &seedSpec{name: "testgm2", level: 400, gm: true, classes: []seedClass{dk, dwGM(), sumGM()}})
	}
	// testunlock（Unlocked：GM + 0 级）。
	specs = append(specs, &seedSpec{name: "testunlock", level: 0, gm: true, unlocked: true, classes: lowLevelClasses()})
	return specs
}

// dwGM / elfGM / sumGM 是 GM 账号的法师/精灵/召唤师（GameMaster*.cs 的直设属性版）。
func dwGM() seedClass {
	return seedClass{suffix: "Dw", class: 3, slot: 1, str: 3000, agi: 3000, ene: 8000,
		items: append([]seedItem{
			{slot: slotLeftHand, group: 5, number: 9, level: 15, option: 4, skill: true, luck: true, excellentBits: 0x20},
			{slot: slotWings, group: groupOrb, number: 37, level: 15},
			{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 1},
		}, append(armorSet(30, 15, 4, true, [5]byte{}), potionRow()...)...)}
}

func elfGM() seedClass {
	return seedClass{suffix: "Elf", class: 11, slot: 2, noria: true, str: 3000, agi: 8000, ene: 2000,
		items: append([]seedItem{
			{slot: 1, group: 4, number: 20, level: 15, option: 4, skill: true, luck: true, excellentBits: 0x20},
			{slot: slotWings, group: groupOrb, number: 38, level: 15},
			{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 2},
		}, append(armorSet(31, 15, 4, true, [5]byte{}), elfRow()...)...)}
}

func sumGM() seedClass {
	return seedClass{masterPoints: 100, suffix: "Sum", class: 23, slot: 0, str: 2000, agi: 2000, ene: 2000, vit: 2000,
		items: append([]seedItem{
			{slot: slotLeftHand, group: 5, number: 36, level: 15, option: 4, skill: true, luck: true}, // AA Stick+15
			{slot: slotHelm, group: 7, number: 40, level: 15, option: 4, luck: true, ancient: 2},      // Semeden Helm
			{slot: slotArmor, group: 8, number: 40, level: 15, option: 4, luck: true, ancient: 2},     // Semeden Armor
			{slot: slotPants, group: 9, number: 40, level: 15, option: 4, luck: true, ancient: 1},     // Chrono Pants
			{slot: slotGloves, group: 10, number: 40, level: 15, option: 4, luck: true, ancient: 2},   // Semeden Gloves
			{slot: slotBoots, group: 11, number: 40, level: 15, option: 4, luck: true, ancient: 2},    // Semeden Boots
			{slot: 10, group: groupMisc1, number: 24, ancient: 2},                                     // Chrono Ring of Magic（原版该件 disc=2）
			{slot: slotWings, group: groupOrb, number: 43, level: 15},                                 // Wing of Dimension +15
			{slot: slotPet, group: groupMisc1, number: petFenrir, dur: 255, fenrir: 4},
		}, potionRow()...)}
}
