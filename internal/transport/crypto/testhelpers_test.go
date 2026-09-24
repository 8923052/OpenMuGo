package crypto

// 测试用的客户端侧管线（与 MuMain ConnectInner 完全同构）。

type clientPipeline struct {
	sm    *SimpleModulus
	xor32 *Xor32
}

func newClientEncryptor() *clientPipeline {
	return &clientPipeline{
		sm:    NewEncryptor(DefaultClientEncryptKeys),
		xor32: NewXor32(Xor32Key),
	}
}

func newClientDecryptor() *SimpleModulus {
	return NewDecryptor(DefaultClientDecryptKeys)
}
