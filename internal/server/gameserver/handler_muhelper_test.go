package gameserver

// handler_muhelper_test.go —— MU Helper 端到端：C2 AE 存程序并回显；C1 BF 51 开启扣首次 Zen
// 并发 Start+ConsumeMoney 两帧；资金不足不启动；开启后 collect 再扣一次；关闭发 Stop。

import (
	"bytes"
	"testing"

	"mugo/internal/gamelogic/entity"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

func newMuHelperSess(t *testing.T, money uint32) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "idler", 20, 20, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: money, Strength: 100, Agility: 100}
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return srv, sess, c, rec
}

func TestMuHelperSaveDataEchoes(t *testing.T) {
	srv, sess, c, rec := newMuHelperSess(t, 1_000_000)
	req := c2s.NewMuHelperSaveDataRequest()
	hd := req.HelperData()
	for i := range hd {
		hd[i] = byte(i + 1)
	}
	srv.handleMuHelperSaveData(sess, req.Bytes())

	if !bytes.Equal(c.MuHelperConfiguration, hd) {
		t.Fatal("存档应等于收到的程序 blob")
	}
	f := findFrame(rec, 0xC2, 0xAE)
	if f == nil {
		t.Fatal("应回显 C2 AE MuHelperConfigurationData")
	}
	if !bytes.Equal(s2c.AsMuHelperConfigurationData(f).HelperData(), hd) {
		t.Fatal("回显内容与存入不一致")
	}
}

func TestMuHelperStartDeductsAndSends(t *testing.T) {
	srv, sess, c, rec := newMuHelperSess(t, 1_000_000)
	// 等级 20 → stage0 费用 20*20 = 400。
	req := c2s.NewMuHelperStatusChangeRequest()
	req.SetPauseStatus(false) // Enabled
	srv.handleMuHelperStatus(sess, req.Bytes())

	if c.Stats.Money != 1_000_000-400 {
		t.Fatalf("应扣 400, 现有 %d", c.Stats.Money)
	}
	if n := countFrames(rec, 0xC1, 0xBF); n != 2 {
		t.Fatalf("应发 Start+ConsumeMoney 两帧, got %d", n)
	}
	if sess.muHelper == nil || !sess.muHelper.running {
		t.Fatal("应处于运行中")
	}
	srv.muHelperStop(sess) // 收摊，停后台循环
}

func TestMuHelperStartInsufficientMoney(t *testing.T) {
	srv, sess, c, rec := newMuHelperSess(t, 100) // < 400
	req := c2s.NewMuHelperStatusChangeRequest()
	req.SetPauseStatus(false)
	srv.handleMuHelperStatus(sess, req.Bytes())

	if c.Stats.Money != 100 {
		t.Fatalf("资金不足不得扣钱, got %d", c.Stats.Money)
	}
	if sess.muHelper != nil {
		t.Fatal("资金不足不得启动")
	}
	if countFrames(rec, 0xC1, 0xBF) != 0 {
		t.Fatal("未启动不应发状态帧")
	}
	wantBlueMessages(t, rec, "MU Helper requires 400 zen.")
}

// TestMuHelperGateMessages 锁定四道门里其余两道（等级越界、重复开启）的蓝字。
func TestMuHelperGateMessages(t *testing.T) {
	t.Run("等级超上限", func(t *testing.T) {
		srv, sess, c, rec := newMuHelperSess(t, 1_000_000)
		c.Level = 500
		req := c2s.NewMuHelperStatusChangeRequest()
		srv.handleMuHelperStatus(sess, req.Bytes())
		wantBlueMessages(t, rec, "MU Helper cannot be used after level 400.")
		if sess.muHelper != nil {
			t.Fatal("越界等级不得启动")
		}
	})
	t.Run("重复开启", func(t *testing.T) {
		srv, sess, _, rec := newMuHelperSess(t, 1_000_000)
		req := c2s.NewMuHelperStatusChangeRequest()
		srv.handleMuHelperStatus(sess, req.Bytes())
		rec.frames = nil
		srv.handleMuHelperStatus(sess, req.Bytes())
		wantBlueMessages(t, rec, "MU Helper is already running.")
		if n := countFrames(rec, 0xC1, 0xBF); n != 0 {
			t.Fatalf("重复开启不应再发状态帧, got %d", n)
		}
		srv.muHelperStop(sess)
	})
}

// TestMuHelperFlagDrivesAttributeElement 锁定属性元素：运行中快照里 IsMuHelperActive=1，
// 关闭（含余额耗尽自动停）后回 0（对照 AddElement / RemoveElement）。
func TestMuHelperFlagDrivesAttributeElement(t *testing.T) {
	srv, sess, _, _ := newMuHelperSess(t, 1_000_000)
	req := c2s.NewMuHelperStatusChangeRequest()
	srv.handleMuHelperStatus(sess, req.Bytes())

	if cv := sess.getCombatValues(); cv == nil || cv.MuHelperActive != 1 {
		t.Fatalf("运行中标志应为 1, got %+v", cv)
	}
	srv.muHelperStop(sess)
	if cv := sess.getCombatValues(); cv == nil || cv.MuHelperActive != 0 {
		t.Fatalf("停止后标志应为 0, got %+v", cv)
	}
}

// TestMuHelperSaveDataParsesSettings 锁定 C2 AE 保存后 blob 被解码为会话设置
// （原版 UpdateMuHelperConfigurationAction 存原始字节 + 重解析）。
func TestMuHelperSaveDataParsesSettings(t *testing.T) {
	srv, sess, _, _ := newMuHelperSess(t, 1_000_000)
	req := c2s.NewMuHelperSaveDataRequest()
	hd := req.HelperData()
	hd[23] = 0x58 // 药品阈值 80% / 治疗阈值 50%
	hd[25] = 1 << 0
	srv.handleMuHelperSaveData(sess, req.Bytes())

	st := sess.getMuHelperSettings()
	if st == nil {
		t.Fatal("blob 应被解析为设置")
	}
	if st.PotionThresholdPercent != 80 || !st.UseHealPotion {
		t.Fatalf("解码不符: %+v", st)
	}
}

// TestEnterWorldEchoesMuHelperConfiguration 锁定进图回显 + 重新解析
// （原版 Player.SendMuHelperConfigurationAsync 与设置初始化插件）。
func TestEnterWorldEchoesMuHelperConfiguration(t *testing.T) {
	srv, sess, c, rec := newMuHelperSess(t, 1_000_000)
	blob := make([]byte, 257)
	blob[23] = 0x30 // 药品阈值 0%、治疗阈值 30%
	c.MuHelperConfiguration = blob
	sess.setMuHelperSettings(nil)
	sess.setState(entity.StateEnteringMap) // enterWorld 的幂等守卫要求该状态

	srv.enterWorld(sess, c)

	if f := findFrame(rec, 0xC2, 0xAE); f == nil {
		t.Fatal("进图应回显 C2 AE 挂机设置")
	}
	if st := sess.getMuHelperSettings(); st == nil || st.HealThresholdPercent != 30 {
		t.Fatalf("进图应重新解析设置: %+v", st)
	}
}

func TestMuHelperCollectChargesThenStopsOnEmpty(t *testing.T) {
	srv, sess, c, _ := newMuHelperSess(t, 1_000_000)
	req := c2s.NewMuHelperStatusChangeRequest()
	req.SetPauseStatus(false)
	srv.handleMuHelperStatus(sess, req.Bytes())
	mgr := sess.muHelper
	if mgr == nil {
		t.Fatal("应先启动")
	}
	afterStart := c.Stats.Money
	mgr.collect() // 再扣一次（elapsed≈0 → 仍 400）
	if c.Stats.Money != afterStart-400 {
		t.Fatalf("collect 应再扣 400: %d → %d", afterStart, c.Stats.Money)
	}
	// 耗尽后自动停止
	c.Stats.Money = 10
	mgr.collect()
	if sess.muHelper != nil {
		t.Fatal("资金耗尽应自动停止")
	}
}
