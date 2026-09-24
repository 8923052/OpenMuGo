package gameserver

import (
	"context"
	"errors"

	"mugo/internal/api"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
)

// errDuplicateServer 表示同 ID 的 GS 已注册。
func errDuplicateServer(id int) error { return errors.New("gameserver: 重复注册的 ServerID") }

// errNotStarted 表示服务尚未 Start（原版未初始化即开监听）。
var errNotStarted = errors.New("gameserver: 服务未启动，先 StartAsync")

// startCtx 是启动/停止上下文的本地别名。
type startCtx = context.Context

// Id 实现契约层（原版 Id = gameServerDefinition.ServerID）。
func (s *Server) Id() int { return s.serverID }

// ConfigurationId 实现契约层（原版 Guid；内存态无持久配置，返回固定占位）。
func (s *Server) ConfigurationId() string { return s.configurationID }

// Description 实现契约层。
func (s *Server) Description() string { return s.description }

// Type 实现契约层。
func (s *Server) Type() api.ServerType { return api.ServerTypeGameServer }

// ServerState 实现契约层。
func (s *Server) ServerState() api.ServerState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// MaximumConnections 实现契约层（原版取 ServerConfiguration.MaximumPlayers）。
func (s *Server) MaximumConnections() int { return s.maxConnections }

// CurrentConnections 实现契约层（原版 = Context.PlayerCount）。
// 当前 M6 的 world 只在进图后登记，登录未进图的连接不计入——与原版"玩家数"口径一致。
func (s *Server) CurrentConnections() int { return s.world.PlayerCount() }

// CreateServerInfo 对应原版 GameServer.CreateServerInfo。
func (s *Server) CreateServerInfo() api.ServerInfo {
	return api.NewServerInfo(uint16(s.serverID), s.description, s.CurrentConnections(), s.maxConnections)
}

// StartAsync 实现契约层 api.ManageableServer（原版 GameServer.StartAsync 的
// P4 两段分离形态）：只建服务，**不开端口**（端口在 StartListeners）。
func (s *Server) StartAsync(ctx context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state != api.ServerStateStopped {
		return nil // 已启动/启动中：幂等（原版 if (State == Stopped) 才启动）
	}
	s.state = api.ServerStateStarting
	s.deps.logger.Printf("gameserver: 服务构建中 id=%d", s.serverID)
	s.seedTerrains()
	s.startAILoop(ctx)
	s.state = api.ServerStateStarted
	return nil
}

// seedTerrains 把导出件里主变体地图的地形解析并挂到 world（T1-1）。
func (s *Server) seedTerrains() {
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	for i := range gc.Maps {
		mp := &gc.Maps[i]
		if mp.Discriminator != 0 || mp.Terrain == nil {
			continue
		}
		raw, err := mp.TerrainBytes()
		if err != nil {
			s.deps.logger.Printf("gameserver: 地图 %d 地形解码失败: %v", mp.Number, err)
			continue
		}
		s.world.SetTerrain(uint16(mp.Number), world.ParseTerrain(raw))
	}
	s.deps.logger.Printf("gameserver: 已挂载 %d 张主变体地图地形", len(gc.Maps))
}

// resolveCharStats 解析角色属性：有导出件走真实属性系统（T1-2），
// 否则退化为线性近似（旧行为）。
func (s *Server) resolveCharStats(c *entity.Character) (*entity.CharStats, error) {
	if gc := s.deps.cfg.GameConfig; gc != nil {
		return player.ResolveCharStats(gc, c)
	}
	return player.NewCharStats(c.ClassNumber, c.Level), nil
}

// StartListeners 开全部端点监听并注册到观察者（原版 listener.StartAsync 的内容；
// 容器两段分离后，端口在此阶段才开）。已开时幂等（原版 "listeners are always started"）。
func (s *Server) StartListeners(ctx context.Context) error {
	s.stateMu.Lock()
	if s.state == api.ServerStateStopped {
		s.stateMu.Unlock()
		return errNotStarted
	}
	s.stateMu.Unlock()

	if err := s.listenAll(ctx); err != nil {
		s.closeAllListeners()
		s.stateMu.Lock()
		s.state = api.ServerStateStopped
		s.stateMu.Unlock()
		s.deps.logger.Printf("gameserver: 开监听失败: %v", err)
		return err
	}
	return nil
}

// ShutdownAsync 实现契约层 api.ManageableServer（原版 GameServer.ShutdownAsync：
// Stopping → stop listeners → 注销观察者 → Stopped）。
func (s *Server) ShutdownAsync(ctx context.Context) error {
	s.stateMu.Lock()
	s.state = api.ServerStateStopping
	s.stateMu.Unlock()

	s.closeAllListeners()

	s.stateMu.Lock()
	s.state = api.ServerStateStopped
	s.stateMu.Unlock()
	s.deps.logger.Printf("gameserver: 已停止")
	return nil
}
