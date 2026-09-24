package gameserver

// handler_enter_gate_test.go —— C3 1C EnterGateRequest（客户端按门编号请求进图，
// 对照 WarpGateHandlerPlugIn + WarpGateAction）。原版全仓只有这一条 EnterGate 消费路径，
// 本仓 checkGateAfterWalk 的落点判定是 M7 期额外保险，二者共用同一套检查。

import (
	"testing"

	"mugo/internal/gamelogic/entity"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// 导出件里 Lorencia(map 0) → Noria(map 3) 的门：矩形 213..217 × 246..247，等级门槛 10。
const (
	gateNum23     = 23
	gateNum23MidX = 215
	gateNum23MidY = 246
	gate23Target  = 3
	gateNotOnMap0 = 4999
)

func enterGateFrame(number uint16) []byte {
	p := c2s.NewEnterGateRequest()
	p.SetGateNumber(number)
	return p.Bytes()
}

// TestEnterGateWarpsToTargetMap 验证站到门旁 + 等级足够：走 T1-7 换图链，
// 出 C3 1C 且 IsMapChange=1（客户端见 Flag==0 只播传送动画、不切图）。
func TestEnterGateWarpsToTargetMap(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	srv.world.Map(0).Walk(wp.ID, gateNum23MidX, gateNum23MidY, 0)

	srv.handleEnterGate(sess, enterGateFrame(gateNum23))

	if c.MapNumber != gate23Target {
		t.Fatalf("目标地图=%d，期望 %d", c.MapNumber, gate23Target)
	}
	f := findFrame(rec, 0xC3, 0x1C)
	if f == nil {
		t.Fatal("未下发 C3 1C MapChanged")
	}
	mc := s2c.AsMapChanged(f)
	if !mc.IsMapChange() {
		t.Fatal("成功形态的 1C 必须置 IsMapChange(Flag)=1")
	}
	if mc.MapNumber() != gate23Target {
		t.Fatalf("包内地图号=%d，期望 %d", mc.MapNumber(), gate23Target)
	}
	if sess.getState() != entity.StateEnteringMap {
		t.Fatalf("换图后状态=%d，期望停在 EnteringMap", sess.getState())
	}
}

// TestEnterGateTooFarSendsFailureFlag 验证原版的距离检查（门矩形外扩 InfoRange）：
// 人不在门旁 → 不传送，并按 EnterGateAsync 的 else 分支回一帧 Flag=0 的 1C。
func TestEnterGateTooFarSendsFailureFlag(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	if wp.X > 200 || wp.Y > 200 {
		t.Fatal("前置失败：出生点本应离门很远")
	}

	srv.handleEnterGate(sess, enterGateFrame(gateNum23))

	if c.MapNumber != 0 {
		t.Fatalf("超距请求不应换图，现在地图=%d", c.MapNumber)
	}
	f := findFrame(rec, 0xC3, 0x1C)
	if f == nil {
		t.Fatal("应下发失败形态的 C3 1C")
	}
	mc := s2c.AsMapChanged(f)
	if mc.IsMapChange() {
		t.Fatal("失败帧必须清掉 IsMapChange(Flag)")
	}
	if mc.MapNumber() != 0 {
		t.Fatalf("失败帧回显的应是当前地图 0，got %d", mc.MapNumber())
	}
}

// TestEnterGateLevelTooLow 验证等级门槛按职业折减值判定并拒发：
// 拒绝时按原版既发蓝字（LevelTooLowToEnterMap）又发 Flag=0 的 1C。
func TestEnterGateLevelTooLow(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 5)
	if c.Level > 9 {
		t.Skipf("脚手架等级=%d，不足以触发门槛 10", c.Level)
	}
	srv.world.Map(0).Walk(wp.ID, gateNum23MidX, gateNum23MidY, 0)

	srv.handleEnterGate(sess, enterGateFrame(gateNum23))

	if c.MapNumber != 0 {
		t.Fatalf("等级不足不应换图，现在地图=%d", c.MapNumber)
	}
	f := findFrame(rec, 0xC3, 0x1C)
	if f == nil || s2c.AsMapChanged(f).IsMapChange() {
		t.Fatal("等级不足应回 Flag=0 的 1C")
	}
	if findFrame(rec, 0xC1, 0x0D) == nil {
		t.Fatal("等级不足应另发一条蓝字（C1 0D LevelTooLowToEnterMap）")
	}
}

// TestEnterGateUnknownNumberIgnored 验证门编号不属于当前地图时只记日志、不回包
// （原版 WarpGateHandlerPlugIn:53-58 的 gate is null 分支）。
func TestEnterGateUnknownNumberIgnored(t *testing.T) {
	srv, sess, wp, _, rec := newExpScaffold(t, 200)
	srv.world.Map(0).Walk(wp.ID, gateNum23MidX, gateNum23MidY, 0)
	before := len(rec.frames)

	srv.handleEnterGate(sess, enterGateFrame(gateNotOnMap0))

	if len(rec.frames) != before {
		t.Fatalf("未知门编号不应发包，新增 %d 帧", len(rec.frames)-before)
	}
}

// TestEnterGateZeroIsWizardTeleport 验证编号 0 的法师瞬间移动形态目前不动作
// （随 doc/17 C 组挂账，避免被当成普通门误传送）。
func TestEnterGateZeroIsWizardTeleport(t *testing.T) {
	srv, sess, wp, c, _ := newExpScaffold(t, 200)
	srv.world.Map(0).Walk(wp.ID, gateNum23MidX, gateNum23MidY, 0)

	srv.handleEnterGate(sess, enterGateFrame(0))

	if c.MapNumber != 0 {
		t.Fatalf("编号 0 不应按门处理，现在地图=%d", c.MapNumber)
	}
}
