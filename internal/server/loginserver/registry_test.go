package loginserver

import (
	"testing"

	"mugo/internal/gamelogic/entity"
)

func TestSessionRegistry(t *testing.T) {
	r := NewSessionRegistry()
	normal := &entity.Account{Name: "u"}
	tmpl := &entity.Account{Name: "_1", IsTemplate: true}

	if err := r.TryLogin(normal); err != nil {
		t.Fatal(err)
	}
	if err := r.TryLogin(normal); err != ErrAlreadyConnected {
		t.Fatalf("重复登录应拒绝，得到 %v", err)
	}
	// 模板账号不占用名额。
	if err := r.TryLogin(tmpl); err != nil {
		t.Fatalf("模板账号不应受会话限制: %v", err)
	}
	r.Logoff(tmpl)
	if err := r.TryLogin(tmpl); err != nil {
		t.Fatalf("模板账号 Logoff 后再登录仍应放行: %v", err)
	}
	r.Logoff(normal)
	if err := r.TryLogin(normal); err != nil {
		t.Fatalf("Logoff 后应可再次登录: %v", err)
	}
}
