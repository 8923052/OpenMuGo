package persistence

import (
	"testing"

	"mugo/internal/gamelogic/entity"
)

func TestMemoryStoreAuthenticate(t *testing.T) {
	st := NewMemoryStore()
	st.Add(&entity.Account{Name: "u", Password: "p"})
	if a, err := st.Authenticate("u", "p"); err != nil || a.Name != "u" {
		t.Fatal("正确凭据应通过")
	}
	if _, err := st.Authenticate("u", "x"); err != ErrInvalidCredentials {
		t.Fatalf("错误密码: %v", err)
	}
	if _, err := st.Authenticate("nope", "p"); err != ErrInvalidCredentials {
		t.Fatalf("未知用户: %v", err)
	}
}
