package crypto

import "testing"

// TestCryptoLayers 分层验证 SimpleModulus 的三个独立环节：
// 模运算互逆、18 位打包互逆、整块加解密一致。任一层偏移都会在此暴露。
func TestCryptoLayers(t *testing.T) {
	enc := NewEncryptor(DefaultServerEncryptKeys)
	dec := NewDecryptor(DefaultClientDecryptKeys)

	var block [decryptedBlockSize]byte
	for i := range block {
		block[i] = byte(i + 1)
	}

	// 层 1：73326 enc -> 73326 dec 模运算互逆。
	copy(enc.inputBuf[:], block[:])
	r := enc.encryptContent(enc.inputBuf)
	var plainOut [decryptedBlockSize]byte
	dec.decryptContent(r, plainOut[:])
	for i := range block {
		if plainOut[i] != block[i] {
			t.Fatalf("模运算层不互逆 at %d: got %d want %d", i, plainOut[i], block[i])
		}
	}

	// 层 2：18 位位流打包/读取互逆（含跨字节 remainder）。
	var packed [encryptedBlockSize]byte
	for i := 0; i < valueCount; i++ {
		writeResultToTarget(packed[:], i, r[i])
	}
	for i := 0; i < valueCount; i++ {
		if got := readInputBuffer(packed, i); got != r[i] {
			t.Fatalf("位打包层不互逆 at %d: got %08X want %08X", i, got, r[i])
		}
	}

	// 层 3：整块加密 -> 解密，块大小与内容一致。
	clearBytes(enc.inputBuf[:])
	for i := range block {
		enc.inputBuf[i] = block[i]
	}
	var encBlock [encryptedBlockSize]byte
	enc.encryptBlock(encBlock[:], decryptedBlockSize)
	var blockOut [decryptedBlockSize]byte
	blockSize, err := dec.decryptBlock(encBlock, blockOut[:])
	if err != nil {
		t.Fatalf("整块解密失败: %v", err)
	}
	if blockSize != decryptedBlockSize {
		t.Fatalf("blockSize=%d, want %d", blockSize, decryptedBlockSize)
	}
	for i := range block {
		if blockOut[i] != block[i] {
			t.Fatalf("整块内容不互逆 at %d", i)
		}
	}
}
