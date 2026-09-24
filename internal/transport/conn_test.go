package transport

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConnLoopback 在本地 TCP 回环上验证：发送方连写两帧（粘包），
// 服务端读循环必须切出两个独立帧；再验证服务端→客户端的发送。
func TestConnLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	clientFrames := [][]byte{{0xC1, 0x04, 0xF4, 0x06}, {0xC1, 0x04, 0x00, 0x01}}
	reply := []byte{0xC1, 0x04, 0x00, 0x01}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		c := NewConn(raw, 0)
		var got [][]byte
		err = c.Serve(func(_ *Conn, packet []byte) {
			got = append(got, append([]byte(nil), packet...))
			if len(got) == len(clientFrames) {
				_ = c.Send(reply)
				_ = c.Close()
			}
		})
		// 对端关闭为 io.EOF；本侧主动 Close 为 net.ErrClosed，均属正常结束。
		if err != nil && err != io.EOF && !errors.Is(err, net.ErrClosed) {
			t.Errorf("Serve: %v", err)
		}
		if len(got) != len(clientFrames) {
			t.Errorf("收到帧数 %d，期望 %d", len(got), len(clientFrames))
			return
		}
		for i := range clientFrames {
			if !bytes.Equal(got[i], clientFrames[i]) {
				t.Errorf("帧 %d 不匹配: %X", i, got[i])
			}
		}
	}()

	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := NewConn(cc, 0)

	// 一次 Write 连写两帧，验证服务端粘包切分。
	var merged []byte
	merged = append(merged, clientFrames[0]...)
	merged = append(merged, clientFrames[1]...)
	if _, err := cc.Write(merged); err != nil {
		t.Fatal(err)
	}

	rd := NewReader(cc, 0)
	got, err := rd.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("回包不匹配: %X", got)
	}
	_ = client.Close()
	wg.Wait()
}

// TestConnSendSerializes 验证并发发送不会交错。
func TestConnSendSerializes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	frame := []byte{0xC1, 0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	const goroutines = 16

	gotCh := make(chan error, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			gotCh <- err
			return
		}
		c := NewConn(raw, 0)
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = c.Send(frame)
			}()
		}
		wg.Wait()
		time.Sleep(20 * time.Millisecond)
		_ = c.Close()
		gotCh <- nil
	}()

	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	rd := NewReader(cc, 0)
	for i := 0; i < goroutines; i++ {
		got, err := rd.ReadPacket()
		if err != nil {
			t.Fatalf("第 %d 帧: %v", i, err)
		}
		if !bytes.Equal(got, frame) {
			t.Fatalf("并发发送导致帧交错/损坏: %X", got)
		}
	}
	cc.Close()
	if err := <-gotCh; err != nil {
		t.Fatal(err)
	}
}

// TestServeRecoversPanic 验证处理器 panic 不击穿进程：Serve 把它恢复成
// 包装 ErrHandlerPanic 的错误（含 panic 值与堆栈）后返回，读循环随即终止。
func TestServeRecoversPanic(t *testing.T) {
	cases := []struct {
		name string
		want string
		fn   func()
	}{
		{"explicit", "boom", func() { panic("boom") }},
		{"nil_deref", "invalid memory address", func() {
			var p *struct{ x int }
			_ = p.x
		}},
		{"slice_oob", "index out of range", func() {
			s := make([]byte, 1)
			_ = s[2]
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			serveErr := make(chan error, 1)
			go func() {
				raw, err := ln.Accept()
				if err != nil {
					return
				}
				c := NewConn(raw, 0)
				serveErr <- c.Serve(func(_ *Conn, _ []byte) { tc.fn() })
			}()

			cc, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer cc.Close()
			if _, err := cc.Write([]byte{0xC1, 0x04, 0x00, 0x01}); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-serveErr:
				if !errors.Is(err, ErrHandlerPanic) {
					t.Fatalf("err=%v, want ErrHandlerPanic", err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err 缺少 panic 信息 %q: %v", tc.want, err)
				}
				if !strings.Contains(err.Error(), "goroutine") {
					t.Fatalf("err 缺少堆栈信息: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Serve 未在 panic 后返回")
			}
		})
	}
}
