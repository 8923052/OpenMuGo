package gameserver

// handler_consume_alcohol_test.go —— 酒（Ale，14,9 → effect 201）消耗测试。
// 使用后：扣耐久（1→0 销毁）、下发 C1 07 激活（effectId=201=0xC9）、效果入表，
// 并把攻速加成写回角色属性 + 下发 C1 26 FF（客户端属性面板才显示变化）。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	s2c "mugo/internal/proto/s2c"
)

func TestConsumeAlcoholActivatesEffect(t *testing.T) {
	m := consumeScaffold(t, 1000, 1000, 0)
	m.c.Stats.CurrentHealth = 1000

	// 酒：group14 number9，durability=1。
	ale := &item.Item{Group: 14, Number: 9, Durability: 1}
	m.putItem(t, 12, ale)

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	// C1 07 激活帧。
	f := findFrame(m.rec, 0xC1, 0x07)
	if f == nil {
		t.Fatal("应下发 C1 07 MagicEffectStatus 激活")
	}
	st := s2c.AsMagicEffectStatus(f)
	if !st.IsActive() {
		t.Fatal("应 IsActive=true")
	}
	if st.PlayerId() != 0x200 {
		t.Fatalf("PlayerId=%d，期望自己 0x200", st.PlayerId())
	}
	// effect 201 的低字节 = 0xC9。
	if st.EffectId() != 0xC9 {
		t.Fatalf("EffectId=%d，期望 201(0xC9)", st.EffectId())
	}
	if m.sess.peekEffects().Len() != 1 {
		t.Fatal("活动效果数应为 1")
	}
	eff := m.sess.peekEffects().Snapshot()[0]
	// 效果定义：持续 80 秒、PowerUp 目标 Attack Speed Any +20。
	if eff.Definition.Number != 201 {
		t.Fatalf("活动效果编号=%d，期望 201", eff.Definition.Number)
	}
	if got := eff.PowerUps[0]; got.Attribute != "Attack Speed Any" || got.Value != 20 {
		t.Fatalf("效果 PowerUp 应为 Attack Speed Any +20, got %v", got)
	}

	// 酒耐久 1→0 → 物品销毁（C1 28），背包槽空。
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("酒应用尽并从背包移除")
	}
	if n := countFrames(m.rec, 0xC1, 0x28); n != 1 {
		t.Fatalf("应发 1 帧 C1 28（物品销毁）, got %d", n)
	}
	// 销毁包 TrueFlag 必须非 0：客户端 ReceiveDeleteInventory 靠 `if (Data->Value) EnableUse = 0;`
	// 解除使用去抖；为 0 会让客户端此后所有物品都用不了（SendRequestUse 被 EnableUse 吞掉）。
	if rm := findFrame(m.rec, 0xC1, 0x28); s2c.AsItemRemoved(rm).TrueFlag() == 0 {
		t.Fatal("C1 28 TrueFlag 不得为 0（否则客户端 EnableUse 卡死，后续物品均无法使用）")
	}
}

// TestConsumeAlcoholRaisesAttackSpeed 锁定"酒后属性面板攻速变化"：
// 效果 201 的 PowerUp 目标 Attack Speed Any 经类关系图（AddRaw）汇入 Attack Speed，
// 服务端必须把它写回 c.Stats.AttackSpeed 并下发 C1 26 FF——客户端只有
// ReceiveStatsExtended 的 case 0xff 才会更新属性面板的 AttackSpeed/MagicSpeed。
func TestConsumeAlcoholRaisesAttackSpeed(t *testing.T) {
	m := newMoveScaffold(t, 4, entity.CharStats{
		Strength: 500, Agility: 500, Vitality: 500, Energy: 500,
		MaximumHealth: 1000, MaximumMana: 1000, MaximumAbility: 100,
	})
	m.c.Level = 20
	m.wp.Stats = m.c.Stats
	m.wp.IsAlive = true

	// 基线：先按"无效果"刷新一次，取得属性系统算出的真实攻速（测试里的
	// CharStats 是手工种子，AttackSpeed 字段初始为 0，须由属性系统投影）。
	m.srv.refreshEffectStats(m.sess, m.c)
	before := m.c.Stats.AttackSpeed
	if before == 0 {
		t.Fatal("前置：攻速基值不应为 0（关系图 Total Agility × 0.05）")
	}
	m.rec.frames = nil
	m.putItem(t, 12, &item.Item{Group: 14, Number: 9, Durability: 1})

	m.srv.handleItemConsume(m.sess, consumeFrame(12))

	after := m.c.Stats.AttackSpeed
	if diff := int(after) - int(before); diff < 19 || diff > 21 {
		t.Fatalf("酒后攻速应 +20: %d → %d", before, after)
	}
	stats := statFrames(m.rec)
	if len(stats) == 0 {
		t.Fatal("应下发 C1 26 FF（否则客户端属性面板攻速不变）")
	}
	last := s2c.AsCurrentStatsExtended(stats[len(stats)-1])
	if last.AttackSpeed() != after {
		t.Fatalf("C1 26 FF 攻速应为 %d, got %d", after, last.AttackSpeed())
	}
}
