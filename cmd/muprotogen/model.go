package main

// XML 模型，严格对应 OpenMU PacketDefinitions.xsd。
// 命名空间通过元素本地名匹配（encoding/xml 对无前缀 tag 忽略命名空间）。

// Definitions 是一个 *Packets.xml 的根对象。
type Definitions struct {
	Description string      `xml:"Description"`
	Structures  []Structure `xml:"Structures>Structure"`
	Packets     []Packet    `xml:"Packets>Packet"`
	Enums       []Enum      `xml:"Enums>Enum"`
}

// Packet 对应 <Packet>。
type Packet struct {
	HeaderType     string      `xml:"HeaderType"`
	Code           string      `xml:"Code"`
	SubCode        *string     `xml:"SubCode"`
	Name           string      `xml:"Name"`
	Caption        string      `xml:"Caption"`
	Length         *int        `xml:"Length"`
	Direction      string      `xml:"Direction"`
	SentWhen       string      `xml:"SentWhen"`
	CausedReaction string      `xml:"CausedReaction"`
	Fields         []Field     `xml:"Fields>Field"`
	Structures     []Structure `xml:"Structures>Structure"`
	Enums          []Enum      `xml:"Enums>Enum"`
}

// Structure 对应 <Structure>，可作为包头、包内结构或独立结构。
type Structure struct {
	Name        string  `xml:"Name"`
	Description string  `xml:"Description"`
	Length      *int    `xml:"Length"`
	Fields      []Field `xml:"Fields>Field"`
}

// Field 对应 <Field>。可选字段用指针区分“未指定”和“零值”。
type Field struct {
	Index            int     `xml:"Index"`
	LeftShifted      *int    `xml:"LeftShifted"`
	Type             string  `xml:"Type"`
	TypeName         string  `xml:"TypeName"`
	Name             string  `xml:"Name"`
	Description      string  `xml:"Description"`
	Length           *int    `xml:"Length"`
	DefaultValue     *string `xml:"DefaultValue"`
	ItemCountField   string  `xml:"ItemCountField"`
	UseCustomIndexer *bool   `xml:"UseCustomIndexer"`
}

// Enum 对应 <Enum>。
type Enum struct {
	Name        string      `xml:"Name"`
	Description string      `xml:"Description"`
	Values      []EnumValue `xml:"Values>EnumValue"`
}

// EnumValue 对应 <EnumValue>。XSD 声明为 unsignedByte，但实际 XML 存在 256
// （如任务条件枚举），C# 生成枚举也未指定 byte 基类型，因此用 uint16。
type EnumValue struct {
	Name        string `xml:"Name"`
	Description string `xml:"Description"`
	Value       uint16 `xml:"Value"`
}

// TargetConfig 是一个生成目标（一个 Go 包，对应一个方向的 XML）。
type TargetConfig struct {
	// Package 是生成的 Go 包名。
	Package string `json:"package"`
	// Output 是生成目录（相对于 protocol.gen.json）。
	Output string `json:"output"`
	// Full 是 mode=full 的 XML：生成其中的包、结构、枚举。
	Full string `json:"full"`
	// EnumRefs 是仅用于解析跨命名空间枚举引用的 XML（不生成其中的包）。
	EnumRefs []string `json:"enum_refs"`
}

// GenConfig 是 protocol.gen.json 的根对象。
type GenConfig struct {
	PacketsVersion string         `json:"packets_version"`
	XMLRoot        string         `json:"xml_root"`
	CommonInputs   []string       `json:"common_inputs"`
	Targets        []TargetConfig `json:"targets"`
}

// targetRuntime 是单个目标运行期解析出的输入集合。
type targetRuntime struct {
	cfg   *GenConfig
	rt    *TargetConfig
	files map[string]*Definitions // scope -> Definitions
}

func (f *Field) bits() int {
	if f.Length != nil {
		return *f.Length
	}
	return 8
}

func (f *Field) shift() int {
	if f.LeftShifted != nil {
		return *f.LeftShifted
	}
	return 0
}

func (f *Field) isBitPacked() bool {
	return f.bits() != 8 || f.LeftShifted != nil
}

func (f *Field) customIndexer() bool {
	return f.UseCustomIndexer != nil && *f.UseCustomIndexer
}

// variableLast 复刻 GenerateStructs.xslt 的 lengthCalculator 判定：
// 仅最后一个字段为无定长 String/Binary/Structure[] 时，整包为变长。
func variableLast(fields []Field) *Field {
	if len(fields) == 0 {
		return nil
	}
	last := &fields[len(fields)-1]
	if last.Type != "String" && last.Type != "Binary" && last.Type != "Structure[]" {
		return nil
	}
	if last.Length != nil {
		return nil
	}
	return last
}
