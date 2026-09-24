// Package transport 实现 MU Online 线上帧（C1-C4）的分帧与连接承载。
// 帧布局（与 OpenMU Network.ArrayExtensions.GetPacketSize 一致）：
//
//	C1: [0xC1][length:1][code][...]
//	C2: [0xC2][lengthH][lengthL][code][...]   长度为大端双字节
//	C3: [0xC3][length:1][code][...]           SimpleModulus 加密
//	C4: [0xC4][lengthH][lengthL][code][...]   SimpleModulus 加密
//
// SubCode 是包层概念：Code 后再跟一个字节，不影响分帧。
package transport

import (
	"encoding/binary"
	"errors"
)

// 帧类型字节。
const (
	TypeC1 byte = 0xC1
	TypeC2 byte = 0xC2
	TypeC3 byte = 0xC3
	TypeC4 byte = 0xC4
)

// ErrInvalidHeader 表示首字节不是 C1-C4，或长度字段为 0。
var ErrInvalidHeader = errors.New("transport: invalid packet header")

// ErrPacketTooLarge 表示帧声明长度超过 Reader 允许的上限。
var ErrPacketTooLarge = errors.New("transport: packet larger than maximum size")

// ErrHandlerPanic 表示单帧解密或处理过程中发生 panic（已恢复），
// 该连接随即终止以防状态损坏扩散。
var ErrHandlerPanic = errors.New("transport: panic while processing packet")

// minHeader 是判定帧长度所需的最少字节数。
const minHeader = 3

// MaxPacketSize 是 C2/C4 双字节长度字段可表达的理论上限。
const MaxPacketSize = 0xFFFF

// PacketSize 按首字节计算整帧长度（含头部）。
// 调用方需保证 len(buf) >= 3；不足时返回 ErrInvalidHeader。
// 语义对齐 OpenMU ArrayExtensions.GetPacketSize。
func PacketSize(buf []byte) (int, error) {
	if len(buf) < minHeader {
		return 0, ErrInvalidHeader
	}
	switch buf[0] {
	case TypeC1, TypeC3:
		if buf[1] == 0 {
			return 0, ErrInvalidHeader
		}
		return int(buf[1]), nil
	case TypeC2, TypeC4:
		n := int(binary.BigEndian.Uint16(buf[1:3]))
		if n == 0 {
			return 0, ErrInvalidHeader
		}
		return n, nil
	default:
		return 0, ErrInvalidHeader
	}
}

// CodeIndex 返回 Code 字节在帧中的下标：C1/C3 为 2，C2/C4 为 3。
func CodeIndex(header byte) int {
	if header == TypeC2 || header == TypeC4 {
		return 3
	}
	return 2
}

// IsEncrypted 报告帧是否走加密链（C3/C4）。
// 传输层只做分帧；加解密在后续里程碑加入，且必须对 C1/C2 明文透传。
func IsEncrypted(header byte) bool {
	return header == TypeC3 || header == TypeC4
}

// SetPacketSize 按帧类型把整帧长度写回长度字段。
// 要求 C1/C3 帧长度 <= 255；超长帧必须使用 C2/C4。
func SetPacketSize(buf []byte) error {
	if len(buf) < minHeader {
		return ErrInvalidHeader
	}
	n := len(buf)
	switch buf[0] {
	case TypeC1, TypeC3:
		if n > 0xFF {
			return ErrPacketTooLarge
		}
		buf[1] = byte(n)
	case TypeC2, TypeC4:
		if n > MaxPacketSize {
			return ErrPacketTooLarge
		}
		binary.BigEndian.PutUint16(buf[1:3], uint16(n))
	default:
		return ErrInvalidHeader
	}
	return nil
}
