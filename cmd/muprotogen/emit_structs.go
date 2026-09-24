package main

import (
	"fmt"
	"strings"
)

// emitStructures 输出全部结构视图（全局 + 包内局部）。
func (g *generator) emitStructures() (string, error) {
	var b strings.Builder
	b.WriteString(g.pkgHeader())

	for _, s := range g.full.Structures {
		if err := g.writeOneStructure(&b, s, ""); err != nil {
			return "", err
		}
	}
	for _, p := range g.full.Packets {
		for _, s := range p.Structures {
			if err := g.writeOneStructure(&b, s, p.Name); err != nil {
				return "", err
			}
		}
	}
	return b.String(), nil
}

func (g *generator) writeOneStructure(b *strings.Builder, s Structure, ownerPacket string) error {
	key := "global:" + g.fullScope + ":" + s.Name
	if ownerPacket != "" {
		key = "local:" + g.fullScope + ":" + ownerPacket + ":" + s.Name
	}
	goName := g.structGo[key]
	if s.Description != "" {
		b.WriteString("// " + goName + " " + oneLine(s.Description) + "\n")
	}
	b.WriteString("type " + goName + " struct{ d []byte }\n\n")

	if s.Length != nil {
		fmt.Fprintf(b, "const %sLength = %d\n\n", goName, *s.Length)
		g.fixedStructs = append(g.fixedStructs, fixedStruct{goName: goName, size: *s.Length})
	}
	fmt.Fprintf(b, "func As%s(buf []byte) *%s { return &%s{d: buf} }\n", goName, goName, goName)
	fmt.Fprintf(b, "func (s *%s) Bytes() []byte { return s.d }\n", goName)
	fmt.Fprintf(b, "func (s *%s) Len() int { return len(s.d) }\n\n", goName)

	code, err := g.emitFields(fieldContext{goName: goName, ownerPacket: ownerPacket, recv: "s"}, s.Fields)
	if err != nil {
		return err
	}
	b.WriteString(code)
	return nil
}
