package action

// moveitem_test.go —— T2-5 搬运决策回归：三道门（槽位/需求/双手冲突）、堆叠、
// 占位与边界、需求公式对拍。全部为纯函数测试（无配置依赖，输入显式构造）。

import (
	"testing"

	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
)

// mvDef 构造装备判定投影（测试用；只填本用例关心的字段）。
func mvDef(group, w, h, dropLevel, durability int, slots []int, reqs []MoveRequirement, qc []int) *MoveItemDef {
	return &MoveItemDef{
		Group: group, Width: w, Height: h,
		DropLevel: dropLevel, Durability: durability,
		Slots: slots, IsWearable: len(slots) > 0,
		Requirements: reqs, QualifiedClasses: qc,
	}
}

// mvEnv 构造决策环境；defs 按 (group, number) 索引。
func mvEnv(defs map[[2]int]*MoveItemDef, attrs map[string]float32, class int) *MoveItemEnv {
	return &MoveItemEnv{
		DefOf: func(it *item.Item) *MoveItemDef {
			return defs[[2]int{int(it.Group), it.Number}]
		},
		AttributeOf: func(d string) float32 { return attrs[d] },
		ClassNumber: class,
	}
}

// put 往背包指定槽放一件物品（尺寸取 def，缺省 1×1）。
func put(t *testing.T, inv *storage.Inventory, slot byte, it *item.Item, def *MoveItemDef) *storage.SlottedItem {
	t.Helper()
	w, h := byte(1), byte(1)
	if def != nil {
		w, h = byte(def.Width), byte(def.Height)
	}
	si := &storage.SlottedItem{It: it, Width: w, Height: h}
	if !inv.AddToSlot(slot, si) {
		t.Fatalf("预置物品失败 slot=%d", slot)
	}
	return si
}

// newTestInventory 构造无扩展页背包（槽 0..75：装备区 0..11 + 主网格 12..75）。
func newTestInventory(t *testing.T) *storage.Inventory {
	t.Helper()
	return storage.NewInventory(0)
}

// TestDecideMoveGridMoves 覆盖网格区搬运：成功、被占、越界、自身占位忽略、原地守卫。
func TestDecideMoveGridMoves(t *testing.T) {
	free := mvDef(14, 1, 1, 0, 3, nil, nil, nil)
	tall := mvDef(0, 1, 2, 6, 20, nil, nil, nil)
	defs := map[[2]int]*MoveItemDef{{14, 0}: free, {14, 1}: free, {0, 0}: tall}
	env := mvEnv(defs, nil, 0)

	t.Run("空格成功", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 14, Number: 0}
		put(t, inv, 12, it, free)
		if got := DecideMove(inv, 12, 20, it, free, env); got != MoveNormal {
			t.Fatalf("want MoveNormal, got %v", got)
		}
	})

	t.Run("目标被异类物品占据拒绝", func(t *testing.T) {
		inv := newTestInventory(t)
		src := &item.Item{Group: 14, Number: 0}
		put(t, inv, 12, src, free)
		put(t, inv, 20, &item.Item{Group: 14, Number: 1}, free) // 不同 number = 不同定义
		if got := DecideMove(inv, 12, 20, src, free, env); got != MoveNone {
			t.Fatalf("want MoveNone, got %v", got)
		}
	})

	t.Run("越界拒绝", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 0, Number: 0}
		put(t, inv, 20, it, tall) // 1×2 占 row1..2
		// slot 75 = 主网格最后一行最后一列（row7,col7）；1×2 需要 row8 → 越界。
		if got := DecideMove(inv, 20, 75, it, tall, env); got != MoveNone {
			t.Fatalf("want MoveNone（行越界）, got %v", got)
		}
		// 1×2 放到 row7 需求 row8；换 1×1 则合法（对照同一格）。
		small := &item.Item{Group: 14, Number: 0}
		put(t, inv, 21, small, free)
		if got := DecideMove(inv, 21, 75, small, free, env); got != MoveNormal {
			t.Fatalf("want MoveNormal（1×1 末列）, got %v", got)
		}
	})

	t.Run("源件自身占位不挡自己", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 0, Number: 0}
		put(t, inv, 20, it, tall) // row1..2, col0
		// slot 12 = row0,col0：落位需要 row0..1；row1 是源件自己 → 必须被忽略。
		if got := DecideMove(inv, 20, 12, it, tall, env); got != MoveNormal {
			t.Fatalf("want MoveNormal（忽略自身占位）, got %v", got)
		}
	})

	t.Run("原地搬运守卫", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 14, Number: 0, Durability: 1}
		put(t, inv, 12, it, free)
		// 同槽：原版会在堆叠分支把 targetItem 当源件自身 → FullStackAsync 数量翻倍，
		// 本实现显式拒绝（见 DecideMove 的防御性守卫注释）。
		if got := DecideMove(inv, 12, 12, it, free, env); got != MoveNone {
			t.Fatalf("want MoveNone（原地守卫）, got %v", got)
		}
	})
}

// TestDecideMoveEquipGuards 覆盖装备区三道门的第一道：槽位匹配与目标槽占用。
func TestDecideMoveEquipGuards(t *testing.T) {
	weapon := mvDef(0, 1, 2, 6, 20, []int{0, 1}, nil, []int{4})
	shield := mvDef(6, 2, 2, 3, 22, []int{1}, nil, []int{4})
	jewel := mvDef(14, 1, 1, 0, 3, nil, nil, nil)
	defs := map[[2]int]*MoveItemDef{{0, 0}: weapon, {6, 0}: shield, {14, 0}: jewel}
	env := mvEnv(defs, map[string]float32{"Total Strength": 1000, "Total Agility": 1000}, 4)

	t.Run("槽位匹配且空", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 0, Number: 0}
		put(t, inv, 12, it, weapon)
		if got := DecideMove(inv, 12, 0, it, weapon, env); got != MoveNormal {
			t.Fatalf("want MoveNormal, got %v", got)
		}
	})

	t.Run("槽位不匹配拒绝", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 6, Number: 0}
		put(t, inv, 12, it, shield)
		// 盾只穿槽 1（右手），拖到槽 0 应拒绝。
		if got := DecideMove(inv, 12, 0, it, shield, env); got != MoveNone {
			t.Fatalf("want MoveNone（槽位不匹配）, got %v", got)
		}
	})

	t.Run("装备槽已占用拒绝", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 0, Number: 0}
		put(t, inv, 12, it, weapon)
		put(t, inv, 1, &item.Item{Group: 6, Number: 0}, shield)
		if got := DecideMove(inv, 12, 1, it, weapon, env); got != MoveNone {
			t.Fatalf("want MoveNone（目标槽非空）, got %v", got)
		}
	})

	t.Run("非可穿戴物品不能进装备槽", func(t *testing.T) {
		inv := newTestInventory(t)
		it := &item.Item{Group: 14, Number: 0}
		put(t, inv, 12, it, jewel)
		if got := DecideMove(inv, 12, 2, it, jewel, env); got != MoveNone {
			t.Fatalf("want MoveNone（不可穿戴）, got %v", got)
		}
	})
}

// TestDecideMoveRequirements 覆盖第二道门：需求属性 + 职业合格。
func TestDecideMoveRequirements(t *testing.T) {
	reqs := []MoveRequirement{
		{Attribute: "Total Strength Requirement Value", Value: 40},
		{Attribute: "Total Agility Requirement Value", Value: 40},
	}
	weapon := mvDef(0, 1, 2, 6, 20, []int{0, 1}, reqs, []int{4, 6})
	defs := map[[2]int]*MoveItemDef{{0, 0}: weapon}

	// 需求值：dl=6, itemLevel=0 → (3*6*40)/100+20 = 27。
	decideWith := func(t *testing.T, attrs map[string]float32, class int) MoveKind {
		t.Helper()
		inv := newTestInventory(t)
		it := &item.Item{Group: 0, Number: 0}
		put(t, inv, 12, it, weapon)
		return DecideMove(inv, 12, 0, it, weapon, mvEnv(defs, attrs, class))
	}

	if got := decideWith(t, map[string]float32{"Total Strength": 26, "Total Agility": 99}, 4); got != MoveNone {
		t.Fatalf("力量不足应拒绝, got %v", got)
	}
	if got := decideWith(t, map[string]float32{"Total Strength": 99, "Total Agility": 26}, 4); got != MoveNone {
		t.Fatalf("敏捷不足应拒绝, got %v", got)
	}
	if got := decideWith(t, map[string]float32{"Total Strength": 27, "Total Agility": 27}, 4); got != MoveNormal {
		t.Fatalf("恰好达标应通过（27 >= 27）, got %v", got)
	}
	if got := decideWith(t, map[string]float32{"Total Strength": 999, "Total Agility": 999}, 0); got != MoveNone {
		t.Fatalf("职业不合格应拒绝（qc=[4,6]，职业 0）, got %v", got)
	}

	// 合格职业集合为空 → 原版 CompliesRequirements 返回 false（Contains 恒 false）。
	noQC := mvDef(0, 1, 2, 6, 20, []int{0, 1}, nil, nil)
	inv := newTestInventory(t)
	it := &item.Item{Group: 0, Number: 0}
	put(t, inv, 12, it, noQC)
	if got := DecideMove(inv, 12, 0, it, noQC, mvEnv(map[[2]int]*MoveItemDef{{0, 0}: noQC}, nil, 4)); got != MoveNone {
		t.Fatalf("合格职业为空应拒绝, got %v", got)
	}
}

// TestResolveRequirementFormula 对拍原版 ItemExtensions.GetRequirement / CalculateRequirement。
func TestResolveRequirementFormula(t *testing.T) {
	wearable := mvDef(0, 1, 3, 6, 20, []int{0, 1}, nil, nil)
	strReq := MoveRequirement{Attribute: "Total Strength Requirement Value", Value: 40}

	cases := []struct {
		name      string
		def       *MoveItemDef
		it        *item.Item
		req       MoveRequirement
		wantAttr  string
		wantValue int
	}{
		{
			// (3 * 6 * 40)/100 + 20 = 7 + 20
			name: "力量 0 级", def: wearable, it: &item.Item{Level: 0},
			req: strReq, wantAttr: "Total Strength", wantValue: 27,
		},
		{
			// dropLevel = 6 + 3*9 = 33 → (3*33*40)/100+20 = 39+20
			name: "力量 9 级", def: wearable, it: &item.Item{Level: 9},
			req: strReq, wantAttr: "Total Strength", wantValue: 59,
		},
		{
			// 普通选项每级 +4 力量需求（9 级基础上 +12）
			name: "力量 9 级 + 3 选项", def: wearable, it: &item.Item{Level: 9, OptionLevel: 3},
			req: strReq, wantAttr: "Total Strength", wantValue: 71,
		},
		{
			// 远古 +30：dropLevel = 6+30 = 36 → (3*36*40)/100+20 = 43+20
			name: "力量 远古", def: wearable, it: &item.Item{AncientDiscriminator: 1},
			req: strReq, wantAttr: "Total Strength", wantValue: 63,
		},
		{
			// 卓越 +25（远古优先，二者互斥）：dropLevel = 31 → (3*31*40)/100+20 = 37+20
			name: "力量 卓越", def: wearable, it: &item.Item{ExcellentBits: 0x01},
			req: strReq, wantAttr: "Total Strength", wantValue: 57,
		},
		{
			// 能量 multiplier = 4：(4*6*40)/100+20 = 9+20
			name: "能量", def: wearable, it: &item.Item{},
			req:      MoveRequirement{Attribute: "Total Energy Requirement Value", Value: 40},
			wantAttr: "Total Energy", wantValue: 29,
		},
		{
			// 非可穿戴 → 原值直出
			name: "不可穿戴直出", def: mvDef(14, 1, 1, 0, 3, nil, nil, nil), it: &item.Item{},
			req: strReq, wantAttr: "Total Strength", wantValue: 40,
		},
		{
			// 未映射属性（Level / 任务标记）原样返回
			name: "未映射属性", def: wearable, it: &item.Item{},
			req:      MoveRequirement{Attribute: "Level", Value: 380},
			wantAttr: "Level", wantValue: 380,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attr, val := ResolveRequirement(tc.def, tc.it, tc.req)
			if attr != tc.wantAttr || val != tc.wantValue {
				t.Fatalf("(%s,%d) want (%s,%d)", attr, val, tc.wantAttr, tc.wantValue)
			}
		})
	}

	t.Run("召唤师书能量公式", func(t *testing.T) {
		// 书：HasSkill 且 group == 5 → ((req * (dropLevel + itemLevel) * 3)/100) + 20，
		// 其中 dropLevel 取 itemLevel=0 的算法（即 def.DropLevel）。
		book := mvDef(5, 1, 2, 10, 20, []int{0, 1}, nil, nil)
		book.HasSkill = true
		it := &item.Item{Level: 4}
		attr, val := ResolveRequirement(book, it, MoveRequirement{Attribute: "Total Energy Requirement Value", Value: 40})
		// ((40 * (10 + 4) * 3)/100) + 20 = 1680/100 + 20 = 16 + 20
		if attr != "Total Energy" || val != 36 {
			t.Fatalf("(%s,%d) want (Total Energy,36)", attr, val)
		}
	})

	t.Run("CalculateDropLevel", func(t *testing.T) {
		def := mvDef(0, 1, 1, 6, 20, nil, nil, nil)
		if got := CalculateDropLevel(def, &item.Item{Level: 5}, 5); got != 21 {
			t.Fatalf("want 21, got %d", got)
		}
		if got := CalculateDropLevel(def, &item.Item{Level: 5, AncientDiscriminator: 2}, 5); got != 51 {
			t.Fatalf("远古 want 51, got %d", got)
		}
		if got := CalculateDropLevel(def, &item.Item{Level: 5, ExcellentBits: 0x01}, 5); got != 46 {
			t.Fatalf("卓越 want 46, got %d", got)
		}
	})
}

// TestConflictsWithEquippedHands 覆盖第三道门（两个分支各自的正反例）。
func TestConflictsWithEquippedHands(t *testing.T) {
	oneHand := mvDef(0, 1, 2, 6, 20, []int{0, 1}, nil, []int{4})
	ammo := mvDef(4, 1, 1, 0, 255, []int{1}, nil, []int{4})
	ammo.IsAmmunition = true
	twoWide := mvDef(0, 2, 3, 30, 36, []int{0}, nil, []int{4})
	shield := mvDef(6, 2, 2, 3, 22, []int{1}, nil, []int{4})
	defs := map[[2]int]*MoveItemDef{
		{0, 0}: oneHand, {4, 7}: ammo, {0, 9}: twoWide, {6, 0}: shield,
	}
	env := mvEnv(defs, map[string]float32{"Total Strength": 9999, "Total Agility": 9999}, 4)

	// 左槽放 2 宽件 → 右手放单手/盾冲突。
	invLeftWide := newTestInventory(t)
	left := &item.Item{Group: 0, Number: 9}
	put(t, invLeftWide, LeftHandSlot, left, twoWide)
	if !ConflictsWithEquippedHands(shield, invLeftWide, 1, env) {
		t.Fatal("左手 2 宽件时右手放盾应冲突")
	}
	if !ConflictsWithEquippedHands(oneHand, invLeftWide, 1, env) {
		t.Fatal("左手 2 宽件时右手放双手武器应冲突")
	}

	// 右槽放非弹药武器 → 左手放 2 宽件冲突。
	invRightWeapon := newTestInventory(t)
	right := &item.Item{Group: 0, Number: 0}
	put(t, invRightWeapon, 1, right, oneHand)
	if !ConflictsWithEquippedHands(twoWide, invRightWeapon, 0, env) {
		t.Fatal("右手非弹药武器时左手放 2 宽件应冲突")
	}

	// 右槽放**弹药** → 左手 2 宽件不再冲突（原版 IsAmmunition 特例）。
	invRightAmmo := newTestInventory(t)
	put(t, invRightAmmo, 1, &item.Item{Group: 4, Number: 7}, ammo)
	if ConflictsWithEquippedHands(twoWide, invRightAmmo, 0, env) {
		t.Fatal("右手弹药不应阻止左手装备 2 宽件")
	}

	// 空手 → 无冲突。
	invEmpty := newTestInventory(t)
	if ConflictsWithEquippedHands(twoWide, invEmpty, 0, env) {
		t.Fatal("空手不应冲突")
	}
	if ConflictsWithEquippedHands(shield, invEmpty, 1, env) {
		t.Fatal("空手不应冲突（右手）")
	}
}

// TestDecideMoveStacking 覆盖堆叠三分支。
func TestDecideMoveStacking(t *testing.T) {
	potion := mvDef(14, 1, 1, 0, 3, nil, nil, nil)
	defs := map[[2]int]*MoveItemDef{{14, 0}: potion}
	env := mvEnv(defs, nil, 0)

	newStackInv := func(srcDur, dstDur byte, dstLevel byte) (*storage.Inventory, *item.Item) {
		inv := newTestInventory(t)
		src := &item.Item{Group: 14, Number: 0, Durability: srcDur}
		put(t, inv, 12, src, potion)
		put(t, inv, 13, &item.Item{Group: 14, Number: 0, Durability: dstDur, Level: dstLevel}, potion)
		return inv, src
	}

	t.Run("叠满", func(t *testing.T) {
		inv, src := newStackInv(1, 2, 0)
		if got := DecideMove(inv, 12, 13, src, potion, env); got != MoveCompleteStack {
			t.Fatalf("want MoveCompleteStack, got %v", got)
		}
	})

	t.Run("部分叠", func(t *testing.T) {
		inv, src := newStackInv(3, 1, 0)
		if got := DecideMove(inv, 12, 13, src, potion, env); got != MovePartiallyStack {
			t.Fatalf("want MovePartiallyStack, got %v", got)
		}
	})

	t.Run("目标已满拒绝", func(t *testing.T) {
		inv, src := newStackInv(1, 3, 0)
		if got := DecideMove(inv, 12, 13, src, potion, env); got != MoveNone {
			t.Fatalf("want MoveNone（目标满且装不下）, got %v", got)
		}
	})

	t.Run("等级不同不可叠", func(t *testing.T) {
		inv, src := newStackInv(1, 1, 1)
		if got := DecideMove(inv, 12, 13, src, potion, env); got != MoveNone {
			t.Fatalf("want MoveNone（IsSameItemAs 要求等级相同）, got %v", got)
		}
	})
}

// TestApplyNormalMoveRollback 锁定落位失败时的回滚（原版 MoveNormalAsync 的补偿分支）。
func TestApplyNormalMoveRollback(t *testing.T) {
	inv := newTestInventory(t)
	it := &item.Item{Group: 14, Number: 0}
	si := put(t, inv, 12, it, nil)

	// 目标槽越界 → AddToSlot 失败 → 应回滚到原槽并返回 false。
	if ApplyNormalMove(inv, 12, 200, si) {
		t.Fatal("越界落位应返回 false")
	}
	if got := inv.GetItem(12); got != si {
		t.Fatalf("回滚失败：源槽应仍有该物品, got %v", got)
	}
	if inv.Count() != 1 {
		t.Fatalf("回滚后物品数应仍为 1, got %d", inv.Count())
	}
}

// TestApplyStacks 锁定两种堆叠的容器副作用。
func TestApplyStacks(t *testing.T) {
	potion := mvDef(14, 1, 1, 0, 3, nil, nil, nil)

	t.Run("叠满后源件销毁", func(t *testing.T) {
		inv := newTestInventory(t)
		src := &item.Item{Group: 14, Number: 0, Durability: 1}
		dst := &item.Item{Group: 14, Number: 0, Durability: 2}
		ssi := put(t, inv, 12, src, potion)
		dsi := put(t, inv, 13, dst, potion)
		ApplyFullStack(inv, ssi, dsi)
		if inv.Count() != 1 {
			t.Fatalf("源件应被销毁, count=%d", inv.Count())
		}
		if dsi.It.Durability != 3 {
			t.Fatalf("目标数量应为 3, got %d", dsi.It.Durability)
		}
	})

	t.Run("部分叠后源件留余量", func(t *testing.T) {
		inv := newTestInventory(t)
		src := &item.Item{Group: 14, Number: 0, Durability: 3}
		dst := &item.Item{Group: 14, Number: 0, Durability: 1}
		ssi := put(t, inv, 12, src, potion)
		dsi := put(t, inv, 13, dst, potion)
		ApplyPartialStack(inv, ssi, dsi, potion)
		if dsi.It.Durability != 3 {
			t.Fatalf("目标应补满到 3, got %d", dsi.It.Durability)
		}
		if ssi.It.Durability != 1 {
			t.Fatalf("源件应留 1, got %d", ssi.It.Durability)
		}
		if inv.Count() != 2 {
			t.Fatalf("部分叠不应销毁源件, count=%d", inv.Count())
		}
	})
}

// TestMoveItemDefWearsSlot 锁定槽位集合判定（"Left or Right Hand" 必须两个槽都算）。
func TestMoveItemDefWearsSlot(t *testing.T) {
	def := mvDef(0, 1, 2, 6, 20, []int{0, 1}, nil, nil)
	if !def.WearsSlot(0) || !def.WearsSlot(1) {
		t.Fatal("双手武器应同时匹配槽 0 与槽 1")
	}
	if def.WearsSlot(2) {
		t.Fatal("槽 2 不应匹配")
	}
	var nilDef *MoveItemDef
	if nilDef.WearsSlot(0) {
		t.Fatal("nil 定义应返回 false")
	}
}
