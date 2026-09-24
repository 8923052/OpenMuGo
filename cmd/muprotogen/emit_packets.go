package main

import (
	"fmt"
	"strconv"
	"strings"
)

// emitPackets 输出全部包视图。
func (g *generator) emitPackets() (string, error) {
	var b strings.Builder
	b.WriteString(g.pkgHeader())

	for i := range g.full.Packets {
		if err := g.writeOnePacket(&b, &g.full.Packets[i]); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func (g *generator) writeOnePacket(b *strings.Builder, p *Packet) error {
	goName := g.packetGo[p.Name]
	header, err := parseHeaderType(p.HeaderType)
	if err != nil {
		return fmt.Errorf("包 %s: %w", p.Name, err)
	}
	code, err := strconv.ParseUint(p.Code, 16, 8)
	if err != nil {
		return fmt.Errorf("包 %s Code=%q: %w", p.Name, p.Code, err)
	}
	subCode := -1
	if p.SubCode != nil {
		v, err := strconv.ParseUint(*p.SubCode, 16, 8)
		if err != nil {
			return fmt.Errorf("包 %s SubCode=%q: %w", p.Name, *p.SubCode, err)
		}
		subCode = int(v)
	}
	vf := variableLast(p.Fields)

	b.WriteString("// " + goName + " (" + p.HeaderType + " 0x" + p.Code)
	if p.SubCode != nil {
		b.WriteString(" 0x" + *p.SubCode)
	}
	b.WriteString(", " + p.Direction + ")\n")
	b.WriteString("type " + goName + " struct{ d []byte }\n\n")

	fmt.Fprintf(b, "const %sHeaderType byte = 0x%02X\n", goName, header)
	fmt.Fprintf(b, "const %sCode byte = 0x%02X\n", goName, code)
	if subCode >= 0 {
		fmt.Fprintf(b, "const %sSubCode byte = 0x%02X\n", goName, subCode)
	}

	fixed := p.Length != nil
	if fixed {
		fmt.Fprintf(b, "const %sLength = %d\n", goName, *p.Length)
	}
	b.WriteString("\n")

	// 构造函数
	if fixed {
		fmt.Fprintf(b, "func New%s() *%s {\n", goName, goName)
		fmt.Fprintf(b, "\tp := &%s{d: make([]byte, %sLength)}\n", goName, goName)
	} else {
		fmt.Fprintf(b, "func New%s(size int) *%s {\n", goName, goName)
		fmt.Fprintf(b, "\tp := &%s{d: make([]byte, size)}\n", goName)
	}
	fmt.Fprintf(b, "\tsetHeader(p.d, 0x%02X, 0x%02X, %d)\n", header, code, subCode)
	g.emitDefaults(b, p)
	b.WriteString("\treturn p\n}\n\n")

	// 变长包的尺寸计算
	if vf != nil {
		if err := g.emitRequiredSize(b, goName, p.Name, vf); err != nil {
			return err
		}
	}

	fmt.Fprintf(b, "func As%s(buf []byte) *%s { return &%s{d: buf} }\n", goName, goName, goName)
	fmt.Fprintf(b, "func (p *%s) Bytes() []byte { return p.d }\n", goName)
	fmt.Fprintf(b, "func (p *%s) Len() int { return len(p.d) }\n\n", goName)

	code2, err := g.emitFields(fieldContext{goName: goName, ownerPacket: p.Name, recv: "p"}, p.Fields)
	if err != nil {
		return err
	}
	b.WriteString(code2)

	sub := byte(0)
	if subCode >= 0 {
		sub = byte(subCode)
	}
	g.routes = append(g.routes, routeEntry{
		header:    header,
		code:      byte(code),
		hasSub:    subCode >= 0,
		sub:       sub,
		goName:    goName,
		fixed:     fixed,
		length:    derefInt(p.Length),
		direction: p.Direction,
	})
	if fixed {
		g.fixedPackets = append(g.fixedPackets, fixedPacket{
			goName: goName, size: *p.Length, header: header, code: byte(code),
			hasSub: subCode >= 0, sub: sub,
		})
	}
	return nil
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// emitDefaults 复刻 XSLT 中 pd:DefaultValue 的初始化调用。
func (g *generator) emitDefaults(b *strings.Builder, p *Packet) {
	methods := methodMap(p.Fields)
	for _, f := range p.Fields {
		if f.DefaultValue == nil || f.Name == "" {
			continue
		}
		method := methods[f.Name]
		switch f.Type {
		case "Boolean":
			fmt.Fprintf(b, "\tp.Set%s(%s)\n", method, strings.ToLower(*f.DefaultValue))
		case "Enum":
			// C# 初始化直接写枚举成员符号；默认值可能带 "EnumType.Member" 限定；数字按数值处理。
			dv := *f.DefaultValue
			if i := strings.LastIndex(dv, "."); i >= 0 {
				dv = dv[i+1:]
			}
			if cn, ok := g.enumConstName(f.TypeName, p.Name, dv); ok {
				fmt.Fprintf(b, "\tp.Set%s(%s)\n", method, cn)
			} else {
				fmt.Fprintf(b, "\tp.Set%s(%s)\n", method, dv)
			}
		case "Byte", "ShortLittleEndian", "ShortBigEndian", "IntegerLittleEndian", "IntegerBigEndian",
			"LongLittleEndian", "LongBigEndian", "Float", "Double":
			fmt.Fprintf(b, "\tp.Set%s(%s)\n", method, *f.DefaultValue)
		}
	}
}

// emitRequiredSize 对齐 C# GetRequiredSize：
// String: index + UTF8 字节数 + 1（结尾 null）；Binary: index + 字节数；Structure[]: index + count*stride。
func (g *generator) emitRequiredSize(b *strings.Builder, goName, ownerPacket string, f *Field) error {
	switch f.Type {
	case "String":
		fmt.Fprintf(b, "func %sRequiredSize(contentBytes int) int { return contentBytes + 1 + %d }\n\n", goName, f.Index)
	case "Binary":
		fmt.Fprintf(b, "func %sRequiredSize(contentLen int) int { return contentLen + %d }\n\n", goName, f.Index)
	case "Structure[]":
		structGo, def, err := g.resolveStruct(f.TypeName, ownerPacket)
		if err != nil {
			return err
		}
		if def.Length != nil {
			fmt.Fprintf(b, "func %sRequiredSize(count int) int { return count*%sLength + %d }\n\n",
				goName, structGo, f.Index)
		} else {
			fmt.Fprintf(b, "func %sRequiredSize(count, stride int) int { return count*stride + %d }\n\n", goName, f.Index)
		}
	}
	return nil
}
