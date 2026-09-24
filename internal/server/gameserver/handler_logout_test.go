package gameserver

// handler_logout_test.go —— C3 F1 02 登出的端到端行为（真实 TCP 连接）。
//
// 真机问题：点“退出游戏”时会闪一下重连界面。根因在服务端**断开时机**，不在协议：
//   - 客户端点退出后要到下一帧的事件泵才置 Destroy（GameOverBtnDown：SendLogOut 后
//     紧跟 PostMessage(WM_CLOSE)），而同一帧末 SceneManager::CheckServerConnection
//     仍会执行；此时若连接已被服务端断开（SocketClient->IsConnected() 因收到 FIN 变
//     false），且场景仍是 MAIN_SCENE、ReconnectManager 有会话，就会启动自动重连
//     → ReconnectDialog 闪一帧。
//   - 原版 OpenMU 的登出路径含持久化保存（RemoveFromGameAsync → SaveProgressAsync），
//     响应与 FIN 实际落在客户端退出之后，因此不触发；本仓是内存态、同一帧内即可完成，
//     反而“抢跑”。
//
// 因此服务端只发响应、不主动断开，由客户端自行关闭连接。本测试锁定该行为。

import (
	"errors"
	"net"
	"testing"
	"time"
)

// TestLogoutCloseGameSendsResponseWithoutClosing 验证 CloseGame 登出：
// 回 C3 F1 02 类型 0（客户端 ReceiveLogOut case 0 → 退出进程），
// 且服务端**不**主动关闭/半关连接（否则客户端会在退出前闪一帧重连界面）。
func TestLogoutCloseGameSendsResponseWithoutClosing(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)

	cl.sendLogin(t, "user", "pass", []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); len(resp) < 5 || resp[4] != 1 {
		t.Fatalf("前置登录失败: %X", resp)
	}

	// 对照客户端 SendLogOut(LogOutType::CloseGame)：C3 F1 02 + type=0。
	cl.sendPacket([]byte{0xC3, 0x05, 0xF1, 0x02, 0x00})

	resp := cl.recvPlain(t, time.Second)
	if len(resp) != 5 || resp[0] != 0xC3 || resp[2] != 0xF1 || resp[3] != 0x02 || resp[4] != 0 {
		t.Fatalf("LogoutResponse 错误: %X", resp)
	}

	// 服务端不得主动断开：随后的读应超时，而不是立刻 EOF（兜底关闭在 1s 之后）。
	_ = cl.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, err := cl.rd.ReadPacket()
	if err == nil {
		t.Fatal("登出后不应再收到数据帧")
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("登出后连接不应被服务端关闭（应读超时），got %v", err)
	}
}

// TestLogoutByCheatDetectionClosesGame 验证 C3 F1 03（客户端外挂检测上报）：
// 对照 LogOutByCheatDetectionHandlerPlugIn —— 走完整登出清理并按 **CloseGame** 回响应
// （不给回选角的机会），响应子码是 02 而不是请求的 03。
func TestLogoutByCheatDetectionClosesGame(t *testing.T) {
	_, addr := testServer(t, accounts())
	cl := dialLoginClient(t, addr)

	cl.sendLogin(t, "user", "pass", []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); len(resp) < 5 || resp[4] != 1 {
		t.Fatalf("前置登录失败: %X", resp)
	}

	cl.sendPacket([]byte{0xC3, 0x06, 0xF1, 0x03, 0x00, 0x00})

	resp := cl.recvPlain(t, time.Second)
	if len(resp) != 5 || resp[0] != 0xC3 || resp[2] != 0xF1 || resp[3] != 0x02 || resp[4] != 0 {
		t.Fatalf("应按 CloseGame 回 F1 02 类型 0，got %X", resp)
	}
}
