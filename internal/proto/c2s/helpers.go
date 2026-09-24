// 手写运行时辅助，供 muprotogen 生成物调用；升级协议版本时不会被覆盖。
// 语义对齐 MUnique.OpenMU.Network.Packets.ByteSpanExtensions。

//go:generate go run ../../../cmd/muprotogen -config ../../../protocol.gen.json

package c2s

import (
	"bytes"
	"encoding/binary"
	"math"
)

// byteBits 读取字节 b 的 [shift, shift+bits) 位。
func byteBits(b byte, bits, shift int) byte {
	mask := uint16(1)<<uint(bits) - 1
	return byte((uint16(b) >> uint(shift)) & mask)
}

// setByteBits 仅修改字节 *dst 的 [shift, shift+bits) 位，其余位保持不变。
func setByteBits(dst *byte, value byte, bits, shift int) {
	mask := uint16(1)<<uint(bits) - 1
	bitMask := byte(mask << uint(shift))
	*dst &^= bitMask
	*dst |= byte((uint16(value) & mask) << uint(shift))
}

func getU16LE(b []byte) uint16    { return binary.LittleEndian.Uint16(b) }
func setU16LE(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func getU16BE(b []byte) uint16    { return binary.BigEndian.Uint16(b) }
func setU16BE(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }

func getU32LE(b []byte) uint32    { return binary.LittleEndian.Uint32(b) }
func setU32LE(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func getU32BE(b []byte) uint32    { return binary.BigEndian.Uint32(b) }
func setU32BE(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }

func getU64LE(b []byte) uint64    { return binary.LittleEndian.Uint64(b) }
func setU64LE(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
func getU64BE(b []byte) uint64    { return binary.BigEndian.Uint64(b) }
func setU64BE(b []byte, v uint64) { binary.BigEndian.PutUint64(b, v) }

func getF32(b []byte) float32    { return math.Float32frombits(getU32LE(b)) }
func setF32(b []byte, v float32) { setU32LE(b, math.Float32bits(v)) }
func getF64(b []byte) float64    { return math.Float64frombits(getU64LE(b)) }
func setF64(b []byte, v float64) { setU64LE(b, math.Float64bits(v)) }

// cstring 在首个 null 处截断，对应 ExtractString 的 null 终止语义。
func cstring(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

// writeCString 先清零再写入 UTF-8 字节（超出定长字段的部分丢弃），对应 WriteString。
func writeCString(dst []byte, s string) {
	for i := range dst {
		dst[i] = 0
	}
	copy(dst, s)
}

// setHeader 写入 C1-C4 包头：C1/C3 长度为单字节，C2/C4 长度为大端双字节。
// subCode 传 -1 表示该包无 SubCode。
func setHeader(dst []byte, header, code byte, subCode int) {
	dst[0] = header
	if header == 0xC2 || header == 0xC4 {
		binary.BigEndian.PutUint16(dst[1:3], uint16(len(dst)))
		dst[3] = code
		if subCode >= 0 {
			dst[4] = byte(subCode)
		}
		return
	}
	dst[1] = byte(len(dst))
	dst[2] = code
	if subCode >= 0 {
		dst[3] = byte(subCode)
	}
}
