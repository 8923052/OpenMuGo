// Command mugo 是 Go 版 MU 服务的入口。
//   - ConnectServer（默认 :44406，明文）：Hello、服务器列表、选服连接信息
//   - GameServer（多端点，S6E3 加密）：登录、选角、进图、行走。
//     每个端点绑定一个客户端版本（doc/14 P2）。
//   - ChatServer（独立端口）：聊天室房间管理（doc/14 P5.3）。
//   - GuildServer / FriendServer（无端口服务）：战盟与好友（P5.2/P5.3）。
//   - 服务容器（P4）：三阶段启动 Chat → Game → Connect，Ctrl-C 逆序优雅停止。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	"mugo/internal/persistence/seedtest"
	"mugo/internal/server/chatserver"
	"mugo/internal/server/connectserver"
	"mugo/internal/server/container"
	"mugo/internal/server/eventbus"
	"mugo/internal/server/friendserver"
	"mugo/internal/server/gameserver"
	"mugo/internal/server/guildserver"
	"mugo/internal/server/loginserver"
	"mugo/internal/transport/crypto"
	"mugo/internal/util"
	"mugo/internal/version"
)

// defaultSeed 是账号种子的编译期默认值：默认 none（不内置任何账号）。
// 需要 OpenMU 测试账号时由构建脚本 -ldflags "-X main.defaultSeed=openmu" 烧入，
// 或运行时 -seed=openmu 指定。
var defaultSeed = "none"

// knownVersions 是 -versions 可选的版本短名（顺序即注册表顺序，第一条为默认版本）。
var knownVersions = map[string]func() version.GameClientDefinition{
	"075":    version.V075,
	"095":    version.V095d,
	"gmo":    version.GMOS6E3,
	"mumain": version.MuMain,
}

// must 在装配失败时立即退出（原版 Startup 阶段 fatal）。
func must(err error) {
	if err != nil {
		log.Fatalf("装配失败: %v", err)
	}
}

// friendNotifierAdapter 对应原版 FriendNotifierToGameServer：
// 好友服的推送按 serverId 路由到对应 GS；无 serverId 的事件广播给全部 GS。
type friendNotifierAdapter struct{ container *gameserver.Container }

func (a friendNotifierAdapter) gs(serverId int) *gameserver.Server {
	g, ok := a.container.Get(serverId)
	if !ok {
		return nil
	}
	return g
}

func (a friendNotifierAdapter) FriendRequestAsync(requester, receiver string, serverId int) error {
	if g := a.gs(serverId); g != nil {
		return g.FriendRequestAsync(requester, receiver)
	}
	return nil
}

func (a friendNotifierAdapter) LetterReceivedAsync(letter api.LetterHeader) error {
	// 原版无 serverId：广播给全部 GS（各自判断接收方是否在线）。
	for _, g := range a.container.All() {
		if err := g.LetterReceivedAsync(letter); err != nil {
			return err
		}
	}
	return nil
}

func (a friendNotifierAdapter) FriendOnlineStateChangedAsync(playerServerId int, player, friend string, friendServerId int) error {
	if g := a.gs(playerServerId); g != nil {
		return g.FriendOnlineStateChangedAsync(player, friend, friendServerId)
	}
	return nil
}

func (a friendNotifierAdapter) ChatRoomCreatedAsync(serverId int, info api.ChatServerAuthenticationInfo, friendName string) error {
	if g := a.gs(serverId); g != nil {
		return g.ChatRoomCreatedAsync(info, friendName)
	}
	return nil
}

func (a friendNotifierAdapter) InitializeMessengerAsync(serverId int, data api.MessengerInitializationData) error {
	if g := a.gs(serverId); g != nil {
		return g.InitializeMessengerAsync(data)
	}
	return nil
}

// guildChangeAdapter 对应原版 GuildChangeToGameServerPublisher：
// 战盟变更按 serverId 路由（Assign）或广播（解散/踢人/同盟/敌对）。
type guildChangeAdapter struct{ container *gameserver.Container }

func (a guildChangeAdapter) all() []*gameserver.Server { return a.container.All() }

func (a guildChangeAdapter) GuildPlayerKickedAsync(playerName string) error {
	for _, g := range a.all() {
		if err := g.GuildPlayerKickedAsync(playerName); err != nil {
			return err
		}
	}
	return nil
}

func (a guildChangeAdapter) GuildDeletedAsync(guildId uint32) error {
	for _, g := range a.all() {
		if err := g.GuildDeletedAsync(guildId); err != nil {
			return err
		}
	}
	return nil
}

func (a guildChangeAdapter) AssignGuildToPlayerAsync(serverId uint8, characterName string, status api.GuildMemberStatus) error {
	if g := a.gs(int(serverId)); g != nil {
		return g.AssignGuildToPlayerAsync(characterName, status)
	}
	return nil
}

func (a guildChangeAdapter) gs(serverId int) *gameserver.Server {
	g, ok := a.container.Get(serverId)
	if !ok {
		return nil
	}
	return g
}

func (a guildChangeAdapter) AllianceCreatedAsync(masterGuildId, memberGuildId uint32) error {
	for _, g := range a.all() {
		if err := g.AllianceCreatedAsync(masterGuildId, memberGuildId); err != nil {
			return err
		}
	}
	return nil
}

func (a guildChangeAdapter) AllianceDisbandedAsync(masterGuildId, memberGuildId uint32) error {
	for _, g := range a.all() {
		if err := g.AllianceDisbandedAsync(masterGuildId, memberGuildId); err != nil {
			return err
		}
	}
	return nil
}

func (a guildChangeAdapter) GuildHostilityChangedAsync(guildIdA uint32, allianceA []uint32, guildIdB uint32, allianceB []uint32, created bool) error {
	for _, g := range a.all() {
		if err := g.GuildHostilityChangedAsync(guildIdA, allianceA, guildIdB, allianceB, created); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	csAddr := flag.String("csaddr", "127.127.127.127:44406", "ConnectServer 监听地址（0xAD00-0xADFF 明文区间）")
	chatAddr := flag.String("chataddr", "", "ChatServer 监听地址（空 = 仅房间管理，不开网络监听）")
	gsHost := flag.String("gshost", "127.0.0.2", "GameServer 监听 IP（各端点共用；多网卡时改为对外 IP）")
	gsPort := flag.Int("gsport", 55901, "GameServer 首个端点端口（后续端点依次 +1）")
	gsID := flag.Int("gsid", 0, "GameServer 的 ServerID（CS 列表中的编号）")
	// versions=逗号分隔的版本短名，每名一个端点、端口自增。默认只开 MuMain（保持单端点部署习惯）。
	versionsFlag := flag.String("versions", "mumain", "端点版本列表: 075|095|gmo|mumain 逗号分隔")
	gsPublicIP := flag.String("gsip", "", "下发给客户端的游戏服 IP（缺省 = -gshost）")
	// seed=openmu：装入 OpenMU 同款内存测试账号 test0..test9；none：空账号库（接 DB/自定义种子时使用）。
	// 默认值 defaultSeed 可在编译期用 ldflags 注入。
	seed := flag.String("seed", defaultSeed, "内存账号种子: openmu|none")
	flag.Parse()

	logger := log.New(os.Stdout, "[mugo] ", log.LstdFlags|log.Lmsgprefix)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 对外公布 IP：未指定时用监听 IP（必须非空——ConnectionInfo 包里的 IP 直接下发给客户端）。
	publicIP := *gsPublicIP
	if publicIP == "" {
		publicIP = *gsHost
	}

	// 端点装配：一个版本一个端点（原版 GameServerDefinition.Endpoints 语义）。
	names := strings.Split(*versionsFlag, ",")
	endpoints := make([]gameserver.Endpoint, 0, len(names))
	for i, raw := range names {
		name := normalizeVersionName(strings.TrimSpace(raw))
		def, ok := knownVersions[name]
		if !ok {
			logger.Fatalf("未知版本短名 %q（可选 075|095|gmo|mumain）", raw)
		}
		endpoints = append(endpoints, gameserver.Endpoint{
			ListenAddr: *gsHost + ":" + itoa(*gsPort+i),
			Client:     def(),
			PublishIP:  publicIP,
		})
	}
	for _, ep := range endpoints {
		logger.Printf("GS 端点: %s ← %s", ep.ListenAddr, ep.Client.Description)
	}

	// T0-c：游戏数值（data/season6，按版本 go:embed）——地形/属性装配/怪物数据源。
	// 先于种子装配：测试账号的物品宽高/耐久需查物品定义。
	gameCfg, err := config.LoadSeason6()
	if err != nil {
		logger.Fatalf("游戏配置载入失败: %v", err)
	}
	logger.Printf("游戏配置: %d 地图 / %d 怪物 / %d 物品 / %d 技能 / %d 职业",
		len(gameCfg.Maps), len(gameCfg.Monsters), len(gameCfg.Items), len(gameCfg.Skills), len(gameCfg.CharacterClasses))

	// 内存账号库；账号来源由 -seed 决定，种子逻辑与主线隔离（internal/persistence/seedtest）。
	store := persistence.NewMemoryStore()
	switch *seed {
	case "openmu":
		for _, a := range seedtest.Accounts(gameCfg) {
			store.Add(a)
		}
		logger.Print("已装入 OpenMU 测试账号种子全集：test0..9/300/400/ancient/socket/quest1-3/testgm/gm2/unlock（密码同名）")
	case "none":
		logger.Print("未装入账号种子（-seed=none）")
	default:
		logger.Fatalf("未知 -seed=%q（可选 openmu|none）", *seed)
	}
	sessions := loginserver.NewSessionRegistry()
	loginSvc := loginserver.NewLoginService(store, sessions)

	// ---- 服务装配（对应原版 Startup 的依赖注入顺序）----
	gsContainer := gameserver.NewContainer()
	// T1-5：怪物/NPC 生成（确定性种子；ID 从 0x8001 起避开玩家区）。
	spawner := npc.NewSpawner(gameCfg, util.NewRand(0x5EED), logger)
	spawner.SpawnAll()
	// T1-6/T2-1：地面掉落物注册表（过期清理随地图 tick，T2-3 掉落接线时启用）。
	dropRegistry := world.NewDropRegistry()
	gs := gameserver.New(*gsID, "GameServer "+itoa(*gsID), endpoints, logger, loginSvc, loginSvc, gameserver.Config{
		FailLimit:      3,
		Codecs:         version.NewCodecRegistry(crypto.S6E3Factories()),
		MaxConnections: 100,
		GameConfig:     gameCfg,
		NPCs:           spawner,
		Drops:          dropRegistry,
	})
	cs := connectserver.New(*csAddr, nil, logger)
	cs.SetSeason(endpoints[0].Client.Season)
	gs.SetStateObserver(cs)
	must(gsContainer.Add(*gsID, gs))

	// P5 社交三件套 + 事件总线（原版 AddGuildServer/AddFriendServer/AddChatServer 的装配序）：
	// GuildServer（变更推送 → GS）→ ChatServer（房间/认证）→ FriendServer（聊天室协调用 ChatServer）
	// → InMemoryEventPublisher（扇出：进/离场 guild→friend；聊天消息 → 全部 GS）。
	// 战盟服/好友服不是 IManageableServer（原版也不进容器）；ChatServer 进容器（Chat 阶段最先启动）。
	guild := guildserver.New(guildChangeAdapter{container: gsContainer}, logger)
	gs.SetGuildService(guild) // S6：GS 入站战盟调用直连战盟服
	chat := chatserver.New(chatserver.Config{
		ListenAddr:          *chatAddr,
		MaxConnections:      1000,
		RoomCleanUpInterval: time.Minute,
	}, logger)
	friend := friendserver.New(friendNotifierAdapter{container: gsContainer}, chat, logger)
	gs.SetFriendService(friend) // P2：GS 入站好友调用直连好友服
	publisher := eventbus.NewInMemoryPublisher(func() []eventbus.GameServerView {
		var out []eventbus.GameServerView
		for _, s := range gsContainer.All() {
			out = append(out, s)
		}
		return out
	}, friend, guild)
	gs.SetEventPublisher(publisher)

	// 服务容器（P4）：两段分离（StartAll 建服务 → StartAllListeners 开端口），
	// 三阶段顺序 Chat → Game → Connect；Ctrl-C 逆序优雅停止。
	all := container.New(logger)
	must(all.Add(chat))
	must(all.Add(gs))
	must(all.Add(cs))

	if err := all.StartAll(ctx); err != nil {
		logger.Fatalf("服务启动失败: %v", err)
	}
	if err := all.StartAllListeners(ctx); err != nil {
		logger.Fatalf("监听启动失败: %v", err)
	}
	logger.Print("全部服务已就绪")

	<-ctx.Done()
	logger.Print("收到退出信号，等待服务停止")
	if err := all.StopAll(ctx); err != nil {
		logger.Printf("停止过程出错: %v", err)
	}
	logger.Print("已停止")
}

// normalizeVersionName 容忍 shell/调用方把 "075"/"095" 当数字解析成 "75"/"95"：
// 纯数字短名补回前导零到 3 位（版本号本身是 5 位 ASCII，短名约定 3 位数字）。
func normalizeVersionName(s string) string {
	if s == "" {
		return s
	}
	allDigit := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigit = false
			break
		}
	}
	if !allDigit || len(s) >= 3 {
		return s
	}
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
