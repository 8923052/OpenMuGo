package version

import "fmt"

// ClientLanguage 是协议语言（对应 Network/PlugIns/ClientLanguage.cs）。
// 它不只是 UI 文案：部分包的 opcode 会随语言变化（见 RemoteView 的 walk/hit 选择）。
type ClientLanguage uint8

const (
	LanguageInvariant  ClientLanguage = 0 // 通配，兼容任意语言
	LanguageEnglish    ClientLanguage = 1
	LanguageJapanese   ClientLanguage = 2
	LanguageVietnamese ClientLanguage = 3
	LanguageFilipino   ClientLanguage = 4
	LanguageChinese    ClientLanguage = 5
	LanguageKorean     ClientLanguage = 6
	LanguageThai       ClientLanguage = 7
)

var languageNames = map[ClientLanguage]string{
	LanguageInvariant:  "Invariant",
	LanguageEnglish:    "English",
	LanguageJapanese:   "Japanese",
	LanguageVietnamese: "Vietnamese",
	LanguageFilipino:   "Filipino",
	LanguageChinese:    "Chinese",
	LanguageKorean:     "Korean",
	LanguageThai:       "Thai",
}

// String 返回语言名（日志用）。
func (l ClientLanguage) String() string {
	if n, ok := languageNames[l]; ok {
		return n
	}
	return fmt.Sprintf("Language(%d)", uint8(l))
}

// ClientVersion 标识客户端版本（对应 Network/PlugIns/ClientVersion.cs 的 record struct）。
// Season=0 表示无赛季的旧版（0.75/0.95d）；Season=106 表示 OpenSource 客户端（原版用 Season+100 标记）。
type ClientVersion struct {
	Season   uint8
	Episode  uint8
	Language ClientLanguage
}

// combined 是原版的 CombinedVersion = (Season << 8) + Episode。
func (v ClientVersion) combined() int { return int(v.Season)<<8 + int(v.Episode) }

// String 返回可读版本号（日志用）。
func (v ClientVersion) String() string {
	return fmt.Sprintf("%d.%d/%s", v.Season, v.Episode, v.Language)
}

// unsuitable 是"语言不兼容"的比较结果，对应原版的 int.MinValue：
// 只要比任何正常差值都小即可——Suitable 只做 >=0 与 >1 两种判定。
const unsuitable = -1 << 31

// CompareTo 复刻 ClientVersion.CompareTo：
//   - 对方语言非 Invariant 且与本方不同 → 不兼容，返回 unsuitable（永不适用）
//   - 否则返回 combined 差值 ×10（留出语言位）
//   - 本方为具体语言、对方为通配语言时 +1（具体语言"略高于"通配）
//
// 注意原版 operator >= / <= 的写法有笔误（误写成 > 0 / < 0）；Go 端不提供这两个运算符，
// 统一用 CompareTo 的返回值判断，避免继承该缺陷。
func (v ClientVersion) CompareTo(other ClientVersion) int {
	if other.Language != LanguageInvariant && v.Language != other.Language {
		return unsuitable
	}
	result := (v.combined() - other.combined()) * 10
	if v.Language != LanguageInvariant && other.Language == LanguageInvariant {
		result++
	}
	return result
}

// characterListExtendedMin 对应原版 ShowCharacterListExtendedPlugIn 标注的 MinimumClient(106,3)。
var characterListExtendedMin = ClientVersion{Season: 106, Episode: 3, Language: LanguageInvariant}

// UsesExtendedCharacterList 报告该客户端版本应下发扩展角色列表（CharacterListExtended）。
//
// 原版不写版本公式，而是由插件框架按 MinimumClient 选中实现：
//
//	(106,3) ShowCharacterListExtendedPlugIn → CharacterListExtended：44B 条目 / 27B 扩展外观
//	(5,0)   ShowCharacterListPlugIn         → CharacterList：34B 条目 / 18B 外观
//	(0,95)  ShowCharacterListPlugIn095      → CharacterList095
//	(0,75)  ShowCharacterListPlugIn075      → CharacterList075
//
// MuMain 的 WSclient.cpp 只实现了 ReceiveCharacterListExtended，其结构体在默认对齐下 sizeof = 44
// （Index1 + ID10 + pad1 + Level2 + CtlCode1 + Class1 + Flags1 + Equipment25 + GuildStatus1 + pad1），
// 与 XML 里 CharacterListExtended 的 44B 条目逐字段重合；下发紧凑版会让客户端解析错位、卡在角色选择。
// 多版本实现补齐后，本方法应并入 ShowCharacterListView 的选择（见 doc/11）。
func (v ClientVersion) UsesExtendedCharacterList() bool {
	return AtLeast(characterListExtendedMin).Suitable(v)
}
