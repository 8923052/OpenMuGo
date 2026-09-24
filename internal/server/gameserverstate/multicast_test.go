package gameserverstate

import (
	"testing"

	"mugo/internal/api"
)

// recordingObserver 记录全部回调，供断言。
type recordingObserver struct {
	registered   []api.ServerInfo
	endpoints    []api.EndPoint
	unregistered []uint16
	loads        map[uint16]int
}

func newRecording() *recordingObserver {
	return &recordingObserver{loads: make(map[uint16]int)}
}

func (r *recordingObserver) RegisterGameServer(info api.ServerInfo, ep api.EndPoint) {
	r.registered = append(r.registered, info)
	r.endpoints = append(r.endpoints, ep)
}

func (r *recordingObserver) UnregisterGameServer(id uint16) {
	r.unregistered = append(r.unregistered, id)
}

func (r *recordingObserver) CurrentConnectionsChanged(id uint16, n int) {
	r.loads[id] = n
}

// TestMulticastFanout 验证 3 个方法都转发给每个观察者（原版 MulticastObserver 语义）。
func TestMulticastFanout(t *testing.T) {
	m := NewMulticastObserver()
	r1, r2 := newRecording(), newRecording()
	m.AddObserver(r1)
	m.AddObserver(r2)

	info := api.NewServerInfo(0, "gs0", 0, 100)
	ep := api.EndPoint{IpAddress: "127.0.0.1", Port: 55901}
	m.RegisterGameServer(info, ep)
	m.CurrentConnectionsChanged(0, 5)
	m.UnregisterGameServer(0)

	for i, r := range []*recordingObserver{r1, r2} {
		if len(r.registered) != 1 || r.registered[0].Id != 0 {
			t.Errorf("观察者%d 未收到注册", i)
		}
		if r.endpoints[0] != ep {
			t.Errorf("观察者%d 端点不符: %+v", i, r.endpoints[0])
		}
		if r.loads[0] != 5 {
			t.Errorf("观察者%d 连接数变化未收到: %v", i, r.loads)
		}
		if len(r.unregistered) != 1 || r.unregistered[0] != 0 {
			t.Errorf("观察者%d 未收到注销", i)
		}
	}
}

// TestPullRegistrations 验证记忆观察者：后加入的观察者经 PullRegistrations
// 拿到**既有 GS 注册**——这是"CS 晚于 GS 启动"场景（P4 顺序）的保障。
func TestPullRegistrations(t *testing.T) {
	m := NewMulticastObserver()
	ep0 := api.EndPoint{IpAddress: "127.0.0.1", Port: 55901}
	ep1 := api.EndPoint{IpAddress: "127.0.0.1", Port: 55902}
	m.RegisterGameServer(api.NewServerInfo(0, "gs0", 3, 100), ep0)
	m.RegisterGameServer(api.NewServerInfo(1, "gs1", 7, 100), ep1)
	// GS0 连接数变化：记忆条目应同步
	m.CurrentConnectionsChanged(0, 42)

	// 模拟后启动的 CS
	late := newRecording()
	m.AddObserver(late)
	m.PullRegistrations(late)

	if len(late.registered) != 2 {
		t.Fatalf("PullRegistrations 应重放 2 条注册，got %d", len(late.registered))
	}
	// 记忆里的连接数是最新值（42），不是注册时的旧值（3）。
	// （重放顺序不保证——记忆是 map；CS 侧列表按 ServerId 排序，故顺序无关紧要。）
	byID := make(map[uint16]api.ServerInfo, 2)
	endpoints := make(map[uint16]api.EndPoint, 2)
	for i, info := range late.registered {
		byID[info.Id] = info
		endpoints[info.Id] = late.endpoints[i]
	}
	if byID[0].CurrentConnections != 42 {
		t.Fatalf("GS0 记忆连接数=%d, want 42（应随 CurrentConnectionsChanged 更新）", byID[0].CurrentConnections)
	}
	if byID[1].CurrentConnections != 7 {
		t.Fatalf("GS1 记忆连接数=%d, want 7", byID[1].CurrentConnections)
	}
	if endpoints[0] != ep0 || endpoints[1] != ep1 {
		t.Fatalf("重放端点不符: %+v", late.endpoints)
	}
}

// TestMemorizingIgnoresDuplicateRegister 锁定 TryAdd 语义：
// 重复注册不覆盖记忆（原版 ConnectServer 侧先 Unregister 再 Register）。
func TestMemorizingIgnoresDuplicateRegister(t *testing.T) {
	m := NewMulticastObserver()
	ep := api.EndPoint{IpAddress: "127.0.0.1", Port: 55901}
	m.RegisterGameServer(api.NewServerInfo(0, "gs0", 1, 100), ep)
	m.RegisterGameServer(api.NewServerInfo(0, "gs0-renamed", 2, 100), ep) // 重复 ID

	sink := newRecording()
	m.PullRegistrations(sink)
	if len(sink.registered) != 1 || sink.registered[0].Description != "gs0" {
		t.Fatalf("重复注册不应覆盖记忆: %+v", sink.registered)
	}
}

// TestUnregisterRemovesMemory 注销后 PullRegistrations 不再重放该条目。
func TestUnregisterRemovesMemory(t *testing.T) {
	m := NewMulticastObserver()
	m.RegisterGameServer(api.NewServerInfo(0, "gs0", 0, 100), api.EndPoint{})
	m.UnregisterGameServer(0)

	sink := newRecording()
	m.PullRegistrations(sink)
	if len(sink.registered) != 0 {
		t.Fatalf("注销后不应重放: %+v", sink.registered)
	}
}
