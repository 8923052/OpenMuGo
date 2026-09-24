// Package crypto 实现 MU Online S6E3 的 SimpleModulus 与 Xor32 加解密。
// 算法与密钥逐字对照 OpenMU：
//   - src/Network/SimpleModulus/PipelinedSimpleModulus*.cs
//   - src/Network/Xor/PipelinedXor32*.cs
//   - src/Network/Xor/DefaultKeys.cs
//
// S6E3 英文 1.04d 管线方向（见 Season6Episode3NetworkEncryptionFactoryPlugIn
// 与 MuMain ClientLibrary/ConnectionManager.ConnectInner）：
//
//	客户端发送: 明文 -> Xor32 -> SimpleModulus(128079 加密侧) -> 线上
//	服务器接收: 线上 -> SimpleModulus(128079 解密侧) -> Xor32 逆 -> 明文
//	服务器发送: 明文 -> SimpleModulus(73326 加密侧) -> 线上   （无 Xor32）
//	客户端接收: 线上 -> SimpleModulus(73326 解密侧) -> 明文    （无 Xor32）
//
// C1/C2 帧不进入 SimpleModulus，但**仍要过 Xor32**（客户端出站无条件 Xor32，
// 服务端 PipelinedDecryptor 也无条件逆 Xor32）；只有 Xor32 之外的加密链是 C3/C4 专属。
package crypto

// Keys 是 SimpleModulus 一个方向的 4 组密钥（New 变体）。
// 布局对应 C# SimpleModulusKeys：Modulus/Encrypt/Decrypt/Xor 各 4 个 uint32。
type Keys struct {
	Modulus [4]uint32
	Encrypt [4]uint32
	Decrypt [4]uint32
	Xor     [4]uint32
}

// createEncryptionKeys 复刻 SimpleModulusKeys.CreateEncryptionKeys：
// 12 个 uint 的顺序为 Modulus[4] + Encrypt[4] + Xor[4]。
func createEncryptionKeys(k [12]uint32) Keys {
	return Keys{
		Modulus: [4]uint32{k[0], k[1], k[2], k[3]},
		Encrypt: [4]uint32{k[4], k[5], k[6], k[7]},
		Xor:     [4]uint32{k[8], k[9], k[10], k[11]},
	}
}

// createDecryptionKeys 复刻 SimpleModulusKeys.CreateDecryptionKeys：
// 12 个 uint 的顺序为 Modulus[4] + Decrypt[4] + Xor[4]。
func createDecryptionKeys(k [12]uint32) Keys {
	return Keys{
		Modulus: [4]uint32{k[0], k[1], k[2], k[3]},
		Decrypt: [4]uint32{k[4], k[5], k[6], k[7]},
		Xor:     [4]uint32{k[8], k[9], k[10], k[11]},
	}
}

// DefaultServerEncryptKeys 是服务器发送方向（S2C）的加密密钥。
// 对应客户端 73326 解密侧；即 PipelinedSimpleModulusEncryptor.DefaultServerKey。
var DefaultServerEncryptKeys = createEncryptionKeys([12]uint32{
	73326, 109989, 98843, 171058,
	13169, 19036, 35482, 29587,
	62004, 64409, 35374, 64599,
})

// DefaultServerDecryptKeys 是服务器接收方向（C2S）的解密密钥。
// 对应客户端 128079 加密侧；即 PipelinedSimpleModulusDecryptor.DefaultServerKey。
var DefaultServerDecryptKeys = createDecryptionKeys([12]uint32{
	128079, 164742, 70235, 106898,
	31544, 2047, 57011, 10183,
	48413, 46165, 15171, 37433,
})

// DefaultClientEncryptKeys 是客户端发送方向的 SM 加密密钥（与服务器解密配对）。
// 即 PipelinedSimpleModulusEncryptor.DefaultClientKey。
var DefaultClientEncryptKeys = createEncryptionKeys([12]uint32{
	128079, 164742, 70235, 106898,
	23489, 11911, 19816, 13647,
	48413, 46165, 15171, 37433,
})

// DefaultClientDecryptKeys 是客户端接收方向的 SM 解密密钥（与服务器发送配对）。
// 即 PipelinedSimpleModulusDecryptor.DefaultClientKey。
var DefaultClientDecryptKeys = createDecryptionKeys([12]uint32{
	73326, 109989, 98843, 171058,
	18035, 30340, 24701, 11141,
	62004, 64409, 35374, 64599,
})

// Xor32Key 是 C2S 方向内层 Xor32 的默认 32 字节密钥（DefaultKeys.Xor32Key）。
var Xor32Key = [32]byte{
	0xAB, 0x11, 0xCD, 0xFE, 0x18, 0x23, 0xC5, 0xA3,
	0xCA, 0x33, 0xC1, 0xCC, 0x66, 0x67, 0x21, 0xF3,
	0x32, 0x12, 0x15, 0x35, 0x29, 0xFF, 0xFE, 0x1D,
	0x44, 0xEF, 0xCD, 0x41, 0x26, 0x3C, 0x4E, 0x4D,
}
