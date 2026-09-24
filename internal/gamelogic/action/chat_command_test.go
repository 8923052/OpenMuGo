package action

// chat_command_test.go —— 频道前缀表与命令参数解析（TRIM-08c/08d）。
// 参数表一律取真实导出件（config.LoadSeason6），避免自造元数据把解析规则"测成自己想要的样子"。

import (
	"testing"

	"mugo/internal/gamelogic/config"
)

func mustConfig(t *testing.T) *config.GameConfig {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestChatChannelOfPrefixTable(t *testing.T) {
	cases := []struct {
		message string
		whisper bool
		want    ChatChannel
	}{
		{"hello", false, ChatChannelNormal},
		{"", false, ChatChannelNormal},
		{"~party talk", false, ChatChannelParty},
		{"@@alliance talk", false, ChatChannelAlliance}, // @@ 必须先于 @ 匹配
		{"@guild talk", false, ChatChannelGuild},
		{"$gens talk", false, ChatChannelGens},
		{"!notice", false, ChatChannelGlobalNotification},
		{"/list", false, ChatChannelCommand},
		{"@@", false, ChatChannelAlliance},
		// 前缀只在开头算数（原版 StartsWith）。
		{"hi @someone", false, ChatChannelNormal},
		{"whatever", true, ChatChannelWhisper}, // 私聊由包种类判定，与前缀无关
	}
	for _, tc := range cases {
		if got := ChatChannelOf(tc.message, tc.whisper); got != tc.want {
			t.Fatalf("%q(whisper=%v) → %d, want %d", tc.message, tc.whisper, got, tc.want)
		}
	}
}

func TestParseChatArgumentsPositional(t *testing.T) {
	cfg := mustConfig(t)
	add, _ := cfg.ChatCommandByCommand("/add")

	args := ParseChatArguments(add, "/add agi 5")
	if !args.Runnable || args.String("StatType") != "agi" || args.Uint16("Amount") != 5 {
		t.Fatalf("基本位置参数解析失败: %+v", args)
	}

	// 无 [Argument] 特性的命令：required = 给出的个数 → 永不因个数被拒；多给的忽略。
	if args := ParseChatArguments(add, "/add agi 5 extra"); !args.Runnable || args.TooFewArguments {
		t.Fatalf("多给一个参数应被忽略: %+v", args)
	}
	// 缺末尾参数不报错——该属性保持默认 0（原版仍返回 instance）。
	if args := ParseChatArguments(add, "/add agi"); !args.Runnable || args.Uint16("Amount") != 0 {
		t.Fatalf("缺 Amount 应仍执行且取值为 0: %+v", args)
	}
	// 类型转换失败：发蓝字、仍执行、该值为 0（对照 TryParseArgumentsAsync 的 success 不参与返回）。
	args = ParseChatArguments(add, "/add agi xx")
	if !args.Runnable || len(args.TypeFailures) != 1 {
		t.Fatalf("类型失败应仍执行并记 1 条: %+v", args)
	}
	if args.TypeFailures[0].Parameter != "Amount" || args.TypeFailures[0].TypeName != "UInt16" {
		t.Fatalf("失败项内容错: %+v", args.TypeFailures[0])
	}
	if args.Uint16("Amount") != 0 {
		t.Fatal("转换失败的参数应保持未赋值（读到 0）")
	}

	// 原版 Split(' ') 的空串残留：多一个空格就会让位置错位——StatType 拿到空串，
	// Amount 收到 "agi" 而转换失败；但命令照旧执行（原版此时仍返回 instance）。
	args = ParseChatArguments(add, "/add  agi 5")
	if !args.Runnable || len(args.TypeFailures) != 1 || args.String("StatType") != "" {
		t.Fatalf("双空格应把 StatType 打成空串、Amount 收到 \"agi\" 而类型失败: %+v", args)
	}
	if args.TypeFailures[0].Parameter != "Amount" {
		t.Fatalf("错位后失败项应是 Amount: %+v", args.TypeFailures)
	}
}

func TestParseChatArgumentsRequiredCount(t *testing.T) {
	cfg := mustConfig(t)
	move, _ := cfg.ChatCommandByCommand("/move") // 只有 Target 带 [Argument(IsRequired=true)]

	// 必填数按"带特性的必填项"算（=1），给够就通过；X/Y 缺省不报错。
	if args := ParseChatArguments(move, "/move Someone"); !args.Runnable || args.String("Target") != "Someone" {
		t.Fatalf("位置模式应只按 1 个必填判定: %+v", args)
	}
	args := ParseChatArguments(move, "/move")
	if args.Runnable || !args.TooFewArguments || args.RequiredCount != 1 || args.GivenCount != 0 {
		t.Fatalf("缺必填应报个数不符(1,0): %+v", args)
	}
	// 全给了位置参数 → 正常。
	if args := ParseChatArguments(move, "/move Someone 0 10 12"); args.String("MapIdOrName") != "0" ||
		args.String("X") != "10" || args.String("Y") != "12" {
		t.Fatalf("四个位置参数应各归各位: %+v", args)
	}
}

func TestParseChatArgumentsNamed(t *testing.T) {
	cfg := mustConfig(t)
	move, _ := cfg.ChatCommandByCommand("/move")

	// 短名记法：顺序随意，未给的选填留空。
	args := ParseChatArguments(move, "/move x=10 target=Someone")
	if !args.Runnable || args.String("Target") != "Someone" || args.String("X") != "10" {
		t.Fatalf("短名解析失败: %+v", args)
	}
	// 缺必填 → 逐条 RequiredArgumentMissing，且不执行。
	args = ParseChatArguments(move, "/move mapIdOrName=Lorencia")
	if args.Runnable || len(args.MissingRequired) != 1 || args.MissingRequired[0] != "Target" {
		t.Fatalf("缺必填 Target 应不执行: %+v", args)
	}
	// 短名模式下类型转换失败 → 原版 return false，命令不执行。
	fireworks, _ := cfg.ChatCommandByCommand("/fireworks")
	args = ParseChatArguments(fireworks, "/fireworks x=abc y=1")
	if args.Runnable || len(args.TypeFailures) != 1 || args.TypeFailures[0].Parameter != "X" {
		t.Fatalf("短名模式转换失败应中止: %+v", args)
	}
	// 值里再出现 "x=" 时按原版 Replace 全删（"x=x=5" → "5"）。
	args = ParseChatArguments(fireworks, "/fireworks x=x=5 y=1")
	if !args.Runnable || args.String("X") != "5" {
		t.Fatalf("值内的短名前缀应被剥掉: %+v", args)
	}
	// 整条含 '=' 但没有任何短名命中：必填全缺。
	args = ParseChatArguments(fireworks, "/fireworks nonsense=1")
	if args.Runnable || len(args.MissingRequired) != 2 {
		t.Fatalf("未命中短名时应缺两条必填: %+v", args)
	}
}

func TestChatValueOfType(t *testing.T) {
	cases := []struct {
		typeName string
		raw      string
		want     bool
	}{
		{"String", "anything", true},
		{"Boolean", "0", true},
		{"Boolean", "1", true},
		{"Boolean", "TRUE", true}, // Convert.ChangeType → Boolean.Parse 大小写不敏感
		{"Boolean", "yes", false},
		{"Byte", "255", true},
		{"Byte", "256", false},
		{"Byte", "-1", false},
		{"UInt16", " 5 ", true}, // 允许首尾空白
		{"Int16", "-5", true},
		{"Int32", "abc", false},
		{"UInt16", "1.5", false},
	}
	for _, tc := range cases {
		if got := chatValueOfType(tc.typeName, tc.raw); got != tc.want {
			t.Fatalf("%s(%q) 可转换 = %v, want %v", tc.typeName, tc.raw, got, tc.want)
		}
	}
}

// /help 的参数没有 [Argument] 特性：空命令（只有命令词）也算解析成功，值为空串。
func TestParseChatArgumentsHelpWithNoToken(t *testing.T) {
	cfg := mustConfig(t)
	help, _ := cfg.ChatCommandByCommand("/help")
	args := ParseChatArguments(help, "/help")
	if !args.Runnable || args.String("CommandName") != "" {
		t.Fatalf("/help 无参数时应可执行且值为空: %+v", args)
	}
	if args := ParseChatArguments(help, "/help add"); args.String("CommandName") != "add" {
		t.Fatalf("/help add 应解析出 add: %+v", args)
	}
}
