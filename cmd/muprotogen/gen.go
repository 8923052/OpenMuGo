package main

import (
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
)

type routeEntry struct {
	header    byte
	code      byte
	hasSub    bool
	sub       byte
	goName    string
	fixed     bool
	length    int
	direction string
}

type fixedPacket struct {
	goName string
	size   int
	header byte
	code   byte
	hasSub bool
	sub    byte
}

type fixedStruct struct {
	goName string
	size   int
}

// generator 持有一次代码生成运行（单个目标）的全部状态。
type generator struct {
	version   string
	target    *TargetConfig
	files     map[string]*Definitions
	full      *Definitions
	fullScope string

	enumPool   map[string]Enum
	structPool map[string]Structure

	typeAlloc *allocator
	enumGo    map[string]string
	enumConst map[string]string // enumKey+"|"+valueName -> Go 常量名
	structGo  map[string]string
	packetGo  map[string]string

	routes       []routeEntry
	fixedPackets []fixedPacket
	fixedStructs []fixedStruct
	warnings     []string
}

func newGenerator(version string, target *TargetConfig, files map[string]*Definitions) (*generator, error) {
	fullScope := scopeOf(target.Full)
	full, ok := files[fullScope]
	if !ok {
		return nil, fmt.Errorf("目标 %s 缺少 full 输入 scope=%s", target.Package, fullScope)
	}
	g := &generator{
		version:    version,
		target:     target,
		files:      files,
		full:       full,
		fullScope:  fullScope,
		enumPool:   buildEnumPool(files),
		structPool: buildStructPool(fullScope, full),
		typeAlloc:  newAllocator(),
		enumGo:     map[string]string{},
		enumConst:  map[string]string{},
		structGo:   map[string]string{},
		packetGo:   map[string]string{},
	}
	g.registerTypeNames()
	return g, nil
}

// registerTypeNames 在生成任何访问器前，确定全部类型在同一个 Go 包内的唯一名。
func (g *generator) registerTypeNames() {
	// 1. 公共枚举（CommonEnums.xml）
	for _, e := range g.files["common"].Enums {
		key := enumKey("common", "", e.Name, "")
		g.enumGo[key] = g.typeAlloc.take(sanitizeName(e.Name))
	}
	// 2. 非 full 输入的全局枚举（如 ClientToServer.PetType 这类跨命名空间引用）
	for scope, defs := range g.files {
		if scope == "common" || scope == g.fullScope {
			continue
		}
		for _, e := range defs.Enums {
			key := enumKey("global", scope, e.Name, "")
			g.enumGo[key] = g.typeAlloc.take(sanitizeName(scope + e.Name))
		}
	}
	// 3. full 文件的全局枚举
	for _, e := range g.full.Enums {
		key := enumKey("global", g.fullScope, e.Name, "")
		g.enumGo[key] = g.typeAlloc.take(sanitizeName(e.Name))
	}
	// 4. 每个包的局部枚举
	for _, p := range g.full.Packets {
		for _, e := range p.Enums {
			key := enumKey("local", g.fullScope, p.Name, e.Name)
			g.enumGo[key] = g.typeAlloc.take(sanitizeName(e.Name))
		}
	}
	// 5. 结构（全局 + 局部）
	for _, s := range g.full.Structures {
		key := "global:" + g.fullScope + ":" + s.Name
		g.structGo[key] = g.typeAlloc.take(sanitizeName(s.Name))
	}
	for _, p := range g.full.Packets {
		for _, s := range p.Structures {
			key := "local:" + g.fullScope + ":" + p.Name + ":" + s.Name
			g.structGo[key] = g.typeAlloc.take(sanitizeName(s.Name))
		}
	}
	// 6. 包
	for _, p := range g.full.Packets {
		g.packetGo[p.Name] = g.typeAlloc.take(sanitizeName(p.Name))
	}

	// 7. 枚举成员常量名（每个枚举独立分配，保证枚举内唯一；生成与默认值引用共用）。
	for key, e := range g.enumPool {
		constAlloc := newAllocator()
		goName := g.enumGo[key]
		for _, v := range e.Values {
			cn := goName + "_" + sanitizeName(v.Name)
			g.enumConst[key+"|"+v.Name] = constAlloc.take(cn)
		}
	}
}

// resolveEnumKey 按 局部→本文件全局→common（或跨命名空间点号引用）顺序找枚举键。
func (g *generator) resolveEnumKey(typeName, ownerPacket string) (string, bool) {
	candidates := []string{}
	if i := strings.Index(typeName, "."); i >= 0 {
		scope, name := typeName[:i], typeName[i+1:]
		candidates = append(candidates, enumKey("global", scope, name, ""))
	} else {
		if ownerPacket != "" {
			candidates = append(candidates, enumKey("local", g.fullScope, ownerPacket, typeName))
		}
		candidates = append(candidates, enumKey("global", g.fullScope, typeName, ""))
		candidates = append(candidates, enumKey("common", "", typeName, ""))
	}
	for _, key := range candidates {
		if _, ok := g.enumPool[key]; ok {
			return key, true
		}
	}
	return "", false
}

// enumConstName 返回某枚举成员的生成常量名。
func (g *generator) enumConstName(typeName, ownerPacket, valueName string) (string, bool) {
	key, ok := g.resolveEnumKey(typeName, ownerPacket)
	if !ok {
		return "", false
	}
	cn, ok := g.enumConst[key+"|"+valueName]
	return cn, ok
}

func (g *generator) resolveEnum(typeName, ownerPacket string) (string, error) {
	key, ok := g.resolveEnumKey(typeName, ownerPacket)
	if !ok {
		return "", fmt.Errorf("无法解析枚举类型: %s (owner=%s)", typeName, ownerPacket)
	}
	return g.enumGo[key], nil
}

func (g *generator) resolveStruct(typeName, ownerPacket string) (string, Structure, error) {
	candidates := []string{}
	if ownerPacket != "" {
		candidates = append(candidates, "local:"+g.fullScope+":"+ownerPacket+":"+typeName)
	}
	candidates = append(candidates, "global:"+g.fullScope+":"+typeName)
	for _, key := range candidates {
		if s, ok := g.structPool[key]; ok {
			return g.structGo[key], s, nil
		}
	}
	return "", Structure{}, fmt.Errorf("无法解析结构类型: %s (owner=%s)", typeName, ownerPacket)
}

func parseHeaderType(headerType string) (byte, error) {
	v, err := strconv.ParseUint(headerType[:2], 16, 8)
	if err != nil {
		return 0, fmt.Errorf("非法 HeaderType %q: %w", headerType, err)
	}
	return byte(v), nil
}

func isWideHeader(header byte) bool { return header == 0xC2 || header == 0xC4 }

// Generate 执行全部生成，返回 文件名 -> 源码。
func (g *generator) Generate() (map[string]string, []string, error) {
	enumsSrc, err := g.emitEnums()
	if err != nil {
		return nil, nil, err
	}
	structsSrc, err := g.emitStructures()
	if err != nil {
		return nil, nil, err
	}
	packetsSrc, err := g.emitPackets()
	if err != nil {
		return nil, nil, err
	}
	registrySrc := g.emitRegistry()
	testSrc, err := g.emitTests()
	if err != nil {
		return nil, nil, err
	}
	versionSrc := g.emitVersion()

	out := map[string]string{
		"enums_gen.go":                    enumsSrc,
		"structs_gen.go":                  structsSrc,
		"packets_gen.go":                  packetsSrc,
		"registry_gen.go":                 registrySrc,
		g.target.Package + "_gen_test.go": testSrc,
		"version_gen.go":                  versionSrc,
	}
	for name, src := range out {
		formatted, err := format.Source([]byte(src))
		if err != nil {
			return nil, nil, fmt.Errorf("gofmt 失败 %s: %w", name, err)
		}
		out[name] = string(formatted)
	}
	return out, g.warnings, nil
}

const genBanner = "// Code generated by muprotogen from MUnique.OpenMU.Network.Packets %s; DO NOT EDIT.\n\n"

func (g *generator) pkgHeader() string {
	return fmt.Sprintf(genBanner, g.version) + "package " + g.target.Package + "\n\n"
}

// emitVersion 输出版本锚点。
func (g *generator) emitVersion() string {
	var b strings.Builder
	b.WriteString(g.pkgHeader())
	b.WriteString("// PacketsVersion 与 MuMain 引用的 MUnique.OpenMU.Network.Packets NuGet 包版本一致。\n")
	b.WriteString("const PacketsVersion = \"" + g.version + "\"\n")
	return b.String()
}

// emitEnums 按确定顺序输出枚举：common → 外部全局 → full 全局 → 包内局部。
func (g *generator) emitEnums() (string, error) {
	var b strings.Builder
	b.WriteString(g.pkgHeader())

	emitOne := func(e Enum, key string) {
		goName := g.enumGo[key]
		if e.Description != "" {
			b.WriteString("// " + goName + " " + oneLine(e.Description) + "\n")
		}
		b.WriteString("type " + goName + " uint16\n\n")
		b.WriteString("const (\n")
		for _, v := range e.Values {
			valName := g.enumConst[key+"|"+v.Name]
			if v.Description != "" {
				b.WriteString("\t// " + valName + " " + oneLine(v.Description) + "\n")
			}
			b.WriteString(fmt.Sprintf("\t%s %s = %d\n", valName, goName, v.Value))
		}
		b.WriteString(")\n\n")
	}

	for _, e := range g.files["common"].Enums {
		emitOne(e, enumKey("common", "", e.Name, ""))
	}
	scopes := make([]string, 0, len(g.files))
	for scope := range g.files {
		if scope != "common" && scope != g.fullScope {
			scopes = append(scopes, scope)
		}
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		for _, e := range g.files[scope].Enums {
			emitOne(e, enumKey("global", scope, e.Name, ""))
		}
	}
	for _, e := range g.full.Enums {
		emitOne(e, enumKey("global", g.fullScope, e.Name, ""))
	}
	for _, p := range g.full.Packets {
		for _, e := range p.Enums {
			emitOne(e, enumKey("local", g.fullScope, p.Name, e.Name))
		}
	}
	return b.String(), nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
