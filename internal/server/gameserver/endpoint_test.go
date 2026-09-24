package gameserver

import (
	"context"
	"net"
	"testing"
	"time"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/persistence"
	"mugo/internal/server/loginserver"
	"mugo/internal/transport/crypto"
	"mugo/internal/version"
)

// startMultiVersion 起多端点 GS：每版本一个注入监听器，返回各端点真实地址。
func startMultiVersion(t *testing.T, defs []version.GameClientDefinition, codecs *version.CodecRegistry) ([]string, *Server) {
	t.Helper()
	store := persistence.NewMemoryStore()
	store.Add(&entity.Account{Name: "user", Password: "pass", State: entity.AccountStateNormal})
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())

	eps := make([]Endpoint, len(defs))
	lns := make([]net.Listener, len(defs))
	for i, d := range defs {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		lns[i] = ln
		t.Cleanup(func() { _ = ln.Close() })
		eps[i] = Endpoint{ListenAddr: "127.0.0.1:0", Client: d}
	}
	srv := New(0, "test", eps, nil, login, login, Config{FailLimit: 3, Codecs: codecs})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.ServeEndpoints(ctx, lns) }()
	time.Sleep(50 * time.Millisecond) // 等 accept 循环就绪
	addrs := make([]string, len(lns))
	for i, ln := range lns {
		addrs[i] = ln.Addr().String()
	}
	return addrs, srv
}

// TestFourVersionEndpoints 握手验收（doc/14 §6 P2）：
// 同进程 4 个端点（07500/09504/10404/20404）各自完成 GameServerEntered 握手，
// 且 F1 00 回显的 5 字节版本号与端点绑定一致。
func TestFourVersionEndpoints(t *testing.T) {
	defs := version.Original()
	addrs, _ := startMultiVersion(t, defs, nil)
	for i, d := range defs {
		d := d
		t.Run(d.Description, func(t *testing.T) {
			cl := dialLoginClient(t, addrs[i])
			if len(cl.enteredVersion) != 5 {
				t.Fatalf("F1 00 版本号长度错误: %q", cl.enteredVersion)
			}
			if string(cl.enteredVersion) != string(d.Version[:]) {
				t.Fatalf("版本回显 %q ≠ 端点绑定 %q", cl.enteredVersion, d.Version)
			}
		})
	}
}

// TestCrossVersionLoginRejected 跨版本连接被拒：20404 客户端连"仅 10404"的端点
// → WrongVersion，不得静默成功（doc/14 §6 P2）。
func TestCrossVersionLoginRejected(t *testing.T) {
	addrs, _ := startMultiVersion(t, []version.GameClientDefinition{version.GMOS6E3()}, nil)
	cl := dialLoginClient(t, addrs[0])
	cl.sendLogin(t, "user", "pass", []byte("20404"))
	resp := cl.recvPlain(t, time.Second)
	if resp[4] != 6 { // WrongVersion
		t.Fatalf("跨版本登录应被拒（WrongVersion=6），got %d: %X", resp[4], resp)
	}
}

// TestCodecFallback 锁定回落语义：注册表为空（选不到工厂）时
// 握手仍成功——原版"打警告 + 默认加密"，不是报错退出。
func TestCodecFallback(t *testing.T) {
	empty := version.NewCodecRegistry(nil)
	addrs, _ := startMultiVersion(t, []version.GameClientDefinition{version.MuMain()}, empty)
	cl := dialLoginClient(t, addrs[0])
	if len(cl.enteredVersion) != 5 {
		t.Fatal("回落默认加密后握手应成功")
	}
}

// TestCodecRegistrySelection 锁定精确命中：注册表含 S6E3 工厂时
// 走工厂创建的 codec（登录往返验证加解密管线完好）。
func TestCodecRegistrySelection(t *testing.T) {
	addrs, _ := startMultiVersion(t, []version.GameClientDefinition{version.MuMain()},
		version.NewCodecRegistry(crypto.S6E3Factories()))
	cl := dialLoginClient(t, addrs[0])
	cl.sendLogin(t, "user", "pass", []byte("20404"))
	resp := cl.recvPlain(t, time.Second)
	if resp[4] != 1 {
		t.Fatalf("走注册表 codec 的登录应成功: %X", resp)
	}
}
