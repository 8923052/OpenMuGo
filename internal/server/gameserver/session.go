package gameserver

import (
	"sync"
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	helper "mugo/internal/gamelogic/muhelper"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	"mugo/internal/transport"
	"mugo/internal/version"
)

// session 是单条连接的登录会话上下文。
// 帧回调在连接的读 goroutine 上串行执行，但 Logoff 可能由其他 goroutine 触发，
// 因此字段访问用 mu 保护。
type session struct {
	conn *transport.Conn
	// ep 是连接所属端点（原版 RemotePlayer 构造时持有该 listener 的 ClientVersion）。
	ep Endpoint

	// id 为本连接的会话序号（日志/测试标识）。进图后的**对象 ID**由 world.AllocID()
	// 在 enterWorld 时另行分配（原版 AddAsync 同池语义，玩家与怪物/NPC 共用
	// 0x201..0x7FFF），两者不是一回事——F1 00 下发的是哨兵值 0x200。
	id uint16

	mu            sync.Mutex
	state         entity.SessionState
	account       *entity.Account
	clientVersion version.ClientVersion // 登录帧解析出的客户端版本（决定下发哪些包版本）
	selected      *entity.Character     // F3 03 选定的角色
	worldPlayer   *world.Player         // F3 12 进图后的世界态
	loginFails    int
	combatVals    *player.CombatValues // 战斗/恢复属性快照（进图时解析，T2-2）
	lastRegen     time.Time            // 上次恢复时刻（原版 _lastRegenerate）
	// regenRem 保存周期恢复不足 1 点的小数余量（原版属性是 float，本仓是整数）。
	regenRem action.RegenRemainder
	// shieldHiatus 是护盾恢复被中断后的静置秒数（原版 ShieldRecoveryHiatus）。
	shieldHiatus float64
	// resting 是"休息中"状态（原版 Stats.IsResting，0x18 动画包置位、移动清零）。
	resting bool
	// muHelperActive 表示 MU Helper 正在运行（原版 MuHelper 往属性系统挂
	// IsMuHelperActive 元素；Stop/登出即移除）。
	muHelperActive bool
	// muHelperSettings 是 C2 AE blob 解析后的挂机设置（原版 player.MuHelperSettings）。
	muHelperSettings *helper.Settings
	// potionCooldownUntil 是药水冷却截止（原版 Player.PotionCooldownUntil，T2-6）。
	// 命名与访问器 potionReadyAt() 区分：Go 不允许字段与方法同名。
	potionCooldownUntil time.Time
	// combo 是角色身上的连击状态机（TRIM-06；原版 Player._comboStateLazy，按职业继承链
	// 解析出定义后懒建）。nil = 该职业链上没有连击定义。
	combo *action.Combo
	// openedNpc 是当前对话打开的 NPC（原版 Player.OpenedNpc，T2-9）。
	// 状态机简化：原版会推进 PlayerState.NpcDialogOpened；本仓会话状态粒度粗，
	// 保持 EnteredWorld（移动等处理器不受影响），商店上下文只由 openedNpc 表达。
	openedNpc *npc.Npc
	// effects 是角色当前活动的魔法效果表（buff；对照原版 player.MagicEffectsList）。
	effects        *action.MagicEffectsList
	playerView     action.PlayerView // 出站视图（T0-b，懒构造；viewFor）
	publishedEnter bool              // 进场事件已发布（换图重入不重复发布）
	closed         bool

	// opMu 把"会改动角色/世界态的操作"在本会话内串行：连接读线程的 handler 分发
	// 与渡鸦攻击后台 goroutine 都持它，避免经验/等级/背包/掉落与宠物攻击并发写
	// c.Stats/c.Level 竞态（C# 用 async 循环 + 单线程世界锁，这里用每会话互斥锁等价）。
	opMu sync.Mutex
	// lastAttackedTargetID 是玩家最近一次攻击的目标对象 ID（原版 Player.LastAttackedTarget，
	// 0 表示无；供渡鸦 AttackWithOwner 行为取用）。0xFFFF 是"无目标"哨兵，不复用。
	lastAttackedTargetID uint16
	// petManager 是黑暗渡鸦命令管理器（可训练攻击宠在右手槽时创建；nil = 无）。
	petManager *petManager

	// trade 是当前交易（S3；nil = 无）。tradeRequestFrom 是发给本会话、尚未应答的邀请者名。
	trade            *trade
	tradeRequestFrom string

	// store 是当前个人商店（S4；nil = 未开店）。
	store *playerStore

	// craftStorage 是混沌之锅临时容器（S8；nil = 未开合成 NPC 窗口）。
	craftStorage *storage.Storage

	// muHelper 是当前 MU Helper 运行管理器（离线挂机助手；nil = 未运行）。
	muHelper *muHelperManager

	// pendingGuildRequesterName 是发给本会话（团长）、尚未应答的入盟申请者角色名（S6b）。
	pendingGuildRequesterName string

	// pendingRelationship 是挂在本会话（团长）身上、尚未应答的战盟关系变更请求。
	// 原版是 Player.PendingAllianceRequest 元组，一次只允许一个。
	pendingRelationship *relationshipRequest

	// vaultLocked 是本次会话的仓库锁状态（原版 Player.IsVaultLocked：账号设了 PIN 即上锁，
	// 解锁只在会话内有效，下次选角重新按账号 PIN 计算）。
	vaultLocked bool
}

func newSession(conn *transport.Conn, id uint16, ep Endpoint) *session {
	return &session{conn: conn, id: id, ep: ep, state: entity.StateConnected}
}

// getEndpoint 返回连接所属端点（端点在连接生命周期内不变，无需加锁）。
func (s *session) getEndpoint() Endpoint { return s.ep }

// setCombatValues 存战斗/恢复属性快照（enterWorld 时解析一次）。
func (s *session) setCombatValues(cv *player.CombatValues) {
	s.mu.Lock()
	s.combatVals = cv
	s.mu.Unlock()
}

func (s *session) getCombatValues() *player.CombatValues {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.combatVals
}

// getEffects 返回（懒创建）角色的活动效果表。
func (s *session) getEffects() *action.MagicEffectsList {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.effects == nil {
		s.effects = action.NewMagicEffectsList()
	}
	return s.effects
}

// peekEffects 返回效果表，未创建时返回 nil（不触发懒创建）。
func (s *session) peekEffects() *action.MagicEffectsList {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effects
}

// beginRegen 取走距上次恢复的秒数，交出小数余量与护盾静置秒数的副本
// （原版 (now - _lastRegenerate) 与 ShieldRecoveryHiatus）。
func (s *session) beginRegen(now time.Time) (action.RegenRemainder, float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	elapsed := now.Sub(s.lastRegen).Seconds()
	s.lastRegen = now
	return s.regenRem, elapsed, s.shieldHiatus
}

// endRegen 写回小数余量与护盾静置秒数（护盾恢复的 hiatus 计时）。
func (s *session) endRegen(rem action.RegenRemainder, shieldHiatus float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regenRem = rem
	s.shieldHiatus = shieldHiatus
}

// setResting 更新休息状态，返回是否发生变化（变化方需重算恢复属性快照）。
func (s *session) setResting(v bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resting == v {
		return false
	}
	s.resting = v
	return true
}

func (s *session) getResting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resting
}

// setMuHelperActive 更新"助手运行中"标志，返回是否变化（变化方需重算属性快照）。
func (s *session) setMuHelperActive(v bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.muHelperActive == v {
		return false
	}
	s.muHelperActive = v
	return true
}

func (s *session) getMuHelperActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muHelperActive
}

// setMuHelperSettings 存下解析后的挂机设置（blob 无效时传 nil，语义即"无设置"）。
func (s *session) setMuHelperSettings(v *helper.Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.muHelperSettings = v
}

func (s *session) getMuHelperSettings() *helper.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muHelperSettings
}

// refreshCombatValues 按"休息中"状态与活动 buff 重算战斗/恢复属性快照
// （原版 IsResting 是属性系统动态输入；生命之光等 buff 也在此进入战斗数值）。
func (s *Server) refreshCombatValues(sess *session, c *entity.Character, resting bool) {
	var effects []action.MagicEffect
	if el := sess.getEffects(); el.Len() > 0 {
		effects = el.Snapshot()
	}
	cv, err := player.ResolveCombatValuesFlags(s.deps.cfg.GameConfig, c, resting, sess.getMuHelperActive(), effects)
	if err != nil {
		return
	}
	sess.setCombatValues(&cv)
	// 同步到世界态：怪物→玩家伤害钩子读的是 wp 上的减伤/防御（穿脱装备/上下宠即时生效）。
	injectDefenderStats(sess.getWorldPlayer(), cv)
}

// injectDefenderStats 把"作为守方"需要的属性投影到世界实体上。进图与刷新两条路径共用，
// 避免加字段时只补一处（历史上防御减伤就只在这两处各写了一份）。
func injectDefenderStats(wp *world.Player, cv player.CombatValues) {
	if wp == nil {
		return
	}
	wp.DefenseRatePvM = cv.DefenseRatePvM
	wp.DefensePvM = cv.DefensePvM
	wp.DamageReceiveMultiplier = cv.DamageReceiveMultiplier
	wp.ArmorDamageDecrease = cv.ArmorDamageDecrease
	wp.DefenseDecrement = cv.DefenseDecrement
	wp.GreaterDefenseBonus = cv.GreaterDefenseBonus
	wp.SoulBarrierReduction = cv.SoulBarrierReduction
	wp.SoulBarrierToll = cv.SoulBarrierToll
}

// remoteAddr 返回对端地址（连接已释放/测试桩为 nil 时返回占位，不 panic）。
func (s *session) remoteAddr() string {
	if s.conn == nil {
		return "unknown"
	}
	return s.conn.RemoteAddr().String()
}

func (s *session) setState(v entity.SessionState) {
	s.mu.Lock()
	s.state = v
	s.mu.Unlock()
}

func (s *session) getState() entity.SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *session) setAccount(a *entity.Account) {
	s.mu.Lock()
	s.account = a
	s.mu.Unlock()
}

func (s *session) getAccount() *entity.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account
}

func (s *session) setVersion(v version.ClientVersion) {
	s.mu.Lock()
	s.clientVersion = v
	s.mu.Unlock()
}

func (s *session) getVersion() version.ClientVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientVersion
}

func (s *session) setSelected(c *entity.Character) {
	s.mu.Lock()
	s.selected = c
	s.mu.Unlock()
}

func (s *session) getSelected() *entity.Character {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selected
}

func (s *session) setWorldPlayer(p *world.Player) {
	s.mu.Lock()
	s.worldPlayer = p
	s.mu.Unlock()
}

func (s *session) getWorldPlayer() *world.Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.worldPlayer
}

// potionReadyAt 返回药水冷却截止时刻；从未使用过药水时 ok=false（零值不等于"早已冷却"，
// 故必须用 ok 区分——原版 PotionCooldownUntil 是 DateTime? 可空语义）。
func (s *session) potionReadyAt() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.potionCooldownUntil.IsZero() {
		return time.Time{}, false
	}
	return s.potionCooldownUntil, true
}

func (s *session) setPotionReadyAt(t time.Time) {
	s.mu.Lock()
	s.potionCooldownUntil = t
	s.mu.Unlock()
}

// setOpenedNpc 记录/清除当前对话 NPC（T2-9；nil = 关闭对话）。
func (s *session) setOpenedNpc(n *npc.Npc) {
	s.mu.Lock()
	s.openedNpc = n
	s.mu.Unlock()
}

func (s *session) getOpenedNpc() *npc.Npc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openedNpc
}

// setLastAttackedTarget 记录玩家最近攻击的目标 ID（供渡鸦 AttackWithOwner）。
func (s *session) setLastAttackedTarget(id uint16) {
	s.mu.Lock()
	s.lastAttackedTargetID = id
	s.mu.Unlock()
}

// getLastAttackedTarget 返回最近攻击目标 ID（0 = 无）。
func (s *session) getLastAttackedTarget() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAttackedTargetID
}

// setPetManager 挂/摘渡鸦命令管理器（非空旧值先停其后台循环）。
func (s *session) setPetManager(m *petManager) {
	s.mu.Lock()
	old := s.petManager
	s.petManager = m
	s.mu.Unlock()
	if old != nil && old != m {
		old.stop()
	}
}

// getPetManager 返回当前渡鸦命令管理器（nil = 无）。
func (s *session) getPetManager() *petManager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.petManager
}

// release 在连接结束时释放登录会话（幂等）。
// login 是契约层接口，故不再依赖 loginserver 具体实现（doc/14 P1）。
func (s *session) release(login api.LoginServer) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	a := s.account
	s.account = nil
	pm := s.petManager
	s.petManager = nil
	s.mu.Unlock()
	if pm != nil {
		pm.stop() // 异常断线也要停渡鸦循环
	}
	if a != nil {
		_ = login.LogOffAsync(a.Name, 0)
	}
}
