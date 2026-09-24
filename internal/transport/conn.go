package transport

import (
	"fmt"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"mugo/internal/transport/crypto"
)

// DefaultSendTimeout 是单次 Send 写操作的默认超时：
// 防止对端零窗口读取时写操作无限阻塞、长期占住该连接的发送串行锁。
const DefaultSendTimeout = 10 * time.Second

// PacketHandler 处理一帧已切分并解密完成的完整封包。
// packet 为独立拷贝，可在回调返回后继续持有；回调在该连接的读 goroutine 上串行执行。
type PacketHandler func(conn *Conn, packet []byte)

// Conn 封装一条 TCP 连接的分帧读取与串行化发送。
// codec 为 nil 时整条连接明文（ConnectServer）；非 nil 时 C3/C4 进入 SimpleModulus+Xor32，
// C1/C2 不做 SimpleModulus 但仍过 Xor32（与 OpenMU PipelinedSimpleModulus{Encryptor,Decryptor}
// 的 HeaderBuffer[0] < 0xC3 直通分支 + PipelinedXor32* 的无条件分支一致）。
type Conn struct {
	nc     net.Conn
	reader *Reader
	codec  crypto.Codec

	sendMu sync.Mutex
	closed sync.Once

	sendTimeout time.Duration
}

// NewConn 包装已有 net.Conn（明文连接）。maxPacketSize <= 0 时使用理论上限。
func NewConn(nc net.Conn, maxPacketSize int) *Conn {
	return &Conn{
		nc:          nc,
		reader:      NewReader(nc, maxPacketSize),
		sendTimeout: DefaultSendTimeout,
	}
}

// NewSecureConn 包装一条带编解码管线的连接（GS 加密端口）。
func NewSecureConn(nc net.Conn, maxPacketSize int, codec crypto.Codec) *Conn {
	return &Conn{
		nc:          nc,
		reader:      NewReader(nc, maxPacketSize),
		codec:       codec,
		sendTimeout: DefaultSendTimeout,
	}
}

// SetSendTimeout 设置单次 Send 写超时；<=0 表示不超时。
func (c *Conn) SetSendTimeout(d time.Duration) {
	c.sendMu.Lock()
	c.sendTimeout = d
	c.sendMu.Unlock()
}

// Serve 运行读循环，逐帧解密（如需要）后调用 handler，直到连接关闭或分帧/解密失败。
// 返回值为导致循环结束的错误（对端正常关闭时为 io.EOF）。
//
// C3/C4 走 SimpleModulus + Xor32；C1/C2 不做 SimpleModulus，但 Xor32 照做
// （对照 OpenMU PipelinedDecryptor：Xor32 对任何帧都执行，只有 SimpleModulus 分帧类型）。
func (c *Conn) Serve(handler PacketHandler) error {
	for {
		packet, err := c.reader.ReadPacket()
		if err != nil {
			return err
		}
		if err := c.processFrame(handler, packet); err != nil {
			return err
		}
	}
}

// processFrame 完成单帧的解密（如需要）与分发，并用 defer recover 兜底：
// 处理器或解密实现 panic 时不允许击穿 Serve、拖垮整个进程，而是转为
// ErrHandlerPanic（带堆栈）返回；调用方随即关闭这条连接、清理会话。
func (c *Conn) processFrame(handler PacketHandler, packet []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrHandlerPanic, r, debug.Stack())
		}
	}()
	if c.codec != nil {
		if crypto.IsEncrypted(packet[0]) {
			var err error
			packet, err = c.codec.Open(packet)
			if err != nil {
				return err
			}
		} else {
			packet = c.codec.OpenPlain(packet)
		}
	}
	handler(c, packet)
	return nil
}

// Send 写入一帧。C3/C4 在加密连接上会先经过 Seal；其余帧透传。
// 多 goroutine 并发调用安全（内部加锁串行化，计数器顺序因此确定）。
// 调用方需保证帧长度字段正确（使用生成视图构造器即可）。
func (c *Conn) Send(packet []byte) error {
	if len(packet) < minHeader {
		return ErrInvalidHeader
	}
	out := packet
	if c.codec != nil && crypto.IsEncrypted(packet[0]) {
		out = c.codec.Seal(packet)
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendTimeout > 0 {
		_ = c.nc.SetWriteDeadline(time.Now().Add(c.sendTimeout))
		defer func() { _ = c.nc.SetWriteDeadline(time.Time{}) }()
	}
	for len(out) > 0 {
		n, err := c.nc.Write(out)
		if err != nil {
			return err
		}
		out = out[n:]
	}
	return nil
}

// Close 关闭底层连接，使 Serve 的读循环退出。可重复调用。
func (c *Conn) Close() error {
	var err error
	c.closed.Do(func() {
		err = c.nc.Close()
	})
	return err
}

// CloseWrite 半关闭写方向（TCP shutdown SD_SEND）：已写入发送缓冲区的数据
// 仍会正常发出，排空后向对端发送 FIN。相比直接 Close，可避免接收缓冲区存在
// 未读数据时协议栈直接发 RST、导致对端丢失刚发出的数据包（对照原版
// Connection 先 Complete 管道、写循环排空后再关闭 socket 的语义）。
// 非 TCP 连接为无操作。
func (c *Conn) CloseWrite() error {
	if tc, ok := c.nc.(*net.TCPConn); ok {
		return tc.CloseWrite()
	}
	return nil
}

// RemoteAddr 返回对端地址。
func (c *Conn) RemoteAddr() net.Addr {
	return c.nc.RemoteAddr()
}
