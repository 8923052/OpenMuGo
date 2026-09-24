package gameserver

import (
	"log"
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/transport"
	"mugo/internal/transport/crypto"
	"mugo/internal/version"
	"mugo/internal/view/remote"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// Authenticator 是 GS 需要的登录服务视图：
// 认证走契约层（api.AccountAuthenticator），取账号实体走本包子接口。
//
// 为什么不把 Entity 塞进 api：Entity 返回 *entity.Account，属领域类型，
// 放契约层会让 api → gamelogic 形成反向依赖。GS 本就依赖 gamelogic，
// 因此这个窄接口定义在 GS 侧最合适（Go 的接口按使用方定义）。
type Authenticator interface {
	api.AccountAuthenticator
	// Entity 取认证成功时绑定的账号实体（用于角色列表等）。
	Entity(name string) (*entity.Account, bool)
}

// deps 聚合处理器依赖。auth/login 都用接口，避免 gameserver → loginserver 的直接依赖。
type deps struct {
	auth     Authenticator
	login    api.LoginServer
	friend   friendService
	guild    guildService
	versions *version.Registry
	codecs   *version.CodecRegistry
	cfg      Config
	logger   *log.Logger
	xor3     crypto.Xor3
}

// newCodec 按端点版本从注册表选加密策略；未命中回落 S6E3 默认并打警告
// （原版 DefaultTcpGameServerListener.StartAsync：选不到插件 → LogWarning + 默认加密，不报错）。
func (d *deps) newCodec(ep Endpoint) crypto.Codec {
	if d.codecs != nil {
		if f := d.codecs.CodecFor(ep.Client.ClientVersion()); f != nil {
			return f.NewCodec()
		}
	}
	return crypto.NewS6E3ServerCodec()
}

// makeHandler 构造每连接帧处理器（分发 F1/F3 组，其他忽略）。
func (s *Server) makeHandler(sess *session) transport.PacketHandler {
	d := s.deps
	return func(conn *transport.Conn, frame []byte) {
		if len(frame) < 3 {
			return
		}
		codeIdx := transport.CodeIndex(frame[0])
		if len(frame) <= codeIdx {
			return
		}
		code := frame[codeIdx]
		hasSub := len(frame) > codeIdx+1
		sub := byte(0)
		if hasSub {
			sub = frame[codeIdx+1]
		}

		// 把"会改动角色/世界态"的 handler 与渡鸦攻击后台循环串行（同一 opMu）。
		// 读线程本就逐帧串行处理，此锁仅与本会话自己的宠物 goroutine 竞争。
		sess.opMu.Lock()
		defer sess.opMu.Unlock()

		switch code {
		case 0xF1:
			if hasSub {
				s.handleF1(sess, sub, frame)
			}
		case 0xF3:
			if hasSub {
				s.handleF3(sess, sub, frame)
			}
		case 0xF5: // 聊天命令族（S6E3 起；TRIM-08）
			if hasSub && sub == 0x00 {
				s.handleChatCommandListRequest(sess)
			}
		case 0x0E: // Ping：M4 不响应（原服仅用于检测），记录后忽略
			d.logger.Printf("gameserver: ping from %s", conn.RemoteAddr())
		case 0xD4: // WalkRequest
			s.handleWalk(sess, frame)
		case 0x11: // HitRequest（普通攻击）
			s.handleHit(sess, frame)
		case 0x22: // PickupItemRequest（C3；拾取地面物）
			s.handlePickupItem(sess, frame)
		case 0x23: // DropItemRequest（C3；丢弃背包物品）
			s.handleDropItem(sess, frame)
		case 0x24: // ItemMoveRequest / ItemMoveRequestExtended（C3；背包内搬运、装备穿脱）
			s.handleItemMove(sess, frame)
		case 0x26: // ConsumeItemRequest（C3；使用消耗品/药水）
			s.handleItemConsume(sess, frame)
		case 0x18: // AnimationRequest（坐下/倚靠/悬挂等姿态；驱动 IsResting 休息恢复）
			s.handleAnimation(sess, frame)
		case 0x30: // TalkToNpcRequest（C3；NPC 对话，T2-9）
			s.handleNpcTalk(sess, frame)
		case 0x31: // CloseNpcRequest（C1；关闭 NPC 对话，T2-9）
			s.handleNpcClose(sess, frame)
		case 0x32: // BuyItemFromNpcRequest（C3；NPC 商店买入，T2-9）
			s.handleNpcBuy(sess, frame)
		case 0x33: // SellItemToNpcRequest（C3；NPC 商店卖出，T2-9）
			s.handleNpcSell(sess, frame)
		case 0x81: // VaultMoveMoneyRequest（C1；仓库金钱存取）
			s.handleVaultMoney(sess, frame)
		case 0x82: // VaultClosed（C1；关闭仓库）
			s.handleVaultClose(sess, frame)
		case 0x83: // UnlockVault / SetVaultPin / RemoveVaultPin（C1 83 按 sub 分派）
			s.handleVaultLockSub(sess, sub, frame)
		case 0x19: // TargetedSkill（C3；定向技能，T2-11）
			s.handleTargetedSkill(sess, frame)
		case 0x1E: // AreaSkill（C3；区域技能自动命中，T2-11）
			s.handleAreaSkill(sess, frame)
		case 0x1B: // MagicEffectCancelRequest（C1；主动取消可取消的自增益）
			s.handleMagicEffectCancel(sess, frame)
		case 0x1C: // EnterGateRequest（C3；客户端按门编号请求进图）
			s.handleEnterGate(sess, frame)
		case 0x87: // CraftingDialogCloseRequest（关合成窗，与 C1 31 同语义）
			s.handleNpcClose(sess, frame)
		case 0x86: // ChaosMachineMixRequest（C1 86；合成，S8）
			s.handleMix(sess, frame)
		case 0xAE: // MuHelperSaveDataRequest（C2 AE；保存 MU Helper 程序）
			s.handleMuHelperSaveData(sess, frame)
		case 0xBC: // LahapJewelMixRequest（C1 BC；宝石升档合成 / 降档拆分）
			s.handleJewelMix(sess, frame)
		case 0xBF: // MuHelperStatusChangeRequest 等（C1 BF 按 sub 分派；本仓 0x51）
			s.handleMuHelperStatus(sess, frame)
		case 0x8E: // WarpCommandRequest（C1 8E 02；传送清单付费换图，T2-10）
			s.handleWarpCommand(sess, frame)
		case 0xA0: // LegacyQuestStateRequest（C1 A0；组 0 任务状态表，TRIM-07）
			s.handleLegacyQuestStateList(sess, frame)
		case 0xA2: // LegacyQuestStateSetRequest（C1 A2；legacy 任务接/交/放弃）
			s.handleLegacyQuestStateSet(sess, frame)
		case 0xA7: // PetCommandRequest（C1；切换渡鸦攻击行为）
			s.handlePetCommand(sess, frame)
		case 0xA9: // PetInfoRequest（C1；宠物信息窗口）
			s.handlePetInfo(sess, frame)
		case 0x15: // InstantMoveRequest：OpenMU 明确不处理（防瞬移）
		case 0x00: // PublicChatMessage（C1 00；公共聊天，S1）
			s.handlePublicChat(sess, frame)
		case 0x02: // WhisperMessage（C1 02；私聊，S1）
			s.handleWhisper(sess, frame)
		case 0x40: // PartyInviteRequest（C1 40；组队邀请，S2）
			s.handlePartyInvite(sess, frame)
		case 0x41: // PartyInviteResponse（C1 41；邀请应答，S2）
			s.handlePartyResponse(sess, frame)
		case 0x42: // PartyListRequest（C1 42；请求组队列表，S2）
			s.handlePartyListRequest(sess)
		case 0x43: // PartyPlayerKickRequest（C1 43；踢人/退队，S2）
			s.handlePartyKick(sess, frame)
		case 0x36: // TradeRequest（C1 36；发起交易，S3）
			s.handleTradeRequest(sess, frame)
		case 0x37: // TradeRequestResponse（C1 37；接受/拒绝交易，S3）
			s.handleTradeResponse(sess, frame)
		case 0x3A: // SetTradeMoney（C1 3A；设置交易金额，S3）
			s.handleTradeMoney(sess, frame)
		case 0x3C: // TradeButtonStateChange（C1 3C；确认按钮，S3）
			s.handleTradeButton(sess, frame)
		case 0x3D: // TradeCancel（C1 3D；取消交易，S3）
			s.handleTradeCancel(sess)
		case 0x34: // RepairItemRequest（C1 34；修理物品，S9）
			s.handleRepair(sess, frame)
		case 0xC1: // FriendAddRequest（C1 C1；加好友，P2）
			s.handleFriendAdd(sess, frame)
		case 0xC2: // FriendAddResponse（C1 C2；应答好友请求，P2）
			s.handleFriendAddResponse(sess, frame)
		case 0xC3: // FriendDelete（C1 C3；删除好友，P2）
			s.handleFriendDelete(sess, frame)
		case 0xC4: // SetFriendOnlineState（C1 C4；本人隐身/可见切换，P2）
			s.handleSetFriendOnlineState(sess, frame)
		case 0xCA: // ChatRoomCreateRequest（C1 CA；建聊天室，P2）
			s.handleChatRoomCreate(sess, frame)
		case 0xCB: // ChatRoomInvitationRequest（C1 CB；邀请进房，P2）
			s.handleChatRoomInvite(sess, frame)
		case 0xC5: // LetterSendRequest（C4 C5；寄信，P2）
			s.handleLetterSend(sess, frame)
		case 0xC7: // LetterReadRequest（C1 C7；读信，P2）
			s.handleLetterRead(sess, frame)
		case 0xC8: // LetterDeleteRequest（C1 C8；删信，P2）
			s.handleLetterDelete(sess, frame)
		case 0xC9: // LetterListRequest（C1 C9；列信，P2）
			s.handleLetterList(sess)
		case 0x52: // GuildListRequest（C1 52；成员列表，S6a）
			s.handleGuildList(sess)
		case 0x53: // GuildKickPlayerRequest（C1 53；踢人，S6a）
			s.handleGuildKick(sess, frame)
		case 0x55: // GuildCreateRequest（C1 55；创建，S6a）
			s.handleGuildCreate(sess, frame)
		case 0x50: // GuildJoinRequest（C1 50；向团长请求入盟，S6b）
			s.handleGuildJoinRequest(sess, frame)
		case 0x51: // GuildJoinResponse（C1 51；团长应答入盟，S6b）
			s.handleGuildJoinResponse(sess, frame)
		case 0x54: // GuildMasterAnswer（C1 54；团长弹创建窗，S6b）
			s.handleGuildMasterAnswer(sess, frame)
		case 0x66: // GuildInfoRequest（C1 66；战盟详情，S6b）
			s.handleGuildInfo(sess, frame)
		case 0xE1: // GuildRoleAssignRequest（C1 E1；改成员职位，S6b）
			s.handleGuildRoleAssign(sess, frame)
		case 0x57: // CancelGuildCreation（C1；关掉建盟窗）
			s.handleCancelGuildCreation(sess)
		case 0xE5: // GuildRelationshipChangeRequest（C1；发起同盟/敌对的建或解）
			s.handleGuildRelationshipRequest(sess, frame)
		case 0xE6: // GuildRelationshipChangeResponse（C1；被请求团长应答）
			s.handleGuildRelationshipResponse(sess, frame)
		case 0xE9: // RequestAllianceList（C1；看同盟清单）
			s.handleAllianceListRequest(sess)
		case 0xEB: // RemoveAllianceGuildRequest（C1 EB 01；把某盟移出同盟）
			if hasSub && sub == c2s.RemoveAllianceGuildRequestSubCode {
				s.handleRemoveAllianceGuild(sess, frame)
			}
		case 0xF6: // Quest 组（C1 F6 按 sub 分派：选中/开始/完成/取消/客户端动作/状态，S10）
			s.handleQuestPacket(sess, sub, frame)
		case 0x3F: // PlayerShop 组（按 sub 分派：定价/开/关/清单/买，S4）
			s.handlePlayerShopSub(sess, sub, frame)
		default:
			d.logger.Printf("gameserver: 忽略 code=0x%02X sub=0x%02X len=%d", code, sub, len(frame))
		}
	}
}

// handleF1 处理登录组：01 登录、02 登出。
func (s *Server) handleF1(sess *session, sub byte, frame []byte) {
	d := s.deps
	switch sub {
	case 0x01:
		s.handleLogin(sess, frame)
	case 0x02:
		s.handleLogout(sess, frame)
	case 0x03:
		// 客户端侧外挂检测上报：原版 LogOutByCheatDetectionHandlerPlugIn 记错误日志后
		// 按 CloseGame 走完整登出清理（不给回选角的机会）。
		who := ""
		if c := sess.getSelected(); c != nil {
			who = c.Name
		}
		d.logger.Printf("gameserver: 客户端外挂检测触发登出 %s", who)
		s.logout(sess, s2c.LogOutType_CloseGame)
	default:
		d.logger.Printf("gameserver: 忽略 F1 sub=0x%02X", sub)
	}
}

// handleLogout 处理 C3 F1 02：对照 OpenMU LogoutAction.RemoveFromGameAsync。
// 先把玩家从地图摘除并回收对象 ID（通知视野内其他人），再回登出响应，
// 最后按登出方式决定断连或回到角色选择（保持连接）。
func (s *Server) handleLogout(sess *session, frame []byte) {
	var t s2c.LogOutType
	if len(frame) >= int(c2s.LogOutLength) {
		t = s2c.LogOutType(c2s.AsLogOut(frame).Type())
	}
	s.logout(sess, t)
}

// logout 是登出的公共收尾（对照 LogoutAction.LogoutAsync）。
func (s *Server) logout(sess *session, t s2c.LogOutType) {
	d := s.deps
	// 世界清理：从地图摘除 + 回收对象 ID（原版 RemoveFromGameAsync 的前置）。
	// 视野内其他人需收到移出包（C1 14，ID 取真实对象 ID）。
	// 先停渡鸦攻击循环，避免玩家离场后其仍改动世界。
	if m := sess.getPetManager(); m != nil {
		m.stop()
		sess.setPetManager(nil)
	}
	if wp := sess.getWorldPlayer(); wp != nil {
		others := s.world.Map(wp.MapNumber).Leave(wp.ID)
		for _, o := range others {
			if o.View != nil {
				_ = o.View.ShowObjectsOutOfScope([]uint16{wp.ID})
			}
		}
		s.world.FreeID(wp.ID)
		sess.setWorldPlayer(nil)
	}
	s.notifyConnectionsChanged()
	// 下发登出响应（C3 F1 02，回显客户端请求的登出方式）。
	if err := s.viewFor(sess).ShowLogoutResponse(action.LogoutType(t)); err != nil {
		d.logger.Printf("gameserver: 发送登出响应失败: %v", err)
	}
	switch t {
	case s2c.LogOutType_BackToCharacterSelection:
		// 回到角色选择：保持连接，状态回 Authenticated（原版 PlayerState.TryAdvanceToAsync(Authenticated)）。
		// 客户端随后可重新请求 F3 00 角色列表。
		sess.setSelected(nil)
		sess.setState(entity.StateAuthenticated)
	case s2c.LogOutType_CloseGame:
		// 只发响应、**不**主动断开（与 BackToServerSelection 同语义），由客户端关闭连接。
		//
		// 原版 LogoutAction.LogoutAsync(CloseGame) 在发响应后立刻 DisconnectAsync，但它的
		// 前置 RemoveFromGameAsync 含 SaveProgressAsync（持久化保存），响应与 FIN 实际落在
		// 客户端已经退出之后。本仓登出是纯内存操作、快得多，若在这里紧跟响应就 CloseWrite，
		// FIN 会赶在客户端退出前到达，客户端会把这次断开误判为网络故障并闪一帧重连界面：
		//   - MuMain 点“退出游戏”只是把 SDL_EVENT_QUIT 入队（GameOverBtnDown：SendLogOut
		//     后紧跟 PostMessage(WM_CLOSE)），置 Destroy 要等下一帧的事件泵；
		//   - 同一帧末 SceneManager::CheckServerConnection 仍会执行，此时若
		//     SocketClient->IsConnected() 已因收到 FIN 变 false，且场景仍是 MAIN_SCENE、
		//     ReconnectManager 有会话，就会启动自动重连（ReconnectDialog 闪一帧）。
		// 客户端收到响应后本来就会自行退出并关闭 socket，读循环等到对端 EOF 即正常清理；
		// 兜底超时强制关闭防悬挂。
		s.scheduleForceClose(sess, closeGameGrace)
	default:
		// BackToServerSelection：原版只发响应、不主动断开，由客户端收到响应后自行断开。
		// 不能直接 Close（同样会发 RST 丢失响应，客户端回不到选服界面）；仅兜底超时强制关闭。
		s.scheduleForceClose(sess, backToServerGrace)
	}
}

// 兜底关闭等待：正常客户端在收到登出响应后毫秒级自行断开；
// BackToServerSelection 涉及客户端切场景重连 CS，给更宽松的窗口。
const (
	closeGameGrace    = 1 * time.Second
	backToServerGrace = 3 * time.Second
)

// scheduleForceClose 延迟兜底关闭：正常路径下对端先断开，Serve 读循环 EOF 并在
// 清理时 Close，本定时器到期后的 Close 因幂等（sync.Once）无副作用；
// 仅在客户端不响应、连接悬挂时才真正生效，避免会话与 socket 泄露。
func (s *Server) scheduleForceClose(sess *session, wait time.Duration) {
	go func() {
		time.Sleep(wait)
		_ = sess.conn.Close()
	}()
}

// handleLogin 复刻 LogInHandlerPlugIn：按帧长度区分三种登录包，
// Xor3 解用户名/密码，白名单校验版本，登录失败计数。
// 结果出站走视图层（T0-b）：语义码由动作层词汇表达，协议映射在 view/remote。
func (s *Server) handleLogin(sess *session, frame []byte) {
	d := s.deps

	// 同账号登录请求计数（连接级）。达到上限断连（对应 ConnectionClosed3Fails）。
	sess.mu.Lock()
	sess.loginFails++
	fails := sess.loginFails
	sess.mu.Unlock()
	if fails > d.cfg.FailLimit {
		d.logger.Printf("gameserver: 登录失败次数超限，断开 %s", sess.conn.RemoteAddr())
		_ = sess.conn.Close()
		return
	}

	var username, password string
	var versionBytes []byte
	switch {
	case len(frame) >= 42:
		msg := c2s.AsLoginLongPassword(frame)
		username = d.xor3.DecryptString(msg.Username())
		password = d.xor3.DecryptString(msg.Password())
		versionBytes = msg.ClientVersion()
	case len(frame) > 28+3:
		msg := c2s.AsLoginShortPassword(frame)
		username = d.xor3.DecryptString(msg.Username())
		password = d.xor3.DecryptString(msg.Password())
		versionBytes = msg.ClientVersion()
	default:
		msg := c2s.AsLogin075(frame)
		username = d.xor3.DecryptString(msg.Username())
		password = d.xor3.DecryptString(msg.Password())
		versionBytes = msg.ClientVersion()
	}

	// 版本白名单（OpenMU 对未知版本回落到默认版本；M4 严格拒绝，便于协议一致性验证）。
	// P2 追加端点语义：注册表里未注册的版本拒绝；注册了但与本连接**所属端点**绑定
	// 的版本不一致也拒绝——端点是"预期版本"的边界，跨端点连接会使后续包形态
	// 与端点加密策略错配，必须早拒。
	clientVer, ok := d.versions.Resolve(version.Key(versionBytes))
	if !ok {
		d.logger.Printf("gameserver: 拒绝版本 % X（用户 %s）", versionBytes, username)
		s.sendLoginResult(sess, action.LoginWrongVersion)
		return
	}
	if !s.endpointAllows(sess, clientVer) {
		d.logger.Printf("gameserver: 版本 %s 与端点绑定 %s 不符（用户 %s）",
			clientVer, sess.getEndpoint().Client.ClientVersion(), username)
		s.sendLoginResult(sess, action.LoginWrongVersion)
		return
	}

	outcome, info, ok := d.auth.Authenticate(username, password)
	if !ok {
		d.logger.Printf("gameserver: 登录拒绝 user=%s outcome=%s", username, outcome)
		s.sendLoginResult(sess, mapLoginOutcome(outcome))
		return
	}
	account, found := d.auth.Entity(info.Name)
	if !found {
		d.logger.Printf("gameserver: 认证成功但取不到账号实体 user=%s", username)
		s.sendLoginResult(sess, action.LoginConnectionError)
		return
	}

	// 成功：清失败计数、推进状态、记录账号与版本，回 Okay。
	sess.mu.Lock()
	sess.loginFails = 0
	sess.mu.Unlock()
	sess.setAccount(account)
	sess.setVersion(clientVer)
	sess.setState(entity.StateAuthenticated)
	d.logger.Printf("gameserver: 登录成功 user=%s version=%d.%d", username, clientVer.Season, clientVer.Episode)
	s.sendLoginResult(sess, action.LoginOkay)
}

func mapLoginOutcome(o api.LoginOutcome) action.LoginResult {
	switch o {
	case api.LoginInvalidPassword:
		return action.LoginInvalidPassword
	case api.LoginAccountAlreadyConnected:
		return action.LoginAccountAlreadyConnected
	case api.LoginAccountBlocked:
		return action.LoginAccountBlocked
	case api.LoginTemporaryBlocked:
		return action.LoginTemporaryBlocked
	default:
		return action.LoginConnectionError
	}
}

// sendLoginResult 经视图下发登录结果（T0-b：协议映射在 view/remote）。
func (s *Server) sendLoginResult(sess *session, result action.LoginResult) {
	if err := s.viewFor(sess).ShowLoginResult(result); err != nil {
		s.deps.logger.Printf("gameserver: 发送登录结果失败: %v", err)
	}
}

// viewFor 返回会话的出站视图（懒构造；形态选项在构造时按当前客户端版本决定）。
func (s *Server) viewFor(sess *session) action.PlayerView {
	sess.mu.Lock()
	if sess.playerView != nil {
		v := sess.playerView
		sess.mu.Unlock()
		return v
	}
	// 同一把锁内直接读字段（不得再调 getVersion 等会重复加锁的方法——mutex 不可重入）。
	extended := sess.clientVersion.UsesExtendedCharacterList()
	clientVersion := sess.clientVersion
	conn := sess.conn
	sess.mu.Unlock()

	v := remote.NewPlayerView(conn, extended, clientVersion, s.deps.logger)
	sess.mu.Lock()
	sess.playerView = v
	sess.mu.Unlock()
	return v
}

// showLocalizedMessage 下发蓝字系统提示（对照 PlayerMessageExtensions.ShowLocalizedBlueMessageAsync）。
func (s *Server) showLocalizedMessage(sess *session, key player.MessageKey, args ...any) {
	s.showBlueMessage(sess, player.LocalizedMessage(key, args...))
}

// showBlueMessage 下发原文蓝字（对照 ShowBlueMessageAsync——原版就地拼接的提示文案）。
func (s *Server) showBlueMessage(sess *session, message string) {
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowMessage(message, action.MessageBlueNormal)
	}
}
