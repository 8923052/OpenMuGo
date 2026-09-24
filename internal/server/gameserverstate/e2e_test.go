package gameserverstate

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"mugo/internal/api"
	"mugo/internal/proto/connect"
	"mugo/internal/server/connectserver"
)

// 本文件是 P3 验收演示的自动化版（doc/14 §3 P3 验收 1/2/3/4/5）：
// GS 侧通过 api.GameServerStateObserver 接口把注册/注销/连接数变化推给 CS，
// CS 的服务器列表实时反映。全部走真实 TCP（CS 监听 + 客户端拨号）。

type listEntry struct {
	id   uint16
	load byte
}

// startCS 起一个注入端口的 CS，返回地址与实例。
func startCS(t *testing.T, season byte) (string, *connectserver.Server) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cs := connectserver.New(ln.Addr().String(), nil, nil)
	cs.SetSeason(season)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = cs.ServeListener(ctx, ln) }()
	return ln.Addr().String(), cs
}

// readFrame 从连接读一帧（CS 响应为单帧小包；C2 帧长 3 字节，补读至完整）。
func readFrame(t *testing.T, raw net.Conn) []byte {
	t.Helper()
	_ = raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame []byte
	buf := make([]byte, 4096)
	for {
		n, err := raw.Read(buf)
		if err != nil {
			t.Fatalf("读帧失败（已读 %d 字节）: %v", len(frame), err)
		}
		frame = append(frame, buf[:n]...)
		if len(frame) >= 2 {
			total := int(frame[1])
			if frame[0] == 0xC2 || frame[0] == 0xC4 {
				if len(frame) < 3 {
					continue
				}
				total = int(frame[1])<<8 | int(frame[2])
			}
			if len(frame) >= total {
				return frame[:total]
			}
		}
	}
}

// csFetchList 模拟客户端：拨号 → Hello → 请求列表 → 解析条目。
// modern 决定请求 sub（06/02）；**响应形态由 CS 的 Season 决定**（P3 行为），
// 请求形态与响应形态是两回事——这正是要验证的解耦。
func csFetchList(t *testing.T, addr string, modern bool) []listEntry {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()

	hello := readFrame(t, raw)
	if len(hello) != 4 || hello[2] != 0x00 || hello[3] != 0x01 {
		t.Fatalf("Hello 错误: %X", hello)
	}

	sub := byte(0x06)
	if !modern {
		sub = 0x02
	}
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xF4, sub}); err != nil {
		t.Fatal(err)
	}
	list := readFrame(t, raw)

	var out []listEntry
	switch {
	case modern && list[4] == 0x06:
		p := connect.AsServerListResponse(list)
		for i := 0; i < int(p.ServerCount()); i++ {
			b := p.Servers(i)
			out = append(out, listEntry{id: b.ServerId(), load: b.LoadPercentage()})
		}
	case !modern && list[4] == 0x02:
		p := connect.AsServerListResponseOld(list)
		for i := 0; i < int(p.ServerCount()); i++ {
			b := p.Servers(i)
			out = append(out, listEntry{id: uint16(b.ServerId()), load: b.LoadPercentage()})
		}
	default:
		t.Fatalf("列表形态不符: %X", list)
	}
	return out
}

// TestE2E_RegistrationReflectsInCSList 验收 1+2+3：
// 注册 → 列表出现（负载=连接数/上限）；连接数变化 → 负载变；多 GS 排序；注销 → 少一项；重启 → 恢复。
func TestE2E_RegistrationReflectsInCSList(t *testing.T) {
	addr, cs := startCS(t, 106)

	if got := csFetchList(t, addr, true); len(got) != 0 {
		t.Fatalf("注册前列表应为空: %+v", got)
	}

	// GS#0 注册（3/100 → 3%）
	cs.RegisterGameServer(api.NewServerInfo(0, "gs0", 3, 100), api.EndPoint{IpAddress: "127.0.0.1", Port: 55901})
	got := csFetchList(t, addr, true)
	if len(got) != 1 || got[0].id != 0 || got[0].load != 3 {
		t.Fatalf("注册后应 1 项 3%%: %+v", got)
	}

	// 连接数变化 3→42：负载 42%
	cs.CurrentConnectionsChanged(0, 42)
	got = csFetchList(t, addr, true)
	if got[0].load != 42 {
		t.Fatalf("变化后应 42%%: %+v", got)
	}

	// GS#1 注册 → 2 项（按 ServerId 排序）
	cs.RegisterGameServer(api.NewServerInfo(1, "gs1", 0, 100), api.EndPoint{IpAddress: "127.0.0.1", Port: 55902})
	got = csFetchList(t, addr, true)
	if len(got) != 2 || got[0].id != 0 || got[1].id != 1 {
		t.Fatalf("应 2 项按序: %+v", got)
	}

	// 停 GS#0 → 列表立刻少一项
	cs.UnregisterGameServer(0)
	got = csFetchList(t, addr, true)
	if len(got) != 1 || got[0].id != 1 {
		t.Fatalf("注销后应只剩 gs1: %+v", got)
	}

	// GS#0 重启 → 恢复
	cs.RegisterGameServer(api.NewServerInfo(0, "gs0", 0, 100), api.EndPoint{IpAddress: "127.0.0.1", Port: 55901})
	got = csFetchList(t, addr, true)
	if len(got) != 2 {
		t.Fatalf("重注册后应恢复 2 项: %+v", got)
	}
}

// TestE2E_OldSeasonUsesByteCount 验收 5：Season==0 的 CS 回 Old 形态
// （ServerCount 为 byte、条目 2 字节），同数据与现代形态不同。
func TestE2E_OldSeasonUsesByteCount(t *testing.T) {
	addr, cs := startCS(t, 0)
	cs.RegisterGameServer(api.NewServerInfo(0, "gs0", 1, 100), api.EndPoint{IpAddress: "127.0.0.1", Port: 55901})

	got := csFetchList(t, addr, false)
	if len(got) != 1 || got[0].id != 0 || got[0].load != 1 {
		t.Fatalf("Old 形态解析错误: %+v", got)
	}

	// 原始字节复核：C2 + len16 + F4 02 + count(byte) + 1×2B 条目 = 6+2 = 8
	raw := readFrame(t, mustDialAndRequest(t, addr, 0x02))
	if len(raw) != 8 || raw[0] != 0xC2 || raw[1] != 0x00 || raw[2] != 0x08 || raw[5] != 0x01 {
		t.Fatalf("Old 形态字节不符: %X", raw)
	}
	if !bytes.Equal(raw[6:8], []byte{0x00, 0x01}) {
		t.Fatalf("Old 条目不符: %X", raw[6:8])
	}
}

func mustDialAndRequest(t *testing.T, addr string, sub byte) net.Conn {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	readFrame(t, raw) // Hello
	if _, err := raw.Write([]byte{0xC1, 0x04, 0xF4, sub}); err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestE2E_MemorizingObserverLateCS 验收 4 的核心机制：
// "CS 晚于 GS 启动"——先有注册历史，新 CS 观察者加入后经 PullRegistrations 拿到全量，
// 且后续变化继续转发。
func TestE2E_MemorizingObserverLateCS(t *testing.T) {
	m := NewMulticastObserver()

	// GS 先注册（此时没有任何 CS 观察者）
	m.RegisterGameServer(api.NewServerInfo(0, "gs0", 9, 100), api.EndPoint{IpAddress: "127.0.0.1", Port: 55901})

	// 晚启动的 CS 加入
	addr, cs := startCS(t, 106)
	m.AddObserver(cs)
	m.PullRegistrations(cs)

	got := csFetchList(t, addr, true)
	if len(got) != 1 || got[0].id != 0 || got[0].load != 9 {
		t.Fatalf("晚启动 CS 应经记忆拿到 GS: %+v", got)
	}

	// 后续变化也继续到达晚加入的 CS
	m.CurrentConnectionsChanged(0, 50)
	got = csFetchList(t, addr, true)
	if got[0].load != 50 {
		t.Fatalf("晚加入后变化应继续转发: %+v", got)
	}

	// GS 停机：晚加入的 CS 同步摘除
	m.UnregisterGameServer(0)
	got = csFetchList(t, addr, true)
	if len(got) != 0 {
		t.Fatalf("停机后晚加入 CS 应摘除: %+v", got)
	}
}
