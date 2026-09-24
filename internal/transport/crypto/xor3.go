package crypto

// Xor3 实现 MU 登录字段使用的 3 字节循环 XOR（用户名/密码"加密"）。
// 对照 OpenMU Xor3Encryptor：key={0xFC,0xCF,0xAB}，从字段偏移 0 开始循环。
// 自反：同算法加密与解密一致（OpenMU 登录仅用 Xor3Decryptor(0)）。
type Xor3 struct{}

// Xor3Keys 是默认 3 字节密钥（Xor3Encryptor 构造内常量）。
var Xor3Keys = [3]byte{0xFC, 0xCF, 0xAB}

// NewXor3 创建 Xor3 变换器（无状态，可全局复用）。
func NewXor3() Xor3 { return Xor3{} }

// Apply 原地对 data 做循环 XOR（startOffset 对应 C# 构造参数，登录字段为 0）。
func (Xor3) Apply(data []byte) {
	for i := range data {
		data[i] ^= Xor3Keys[i%len(Xor3Keys)]
	}
}

// DecryptString 对登录的用户名/密码字段解密并按首个 null 截断为 UTF-8 字符串。
// 与 C# LogInHandlerPlugIn.Decrypt 等价：先 Xor3 再 ExtractString。
func (x Xor3) DecryptString(field []byte) string {
	buf := append([]byte(nil), field...)
	x.Apply(buf)
	if i := indexZero(buf); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}
