package config

// chat_commands.go —— 聊天命令表（TRIM-08）。原版这份元数据不在 GameConfiguration 里，
// 而是**代码特性 + 本地化资源**：[ChatCommandHelp]（命令串/参数类型/最小角色状态）、
// [Argument]/[ValidValues]（参数名/shortName/必填/枚举值）、[Display]+PlugInResources
// （名字与描述），运行期由反射与 PlugInManager 组合。Go 没有反射式插件容器，故由
// tools/goldenconfig 在导出期调原版公开的 TryCreateChatCommandInfo 求值落盘（97_chat_commands.json）。
//
// 激活状态也是数据：原版 GetAvailableCommands 与 GetStrategy 都只认"已激活"插件
// （PlugInManager.cs:275-278），初始化按 `IsActive = !IsAssignableTo(IDisabledByDefault)` 落库
// （DataInitializationBase.cs:135-138）→ 65 条里 22 条默认停用（/addstr 一族、/get*/set* 一族、
// /clearinv /npc /openware），既不进 F5 列表也不能执行。本表用 enabled_by_default 承载该事实。

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// ChatCommandParameter 是命令的一个参数（顺序 = 不带 shortName 时的入参顺序）。
type ChatCommandParameter struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	// TypeName 保留原版 CLR 类型名（String/Byte/UInt16/Int32/Boolean…）。
	// 到 wire 的 ChatCommandParameterType 映射在视图层（对照 ChatCommandListViewPlugIn.GetParameterType）。
	TypeName    string `json:"type_name"`
	IsRequired  bool   `json:"is_required"`
	ValidValues string `json:"valid_values"` // 原版 '|' 连接；空 = 不限
}

// ValueAllowed 报告给定值是否落在该参数的枚举值集合内（无集合时恒真）。
// 对照原版 ArgumentAttribute 的 ValidValues 校验（大小写不敏感）。
func (p *ChatCommandParameter) ValueAllowed(value string) bool {
	if p.ValidValues == "" {
		return true
	}
	for _, v := range strings.Split(p.ValidValues, "|") {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

// ChatCommand 是一条聊天命令（键含前导 '/'）。
type ChatCommand struct {
	Command                string                 `json:"command"`
	MinimumCharacterStatus byte                   `json:"minimum_character_status"`
	EnabledByDefault       bool                   `json:"enabled_by_default"`
	Name                   string                 `json:"name"`
	Description            string                 `json:"description"`
	Usage                  string                 `json:"usage"`
	Parameters             []ChatCommandParameter `json:"parameters"`
}

// AvailableChatCommands 返回该角色状态下**可执行且会在 F5 列表里出现**的命令
// （对照 ChatCommandTypeExtensions.GetAvailableCommands 的两道过滤：插件已激活、
// `SelectedCharacter.CharacterStatus >= Help.MinimumCharacterStatus`）。
func (c *GameConfig) AvailableChatCommands(status byte) []*ChatCommand {
	out := make([]*ChatCommand, 0, len(c.ChatCommands))
	for i := range c.ChatCommands {
		cmd := &c.ChatCommands[i]
		if cmd.EnabledByDefault && status >= cmd.MinimumCharacterStatus {
			out = append(out, cmd)
		}
	}
	return out
}

// ChatCommandByCommand 按命令串（含 '/'）查定义。
func (c *GameConfig) ChatCommandByCommand(command string) (*ChatCommand, bool) {
	cmd, ok := c.chatCommandByKey[command]
	return cmd, ok
}

// loadChatCommands 读 97_chat_commands.json；缺文件容忍为空（旧导出件）。
func (c *GameConfig) loadChatCommands(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "97_chat_commands.json")
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(raw, &c.ChatCommands); err != nil {
		return fmt.Errorf("config: 解析 97_chat_commands.json: %w", err)
	}
	c.chatCommandByKey = make(map[string]*ChatCommand, len(c.ChatCommands))
	for i := range c.ChatCommands {
		cmd := &c.ChatCommands[i]
		if _, dup := c.chatCommandByKey[cmd.Command]; dup {
			return fmt.Errorf("config: 聊天命令 %s 重复", cmd.Command)
		}
		c.chatCommandByKey[cmd.Command] = cmd
	}
	return nil
}

// validateChatCommands 校验命令表与 meta 计数一致、键形态合法。
func (c *GameConfig) validateChatCommands() error {
	if got, want := len(c.ChatCommands), c.Meta.Counts.ChatCommands; got != want {
		return fmt.Errorf("聊天命令数 %d 与 meta 计数 %d 不一致", got, want)
	}
	for i := range c.ChatCommands {
		cmd := &c.ChatCommands[i]
		if !strings.HasPrefix(cmd.Command, "/") || strings.ContainsAny(cmd.Command, " \t") {
			return fmt.Errorf("聊天命令 %q 形态非法（须以 / 开头且不含空格）", cmd.Command)
		}
		switch cmd.MinimumCharacterStatus {
		case 0, 1, 32: // 原版 CharacterStatus：Normal / Banned / GameMaster
		default:
			return fmt.Errorf("聊天命令 %s 的最小状态 %d 不在 CharacterStatus 枚举内",
				cmd.Command, cmd.MinimumCharacterStatus)
		}
		for _, p := range cmd.Parameters {
			if p.Name == "" || p.TypeName == "" {
				return fmt.Errorf("聊天命令 %s 有参数缺名字或类型", cmd.Command)
			}
		}
	}
	return nil
}
