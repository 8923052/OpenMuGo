package gameserver

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/server/loginserver"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

// packetRecorder 记录视图下发的封包（测试断言用）。
type packetRecorder struct {
	frames [][]byte
}

func (r *packetRecorder) Send(b []byte) error {
	cp := make([]byte, len(b))
	copy(cp, b)
	r.frames = append(r.frames, cp)
	return nil
}

// TestLorenciaGateLookup 锁定 T1-7 门解析：真实导出件中 Lorencia 的
// 门 23（213..217, 246..247 → Noria map 3, 等级 10）按坐标命中。
func TestLorenciaGateLookup(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	mp, ok := cfg.Map(0)
	if !ok {
		t.Fatal("Lorencia 不存在")
	}
	if len(mp.EnterGates) == 0 {
		t.Fatal("Lorencia 应有进门")
	}

	// 门 23 的矩形内一点。
	g := mp.EnterGateAt(215, 246)
	if g == nil {
		t.Fatal("门矩形内应命中")
	}
	if g.Number != 23 || g.Target == nil || g.Target.Map == nil || *g.Target.Map != 3 {
		t.Fatalf("门 23 应指向 map 3: %+v", g)
	}
	// 矩形外不命中。
	if mp.EnterGateAt(100, 100) != nil {
		t.Fatal("非门坐标不应命中")
	}
}

// TestWarpUpdatesCharacterAndWorld 锁定换图核心语义（Server.warpThroughGate）：
// 角色实体位置更新、旧图 AoI 摘除、92B 下发、状态回 EnteringMap。
func TestWarpUpdatesCharacterAndWorld(t *testing.T) {
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	store := persistence.NewMemoryStore()
	for _, acc := range seedAccountsForWarp() {
		store.Add(acc)
	}
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, login, login, Config{GameConfig: cfg})

	// 构造一个在 Lorencia 门 23 内的角色（等级 20 满足 level_requirement=10）。
	c := &entity.Character{
		Name: "warper", ClassNumber: 0, Level: 20,
		MapNumber: 0, X: 215, Y: 246, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(&entity.Account{Name: "warper"})
	sess.setSelected(c)
	sess.setState(entity.StateEnteringMap)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.publishedEnter = true // 换图重入不重复发布进场事件
	// 注入记录型视图（conn 为 nil，避免空指针；断言用 recorder 收到的 92B）。
	recorded := &packetRecorder{}
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(recorded, true, muMainClient, nil)
	sess.mu.Unlock()

	// 先把它放进 Lorencia 的 AoI（模拟已进图）。
	wp := &world.Player{ID: sess.id, MapNumber: 0, Name: c.Name, X: c.X, Y: c.Y}
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)

	// 门 23 → Noria (map 3) (148..155, 5..6)。
	lorencia, _ := cfg.Map(0)
	gate := lorencia.EnterGateAt(215, 246)
	if gate == nil {
		t.Fatal("门 23 未命中")
	}
	srv.warpThroughGate(sess, c, wp, gate)

	// 角色实体位置更新为目标出口门。
	if c.MapNumber != 3 {
		t.Fatalf("角色地图号=%d, want 3", c.MapNumber)
	}
	if int(c.X) < gate.Target.X1 || int(c.X) > gate.Target.X2 {
		t.Fatalf("落点 X=%d 不在目标门矩形内", c.X)
	}
	// 旧地图 AoI 已摘除。
	if p := srv.world.Map(0).Player(7); p != nil {
		t.Fatal("旧地图应已摘除玩家")
	}
	// 状态回 EnteringMap（等 F3 12 重建视野）。
	if sess.getState() != entity.StateEnteringMap {
		t.Fatalf("状态=%d, want EnteringMap", sess.getState())
	}
	// 新地图尚无该玩家（F3 12 后才进）。
	if p := srv.world.Map(3).Player(7); p != nil {
		t.Fatal("新地图不应提前有玩家")
	}
	// 换图出站 = MapChanged（C3 1C，真机修复 10：原版 WarpToAsync 发 MapChanged，
	// 不是 92B——92B 是选角登录专用，游戏内收到时 MuMain 不重载地图也不回 F3 12）。
	// 字节 golden（MuMain 消费语义，见 ShowMapChanged 注释）：
	//   d[3]=0x0F(SubCode)  d[4] bit0=1(IsMapChange)  d[5]=Map 高字节(≤255 恒 0)
	//   d[6]=Map  d[7]=X  d[8]=Y  d[9]=Rot。
	last := recorded.frames[len(recorded.frames)-1]
	if len(last) != s2c.MapChangedLength || last[0] != 0xC3 || last[2] != 0x1C {
		t.Fatalf("最后一帧应为 %d 字节 MapChanged: % X", s2c.MapChangedLength, last)
	}
	if last[3] != 0x0F || last[4]&1 != 1 || last[5] != 0 {
		t.Fatalf("MapChanged 头部/Flag 异常: % X", last)
	}
	mapID := int(last[6])
	if mapID != 3 {
		t.Fatalf("MapChanged 内地图号=%d, want 3", mapID)
	}
	if last[7] != c.X || last[8] != c.Y {
		t.Fatalf("MapChanged 坐标应与角色一致: frame=(%d,%d) char=(%d,%d)", last[7], last[8], c.X, c.Y)
	}
}

func seedAccountsForWarp() []*entity.Account {
	return nil // 由下方 test1DkChar 直接构造角色；占位以满足原测试结构
}
