package version

// Constraint 描述一个实现（handler / 序列化器 / 加密器）适用的客户端版本区间。
//
// 对应原版的 MinimumClientAttribute / MaximumClientAttribute：原版把区间写在类型上，
// 由 PlugInManager 反射读取；Go 端不移植反射框架，改为每个实现显式暴露一个 Constraint，
// 由注册表在连接建立 / 版本变更时筛选。判定语义必须与原版逐字一致。
type Constraint struct {
	// Min 为包含式下限，nil 表示不限下限。
	Min *ClientVersion
	// Max 为包含式上限，nil 表示不限上限。
	Max *ClientVersion
}

// AtLeast 构造只带下限的约束（对应单个 MinimumClientAttribute）。
func AtLeast(min ClientVersion) Constraint { return Constraint{Min: &min} }

// Below 构造只带上限的约束（对应单个 MaximumClientAttribute）。
func Below(max ClientVersion) Constraint { return Constraint{Max: &max} }

// Between 构造闭区间约束（同时标了 Min 与 Max）。
func Between(min, max ClientVersion) Constraint { return Constraint{Min: &min, Max: &max} }

// Suitable 复刻 GameServer/PlugInTypeExtensions.cs 的 IsPlugInSuitable：
//
//	(min == nil || client.CompareTo(min) >= 0) && (max == nil || client.CompareTo(max) <= 1)
//
// 上限之所以是 `<= 1` 而不是 `<= 0`：CompareTo 的差值被放大 10 倍，且"本方具体语言 vs
// 对方通配语言"会额外 +1，`<= 1` 才能同时接受"正好相等"与"仅语言更具体"两种情形。
// 语言不兼容时 CompareTo 返回 unsuitable，下限判定必然失败——即语言不符的实现不会被选中。
func (c Constraint) Suitable(client ClientVersion) bool {
	if c.Min != nil && client.CompareTo(*c.Min) < 0 {
		return false
	}
	if c.Max != nil && client.CompareTo(*c.Max) > 1 {
		return false
	}
	return true
}
