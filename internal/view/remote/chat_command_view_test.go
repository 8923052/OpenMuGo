package remote

// chat_command_view_test.go —— C2 F5 01 AvailableChatCommand 的逐字节锚定（TRIM-08e）。
// 对照 OpenMU ServerToClientPackets.xml:11071-11160 与
// RemoteView/ChatCommandListViewPlugIn.cs:35-99（每条命令一包、参数块 102B、
// 类型枚举由 CLR 类型名映射）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
)

func chatCommandFixture() action.ChatCommandView {
	return action.ChatCommandView{
		Command: "/add", Name: "Add Stat chat command", Description: "Adds stat points",
		MinimumCharacterStatus: 0,
		Parameters: []action.ChatCommandParameterView{
			{Name: "StatType", TypeName: "String", ValidValues: "agi|cmd|ene|str|vit"},
			{Name: "Amount", TypeName: "UInt16", IsRequired: true},
		},
	}
}

func TestAvailableChatCommandLayout(t *testing.T) {
	sender := &recordingSender{}
	if err := NewPlayerView(sender, true, s6e3, nil).
		ShowAvailableChatCommands([]action.ChatCommandView{chatCommandFixture()}); err != nil {
		t.Fatal(err)
	}
	if len(sender.frames) != 1 {
		t.Fatalf("一条命令应发一帧, got %d", len(sender.frames))
	}
	f := sender.frames[0]
	want := s2c.AvailableChatCommandRequiredSize(2)
	// C2 头是 5 字节：类型、2 字节总长（大端）、code、subcode → 字段从 d[5] 起。
	if len(f) != want || f[0] != 0xC2 || f[3] != 0xF5 || f[4] != 0x01 {
		t.Fatalf("应为 C2 F5 01 / %dB, got %dB %X", want, len(f), f[:5])
	}
	p := s2c.AsAvailableChatCommand(f)
	if p.Index() != 0 || p.Count() != 1 || p.MinimumCharacterStatus() != s2c.CharacterStatus_Normal {
		t.Fatalf("Index/Count/状态错: %d %d %d", p.Index(), p.Count(), p.MinimumCharacterStatus())
	}
	if p.ParameterCount() != 2 || p.CommandString() != "/add" ||
		p.NameString() != "Add Stat chat command" || p.DescriptionString() != "Adds stat points" {
		t.Fatalf("头部字段错: cnt=%d cmd=%q name=%q", p.ParameterCount(), p.CommandString(), p.NameString())
	}
	first := p.Parameters(0)
	if first == nil || first.NameString() != "StatType" || first.Type() != s2c.ChatCommandParameterType_Text ||
		first.IsRequired() || first.ValidValuesString() != "agi|cmd|ene|str|vit" {
		t.Fatalf("参数 0 错: %+v", first)
	}
	second := p.Parameters(1)
	if second == nil || second.NameString() != "Amount" || !second.IsRequired() ||
		second.Type() != s2c.ChatCommandParameterType_Number || second.ShortNameString() != "" {
		t.Fatalf("参数 1 错: %+v", second)
	}
	if p.Parameters(2) != nil {
		t.Fatal("越界索引应返回 nil")
	}
}

// TestAvailableChatCommandIndexCountAndTypes：多条时 Index 递增、Count 为总数；
// 三种 CLR 类型名按原版 switch 落到 Text/Number/Boolean。
func TestAvailableChatCommandIndexCountAndTypes(t *testing.T) {
	cmd := chatCommandFixture()
	cmd.Command = "/move"
	cmd.MinimumCharacterStatus = 32
	cmd.Parameters = []action.ChatCommandParameterView{
		{Name: "Flag", TypeName: "Boolean"},
		{Name: "Coord", TypeName: "Byte"},
		{Name: "Whatever", TypeName: "SomeUnknownType"},
	}
	sender := &recordingSender{}
	if err := NewPlayerView(sender, true, s6e3, nil).
		ShowAvailableChatCommands([]action.ChatCommandView{chatCommandFixture(), cmd}); err != nil {
		t.Fatal(err)
	}
	if len(sender.frames) != 2 {
		t.Fatalf("两条命令应发两帧, got %d", len(sender.frames))
	}
	for i, f := range sender.frames {
		p := s2c.AsAvailableChatCommand(f)
		if int(p.Index()) != i || p.Count() != 2 {
			t.Fatalf("第 %d 帧 Index/Count = %d/%d", i, p.Index(), p.Count())
		}
	}
	gm := s2c.AsAvailableChatCommand(sender.frames[1])
	if gm.MinimumCharacterStatus() != s2c.CharacterStatus_GameMaster {
		t.Fatalf("最小状态应透传 GameMaster, got %d", gm.MinimumCharacterStatus())
	}
	wantTypes := []s2c.ChatCommandParameterType{
		s2c.ChatCommandParameterType_Boolean,
		s2c.ChatCommandParameterType_Number,
		s2c.ChatCommandParameterType_Text,
	}
	for i, want := range wantTypes {
		if got := gm.Parameters(i).Type(); got != want {
			t.Fatalf("参数 %d 类型 = %d, want %d", i, got, want)
		}
	}
}

// TestAvailableChatCommandBelowClientMinimum：低客户端（原版插件挂 MinimumClient(106,3)）不发。
func TestAvailableChatCommandBelowClientMinimum(t *testing.T) {
	sender := &recordingSender{}
	if err := NewPlayerView(sender, true, plainClient, nil).
		ShowAvailableChatCommands([]action.ChatCommandView{chatCommandFixture()}); err != nil {
		t.Fatal(err)
	}
	if len(sender.frames) != 0 {
		t.Fatalf("1063 之前不该发命令表, got %d 帧", len(sender.frames))
	}
}
