package gameserver

import (
	"net"
	"strconv"
	"testing"

	connect "mugo/internal/proto/connect"
	"mugo/internal/version"
)

// TestEndpointPublishData 锁定端点的"可发布属性"：
// CS 的 ConnectionInfo 只能吃端点的 PublishIP + PublishedPort，一个字段都不能空。
// 回归背景：装配层曾把空 PublishIP 喂给 CS（-gsip 未指定时），客户端选服后连不上 GS。
func TestEndpointPublishData(t *testing.T) {
	ep := Endpoint{
		ListenAddr: "127.0.0.1:55981",
		Client:     version.MuMain(),
		PublishIP:  "127.0.0.1",
	}
	if got := ep.PublishedPort(); got != 55981 {
		t.Fatalf("PublishedPort=%d, want 55981", got)
	}
	if ep.PublishIP == "" {
		t.Fatal("PublishIP 为空——ConnectionInfo 会下发改不了的空地址")
	}

	// AlternativePublishedPort 非 0 时上报端口被取代（监听不变）。
	alt := ep
	alt.AlternativePublishedPort = 60000
	if got := alt.PublishedPort(); got != 60000 {
		t.Fatalf("AlternativePublishedPort 未生效: got %d", got)
	}
	if alt.ListenAddr != "127.0.0.1:55981" {
		t.Fatal("AlternativePublishedPort 不得影响监听地址")
	}
}

// TestEndpointPublishFitsConnectionInfo 验证 PublishIP/PublishedPort 能无损装进
// ConnectionInfo 包（22B，IP 为 16 字节 null 填充串 + 端口 u16 LE）。
func TestEndpointPublishFitsConnectionInfo(t *testing.T) {
	ep := Endpoint{ListenAddr: "127.0.0.1:55981", Client: version.MuMain(), PublishIP: "192.168.1.5"}

	p := connect.NewConnectionInfo()
	p.SetIpAddress(ep.PublishIP)
	p.SetPort(uint16(ep.PublishedPort()))
	b := p.Bytes()
	if len(b) != 22 {
		t.Fatalf("ConnectionInfo 长度 %d", len(b))
	}
	if got := connect.AsConnectionInfo(b).IpAddressString(); got != "192.168.1.5" {
		t.Fatalf("IP 回读 %q", got)
	}
	if got := connect.AsConnectionInfo(b).Port(); got != 55981 {
		t.Fatalf("Port 回读 %d", got)
	}
}

// TestPublishIPDefaultsToListenHost 锁定装配回填语义：PublishIP 为空时必须等于监听 host
// （Server.New 的回填逻辑；本测试直接检查 New 后端点的值）。
func TestPublishIPDefaultsToListenHost(t *testing.T) {
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "test", eps, nil, nil, nil, Config{})
	if srv.endpoints[0].PublishIP != "127.0.0.1" {
		t.Fatalf("PublishIP 未回填监听 host: %q", srv.endpoints[0].PublishIP)
	}
}

// TestEndpointAddrReachable 冒烟：真实 Listen 后端点可被拨通。
func TestEndpointAddrReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ep := Endpoint{ListenAddr: ln.Addr().String(), Client: version.MuMain(), PublishIP: "127.0.0.1"}
	conn, err := net.Dial("tcp", net.JoinHostPort(ep.PublishIP, strconv.Itoa(ep.PublishedPort())))
	if err != nil {
		t.Fatalf("按公布属性拨号失败: %v", err)
	}
	_ = conn.Close()
}
