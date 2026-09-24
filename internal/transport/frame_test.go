package transport

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestPacketSize(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
		want int
		err  error
	}{
		{"C1 单字节长度", []byte{0xC1, 0x04, 0x00}, 4, nil},
		{"C3 单字节长度", []byte{0xC3, 0x0C, 0x0E}, 12, nil},
		{"C2 双字节大端长度", []byte{0xC2, 0x01, 0x05, 0xAE}, 261, nil},
		{"C4 双字节大端长度", []byte{0xC4, 0xFF, 0xFF, 0x00}, 0xFFFF, nil},
		{"非法首字节", []byte{0x00, 0x04, 0x00}, 0, ErrInvalidHeader},
		{"C1 长度为 0", []byte{0xC1, 0x00, 0x00}, 0, ErrInvalidHeader},
		{"不足 3 字节", []byte{0xC1, 0x04}, 0, ErrInvalidHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PacketSize(tc.buf)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("size = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSetPacketSize(t *testing.T) {
	c1 := []byte{0xC1, 0x00, 0x00, 0xAA}
	if err := SetPacketSize(c1); err != nil || c1[1] != 4 {
		t.Fatalf("C1 SetPacketSize 错误: %v %02X", err, c1[1])
	}
	c2 := make([]byte, 261)
	c2[0] = 0xC2
	if err := SetPacketSize(c2); err != nil || c2[1] != 0x01 || c2[2] != 0x05 {
		t.Fatalf("C2 SetPacketSize 错误: %v %02X%02X", err, c2[1], c2[2])
	}
	tooBig := make([]byte, 256)
	tooBig[0] = 0xC1
	if err := SetPacketSize(tooBig); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("C1 超长帧应报错，得到 %v", err)
	}
}

// chunkReader 按指定小块大小返回数据，模拟 TCP 半包。
type chunkReader struct {
	data   []byte
	sizes  []int
	pos    int
	si     int
	closed bool
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.sizes[r.si%len(r.sizes)]
	r.si++
	if n > len(r.data)-r.pos {
		n = len(r.data) - r.pos
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func TestReaderPartialAndCoalesced(t *testing.T) {
	frames := [][]byte{
		{0xC1, 0x04, 0x00, 0x01}, // Hello
		{0xC1, 0x04, 0xF4, 0x06}, // ServerListRequest
		append([]byte{0xC2, 0x00, 0x09, 0xF4, 0x06}, make([]byte, 4)...), // C2 负载
	}
	var stream []byte
	for _, f := range frames {
		stream = append(stream, f...)
	}

	// 用 1、2、3、5 字节的极小读块制造跨块帧与粘包。
	rd := NewReader(&chunkReader{data: stream, sizes: []int{1, 2, 3, 5}}, 0)
	for i, want := range frames {
		got, err := rd.ReadPacket()
		if err != nil {
			t.Fatalf("第 %d 帧读取失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("第 %d 帧不匹配\n got: %X\nwant: %X", i, got, want)
		}
	}
	if _, err := rd.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatalf("流结束应返回 EOF，得到 %v", err)
	}
}

func TestReaderInvalidHeader(t *testing.T) {
	rd := NewReader(bytes.NewReader([]byte{0x00, 0x04, 0x00, 0x01}), 0)
	if _, err := rd.ReadPacket(); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("非法帧头应返回 ErrInvalidHeader，得到 %v", err)
	}
}

func TestReaderOversized(t *testing.T) {
	// 声明长度 10，但读取器上限 4。
	rd := NewReader(bytes.NewReader([]byte{0xC1, 0x0A, 0x00, 0x01, 0, 0, 0, 0, 0, 0}), 4)
	if _, err := rd.ReadPacket(); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("超限帧应返回 ErrPacketTooLarge，得到 %v", err)
	}
}

func TestIsEncryptedAndCodeIndex(t *testing.T) {
	if IsEncrypted(0xC1) || IsEncrypted(0xC2) || !IsEncrypted(0xC3) || !IsEncrypted(0xC4) {
		t.Fatal("IsEncrypted 判定错误")
	}
	if CodeIndex(0xC1) != 2 || CodeIndex(0xC3) != 2 || CodeIndex(0xC2) != 3 || CodeIndex(0xC4) != 3 {
		t.Fatal("CodeIndex 判定错误")
	}
}
