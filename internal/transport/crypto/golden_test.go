package crypto

// 与官方 NuGet 包 MUnique.OpenMU.Network 0.9.10 的 C# 管线做逐字节交叉验证。
// 权威向量由 tools/goldencrypto 产出（testdata/golden.json），本测试不依赖 dotnet。

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"testing"
)

//go:embed testdata/golden.json
var goldenJSON []byte

type cryptoGolden struct {
	PacketsVersion   string   `json:"packets_version"`
	S2CPlain         []string `json:"s2c_plain"`
	S2CSealed        []string `json:"s2c_sealed"`
	C2SPlain         []string `json:"c2s_plain"`
	C2SSealed        []string `json:"c2s_sealed"`
	PassThroughPlain string   `json:"pass_through_plain"`
	PassThroughOut   string   `json:"pass_through_out"`
}

func loadCryptoGolden(t *testing.T) cryptoGolden {
	t.Helper()
	var g cryptoGolden
	if err := json.Unmarshal(goldenJSON, &g); err != nil {
		t.Fatalf("解析 golden.json: %v", err)
	}
	if g.PacketsVersion != "0.9.10" {
		t.Fatalf("golden 版本 %s 与测试期望 0.9.10 不一致", g.PacketsVersion)
	}
	return g
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex 解码 %q: %v", s, err)
	}
	return b
}

// TestGoldenS2CSeal：Go 的 GS 发送加密必须与官方 C# PipelinedSimpleModulusEncryptor
// (DefaultServerKey) 输出逐字节一致，含连续计数器序列与 C4 双字节长度帧。
func TestGoldenS2CSeal(t *testing.T) {
	g := loadCryptoGolden(t)
	enc := NewEncryptor(DefaultServerEncryptKeys) // 计数器从 0 开始
	for i, plainHex := range g.S2CPlain {
		plain := mustHex(t, plainHex)
		want := mustHex(t, g.S2CSealed[i])
		got := enc.Seal(plain)
		if !bytes.Equal(got, want) {
			t.Fatalf("S2C 帧 %d 与 C# 不一致\n plain: %X\n  got: %X\n want: %X", i, plain, got, want)
		}
	}
}

// TestGoldenC2SOpen：官方 C# 客户端链（Xor32 -> SM DefaultClientKey）的密文，
// 必须被 Go GS 接收链（S6E3ServerCodec.Open）逐字节还原。
func TestGoldenC2SOpen(t *testing.T) {
	g := loadCryptoGolden(t)
	codec := NewS6E3ServerCodec()
	for i, plainHex := range g.C2SPlain {
		want := mustHex(t, plainHex)
		wire := mustHex(t, g.C2SSealed[i])
		got, err := codec.Open(wire)
		if err != nil {
			t.Fatalf("C2S 帧 %d 解密失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("C2S 帧 %d 与明文不一致\n  got: %X\n want: %X", i, got, want)
		}
	}
}

// TestGoldenPassThrough：C1 帧不进入加密链（管线行为在 Conn 层同样保证）。
func TestGoldenPassThrough(t *testing.T) {
	g := loadCryptoGolden(t)
	plain := mustHex(t, g.PassThroughPlain)
	want := mustHex(t, g.PassThroughOut)
	if !bytes.Equal(plain, want) {
		t.Fatalf("C# 透传向量自身不一致: %X vs %X", plain, want)
	}
	if IsEncrypted(0xC1) {
		t.Fatal("IsEncrypted(0xC1) 必须为 false，由 Conn 层据此透传")
	}
}
