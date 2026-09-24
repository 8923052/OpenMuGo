package gameserver

// handler_crafting_test.go —— S8 接线测试（成功/失败掷骰在 action 层已确定性覆盖）：
// ① 0x24 把物品移入混沌锅临时容器；② 缺料合成 → C1 86 LackingMixItems、不消费、不扣钱、
// 刷新清单。用真实载入的 +10 配方（number 3）。

import (
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// firstWearableDef 返回配置里第一件可穿戴、有耐久的装备定义。
func firstWearableDef(t *testing.T, gc *config.GameConfig) *config.Item {
	t.Helper()
	for i := range gc.Items {
		d := &gc.Items[i]
		if (d.Slot != nil || len(d.Slots) > 0) && d.Durability > 0 {
			return d
		}
	}
	t.Fatal("无可穿戴装备定义")
	return nil
}

// chaosMachineNpc 是 S6 里挂混沌锅配方的 NPC 号（导出数据：配方宿主 = 238）。
const chaosMachineNpc int16 = 238

// newCraftSess 建一个"已打开混沌锅"的会话（配方按 OpenedNpc 归属查找，必须有宿主 NPC）。
func newCraftSess(t *testing.T) (*Server, *session, *entity.Character, *packetRecorder) {
	t.Helper()
	srv := newScopeTestSrv(t)
	rec := &packetRecorder{}
	sess, wp := newScopedSession(7, "smith", 20, 20, rec)
	c := sess.getSelected()
	c.Stats = &entity.CharStats{Money: 5_000_000, Strength: 100, Agility: 100}
	c.Inventory = storage.NewInventory(0)
	wp.View = sess.playerView
	srv.world.Map(0).Enter(wp)
	sess.setWorldPlayer(wp)
	sess.setOpenedNpc(craftHostNpc(t, srv, chaosMachineNpc))
	sess.craftStorage = newCraftStorage()
	return srv, sess, c, rec
}

func mixFrame(t *testing.T, mixType byte) []byte {
	t.Helper()
	req := c2s.NewChaosMachineMixRequest()
	req.SetMixType(c2s.ChaosMachineMixType(mixType))
	return req.Bytes()
}

func jewelInto(t *testing.T, srv *Server, sess *session, slot byte, name string, level int) {
	t.Helper()
	def, ok := srv.deps.cfg.GameConfig.ItemByName(name)
	if !ok {
		t.Fatalf("导出件里没有 %s", name)
	}
	it := &item.Item{Group: byte(def.Group), Number: def.Number, Level: byte(level)}
	if !sess.craftStorage.AddToFree(srv.newSlottedItem(it)) {
		t.Fatal("放入临时容器失败")
	}
}

// TestCraftMixInOtherWindow 锁定窗口隔离：配方不挂在当前 NPC 上时按 IncorrectMix 拒（
// 原版只在 OpenedNpc.Definition.ItemCraftings 里找）。
func TestCraftMixInOtherWindow(t *testing.T) {
	srv, sess, c, rec := newCraftSess(t)
	// SeedMaster（452）挂的是镶嵌系配方，做不了 +10。
	sess.setOpenedNpc(craftHostNpc(t, srv, 452))
	before := c.Stats.Money

	srv.handleMix(sess, mixFrame(t, 3))

	if c.Stats.Money != before {
		t.Fatal("被拒的合成不得扣钱")
	}
	f := findFrame(rec, 0xC1, 0x86)
	if f == nil || s2c.AsItemCraftingResult(f).Result() != s2c.CraftingResult_IncorrectMixItems {
		t.Fatalf("应回 IncorrectMixItems, got %v", f)
	}
}

// TestCraftMixChargesPriceWhateverOutcome 锁定"+10 每次必收 2000000、四件投入无论成败都消耗"
// （原版先扣钱再掷骰，失败时目标与宝石一起消失）。
func TestCraftMixChargesPriceWhateverOutcome(t *testing.T) {
	srv, sess, c, rec := newCraftSess(t)
	w := firstWearableDef(t, srv.deps.cfg.GameConfig)
	if !sess.craftStorage.AddToFree(srv.newSlottedItem(&item.Item{
		Group: byte(w.Group), Number: w.Number, Level: 9, Durability: byte(w.Durability)})) {
		t.Fatal("预置目标失败")
	}
	jewelInto(t, srv, sess, 1, "Jewel of Chaos", 0)
	jewelInto(t, srv, sess, 2, "Jewel of Bless", 0)
	jewelInto(t, srv, sess, 3, "Jewel of Soul", 0)

	before := c.Stats.Money
	srv.handleMix(sess, mixFrame(t, 3))

	if got := before - c.Stats.Money; got != 2_000_000 {
		t.Fatalf("价格应为 2000000（先扣钱再掷骰）, got %d", got)
	}
	if n := countFrames(rec, 0xC1, 0x86); n != 1 {
		t.Fatalf("应回 1 帧 C1 86, got %d", n)
	}
	result := s2c.AsItemCraftingResult(findFrame(rec, 0xC1, 0x86)).Result()
	// 成功：目标 StaysAsIs 留在锅里（已 +1），三颗宝石消失；失败：四件全消失。
	switch result {
	case s2c.CraftingResult_Success:
		if sess.craftStorage.Count() != 1 {
			t.Fatalf("成功时锅里应只剩升级后的目标, got %d", sess.craftStorage.Count())
		}
		if got := sess.craftStorage.Items()[0].It.Level; got != 10 {
			t.Fatalf("目标应升到 +10, got %d", got)
		}
	case s2c.CraftingResult_Failed:
		if sess.craftStorage.Count() != 0 {
			t.Fatalf("失败时四件都应消失, got %d", sess.craftStorage.Count())
		}
	default:
		t.Fatalf("结果应是成功或失败, got %d", result)
	}
	if n := countFrames(rec, 0xC2, 0x31); n == 0 {
		t.Fatal("合成后应刷新混沌锅清单（C2 31）")
	}
}

// TestCraftDialogCloseReturnsItems 锁定 C1 87 与 C1 31 同语义：退回容器里的物品。
func TestCraftDialogCloseReturnsItems(t *testing.T) {
	srv, sess, c, _ := newCraftSess(t)
	jewelInto(t, srv, sess, 0, "Jewel of Chaos", 0)

	srv.handleNpcClose(sess, []byte{0xC1, 0x03, 0x87})

	if sess.craftStorage != nil {
		t.Fatal("关窗后临时容器应释放")
	}
	if c.Inventory.GetItem(12) == nil && c.Inventory.Count() == 0 {
		t.Fatal("宝石应退回背包")
	}
}

func TestCraftMoveItemIntoStorage(t *testing.T) {
	srv, sess, c, _ := newCraftSess(t)
	gc := srv.deps.cfg.GameConfig
	jd, ok := gc.ItemByName("Jewel of Chaos")
	if !ok {
		t.Fatal("无 Jewel of Chaos")
	}
	si := srv.newSlottedItem(&item.Item{Group: byte(jd.Group), Number: jd.Number, Durability: 1})
	if !c.Inventory.AddToSlot(12, si) {
		t.Fatal("预置宝石失败")
	}

	if !srv.handleItemMoveCraft(sess, inventoryStorageKind, craftStorageKind, 12, 0) {
		t.Fatal("移入混沌锅应成功")
	}
	if sess.craftStorage.GetItem(0) == nil {
		t.Fatal("混沌锅槽 0 应有物品")
	}
	if c.Inventory.GetItem(12) != nil {
		t.Fatal("宝石应已离开背包")
	}
}

func TestCraftMixLackingItems(t *testing.T) {
	srv, sess, c, rec := newCraftSess(t)
	gc := srv.deps.cfg.GameConfig
	// 只放一件 +9 武器，缺三颗宝石 → LackingMixItems。
	w := firstWearableDef(t, gc)
	_ = sess.craftStorage.AddToSlot(0, srv.newSlottedItem(&item.Item{Group: byte(w.Group), Number: w.Number, Level: 9, Durability: byte(w.Durability)}))

	before := c.Stats.Money
	srv.handleMix(sess, mixFrame(t, 3)) // +10

	f := findFrame(rec, 0xC1, 0x86)
	if f == nil {
		t.Fatal("应收到 C1 86 合成结果")
	}
	if got := s2c.AsItemCraftingResult(f).Result(); got != s2c.CraftingResult_LackingMixItems {
		t.Fatalf("结果=%d, want LackingMixItems(%d)", got, s2c.CraftingResult_LackingMixItems)
	}
	if c.Stats.Money != before {
		t.Fatalf("缺料不应扣钱: before=%d after=%d", before, c.Stats.Money)
	}
	if sess.craftStorage == nil || sess.craftStorage.Count() != 1 {
		t.Fatal("缺料不应消费容器物品")
	}
}

// craftHostNpc 造一个带定义与窗口的合成 NPC（定义缺失会让关窗路径解引用 nil）。
func craftHostNpc(t *testing.T, srv *Server, number int16) *npc.Npc {
	t.Helper()
	def, ok := srv.deps.cfg.GameConfig.Monster(int(number))
	if !ok {
		t.Fatalf("导出件里没有 NPC %d", number)
	}
	return &npc.Npc{ID: 0x2FF, Number: number, Name: def.Name, Def: def}
}
