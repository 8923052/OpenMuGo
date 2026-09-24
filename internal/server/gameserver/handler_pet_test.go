package gameserver

// handler_pet_test.go —— 宠物子系统 GS 侧闭环：命令切换（C1 A7）、信息窗口（C1 A9）、
// 可训练宠物经验升级（PetExperiencePlugIn）。用 newExpScaffold 复用带怪物的 GS 桩。

import (
	"testing"

	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/storage"
	c2s "mugo/internal/proto/c2s"
)

// findPetFrame 返回 rec 里第一个匹配 code 的下行帧（C1 组），无则 nil。
func findPetFrame(rec *packetRecorder, code byte) []byte {
	for _, f := range rec.frames {
		if len(f) >= 3 && f[0] == 0xC1 && f[2] == code {
			return f
		}
	}
	return nil
}

func equipPet(c *entity.Character, slot byte, it *item.Item) bool {
	return c.Inventory.AddToSlot(slot, &storage.SlottedItem{It: it, Width: 1, Height: 1})
}

// TestPetCommandSwitch 验证：右手槽装备黑暗渡鸦后 syncPetManager 建管理器，
// 收到 C1 A7（Normal）→ 切 Idle 并回 C1 A7 PetMode（mode=0、targetId=0xFFFF）。
func TestPetCommandSwitch(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	if !equipPet(c, item.SlotRightHand, &item.Item{Group: 13, Number: 5, Durability: 255}) {
		t.Fatal("装备渡鸦失败")
	}
	srv.syncPetManager(sess, c, wp)
	if sess.getPetManager() == nil {
		t.Fatal("装备渡鸦后未创建命令管理器")
	}

	req := c2s.NewPetCommandRequest()
	req.SetCommandMode(c2s.PetCommandMode_Normal)
	srv.handlePetCommand(sess, req.Bytes())

	frame := findPetFrame(rec, 0xA7)
	if frame == nil {
		t.Fatal("未下发 PetMode(0xA7)")
	}
	if frame[4] != 0 { // PetCommandMode_Normal
		t.Fatalf("PetMode 命令应为 0(Idle)，got %d", frame[4])
	}
}

// TestPetInfoResponse 验证：C1 A9 请求黑暗之马信息 → 回 C1 A9，含等级/经验/耐久。
func TestPetInfoResponse(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	if !equipPet(c, item.SlotPet, &item.Item{Group: 13, Number: 4, Level: 7, Durability: 200, PetExperience: 300000}) {
		t.Fatal("装备黑暗之马失败")
	}
	req := c2s.NewPetInfoRequest()
	req.SetItemSlot(item.SlotPet)
	srv.handlePetInfo(sess, req.Bytes())

	frame := findPetFrame(rec, 0xA9)
	if frame == nil {
		t.Fatal("未下发 PetInfoResponse(0xA9)")
	}
	if frame[3] != 1 { // PetType: 马=1
		t.Fatalf("PetInfoResponse 宠物类型应为马(1)，got %d", frame[3])
	}
	if frame[6] != 7 { // Level
		t.Fatalf("PetInfoResponse 等级应为 7，got %d", frame[6])
	}
}

// TestGrantPetExperience 验证可训练宠物按主人击杀经验升级（黑暗之马无统率门槛）：
// 10000 击杀经验 → 宠物得 20% = 2000 → 越过 1 级阈值 1100、未达 2 级 9600 → 升到 1 级。
func TestGrantPetExperience(t *testing.T) {
	srv, sess, _, c, _ := newExpScaffold(t, 200)
	horse := &item.Item{Group: 13, Number: 4, Level: 0, Durability: 255}
	if !equipPet(c, item.SlotPet, horse) {
		t.Fatal("装备黑暗之马失败")
	}
	srv.grantPetExperience(sess, c, 10000)

	if horse.PetExperience != 2000 {
		t.Fatalf("宠物经验应累计 2000，got %d", horse.PetExperience)
	}
	if horse.Level != 1 {
		t.Fatalf("宠物应升到 1 级，got %d", horse.Level)
	}
	// 再给大量经验 → 继续升级（验证 while 逐级 + 上限内推进）。
	srv.grantPetExperience(sess, c, 1000000)
	if horse.Level < 2 {
		t.Fatalf("追加经验后应至少升到 2 级，got %d", horse.Level)
	}
}
