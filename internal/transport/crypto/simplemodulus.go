package crypto

import (
	"encoding/binary"
	"errors"
)

// New 变体（S6E3）常量，对应 PipelinedSimpleModulusBase(Variant.New)。
const (
	decryptedBlockSize = 8
	encryptedBlockSize = 11
	valueCount         = 4 // EncryptionResult 长度

	blockSizeXorKey     = 0x3D
	blockCheckSumXorKey = 0xF8

	bitsPerByte  = 8
	bitsPerValue = bitsPerByte*2 + 2 // 18
)

var (
	// ErrBadBlockSize 表示解密块尾声明的明文长度超过 8。
	ErrBadBlockSize = errors.New("crypto: invalid encrypted block size")
	// ErrBadChecksum 表示块校验和不匹配。
	ErrBadChecksum = errors.New("crypto: invalid block checksum")
	// ErrBadCounter 表示首块计数器与期望值不一致。
	ErrBadCounter = errors.New("crypto: invalid packet counter")
	// ErrBadContentSize 表示密文内容长度不是 11 的整数倍。
	ErrBadContentSize = errors.New("crypto: encrypted content size must be a multiple of 11")
)

// counter 复刻 Network.Counter（min=0, max=255，到 255 回绕到 0）。
type counter struct {
	value byte
}

func (c *counter) count() byte { return c.value }

func (c *counter) increase() {
	if c.value == 255 {
		c.value = 0
	} else {
		c.value++
	}
}

func (c *counter) reset() { c.value = 0 }

// headerSize 返回帧头中长度字段之后的偏移（Code 所在下标）：
// C1/C3 为 2，C2/C4 为 3。与 OpenMU GetPacketHeaderSize 一致。
func headerSize(header byte) int {
	if header == 0xC2 || header == 0xC4 {
		return 3
	}
	return 2
}

// SimpleModulus 是一条有状态的 SimpleModulus(New) 变换。
// 加密与解密各自持有独立实例（计数器独立推进）。
type SimpleModulus struct {
	keys     Keys
	decrypt  bool // true=解密密钥集（含 Decrypt），false=加密密钥集
	counter  counter
	inputBuf [decryptedBlockSize]byte
}

// NewEncryptor 创建加密方向实例（keys 为 *EncryptionKeys）。
func NewEncryptor(keys Keys) *SimpleModulus {
	return &SimpleModulus{keys: keys}
}

// NewDecryptor 创建解密方向实例（keys 为 *DecryptionKeys）。
func NewDecryptor(keys Keys) *SimpleModulus {
	return &SimpleModulus{keys: keys, decrypt: true}
}

// Reset 将计数器归零（连接重置时使用）。
func (m *SimpleModulus) Reset() {
	m.counter.reset()
}

// Seal 加密一帧 C3/C4 明文。明文布局为标准帧 [type][len][code][content]。
// 输出为线上帧 [type][len][11 字节块...]（code 起进入加密块，首块前缀计数器）。
func (m *SimpleModulus) Seal(packet []byte) []byte {
	hdr := headerSize(packet[0])
	input := packet[hdr:]
	inputLen := len(input)

	// GetEncryptedSize：内容长度额外计入 1 字节计数器，按 8 字节分块。
	contentSize := inputLen + 1
	blockCount := contentSize / decryptedBlockSize
	if contentSize%decryptedBlockSize != 0 {
		blockCount++
	}
	out := make([]byte, hdr+blockCount*encryptedBlockSize)
	out[0] = packet[0]
	setFrameLength(out) // 与 C# result.SetPacketSize() 一致

	resultOffset := 0
	sourceOffset := 0

	// 首块：计数器前缀 + 最多 7 字节输入。
	m.inputBuf[0] = m.counter.count()
	firstBlockSize := decryptedBlockSize
	if inputLen+1 < decryptedBlockSize {
		firstBlockSize = inputLen + 1
		copy(m.inputBuf[1:], input)
		clearBytes(m.inputBuf[inputLen+1:])
	} else {
		copy(m.inputBuf[1:], input[:decryptedBlockSize-1])
	}
	m.encryptBlock(out[hdr:], firstBlockSize)
	resultOffset += encryptedBlockSize
	sourceOffset = decryptedBlockSize - 1

	// 其余块。
	for sourceOffset < inputLen {
		blockSize := decryptedBlockSize
		remaining := inputLen - sourceOffset
		if remaining < blockSize {
			blockSize = remaining
		}
		copy(m.inputBuf[:], input[sourceOffset:sourceOffset+blockSize])
		clearBytes(m.inputBuf[blockSize:])
		m.encryptBlock(out[hdr+resultOffset:], blockSize)
		sourceOffset += decryptedBlockSize
		resultOffset += encryptedBlockSize
	}

	m.counter.increase()
	return out
}

// Open 解密一帧线上 C3/C4 密文，返回标准明文帧。
func (m *SimpleModulus) Open(packet []byte) ([]byte, error) {
	hdr := headerSize(packet[0])
	content := packet[hdr:]
	if len(content)%encryptedBlockSize != 0 {
		return nil, ErrBadContentSize
	}
	blockCount := len(content) / encryptedBlockSize

	// 解密内容写入的平面区（每块 8 字节，首块首字节为计数器）。
	plain := make([]byte, blockCount*decryptedBlockSize)
	var encInput [encryptedBlockSize]byte
	totalBlockSize := 0
	for b := 0; b < blockCount; b++ {
		copy(encInput[:], content[b*encryptedBlockSize:(b+1)*encryptedBlockSize])
		blockSize, err := m.decryptBlock(encInput, plain[b*decryptedBlockSize:(b+1)*decryptedBlockSize])
		if err != nil {
			return nil, err
		}
		if b == 0 && plain[0] != m.counter.count() {
			return nil, ErrBadCounter
		}
		totalBlockSize += blockSize
	}
	m.counter.increase()

	// 最终帧长度 = 各块实际字节之和（含计数器）+ 帧头长度 - 计数器 1 字节。
	outLen := totalBlockSize + hdr - 1
	out := make([]byte, outLen)
	out[0] = packet[0]
	// plain[0] 是计数器，跳过；明文从 Code 开始，写到帧的 Code 偏移处。
	copy(out[hdr:], plain[1:1+totalBlockSize-1])
	setFrameLength(out)
	return out, nil
}

// encryptContent 复刻 EncryptContent：输入为 8 字节明文块（4 个 LE ushort）。
func (m *SimpleModulus) encryptContent(block [decryptedBlockSize]byte) [valueCount]uint32 {
	k := m.keys
	in0 := uint32(binary.LittleEndian.Uint16(block[0:]))
	in1 := uint32(binary.LittleEndian.Uint16(block[2:]))
	in2 := uint32(binary.LittleEndian.Uint16(block[4:]))
	in3 := uint32(binary.LittleEndian.Uint16(block[6:]))

	var r [valueCount]uint32
	r[0] = ((k.Xor[0] ^ in0) * k.Encrypt[0]) % k.Modulus[0]
	r[1] = ((k.Xor[1] ^ (in1 ^ (r[0] & 0xFFFF))) * k.Encrypt[1]) % k.Modulus[1]
	r[2] = ((k.Xor[2] ^ (in2 ^ (r[1] & 0xFFFF))) * k.Encrypt[2]) % k.Modulus[2]
	r[3] = ((k.Xor[3] ^ (in3 ^ (r[2] & 0xFFFF))) * k.Encrypt[3]) % k.Modulus[3]

	for i := 0; i < valueCount-1; i++ {
		r[i] = r[i] ^ k.Xor[i] ^ (r[i+1] & 0xFFFF)
	}
	return r
}

// encryptBlock 复刻 EncryptBlock + WriteResultToTarget + EncryptFinalBlockByte。
func (m *SimpleModulus) encryptBlock(output []byte, blockSize int) {
	clearBytes(output)
	r := m.encryptContent(m.inputBuf)
	for i := 0; i < valueCount; i++ {
		writeResultToTarget(output, i, r[i])
	}

	// EncryptFinalBlockByte：校验和只遍历实际字节（未用部分已清零，效果等价）。
	checksum := byte(blockCheckSumXorKey)
	for i := 0; i < blockSize; i++ {
		checksum ^= m.inputBuf[i]
	}
	size := byte(blockSize) ^ blockSizeXorKey
	size ^= checksum
	output[encryptedBlockSize-2] = size
	output[encryptedBlockSize-1] = checksum
}

// decryptContent 复刻 DecryptContent：输出为 8 字节明文块。
func (m *SimpleModulus) decryptContent(r [valueCount]uint32, output []byte) {
	k := m.keys
	// 逆序解除第二轮 XOR 链。
	for i := valueCount - 1; i > 0; i-- {
		r[i-1] = r[i-1] ^ k.Xor[i-1] ^ (r[i] & 0xFFFF)
	}

	var out16 [valueCount]uint16
	for i := 0; i < valueCount; i++ {
		result := k.Xor[i] ^ ((r[i] * k.Decrypt[i]) % k.Modulus[i])
		if i > 0 {
			result ^= r[i-1] & 0xFFFF
		}
		out16[i] = uint16(result)
	}
	for i := 0; i < valueCount; i++ {
		binary.LittleEndian.PutUint16(output[i*2:], out16[i])
	}
}

// decryptBlock 复刻 DecryptBlock + ReadInputBuffer + DecodeFinal。
// 返回该块的实际明文长度（含首块计数器字节）。
func (m *SimpleModulus) decryptBlock(input [encryptedBlockSize]byte, output []byte) (int, error) {
	var r [valueCount]uint32
	for i := 0; i < valueCount; i++ {
		r[i] = readInputBuffer(input, i)
	}
	m.decryptContent(r, output)

	// DecodeFinal。
	blockSize := input[encryptedBlockSize-2] ^ input[encryptedBlockSize-1] ^ blockSizeXorKey
	if blockSize > decryptedBlockSize {
		return 0, ErrBadBlockSize
	}
	checksum := byte(blockCheckSumXorKey)
	for i := 0; i < decryptedBlockSize; i++ {
		checksum ^= output[i]
	}
	if input[encryptedBlockSize-1] != checksum {
		return 0, ErrBadChecksum
	}
	return int(blockSize), nil
}

// 以下位布局函数逐行翻译自 PipelinedSimpleModulusBase 的
// GetByteOffset/GetBitOffset/GetFirstBitMask/GetRemainderBitMask
// 与 Encryptor.WriteResultToTarget / Decryptor.ReadInputBuffer。

func getBitIndex(resultIndex int) int     { return resultIndex * bitsPerValue }
func getByteOffset(resultIndex int) int   { return getBitIndex(resultIndex) / bitsPerByte }
func getBitOffset(resultIndex int) int    { return getBitIndex(resultIndex) % bitsPerByte }
func getFirstBitMask(resultIndex int) int { return 0xFF >> getBitOffset(resultIndex) }

func getRemainderBitMask(resultIndex int) int {
	return ((0xFF << (6 - getBitOffset(resultIndex))) & 0xFF) - ((0xFF << (8 - getBitOffset(resultIndex))) & 0xFF)
}

func writeResultToTarget(target []byte, resultIndex int, result uint32) {
	byteOffset := getByteOffset(resultIndex)
	bitOffset := getBitOffset(resultIndex)
	firstMask := byte(getFirstBitMask(resultIndex))
	swapped := reverseEndian32(result)

	target[byteOffset] |= byte((swapped >> (24 + bitOffset)) & uint32(firstMask))
	byteOffset++
	target[byteOffset] = byte(swapped >> (16 + bitOffset))
	byteOffset++
	target[byteOffset] = byte((swapped >> (8 + bitOffset)) & uint32(0xFF<<(8-bitOffset)))
	remainderMask := byte(getRemainderBitMask(resultIndex))
	remainder := (result >> 16) << (6 - bitOffset)
	target[byteOffset] |= byte(remainder & uint32(remainderMask))
}

func readInputBuffer(input [encryptedBlockSize]byte, resultIndex int) uint32 {
	byteOffset := getByteOffset(resultIndex)
	bitOffset := getBitOffset(resultIndex)
	firstMask := byte(getFirstBitMask(resultIndex))

	// 注意：所有移位必须在 uint32 宽度进行——C# 中 byte 参与移位会提升为 int，
	// Go 中 byte 移位按 byte 宽度会直接溢出为 0。掩码也保留 C# 的 int 级形式
	// （如 0xFF<<8 = 0x1FF00），先与零扩展的输入相与再移位。
	var result uint32
	result += uint32(input[byteOffset]&firstMask) << (24 + bitOffset)
	byteOffset++
	result += uint32(input[byteOffset]) << (16 + bitOffset)
	byteOffset++
	result += (uint32(input[byteOffset]) & uint32(0xFF<<(8-bitOffset))) << (8 + bitOffset)

	result = reverseEndian32(result)
	remainderMask := byte(getRemainderBitMask(resultIndex))
	remainder := input[byteOffset] & remainderMask
	result += (uint32(remainder) << 16) >> (6 - bitOffset)
	return result
}

func reverseEndian32(v uint32) uint32 {
	return (v&0xFF)<<24 |
		(v&0xFF00)<<8 |
		(v&0xFF0000)>>8 |
		(v>>24)&0xFF
}

// setFrameLength 按帧类型写回长度字段（C3 单字节 / C4 大端双字节）。
func setFrameLength(buf []byte) {
	if buf[0] == 0xC4 {
		binary.BigEndian.PutUint16(buf[1:3], uint16(len(buf)))
	} else {
		buf[1] = byte(len(buf))
	}
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
