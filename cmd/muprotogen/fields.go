package main

import (
	"fmt"
	"strings"
)

// fieldContext 描述字段所在容器（包或结构），用于局部类型解析与接收者命名。
type fieldContext struct {
	goName      string
	ownerPacket string // 局部枚举/结构解析用；全局结构为 ""
	recv        string // 接收者变量名，通常为 p
}

// methodMap 为容器内字段分配唯一的访问器方法名。
func methodMap(fields []Field) map[string]string {
	alloc := newAllocator()
	// 保留视图内建方法名，避免字段访问器冲突。
	alloc.take("Bytes")
	alloc.take("Len")
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Name == "" {
			continue
		}
		m[f.Name] = alloc.take(sanitizeName(f.Name))
	}
	return m
}

// emitFields 生成一个容器内全部字段的访问器。
func (g *generator) emitFields(cx fieldContext, fields []Field) (string, error) {
	var b strings.Builder
	methods := methodMap(fields)
	for i := range fields {
		f := &fields[i]
		if f.Name == "" {
			continue
		}
		if f.Type == "Structure[]" && f.customIndexer() {
			// 与 C# partial struct 策略一致：UseCustomIndexer 包只出壳，索引器手写。
			g.warnings = append(g.warnings,
				fmt.Sprintf("%s: 字段 %s 使用 UseCustomIndexer，已跳过自动索引器，需在 custom 钩子中实现", cx.goName, f.Name))
			continue
		}
		code, err := g.emitOneField(cx, f, methods)
		if err != nil {
			return "", err
		}
		b.WriteString(code)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (g *generator) emitOneField(cx fieldContext, f *Field, methods map[string]string) (string, error) {
	r := cx.recv
	method := methods[f.Name]
	var b strings.Builder

	switch f.Type {
	case "Boolean":
		shift := f.shift()
		fmt.Fprintf(&b, "func (%s *%s) %s() bool { return %s.d[%d]&(1<<%d) != 0 }\n", r, cx.goName, method, r, f.Index, shift)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value bool) {\n", r, cx.goName, method)
		fmt.Fprintf(&b, "\tif value { %s.d[%d] |= 1 << %d } else { %s.d[%d] &^= 1 << %d }\n", r, f.Index, shift, r, f.Index, shift)
		b.WriteString("}\n")

	case "Byte":
		g.emitByteAccessors(&b, cx, method, f, "byte", "")

	case "Enum":
		enumGo, err := g.resolveEnum(f.TypeName, cx.ownerPacket)
		if err != nil {
			return "", err
		}
		g.emitByteAccessors(&b, cx, method, f, enumGo, "byte")

	case "ShortLittleEndian", "ShortBigEndian":
		ord := "LE"
		if f.Type == "ShortBigEndian" {
			ord = "BE"
		}
		fmt.Fprintf(&b, "func (%s *%s) %s() uint16 { return getU16%s(%s.d[%d:]) }\n", r, cx.goName, method, ord, r, f.Index)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value uint16) { setU16%s(%s.d[%d:], value) }\n", r, cx.goName, method, ord, r, f.Index)

	case "IntegerLittleEndian", "IntegerBigEndian":
		ord := "LE"
		if f.Type == "IntegerBigEndian" {
			ord = "BE"
		}
		fmt.Fprintf(&b, "func (%s *%s) %s() uint32 { return getU32%s(%s.d[%d:]) }\n", r, cx.goName, method, ord, r, f.Index)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value uint32) { setU32%s(%s.d[%d:], value) }\n", r, cx.goName, method, ord, r, f.Index)

	case "LongLittleEndian", "LongBigEndian":
		ord := "LE"
		if f.Type == "LongBigEndian" {
			ord = "BE"
		}
		fmt.Fprintf(&b, "func (%s *%s) %s() uint64 { return getU64%s(%s.d[%d:]) }\n", r, cx.goName, method, ord, r, f.Index)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value uint64) { setU64%s(%s.d[%d:], value) }\n", r, cx.goName, method, ord, r, f.Index)

	case "Float":
		fmt.Fprintf(&b, "func (%s *%s) %s() float32 { return getF32(%s.d[%d:]) }\n", r, cx.goName, method, r, f.Index)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value float32) { setF32(%s.d[%d:], value) }\n", r, cx.goName, method, r, f.Index)

	case "Double":
		fmt.Fprintf(&b, "func (%s *%s) %s() float64 { return getF64(%s.d[%d:]) }\n", r, cx.goName, method, r, f.Index)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value float64) { setF64(%s.d[%d:], value) }\n", r, cx.goName, method, r, f.Index)

	case "String":
		end := ""
		if f.Length != nil {
			end = fmt.Sprintf("%d", f.Index+*f.Length)
		}
		slice := fmt.Sprintf("%s.d[%d:%s]", r, f.Index, end)
		fmt.Fprintf(&b, "func (%s *%s) %s() []byte { return cstring(%s) }\n", r, cx.goName, method, slice)
		fmt.Fprintf(&b, "func (%s *%s) %sString() string { return string(cstring(%s)) }\n", r, cx.goName, method, slice)
		fmt.Fprintf(&b, "func (%s *%s) Set%s(value string) { writeCString(%s, value) }\n", r, cx.goName, method, slice)

	case "Binary":
		if f.Length != nil {
			fmt.Fprintf(&b, "func (%s *%s) %s() []byte { return %s.d[%d:%d] }\n", r, cx.goName, method, r, f.Index, f.Index+*f.Length)
		} else {
			fmt.Fprintf(&b, "func (%s *%s) %s() []byte { return %s.d[%d:] }\n", r, cx.goName, method, r, f.Index)
		}

	case "Structure[]":
		code, err := g.emitArrayField(cx, f, methods)
		if err != nil {
			return "", err
		}
		b.WriteString(code)

	default:
		return "", fmt.Errorf("%s: 不支持的字段类型 %q", cx.goName, f.Type)
	}
	return b.String(), nil
}

// emitByteAccessors 处理 Byte 与 Enum（共享字节/位压缩语义，对齐 GetByteValue/SetByteValue）。
func (g *generator) emitByteAccessors(b *strings.Builder, cx fieldContext, method string, f *Field, goType string, castFrom string) {
	r := cx.recv
	getExpr := fmt.Sprintf("%s.d[%d]", r, f.Index)
	setTarget := fmt.Sprintf("%s.d[%d]", r, f.Index)
	if f.isBitPacked() {
		bits, shift := f.bits(), f.shift()
		getExpr = fmt.Sprintf("byteBits(%s.d[%d], %d, %d)", r, f.Index, bits, shift)
		setTarget = "&" + setTarget
		fmt.Fprintf(b, "func (%s *%s) %s() %s { return %s(%s) }\n", r, cx.goName, method, goType, goType, getExpr)
		fmt.Fprintf(b, "func (%s *%s) Set%s(value %s) { setByteBits(%s, byte(value), %d, %d) }\n", r, cx.goName, method, goType, setTarget, bits, shift)
		return
	}
	if castFrom == "" {
		fmt.Fprintf(b, "func (%s *%s) %s() %s { return %s }\n", r, cx.goName, method, goType, getExpr)
		fmt.Fprintf(b, "func (%s *%s) Set%s(value %s) { %s = byte(value) }\n", r, cx.goName, method, goType, setTarget)
		return
	}
	fmt.Fprintf(b, "func (%s *%s) %s() %s { return %s(%s) }\n", r, cx.goName, method, goType, goType, getExpr)
	fmt.Fprintf(b, "func (%s *%s) Set%s(value %s) { %s = byte(value) }\n", r, cx.goName, method, goType, setTarget)
}

// emitArrayField 处理 Structure[] 字段（固定跨步或变长跨步）。
func (g *generator) emitArrayField(cx fieldContext, f *Field, methods map[string]string) (string, error) {
	structGo, def, err := g.resolveStruct(f.TypeName, cx.ownerPacket)
	if err != nil {
		return "", err
	}
	r := cx.recv
	method := methods[f.Name]
	countExpr := ""
	if f.ItemCountField != "" {
		countMethod, ok := methods[f.ItemCountField]
		if !ok {
			return "", fmt.Errorf("%s: 数组 %s 的 ItemCountField=%s 未找到", cx.goName, f.Name, f.ItemCountField)
		}
		countExpr = fmt.Sprintf("int(%s.%s())", r, countMethod)
	} else if def.Length != nil {
		countExpr = fmt.Sprintf("(len(%s.d)-%d)/%sLength", r, f.Index, structGo)
	} else {
		return "", fmt.Errorf("%s: 变长结构数组 %s 缺少 ItemCountField", cx.goName, f.Name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "func (%s *%s) %sCount() int { return %s }\n", r, cx.goName, method, countExpr)

	if def.Length != nil {
		stride := fmt.Sprintf("%sLength", structGo)
		fmt.Fprintf(&b, "func (%s *%s) %s(i int) *%s {\n", r, cx.goName, method, structGo)
		fmt.Fprintf(&b, "\tcount := %s\n", countExpr)
		fmt.Fprintf(&b, "\tstart := %d + i*%s\n", f.Index, stride)
		fmt.Fprintf(&b, "\tif i < 0 || i >= count || start+%s > len(%s.d) {\n\t\treturn nil\n\t}\n", stride, r)
		fmt.Fprintf(&b, "\treturn As%s(%s.d[start:])\n}\n", structGo, r)
	} else {
		fmt.Fprintf(&b, "func (%s *%s) %s(i, stride int) *%s {\n", r, cx.goName, method, structGo)
		fmt.Fprintf(&b, "\tcount := %s\n", countExpr)
		fmt.Fprintf(&b, "\tif stride <= 0 { return nil }\n")
		fmt.Fprintf(&b, "\tstart := %d + i*stride\n", f.Index)
		fmt.Fprintf(&b, "\tif i < 0 || i >= count || start+stride > len(%s.d) {\n\t\treturn nil\n\t}\n", r)
		fmt.Fprintf(&b, "\treturn As%s(%s.d[start:])\n}\n", structGo, r)
	}
	return b.String(), nil
}
