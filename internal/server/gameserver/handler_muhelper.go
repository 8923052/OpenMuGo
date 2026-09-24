package gameserver

// handler_muhelper.go —— MU Helper 入站与运行编排，对照 OpenMU：
//   - MuHelperSaveDataRequestHandlerPlugin（C2 AE）→ UpdateMuHelperConfigurationAction：
//     原样存 blob 到角色 + 回显（MuHelperConfigurationData）。服务器不解析程序内容。
//   - MuHelperStatusChangeRequestHandlerPlugIn（C1 BF 51）→ ChangeMuHelperStateAction：
//     Enabled→MuHelper.TryStart（校验等级/扣首次 Zen/起后台按 PayInterval 续费的循环），
//     Disabled→Stop。计费公式见 muhelper.ZenCost。
// 后台循环与连接读线程用 sess.opMu 串行（同 petManager），避免并发改 c.Stats。

import (
	"context"
	"sync"
	"time"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/muhelper"
	"mugo/internal/gamelogic/player"
	c2s "mugo/internal/proto/c2s"
	remoteMuHelper "mugo/internal/view/remote/muhelper"
)

// muHelperConfig 是本仓 MU Helper 服务器配置（默认照抄原版；测试可改短间隔）。
func (s *Server) muHelperConfig() muhelper.Configuration { return muhelper.DefaultConfiguration() }

type muHelperManager struct {
	s    *Server
	sess *session
	c    *entity.Character
	cfg  muhelper.Configuration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	startAt time.Time
}

// handleMuHelperSaveData 处理 C2 AE：存程序 blob 并原样回显。
func (s *Server) handleMuHelperSaveData(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	if c == nil {
		return
	}
	data := make([]byte, len(c2s.AsMuHelperSaveDataRequest(frame).HelperData()))
	copy(data, c2s.AsMuHelperSaveDataRequest(frame).HelperData())
	c.MuHelperConfiguration = data
	sess.setMuHelperSettings(remoteMuHelper.TryDeserialize(data))
	_ = s.viewFor(sess).ShowMuHelperConfiguration(data)
}

// handleMuHelperStatus 处理 C1 BF 0x51：切换 MU Helper 开关。
func (s *Server) handleMuHelperStatus(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	if int(c2s.MuHelperStatusChangeRequestSubCode) != int(frame[3]) {
		return
	}
	c := sess.getSelected()
	if c == nil {
		return
	}
	status := muhelper.StatusFromPause(c2s.AsMuHelperStatusChangeRequest(frame).PauseStatus())
	switch status {
	case muhelper.StatusEnabled:
		s.muHelperStart(sess, c)
	case muhelper.StatusDisabled:
		s.muHelperStop(sess)
	}
}

// muHelperStart 尝试开启（对照 MuHelper.TryStartAsync：已运行/等级/金额三道门）。
// 调用方（分发）已持 sess.opMu。
func (s *Server) muHelperStart(sess *session, c *entity.Character) {
	if sess.muHelper != nil && sess.muHelper.running {
		s.deps.logger.Printf("gameserver: MU Helper 已在运行 %s", c.Name)
		s.showLocalizedMessage(sess, player.MsgMuHelperAlreadyRunning)
		return
	}
	cfg := s.muHelperConfig()
	level := int(c.Level)
	if level < cfg.MinLevel {
		s.deps.logger.Printf("gameserver: MU Helper 等级 %d 低于下限 %d %s", level, cfg.MinLevel, c.Name)
		s.showLocalizedMessage(sess, player.MsgMuHelperMinimumLevel, cfg.MinLevel)
		return
	}
	if level > cfg.MaxLevel {
		s.deps.logger.Printf("gameserver: MU Helper 等级 %d 超过上限 %d %s", level, cfg.MaxLevel, c.Name)
		s.showLocalizedMessage(sess, player.MsgMuHelperMaximumLevel, cfg.MaxLevel)
		return
	}
	if c.Stats == nil {
		return
	}
	amount := muhelper.ZenCost(cfg, level, 0)
	if int(c.Stats.Money) < amount {
		s.deps.logger.Printf("gameserver: MU Helper 启动资金不足 %s 需 %d", c.Name, amount)
		s.showLocalizedMessage(sess, player.MsgMuHelperRequiresMoney, amount)
		return
	}
	c.Stats.Money -= uint32(amount)

	mgr := &muHelperManager{s: s, sess: sess, c: c, cfg: cfg, running: true, startAt: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	mgr.cancel = cancel
	sess.muHelper = mgr

	// 属性元素：原版 TryStart 里 Attributes.AddElement(ConstantElement(1), IsMuHelperActive)。
	if sess.setMuHelperActive(true) {
		s.refreshCombatValues(sess, c, sess.getResting())
	}

	view := s.viewFor(sess)
	_ = view.ShowMuHelperStatus(false, 0, false) // Start
	_ = view.ShowMuHelperStatus(true, uint32(amount), false)
	if cfg.PayInterval > 0 {
		go mgr.loop(ctx)
	}
}

// muHelperDeactivate 清属性元素并重算快照（Stop 与"余额耗尽自动停"共用）。
func (s *Server) muHelperDeactivate(sess *session, c *entity.Character) {
	if sess.setMuHelperActive(false) && c != nil {
		s.refreshCombatValues(sess, c, sess.getResting())
	}
}

// muHelperStop 关闭（若在运行则发 Stop 包）。调用方已持 opMu。
func (s *Server) muHelperStop(sess *session) {
	mgr := sess.muHelper
	if mgr == nil {
		return
	}
	mgr.mu.Lock()
	was := mgr.running
	mgr.running = false
	if mgr.cancel != nil {
		mgr.cancel()
		mgr.cancel = nil
	}
	mgr.mu.Unlock()
	if was {
		s.muHelperDeactivate(sess, sess.getSelected())
		_ = s.viewFor(sess).ShowMuHelperStatus(false, 0, true) // Stop
	}
	sess.muHelper = nil
}

// loop 是后台续费循环：每 PayInterval 触发一次 collect。
func (m *muHelperManager) loop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PayInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collect()
		}
	}
}

// collect 扣一次费；资金不足则自动停止。持 opMu 与读线程/其它切换串行。
func (m *muHelperManager) collect() {
	m.sess.opMu.Lock()
	defer m.sess.opMu.Unlock()

	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if !running {
		return
	}
	amount := muhelper.ZenCost(m.cfg, int(m.c.Level), time.Since(m.startAt))
	if m.c.Stats != nil && int(m.c.Stats.Money) >= amount {
		m.c.Stats.Money -= uint32(amount)
		_ = m.s.viewFor(m.sess).ShowMuHelperStatus(true, uint32(amount), false)
		return
	}
	m.s.muHelperDeactivate(m.sess, m.c)
	_ = m.s.viewFor(m.sess).ShowMuHelperStatus(false, 0, true) // 资金耗尽 → Stop
	m.mu.Lock()
	m.running = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
	m.sess.muHelper = nil
}
