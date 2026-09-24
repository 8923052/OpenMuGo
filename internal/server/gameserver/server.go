// Package gameserver 是加密游戏服（S6E3：SimpleModulus + Xor32）。
// 每连接独立编解码器与登录会话；帧解密后按 (Code, SubCode) 分发。
//
// 多版本（doc/14 P2，对应原版 GameServerDefinition.Endpoints + DefaultTcpGameServerListener）：
// 一个 GameServer 可挂多个监听端点，每个端点绑定一个 GameClientDefinition 与加密策略——
// 端点决定"预期版本"（F1 00 回显它），真实版本以登录包内的 5 字节为准（须与端点一致），
// 随后重选 handler 与 View。入站 handler 按 (包类型, 客户端版本) 选择，
// 命名沿用原版后缀：handler_<功能>.go / handler_<功能>_075.go / handler_<功能>_095.go。
// 变体清单见 doc/11-multi-version-adaptation.md。
package gameserver

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/drops"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	"mugo/internal/transport"
	"mugo/internal/transport/crypto"
	"mugo/internal/version"

	s2c "mugo/internal/proto/s2c"
)

// Endpoint 对应原版 GameServerEndpoint（继承 ServerEndpoint）：
// 一个监听地址 + 端点绑定的客户端版本 + 对外公布端口。
type Endpoint struct {
	// ListenAddr 监听地址（原版 NetworkPort；Go 侧带 IP 便于多网卡）。
	ListenAddr string
	// Client 端点绑定的客户端版本定义（原版 ServerEndpoint.Client，必填）。
	Client version.GameClientDefinition
	// PublishIP 对外公布的 IP（原版由 IIpAddressResolver 解析；P4 前为静态配置）。
	// 空串表示公布监听地址的 IP 部分。
	PublishIP string
	// AlternativePublishedPort 对外公布端口（原版同名字段）：非 0 时上报给 CS 的是它，
	// 监听仍是 ListenAddr——支持代理/端口转发不改客户端地址。
	AlternativePublishedPort int
}

// Server 是游戏服：一个业务实例 + 多个监听端点（对应原版 GameServer + 其 listeners）。
// 身份与状态字段由 P3 的契约层实现（manageable.go）使用。
type Server struct {
	// 身份（原版 GameServerDefinition.ServerID / Description / ConfigurationId）。
	serverID        int
	description     string
	configurationID string
	maxConnections  int

	// 状态机（原版 ServerState 属性；stateMu 保护）。
	stateMu sync.RWMutex
	state   api.ServerState

	// 观察者：Start 时 RegisterGameServer、Shutdown 时 UnregisterGameServer、
	// 连接数变化时 CurrentConnectionsChanged（原版由 DefaultTcpGameServerListener 回调）。
	stateObserver api.GameServerStateObserver

	// eventPublisher 事件总线（P5.1）：玩家进/离游戏时发布（原版 IEventPublisher）。
	eventMu   sync.Mutex
	publisher api.EventPublisher

	// guildMu/guildByCharacter 记录角色的战盟归属（AssignGuildToPlayerAsync 存储，
	// 离场事件 guildId 的来源——原版由玩家身上的战盟数据提供）。
	guildMu          sync.RWMutex
	guildByCharacter map[string]api.GuildMemberStatus

	// parties 是组队状态机（S2）；以角色名为键，GS 内唯一。
	parties *partyManager

	// sessMu/sessions 为在连会话登记表（周期任务遍历：恢复/掉落过期等）。
	sessMu   sync.RWMutex
	sessions map[uint16]*session

	endpoints []Endpoint
	maxSize   int
	deps      deps
	world     *world.World
	dropGen   *drops.Generator // 掉落生成器（T2-3；GameConfig 注入时创建）
	// dropDelay 对照原版 DropItemDelayedAsync 的 Task.Delay(1000)：击杀后掉落整体延后落地。
	dropDelay time.Duration
	// npcDialogDelay 对照 TalkNpcAction.cs:50 的 Task.Delay(500)：商店 NPC 对话在开窗前等
	// 客户端对话动画。与 dropDelay 同理可注入 0 供测试同步断言。
	npcDialogDelay time.Duration
	// areaHitScheduler 把"延迟到达的面积技伤害"排队执行，对照 AreaSkillAttackAction.cs:334-347
	// 的 `Task.Run(() => { Task.Delay(attackDelay); ApplySkillAsync(...) })`（TRIM-11a）。
	// 默认 time.AfterFunc；测试注入假时钟以断言每发的时刻与到点复检。
	areaHitScheduler func(delay time.Duration, fn func())
	route            *routeFinder // 怪物追击绕障寻路器（T1-f；NPCs 注入时创建）

	listenMu  sync.Mutex
	listeners []net.Listener // 已建立的监听器（Start 与 Shutdown 并发安全的关键）
	cancelAll context.CancelFunc
}

// Config 是 GameServer 的业务配置。
type Config struct {
	// FailLimit 为连续登录失败上限（OpenMU 行为：3 次断连）。
	FailLimit int
	// Codecs 按版本选加密策略（对应原版加密工厂插件选择）。nil 时全部用 S6E3 默认。
	Codecs *version.CodecRegistry
	// MaxConnections 对外上报的最大连接数（原版 ServerConfiguration.MaximumPlayers）。
	// <=0 时取默认 100。
	MaxConnections int
	// GameConfig 为 T0-c 导出的游戏配置（T1-1 地形 / T1-2 属性装配消费）。
	// nil 时退化为旧行为：无地形碰撞、charstats 线性近似。
	GameConfig *config.GameConfig
	// NPCs 为怪物/NPC 生成器（T1-5）。nil 时无怪物。
	NPCs *npc.Spawner
	// Drops 为地面掉落物注册表（T1-6/T2-1）。nil 时无掉落物视野。
	Drops *world.DropRegistry
}

// partySizeOf 从配置取 GameConfiguration.MaximumPartySize（无配置返回 0 → newPartyManager 回落 5）。
func partySizeOf(cfg Config) int {
	if cfg.GameConfig != nil {
		return cfg.GameConfig.Globals.MaximumPartySize
	}
	return 0
}

// defaultKillLootDelay 对照原版 AttackableNpcBase.DropItemDelayedAsync 的 Task.Delay(1000)。
const defaultKillLootDelay = 1 * time.Second

// defaultNpcDialogDelay 对照 TalkNpcAction.cs:50 的 Task.Delay(500)。
const defaultNpcDialogDelay = 500 * time.Millisecond

// New 创建游戏服（多端点）。auth 提供认证 + 账号实体，login 提供会话占用——
// 两者都是接口，故 gameserver 不 import loginserver 实现包（doc/14 P1）。
func New(serverID int, description string, endpoints []Endpoint, logger *log.Logger, auth Authenticator, login api.LoginServer, cfg Config) *Server {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.FailLimit <= 0 {
		cfg.FailLimit = 3
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 100
	}
	eps := append([]Endpoint(nil), endpoints...)
	for i := range eps {
		if eps[i].PublishIP == "" {
			eps[i].PublishIP = hostOf(eps[i].ListenAddr)
		}
	}
	srv := &Server{
		serverID:         serverID,
		description:      description,
		configurationID:  "mugo-gs-" + itoa(serverID),
		maxConnections:   cfg.MaxConnections,
		state:            api.ServerStateStopped,
		endpoints:        eps,
		maxSize:          transport.MaxPacketSize,
		world:            world.New(),
		dropDelay:        defaultKillLootDelay,
		npcDialogDelay:   defaultNpcDialogDelay,
		areaHitScheduler: scheduleAfterDefault,
		guildByCharacter: make(map[string]api.GuildMemberStatus),
		parties:          newPartyManager(partySizeOf(cfg)),
		sessions:         make(map[uint16]*session),
		deps: deps{
			auth:     auth,
			login:    login,
			cfg:      cfg,
			logger:   logger,
			xor3:     crypto.NewXor3(),
			codecs:   cfg.Codecs,
			versions: version.NewRegistry(defsOf(eps)),
		},
	}
	// 怪物/NPC 实例 ID 预占进对象 ID 池（原版：怪物与玩家同池分配，怪物先建）。
	if cfg.NPCs != nil {
		ids := make([]uint16, 0, cfg.NPCs.Count())
		for _, n := range cfg.NPCs.All() {
			ids = append(ids, n.ID)
		}
		srv.world.ReserveIDs(ids)
		srv.route = newRouteFinder()
		srv.wireMonsterCombat()
	}
	// T2-3：掉落生成器（固定种子共享 world.RNG——与原版全局 Rand 语义一致）。
	if cfg.GameConfig != nil {
		srv.dropGen = drops.NewGenerator(cfg.GameConfig, srv.world.RNG())
	}
	return srv
}

// SetStateObserver 注入 CS 状态观察者（装配层在 Start 前调用；对应原版
// GameServerContainer.InitializeGameServer 里 listener 构造参数 stateObserver）。
func (s *Server) SetStateObserver(o api.GameServerStateObserver) {
	s.stateObserver = o
}

// SetEventPublisher 注入事件总线（P5.1；对应原版 GameServer 构造参数 eventPublisher）。
func (s *Server) SetEventPublisher(p api.EventPublisher) {
	s.eventMu.Lock()
	s.publisher = p
	s.eventMu.Unlock()
}

// publishEnteredGame 玩家进图时发布事件（原版玩家接入流程里的
// eventPublisher.PlayerEnteredGameAsync 调用点；characterId 用角色名，偏差登记见 doc/14）。
func (s *Server) publishEnteredGame(characterName string) {
	s.eventMu.Lock()
	p := s.publisher
	s.eventMu.Unlock()
	if p == nil {
		return
	}
	if err := p.PlayerEnteredGameAsync(uint8(s.serverID), characterName, characterName); err != nil {
		s.deps.logger.Printf("gameserver: 进场事件发布失败 %s: %v", characterName, err)
	}
}

// publishLeftGame 玩家离场时发布事件（guildId 取会话上的战盟归属；
// 原版签名带 guildId 默认参数）。
func (s *Server) publishLeftGame(characterName string) {
	s.eventMu.Lock()
	p := s.publisher
	s.eventMu.Unlock()
	if p == nil {
		return
	}
	guildID := s.guildOfCharacter(characterName)
	if err := p.PlayerLeftGameAsync(uint8(s.serverID), characterName, characterName, guildID); err != nil {
		s.deps.logger.Printf("gameserver: 离场事件发布失败 %s: %v", characterName, err)
	}
}

// publishGuildMessage 把战盟频道发言交给事件总线（对照 ChatMessageGuildProcessor 的
// publisher.GuildMessageAsync）：总线扇出到**全部**游戏服，本服由 GuildChatMessageAsync 落地。
func (s *Server) publishGuildMessage(guildID uint32, sender, message string) {
	if p := s.eventPublisher(); p != nil {
		if err := p.GuildMessageAsync(guildID, sender, message); err != nil {
			s.deps.logger.Printf("gameserver: 战盟聊天发布失败 %s: %v", sender, err)
		}
	}
}

// publishAllianceMessage 把同盟频道发言交给事件总线（对照 ChatMessageAllianceProcessor）。
func (s *Server) publishAllianceMessage(guildID uint32, sender, message string) {
	if p := s.eventPublisher(); p != nil {
		if err := p.AllianceMessageAsync(guildID, sender, message); err != nil {
			s.deps.logger.Printf("gameserver: 同盟聊天发布失败 %s: %v", sender, err)
		}
	}
}

// eventPublisher 取当前事件总线（可能未注入：单机测试或装配早期）。
func (s *Server) eventPublisher() api.EventPublisher {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	return s.publisher
}

// broadcastMessage 给本服全部在线玩家发一条系统消息
// （对照 GameContext.SendGlobalMessageAsync 的 ForEachPlayerAsync）。
func (s *Server) broadcastMessage(message string, msgType action.MessageType) {
	for _, sess := range s.trackedSessions() {
		if sess.getState() != entity.StateEnteredWorld {
			continue
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowMessage(message, msgType)
		}
	}
}

// ListenAndServe 便捷方法：StartAsync → StartListeners → 阻塞至 ctx 取消 → ShutdownAsync。
// 装配层（容器）用两段分离的接口；测试与简单部署用本方法。
func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.StartAsync(ctx); err != nil {
		return err
	}
	if err := s.StartListeners(ctx); err != nil {
		_ = s.ShutdownAsync(ctx)
		return err
	}
	defer func() { _ = s.ShutdownAsync(ctx) }()
	<-ctx.Done()
	return ctx.Err()
}

// listenAll 建立全部端点监听并启动 accept 循环（Start 的实现核心）。
// 任一端点失败回滚全部监听（与原版 StartAsync 失败 → foreach listener.Stop() 一致）。
func (s *Server) listenAll(ctx context.Context) error {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.listeners != nil {
		return nil // 已在监听：幂等
	}
	if len(s.endpoints) == 0 {
		return errNoEndpoints
	}
	listeners := make([]net.Listener, len(s.endpoints))
	for i, ep := range s.endpoints {
		ln, err := net.Listen("tcp", ep.ListenAddr)
		if err != nil {
			for _, l := range listeners[:i] {
				_ = l.Close()
			}
			return err
		}
		listeners[i] = ln
	}
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancelAll = cancel
	s.listeners = listeners
	for i, ln := range listeners {
		go s.acceptLoop(loopCtx, ln, s.endpoints[i])
	}

	// 注册到观察者（原版 DefaultTcpGameServerListener.StartAsync → stateObserver.RegisterGameServer）。
	// 端点归属哪个观察者由装配层按端点的客户端版本决定（ConnectServerContainer.GetObserver(endpoint.Client)）；
	// 单观察者部署（当前）直接下发。上报端口取 PublishedPort（AlternativePublishedPort 语义）。
	if o := s.stateObserver; o != nil {
		for _, ep := range s.endpoints {
			o.RegisterGameServer(s.CreateServerInfo(), api.EndPoint{IpAddress: ep.PublishIP, Port: ep.PublishedPort()})
		}
	}
	return nil
}

// closeAllListeners 停全部监听并注销观察者（Shutdown 的实现核心）。
func (s *Server) closeAllListeners() {
	s.listenMu.Lock()
	listeners := s.listeners
	s.listeners = nil
	cancel := s.cancelAll
	s.cancelAll = nil
	s.listenMu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
	if o := s.stateObserver; o != nil && listeners != nil {
		o.UnregisterGameServer(uint16(s.serverID))
	}
}

// notifyConnectionsChanged 把连接数变化转发给观察者（原版
// DefaultTcpGameServerListener.OnClientAcceptedAsync → stateObserver.CurrentConnectionsChanged）。
func (s *Server) notifyConnectionsChanged() {
	if o := s.stateObserver; o != nil {
		o.CurrentConnectionsChanged(uint16(s.serverID), s.CurrentConnections())
	}
}

// ServeEndpoints 测试注入入口：以给定监听器启动 accept 循环（不走 Start 状态机）。
// 连接从 accept 那一刻起就归属其端点（对应原版 OnClientAcceptedAsync）。
func (s *Server) ServeEndpoints(ctx context.Context, lns []net.Listener) error {
	if len(s.endpoints) == 0 {
		return errNoEndpoints
	}
	if lns != nil && len(lns) != len(s.endpoints) {
		return errListenerCount
	}
	// 先全部 Listen 再进入 accept 循环：半启动状态（部分端点失败）不应留下幽灵监听。
	listeners := lns
	if listeners == nil {
		listeners = make([]net.Listener, len(s.endpoints))
		for i, ep := range s.endpoints {
			ln, err := net.Listen("tcp", ep.ListenAddr)
			if err != nil {
				for _, l := range listeners[:i] {
					_ = l.Close()
				}
				return err
			}
			listeners[i] = ln
		}
	}
	for i, ln := range listeners {
		go s.acceptLoop(ctx, ln, s.endpoints[i])
	}
	<-ctx.Done()
	for _, ln := range listeners {
		_ = ln.Close()
	}
	return ctx.Err()
}

// ServeListener 兼容旧接口：单监听器注入（原 ServeListener 语义）。
// 多端点测试请用 ServeEndpoints。
func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	return s.ServeEndpoints(ctx, []net.Listener{ln})
}

func (s *Server) acceptLoop(ctx context.Context, ln net.Listener, ep Endpoint) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(raw, ep)
	}
}

func (s *Server) serveConn(raw net.Conn, ep Endpoint) {
	// 每连接独立 codec：SimpleModulus 计数器绝不能跨连接共享。
	// 策略按端点版本从注册表选（未命中回落默认 + 警告，原版语义）。
	conn := transport.NewSecureConn(raw, s.maxSize, s.deps.newCodec(ep))
	id := s.world.AllocID()
	sess := newSession(conn, id, ep)
	s.trackSession(sess)
	defer func() {
		s.untrackSession(sess)
		s.removeFromWorld(sess)
		s.world.FreeID(id)
		sess.release(s.deps.login)
		_ = conn.Close()
	}()

	s.deps.logger.Printf("gameserver: 客户端接入 %s id=%d", conn.RemoteAddr(), id)
	// MuMain 连上 GS 后等待 F1 00 GameServerEntered 才弹登录框，HeroKey 取自 PlayerId。
	s.sendGameServerEntered(sess)
	err := conn.Serve(s.makeHandler(sess))
	if err != nil {
		s.deps.logger.Printf("gameserver: 连接 %s 结束: %v", conn.RemoteAddr(), err)
	}
}

// sendGameServerEntered 下发 C1 F1 00（12B）。
// PlayerId 恒为哨兵值 0x200（原版 ShowLoginWindowPlugIn：PlayerId = ViewExtensions.ConstantPlayerId）；
// 真实对象 ID 在进图（AddAsync）时分配。版本号取**连接所属端点**绑定的客户端版本
// （原版语义：DefaultTcpGameServerListener.ClientVersion 来自该端点的 GameClientDefinition）。
func (s *Server) sendGameServerEntered(sess *session) {
	p := s2c.NewGameServerEntered()
	p.SetSuccess(true)
	p.SetPlayerId(world.ConstantPlayerID)
	if b, ok := s.deps.versions.VersionBytes(sess.getEndpoint().ClientVersion()); ok {
		p.SetVersionString(string(b[:]))
	} else {
		s.deps.logger.Printf("gameserver: 端点版本 %s 无注册字节，F1 00 置空", sess.getEndpoint().Client.Description)
	}
	if err := sess.conn.Send(p.Bytes()); err != nil {
		s.deps.logger.Printf("gameserver: 下发 GameServerEntered 失败: %v", err)
	}
}

// trackSession / untrackSession 维护在连会话登记表（周期恢复任务遍历用）。
func (s *Server) trackSession(sess *session) {
	s.sessMu.Lock()
	s.sessions[sess.id] = sess
	s.sessMu.Unlock()
}

func (s *Server) untrackSession(sess *session) {
	s.sessMu.Lock()
	delete(s.sessions, sess.id)
	s.sessMu.Unlock()
}

func (s *Server) trackedSessions() []*session {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	out := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// removeFromWorld 把会话当前角色从其地图摘除（断连/登出共用，幂等）。
func (s *Server) removeFromWorld(sess *session) {
	if sess.trade != nil {
		s.cancelTrade(sess.trade) // S3：离场/断连时取消进行中的交易（各自回退）
	}
	if sess.craftStorage != nil {
		s.returnCraftItems(sess) // S8：断连时退回混沌锅物品
	}
	if sess.muHelper != nil {
		s.muHelperStop(sess) // 停止挂机助手后台循环
	}
	if wp := sess.getWorldPlayer(); wp != nil {
		s.partyRemove(wp.Name) // S2：离场即退队（剩余成员刷新列表；解散则逐个清）
		s.world.Map(wp.MapNumber).Leave(wp.ID)
		s.world.FreeID(wp.ID) // 对象 ID 回收（原版 RemoveAsync → GiveBack）
		// M6 暂无离场封包；摘除注册表即可防止后续移动广播到死连接。
		sess.setWorldPlayer(nil)
		s.notifyConnectionsChanged()
		s.publishLeftGame(wp.Name) // P5.1：离场事件 → 战盟服/好友服
	}
}
