package transport

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"mugo/internal/transport/crypto"
)

// clientSide 模拟 MuMain 客户端管线（ConnectInner 同构）：
// 发送 Xor32（无条件，C1/C2 也做）-> SM(128079-enc，仅 C3/C4)，接收 SM(73326-dec)。
type clientSide struct {
	conn   net.Conn
	reader *Reader
	smSeal *crypto.SimpleModulus
	smOpen *crypto.SimpleModulus
	xor32  *crypto.Xor32
}

func newClientSide(t *testing.T, c net.Conn) *clientSide {
	t.Helper()
	return &clientSide{
		conn:   c,
		reader: NewReader(c, 0),
		smSeal: crypto.NewEncryptor(crypto.DefaultClientEncryptKeys),
		smOpen: crypto.NewDecryptor(crypto.DefaultClientDecryptKeys),
		xor32:  crypto.NewXor32(crypto.Xor32Key),
	}
}

func (cl *clientSide) send(p []byte) error {
	buf := append([]byte(nil), p...) // Xor32 原地变换，避免污染调用方明文
	cl.xor32.Seal(buf)
	out := buf
	if crypto.IsEncrypted(buf[0]) {
		out = cl.smSeal.Seal(buf)
	}
	_, err := cl.conn.Write(out)
	return err
}

func (cl *clientSide) recv() ([]byte, error) {
	frame, err := cl.reader.ReadPacket()
	if err != nil {
		return nil, err
	}
	if crypto.IsEncrypted(frame[0]) {
		return cl.smOpen.Open(frame)
	}
	return frame, nil
}

func plainC3(code, sub byte, payload []byte) []byte {
	p := make([]byte, 4+len(payload))
	p[0] = 0xC3
	p[1] = byte(len(p))
	p[2] = code
	p[3] = sub
	copy(p[4:], payload)
	return p
}

// TestSecureConnLoopback 验证完整 S6E3 管线在 TCP 上双向互通，
// 包含连续计数器、多块帧、密文粘包/碎包由 Reader 切分、C1 帧只做 Xor32（不加密）。
func TestSecureConnLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// 客户端将发送的明文帧序列（长度覆盖块边界）。
	clientFrames := [][]byte{
		plainC3(0xF1, 0x00, []byte{0x11, 0x22}),             // 6B 明文
		plainC3(0xF1, 0x00, bytes.Repeat([]byte{0xAB}, 20)), // 多块
		{0xC1, 0x04, 0x00, 0x01},                            // C1：不加密，但仍过 Xor32
		plainC3(0x00, 0x00, nil),                            // 无 sub
	}
	serverReplies := []*bytes.Buffer{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		srv := NewSecureConn(raw, 0, crypto.NewS6E3ServerCodec())
		var got [][]byte
		serveErr := srv.Serve(func(c *Conn, packet []byte) {
			frame := append([]byte(nil), packet...)
			got = append(got, frame)
			if packet[0] == 0xC3 {
				// 原样加密回显，验证 S2C 方向
				reply := append([]byte(nil), packet...)
				reply[2] = 0x7E // 改成回应 code，区分方向
				// 重新计算长度（长度未变，无需）
				_ = c.Send(reply)
				serverReplies = append(serverReplies, bytes.NewBuffer(reply))
			}
			if len(got) == len(clientFrames) {
				_ = c.Close()
			}
		})
		if serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, net.ErrClosed) {
			t.Errorf("Serve: %v", serveErr)
		}
		for i, want := range clientFrames {
			if i >= len(got) || !bytes.Equal(got[i], want) {
				t.Errorf("服务端收到帧 %d 不匹配\n got: %X\nwant: %X", i, got[i], want)
				return
			}
		}
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	cl := newClientSide(t, raw)

	// 客户端发送：前两帧合并为一次 Write 制造密文粘包。
	var merged []byte
	for _, f := range clientFrames[:2] {
		merged = append(merged, cl.mustSealForTest(f)...)
	}
	if _, err := raw.Write(merged); err != nil {
		t.Fatal(err)
	}
	// 后两帧单独发
	for _, f := range clientFrames[2:] {
		if err := cl.send(f); err != nil {
			t.Fatal(err)
		}
	}

	// 接收 3 个 C3 回应（第 3 帧是 C1，服务端不回显）。
	for i := 0; i < 3; i++ {
		_ = raw.SetReadDeadline(time.Now().Add(3 * time.Second))
		got, err := cl.recv()
		if err != nil {
			t.Fatalf("接收回应 %d: %v", i, err)
		}
		if got[0] != 0xC3 || got[2] != 0x7E {
			t.Fatalf("回应 %d 帧头错误: %X", i, got)
		}
	}
	wg.Wait()
}

// mustSealForTest 用客户端管线加密一帧但不改写原切片（返回密文副本）。
func (cl *clientSide) mustSealForTest(plain []byte) []byte {
	buf := append([]byte(nil), plain...)
	cl.xor32.Seal(buf)
	return cl.smSeal.Seal(buf)
}
