package version

// Registry 是版本注册表，对应 GameServer/ClientVersionResolver.cs。
// 原版是进程内静态表；启动时由 GameServerContainer.LoadGameClientDefinitionsAsync
// 把持久化的 GameClientDefinition 逐条注册，并把第一条设为默认版本。
type Registry struct {
	defs     []GameClientDefinition
	byKey    map[uint64]GameClientDefinition
	byVer    map[ClientVersion][5]byte
	defVer   ClientVersion
	hasDeflt bool
}

// NewRegistry 用给定定义建立注册表；第一条同时作为默认版本（与原版一致）。
func NewRegistry(defs []GameClientDefinition) *Registry {
	r := &Registry{
		defs:  defs,
		byKey: make(map[uint64]GameClientDefinition, len(defs)),
		byVer: make(map[ClientVersion][5]byte, len(defs)),
	}
	for _, d := range defs {
		r.byKey[d.Key()] = d
		r.byVer[d.ClientVersion()] = d.Version
	}
	if len(defs) > 0 {
		r.defVer = defs[0].ClientVersion()
		r.hasDeflt = true
	}
	return r
}

// Key 计算版本键，复刻 ClientVersionResolver.CalculateVersionValue：
// 前 4 字节按小端组成 dword，乘 0x100 后加第 5 字节；不足 5 字节返回 0（0.75 只用到前 3 字节）。
func Key(v []byte) uint64 {
	if len(v) < 5 {
		return 0
	}
	dword := uint64(v[0]) | uint64(v[1])<<8 | uint64(v[2])<<16 | uint64(v[3])<<24
	return dword*0x100 + uint64(v[4])
}

// Resolve 按键查版本，ok=false 表示该版本未注册。
//
// 偏离登记（见 doc/11）：原版未命中时回落到 DefaultVersion；本项目当前返回 ok=false，
// 由登录处理器拒绝（WrongVersion），以便在多版本实现补齐前保持协议一致性可验证。
func (r *Registry) Resolve(key uint64) (ClientVersion, bool) {
	d, ok := r.byKey[key]
	if !ok {
		return ClientVersion{}, false
	}
	return d.ClientVersion(), true
}

// Default 返回默认版本（注册表第一条）。原版用端点绑定的客户端版本；
// 本项目单端点，F1 00 下发的版本号即取此处——多端点落地后应改为按端点取。
func (r *Registry) Default() (ClientVersion, bool) {
	return r.defVer, r.hasDeflt
}

// VersionBytes 返回版本对应的 5 字节版本号（F1 00 GameServerEntered 回显用）。
func (r *Registry) VersionBytes(v ClientVersion) ([5]byte, bool) {
	b, ok := r.byVer[v]
	return b, ok
}

// Definitions 返回注册表中的全部客户端定义。
func (r *Registry) Definitions() []GameClientDefinition { return r.defs }
