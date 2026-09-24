package gameserver

import (
	"context"
	"net"
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/persistence"
	"mugo/internal/server/loginserver"
	"mugo/internal/transport"
	"mugo/internal/transport/crypto"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

// testServer 启动只放行 MuMain 版本（20404）的 GS。
func testServer(t *testing.T, store persistence.Store) (*Server, string) {
	t.Helper()
	return testServerWithVersions(t, store, []version.GameClientDefinition{version.MuMain()})
}

// testServerWithVersions 启动带指定版本端点的 GS（每个版本一个端点，验证版本分支与端点判定）。
// 注入 T0-c 导出件：T1-1 地形碰撞与 T1-2 真实属性装配在生产与测试中同源生效。
func testServerWithVersions(t *testing.T, store persistence.Store, defs []version.GameClientDefinition) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	endpoints := make([]Endpoint, len(defs))
	for i, d := range defs {
		endpoints[i] = Endpoint{ListenAddr: "127.0.0.1:0", Client: d}
	}
	gameCfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatalf("载入游戏配置失败: %v", err)
	}
	srv := New(0, "test", endpoints, nil, login, login, Config{FailLimit: 3, GameConfig: gameCfg})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.ServeListener(ctx, ln) }()
	return srv, ln.Addr().String()
}

// loginClient 封装一条模拟 MuMain 的加密连接：
// 发送 Xor32（无条件，C1/C2 也做）→ SM（仅 C3/C4），接收 SM 解 S2C。
type loginClient struct {
	conn     net.Conn
	rd       *transport.Reader
	smSeal   *crypto.SimpleModulus
	smOpen   *crypto.SimpleModulus
	xor32    *crypto.Xor32
	xor3     crypto.Xor3
	playerID uint16 // F1 00 GameServerEntered 下发
	// enteredVersion 是 F1 00 回显的 5 字节版本号（多端点握手验收用）。
	enteredVersion []byte
}

func dialLoginClient(t *testing.T, addr string) *loginClient {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	cl := &loginClient{
		conn:   c,
		rd:     transport.NewReader(c, 0),
		smSeal: crypto.NewEncryptor(crypto.DefaultClientEncryptKeys),
		smOpen: crypto.NewDecryptor(crypto.DefaultClientDecryptKeys),
		xor32:  crypto.NewXor32(crypto.Xor32Key),
		xor3:   crypto.NewXor3(),
	}
	// MuMain 接入后首先收到 F1 00 GameServerEntered（HeroKey 来源）。
	entered := cl.recvPlain(t, time.Second)
	if len(entered) != 12 || entered[0] != 0xC1 || entered[2] != 0xF1 || entered[3] != 0x00 || entered[4] != 1 {
		t.Fatalf("GameServerEntered 错误: %X", entered)
	}
	cl.playerID = uint16(entered[5])<<8 | uint16(entered[6])
	cl.enteredVersion = append([]byte(nil), entered[7:12]...)
	return cl
}

// sendLogin 构造并加密发送 LoginLongPassword。
func (cl *loginClient) sendLogin(t *testing.T, user, pass string, version []byte) {
	t.Helper()
	p := make([]byte, 60)
	p[0] = 0xC3
	p[1] = 60
	p[2] = 0xF1
	p[3] = 0x01
	// C# 对整个定长字段做 Xor3（含零填充），服务端解密后重新得到 null 终止。
	putZ := func(dst []byte, s string) {
		copy(dst, s)
		cl.xor3.Apply(dst)
	}
	putZ(p[4:14], user)
	putZ(p[14:34], pass)
	// TickCount 偏移34 大端，保持 0；ClientVersion 偏移38，ClientSerial 偏移43 保持 0。
	copy(p[38:43], version)

	cl.sendPacket(p)
}

// sendPacket 按真实客户端管线发送一帧明文：
// Xor32（无条件，C1/C2 也做）→ SimpleModulus（仅 C3/C4）。
// 对照 ClientLibrary ConnectionManager.ConnectInner：
//
//	PipelinedXor32Encryptor(PipelinedSimpleModulusEncryptor(...).Writer)
func (cl *loginClient) sendPacket(p []byte) {
	buf := append([]byte(nil), p...) // Xor32 原地变换，避免污染调用方明文
	cl.xor32.Seal(buf)
	out := buf
	if crypto.IsEncrypted(buf[0]) {
		out = cl.smSeal.Seal(buf)
	}
	_, _ = cl.conn.Write(out)
}

// recvPlain 读取一帧；C1 透传，C3 走 SM 解密。
func (cl *loginClient) recvPlain(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	_ = cl.conn.SetReadDeadline(time.Now().Add(timeout))
	frame, err := cl.rd.ReadPacket()
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	if crypto.IsEncrypted(frame[0]) {
		frame, err = cl.smOpen.Open(frame)
		if err != nil {
			t.Fatalf("解密 S2C 帧失败: %v", err)
		}
	}
	return frame
}

func indexZeroByte(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

func (cl *loginClient) sendCharacterListRequest() {
	// C1 F3 00 00：不做 SimpleModulus，但仍要过客户的 Xor32（见 sendPacket）。
	cl.sendPacket([]byte{0xC1, 0x05, 0xF3, 0x00, 0x00})
}

func accounts() *persistence.MemoryStore {
	bobLook := make([]byte, remote.AppearanceSize)
	remote.EncodeAppearance(&item.Appearance{ClassNumber: 4}, bobLook) // 裸身 Dark Knight
	st := persistence.NewMemoryStore()
	st.Add(&entity.Account{
		Name: "user", Password: "pass", State: entity.AccountStateNormal,
		Characters: []entity.Character{
			{
				Slot: 0, Name: "Bob", Level: 100, Status: entity.CharacterStatusNormal,
				ClassNumber: 4, // DarkKnight（与 18B 外观编码一致）
				Appearance:  bobLook,
			},
		},
	})
	st.Add(&entity.Account{Name: "blocked", Password: "p", State: entity.AccountStateBanned})
	return st
}

// nakedDarkKnight 是 OpenMU 官方 SmallAxe 锚点同源的裸身 DK 外观（naked_darkknight golden）。
var nakedDarkKnight = [18]byte{
	0x20, 0xFF, 0xFF, 0xFF, 0xFF, 0xF3, 0x00, 0x00, 0x00,
	0xF8, 0x00, 0x00, 0xF0, 0xFF, 0xFF, 0xFF, 0x00, 0x00,
}

func TestLoginSuccessAndCharacterList(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)

	cl.sendLogin(t, "user", "pass", []byte("20404"))
	resp := cl.recvPlain(t, time.Second)
	if len(resp) != 5 || resp[0] != 0xC1 || resp[2] != 0xF1 || resp[3] != 0x01 || resp[4] != 1 {
		t.Fatalf("LoginResponse 错误: %X", resp)
	}

	cl.sendCharacterListRequest()
	list := cl.recvPlain(t, time.Second)
	// 20404 → CharacterListExtended：1 个 44 字节块 → 8+44=52=0x34（首块从偏移 8 开始）
	if len(list) != 52 || list[0] != 0xC1 || list[1] != 52 || list[2] != 0xF3 || list[3] != 0x00 {
		t.Fatalf("CharacterListExtended 帧头错误: %X", list)
	}
	if list[6] != 1 { // CharacterCount
		t.Fatalf("CharacterCount=%d", list[6])
	}
	nameField := list[9:19] // 块0 SlotIndex 在 8，Name 9..18（10 字节，null 终止）
	if i := indexZeroByte(nameField); i >= 0 {
		nameField = nameField[:i]
	}
	if string(nameField) != "Bob" {
		t.Fatalf("角色名错误: %q", list[9:19])
	}
	// 27B 扩展外观在块0偏移15..41（整帧 23..49）：首字节＝职业原始编号（DK=4），
	// 次字节＝Pose，随后 7 件闪光装备空槽 FF FF 00。
	if list[23] != 4 {
		t.Fatalf("扩展外观职业字节=%d want 4", list[23])
	}
	if list[24] != 0x00 || list[25] != 0xFF || list[26] != 0xFF || list[27] != 0x00 {
		t.Fatalf("扩展外观空槽编码错误: % X", list[24:28])
	}
	// 18B 预览外观（list[23:41]）与扩展外观是两套编码，这里只校验长度。
	if gotLook := list[23:41]; len(gotLook) != remote.AppearanceSize {
		t.Fatalf("外观切片长度错误: %d", len(gotLook))
	}
	// GuildPosition 为块0偏移42（整帧50）：无公会须 0xFF（客户端 G_NONE），
	// 发 0 会被当成普通成员，删角时本地拦截弹公会警告。
	if list[50] != 0xFF {
		t.Fatalf("GuildPosition=%d，期望 0xFF(Undefined)", list[50])
	}
}

// TestCharacterListCompactVariantForGmo 验证 10404（Season 6 Episode 3 GMO）仍走
// 34B 紧凑版角色列表 + 18B 预览外观（对照 OpenMU ShowCharacterListPlugIn），
// 并锁定裸身 DK 的 18B golden——两个版本分支的编码不能混用。
func TestCharacterListCompactVariantForGmo(t *testing.T) {
	_, addr := testServerWithVersions(t, accounts(), []version.GameClientDefinition{version.GMOS6E3()})
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "user", "pass", []byte("10404"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 1 {
		t.Fatalf("登录失败: %X", resp)
	}

	cl.sendCharacterListRequest()
	list := cl.recvPlain(t, time.Second)
	// C1 F3 00，1 个 34 字节块 → 8+34=42=0x2A
	if len(list) != 42 || list[0] != 0xC1 || list[2] != 0xF3 || list[3] != 0x00 {
		t.Fatalf("CharacterList 帧头错误: %X", list)
	}
	var gotLook [18]byte
	copy(gotLook[:], list[23:41])
	if gotLook != nakedDarkKnight {
		t.Fatalf("角色外观不匹配:\n got: %X\nwant: %X", gotLook, nakedDarkKnight)
	}
	// 紧凑块（34B）末字节为公会职位（整帧偏移41）：无公会须 0xFF。
	if list[41] != 0xFF {
		t.Fatalf("GuildPosition=%d，期望 0xFF(Undefined)", list[41])
	}
}

// TestCharacterListRequestMaskedWireBytes 复现实测抓到的线上字节：
// MuMain 发出的 C1 F3 00 请求经 Xor32 掩码后是 C1 05 F3 0D 15
// （0x00 ^ 0xF3 ^ Xor32Key[3]=0xFE = 0x0D）。修复前服务端把 C1 当纯明文，
// 分发器看到 code=F3 sub=0x0D 的"未知包"直接忽略，客户端登录后无响应卡死。
func TestCharacterListRequestMaskedWireBytes(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "user", "pass", []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 1 {
		t.Fatalf("登录失败: %X", resp)
	}

	// 不经客户端管线、直接写线上字节（等价于真机抓包）。
	if _, err := cl.conn.Write([]byte{0xC1, 0x05, 0xF3, 0x0D, 0x15}); err != nil {
		t.Fatal(err)
	}
	list := cl.recvPlain(t, time.Second)
	if len(list) != 52 || list[2] != 0xF3 || list[3] != 0x00 || list[6] != 1 {
		t.Fatalf("掩码后的角色列表请求未被正确还原: %X", list)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "user", "WRONG", []byte("20404"))
	resp := cl.recvPlain(t, time.Second)
	if resp[4] != 0 { // InvalidPassword
		t.Fatalf("结果=%d，期望 InvalidPassword(0)", resp[4])
	}
}

func TestLoginWrongVersion(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "user", "pass", []byte("99999"))
	resp := cl.recvPlain(t, time.Second)
	if resp[4] != 6 { // WrongVersion
		t.Fatalf("结果=%d，期望 WrongVersion(6)", resp[4])
	}
}

func TestLoginBlocked(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "blocked", "p", []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 5 {
		t.Fatalf("结果=%d，期望 AccountBlocked(5)", resp[4])
	}
}

func TestThreeFailsClosesConnection(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	for i := 0; i < 3; i++ {
		cl.sendLogin(t, "user", "bad", []byte("20404"))
		_ = cl.recvPlain(t, time.Second) // 每次失败都有响应
	}
	// 第 4 次尝试应在计数超限时被断开（无响应，读超时/EOF）。
	cl.sendLogin(t, "user", "bad", []byte("20404"))
	_ = cl.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := cl.rd.ReadPacket(); err == nil {
		t.Fatal("连续失败超限后应断开连接")
	}
}

func TestCharacterListRequiresLogin(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendCharacterListRequest()
	_ = cl.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := cl.rd.ReadPacket(); err == nil {
		t.Fatal("未登录请求角色列表不应得到响应")
	}
}

func TestDuplicateLoginRejected(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl1 := dialLoginClient(t, addr)
	cl2 := dialLoginClient(t, addr)
	cl1.sendLogin(t, "user", "pass", []byte("20404"))
	if r := cl1.recvPlain(t, time.Second); r[4] != 1 {
		t.Fatalf("首次登录应成功: %X", r)
	}
	cl2.sendLogin(t, "user", "pass", []byte("20404"))
	if r := cl2.recvPlain(t, time.Second); r[4] != 3 { // AccountAlreadyConnected
		t.Fatalf("重复登录结果=%d，期望 3", r[4])
	}
}
