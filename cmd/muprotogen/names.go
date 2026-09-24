package main

import (
	"strings"
	"unicode"
)

// sanitizeName 复刻 XSLT 中 translate(., ' ()/-:', ”)，并扩展为合法 Go 标识符。
func sanitizeName(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case ' ', '(', ')', '/', '-', ':', '.':
			continue
		}
		if r > 127 || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if s == "" {
		s = "X"
	}
	if unicode.IsDigit(rune(s[0])) {
		s = "P" + s
	}
	return exportize(s)
}

// exportize 保证标识符首字母大写（导出）。
func exportize(s string) string {
	if s == "" {
		return s
	}
	first := []rune(s)[0]
	if unicode.IsLower(first) {
		return strings.ToUpper(string(first)) + s[len(string(first)):]
	}
	return s
}

// allocator 在单个 Go 包内分配唯一名字。
type allocator struct {
	used map[string]bool
}

func newAllocator() *allocator {
	return &allocator{used: map[string]bool{}}
}

// take 申请名字；冲突时追加数字后缀。
func (a *allocator) take(name string) string {
	if !a.used[name] {
		a.used[name] = true
		return name
	}
	for i := 2; ; i++ {
		cand := name + itoa(i)
		if !a.used[cand] {
			a.used[cand] = true
			return cand
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
