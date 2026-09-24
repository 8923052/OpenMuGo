package version

// CodecSelector 按客户端版本选出传输编解码器工厂。
//
// 对应原版 DefaultTcpGameServerListener.StartAsync 里的插件选择：
//
//	EncryptionFactoryPlugIn = PlugInManager.GetStrategy<ClientVersion, INetworkEncryptionFactoryPlugIn>(ClientVersion)
//	                          ?? PlugInManager.GetStrategy<...>(default)
//
// 语义（必须原样保留）：
//   - 精确命中：先按端点版本精确选；
//   - 兜底回落：选不到时用 default 版本（原版 (6,3,English)）的策略；
//   - **选不到不报错**：打警告并落到默认加密（Go 侧返回 nil 表示"用实现内置默认"）。
//
// 本接口放在 version 包而非 crypto：接口跟着使用方走——crypto 的注册表实现它，
// gameserver 只依赖本接口（避免 server→crypto 的耦合面扩大）。

// CodecFactory 为单条连接创建独立编解码器（SimpleModulus 计数器绝不能跨连接共享）。
type CodecFactory interface {
	NewCodec() Codec
}

// Codec 是 transport.Codec 的最小视图（避免 version 直接 import transport，保持叶子包地位）。
// crypto.S6E3ServerCodec 天然满足它（方法集相同，无需适配）。
type Codec interface {
	Seal(packet []byte) []byte
	Open(packet []byte) ([]byte, error)
	OpenPlain(packet []byte) []byte
}

// CodecRegistry 持有版本 → 编解码器工厂 的注册表（对应原版加密插件按 ClientVersion 选择）。
// 零值不可用，须用 NewCodecRegistry 构造。
type CodecRegistry struct {
	byVer map[ClientVersion]CodecFactory
}

// NewCodecRegistry 建注册表并逐条登记。
func NewCodecRegistry(entries map[ClientVersion]CodecFactory) *CodecRegistry {
	m := make(map[ClientVersion]CodecFactory, len(entries))
	for v, f := range entries {
		m[v] = f
	}
	return &CodecRegistry{byVer: m}
}

// CodecFor 按端点版本选工厂；未命中返回 nil（调用方打警告后用默认 codec）。
func (r *CodecRegistry) CodecFor(v ClientVersion) CodecFactory {
	return r.byVer[v]
}
