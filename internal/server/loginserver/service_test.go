package loginserver

import (
	"testing"

	"mugo/internal/api"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/persistence"
)

func TestLoginServiceOutcomes(t *testing.T) {
	st := persistence.NewMemoryStore()
	st.Add(&entity.Account{Name: "ok", Password: "p"})
	st.Add(&entity.Account{Name: "ban", Password: "p", State: entity.AccountStateBanned})
	st.Add(&entity.Account{Name: "tmp", Password: "p", State: entity.AccountStateTemporarilyBanned})
	svc := NewLoginService(st, NewSessionRegistry())

	if _, _, ok := svc.Authenticate("ok", "bad"); ok {
		t.Fatal("错误密码应失败")
	}
	o, info, ok := svc.Authenticate("ok", "p")
	if !ok || o != api.LoginOK || info.Name != "ok" {
		t.Fatalf("正常登录不符: o=%v info=%v ok=%v", o, info, ok)
	}
	if o, _, ok = svc.Authenticate("ok", "p"); ok || o != api.LoginAccountAlreadyConnected {
		t.Fatalf("重复登录应被拒: o=%v ok=%v", o, ok)
	}
	if o, _, ok = svc.Authenticate("ban", "p"); ok || o != api.LoginAccountBlocked {
		t.Fatalf("封禁应被拒: o=%v ok=%v", o, ok)
	}
	if o, _, ok = svc.Authenticate("tmp", "p"); ok || o != api.LoginTemporaryBlocked {
		t.Fatalf("临时封禁应被拒: o=%v ok=%v", o, ok)
	}
}

// TestLoginServiceImplementsContract 锁定契约层实现：LoginServer 三个方法的行为。
func TestLoginServiceImplementsContract(t *testing.T) {
	svc := NewLoginService(persistence.NewMemoryStore(), NewSessionRegistry())
	var _ api.LoginServer = svc

	if ok, err := svc.TryLoginAsync("alice", 0); err != nil || !ok {
		t.Fatalf("首次占用应成功: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.TryLoginAsync("alice", 0); err != nil || ok {
		t.Fatalf("重复占用应失败: ok=%v err=%v", ok, err)
	}
	snap, err := svc.GetSnapshotAsync()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := snap["alice"]; !found || len(snap) != 1 {
		t.Fatalf("快照应只含 alice: %v", snap)
	}
	if err := svc.LogOffAsync("alice", 0); err != nil {
		t.Fatal(err)
	}
	if snap, _ = svc.GetSnapshotAsync(); len(snap) != 0 {
		t.Fatalf("登出后快照应为空: %v", snap)
	}
}
