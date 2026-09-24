package gameserver

// handler_warp_requirement_test.go —— TRIM-11b/11c：进图两条路径的等级折减与属性需求。
// 数据全部取真实导出件（70_warps.json、60_maps.json、80_attributes.json），
// 口径对照 WarpAction.cs:34-65、WarpGateAction.cs:33-68、
// CharacterExtensions.GetEffectiveMoveLevelRequirement:113-126、
// GameMapDefinitionExtensions.TryGetRequirementError:21-40。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
)

// icarusRequirementDesignation / ...ID 是 80_attributes.json 里 Icarus 的进图需求属性
// （原版由翅膀/迪诺特/菲尼瑞/飞龙装备给出）。
const (
	icarusRequirementDesignation = "Requirement of the Icarus map."
	icarusRequirementID          = "ec34c673-84de-4811-8962-cd2164a2248c"
	icarusRequirementDescription = "You can enter Icarus only with wings, dinorant, fenrir."
)

func grantIcarusRequirement(c *entity.Character) {
	c.AttributeBonuses = append(c.AttributeBonuses, entity.AttributeBonus{
		AttributeID: icarusRequirementID, Designation: icarusRequirementDesignation, Value: 1,
	})
}

// TestWarpCommandLevelReduction：Raklion(34) 门槛 280，MG 线（职业 12）带 34% 折减 → 184。
// 150 级被拒时蓝字必须打印**折减后**的 184（原版用同一个数），200 级则放行。
func TestWarpCommandLevelReduction(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 30000, 150)
	c.ClassNumber = 12

	sendWarp(srv, sess, 34)
	msgs := systemMessages(rec)
	want := "1:You need to be level 184 in order to warp"
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("150 级 MG 应被拒且提示折减后的 184, got %v", msgs)
	}
	if c.Stats.Money != 30000 || c.MapNumber != 0 {
		t.Fatalf("拒绝不该扣钱/换图: money=%d map=%d", c.Stats.Money, c.MapNumber)
	}

	srv2, sess2, c2, _ := warpScaffold(t, 30000, 200)
	c2.ClassNumber = 12
	sendWarp(srv2, sess2, 34)
	if c2.MapNumber != 57 {
		t.Fatalf("200 级 MG 应能去 Raklion(57)，折减门槛 184: map=%d", c2.MapNumber)
	}
	if c2.Stats.Money != 30000-15000 {
		t.Fatalf("应扣 15000 金: %d", c2.Stats.Money)
	}
}

// TestWarpCommandLevel400NotReduced：原版对门槛 400 有硬特例（Peace Swamp）——
// 特殊职业也不折减。
func TestWarpCommandLevel400NotReduced(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 30000, 300)
	c.ClassNumber = 12

	sendWarp(srv, sess, 33) // PeaceSwamp：门槛 400、15000 金
	msgs := systemMessages(rec)
	want := "1:You need to be level 400 in order to warp"
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("400 门槛不该被折减, got %v", msgs)
	}
	if c.MapNumber != 0 || c.Stats.Money != 30000 {
		t.Fatalf("300 级 MG 不该进 Peace Swamp（400 不折减）: map=%d money=%d", c.MapNumber, c.Stats.Money)
	}
}

// TestWarpCommandMapRequirement：Icarus(23) 等级够、钱够，但没翅膀 → 按地图需求被拒，
// 且**不扣钱**（金币检查在需求之后）；补上需求属性后放行。
func TestWarpCommandMapRequirement(t *testing.T) {
	srv, sess, c, rec := warpScaffold(t, 30000, 200)

	sendWarp(srv, sess, 23)
	want := "1:Missing requirement to enter the map: " + icarusRequirementDescription
	msgs := systemMessages(rec)
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("Icarus 需求未满足应发属性 Description 文案, got %v", msgs)
	}
	if c.Stats.Money != 30000 || c.MapNumber != 0 {
		t.Fatalf("需求未满足时不该扣款换图: money=%d map=%d", c.Stats.Money, c.MapNumber)
	}

	grantIcarusRequirement(c)
	sess.setCombatValues(nil) // 需求属性变了，快照作废（与任务奖励同口径）
	sendWarp(srv, sess, 23)
	if c.MapNumber != 10 {
		t.Fatalf("满足需求后应进 Icarus(map 10), got %d", c.MapNumber)
	}
	if c.Stats.Money != 30000-10000 {
		t.Fatalf("应扣 10000 金: %d", c.Stats.Money)
	}
}

// TestGateWalkLevelReductionAndRequirement：走传送门那条路（map 4 的门 62 → Icarus，
// 原始门槛 160）：MG 折减到 105 → 120 级过等级门，但仍被地图需求拦住；
// 补需求属性后才换图。
func TestGateWalkLevelReductionAndRequirement(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "walker", 18, 250, rec)
	c := sess.getSelected()
	c.ClassNumber = 12
	c.Level = 120
	c.MapNumber = 4
	wp.MapNumber = 4
	wp.View = sess.playerView
	srv.world.Map(4).Enter(wp)
	sess.setWorldPlayer(wp)

	// 该门确实落在 17..19 / 250（导出件数据），确认夹具站对了位置。
	m4, ok := srv.deps.cfg.GameConfig.Map(4)
	if !ok {
		t.Fatal("导出件缺 map 4")
	}
	gate := m4.EnterGateAt(18, 250)
	if gate == nil || gate.Number != 62 {
		t.Fatalf("夹具应站在门 62 上, got %+v", gate)
	}

	srv.checkGateAfterWalk(sess, c, wp)
	if wp.MapNumber != 4 || c.MapNumber != 4 {
		t.Fatalf("需求未满足不该换图, wp.map=%d", wp.MapNumber)
	}
	want := "1:Missing requirement to enter the map: " + icarusRequirementDescription
	if msgs := systemMessages(rec); len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("应回地图需求蓝字, got %v", msgs)
	}

	rec.frames = nil
	grantIcarusRequirement(c)
	srv.checkGateAfterWalk(sess, c, wp)
	if c.MapNumber != 10 {
		t.Fatalf("满足等级(折减后 105)与需求后应进 Icarus, got %d", c.MapNumber)
	}
	if len(systemMessages(rec)) != 0 {
		t.Fatalf("放行时不该有拒绝蓝字: %v", systemMessages(rec))
	}
}

// TestGateWalkLevelTooLowAfterReduction：非特殊职业（Dark Wizard）没有折减，
// 门 62 的 160 级门槛原样生效。
func TestGateWalkLevelTooLowAfterReduction(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "walker", 18, 250, rec)
	c := sess.getSelected()
	c.ClassNumber = 0
	c.Level = 120
	c.MapNumber = 4
	wp.MapNumber = 4
	wp.View = sess.playerView
	srv.world.Map(4).Enter(wp)
	sess.setWorldPlayer(wp)

	srv.checkGateAfterWalk(sess, c, wp)
	if msgs := systemMessages(rec); len(msgs) != 1 ||
		msgs[0] != "1:Your level is too low to enter this map." {
		t.Fatalf("120 级法师过不了 160 的门, got %v", msgs)
	}
	if c.MapNumber != 4 {
		t.Fatalf("等级不足不该换图, got %d", c.MapNumber)
	}
}
