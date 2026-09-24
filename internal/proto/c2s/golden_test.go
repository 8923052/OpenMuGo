package c2s

// 与官方 NuGet 包 MUnique.OpenMU.Network.Packets 0.9.10 的 C# 生成物做逐字节差分。
// 权威向量由 tools/goldengen c2s 模式产出，提交在 testdata/golden。

import (
	"bytes"
	"embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/golden/golden.json testdata/golden/*.hex
var goldenFS embed.FS

type goldenMeta struct {
	File   string `json:"file"`
	Packet string `json:"packet"`
	Note   string `json:"note"`
}

func loadGolden(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := goldenFS.ReadFile("testdata/golden/" + file)
	if err != nil {
		t.Fatalf("读取 golden 向量 %s: %v", file, err)
	}
	got, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("hex 解码 %s: %v", file, err)
	}
	return got
}

func buildByName(t *testing.T, packet string) []byte {
	t.Helper()
	switch packet {
	case "Ping":
		p := NewPing()
		p.SetTickCount(0x01020304)
		p.SetAttackSpeed(0x1122)
		return p.Bytes()

	case "PublicChatMessage":
		p := NewPublicChatMessage(PublicChatMessageRequiredSize(2))
		p.SetCharacter("Alice")
		p.SetMessage("Hi")
		return p.Bytes()

	default:
		t.Fatalf("未实现的 golden 构造分支: %s", packet)
		return nil
	}
}

func TestGoldenVectors(t *testing.T) {
	manifestRaw, err := goldenFS.ReadFile("testdata/golden/golden.json")
	if err != nil {
		t.Fatalf("读取 golden.json: %v", err)
	}
	var metas []goldenMeta
	if err := json.Unmarshal(manifestRaw, &metas); err != nil {
		t.Fatalf("解析 golden.json: %v", err)
	}
	for _, m := range metas {
		t.Run(m.Packet, func(t *testing.T) {
			want := loadGolden(t, m.File)
			got := buildByName(t, m.Packet)
			if !bytes.Equal(got, want) {
				t.Fatalf("与 C# 权威字节不一致\n got: %X\nwant: %X", got, want)
			}
			if info, ok := LookupPacket(want); !ok || info.Name != m.Packet {
				t.Fatalf("路由解析失败: ok=%v info=%+v", ok, info)
			}
		})
	}

	// 抽读 Ping 字段
	ping := AsPing(loadGolden(t, "c2s_01_ping.hex"))
	if ping.TickCount() != 0x01020304 || ping.AttackSpeed() != 0x1122 {
		t.Fatalf("Ping 回读失败")
	}
}
