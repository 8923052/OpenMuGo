package connectserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"mugo/internal/transport"
)

// startServer 在随机端口启动服务，返回拨号地址。
func startServer(t *testing.T, entries []ServerEntry) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(ln.Addr().String(), entries, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.ServeListener(ctx, ln) }()
	return ln.Addr().String()
}

func dialAndReadHello(t *testing.T, addr string) (net.Conn, *transport.Reader, []byte) {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	rd := transport.NewReader(raw, 0)
	hello, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	return raw, rd, hello
}

func TestHandshakeHello(t *testing.T) {
	addr := startServer(t, nil)
	_, _, hello := dialAndReadHello(t, addr)
	want := []byte{0xC1, 0x04, 0x00, 0x01}
	if !bytes.Equal(hello, want) {
		t.Fatalf("Hello 字节不匹配: got %X want %X", hello, want)
	}
}

func TestServerListAndConnectionInfo(t *testing.T) {
	entries := []ServerEntry{
		{ServerID: 0, LoadPercentage: 20, GameServerIP: "127.0.0.1", GameServerPort: 55901},
		{ServerID: 1, LoadPercentage: 99, GameServerIP: "10.0.0.5", GameServerPort: 55960},
	}
	addr := startServer(t, entries)
	raw, rd, _ := dialAndReadHello(t, addr)

	// 服务器列表请求 C1 F4 06
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xF4, 0x06}); err != nil {
		t.Fatal(err)
	}
	list, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	// P3 行为修正：序列化形态按 CS 的客户端版本（Season==0 → Old）而非 sub 码。
	// startServer 未设 Season → 期望 C2 Old 形态（sub 0x02）：
	// 长度 = 6 + 2×2（ServerLoadInfo2：id byte + load byte）= 10 = 0x000A
	if len(list) != 10 || list[0] != 0xC2 || list[1] != 0x00 || list[2] != 0x0A ||
		list[3] != 0xF4 || list[4] != 0x02 {
		t.Fatalf("服务器列表帧头错误（期望 Old 形态）: %X", list)
	}
	if list[5] != 0x02 { // ServerCount byte
		t.Fatalf("ServerCount 错误: %02X", list[5])
	}
	if !bytes.Equal(list[6:8], []byte{0x00, 0x14}) { // Id byte, Load byte（20%）
		t.Fatalf("条目 0 错误: %X", list[6:8])
	}
	if !bytes.Equal(list[8:10], []byte{0x01, 0x63}) { // Id=1, Load=99%
		t.Fatalf("条目 1 错误: %X", list[8:10])
	}

	// 现代选服请求 C1 F4 03 + ServerId LE(0x0001)，共 6 字节
	if _, err := raw.Write([]byte{0xC1, 0x06, 0xF4, 0x03, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	info, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 22 || info[0] != 0xC1 || info[1] != 0x16 || info[2] != 0xF4 || info[3] != 0x03 {
		t.Fatalf("ConnectionInfo 帧头错误: %X", info)
	}
	wantIP := append([]byte("10.0.0.5"), make([]byte, 16-len("10.0.0.5"))...)
	if !bytes.Equal(info[4:20], wantIP) {
		t.Fatalf("IP 字段错误: %X", info[4:20])
	}
	if info[20] != 0x98 || info[21] != 0xDA { // 55960 = 0xDA98，LE = 98 DA
		t.Fatalf("Port 字段错误: %02X%02X", info[20], info[21])
	}

	// 旧版选服请求（5 字节，ServerId 为单字节）
	if _, err := raw.Write([]byte{0xC1, 0x05, 0xF4, 0x03, 0x00}); err != nil {
		t.Fatal(err)
	}
	info2, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(info2) != 22 || info2[20] != 0x5D || info2[21] != 0xDA { // 55901 = 0xDA5D
		t.Fatalf("旧版 ConnectionInfo 错误: len=%d port=%02X%02X", len(info2), info2[20], info2[21])
	}

	// 未知包不应断连：发完仍能完成下一次合法交互
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xEE, 0x00}); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xF4, 0x06}); err != nil {
		t.Fatal(err)
	}
	// 二次列表（未知包后）：缓存命中，长度一致
	list2, err := rd.ReadPacket()
	if err != nil {
		t.Fatalf("未知包后连接已断开: %v", err)
	}
	if len(list2) != 10 {
		t.Fatalf("二次列表长度错误: %d", len(list2))
	}
}

// TestServerListModernSeason 锁定 P3 序列化分形态：Season>0 的 CS 用
// ServerListResponse（ServerCount 为 ushort），与 Season==0 的 Old 形态不同。
func TestServerListModernSeason(t *testing.T) {
	entries := []ServerEntry{
		{ServerID: 0, LoadPercentage: 20, GameServerIP: "127.0.0.1", GameServerPort: 55901},
		{ServerID: 1, LoadPercentage: 99, GameServerIP: "10.0.0.5", GameServerPort: 55960},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(ln.Addr().String(), entries, nil)
	srv.SetSeason(6)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.ServeListener(ctx, ln) }()

	raw, rd, _ := dialAndReadHello(t, ln.Addr().String())
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xF4, 0x06}); err != nil {
		t.Fatal(err)
	}
	list, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	// 现代形态：长度 = 7 + 2×4（ServerLoadInfo：id u16LE + load + pad）= 15 = 0x000F
	if len(list) != 15 || list[0] != 0xC2 || list[1] != 0x00 || list[2] != 0x0F ||
		list[3] != 0xF4 || list[4] != 0x06 {
		t.Fatalf("现代形态帧头错误: %X", list)
	}
	if list[5] != 0x00 || list[6] != 0x02 {
		t.Fatalf("ServerCount 错误: %02X%02X", list[5], list[6])
	}
	if !bytes.Equal(list[7:11], []byte{0x00, 0x00, 0x14, 0x00}) {
		t.Fatalf("条目 0 错误: %X", list[7:11])
	}
}

func TestUnknownServerIdKeepsSilent(t *testing.T) {
	addr := startServer(t, []ServerEntry{{ServerID: 0, GameServerIP: "127.0.0.1", GameServerPort: 55901}})
	raw, rd, _ := dialAndReadHello(t, addr)

	if _, err := raw.Write([]byte{0xC1, 0x05, 0xF4, 0x03, 0x05}); err != nil {
		t.Fatal(err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, err := rd.ReadPacket()
	if err == nil {
		t.Fatal("不存在的 serverId 不应返回 ConnectionInfo")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return // 预期：无响应且连接保持
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("不存在的 serverId 不应导致断连")
	}
}
