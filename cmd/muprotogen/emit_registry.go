package main

import (
	"fmt"
	"sort"
	"strings"
)

// emitRegistry 输出 (HeaderType, Code[, SubCode]) 路由表，替代 C++ 端手写 switch。
func (g *generator) emitRegistry() string {
	routes := make([]routeEntry, len(g.routes))
	copy(routes, g.routes)
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		if a.header != b.header {
			return a.header < b.header
		}
		if a.code != b.code {
			return a.code < b.code
		}
		if a.hasSub != b.hasSub {
			return !a.hasSub // 无 sub 的条目排前面
		}
		return a.sub < b.sub
	})

	var b strings.Builder
	b.WriteString(g.pkgHeader())
	b.WriteString("// RouteKey 是包分发表的键，对应线上帧头。\n")
	b.WriteString("type RouteKey struct {\n\tHeader byte\n\tCode   byte\n\tHasSub bool\n\tSub    byte\n}\n\n")
	b.WriteString("// PacketInfo 描述一个已注册包的静态信息。\n")
	b.WriteString("type PacketInfo struct {\n\tName      string\n\tDirection string\n\tFixed     bool\n\tLength    int\n}\n\n")
	b.WriteString("type routeEntry struct {\n\tKey  RouteKey\n\tInfo PacketInfo\n}\n\n")
	b.WriteString("var routes = []routeEntry{\n")
	for _, r := range routes {
		fmt.Fprintf(&b, "\t{RouteKey{0x%02X, 0x%02X, %t, 0x%02X}, PacketInfo{%q, %q, %t, %d}},\n",
			r.header, r.code, r.hasSub, r.sub, r.goName, r.direction, r.fixed, r.length)
	}
	b.WriteString("}\n\n")

	b.WriteString("// Lookup 按精确键查包；重复键返回最先注册的条目。\n")
	b.WriteString("func Lookup(key RouteKey) (PacketInfo, bool) {\n")
	b.WriteString("\tfor _, e := range routes {\n")
	b.WriteString("\t\tif e.Key == key {\n\t\t\treturn e.Info, true\n\t\t}\n\t}\n")
	b.WriteString("\treturn PacketInfo{}, false\n}\n\n")

	b.WriteString("// LookupPacket 直接从一帧原始字节判定路由（C1/C3 的 Code 在偏移2，C2/C4 在偏移3）。\n")
	b.WriteString("// 同时存在带/不带 SubCode 的条目时，优先匹配带 SubCode 的精确键。\n")
	b.WriteString("func LookupPacket(buf []byte) (PacketInfo, bool) {\n")
	b.WriteString("\tif len(buf) < 3 {\n\t\treturn PacketInfo{}, false\n\t}\n")
	b.WriteString("\theader := buf[0]\n")
	b.WriteString("\tcodeIdx := 2\n")
	b.WriteString("\tif header == 0xC2 || header == 0xC4 {\n\t\tcodeIdx = 3\n\t}\n")
	b.WriteString("\tif len(buf) <= codeIdx {\n\t\treturn PacketInfo{}, false\n\t}\n")
	b.WriteString("\tcode := buf[codeIdx]\n")
	b.WriteString("\tif len(buf) > codeIdx+1 {\n")
	b.WriteString("\t\tif info, ok := Lookup(RouteKey{header, code, true, buf[codeIdx+1]}); ok {\n")
	b.WriteString("\t\t\treturn info, true\n\t\t}\n\t}\n")
	b.WriteString("\treturn Lookup(RouteKey{header, code, false, 0})\n}\n")
	return b.String()
}
