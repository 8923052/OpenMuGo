package crypto

import (
	"bytes"
	"errors"
	"testing"
)

// buildPlainFrame 构造一帧 C3 + SubCode 明文 [C3][len][F1][00][payload]。
func buildPlainFrame(payload []byte) []byte {
	p := make([]byte, 4+len(payload))
	p[0] = 0xC3
	p[1] = byte(len(p))
	p[2] = 0xF1
	p[3] = 0x00
	copy(p[4:], payload)
	return p
}

func TestSimpleModulusRoundTrip(t *testing.T) {
	enc := NewEncryptor(DefaultServerEncryptKeys)

	// 注意：加密 73326 与解密 128079 不是一对密钥（方向不同），
	// 自反测试需要用数学上互逆的密钥对——这里用同一组 enc/dec 不成立，
	// 因此自反性通过"加密侧再配对应解密侧"验证：
	// 73326-enc 的对应解密是 73326-dec（DefaultClientKey 解密侧）。
	clientDec := newClientDecryptor()

	sizes := []int{0, 1, 6, 7, 8, 9, 15, 16, 17, 100}
	for _, sz := range sizes {
		payload := make([]byte, sz)
		for i := range payload {
			payload[i] = byte(i*7 + 1)
		}
		plain := buildPlainFrame(payload)
		wire := enc.Seal(plain)
		got, err := clientDec.Open(wire)
		if err != nil {
			t.Fatalf("size=%d 解密失败: %v", sz, err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("size=%d 往返不一致\n got: %X\nwant: %X", sz, got, plain)
		}
	}
}

func TestCounterSequence(t *testing.T) {
	enc := NewEncryptor(DefaultServerEncryptKeys)
	clientDec := newClientDecryptor()

	// 连续 257 帧：计数器 0,1,...,255,0。
	for seq := 0; seq < 257; seq++ {
		payload := []byte{byte(seq), byte(seq >> 8), 0xAA}
		wire := enc.Seal(buildPlainFrame(payload))
		got, err := clientDec.Open(wire)
		if err != nil {
			t.Fatalf("seq=%d: %v", seq, err)
		}
		want := buildPlainFrame(payload)
		if !bytes.Equal(got, want) {
			t.Fatalf("seq=%d 不一致", seq)
		}
	}
}

func TestClientToServerChain(t *testing.T) {
	// 模拟客户端发送链：Xor32 -> SM(128079-enc)；服务器接收链：SM(128079-dec) -> Xor32。
	clientEnc := newClientEncryptor()
	server := NewS6E3ServerCodec()

	frames := [][]byte{
		buildPlainFrame([]byte{1, 2, 3}),
		buildPlainFrame(bytes.Repeat([]byte{0x5A}, 20)),
		buildPlainFrame(nil),
	}
	for i, f := range frames {
		want := append([]byte(nil), f...) // Xor32 会原地修改，先留存原始明文
		clientEnc.xor32.Seal(f)
		wire := clientEnc.sm.Seal(f)
		got, err := server.Open(wire)
		if err != nil {
			t.Fatalf("帧 %d C2S 链失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("帧 %d C2S 链不一致\n got: %X\nwant: %X", i, got, want)
		}
	}
}

func TestDecryptRejectsBadChecksum(t *testing.T) {
	enc := NewEncryptor(DefaultServerEncryptKeys)
	clientDec := newClientDecryptor()
	wire := enc.Seal(buildPlainFrame([]byte{1, 2, 3}))
	wire[3] ^= 0xFF // 破坏首块密文数据字节（避开尾 2 字节的 size/checksum）
	if _, err := clientDec.Open(wire); !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("期望 ErrBadChecksum，得到 %v", err)
	}
}

func TestDecryptRejectsBadCounter(t *testing.T) {
	enc := NewEncryptor(DefaultServerEncryptKeys)
	clientDec := newClientDecryptor()
	wire := enc.Seal(buildPlainFrame([]byte{1}))
	clientDec.counter.increase() // 接收端计数器错位
	if _, err := clientDec.Open(wire); !errors.Is(err, ErrBadCounter) {
		t.Fatalf("期望 ErrBadCounter，得到 %v", err)
	}
}

func TestBadContentSize(t *testing.T) {
	dec := NewDecryptor(DefaultServerDecryptKeys)
	bad := []byte{0xC3, 0x08, 0x00, 0x00, 0x00, 0x00} // 内容 4 字节，非 11 倍数
	if _, err := dec.Open(bad); !errors.Is(err, ErrBadContentSize) {
		t.Fatalf("期望 ErrBadContentSize，得到 %v", err)
	}
}

func TestXor32RoundTrip(t *testing.T) {
	x := NewXor32(Xor32Key)
	orig := []byte{0xC3, 0x0C, 0x0E, 0x00, 1, 2, 3, 4, 5, 6, 7, 8}
	cp := append([]byte(nil), orig...)
	x.Seal(cp)
	if bytes.Equal(cp, orig) {
		t.Fatal("Xor32 未改变数据")
	}
	x.Open(cp)
	if !bytes.Equal(cp, orig) {
		t.Fatalf("Xor32 往返失败: %X", cp)
	}
}
