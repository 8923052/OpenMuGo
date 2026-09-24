package gameserver

// handler_move_test.go —— 行走×攻击交互回归（真机事故："攻击动作一顿一顿，一开始就停住"）。
//
// 事故链条（逐层对照原版）：
//
//  1. **客户端**（MuMain `ZzzInterface.cpp` `Action()` 的 MOVEMENT_ATTACK 分支）：
//     每一下普通挥击依次执行 `SetPlayerAttack(c)`（起挥击动画）→ 转向 →
//     `LetHeroStop()` → `SendHitRequest(...)`。`LetHeroStop` 内部的
//     `SendCharacterMove(Hero->Key, ..., PathNum = 1, ...)` 会发一个 **0 步的 D4 包**
//     （只有 TargetRotation，没有方向字节；源码注释原文 "it's 1 when the character
//     stops walking by starting a skill"）。即：**每一刀都会先来一个 0 步方向包**。
//
//  2. **原版服务端**（OpenMU C#）：`CharacterWalkBaseHandlerPlugIn.WalkAsync` 只在
//     `request.Header.Length > 6` 时走 `WalkToAsync`，否则
//     `player.Rotation = request.TargetRotation.ParseAsDirection()`；
//     `PlayerMovement.WalkToAsync` 开头还有 `if (steps.IsEmpty) { return; }`。
//     —— 6 字节帧**永远不产生"位置回拉"**。
//
//  3. **本仓早期 handleWalk** 没做这个区分：0 步包被当成"首步阻挡" →
//     `resync()` → 下发 C1 15。客户端 `ReceiveMovePosition`（WSclient.cpp）收尾调
//     `SetPlayerStop(c)`（ZzzCharacter.cpp → `SetAction(PLAYER_STOP…)`），
//     把刚起手的挥击动画切成站立动作。
//     —— 真机表现：**伤害照出，但攻击动作一开始就停住、一顿一顿**。
//
// 本文件锁定修复后的契约：0 步方向包**只更新朝向、零出站帧**；正常行走不受影响。

import (
	"io"
	"log"
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/world"
	"mugo/internal/proto/c2s"
)

// newMoveTestSrv 复用 scope 脚手架，但把 debug 日志丢弃以保持测试输出干净。
func newMoveTestSrv(t *testing.T) *Server {
	t.Helper()
	srv := newScopeTestSrv(t)
	srv.deps.logger = log.New(io.Discard, "", 0)
	return srv
}

// newMoveTestSession 构造一个已进图、已入 AoI 的会话（视图写入 rec）。
func newMoveTestSession(t *testing.T, srv *Server, id uint16, name string, x, y byte, rec *packetRecorder) (*session, *world.Player) {
	t.Helper()
	sess, wp := newScopedSession(id, name, x, y, rec)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	return sess, wp
}

// zeroStepFrame 构造客户端的"0 步方向包"：C1 06 D4 <x> <y> <rot<<4 | 0>。
// 与真机日志中的 `frame=C106D4B2…20` 同构（0x20 → 高 nibble=朝向 2 / 低 nibble=0 步）。
func zeroStepFrame(x, y, rot byte) []byte {
	req := c2s.NewWalkRequest(c2s.WalkRequestRequiredSize(0))
	req.SetSourceX(x)
	req.SetSourceY(y)
	req.SetStepCount(0)
	req.SetTargetRotation(rot & 0x0F)
	return req.Bytes()
}

// TestIsNonMovingWalk 锁定判据本身（两个原版锚点：帧长 `> 6` + 空步点）。
func TestIsNonMovingWalk(t *testing.T) {
	cases := []struct {
		stepCount byte
		frameLen  int
		want      bool
	}{
		{0, 6, true},  // 真机日志 C1 06 D4 … 20：0 步方向包
		{0, 8, true},  // 自称 0 步（帧再长也只是方向包）
		{4, 6, true},  // 帧长 6 → 物理上不可能带方向字节，按原版 `> 6` 归为方向包
		{1, 7, false}, // 正常单步
		{2, 7, false},
		{15, 14, false}, // 满步
	}
	for _, c := range cases {
		if got := action.IsNonMovingWalk(c.stepCount, c.frameLen); got != c.want {
			t.Errorf("IsNonMovingWalk(%d,%d)=%v, want %v", c.stepCount, c.frameLen, got, c.want)
		}
	}
	// 常量必须与原版判据 `Header.Length > 6` 对齐（C1 头 2B + code 1B + src 2B + nibble 1B）。
	if action.WalkRequestHeaderSize != 6 {
		t.Fatalf("WalkRequestHeaderSize=%d, want 6", action.WalkRequestHeaderSize)
	}
}

// TestWalkZeroStepPacketOnlyRotates 是本次事故的核心回归：
// 0 步方向包必须只改朝向，**不许下发 C1 15**（否则客户端 SetPlayerStop 掐断攻击动画）。
func TestWalkZeroStepPacketOnlyRotates(t *testing.T) {
	srv := newMoveTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newMoveTestSession(t, srv, 11, "hero", 30, 30, rec)

	srv.handleWalk(sess, zeroStepFrame(30, 30, 5))

	if n := countFrames(rec, 0xC1, 0x15); n != 0 {
		t.Fatalf("0 步方向包不得下发 C1 15 回拉（会触发客户端 SetPlayerStop 掐断攻击动画），got %d 帧", n)
	}
	if n := len(rec.frames); n != 0 {
		t.Fatalf("0 步方向包不应产生任何出站帧，got %d: %X", n, rec.frames)
	}
	if wp.Rotation != 5 {
		t.Fatalf("0 步方向包应把朝向更新为 5，got %d", wp.Rotation)
	}
	if wp.X != 30 || wp.Y != 30 {
		t.Fatalf("0 步方向包不得改坐标，got (%d,%d)", wp.X, wp.Y)
	}
}

// TestWalkZeroStepRepeatedDuringAttack 模拟连续挥击节奏：客户端每一刀都发 0 步方向包
// （朝向随目标微调）。连续 8 次必须始终零出站帧，且攻击结束后的正常行走仍然生效。
func TestWalkZeroStepRepeatedDuringAttack(t *testing.T) {
	srv := newMoveTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newMoveTestSession(t, srv, 12, "hero", 30, 30, rec)

	for i := 0; i < 8; i++ {
		srv.handleWalk(sess, zeroStepFrame(30, 30, byte(i%8)))
		if n := len(rec.frames); n != 0 {
			t.Fatalf("第 %d 刀的方向包产生了 %d 帧出站（应恒为 0）: %X", i+1, n, rec.frames)
		}
	}
	if wp.Rotation != 7 {
		t.Fatalf("末次挥击后朝向应为 7，got %d", wp.Rotation)
	}
	if wp.X != 30 || wp.Y != 30 {
		t.Fatalf("连续方向包不得位移，got (%d,%d)", wp.X, wp.Y)
	}

	// 攻击结束后的正常行走不能被新分支吞掉：nibble 3 = SouthEast(+1,0)，2 步 → (32,30)。
	srv.handleWalk(sess, walkFrame(30, 30, 3, 2))
	if wp.X != 32 || wp.Y != 30 {
		t.Fatalf("正常行走后=(%d,%d)，want (32,30)", wp.X, wp.Y)
	}
	if n := countFrames(rec, 0xC1, 0xD4); n == 0 {
		t.Fatalf("正常行走应下发 C1 D4 行走确认")
	}
}

// TestWalkShortFrameIgnored 短帧（< 6B）不得越界读 d[5]，也不得产生任何出站帧。
// AsWalkRequest 不做长度校验，全靠 handler 的帧长防御。
func TestWalkShortFrameIgnored(t *testing.T) {
	srv := newMoveTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newMoveTestSession(t, srv, 13, "hero", 30, 30, rec)

	for _, frame := range [][]byte{
		{0xC1, 0x02, 0xD4},
		{0xC1, 0x04, 0xD4, 0x1E},
		{0xC1, 0x05, 0xD4, 0x1E, 0x1E},
	} {
		srv.handleWalk(sess, frame)
	}
	if n := len(rec.frames); n != 0 {
		t.Fatalf("短帧不应产生出站帧，got %d: %X", n, rec.frames)
	}
	if wp.Rotation != 0 || wp.X != 30 || wp.Y != 30 {
		t.Fatalf("短帧不得改朝向/坐标，got rot=%d (%d,%d)", wp.Rotation, wp.X, wp.Y)
	}
}
