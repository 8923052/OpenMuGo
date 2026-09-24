package crypto

import "mugo/internal/version"

// Codec 描述一条连接在一个方向上的字节变换（出站/入站）。
// C3/C4 走 SimpleModulus + Xor32；C1/C2 不做 SimpleModulus，但**仍要过 Xor32**。
type Codec interface {
	// Seal 把标准明文帧变换为线上帧（出站）。
	Seal(packet []byte) []byte
	// Open 把 C3/C4 线上帧还原为标准明文帧（入站）。
	Open(packet []byte) ([]byte, error)
	// OpenPlain 把 C1/C2 线上帧还原为标准明文帧（入站，只逆 Xor32）。
	OpenPlain(packet []byte) []byte
}

// S6E3ServerCodec 是 GS 在 S6E3 英文 1.04d 下的完整编解码组合：
//   - Seal（服务器发送）: SimpleModulus(73326 加密侧)，无 Xor32
//   - Open（服务器接收 C3/C4）: SimpleModulus(128079 解密侧) 后再做 Xor32 逆变换
//   - OpenPlain（服务器接收 C1/C2）: 只做 Xor32 逆变换
type S6E3ServerCodec struct {
	sealSM *SimpleModulus
	openSM *SimpleModulus
	xor32  *Xor32
}

// NewS6E3ServerCodec 创建每连接独立的 S6E3 编解码器（计数器互相独立）。
func NewS6E3ServerCodec() *S6E3ServerCodec {
	return &S6E3ServerCodec{
		sealSM: NewEncryptor(DefaultServerEncryptKeys),
		openSM: NewDecryptor(DefaultServerDecryptKeys),
		xor32:  NewXor32(Xor32Key),
	}
}

// Seal 实现 GS 发送方向。输入必须为 C3/C4 帧。
func (c *S6E3ServerCodec) Seal(packet []byte) []byte {
	return c.sealSM.Seal(packet)
}

// Open 实现 GS 接收方向（C3/C4）：先 SimpleModulus 解密，再逆向 Xor32。
func (c *S6E3ServerCodec) Open(packet []byte) ([]byte, error) {
	plain, err := c.openSM.Open(packet)
	if err != nil {
		return nil, err
	}
	c.xor32.Open(plain)
	return plain, nil
}

// OpenPlain 实现 GS 接收方向的 C1/C2 分支：SimpleModulus 直通，只逆向 Xor32。
// 必读：真实客户端的出站管线（OpenMU ClientLibrary ConnectInner +
// PipelinedXor32Encryptor）对**任何**帧都做 Xor32，没有 C1/C3 判断；
// 服务端 PipelinedDecryptor 同理对任何帧都逆向 Xor32。曾经把 C1 当"纯明文"直接交给
// 分发器，导致客户端第一个 C1 包（F3 00 请求角色列表）解出来是
// C1 05 F3 0D 15，被当成未知包忽略 → 客户端登录后卡死。
func (c *S6E3ServerCodec) OpenPlain(packet []byte) []byte {
	c.xor32.Open(packet)
	return packet
}

// IsEncrypted 报告帧类型是否进入加密链（C3/C4）。C1/C2 不走 SimpleModulus，
// 但仍需 OpenPlain 逆 Xor32。
func IsEncrypted(header byte) bool {
	return header == 0xC3 || header == 0xC4
}

// S6E3CodecFactory 是 S6E3 编解码器的工厂，实现 version.CodecFactory（每连接独立计数器）。
// 对应原版 Season6Episode3NetworkEncryptionFactoryPlugIn 与 OpenSourceClientNetworkEncryptionFactoryPlugIn
// （两个工厂逐行相同、仅 Key 不同，见 doc/11；一个工厂覆盖 (6,3) 与 (106,3) 两个版本）。
type S6E3CodecFactory struct{}

// NewCodec 创建单连接编解码器。
func (S6E3CodecFactory) NewCodec() version.Codec { return NewS6E3ServerCodec() }

// S6E3Factories 返回 S6E3 家族两个版本的 version.CodecFactory 注册项。
// 装配层用法：version.NewCodecRegistry(crypto.S6E3Factories())。
func S6E3Factories() map[version.ClientVersion]version.CodecFactory {
	return map[version.ClientVersion]version.CodecFactory{
		{Season: 6, Episode: 3, Language: version.LanguageEnglish}:   S6E3CodecFactory{},
		{Season: 106, Episode: 3, Language: version.LanguageEnglish}: S6E3CodecFactory{},
	}
}
