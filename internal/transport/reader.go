package transport

import (
	"io"
)

// Reader 从底层字节流中切分出完整 MU 帧。
// 支持半包（一帧跨多次 Read）与粘包（一次 Read 含多帧）。
type Reader struct {
	r       io.Reader
	pending []byte // 已收到但尚未成帧（或尚未消费完）的字节
	maxSize int    // 单帧允许的最大长度
}

// NewReader 创建分帧读取器。maxSize <= 0 或超过理论上限时使用 MaxPacketSize。
func NewReader(r io.Reader, maxSize int) *Reader {
	if maxSize <= 0 || maxSize > MaxPacketSize {
		maxSize = MaxPacketSize
	}
	return &Reader{r: r, maxSize: maxSize}
}

// ReadPacket 阻塞读取并返回下一帧的独立拷贝。
// 返回的切片归调用方所有，可长期持有；底层流结束时返回 io.EOF。
func (rd *Reader) ReadPacket() ([]byte, error) {
	for {
		packet, rest, err := rd.tryExtract()
		if err == nil {
			rd.pending = rest
			return packet, nil
		}
		if err != errNeedMore {
			return nil, err
		}

		// 容量不足时增长；正常情况下一帧一次分配。
		if cap(rd.pending)-len(rd.pending) < 512 {
			grown := make([]byte, len(rd.pending), max(cap(rd.pending)*2, 512))
			copy(grown, rd.pending)
			rd.pending = grown
		}
		n, readErr := rd.r.Read(rd.pending[len(rd.pending):cap(rd.pending)])
		rd.pending = rd.pending[:len(rd.pending)+n]
		if readErr != nil {
			// 流结束且缓冲无完整帧：直接上抛（含 io.EOF），半包没有帧边界，无法恢复。
			return nil, readErr
		}
	}
}

// errNeedMore 是内部信号：当前字节不足一帧，需继续从底层流读取。
var errNeedMore = io.ErrShortBuffer

// tryExtract 尝试从 pending 头部切出一帧。
func (rd *Reader) tryExtract() (packet []byte, rest []byte, err error) {
	if len(rd.pending) < minHeader {
		return nil, rd.pending, errNeedMore
	}
	size, err := PacketSize(rd.pending)
	if err != nil {
		return nil, rd.pending, err
	}
	if size > rd.maxSize {
		return nil, rd.pending, ErrPacketTooLarge
	}
	if len(rd.pending) < size {
		return nil, rd.pending, errNeedMore
	}
	out := make([]byte, size)
	copy(out, rd.pending[:size])
	return out, rd.pending[size:], nil
}
