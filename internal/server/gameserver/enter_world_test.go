package gameserver

import (
	"bytes"
	"testing"
	"time"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	"mugo/internal/persistence/seedtest"
	s2c "mugo/internal/proto/s2c"
)

// 模拟 MuMain 完整时序：F1 00 → 登录 → 角色列表 → F3 03(92B) → F3 12 → C2 12 进图 → D4 行走，
// 以及双客户端互见与广播（无真机，回环）。

// seedTestServer 启动装入 OpenMU 测试账号种子的 GS。
func seedTestServer(t *testing.T) (string, func()) {
	t.Helper()
	st := persistence.NewMemoryStore()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range seedtest.Accounts(cfg) {
		st.Add(a)
	}
	_, addr := testServer(t, st)
	return addr, func() {}
}

func enterClient(t *testing.T, addr, user string) *loginClient {
	t.Helper()
	cl := dialLoginClient(t, addr)
	cl.sendLogin(t, user, user, []byte("20404"))
	if resp := cl.recvPlain(t, time.Second); len(resp) != 5 || resp[4] != 1 {
		t.Fatalf("%s 登录失败: %X", user, resp)
	}
	return cl
}

func (cl *loginClient) sendSelectCharacter(t *testing.T, name string) {
	t.Helper()
	p := make([]byte, 14)
	p[0] = 0xC1
	p[1] = 14
	p[2] = 0xF3
	p[3] = 0x03
	copy(p[4:14], name)
	cl.sendPacket(p)
}

func (cl *loginClient) sendClientReady(t *testing.T) {
	t.Helper()
	cl.sendPacket([]byte{0xC1, 0x04, 0xF3, 0x12})
}

// sendWalk 发送 C1 D4：sx/sy 起点，rot 朝向，dirs 为 nibble 方向串。
func (cl *loginClient) sendWalk(t *testing.T, sx, sy, rot byte, dirs ...byte) {
	t.Helper()
	steps := byte(len(dirs))
	p := make([]byte, 6+(int(steps)+1)/2)
	p[0] = 0xC1
	p[1] = byte(len(p))
	p[2] = 0xD4
	p[3] = sx
	p[4] = sy
	p[5] = rot<<4 | steps
	for i, d := range dirs {
		if i%2 == 0 {
			p[6+i/2] = d << 4
		} else {
			p[6+i/2] |= d
		}
	}
	cl.sendPacket(p)
}

// recvStatsOnEnter 收进图时的两帧属性包并校验（原版属性初始化触发
// UpdateStatsExtendedPlugIn → C1 26 FE 最大值 20B + C1 26 FF 当前值 24B）。
//
// 实测坑：不下发这两帧，死亡重生后客户端的血/蓝/AG/SD 会一直停在死亡时的 0
// （服务端已回满但没告诉客户端）。回归锁定在这里。
func (cl *loginClient) recvStatsOnEnter(t *testing.T) (maximum, current []byte) {
	t.Helper()
	maximum = cl.recvPlain(t, time.Second)
	if len(maximum) != 20 || maximum[0] != 0xC1 || maximum[2] != 0x26 || maximum[3] != 0xFE {
		t.Fatalf("C1 26 FE MaximumStatsExtended 错误 len=%d: %X", len(maximum), maximum)
	}
	current = cl.recvPlain(t, time.Second)
	if len(current) != 24 || current[0] != 0xC1 || current[2] != 0x26 || current[3] != 0xFF {
		t.Fatalf("C1 26 FF CurrentStatsExtended 错误 len=%d: %X", len(current), current)
	}
	return maximum, current
}

// statsU32 读小端 u32（客户端 ReceiveStatsExtended 按 DWORD 小端解析）。
func statsU32(b []byte, off int) uint32 {
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

// recvMaybe 在超时内收一帧；超时返回 ok=false（用于"不应有响应"断言）。
func (cl *loginClient) recvMaybe(t *testing.T, timeout time.Duration) (frame []byte, ok bool) {
	t.Helper()
	_ = cl.conn.SetReadDeadline(time.Now().Add(timeout))
	frame, err := cl.rd.ReadPacket()
	if err != nil {
		return nil, false
	}
	if len(frame) > 0 && frame[0] >= 0xC3 {
		dec, derr := cl.smOpen.Open(frame)
		if derr != nil {
			t.Fatalf("解密失败: %v", derr)
		}
		frame = dec
	}
	return frame, true
}

// test1 的 DarkKnight 种子坐标（seedtest 确定性散列：x=133+7=140，y=118+13=131）。
const (
	// 种子坐标：确定性散列（seedtest.buildCharacter 同款公式推导）。
	test1DkX   byte = 149
	test1DkY   byte = 120
	test1DkMap      = uint16(0)
)

// isSelfEnterResidual 报告进图时"只发给自己的残余帧"，视野广播用例据此跳过：
//   - C4 F3 10 背包清单；
//   - C1 F3 11 技能列表；
//   - C1 F6 1A 进行中任务清单（对照原版 Player.cs:1753 —— 进图必发，空清单也发）。
func isSelfEnterResidual(f []byte) bool {
	switch {
	case f[0] == 0xC4:
		return true
	case len(f) > 3 && f[0] == 0xC1 && f[2] == 0xF3 && f[3] == 0x11:
		return true
	case len(f) > 3 && f[0] == 0xC1 && f[2] == 0xF6 && f[3] == 0x1A:
		return true
	case len(f) > 2 && f[0] == 0xC1 && f[2] == 0xA0:
		return true // C1 A0 组 0 任务状态表（原版 Player.cs:1752 进图必发）
	}
	return false
}

func TestEnterWorldAndWalk(t *testing.T) {
	addr, _ := seedTestServer(t)
	cl := enterClient(t, addr, "test1")

	// 角色列表含 test1Dk
	cl.sendCharacterListRequest()
	list := cl.recvPlain(t, time.Second)
	if !bytes.Contains(list, []byte("test1Dk")) {
		t.Fatalf("角色列表缺少 test1Dk: %X", list)
	}

	// 选角 → C3 F3 03 CharacterInformationExtended 92B
	cl.sendSelectCharacter(t, "test1Dk")
	info := cl.recvPlain(t, time.Second)
	if len(info) != 92 || info[0] != 0xC3 || info[2] != 0xF3 || info[3] != 0x03 {
		t.Fatalf("CharacterInformationExtended 错误: %X", info)
	}
	if info[4] != test1DkX || info[5] != test1DkY {
		t.Fatalf("出生坐标错误: (%d,%d) want (%d,%d)", info[4], info[5], test1DkX, test1DkY)
	}
	if uint16(info[6])|uint16(info[7])<<8 != test1DkMap {
		t.Fatalf("地图号错误: %v", info[6:8])
	}
	if money := uint32(info[68]) | uint32(info[69])<<8 | uint32(info[70])<<16 | uint32(info[71])<<24; money != 10_000_000 {
		t.Fatalf("金币错误: %d", money)
	}

	// 选角即进世界：先出属性两帧（C1 26 FE 最大 + C1 26 FF 当前），再出出生包。
	// 语义：种子 Current=0（不构成已持久化正值）→ 满状态进场，当前=最大=132。
	maxStats, curStats := cl.recvStatsOnEnter(t)
	if got, want := statsU32(maxStats, 4), uint32(132); got != want {
		t.Fatalf("最大生命应为属性系统派生值: got=%d want=%d", got, want)
	}
	if got, want := statsU32(curStats, 4), uint32(132); got != want {
		t.Fatalf("当前生命应为满状态: got=%d want=%d", got, want)
	}

	// 选角即进世界（原版 SelectCharacterAsync 尾部直接进图，不等 F3 12）：
	// 紧跟自己的 C2 12 出生包。**自己的 Id 恒为哨兵 0x200（出生位置 0x8000 → 0x8200）**
	// （原版 GetId(self)=ConstantPlayerId），与 F1 00 的 PlayerId 呼应；
	// 真实动态 ID（0x201..0x7FFF）只存在于其他观察者的视角。
	scope := cl.recvPlain(t, time.Second)
	// C2 头：C2 lenH lenL Code（code 在偏移 3）
	if len(scope) != 54 || scope[0] != 0xC2 || scope[3] != 0x12 {
		t.Fatalf("AddCharacterToScopeExtended 错误 len=%d: %X", len(scope), scope)
	}
	if id := uint16(scope[4]) | uint16(scope[5])<<8; id != 0x8000|world.ConstantPlayerID {
		t.Fatalf("自己出生 Id 错误: %04X want %04X（哨兵 0x200 + 出生位）", id, 0x8000|world.ConstantPlayerID)
	}
	if string(scope[16:26]) != "test1Dk\x00\x00\x00" {
		t.Fatalf("角色名错误: %q", scope[16:26])
	}
	if scope[26] != 4 { // 27B 扩展外观首字节：DarkKnight 原始编号
		t.Fatalf("扩展外观职业错误: %d", scope[26])
	}
	if scope[53] != 0 { // 效果计数
		t.Fatalf("效果计数应为0: %d", scope[53])
	}

	// 先排干 enterWorld 残余的 F3 10 背包清单帧（C4）、F3 11 技能列表帧（C1）与
	// F6 1A 进行中任务清单帧（C1），再验证重复 F3 12 幂等。
	var skl []byte
	for {
		f, ok := cl.recvMaybe(t, 200*time.Millisecond)
		if !ok {
			break
		}
		switch {
		case isSelfEnterResidual(f):
			if f[0] == 0xC1 && f[2] == 0xF3 && f[3] == 0x11 {
				skl = f // F3 11 技能列表（原版 SelectCharacterAsync 视图序列段）
			}
		default:
			t.Fatalf("排干阶段出现非预期帧: %X", f)
		}
	}
	// 校验技能列表帧（C1 F3 11）：种子角色未学技能 → count=0，长度=SkillListUpdateRequiredSize(0)。
	if skl == nil {
		t.Fatalf("缺省技能列表帧 C1 F3 11")
	}
	if len(skl) != s2c.SkillListUpdateRequiredSize(int(skl[4])) || skl[4] != 0 {
		t.Fatalf("技能列表帧错误 len=%d count=%d: %X", len(skl), skl[4], skl)
	}
	// 客户端补发 F3 12（加载完地图）→ 幂等忽略，无新帧
	cl.sendClientReady(t)
	if f, ok := cl.recvMaybe(t, 200*time.Millisecond); ok {
		t.Fatalf("重复 F3 12 不应响应，却收到: %X", f)
	}

	// D4 行走：nibble 3 = SouthEast(+1,0) ×2 → (142,131)。给自己回的行走确认 ID = 哨兵 0x200。
	cl.sendWalk(t, test1DkX, test1DkY, 3, 3, 3)
	walk := cl.recvPlain(t, time.Second)
	if len(walk) < 10 || walk[0] != 0xC1 || walk[2] != 0xD4 {
		t.Fatalf("ObjectWalkedExtended 错误: %X", walk)
	}
	if id := uint16(walk[3])<<8 | uint16(walk[4]); id != world.ConstantPlayerID {
		t.Fatalf("行走 Id 错误: %04X want %04X（哨兵自 ID）", id, world.ConstantPlayerID)
	}
	if walk[5] != test1DkX || walk[6] != test1DkY || walk[7] != test1DkX+2 || walk[8] != test1DkY {
		t.Fatalf("行走坐标错误: src(%d,%d) tgt(%d,%d)", walk[5], walk[6], walk[7], walk[8])
	}
	if walk[9]&0x0F != 2 {
		t.Fatalf("步数错误: %02X", walk[9])
	}

	// 重复 F3 12 不产生新广播
	cl.sendClientReady(t)
	if f, ok := cl.recvMaybe(t, 200*time.Millisecond); ok {
		t.Fatalf("重复 F3 12 不应响应，却收到: %X", f)
	}
}

func TestScopeAndWalkBroadcast(t *testing.T) {
	addr, _ := seedTestServer(t)
	cl1 := enterClient(t, addr, "test1")
	cl1.sendSelectCharacter(t, "test1Dk")
	if f := cl1.recvPlain(t, time.Second); len(f) != 92 {
		t.Fatalf("cl1 角色信息错误: %X", f)
	}
	// 选角即进世界：属性两帧 → 自己的出生包 Id = 0x8000|0x200（哨兵）。
	cl1.recvStatsOnEnter(t)
	f := cl1.recvPlain(t, time.Second)
	if len(f) != 54 || f[3] != 0x12 {
		t.Fatalf("cl1 自己出生包错误: %X", f)
	}
	if id := uint16(f[4]) | uint16(f[5])<<8; id != 0x8000|world.ConstantPlayerID {
		t.Fatalf("cl1 自己出生 Id 错误: %04X", id)
	}

	// cl2 进图
	cl2 := enterClient(t, addr, "test2")
	cl2.sendSelectCharacter(t, "test2Dk")
	if f := cl2.recvPlain(t, time.Second); len(f) != 92 {
		t.Fatalf("cl2 角色信息错误: %X", f)
	}

	// cl1 收到 cl2 的出生广播（无出生位，真实动态 ID）。
	// cl2 进图会先下发 F3 10 背包清单（真机修复 11）与 F3 11 技能列表（自身残余），
	// cl1 只关心出生包——跳过 C4 / C1 F3 11 帧直到收到 C2 12。
	var got []byte
	for {
		got = cl1.recvPlain(t, time.Second)
		if isSelfEnterResidual(got) {
			continue // 自身残余（背包清单 / 技能列表 / 进行中任务清单），跳过
		}
		break
	}
	if len(got) != 54 || got[3] != 0x12 {
		t.Fatalf("cl1 收到 cl2 视野包错误: %X", got)
	}
	cl2RealID := uint16(got[4]) | uint16(got[5])<<8
	if cl2RealID&0x8000 != 0 || cl2RealID < 0x201 {
		t.Fatalf("cl2 视野 Id 应为无出生位的动态 ID(≥0x201): %04X", cl2RealID)
	}

	// cl2 收到自己（0x8200）+ 已在场的 cl1（无出生位真实 ID）两个包
	var sawSelf, sawCl1 bool
	var cl1RealID uint16
	cl2.recvStatsOnEnter(t)
	for saw := 0; saw < 2; {
		fr := cl2.recvPlain(t, time.Second)
		if isSelfEnterResidual(fr) {
			continue // 自身残余（背包清单 / 技能列表 / 进行中任务清单），跳过
		}
		if len(fr) != 54 {
			t.Fatalf("cl2 视野包长度错误: %X", fr)
		}
		id := uint16(fr[4]) | uint16(fr[5])<<8
		switch {
		case id == 0x8000|world.ConstantPlayerID:
			sawSelf = true
		case id&0x8000 == 0 && id >= 0x201:
			sawCl1 = true
			cl1RealID = id
		}
		saw++
	}
	if !sawSelf || !sawCl1 {
		t.Fatalf("cl2 进图视野缺失 self=%v cl1=%v", sawSelf, sawCl1)
	}

	// cl1 行走 → cl2 收到同内容广播（cl1 的真实 ID）。nibble 3 = SouthEast(+1,0) ×2。
	cl1.sendWalk(t, test1DkX, test1DkY, 3, 3, 3)
	var relay []byte
	for {
		relay = cl2.recvPlain(t, time.Second)
		if len(relay) >= 9 && relay[2] == 0xD4 {
			break
		}
	}
	if id := uint16(relay[3])<<8 | uint16(relay[4]); id != cl1RealID {
		t.Fatalf("广播 Id 错误: %04X want %04X", id, cl1RealID)
	}
	if relay[7] != test1DkX+2 || relay[8] != test1DkY {
		t.Fatalf("广播目标坐标错误: (%d,%d)", relay[7], relay[8])
	}
}

func TestWalkRejectedBeforeEnter(t *testing.T) {
	addr, _ := seedTestServer(t)
	cl := enterClient(t, addr, "test3")

	// 已登录未进图发 D4：服务端忽略
	cl.sendWalk(t, 140, 131, 0, 4)
	if f, ok := cl.recvMaybe(t, 200*time.Millisecond); ok {
		t.Fatalf("未进图行走应被忽略，却收到: %X", f)
	}
}
