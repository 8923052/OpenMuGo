// player_view_message_test.go —— C1 0D ServerMessage（蓝字/金字系统提示）出站锚定。
//
// 对照 OpenMU `GameServer/RemoteView/ShowMessagePlugIn.cs`：
//   - 长度 >241 UTF-8 字节时按**字符边界**拆多包（每包各自带前缀）；
//   - `ClientVersion.Season > 0` 时正文前置 "000000000"（真机 20404 = Season 106 需要它，
//     0.75/0.95d 的 Season=0 不带）；
//   - 空文本不发（原版提前返回）。
//
// 封包布局（spec/0.9.10 ServerToClientPackets.xml 的 ServerMessage）：
// C1 <len> 0D <type> <utf8 正文> 00，type：GoldenCenter=0 / BlueNormal=1 / GuildNotice=2。
package remote

import (
	"strings"
	"testing"
	"unicode/utf8"

	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

const seasonPrefix = "000000000"

// s6e3 是真机客户端版本轴（Season 106 Episode 3，即 ASCII 20404）。
var s6e3 = version.ClientVersion{Season: 106, Episode: 3, Language: version.LanguageInvariant}

func TestShowMessageSeasonPrefixAndType(t *testing.T) {
	cases := []struct {
		name    string
		client  version.ClientVersion
		msgType action.MessageType
		wantTyp s2c.MessageType
		wantPfx bool
	}{
		{"S6E3 蓝字", s6e3, action.MessageBlueNormal, s2c.MessageType_BlueNormal, true},
		{"金字", s6e3, action.MessageGoldenCenter, s2c.MessageType_GoldenCenter, true},
		{"公会通知", s6e3, action.MessageGuildNotice, s2c.MessageType_GuildNotice, true},
		{"0.95d 无前缀", version.ClientVersion{}, action.MessageBlueNormal, s2c.MessageType_BlueNormal, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingSender{}
			view := NewPlayerView(rec, true, tc.client, nil)
			if err := view.ShowMessage("Item Unknown", tc.msgType); err != nil {
				t.Fatal(err)
			}
			if len(rec.frames) != 1 {
				t.Fatalf("应发 1 帧, got %d", len(rec.frames))
			}
			f := rec.frames[0]
			want := "Item Unknown"
			if tc.wantPfx {
				want = seasonPrefix + want
			}
			p := s2c.AsServerMessage(f)
			if p.Type() != tc.wantTyp {
				t.Fatalf("type 字节应为 %d, got %d", tc.wantTyp, p.Type())
			}
			if p.MessageString() != want {
				t.Fatalf("正文应为 %q, got %q", want, p.MessageString())
			}
			// 头三字节 + type + 正文 + NUL，且长度单字节自洽。
			if f[0] != 0xC1 || f[2] != s2c.ServerMessageCode {
				t.Fatalf("包头应为 C1 ?? 0D, got % X", f[:3])
			}
			if int(f[1]) != len(f) || len(f) != s2c.ServerMessageRequiredSize(len(want)) {
				t.Fatalf("长度应为 %d(自述 %d), got %d", s2c.ServerMessageRequiredSize(len(want)), f[1], len(f))
			}
		})
	}
}

func TestShowMessageEmptyIsSilent(t *testing.T) {
	rec := &recordingSender{}
	view := NewPlayerView(rec, true, s6e3, nil)
	if err := view.ShowMessage("", action.MessageBlueNormal); err != nil {
		t.Fatal(err)
	}
	if len(rec.frames) != 0 {
		t.Fatalf("空文本不得发包, got %d", len(rec.frames))
	}
}

// TestShowMessageSplitsLongText 锁定 241 字节拆分：每段独立成包、各自带前缀，
// 且拼接回去与原文逐字节相同（含不切断多字节字符）。
func TestShowMessageSplitsLongText(t *testing.T) {
	messages := map[string]string{
		"300 ASCII": strings.Repeat("a", 300),
		// 240 个单字节 + 1 个三字节字符：整体 243B，拆分点必须落在字符边界。
		"字符边界": strings.Repeat("b", 240) + "中",
	}
	for name, msg := range messages {
		t.Run(name, func(t *testing.T) {
			rec := &recordingSender{}
			view := NewPlayerView(rec, true, s6e3, nil)
			if err := view.ShowMessage(msg, action.MessageBlueNormal); err != nil {
				t.Fatal(err)
			}
			if len(rec.frames) != 2 {
				t.Fatalf("应拆成 2 帧, got %d", len(rec.frames))
			}
			var rebuilt strings.Builder
			for i, f := range rec.frames {
				p := s2c.AsServerMessage(f)
				if int(f[1]) > 255 {
					t.Fatalf("第 %d 帧超过单字节长度上限: %d", i+1, f[1])
				}
				text := p.MessageString()
				if !strings.HasPrefix(text, seasonPrefix) {
					t.Fatalf("分段包必须各自带前缀, got %q", text)
				}
				part := strings.TrimPrefix(text, seasonPrefix)
				if !utf8.ValidString(part) {
					t.Fatalf("第 %d 帧正文被切断在字符中间: % X", i+1, []byte(part))
				}
				if len(part) > 241 {
					t.Fatalf("第 %d 帧正文 %dB 超过 241", i+1, len(part))
				}
				rebuilt.WriteString(part)
			}
			if rebuilt.String() != msg {
				t.Fatalf("分段拼接与原文不一致: got %dB want %dB", rebuilt.Len(), len(msg))
			}
		})
	}
}
