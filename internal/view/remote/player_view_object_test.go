// player_view_object_test.go —— 世界对象相关出站封包的逐字节锚定。
//
// 重点是 0x18 ObjectAnimation：客户端 WSclient.cpp 顶层派发表
//
//	case 0x18: ReceiveAction(ReceiveBuffer, Size)
//
// 且只有这一条路径会让客户端播放"怪物挥击"动画——伤害数字（0x11）与击杀（0x17）
// 都不会触发动作，所以缺少 0x18 时近战怪看起来就是"隔空掉血/像远程"。
//
// 客户端结构（WSclient.h）：
//
//	typedef struct {
//	    PBMSG_HEADER Header;   // {Code, Size, HeadCode} = 3B
//	    BYTE KeyH, KeyL;       // 动画发起者：Key = KeyH<<8 | KeyL  → 线上大端
//	    BYTE Angle;            // 方向包字节
//	    BYTE Action;           // 动作号，AT_ATTACK1 = 120
//	    BYTE TargetKeyH, TargetKeyL;
//	} PRECEIVE_ACTION;         // 合计 9B
//
// 注意客户端把它当**角度**用：`c->Object.Angle[2] = ((float)Angle - 1.f) * 45.f;`
// 即线上是 1 基的 45° 步进。OpenMU 的 DirectionExtensions.ToPacketByte 为
// `(byte)(direction - 1)`，方向增量表 CalculateTargetPoint 与本项目
// action.DirectionDeltas / npcMoveNibble 三方逐项一致，故此处只需锚定
// "字段落在哪一字节"，方向取值由 npc_ai 侧的对拍保证。
package remote

import (
	"bytes"
	"testing"

	"mugo/internal/gamelogic/action"
)

// recordingSender 收集出站帧（view 层测试用的最小 PacketSender）。
type recordingSender struct{ frames [][]byte }

func (r *recordingSender) Send(b []byte) error {
	r.frames = append(r.frames, append([]byte(nil), b...))
	return nil
}

func TestShowObjectAnimationFrame(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)

	// 怪物 0x1234 朝方向 3 攻击玩家 0x200（自己视角 ID）。
	if err := view.ShowObjectAnimation(0x1234, 3, action.AnimationAttack1, 0x200); err != nil {
		t.Fatal(err)
	}
	if len(rec.frames) != 1 {
		t.Fatalf("应下发 1 帧, got %d", len(rec.frames))
	}
	want := []byte{
		0xC1, 0x09, 0x18, // 头 + code
		0x12, 0x34, // ObjectId = 0x1234（大端）
		0x03,       // Angle/Direction
		0x78,       // Action = 120 (AT_ATTACK1)
		0x02, 0x00, // TargetId = 0x200（大端）
	}
	got := rec.frames[0]
	if !bytes.Equal(got, want) {
		t.Fatalf("0x18 帧不一致\n got: %X\nwant: %X", got, want)
	}
	if action.AnimationAttack1 != 120 {
		t.Fatalf("AT_ATTACK1 应为 120, got %d", action.AnimationAttack1)
	}
}

// TestShowObjectAnimationSelfTarget 校验按接收者视角解析的自身 ID：
// 发给"自己"时用 ConstantPlayerID（0x200），而非角色真实 ID——
// 这与 OpenMU GetId(playerOfView) 的语义一致（见 ShowAnimationPlugIn）。
func TestShowObjectAnimationSelfTarget(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	if err := view.ShowObjectAnimation(0x7777, 0, action.AnimationAttack1, 0x200); err != nil {
		t.Fatal(err)
	}
	got := rec.frames[0]
	if got[3] != 0x77 || got[4] != 0x77 {
		t.Fatalf("ObjectId 字节序异常: %X", got)
	}
	if got[7] != 0x02 || got[8] != 0x00 {
		t.Fatalf("TargetId 应为 0x200, got %X", got[7:9])
	}
	if got[5] != 0 {
		t.Fatalf("Angle=0 应可表达（客户端算成 -45°=315°）, got %d", got[5])
	}
}
