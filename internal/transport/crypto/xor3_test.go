package crypto

import "testing"

func TestXor3LoginFields(t *testing.T) {
	x := NewXor3()
	// 自反：对同一字段 Apply 两次还原。
	orig := []byte("user")
	buf := append([]byte(nil), orig...)
	x.Apply(buf)
	if string(buf) == string(orig) {
		t.Fatal("Xor3 未改变字段")
	}
	x.Apply(buf)
	if string(buf) != string(orig) {
		t.Fatalf("Xor3 自反失败: %X", buf)
	}
}

func TestXor3DecryptString(t *testing.T) {
	x := NewXor3()
	// 模拟客户端：10 字节定长用户名，Xor3 后零填充。
	field := make([]byte, 10)
	copy(field, []byte("Alice"))
	x.Apply(field)
	if got := x.DecryptString(field); got != "Alice" {
		t.Fatalf("用户名解密=%q，期望 Alice", got)
	}
}
