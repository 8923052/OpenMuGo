package gameserver

import (
	"testing"
	"time"
)

// createCharacterPacket 构造 C1 F3 01（15B）：name@4..13，class@14。
func createCharacterPacket(name string, class byte) []byte {
	p := make([]byte, 15)
	p[0] = 0xC1
	p[1] = 15
	p[2] = 0xF3
	p[3] = 0x01
	copy(p[4:14], name)
	p[14] = class
	return p
}

// deleteCharacterPacket 构造 C1 F3 02（24B）：name@4..13，securityCode@14..23。
func deleteCharacterPacket(name, securityCode string) []byte {
	p := make([]byte, 24)
	p[0] = 0xC1
	p[1] = 24
	p[2] = 0xF3
	p[3] = 0x02
	copy(p[4:14], name)
	copy(p[14:24], securityCode)
	return p
}

// loginTestClient 登录 user/pass 并返回客户端（账号初始有 1 个角色 Bob）。
func loginTestClient(t *testing.T) *loginClient {
	t.Helper()
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, "user", "pass", []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 1 {
		t.Fatalf("登录失败: %X", resp)
	}
	return cl
}

// characterCount 请求角色列表并返回角色数。
func characterCount(t *testing.T, cl *loginClient) int {
	t.Helper()
	cl.sendCharacterListRequest()
	list := cl.recvPlain(t, time.Second)
	if list[2] != 0xF3 || list[3] != 0x00 {
		t.Fatalf("角色列表帧错误: %X", list)
	}
	return int(list[6])
}

// TestCreateCharacterSuccess 验证创角成功：42B 响应字段正确，账号角色数 +1。
func TestCreateCharacterSuccess(t *testing.T) {
	cl := loginTestClient(t)
	if n := characterCount(t, cl); n != 1 {
		t.Fatalf("初始角色数=%d，期望 1", n)
	}

	// DarkWizard=0，基础职业可创建。
	cl.sendPacket(createCharacterPacket("Newbie", 0))
	resp := cl.recvPlain(t, time.Second)
	if len(resp) != 42 || resp[2] != 0xF3 || resp[3] != 0x01 {
		t.Fatalf("创角成功帧头错误: %X", resp)
	}
	if resp[4]&1 == 0 {
		t.Fatalf("Success 位未置: %X", resp)
	}
	if got := string(resp[5:11]); got != "Newbie" {
		t.Fatalf("响应角色名=%q", got)
	}
	if resp[15] != 1 { // 空槽：Bob 在 slot0，新角色 slot1
		t.Fatalf("角色槽=%d，期望 1", resp[15])
	}
	if resp[16] != 1 || resp[17] != 0 { // Level=1 LE
		t.Fatalf("等级字段错误: %X", resp[16:18])
	}
	if resp[18] != 0 { // Class=DarkWizard
		t.Fatalf("职业字段=%d", resp[18])
	}
	for _, b := range resp[20:42] { // PreviewData 全 0xFF
		if b != 0xFF {
			t.Fatalf("PreviewData 未填 0xFF: %X", resp[20:42])
		}
	}

	if n := characterCount(t, cl); n != 2 {
		t.Fatalf("创角后角色数=%d，期望 2", n)
	}
}

// TestCreateCharacterFailures 验证两类失败：不可创建的转职职业、同账号重名。
func TestCreateCharacterFailures(t *testing.T) {
	cl := loginTestClient(t)

	// BladeKnight=6：CanGetCreated=false → 5B 失败帧。
	cl.sendPacket(createCharacterPacket("Trans", 6))
	if resp := cl.recvPlain(t, time.Second); len(resp) != 5 || resp[3] != 0x01 {
		t.Fatalf("不可创建职业的失败帧错误: %X", resp)
	}

	// 重名 Bob → 失败。
	cl.sendPacket(createCharacterPacket("Bob", 0))
	if resp := cl.recvPlain(t, time.Second); len(resp) != 5 {
		t.Fatalf("重名失败帧错误: %X", resp)
	}

	if n := characterCount(t, cl); n != 1 {
		t.Fatalf("失败后角色数=%d，期望 1", n)
	}
}

// TestCreateCharacterRequiresLogin 验证未登录创角无响应。
func TestCreateCharacterRequiresLogin(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)
	cl.sendPacket(createCharacterPacket("Lonely", 0))
	_ = cl.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := cl.rd.ReadPacket(); err == nil {
		t.Fatal("未登录创角不应得到响应")
	}
}

// TestDeleteCharacter 验证删角：错误安全码 → WrongSecurityCode(2)；
// 正确密码 → Successful(1) 且账号角色数 -1；删不存在角色 → Unsuccessful(0)。
func TestDeleteCharacter(t *testing.T) {
	cl := loginTestClient(t)

	// 错误安全码。
	cl.sendPacket(deleteCharacterPacket("Bob", "wrong"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 2 {
		t.Fatalf("结果=%d，期望 WrongSecurityCode(2)", resp[4])
	}
	if n := characterCount(t, cl); n != 1 {
		t.Fatalf("拒绝后角色数=%d，期望 1", n)
	}

	// 不存在的角色。
	cl.sendPacket(deleteCharacterPacket("Ghost", "pass"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 0 {
		t.Fatalf("结果=%d，期望 Unsuccessful(0)", resp[4])
	}

	// 正确密码（checkAsPassword 语义）。
	cl.sendPacket(deleteCharacterPacket("Bob", "pass"))
	if resp := cl.recvPlain(t, time.Second); resp[4] != 1 {
		t.Fatalf("结果=%d，期望 Successful(1)", resp[4])
	}
	if n := characterCount(t, cl); n != 0 {
		t.Fatalf("删除后角色数=%d，期望 0", n)
	}
}
