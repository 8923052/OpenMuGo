package container

import (
	"context"
	"testing"

	"mugo/internal/api"
)

// fakeServer 是记录调用序列的假服务（验证启动/停止顺序用）。
type fakeServer struct {
	id    int
	typ   api.ServerType
	log   *[]string
	state api.ServerState
}

func (f *fakeServer) Id() int                            { return f.id }
func (f *fakeServer) ConfigurationId() string            { return "fake" }
func (f *fakeServer) Description() string                { return "fake" }
func (f *fakeServer) Type() api.ServerType               { return f.typ }
func (f *fakeServer) ServerState() api.ServerState       { return f.state }
func (f *fakeServer) MaximumConnections() int            { return 0 }
func (f *fakeServer) CurrentConnections() int            { return 0 }
func (f *fakeServer) StartAsync(_ context.Context) error { f.mark("start"); return nil }
func (f *fakeServer) StartListeners(_ context.Context) error {
	f.mark("listeners")
	f.state = api.ServerStateStarted
	return nil
}
func (f *fakeServer) ShutdownAsync(_ context.Context) error { f.mark("shutdown"); return nil }

func (f *fakeServer) mark(op string) {
	*f.log = append(*f.log, f.name()+"."+op)
}

func (f *fakeServer) name() string {
	switch f.typ {
	case api.ServerTypeChatServer:
		return "chat"
	case api.ServerTypeGameServer:
		return "gs"
	case api.ServerTypeConnectServer:
		return "cs"
	}
	return "srv"
}

func mustAdd(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
}

// TestStartOrderIsThreePhases 验收：启动顺序严格为 Chat → Game → Connect
// （三段 for，阶段内按注册顺序），且 Start 与 StartListeners 两段分离。
func TestStartOrderIsThreePhases(t *testing.T) {
	var log []string
	c := New(nil)

	// 故意按逆序注册：CS → GS → Chat，验证容器按阶段重排。
	cs := &fakeServer{id: 1, typ: api.ServerTypeConnectServer, log: &log}
	gs := &fakeServer{id: 2, typ: api.ServerTypeGameServer, log: &log}
	chat := &fakeServer{id: 3, typ: api.ServerTypeChatServer, log: &log}
	mustAdd(t, c.Add(cs))
	mustAdd(t, c.Add(gs))
	mustAdd(t, c.Add(chat))

	ctx := context.Background()
	if err := c.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.StartAllListeners(ctx); err != nil {
		t.Fatal(err)
	}

	// 容器级时序与原版一致：全部 Start（三阶段）→ 全部 StartListeners（三阶段）。
	want := []string{
		"chat.start", "gs.start", "cs.start",
		"chat.listeners", "gs.listeners", "cs.listeners",
	}
	if len(log) != len(want) {
		t.Fatalf("调用序列长度 %d 不等于 %d: %v", len(log), len(want), log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("顺序错误 @%d: got %v want %v", i, log, want)
		}
	}
}

// TestStopAllIsReverse 验收：Ctrl-C 停止时逆阶段（Connect → Game → Chat）。
func TestStopAllIsReverse(t *testing.T) {
	var log []string
	c := New(nil)
	mustAdd(t, c.Add(&fakeServer{id: 1, typ: api.ServerTypeConnectServer, log: &log}))
	mustAdd(t, c.Add(&fakeServer{id: 2, typ: api.ServerTypeGameServer, log: &log}))
	mustAdd(t, c.Add(&fakeServer{id: 3, typ: api.ServerTypeChatServer, log: &log}))

	ctx := context.Background()
	if err := c.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.StartAllListeners(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.StopAll(ctx); err != nil {
		t.Fatal(err)
	}

	// 停止事件序列应为 cs.shutdown → gs.shutdown → chat.shutdown
	var stops []string
	for _, e := range log {
		if len(e) > 9 && e[len(e)-9:] == ".shutdown" {
			stops = append(stops, e)
		}
	}
	want := []string{"cs.shutdown", "gs.shutdown", "chat.shutdown"}
	if len(stops) != 3 {
		t.Fatalf("停止事件数 %d 不等于 3: %v", len(stops), stops)
	}
	for i := range want {
		if stops[i] != want[i] {
			t.Fatalf("停止顺序错误 @%d: %v", i, stops)
		}
	}
}

// TestRestartAll 验收：RestartAll = Stop → Start → StartListeners（原版 RestartAllAsync）。
func TestRestartAll(t *testing.T) {
	var log []string
	c := New(nil)
	mustAdd(t, c.Add(&fakeServer{id: 1, typ: api.ServerTypeGameServer, log: &log}))

	if err := c.RestartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// RestartAll = StopAll → StartAll → StartAllListeners（服务此前未启动，故先 shutdown）。
	want := []string{"gs.shutdown", "gs.start", "gs.listeners"}
	if len(log) != len(want) {
		t.Fatalf("调用序列 %v 不等于 %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("重启顺序错误 @%d: %v", i, log)
		}
	}
}

// TestDuplicateAddRejected 同 id 重复注册报错。
func TestDuplicateAddRejected(t *testing.T) {
	c := New(nil)
	if err := c.Add(&fakeServer{id: 7, typ: api.ServerTypeGameServer}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(&fakeServer{id: 7, typ: api.ServerTypeGameServer}); err == nil {
		t.Fatal("重复注册应报错")
	}
}

// TestRemove 移除后不再参与启动。
func TestRemove(t *testing.T) {
	var log []string
	c := New(nil)
	gs := &fakeServer{id: 2, typ: api.ServerTypeGameServer, log: &log}
	mustAdd(t, c.Add(gs))
	if !c.Remove(2) {
		t.Fatal("Remove 应返回 true")
	}
	if err := c.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Fatalf("移除后不应有启动调用: %v", log)
	}
}
