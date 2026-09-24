// Command muprotogen 从 OpenMU 的 *Packets.xml 生成 Go 协议视图代码。
// 升级协议版本时：修改 protocol.gen.json 的 packets_version/xml_root，执行 go generate ./...。
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	configPath := flag.String("config", "protocol.gen.json", "生成配置文件路径")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "muprotogen:", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	configDir := filepath.Dir(absConfig)

	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		files, err := loadTarget(cfg, t, configDir)
		if err != nil {
			return err
		}
		g, err := newGenerator(cfg.PacketsVersion, t, files)
		if err != nil {
			return err
		}
		if err := writeTarget(configDir, t, g); err != nil {
			return err
		}
	}
	return nil
}

func writeTarget(configDir string, t *TargetConfig, g *generator) error {
	out, warnings, err := g.Generate()
	if err != nil {
		return err
	}

	outDir := t.Output
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(configDir, outDir)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// 删除旧生成物，防止改名后的陈旧文件残留；helpers.go 为手写文件，保留。
	old, _ := filepath.Glob(filepath.Join(outDir, "*.gen.go"))
	oldTests, _ := filepath.Glob(filepath.Join(outDir, "*.gen_test.go"))
	for _, p := range append(old, oldTests...) {
		if err := os.Remove(p); err != nil {
			return err
		}
	}

	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(out[name]), 0o644); err != nil {
			return err
		}
	}

	helpersPath := filepath.Join(outDir, "helpers.go")
	if _, err := os.Stat(helpersPath); os.IsNotExist(err) {
		formatted, err := format.Source([]byte(renderHelpers(t.Package)))
		if err != nil {
			return fmt.Errorf("gofmt 失败 helpers.go: %w", err)
		}
		if err := os.WriteFile(helpersPath, formatted, 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("muprotogen: 包版本=%s 目标=%s 包数=%d 结构=%d 输出=%s\n",
		g.version, t.Package, len(g.full.Packets), len(g.structGo), outDir)
	if len(warnings) > 0 {
		fmt.Printf("注意（%d）:\n", len(warnings))
		for _, w := range warnings {
			fmt.Println("  -", w)
		}
	}
	return nil
}
