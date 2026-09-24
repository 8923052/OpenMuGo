package gameserver

// npc_enter_test.go —— 端到端验证：真实 Spawner + handleClientReady，
// 玩家在 Lorencia 出生点 (139,127) 进图后应收到 C2 13 NPC 入视野包。

import (
	"fmt"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/persistence"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/server/loginserver"
	"mugo/internal/util"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

func TestClientReadySendsNpcsInScope(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	if sp.ByMap(0) == nil {
		t.Fatal("Lorencia 应有 NPC")
	}
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp, Drops: nil})

	rec := &packetRecorder{}
	c := &entity.Character{
		Name: "spawned", ClassNumber: 0, Level: 1,
		MapNumber: 0, X: 139, Y: 127, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(&entity.Account{Name: "spawned"})
	sess.setSelected(c)
	sess.setState(entity.StateEnteringMap)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()

	// 真实 handler 路径：客户端 F3 12。
	srv.handleClientReady(sess)

	// 应收到 C2 13（NPC 入视野）帧，且数量≥1；内容含 Elf Soldier(257)/Leo(371)/Julia(547) 之一。
	var npcFrame []byte
	for _, f := range rec.frames {
		if len(f) >= 5 && f[0] == 0xC2 && f[3] == 0x13 {
			npcFrame = f
		}
	}
	if npcFrame == nil {
		t.Fatalf("进图后未收到 C2 13 NPC 入视野包（帧数=%d）", len(rec.frames))
	}
	count := int(npcFrame[4])
	if count == 0 {
		t.Fatal("C2 13 帧内 NPC 数量为 0")
	}
	t.Logf("C2 13 帧长度=%d NPC数=%d 内容=% X", len(npcFrame), count, npcFrame)

	// 解析第一只 NPC：id(2) type(2) x y。
	off := 5
	id := uint16(npcFrame[off])<<8 | uint16(npcFrame[off+1])
	typ := uint16(npcFrame[off+2])<<8 | uint16(npcFrame[off+3])
	if id&0x8000 == 0 {
		t.Fatalf("NPC ID %X 缺少出生位 0x8000", id)
	}
	t.Logf("首只 NPC id=%X type=%d @(%d,%d)", id, typ, npcFrame[off+4], npcFrame[off+5])

	// F3 12 后状态应转入 EnteredWorld（真实路径防回归）。
	if sess.getState() != entity.StateEnteredWorld {
		t.Fatalf("状态=%d, want EnteredWorld", sess.getState())
	}
}

// TestSelectCharacterEntersWorldImmediately 锁定修正后的进图流程：
// 选角（F3 03）后**立即**进世界并下发 NPC 视野包，不等客户端 F3 12
// （对照 OpenMU SelectCharacterAsync 尾部直接 ClientReadyAfterMapChangeAsync）。
// 真实客户端初始进图不发 F3 12——本测试是"实测看不到 NPC"缺陷的回归锁。
func TestSelectCharacterEntersWorldImmediately(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp})

	rec := &packetRecorder{}
	acc := &entity.Account{Name: "realclient"}
	acc.Characters = append(acc.Characters, entity.Character{
		Name: "realclient", ClassNumber: 0, Level: 1,
		MapNumber: 0, X: 139, Y: 127, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	})
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(acc)
	sess.setState(entity.StateAuthenticated)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()

	// 真实客户端的完整进图序列：只发 F3 03 选角，没有 F3 12。
	req := c2s.NewSelectCharacter()
	req.SetName("realclient")
	srv.handleSelectCharacter(sess, req.Bytes())

	if sess.getState() != entity.StateEnteredWorld {
		t.Fatalf("选角后状态=%d, want EnteredWorld（应立即进世界）", sess.getState())
	}
	if wp := sess.getWorldPlayer(); wp == nil {
		t.Fatal("选角后应已在地图 AoI 中")
	}
	npcFrame := findFrame(rec, 0xC2, 0x13)
	if npcFrame == nil || npcFrame[4] == 0 {
		t.Fatalf("选角后应收到 NPC 入视野包: %X", npcFrame)
	}
	t.Logf("选角即进世界：C2 13 NPC数=%d", npcFrame[4])

	// 之后再收到 F3 12（客户端加载完地图补发）→ 幂等忽略，不重复进世界。
	framesBefore := len(rec.frames)
	srv.handleClientReady(sess)
	if sess.getState() != entity.StateEnteredWorld {
		t.Fatalf("重复 F3 12 后状态异常: %d", sess.getState())
	}
	if got := len(rec.frames); got != framesBefore {
		t.Fatalf("重复 F3 12 不应产生新帧: %d → %d", framesBefore, got)
	}
}

// TestWalkNpcScopeDiff 锁定行走驱动的 NPC 视野差分（真机缺陷"走到有怪的地方看不到新 NPC"）：
// 初始 (139,127) 视野内有 Julia(547)@139,138 → 西行走远后收到 C1 14 含其 ID；
// 靠近 Hanzo(251)@116,141 后收到含其 TypeNumber 的 C2 13。
func TestWalkNpcScopeDiff(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp})

	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "walker", 139, 127, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	// 手工进场（不经 enterWorld）：补发与 enterWorld 相同的初始 NPC 快照。
	if entries := srv.npcsInScope(sp.ByMap(0), wp.X, wp.Y); len(entries) > 0 {
		_ = sess.playerView.ShowNpcsInScope(entries)
	}

	// 找 Julia(547) 与 Hanzo(251) 的实例 ID。
	var juliaID, hanzoID uint16
	for _, n := range sp.ByMap(0) {
		if n.Def.Number == 547 {
			juliaID = n.ID
		}
		if n.Def.Number == 251 {
			hanzoID = n.ID
		}
	}
	if juliaID == 0 || hanzoID == 0 {
		t.Fatal("Lorencia 应有 Julia/Hanzo")
	}

	// 初始快照应含 Julia（d=11 ≤12）。
	inScope := func(ids []uint16, want uint16) bool {
		for _, id := range ids {
			if id&0x7FFF == want {
				return true
			}
		}
		return false
	}
	var initialIDs []uint16
	for _, f := range rec.frames {
		if len(f) >= 5 && f[0] == 0xC2 && f[3] == 0x13 {
			for i := 0; i < int(f[4]); i++ {
				off := 5 + i*10
				initialIDs = append(initialIDs, uint16(f[off])<<8|uint16(f[off+1]))
			}
		}
	}
	if !inScope(initialIDs, juliaID) {
		t.Fatalf("初始快照应含 Julia id=%X: %X", juliaID, initialIDs)
	}

	// 西行（nibble 7 = NorthWest，-x）至 (116,127)：Julia d=13 >12 → 移出。
	for x := 139; x > 116; x -= 5 {
		step := 5
		if x-step < 116 {
			step = x - 116
		}
		srv.handleWalk(sess, walkFrame(byte(x), 127, 7, step))
	}
	if wp.X != 116 {
		t.Fatalf("应走到 116, got %d", wp.X)
	}
	// 移出包应含 Julia（C1 14：d[3]=数量，后续每 2B 一个 ID）。
	foundGone := false
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0x14 {
			for i := 0; i < int(f[3]); i++ {
				if uint16(f[4+i*2])<<8|uint16(f[5+i*2]) == juliaID {
					foundGone = true
				}
			}
		}
	}
	if !foundGone {
		t.Fatalf("西行后应收到含 Julia 的移出包")
	}

	// 南行（nibble 5 = NorthEast，+y）至 (116,141)：Hanzo@116,141 进入视野（C2 13）。
	for y := 127; y < 141; y += 5 {
		step := 5
		if y+step > 141 {
			step = 141 - y
		}
		srv.handleWalk(sess, walkFrame(116, byte(y), 5, step))
	}
	if wp.Y != 141 {
		t.Fatalf("应走到 Y=141, got %d", wp.Y)
	}
	foundHanzo := false
	for _, f := range rec.frames {
		if len(f) >= 5 && f[0] == 0xC2 && f[3] == 0x13 {
			for i := 0; i < int(f[4]); i++ {
				off := 5 + i*10
				if uint16(f[off+2])<<8|uint16(f[off+3]) == 251 {
					foundHanzo = true
				}
			}
		}
	}
	if !foundHanzo {
		t.Fatalf("南行靠近后应收到含 Hanzo(251) 的入视野包")
	}
}
func TestClientReadyFrameOrder(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp})

	rec := &packetRecorder{}
	c := &entity.Character{
		Name: "ordered", ClassNumber: 0, Level: 1,
		MapNumber: 0, X: 139, Y: 127, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(&entity.Account{Name: "ordered"})
	sess.setSelected(c)
	sess.setState(entity.StateEnteringMap)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()

	srv.handleClientReady(sess)

	// 打印帧序列（首字节/长度/码位），供人工核对顺序。
	seq := ""
	for i, f := range rec.frames {
		if i < 8 {
			code := f[2]
			if f[0] == 0xC2 && len(f) > 3 {
				code = f[3]
			}
			seq += fmt.Sprintf("%X(%d) ", code, len(f))
		}
	}
	t.Logf("帧序列: %s（共 %d 帧）", seq, len(rec.frames))
}
