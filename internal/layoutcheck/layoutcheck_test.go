// Package layoutcheck 用 go list 静态校验分层与 import 方向。
//
// 这是 doc/14 §4 的「第 6 条防线」：把原来靠人读代码证明的约束变成机器检查。
// 只读 go list 输出，不 import 被测包；断言失败即测试失败，随 go test ./... 生效。
package layoutcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pkg 是 go list -json 的裁剪结构。
type pkg struct {
	ImportPath string
	Imports    []string
	GoFiles    []string
	Dir        string
}

// list 返回模块内全部包。
//
// go list -json 输出的是**连续拼接**的 JSON 对象（不是数组，对象之间也没有分隔符），
// 必须用 json.Decoder 反复 Decode 直到 io.EOF。
func list(t *testing.T) []pkg {
	t.Helper()
	pkgs, err := listPkgs()
	if err != nil {
		t.Fatalf("go list 失败: %v", err)
	}
	return pkgs
}

// listPkgs 是 list 的实现，便于把环境问题与解码问题分开定位。
//
// 关键：go test 会把工作目录切到**被测包目录**，所以 `go list ./...` 只会列出
// internal/layoutcheck 自己。必须先定位模块根（go.mod 所在目录）再执行。
func listPkgs() ([]pkg, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("找不到 go 可执行文件: %w", err)
	}
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(goBin, "list", "-json", "./...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list 退出异常: %w; stderr=%s", err, stderr.String())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("go list 无输出（%s，模块根=%s）", goBin, root)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []pkg
	for {
		var p pkg
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 go list 输出失败: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// moduleRoot 从当前工作目录向上找含 go.mod 的目录。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 向上未找到 go.mod", dir)
		}
		dir = parent
	}
}

// rule 是一条 import 方向约束。
type rule struct {
	name string
	// applies 判断该规则是否适用于某包。
	applies func(importPath string) bool
	// forbidden 返回该包**不允许**出现的 import 前缀。
	forbidden func(importPath string) []string
}

// allowedExceptions 是显式登记的违规豁免（key=包路径，value=允许的 import 前缀）。
// 每条豁免必须在包文档与 doc/14 里说明理由，不许静默增加。
var allowedExceptions = map[string]map[string]bool{
	// seedtest 是测试账号种子：要预编码外观字节，而外观编码按铁律只能放 view/remote；
	// 包内无版本判断，不违反版本无关铁律。接 DB 后本包整体弃用。
	"mugo/internal/persistence/seedtest": {
		"mugo/internal/view/": true,
	},
}

// TestImportDirection 校验分层铁律：只有"外层依赖内层"一个方向。
func TestImportDirection(t *testing.T) {
	rules := []rule{
		{
			name:    "internal/api 是契约层，不得 import 任何实现层",
			applies: func(p string) bool { return p == "mugo/internal/api" },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/server/",
					"mugo/internal/gamelogic/",
					"mugo/internal/persistence",
					"mugo/internal/version",
					"mugo/internal/proto/",
					"mugo/internal/transport",
					"mugo/internal/view/",
				}
			},
		},
		{
			name:    "gamelogic 版本无关：不得 import version / proto / server / view / transport",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/gamelogic") },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/version",
					"mugo/internal/proto/",
					"mugo/internal/server/",
					"mugo/internal/view/",
					"mugo/internal/transport",
				}
			},
		},
		{
			name:    "persistence 版本无关且不碰传输：不得 import version / proto / server / view / transport",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/persistence") },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/version",
					"mugo/internal/proto/",
					"mugo/internal/server/",
					"mugo/internal/view/",
					"mugo/internal/transport",
				}
			},
		},
		{
			name:    "transport 是最内层：不得 import gamelogic / server / proto / view / api",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/transport") },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/gamelogic/",
					"mugo/internal/server/",
					"mugo/internal/proto/",
					"mugo/internal/view/",
					"mugo/internal/api",
				}
			},
		},
		{
			name:    "version 是叶子：不得 import gamelogic / server / proto / transport / view / api",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/version") },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/gamelogic/",
					"mugo/internal/server/",
					"mugo/internal/proto/",
					"mugo/internal/transport",
					"mugo/internal/view/",
					"mugo/internal/api",
				}
			},
		},
		{
			name:    "view 不依赖 server / api",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/view") },
			forbidden: func(string) []string {
				return []string{"mugo/internal/server/", "mugo/internal/api"}
			},
		},
		{
			name:    "proto 只依赖 transport（不碰业务层）",
			applies: func(p string) bool { return strings.HasPrefix(p, "mugo/internal/proto") },
			forbidden: func(string) []string {
				return []string{
					"mugo/internal/gamelogic/",
					"mugo/internal/server/",
					"mugo/internal/view/",
					"mugo/internal/version",
					"mugo/internal/api",
				}
			},
		},
	}

	pkgs := list(t)
	if len(pkgs) == 0 {
		t.Fatal("go list 未返回任何包")
	}

	// 记录每个规则命中到的包数，避免规则因为"没有匹配包"而静默失效。
	hits := make([]int, len(rules))
	for _, p := range pkgs {
		for i, r := range rules {
			if !r.applies(p.ImportPath) {
				continue
			}
			hits[i]++
			for _, bad := range r.forbidden(p.ImportPath) {
				for _, imp := range p.Imports {
					if imp == bad || strings.HasPrefix(imp, bad) {
						// 显式登记的豁免不算违规（见 allowedExceptions 注释）。
						if allowedExceptions[p.ImportPath][imp] || allowedExceptions[p.ImportPath][bad] {
							continue
						}
						t.Errorf("[%s] %s 违规 import %s", r.name, p.ImportPath, imp)
					}
				}
			}
		}
	}

	for i, r := range rules {
		if hits[i] == 0 {
			t.Errorf("[%s] 规则未匹配到任何包，规则已失效（包路径改名了？）", r.name)
		}
	}
}
