package config

// chat_commands_test.go —— 聊天命令导出件的载入、过滤与校验（TRIM-08b）。
// 数值口径来自 data/season6/97_chat_commands.json（tools/goldenconfig 用原版
// ChatCommandTypeExtensions.TryCreateChatCommandInfo 反射求值）。

import (
	"strings"
	"testing"
)

func TestChatCommandsLoaded(t *testing.T) {
	cfg := mustLoad(t)
	if got := len(cfg.ChatCommands); got != 65 {
		t.Fatalf("命令数 %d, want 65", got)
	}
	if got := cfg.Meta.Counts.ChatCommands; got != 65 {
		t.Fatalf("meta 计数 %d, want 65", got)
	}
	add, ok := cfg.ChatCommandByCommand("/add")
	if !ok {
		t.Fatal("/add 缺失")
	}
	if add.Usage != "/add {StatType:agi|cmd|ene|str|vit} {Amount:UInt16} " {
		t.Fatalf("用法串与原版 CreateUsage 不一致: %q", add.Usage)
	}
	if len(add.Parameters) != 2 || add.Parameters[0].Name != "StatType" ||
		add.Parameters[1].TypeName != "UInt16" {
		t.Fatalf("参数表异常: %+v", add.Parameters)
	}
	if !add.Parameters[0].ValueAllowed("agi") || add.Parameters[0].ValueAllowed("hp") {
		t.Fatal("ValidValues 判定不符（agi 允许、hp 不允许）")
	}
	// 无枚举集合的参数恒允许。
	if !add.Parameters[1].ValueAllowed("123") {
		t.Fatal("无 ValidValues 应允许任意值")
	}
}

// TestChatCommandsActivationSplit 锁住"默认停用"这一事实：原版 65 条里 22 条
// 实现 IDisabledByDefault，既不进可用列表也不能执行（DataInitializationBase.cs:135-138）。
func TestChatCommandsActivationSplit(t *testing.T) {
	cfg := mustLoad(t)
	enabled, disabled := 0, 0
	for i := range cfg.ChatCommands {
		if cfg.ChatCommands[i].EnabledByDefault {
			enabled++
		} else {
			disabled++
		}
	}
	if enabled != 43 || disabled != 22 {
		t.Fatalf("激活/停用 = %d/%d, want 43/22", enabled, disabled)
	}
	// 停用的一条：/addstr（原版连 GetStrategy 都解析不到）。
	if cmd, ok := cfg.ChatCommandByCommand("/addstr"); !ok || cmd.EnabledByDefault {
		t.Fatalf("/addstr 应存在且默认停用, got %+v", cmd)
	}
}

// TestAvailableChatCommands 对照 ChatCommandTypeExtensions.GetAvailableCommands：
// 只给"已激活且状态达标"的命令（原版比较是 `CharacterStatus >= Help.MinimumCharacterStatus`）。
func TestAvailableChatCommands(t *testing.T) {
	cfg := mustLoad(t)
	normal := cfg.AvailableChatCommands(0)
	gm := cfg.AvailableChatCommands(32)
	if len(normal) != 13 {
		t.Fatalf("普通玩家可用命令 %d 条, want 13", len(normal))
	}
	if len(gm) != 43 {
		t.Fatalf("GM 可用命令 %d 条, want 43", len(gm))
	}
	// 普通玩家看不到 GM 命令，也看不到停用的。
	for _, cmd := range normal {
		if cmd.MinimumCharacterStatus != 0 || !cmd.EnabledByDefault {
			t.Fatalf("普通玩家列表混入 %s（状态 %d、激活 %v）",
				cmd.Command, cmd.MinimumCharacterStatus, cmd.EnabledByDefault)
		}
	}
	if _, ok := findChatCommand(normal, "/teleport"); ok {
		t.Fatal("普通玩家不该看到 /teleport")
	}
	if _, ok := findChatCommand(gm, "/teleport"); !ok {
		t.Fatal("GM 应看到 /teleport")
	}
	if _, ok := findChatCommand(gm, "/addstr"); ok {
		t.Fatal("停用的 /addstr 不该出现在任何列表")
	}
}

func findChatCommand(list []*ChatCommand, command string) (*ChatCommand, bool) {
	for _, c := range list {
		if c.Command == command {
			return c, true
		}
	}
	return nil, false
}

func TestValidateChatCommandsRejectsBrokenTables(t *testing.T) {
	cfg := mustLoad(t)
	good := cfg.ChatCommands
	defer func() { cfg.ChatCommands = good }()

	cases := []struct {
		name     string
		commands []ChatCommand
		count    int
	}{
		{"计数与 meta 不一致", good, 3},
		{"键不以 / 开头", []ChatCommand{{Command: "help", EnabledByDefault: true}}, 1},
		{"键含空格", []ChatCommand{{Command: "/a b", EnabledByDefault: true}}, 1},
		{"最小状态不在枚举内", []ChatCommand{{Command: "/x", MinimumCharacterStatus: 7, EnabledByDefault: true}}, 1},
		{"参数缺名字", []ChatCommand{{Command: "/x", Parameters: []ChatCommandParameter{{TypeName: "String"}}}}, 1},
		{"参数缺类型", []ChatCommand{{Command: "/x", Parameters: []ChatCommandParameter{{Name: "A"}}}}, 1},
	}
	for _, tc := range cases {
		cfg.ChatCommands = tc.commands
		cfg.Meta.Counts.ChatCommands = tc.count
		if err := cfg.validateChatCommands(); err == nil {
			t.Fatalf("%s：应校验失败", tc.name)
		}
		if err := cfg.validateChatCommands(); err != nil && !strings.Contains(err.Error(), "命令") &&
			!strings.Contains(err.Error(), "参数") {
			t.Fatalf("%s：错误信息不含线索: %v", tc.name, err)
		}
	}
}

// 空表 + 计数 0 必须放行（旧导出件里没有 97 文件时的行为）。
func TestValidateChatCommandsAllowsEmpty(t *testing.T) {
	cfg := mustLoad(t)
	cfg.ChatCommands = nil
	cfg.Meta.Counts.ChatCommands = 0
	if err := cfg.validateChatCommands(); err != nil {
		t.Fatalf("空表应放行: %v", err)
	}
}
