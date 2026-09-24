package gameserver

// handler_item_consume_test.go —— T2-6 闭环测试：使用药水（C3 26）→ 扣耐久 →
// 分段/一次性恢复 → C1 26 FF 当前属性下发；用尽销毁（C1 28）；冷却与各类拒绝（C1 26 FD）。
//
// 脚手架复用 handler_item_move_test.go 的 moveScaffold（GS + 已进图会话 + 封包录制），
// 因为两者需要的前置条件完全一致（属性、背包、世界态、观察者）。

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// consumeFrame 构造 C3 26 使用物品帧（只有 ItemSlot 有意义；TargetSlot/Fruit 置 0）。
func consumeFrame(slot byte) []byte {
	req := c2s.NewConsumeItemRequest()
	req.SetItemSlot(slot)
	return req.Bytes()
}

// statFrames 返回 C1 26 FF（当前属性下发）帧。
func statFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0x26 && f[3] == 0xFF {
			out = append(out, f)
		}
	}
	return out
}

// failedFrames 返回 C1 26 FD（使用失败）帧。
func failedFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0x26 && f[3] == 0xFD {
			out = append(out, f)
		}
	}
	return out
}

// consumeScaffold 构造消耗品测试脚手架（角色 20 级，背包槽 12 预留给药水）。
// hp/mana/shield 为上限；当前值由调用方按需设成 0。
func consumeScaffold(t *testing.T, hp, mana, shield uint32) *moveScaffold {
	t.Helper()
	// 注意：等级在 Character 上（CharStats 只放属性），恢复公式的角色等级取 c.Level。
	m := newMoveScaffold(t, 4, entity.CharStats{
		MaximumHealth: hp, MaximumMana: mana, MaximumShield: shield,
		MaximumAbility: 100,
	})
	m.c.Level = 20
	m.wp.Stats = m.c.Stats
	m.wp.IsAlive = true
	return m
}

// TestConsumePotionOneShotHealsAndBurnsDurability 锁定 T2-6 主路径（药水等级 16 → 一次性）：
//
//	Small Healing Potion(14,1)、20 级角色、上限 1000：
//	  percentage = 10 + 16×1 = 26；additional = max(0, 100−20) = 80
//	  total = 1000×26/100 + 80 = 340 → 血 100 → 440
//
// 断言：恰一帧 C1 26 FF（24B、**小端**）、耐久 3→2 并出一帧 C1 2A、无失败包、无删除包。
func TestConsumePotionOneShotHealsAndBurnsDurability(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 100
	it := &item.Item{Group: 14, Number: 1, Level: 16, Durability: 3}
	m.putItem(t, 12, it)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	stats := statFrames(m.rec)
	if len(stats) != 1 {
		t.Fatalf("应恰有 1 帧 C1 26 FF, got %d", len(stats))
	}
	if len(failedFrames(m.rec)) != 0 {
		t.Fatal("成功使用不应回失败包")
	}
	frame := stats[0]
	if len(frame) != s2c.CurrentStatsExtendedLength {
		t.Fatalf("C1 26 FF 应为 %dB, got %d", s2c.CurrentStatsExtendedLength, len(frame))
	}
	p := s2c.AsCurrentStatsExtended(frame)
	if p.Health() != 440 {
		t.Fatalf("当前血应为 440, got %d", p.Health())
	}
	if p.Shield() != 0 || p.Mana() != 0 || p.Ability() != 0 {
		t.Fatalf("未恢复项不得变化: shield=%d mana=%d ability=%d", p.Shield(), p.Mana(), p.Ability())
	}
	// 锁死**小端**：440 = 0x1B8，小端 B8 01 00 00；若误写成大端会是 00 00 01 B8
	// （C1 26 FE/24B 形态的字节序陷阱，客户端读错会显示错误血量）。
	want := []byte{0xB8, 0x01, 0x00, 0x00}
	for i, b := range want {
		if frame[4+i] != b {
			t.Fatalf("Health 字段应为小端 % X, got % X（整帧 % X）", want, frame[4:8], frame)
		}
	}
	// 真实状态：血已加、耐久已扣。
	if m.c.Stats.CurrentHealth != 440 {
		t.Fatalf("角色当前血=%d, want 440", m.c.Stats.CurrentHealth)
	}
	if it.Durability != 2 {
		t.Fatalf("耐久应为 2, got %d", it.Durability)
	}
	// 未用尽 → 刷新数量（byConsumption=true）而不是删除。
	if n := countFrames(m.rec, 0xC1, 0x28); n != 0 {
		t.Fatalf("未用尽不应发删除包, got %d", n)
	}
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 1 {
		t.Fatalf("应发 1 帧 C1 2A, got %d", n)
	}
	dc := s2c.AsItemDurabilityChanged(findFrame(m.rec, 0xC1, 0x2A))
	if dc.InventorySlot() != 12 || dc.Durability() != 2 {
		t.Fatalf("C1 2A 应为槽 12 耐久 2, got 槽 %d 耐久 %d", dc.InventorySlot(), dc.Durability())
	}
	if !dc.ByConsumption() {
		t.Fatal("消耗品扣耐久必须置 byConsumption 位（客户端播放使用反馈）")
	}
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("未用尽时物品应留在原槽")
	}
}

// TestConsumePotionLastChargeRemoved 锁定用尽语义：耐久 1 → 0 时发 C1 28 且**不发** C1 2A。
func TestConsumePotionLastChargeRemoved(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 100
	it := &item.Item{Group: 14, Number: 1, Level: 16, Durability: 1}
	m.putItem(t, 12, it)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if n := countFrames(m.rec, 0xC1, 0x28); n != 1 {
		t.Fatalf("用尽应发 1 帧 C1 28, got %d", n)
	}
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 0 {
		t.Fatalf("用尽不应再发 C1 2A, got %d", n)
	}
	rm := s2c.AsItemRemoved(findFrame(m.rec, 0xC1, 0x28))
	if rm.InventorySlot() != 12 {
		t.Fatalf("C1 28 槽位应为 12, got %d", rm.InventorySlot())
	}
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("用尽后背包槽应为空")
	}
	if m.c.Inventory.Count() != 0 {
		t.Fatalf("背包应为空, got %d", m.c.Inventory.Count())
	}
	// 恢复仍然生效（原版先恢复再扣耐久，顺序不可颠倒）。
	if m.c.Stats.CurrentHealth != 440 {
		t.Fatalf("用尽前恢复仍应生效: 血=%d, want 440", m.c.Stats.CurrentHealth)
	}
	if it.Durability != 0 {
		t.Fatalf("耐久应为 0, got %d", it.Durability)
	}
}

// TestConsumePotionSegmentedThreeSteps 锁定默认分级路径（药水等级 8 → 延迟减半）：
//
//	percentage = 10 + 8 = 18；total = 180 + 80 = 260 → 52 / 156 / 52 @ 100/300/100ms
//
// 三帧 C1 26 FF 携带的是**绝对当前血**（52 → 208 → 260），不是增量。
func TestConsumePotionSegmentedThreeSteps(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 0
	it := &item.Item{Group: 14, Number: 1, Level: 8, Durability: 5}
	m.putItem(t, 12, it)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	// 第一段要等 100ms，调用返回时不应有任何恢复包（原版"先返回、定时器里施加"）。
	if n := len(statFrames(m.rec)); n != 0 {
		t.Fatalf("分段恢复不应同步下发, got %d", n)
	}
	if m.c.Stats.CurrentHealth != 0 {
		t.Fatalf("分段恢复不应同步生效: 血=%d", m.c.Stats.CurrentHealth)
	}

	time.Sleep(250 * time.Millisecond)
	stats := statFrames(m.rec)
	if len(stats) != 1 {
		t.Fatalf("250ms 后应收到第 1 段, got %d", len(stats))
	}
	if got := s2c.AsCurrentStatsExtended(stats[0]).Health(); got != 52 {
		t.Fatalf("第 1 段当前血=%d, want 52", got)
	}

	// 剩余两段分别在 ~400ms / ~500ms 施加；留足余量。
	time.Sleep(700 * time.Millisecond)
	stats = statFrames(m.rec)
	if len(stats) != 3 {
		t.Fatalf("三段应各出一帧, got %d", len(stats))
	}
	wantHealth := []uint32{52, 208, 260}
	for i, f := range stats {
		if got := s2c.AsCurrentStatsExtended(f).Health(); got != wantHealth[i] {
			t.Fatalf("第 %d 帧当前血=%d, want %d", i+1, got, wantHealth[i])
		}
	}
	if m.c.Stats.CurrentHealth != 260 {
		t.Fatalf("三段后当前血=%d, want 260", m.c.Stats.CurrentHealth)
	}
	if it.Durability != 4 {
		t.Fatalf("耐久应只扣 1（分段不重复扣）, got %d", it.Durability)
	}
}

// TestConsumePotionSegmentedStopsOnDeath 变异检验：分段推进中角色死亡必须中止后续恢复
// （原版 RecoverByStepsAsync 每步开头 `if (!player.IsAlive) break;`）。
func TestConsumePotionSegmentedStopsOnDeath(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 0
	m.putItem(t, 12, &item.Item{Group: 14, Number: 1, Level: 8, Durability: 5})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	time.Sleep(250 * time.Millisecond)
	if n := len(statFrames(m.rec)); n != 1 {
		t.Fatalf("死前应已收到第 1 段, got %d", n)
	}
	health := m.c.Stats.CurrentHealth

	// 死亡：后续步骤必须被守卫拦下。
	m.wp.IsAlive = false
	time.Sleep(700 * time.Millisecond)

	if n := len(statFrames(m.rec)); n != 1 {
		t.Fatalf("死亡后不得继续下发恢复包, got %d", n)
	}
	if m.c.Stats.CurrentHealth != health {
		t.Fatalf("死亡后不得继续加血: %d → %d", health, m.c.Stats.CurrentHealth)
	}
}

// TestConsumePotionCooldownBlocksSecondUse 锁定冷却：0.5s 内第二次使用直接失败，
// 且**不**再扣耐久（原版 CheckPreconditions 早于 ConsumeSourceItem）。
func TestConsumePotionCooldownBlocksSecondUse(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 100
	it := &item.Item{Group: 14, Number: 1, Level: 16, Durability: 5}
	m.putItem(t, 12, it)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))
	if it.Durability != 4 {
		t.Fatalf("首次使用后耐久应为 4, got %d", it.Durability)
	}
	healthAfterFirst := m.c.Stats.CurrentHealth

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	if n := len(failedFrames(m.rec)); n != 1 {
		t.Fatalf("冷却期内应回 1 帧 C1 26 FD, got %d", n)
	}
	if n := len(statFrames(m.rec)); n != 1 {
		t.Fatalf("冷却期内不应再出属性包, got %d", n)
	}
	if it.Durability != 4 {
		t.Fatalf("冷却拒绝不得扣耐久: got %d", it.Durability)
	}
	if m.c.Stats.CurrentHealth != healthAfterFirst {
		t.Fatalf("冷却拒绝不得回血: %d → %d", healthAfterFirst, m.c.Stats.CurrentHealth)
	}
	// 失败包必须带当下血/盾，供客户端纠正本地预测。
	fail := failedFrames(m.rec)[0]
	if len(fail) != s2c.ItemConsumptionFailedExtendedLength {
		t.Fatalf("C1 26 FD 应为 %dB, got %d", s2c.ItemConsumptionFailedExtendedLength, len(fail))
	}
	fp := s2c.AsItemConsumptionFailedExtended(fail)
	if fp.Health() != healthAfterFirst || fp.Shield() != 0 {
		t.Fatalf("失败包内容应为血 %d 盾 0, got 血 %d 盾 %d", healthAfterFirst, fp.Health(), fp.Shield())
	}
}

// TestConsumeManaAndShieldPotion 锁定法力/护盾药水目标属性正确（配方表键序与目标映射）。
func TestConsumeManaAndShieldPotion(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 500)
	m.c.Stats.CurrentHealth = 1000
	m.c.Stats.CurrentMana = 0
	m.c.Stats.CurrentShield = 0
	m.putItem(t, 12, &item.Item{Group: 14, Number: 4, Level: 16, Durability: 3})  // 小法力药水
	m.putItem(t, 13, &item.Item{Group: 14, Number: 35, Level: 16, Durability: 3}) // 小护盾药水

	// 小法力药水：multiplier 1 → total = 1000×26/100 + 80 = 340。
	m.srv.handleItemConsume(m.sess, consumeFrame(12))
	// 两瓶是**两次独立使用**，必须清掉上一瓶的 0.5s 冷却（否则第二次会被冷却拒绝）。
	m.sess.setPotionReadyAt(time.Time{})
	// 小护盾药水：total = 500×36/100 = 180（**无**角色等级补偿）。
	m.srv.handleItemConsume(m.sess, consumeFrame(13))

	stats := statFrames(m.rec)
	if len(stats) != 2 {
		t.Fatalf("应收到 2 帧属性包, got %d", len(stats))
	}
	if got := s2c.AsCurrentStatsExtended(stats[0]).Mana(); got != 340 {
		t.Fatalf("法力应为 340, got %d", got)
	}
	last := s2c.AsCurrentStatsExtended(stats[1])
	if last.Shield() != 180 {
		t.Fatalf("护盾应为 180, got %d", last.Shield())
	}
	if last.Mana() != 340 {
		t.Fatalf("第 2 帧应保留已恢复的法力 340, got %d", last.Mana())
	}
	if last.Health() != 1000 {
		t.Fatalf("未受影响的当前血应为 1000, got %d", last.Health())
	}
}

// TestConsumeComplexPotionHealsBothPerStep 锁定复合药水：同一步的血/盾只出一帧
// （原版 16ms 合并窗口 → 本仓按步聚合）。
func TestConsumeComplexPotionHealsBothPerStep(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 500)
	m.c.Stats.CurrentHealth = 0
	m.c.Stats.CurrentShield = 0
	m.putItem(t, 12, &item.Item{Group: 14, Number: 38, Level: 16, Durability: 3}) // 小复合药水

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	stats := statFrames(m.rec)
	if len(stats) != 1 {
		t.Fatalf("一次性复合恢复应只出 1 帧（血盾合并）, got %d", len(stats))
	}
	p := s2c.AsCurrentStatsExtended(stats[0])
	if p.Health() != 340 {
		t.Fatalf("复合药水生命=340, got %d", p.Health())
	}
	if p.Shield() != 180 {
		t.Fatalf("复合药水护盾（500×36%%）=180, got %d", p.Shield())
	}
}

// TestConsumePotionClampsToMaximum 锁定上限钳制：满血时用药不溢出。
func TestConsumePotionClampsToMaximum(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 1000
	m.putItem(t, 12, &item.Item{Group: 14, Number: 1, Level: 16, Durability: 3})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	stats := statFrames(m.rec)
	if len(stats) != 1 {
		t.Fatalf("应出 1 帧, got %d", len(stats))
	}
	if got := s2c.AsCurrentStatsExtended(stats[0]).Health(); got != 1000 {
		t.Fatalf("满血用药应钳在上限 1000, got %d", got)
	}
}

// TestConsumePotionRejections 锁定各类拒绝路径都回 C1 26 FD 且不改变状态。
func TestConsumePotionRejections(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 500
	weapon := &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20}  // 不是药水
	broken := &item.Item{Group: 14, Number: 1, Level: 16, Durability: 0} // 耐久为 0
	m.putItem(t, 12, weapon)
	m.putItem(t, 13, broken)

	// 1) 空槽。
	m.srv.handleItemConsume(m.sess, consumeFrame(30))
	// 2) 非药水物品（未实现分支）。
	m.srv.handleItemConsume(m.sess, consumeFrame(12))
	// 3) 耐久为 0 的药水（原版 CheckPreconditions 拒绝）。
	m.srv.handleItemConsume(m.sess, consumeFrame(13))

	if n := len(failedFrames(m.rec)); n != 3 {
		t.Fatalf("三类拒绝应各回一帧 C1 26 FD, got %d", n)
	}
	if n := len(statFrames(m.rec)); n != 0 {
		t.Fatalf("拒绝不应出属性包, got %d", n)
	}
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 0 {
		t.Fatalf("拒绝不应改耐久, got %d 帧 C1 2A", n)
	}
	if weapon.Durability != 20 || broken.Durability != 0 {
		t.Fatalf("拒绝不得改动物品: weapon=%d broken=%d", weapon.Durability, broken.Durability)
	}
	if m.c.Stats.CurrentHealth != 500 {
		t.Fatalf("拒绝不得改血: %d", m.c.Stats.CurrentHealth)
	}
	// 失败包必须带当下血值（12B 扩展形态）。
	if len(failedFrames(m.rec)[0]) != s2c.ItemConsumptionFailedExtendedLength {
		t.Fatalf("C1 26 FD 长度应为 %dB", s2c.ItemConsumptionFailedExtendedLength)
	}
}

// TestConsumePotionMalformedAndStateGuards 锁定短帧与未进图状态：不 panic、不出包。
func TestConsumePotionMalformedAndStateGuards(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 100
	m.putItem(t, 12, &item.Item{Group: 14, Number: 1, Level: 16, Durability: 3})

	// 短帧（C3 头 3B + ItemSlot 1B，缺 TargetSlot/Fruit）：静默拒绝，不得越界。
	before := len(m.rec.frames)
	m.srv.handleItemConsume(m.sess, []byte{0xC3, 0x04, 0x26, 0x0C})
	if len(m.rec.frames) != before {
		t.Fatal("短帧不应产生任何出站包")
	}

	// 未进图：直接忽略。
	m.sess.setState(entity.StateAuthenticated)
	before = len(m.rec.frames)
	m.srv.handleItemConsume(m.sess, consumeFrame(12))
	if len(m.rec.frames) != before {
		t.Fatal("非进图状态不应产生出站包")
	}

	// 回到进图态确认状态守卫没有把正常路径一并封死。
	m.sess.setState(entity.StateEnteredWorld)
	m.srv.handleItemConsume(m.sess, consumeFrame(12))
	if len(statFrames(m.rec)) != 1 {
		t.Fatal("回到进图态后应能正常使用药水")
	}
}

// TestConsumePotionResolvesStatsWhenMissing 锁定属性懒解析分支：
// 会话未带 Stats 时必须先解析再使用（原版 player.Attributes 非空前置）。
func TestConsumePotionResolvesStatsWhenMissing(t *testing.T) {
	// 用非进图脚手架（不预置 Stats），再手工补齐关卡。
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(21, "lazy", 100, 100, rec)
	c := sess.getSelected()
	c.Stats = nil // 强制走 resolveCharStats
	c.Inventory = storage.NewInventory(0)
	wp.View = sess.playerView
	wp.IsAlive = true
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	if !c.Inventory.AddToSlot(12, srv.newSlottedItem(&item.Item{Group: 14, Number: 1, Level: 16, Durability: 3})) {
		t.Fatal("预置药水失败")
	}

	srv.handleItemConsume(sess, consumeFrame(12))

	if c.Stats == nil {
		t.Fatal("使用后应已解析出属性")
	}
	if n := len(statFrames(rec)); n != 1 {
		t.Fatalf("应出 1 帧属性包, got %d", n)
	}
	// 解析出的上限来自职业/等级，血必须在上限内且被提升过（初始血 = 上限，已满则不变）。
	p := s2c.AsCurrentStatsExtended(statFrames(rec)[0])
	if p.Health() == 0 || p.Health() > c.Stats.MaximumHealth {
		t.Fatalf("当前血 %d 应落在 (0, %d]", p.Health(), c.Stats.MaximumHealth)
	}
}
