package remote

// 与 C# 参考向量（OpenMU ItemSerializer/AppearanceSerializer* 逐行移植）逐字节比对。
// 外观参考实现已通过 OpenMU 官方单元测试 SmallAxe 硬编码锚点自校验。
// 向量文件随被测代码同迁 —— 原来在 gamelogic/entity/item。

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"testing"

	"mugo/internal/gamelogic/entity/item"
)

//go:embed testdata/golden.json
var goldenJSON []byte

type goldenFile struct {
	Items       []goldenItem `json:"items"`
	Appearances []struct {
		Name  string `json:"name"`
		Bytes string `json:"bytes"`
	} `json:"appearances"`
	AppearancesExt []struct {
		Name  string `json:"name"`
		Bytes string `json:"bytes"`
	} `json:"appearances_ext"`
}

type goldenItem struct {
	Name  string `json:"name"`
	Bytes string `json:"bytes"`
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func goldenItems(t *testing.T) goldenFile {
	t.Helper()
	var g goldenFile
	if err := json.Unmarshal(goldenJSON, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// itemByName 返回 golden 中按名称构造的物品（与 C# Program.cs 输入保持一致）。
func itemByName(t *testing.T, name string) *item.Item {
	t.Helper()
	switch name {
	case "plain_sword":
		return &item.Item{Number: 0, Group: 0, Durability: 100}
	case "level9_skill_luck":
		return &item.Item{Number: 4, Group: 0, Level: 9, Durability: 255, HasSkill: true, Luck: true}
	case "option_level7":
		return &item.Item{Number: 6, Group: 2, Level: 3, Durability: 50, OptionLevel: 7}
	case "excellent_full":
		return &item.Item{Number: 8, Group: 4, Level: 11, Durability: 200, ExcellentBits: 0x3F, Luck: true}
	case "item512_fenrir_blue":
		return &item.Item{Number: 256, Group: 12, Durability: 1, FenrirBits: 0x02}
	case "ancient":
		return &item.Item{Number: 10, Group: 7, Level: 6, Durability: 80, AncientDiscriminator: 2, AncientBonusLevel: 3}
	case "guardian380":
		return &item.Item{Number: 12, Group: 5, Durability: 120, GuardianOption: true}
	case "harmony":
		return &item.Item{Number: 14, Group: 3, Level: 7, Durability: 60, HarmonyNumber: 9, HarmonyLevel: 12}
	case "sockets_3":
		// 4 孔：3 已镶嵌 + 1 空孔；第 5 槽无孔
		it := &item.Item{Number: 20, Group: 8, Level: 13, Durability: 250, SocketCount: 4, HasSocketBonus: true, SocketBonus: 20}
		it.SocketFilled[0] = true
		it.SocketSlots[0] = 1
		it.SocketFilled[1] = true
		it.SocketSlots[1] = 11
		it.SocketFilled[2] = true
		it.SocketSlots[2] = 21
		return it
	case "sockets_empty_bonus_absent":
		return &item.Item{Number: 21, Group: 8, Durability: 250, SocketCount: 5}
	default:
		t.Fatalf("未知物品 golden: %s", name)
		return nil
	}
}

func TestItemGoldenVectors(t *testing.T) {
	g := goldenItems(t)
	buf := make([]byte, item.Size)
	for _, gi := range g.Items {
		it := itemByName(t, gi.Name)
		EncodeItem(it, buf)
		want := mustHex(t, gi.Bytes)
		if string(buf) != string(want) {
			t.Fatalf("物品 %s 不一致\n got: %X\nwant: %X", gi.Name, buf, want)
		}
	}
}

func appearanceByName(t *testing.T, name string) *item.Appearance {
	t.Helper()
	switch name {
	case "smallaxe_anchor":
		return &item.Appearance{ClassNumber: 4, Equipment: []*item.Equip{
			{Number: 0, Group: 1}, // 左手
		}}
	case "naked_darkknight":
		return &item.Appearance{ClassNumber: 4}
	case "armored":
		return &item.Appearance{ClassNumber: 4, Equipment: []*item.Equip{
			{Number: 0, Group: 0, Level: 9},
			nil,
			{Number: 7, Group: 7, Level: 11, Excellent: true},
			{Number: 8, Group: 8, Level: 10, Ancient: true},
		}}
	case "wings_fenrir":
		eq := make([]*item.Equip, 9)
		eq[item.SlotWings] = &item.Equip{Number: 37, Group: 12, Level: 13}
		eq[item.SlotPet] = &item.Equip{Number: 37, Group: 13, BlackFenrir: true, GoldFenrir: true}
		return &item.Appearance{ClassNumber: 4, Equipment: eq}
	default:
		t.Fatalf("未知外观 golden: %s", name)
		return nil
	}
}

func TestAppearanceGoldenVectors(t *testing.T) {
	g := goldenItems(t)
	buf := make([]byte, AppearanceSize)
	for _, ga := range g.Appearances {
		a := appearanceByName(t, ga.Name)
		EncodeAppearance(a, buf)
		want := mustHex(t, ga.Bytes)
		if string(buf) != string(want) {
			t.Fatalf("外观 %s 不一致\n got: %X\nwant: %X", ga.Name, buf, want)
		}
	}
}

func appearanceExtByName(t *testing.T, name string) *item.Appearance {
	t.Helper()
	switch name {
	case "naked_darkknight_ext":
		return &item.Appearance{ClassNumber: 4}
	case "naked_elf_ext":
		return &item.Appearance{ClassNumber: 8}
	case "gamemaster_ext":
		return &item.Appearance{ClassNumber: 4, GameMaster: true}
	case "armored_ext":
		eq := make([]*item.Equip, 9)
		eq[item.SlotLeftHand] = &item.Equip{Number: 0, Group: 1, Level: 9, Excellent: true, Ancient: true}
		eq[item.SlotWings] = &item.Equip{Number: 37, Group: 12}
		eq[item.SlotPet] = &item.Equip{Number: 37, Group: 13, GoldFenrir: true}
		return &item.Appearance{ClassNumber: 4, Equipment: eq}
	default:
		t.Fatalf("未知扩展外观 golden: %s", name)
		return nil
	}
}

func TestAppearanceExtGoldenVectors(t *testing.T) {
	g := goldenItems(t)
	buf := make([]byte, AppearanceExtSize)
	for _, ga := range g.AppearancesExt {
		a := appearanceExtByName(t, ga.Name)
		EncodeAppearanceExt(a, buf)
		want := mustHex(t, ga.Bytes)
		if string(buf) != string(want) {
			t.Fatalf("扩展外观 %s 不一致\n got: %X\nwant: %X", ga.Name, buf, want)
		}
	}
}
