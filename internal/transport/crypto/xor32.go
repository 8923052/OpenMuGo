package crypto

// Xor32 实现 MU 的 32 字节链式 XOR（内层加密，仅 C2S 方向使用）。
// 无状态，可作为管线一环复用；逐行对照 PipelinedXor32Encryptor/Decryptor。
type Xor32 struct {
	key [32]byte
}

// NewXor32 使用指定 32 字节密钥创建实例。
func NewXor32(key [32]byte) *Xor32 {
	return &Xor32{key: key}
}

// Seal 为客户端→服务器方向的加密：从帧尾向"前"依赖。
// 作用范围 i = headerSize+1 .. len-1（Code 字节不参与）。
func (x *Xor32) Seal(packet []byte) {
	hs := headerSize(packet[0])
	for i := hs + 1; i < len(packet); i++ {
		packet[i] = packet[i] ^ packet[i-1] ^ x.key[i%32]
	}
}

// Open 为 Seal 的逆运算，从帧尾向前还原。
func (x *Xor32) Open(packet []byte) {
	hs := headerSize(packet[0])
	for i := len(packet) - 1; i > hs; i-- {
		packet[i] = packet[i] ^ packet[i-1] ^ x.key[i%32]
	}
}
