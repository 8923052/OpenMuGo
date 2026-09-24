package gameserver

// scope_test.go —— T2-1 视野差分测试：行走触发的双向移出包（C1 14）与
// 双向入视野包（C2 12），以及地面掉落物的视野下发（C2 20）。
// 复用 warp_test 的 packetRecorder / 会话脚手架；不启动监听，直接调 handler。

import (
	"bytes"
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	c2s "mugo/internal/proto/c2s"
	"mugo/internal/server/loginserver"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

// muMainClient 是真机客户端（20404 = Season 106 Episode 3）的版本轴：视图按它决定
// 系统消息前缀（Season > 0）与包变体（任务 0x1B/0x0C 的 C2 Extended 下限是 (106,3)）。
var muMainClient = version.MuMain().ClientVersion()

// newScopeTestSrv 构造带配置与掉落注册表的 GS（不 StartAsync，无地形 → 全可走）。
func newScopeTestSrv(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	return New(0, "test", eps, nil, login, login, Config{GameConfig: cfg, Drops: world.NewDropRegistry()})
}

// newScopedSession 构造一个已进图的会话（坐标 x,y；视图写入 rec）。
func newScopedSession(id uint16, name string, x, y byte, rec *packetRecorder) (*session, *world.Player) {
	c := &entity.Character{
		Name: name, ClassNumber: 0, Level: 20,
		MapNumber: 0, X: x, Y: y, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, id, Endpoint{})
	sess.setAccount(&entity.Account{Name: name})
	sess.setSelected(c)
	sess.setState(entity.StateEnteredWorld)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()
	wp := &world.Player{ID: id, MapNumber: 0, Name: name, X: x, Y: y, Class: 0, Appearance: make([]byte, 27)}
	return sess, wp
}

// walkFrame 构造从 (x,y) 沿方向 dir 连走 steps 步的 D4 行走帧。
func walkFrame(x, y, dir byte, steps int) []byte {
	payload := (steps + 1) / 2
	req := c2s.NewWalkRequest(c2s.WalkRequestRequiredSize(payload))
	req.SetSourceX(x)
	req.SetSourceY(y)
	req.SetStepCount(byte(steps))
	req.SetTargetRotation(dir)
	for i := 0; i < payload; i++ {
		req.Directions()[i] = dir<<4 | dir
	}
	return req.Bytes()
}

// frameCode 返回帧的 opcode：C1/C3 code@2（长度单字节）；C2/C4 code@3（长度双字节）。
func frameCode(f []byte) byte {
	if len(f) < 4 {
		return 0
	}
	if f[0] == 0xC2 || f[0] == 0xC4 {
		return f[3]
	}
	return f[2]
}

// countFrames 统计 recorder 中 (h, code) 匹配的帧数。
func countFrames(rec *packetRecorder, h, code byte) int {
	n := 0
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == h && frameCode(f) == code {
			n++
		}
	}
	return n
}

// findFrame 返回 recorder 中 (h, code) 匹配的最后一帧。
func findFrame(rec *packetRecorder, h, code byte) []byte {
	var out []byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == h && frameCode(f) == code {
			out = f
		}
	}
	return out
}

// TestWalkScopeDiffPackets 锁定 T2-1：a 与 b 相邻 → a 东行 14 步（距 13>12）
// → 双方互收 C1 14 移出包；a 西行走回 → 双方互收 C2 12 入视野包。
func TestWalkScopeDiffPackets(t *testing.T) {
	srv := newScopeTestSrv(t)
	recA, recB := &packetRecorder{}, &packetRecorder{}
	sessA, wpA := newScopedSession(7, "alpha", 20, 20, recA)
	sessB, wpB := newScopedSession(8, "beta", 21, 20, recB)
	wpA.View, wpB.View = sessA.playerView, sessB.playerView
	srv.world.Map(0).Enter(wpA)
	srv.world.Map(0).Enter(wpB)
	sessA.setWorldPlayer(wpA)
	sessB.setWorldPlayer(wpB)

	// a 向 SE 走两段各 10 步（nibble 3 = SouthEast，(+1,0)）：20→30→40。
	// 桶语义：a@40 覆盖桶 bx=3..6，b(21,20) 所在桶 bx=2 未覆盖 → 双向移出。
	srv.handleWalk(sessA, walkFrame(20, 20, 3, 10))
	srv.handleWalk(sessA, walkFrame(30, 20, 3, 10))
	if wpA.X != 40 || wpA.Y != 20 {
		t.Fatalf("a 行走后=(%d,%d), want (40,20)", wpA.X, wpA.Y)
	}
	if n := countFrames(recA, 0xC1, 0x14); n != 1 {
		t.Fatalf("a 应收到 1 帧移出包, got %d", n)
	}
	if n := countFrames(recB, 0xC1, 0x14); n != 1 {
		t.Fatalf("b 应收到 1 帧移出包, got %d", n)
	}
	// a 的移出包应含 b 的 ID（C1 14: d[3]=数量, 之后每 2B 一个 ID 大端）。
	outA := findFrame(recA, 0xC1, 0x14)
	if outA == nil || outA[3] != 1 {
		t.Fatalf("a 的移出包应含 1 个对象: %X", outA)
	}
	if id := uint16(outA[4])<<8 | uint16(outA[5]); id != 8 {
		t.Fatalf("a 的移出包对象 ID=%d, want 8 (b)", id)
	}
	// b 的移出包应含 a 的 ID。
	outB := findFrame(recB, 0xC1, 0x14)
	if outB == nil || outB[3] != 1 {
		t.Fatalf("b 的移出包应含 1 个对象: %X", outB)
	}
	if id := uint16(outB[4])<<8 | uint16(outB[5]); id != 7 {
		t.Fatalf("b 的移出包对象 ID=%d, want 7 (a)", id)
	}

	// a 向 NW 走两段各 10 步（nibble 7 = NorthWest，(-1,0)）回 (20,20)：
	// b 桶重新被覆盖 → 双方互收 C2 12。
	nScopeA, nScopeB := countFrames(recA, 0xC2, 0x12), countFrames(recB, 0xC2, 0x12)
	srv.handleWalk(sessA, walkFrame(40, 20, 7, 10))
	srv.handleWalk(sessA, walkFrame(30, 20, 7, 10))
	if wpA.X != 20 || wpA.Y != 20 {
		t.Fatalf("a 走回后=(%d,%d), want (20,20)", wpA.X, wpA.Y)
	}
	if n := countFrames(recA, 0xC2, 0x12); n <= nScopeA {
		t.Fatalf("a 走回后应收到 b 的入视野包")
	}
	if n := countFrames(recB, 0xC2, 0x12); n <= nScopeB {
		t.Fatalf("b 走回后应收到 a 的入视野包")
	}
}

// TestWalkWestFrameUsedForReturn 校正方向语义：-x 为 nibble 7（NorthWest），5 步 40→35。
func TestWalkWestFrameUsedForReturn(t *testing.T) {
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(9, "solo", 40, 40, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)

	srv.handleWalk(sess, walkFrame(40, 40, 7, 5))
	if wp.X != 35 {
		t.Fatalf("西行后 X=%d, want 35", wp.X)
	}
}

// TestDropsInScopePackets 锁定 T2-1 掉落物视野封包，**逐字节锚定客户端结构体**：
//
// 物品（C2 20 ItemsDropped）——客户端 WSclient.h/WSclient.cpp：
//
//	case 0x20 → ReceiveCreateItemViewportExtended
//	typedef struct { BYTE IdL, IdH, PositionX, PositionY; BYTE Item[5]; } PCREATE_ITEM_EXTENDED
//	id 由 MAKEWORD(IdH, IdL) 还原（等价于线上大端），fresh 位在 IdL 的 bit7；
//	Item 长度为 CalcItemLength() 推出的动态 5~15B，故本帧总长为 5+4+N 而非固定 21B。
//
// 金币（C1 2F MoneyDroppedExtended）——客户端与 OpenMU 生成物一致：
//
//	case 0x2F → ReceiveCreateMoney
//	typedef struct { PBMSG_HEADER Header; BYTE IsFreshDrop; WORD Id; BYTE X, Y; DWORD Amount; }
//	PBMSG_HEADER = {Code, Size, HeadCode} 共 3B，故 HeadCode(2F) 落在 [2]；
//	Id 是原生 WORD 直读（小端，与物品相反），Amount 小端，整帧恒 12B。
//
// 注意 0x20 的 MoneyDropped（C2，21B，MoneyNumber=15）是给 <106.3 客户端的老路径，
// 本客户端派发表把它按"物品"处理，故金币必须走 0x2F。
func TestDropsInScopePackets(t *testing.T) {
	it := &item.Item{Group: 2, Number: 4, Level: 5, Durability: 28}
	data := make([]byte, remote.ItemExtendedMaxSize)
	n := remote.EncodeItemExtended(it, data)
	if n != remote.ItemExtendedMinSize {
		t.Fatalf("无附加选项的物品应为 5B 扩展编码, got %d", n)
	}
	data = data[:n]

	rec := &packetRecorder{}
	view := remote.NewPlayerView(rec, true, muMainClient, nil)
	err := view.ShowDropsInScope(
		[]action.DropEntry{{ID: 42, X: 21, Y: 21, Data: data}},
		[]action.MoneyEntry{{ID: 43, X: 22, Y: 22, Amount: 1234}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.frames) != 2 {
		t.Fatalf("应下发 2 帧, got %d", len(rec.frames))
	}

	// 物品帧：C2 | sizeH sizeL | 20 | 数量1 | Id(2) X Y | 物品数据(5)
	//         总长 = 5(头+数量) + 4(Id/XY) + 5(数据) = 14
	want := []byte{
		0xC2, 0x00, 0x0E, 0x20, 0x01, // 头 + code + 数量
		0x00, 0x2A, 0x15, 0x15, // Id=42(BE) X=21 Y=21
		0x20, 0x04, 0x05, 0x1C, 0x00, // Group2|Number4, Level5, Dura28, flags=0
	}
	itemFrame := rec.frames[0]
	if len(itemFrame) != len(want) {
		t.Fatalf("ItemsDropped 帧长=%d, want %d: %X", len(itemFrame), len(want), itemFrame)
	}
	for i, b := range want {
		if itemFrame[i] != b {
			t.Fatalf("ItemsDropped[%d]=%02X, want %02X（完整帧 %X）", i, itemFrame[i], b, itemFrame)
		}
	}
	// 交叉校验字段语义（与上面的字节断言互为印证）。
	if id := uint16(itemFrame[5])<<8 | uint16(itemFrame[6]); id != 42 {
		t.Fatalf("物品帧 ID=%d, want 42", id)
	}
	if itemFrame[7] != 21 || itemFrame[8] != 21 {
		t.Fatalf("物品帧坐标异常: %X", itemFrame)
	}
	if !bytes.Equal(itemFrame[9:], data) {
		t.Fatalf("物品帧数据=%X, want %X", itemFrame[9:], data)
	}

	// 金币帧：C1 | 0C | 2F | fresh0 | Id=43(LE) | 16 16 | Amount=1234(LE)
	wantMoney := []byte{
		0xC1, 0x0C, 0x2F, 0x00,
		0x2B, 0x00,
		0x16, 0x16,
		0xD2, 0x04, 0x00, 0x00,
	}
	moneyFrame := rec.frames[1]
	if len(moneyFrame) != 12 {
		t.Fatalf("MoneyDroppedExtended 应为固定 12B, got %d: %X", len(moneyFrame), moneyFrame)
	}
	for i, b := range wantMoney {
		if moneyFrame[i] != b {
			t.Fatalf("MoneyDroppedExtended[%d]=%02X, want %02X（完整帧 %X）", i, moneyFrame[i], b, moneyFrame)
		}
	}
	// 交叉校验字段语义。
	if id := uint16(moneyFrame[4]) | uint16(moneyFrame[5])<<8; id != 43 {
		t.Fatalf("金币帧 ID=%d, want 43", id)
	}
	if moneyFrame[6] != 22 || moneyFrame[7] != 22 {
		t.Fatalf("金币帧坐标异常: %X", moneyFrame)
	}
	amount := uint32(moneyFrame[8]) | uint32(moneyFrame[9])<<8 | uint32(moneyFrame[10])<<16 | uint32(moneyFrame[11])<<24
	if amount != 1234 {
		t.Fatalf("金币金额=%d, want 1234", amount)
	}
}
