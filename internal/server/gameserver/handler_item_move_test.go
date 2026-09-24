package gameserver

// handler_item_move_test.go —— T2-5 闭环测试：背包内搬运（C3 24）与装备穿脱 +
// 外观广播（C1 25）。覆盖三道门的端到端表现、堆叠出站序列、自制物品重复漏洞守卫。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// moveScaffold 是搬运测试脚手架：GS + 移动者会话 + 观察者会话（两者都在同一地图视野内）。
type moveScaffold struct {
	srv     *Server
	sess    *session
	wp      *world.Player
	c       *entity.Character
	rec     *packetRecorder
	obsSess *session
	obsWP   *world.Player
	obsRec  *packetRecorder
}

// newMoveScaffold 构造脚手架；class 为角色职业编号，attrs 为 Total* 属性种子。
func newMoveScaffold(t *testing.T, class byte, attrs entity.CharStats) *moveScaffold {
	t.Helper()
	srv := newScopeTestSrv(t)

	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "mover", 100, 100, rec)
	c := sess.getSelected()
	c.ClassNumber = class
	c.Stats = &attrs
	c.Inventory = storage.NewInventory(0)
	wp.Class = class
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)

	obsRec := &packetRecorder{}
	obsSess, obsWP := newScopedSession(9, "observer", 102, 100, obsRec)
	obsWP.View = obsSess.playerView
	srv.world.Map(0).Enter(obsWP)
	obsSess.setWorldPlayer(obsWP)

	return &moveScaffold{
		srv: srv, sess: sess, wp: wp, c: c, rec: rec,
		obsSess: obsSess, obsWP: obsWP, obsRec: obsRec,
	}
}

// putItem 把物品放进背包指定槽（尺寸取导出件的定义）。
func (m *moveScaffold) putItem(t *testing.T, slot byte, it *item.Item) *storage.SlottedItem {
	t.Helper()
	si := m.srv.newSlottedItem(it)
	if !m.c.Inventory.AddToSlot(slot, si) {
		t.Fatalf("预置物品失败 slot=%d group=%d number=%d", slot, it.Group, it.Number)
	}
	return si
}

// move 构造并发一个 C3 24 扩展搬运帧。
func (m *moveScaffold) move(fromStorage, fromSlot, toStorage, toSlot byte) {
	req := c2s.NewItemMoveRequestExtended()
	req.SetFromStorage(c2s.ItemStorageKind(fromStorage))
	req.SetFromSlot(fromSlot)
	req.SetToStorage(c2s.ItemStorageKind(toStorage))
	req.SetToSlot(toSlot)
	m.srv.handleItemMove(m.sess, req.Bytes())
}

// strongStats 返回满足绝大多数装备需求的属性种子。
func strongStats() entity.CharStats {
	return entity.CharStats{Strength: 3000, Agility: 3000, Vitality: 3000, Energy: 3000, Leadership: 3000}
}

// moveSuccessCount 统计**成功**的 C3 24 帧（失败帧的第 3 字节是子码 0xFF，
// 与成功帧同码不同子码，必须区分开）。
func moveSuccessCount(rec *packetRecorder) int {
	n := 0
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC3 && frameCode(f) == 0x24 && f[3] != 0xFF {
			n++
		}
	}
	return n
}

// TestItemMoveWithinInventory 锁定 T2-5 主路径：背包格间搬输出 C3 24（目标槽 + 容器）。
func TestItemMoveWithinInventory(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	kris := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 20}
	m.putItem(t, 12, kris)

	m.move(0, 12, 0, 20)

	if n := moveSuccessCount(m.rec); n != 1 {
		t.Fatalf("应收到 1 帧成功的 C3 24, got %d", n)
	}
	frame := findFrame(m.rec, 0xC3, 0x24)
	p := s2c.AsItemMoved(frame)
	if p.TargetStorageType() != s2c.ItemStorageKind_Inventory {
		t.Fatalf("目标容器应为 Inventory(0), got %d", p.TargetStorageType())
	}
	if p.TargetSlot() != 20 {
		t.Fatalf("目标槽应为 20, got %d", p.TargetSlot())
	}
	if n := len(p.ItemData()); n < 5 || n > 15 {
		t.Fatalf("物品数据长度应在 5~15, got %d", n)
	}
	// 帧长 = C3 头(3) + 容器/槽(2) + 物品数据。
	if len(frame) != 5+len(p.ItemData()) {
		t.Fatalf("帧长异常: %d vs 5+%d", len(frame), len(p.ItemData()))
	}
	if m.c.Inventory.GetItem(12) != nil {
		t.Fatal("源槽应已清空")
	}
	if si := m.c.Inventory.GetItem(20); si == nil || si.It != kris {
		t.Fatal("目标槽应持有该物品")
	}
	// 网格搬运不产生外观广播。
	if n := countFrames(m.rec, 0xC1, 0x25); n != 0 {
		t.Fatalf("网格搬运不应广播外观, got %d", n)
	}
	if n := countFrames(m.obsRec, 0xC1, 0x25); n != 0 {
		t.Fatalf("观察者不应收到外观包, got %d", n)
	}
}

// TestItemMoveRejectedKeepsState 锁定"重叠/异类占位"拒绝：回 C3 24 FF 且容器不变。
func TestItemMoveRejectedKeepsState(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	a := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 20} // Kris 1×2
	b := &item.Item{Group: 1, Number: 0, Level: 0, Durability: 18} // Small Axe 1×3
	m.putItem(t, 12, a)                                            // row0..1 col0
	m.putItem(t, 14, b)                                            // row0..2 col2

	m.move(0, 12, 0, 14)

	if moveSuccessCount(m.rec) != 0 {
		t.Fatal("不应收到成功的 C3 24")
	}
	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || len(fail) < 4 || fail[3] != 0xFF {
		t.Fatalf("应收到 C3 24 FF 失败包: % X", fail)
	}
	if m.c.Inventory.GetItem(12) == nil || m.c.Inventory.GetItem(14) == nil {
		t.Fatal("拒绝后两槽物品都应原地不动")
	}
}

// TestItemMoveOutOfBoundsRejected 锁定越界（1×3 长剑放不下末行）拒绝。
func TestItemMoveOutOfBoundsRejected(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	// Short Sword (0,1) 是 1×3；slot 75 是主网格末行末列，3 高放不下。
	sword := &item.Item{Group: 0, Number: 1, Level: 0, Durability: 22}
	m.putItem(t, 12, sword)

	m.move(0, 12, 0, 75)

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("越界应拒绝: % X", fail)
	}
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("拒绝后物品应留在原槽")
	}
}

// TestItemMoveSelfSlotGuard 变异检验：同槽搬运不得触发"数量翻倍"（原版堆叠分支的潜在漏洞）。
func TestItemMoveSelfSlotGuard(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	apple := &item.Item{Group: 14, Number: 0, Durability: 1}
	m.putItem(t, 12, apple)

	m.move(0, 12, 0, 12)

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("同槽搬运应被拒绝: % X", fail)
	}
	si := m.c.Inventory.GetItem(12)
	if si == nil || si.It.Durability != 1 {
		t.Fatalf("数量不得翻倍: %+v", si)
	}
	if m.c.Inventory.Count() != 1 {
		t.Fatalf("物品数不得变化: %d", m.c.Inventory.Count())
	}
}

// TestEquipItemBroadcastsAppearance 锁定 T2-5 核心验收项：穿上装备 → 观察者收 C1 25
// （字段正确、含卓越标记），穿的人自己**不**收，且自身 27B 进图外观同步打补丁。
func TestEquipItemBroadcastsAppearance(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	kris := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 20, ExcellentBits: 0x01}
	m.putItem(t, 12, kris)

	m.move(0, 12, 0, byte(item.SlotLeftHand))

	// 自己：成功包，无外观包。
	if moveSuccessCount(m.rec) != 1 {
		t.Fatal("自己应收到成功的 C3 24 包")
	}
	if n := countFrames(m.rec, 0xC1, 0x25); n != 0 {
		t.Fatalf("外观包不应发给自己, got %d", n)
	}
	// 观察者：恰一帧 C1 25，且字段与物品一致。
	if n := countFrames(m.obsRec, 0xC1, 0x25); n != 1 {
		t.Fatalf("观察者应收到 1 帧 C1 25, got %d", n)
	}
	frame := findFrame(m.obsRec, 0xC1, 0x25)
	if len(frame) != s2c.AppearanceChangedExtendedLength {
		t.Fatalf("C1 25 应为 14B, got %d", len(frame))
	}
	p := s2c.AsAppearanceChangedExtended(frame)
	if p.ChangedPlayerId() != m.wp.ID {
		t.Fatalf("对象 ID 应为移动者 %d, got %d", m.wp.ID, p.ChangedPlayerId())
	}
	if p.ItemSlot() != byte(item.SlotLeftHand) {
		t.Fatalf("槽位应为 0, got %d", p.ItemSlot())
	}
	if p.ItemGroup() != 0 {
		t.Fatalf("ItemGroup 应为物品组 0, got %d", p.ItemGroup())
	}
	if p.ItemNumber() != 0 || p.ItemLevel() != 4 {
		t.Fatalf("编号/等级异常: %d/%d", p.ItemNumber(), p.ItemLevel())
	}
	if p.ExcellentFlags() != 0x01 {
		t.Fatalf("卓越位应为 0x01, got %#x", p.ExcellentFlags())
	}
	if p.IsAncientSetComplete() {
		t.Fatal("非整套远古不应置位")
	}
	// 自身进图外观补丁：[2..4] = group/num 高 nibble、num 低字节、glow|flags。
	if m.wp.Appearance[3] != 0 {
		t.Fatalf("外观 number 低字节应为 0, got %#x", m.wp.Appearance[3])
	}
	if want := byte(((4-1)/2)<<4 | 0x08); m.wp.Appearance[4] != want {
		t.Fatalf("外观 glow|卓越应为 %#x, got %#x", want, m.wp.Appearance[4])
	}
}

// TestUnequipItemBroadcastsGroupFF 锁定卸下语义：ItemGroup 下 0xFF（客户端清空该槽模型）。
func TestUnequipItemBroadcastsGroupFF(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	kris := &item.Item{Group: 0, Number: 0, Level: 4, Durability: 20}
	m.putItem(t, byte(item.SlotLeftHand), kris)

	m.move(0, byte(item.SlotLeftHand), 0, 12)

	if n := countFrames(m.obsRec, 0xC1, 0x25); n != 1 {
		t.Fatalf("观察者应收到 1 帧 C1 25, got %d", n)
	}
	p := s2c.AsAppearanceChangedExtended(findFrame(m.obsRec, 0xC1, 0x25))
	if p.ItemSlot() != byte(item.SlotLeftHand) {
		t.Fatalf("槽位应报告被卸下的槽 0, got %d", p.ItemSlot())
	}
	if p.ItemGroup() != 0xFF {
		t.Fatalf("卸下必须下 0xFF, got %#x", p.ItemGroup())
	}
	// 外观补丁应清空该槽（FF FF 00）。
	if m.wp.Appearance[2] != 0xFF || m.wp.Appearance[3] != 0xFF || m.wp.Appearance[4] != 0x00 {
		t.Fatalf("卸下后外观槽应清空: % X", m.wp.Appearance[2:5])
	}
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("卸下后物品应回到网格")
	}
}

// TestEquipRejectedByRequirements 锁定第二道门：需求不足拒绝且不广播外观。
func TestEquipRejectedByRequirements(t *testing.T) {
	m := newMoveScaffold(t, 4, entity.CharStats{Strength: 10, Agility: 10})
	kris := &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20}
	m.putItem(t, 12, kris)

	m.move(0, 12, 0, byte(item.SlotLeftHand))

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("需求不足应拒绝: % X", fail)
	}
	if m.c.Inventory.GetItem(byte(item.SlotLeftHand)) != nil {
		t.Fatal("拒绝后装备槽应为空")
	}
	if n := countFrames(m.obsRec, 0xC1, 0x25); n != 0 {
		t.Fatalf("拒绝不应广播外观, got %d", n)
	}
}

// TestEquipRejectedByHandsConflict 锁定第三道门：右手非弹药武器时左手放不下 2 宽件。
func TestEquipRejectedByHandsConflict(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	// 右手已有近战武器 Kris（slots [0,1]，非弹药）。
	m.putItem(t, byte(item.SlotRightHand), &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20})
	// Sword of Salamander (0,9)：2 宽、slots [0]、需求可满足 → 仅因双手冲突被拒。
	wide := &item.Item{Group: 0, Number: 9, Level: 0, Durability: 30}
	m.putItem(t, 12, wide)

	m.move(0, 12, 0, byte(item.SlotLeftHand))

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("双手冲突应拒绝: % X", fail)
	}
	if m.c.Inventory.GetItem(byte(item.SlotLeftHand)) != nil {
		t.Fatal("冲突时左手应为空")
	}
	// 换掉右手为弹药（Bolt 4,7）后应当放得下。
	if !m.c.Inventory.Remove(m.c.Inventory.GetItem(byte(item.SlotRightHand))) {
		t.Fatal("移除右手武器失败")
	}
	m.putItem(t, byte(item.SlotRightHand), &item.Item{Group: 4, Number: 7, Level: 0, Durability: 255})
	m.move(0, 12, 0, byte(item.SlotLeftHand))
	si := m.c.Inventory.GetItem(byte(item.SlotLeftHand))
	if si == nil || si.It != wide {
		t.Fatal("右手为弹药时左手应能装备 2 宽件")
	}
}

// TestCompleteStackFlow 锁定叠满：C3 24 FF（归位）+ C1 28（删源件）+ C1 2A（目标数量）。
func TestCompleteStackFlow(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	src := &item.Item{Group: 14, Number: 0, Level: 0, Durability: 1}
	dst := &item.Item{Group: 14, Number: 0, Level: 0, Durability: 2}
	m.putItem(t, 12, src)
	m.putItem(t, 13, dst)

	m.move(0, 12, 0, 13)

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("叠满应先回失败包: % X", fail)
	}
	if n := countFrames(m.rec, 0xC1, 0x28); n != 1 {
		t.Fatalf("应收到 1 帧 C1 28（删源件）, got %d", n)
	}
	rm := s2c.AsItemRemoved(findFrame(m.rec, 0xC1, 0x28))
	if rm.InventorySlot() != 12 {
		t.Fatalf("C1 28 槽位应为源槽 12, got %d", rm.InventorySlot())
	}
	if n := countFrames(m.rec, 0xC1, 0x2A); n != 1 {
		t.Fatalf("应收到 1 帧 C1 2A, got %d", n)
	}
	dc := s2c.AsItemDurabilityChanged(findFrame(m.rec, 0xC1, 0x2A))
	if dc.InventorySlot() != 13 || dc.Durability() != 3 {
		t.Fatalf("C1 2A 应为槽 13 数量 3, got 槽 %d 数量 %d", dc.InventorySlot(), dc.Durability())
	}
	if m.c.Inventory.Count() != 1 {
		t.Fatalf("叠满后背包应只剩 1 件, got %d", m.c.Inventory.Count())
	}
	if m.c.Inventory.GetItem(13) == nil {
		t.Fatal("目标槽应仍在")
	}
}

// TestPartialStackFlow 锁定部分堆叠：失败包 + 两端各一帧 C1 2A，源件留余量。
func TestPartialStackFlow(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	src := &item.Item{Group: 14, Number: 0, Level: 0, Durability: 3}
	dst := &item.Item{Group: 14, Number: 0, Level: 0, Durability: 1}
	m.putItem(t, 12, src)
	m.putItem(t, 13, dst)

	m.move(0, 12, 0, 13)

	if n := countFrames(m.rec, 0xC1, 0x2A); n != 2 {
		t.Fatalf("部分堆叠应刷新两端数量（2 帧 C1 2A）, got %d", n)
	}
	if m.c.Inventory.Count() != 2 {
		t.Fatalf("部分堆叠不应删件, got %d", m.c.Inventory.Count())
	}
	if src.Durability != 1 || dst.Durability != 3 {
		t.Fatalf("数量分配错误: src=%d dst=%d", src.Durability, dst.Durability)
	}
}

// TestItemMoveWrongStorageRejected 锁定非 INVENTORY 容器一律拒绝（T2-5 只支持背包内部）。
func TestItemMoveWrongStorageRejected(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())
	kris := &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20}
	m.putItem(t, 12, kris)

	m.move(1 /* TRADE */, 12, 0, 20)

	fail := findFrame(m.rec, 0xC3, 0x24)
	if fail == nil || fail[3] != 0xFF {
		t.Fatalf("非背包容器应拒绝: % X", fail)
	}
	if m.c.Inventory.GetItem(12) == nil {
		t.Fatal("拒绝后物品应原地不动")
	}
}

// lastSubFrame 返回 recorder 中最后一帧"头+code+子码"全部匹配的帧
// （穿脱会连续产生多帧 C1 26，取最后一帧即该次移动的最新值）。
func lastSubFrame(rec *packetRecorder, h, code, sub byte) []byte {
	for i := len(rec.frames) - 1; i >= 0; i-- {
		f := rec.frames[i]
		if len(f) >= 4 && f[0] == h && frameCode(f) == code && f[3] == sub {
			return f
		}
	}
	return nil
}

// TestEquipExcellentSyncsStats 锁定卓越属性穿脱同步（真机回归）：穿上卓越装备
// （Kris 卓越位 3 = Attack Speed Any +7）自己应收到 C1 26 FE/FF，攻速含卓越
// 加成；脱下后攻速回落。旧代码穿脱只刷战斗快照，不重算 c.Stats、不下发，
// 客户端属性面板毫无变化。
func TestEquipExcellentSyncsStats(t *testing.T) {
	// 低属性种子 + DK：避免敏捷关系把攻速顶到 200 上限，+7 差值才可辨。
	m := newMoveScaffold(t, 0, entity.CharStats{Strength: 100, Agility: 100, Vitality: 20, Energy: 20})
	plain := &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20}
	m.putItem(t, 12, plain)
	m.move(0, 12, 0, byte(item.SlotLeftHand))
	baseSpeed := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).AttackSpeed()

	// 换回带卓越位 3（攻速 +7）的同型装备。
	m.move(0, byte(item.SlotLeftHand), 0, 12)
	// 此时手上已无武器，记录空手攻速（卓越武器脱下后应回到此值）。
	bareSpeed := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).AttackSpeed()
	exc := &item.Item{Group: 0, Number: 0, Level: 0, Durability: 20, ExcellentBits: 1 << 2}
	m.putItem(t, 13, exc)
	m.move(0, 13, 0, byte(item.SlotLeftHand))
	got := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).AttackSpeed()
	if got != baseSpeed+7 {
		t.Fatalf("穿上卓越装备攻速 = %d, want 基线 %d +7", got, baseSpeed)
	}
	// 上限包 FE 也应下发（卓越装备可能改变血/蓝/盾/AG 上限）。
	if lastSubFrame(m.rec, 0xC1, 0x26, 0xFE) == nil {
		t.Fatal("应下发 C1 26 FE 上限更新")
	}

	// 脱下 → 攻速回落为空手值。
	m.move(0, byte(item.SlotLeftHand), 0, 13)
	back := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).AttackSpeed()
	if back != bareSpeed {
		t.Fatalf("脱下后攻速应回落为空手 %d, got %d", bareSpeed, back)
	}
}

// TestUnequipClampsCurrentStats 锁定脱下加属性装备时的当前值钳制（原版
// LimitCurrentAttribute）：穿上卓越位 2（+生命）的 Kris 时当前血为新上限的
// 满血；脱下后上限回落空手值，FF 的当前血必须同步钳到新上限（旧实现会下发
// FE 小上限 + FF 大当前值的不自洽数据）。
func TestUnequipClampsCurrentStats(t *testing.T) {
	// Small Shield(6,0)：需求力量 70；卓越位 6（bit5）= MaximumHealth ×1.04。
	m := newMoveScaffold(t, 0, entity.CharStats{Strength: 100})
	exc := &item.Item{Group: 6, Number: 0, Level: 0, Durability: 20, ExcellentBits: 1 << 5}
	m.putItem(t, 12, exc)
	m.move(0, 12, 0, byte(item.SlotRightHand))
	equippedMax := s2c.AsMaximumStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFE)).Health()
	equippedCur := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).Health()
	if equippedCur != equippedMax {
		t.Fatalf("穿上后当前血应=满血 %d, got %d", equippedMax, equippedCur)
	}

	// 脱下 → 上限下降，当前血必须钳到新上限。
	m.move(0, byte(item.SlotRightHand), 0, 12)
	bareMax := s2c.AsMaximumStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFE)).Health()
	bareCur := s2c.AsCurrentStatsExtended(lastSubFrame(m.rec, 0xC1, 0x26, 0xFF)).Health()
	if bareMax >= equippedMax {
		t.Fatalf("脱下后血上限应下降: equipped=%d bare=%d", equippedMax, bareMax)
	}
	if bareCur != bareMax {
		t.Fatalf("脱下后当前血应钳到新上限 %d, got %d", bareMax, bareCur)
	}
}

// TestItemMoveMalformedFrames 锁定短帧防御：不 panic、回失败包、源头为空也回失败包。
func TestItemMoveMalformedFrames(t *testing.T) {
	m := newMoveScaffold(t, 4, strongStats())

	// 短帧：扩展形态固定 7B，4B 帧直接拒绝。
	m.srv.handleItemMove(m.sess, []byte{0xC3, 0x04, 0x24, 0x00})
	if f := findFrame(m.rec, 0xC3, 0x24); f == nil || f[3] != 0xFF {
		t.Fatalf("短帧应回失败包: % X", f)
	}

	// 源槽为空。
	m.move(0, 30, 0, 31)
	if f := findFrame(m.rec, 0xC3, 0x24); f == nil || f[3] != 0xFF {
		t.Fatalf("空源槽应回失败包: % X", f)
	}

	// 未进图状态直接忽略（不产生任何包）。
	m.sess.setState(entity.StateAuthenticated)
	before := len(m.rec.frames)
	m.move(0, 12, 0, 20)
	if len(m.rec.frames) != before {
		t.Fatal("非进图状态不应产生出站包")
	}
}
