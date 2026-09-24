package version

// GameClientDefinition 对应 DataModel/Configuration/GameClientDefinition.cs：
// 一个可被服务的客户端版本，含线上 5 字节版本号与客户端序列号。
type GameClientDefinition struct {
	Season   uint8
	Episode  uint8
	Language ClientLanguage
	// Version 是客户端二进制里的 5 字节版本号（登录包与 F1 00 回显用同一串）。
	Version [5]byte
	// Serial 是客户端序列号（原版用于校验客户端合法性）。
	Serial string
	// Description 是人类可读名（原版同名）。
	Description string
}

// ClientVersion 返回该定义的版本三元组。
func (d GameClientDefinition) ClientVersion() ClientVersion {
	return ClientVersion{Season: d.Season, Episode: d.Episode, Language: d.Language}
}

// Key 返回该定义在版本注册表里的键。
func (d GameClientDefinition) Key() uint64 { return Key(d.Version[:]) }

// 原版 `Persistence/Initialization/*/DataInitialization.cs` 的 CreateGameClientDefinition 注册项：
// 每个版本的 5 字节版本号都是 ASCII 数字串，Season/Episode/Language 由该文件显式指定。
const (
	serialSeasonSix = "k1Pk2jcET48mxL3b"
	serial095d      = "4zYGWgYggf9ZENHc"
	serial075       = "sudv(*40ds7lkN2n"
)

// GMOS6E3 是 Season 6 Episode 3 的 GMO 官方客户端（ASCII 10404）。
func GMOS6E3() GameClientDefinition {
	return GameClientDefinition{
		Season: 6, Episode: 3, Language: LanguageEnglish,
		Version: [5]byte{'1', '0', '4', '0', '4'}, Serial: serialSeasonSix,
		Description: "Season 6 Episode 3 GMO Client",
	}
}

// OpenSourceS6E3 是 Season 6 Episode 3 的开源客户端（ASCII 20404，即 MuMain）。
// 原版把它注册为 Season 106（"Season 6+100"标记法），与 GMO 的 6/3 不是同一版本号，
// 插件选择结果也因此不同（如角色列表 44B/27B 扩展 vs 34B/18B 紧凑）。
func OpenSourceS6E3() GameClientDefinition {
	return GameClientDefinition{
		Season: 106, Episode: 3, Language: LanguageEnglish,
		Version: [5]byte{'2', '0', '4', '0', '4'}, Serial: serialSeasonSix,
		Description: "Season 6 Episode 3 Open Source Client",
	}
}

// V095d 是 0.95d 客户端（ASCII 09504）。
func V095d() GameClientDefinition {
	return GameClientDefinition{
		Season: 0, Episode: 95, Language: LanguageEnglish,
		Version: [5]byte{'0', '9', '5', '0', '4'}, Serial: serial095d,
		Description: "Version 0.95d Client",
	}
}

// V075 是 0.75 客户端（ASCII 07500）。
// 原版注释：后两字节无关，0.75 只用到前 3 字节；语言为 Invariant（尚无协议差异）。
func V075() GameClientDefinition {
	return GameClientDefinition{
		Season: 0, Episode: 75, Language: LanguageInvariant,
		Version: [5]byte{'0', '7', '5', '0', '0'}, Serial: serial075,
		Description: "Version 0.75 Client",
	}
}

// Original 返回原版注册的全部四个客户端版本定义。
// 多版本服务 = GameServerDefinition.Endpoints 上每个端点绑一个 GameClientDefinition。
func Original() []GameClientDefinition {
	return []GameClientDefinition{V075(), V095d(), GMOS6E3(), OpenSourceS6E3()}
}

// MuMain 是本项目真机联调对齐的客户端（开源客户端 S6E3，ASCII 20404）。
func MuMain() GameClientDefinition { return OpenSourceS6E3() }
