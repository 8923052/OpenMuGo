package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// loadConfig 读取生成配置。
func loadConfig(path string) (*GenConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg GenConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s: %w", path, err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("配置缺少 targets")
	}
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.Package == "" || t.Output == "" || t.Full == "" {
			return nil, fmt.Errorf("targets[%d] 缺少 package/output/full", i)
		}
	}
	return &cfg, nil
}

// xmlPath 解析一个 XML 相对路径（相对于 xml_root）。
func xmlPath(configDir, xmlRoot, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(configDir, xmlRoot, rel)
}

// loadDefinitions 读取并解析单个 XML。
func loadDefinitions(path, label string) (*Definitions, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", label, err)
	}
	var defs Definitions
	if err := xml.Unmarshal(raw, &defs); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", label, err)
	}
	return &defs, nil
}

// scopeOf 以 XML 所在目录名作为 scope（与 C# 子命名空间一致，
// 如 ClientToServer/ClientToServerPackets.xml -> ClientToServer）。
func scopeOf(rel string) string {
	dir := filepath.Dir(filepath.ToSlash(rel))
	if dir == "." || dir == "" {
		return "common"
	}
	return filepath.Base(dir)
}

// loadTarget 按目标配置解析全部输入 XML，返回 scope -> Definitions。
func loadTarget(cfg *GenConfig, t *TargetConfig, configDir string) (map[string]*Definitions, error) {
	files := make(map[string]*Definitions)
	add := func(rel string) error {
		defs, err := loadDefinitions(xmlPath(configDir, cfg.XMLRoot, rel), rel)
		if err != nil {
			return err
		}
		scope := scopeOf(rel)
		if filepath.Base(rel) == "CommonEnums.xml" {
			scope = "common"
		}
		if _, exists := files[scope]; exists {
			return fmt.Errorf("目标 %s 内 scope 重复: %s", t.Package, scope)
		}
		files[scope] = defs
		return nil
	}
	for _, rel := range cfg.CommonInputs {
		if err := add(rel); err != nil {
			return nil, err
		}
	}
	if err := add(t.Full); err != nil {
		return nil, err
	}
	for _, rel := range t.EnumRefs {
		if err := add(rel); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// enumKey 为枚举实例生成解析键。
//   - 公共枚举:   common:<name>
//   - 文件全局:   global:<scope>:<name>
//   - 包内局部:   local:<scope>:<packet>:<name>
func enumKey(kind, scope, packetOrName, name string) string {
	switch kind {
	case "common":
		return "common::" + packetOrName
	case "global":
		return "global:" + scope + ":" + packetOrName
	default:
		return "local:" + scope + ":" + packetOrName + ":" + name
	}
}

// buildEnumPool 汇总所有输入文件中的枚举。
// 返回 key -> Enum。
func buildEnumPool(files map[string]*Definitions) map[string]Enum {
	pool := make(map[string]Enum)
	for scope, defs := range files {
		for _, e := range defs.Enums {
			if scope == "common" {
				pool[enumKey("common", "", e.Name, "")] = e
			} else {
				pool[enumKey("global", scope, e.Name, "")] = e
			}
		}
		for _, p := range defs.Packets {
			for _, e := range p.Enums {
				pool[enumKey("local", scope, p.Name, e.Name)] = e
			}
		}
	}
	return pool
}

// buildStructPool 汇总 full 文件内全部结构（全局 + 包内局部）。
func buildStructPool(fullScope string, full *Definitions) map[string]Structure {
	pool := make(map[string]Structure)
	for _, s := range full.Structures {
		pool["global:"+fullScope+":"+s.Name] = s
	}
	for _, p := range full.Packets {
		for _, s := range p.Structures {
			pool["local:"+fullScope+":"+p.Name+":"+s.Name] = s
		}
	}
	return pool
}
