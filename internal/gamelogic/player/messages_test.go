// messages_test.go —— 消息目录的对拍：键名 = resx 条目名，文案 = 中立英文原文，
// 参数按 {0}/{1} 代入，缺键回空串（原版 ResourceManager 未命中即 null → 空文本 → 发包器提前返回）。
package player

import (
	"testing"
)

func TestLocalizedMessageTexts(t *testing.T) {
	cases := []struct {
		key  MessageKey
		want string
	}{
		{MsgUsingThisItemNotImplemented, "Using this item is not implemented."},
		{MsgItemUnknown, "Item Unknown"},
		{MsgInventoryFull, "Inventory is full"},
		{MsgNotEnoughMoney, "You don't have enough Money"},
		{MsgNotEnoughLevelUpPoints, "Not enough level up points available."},
		{MsgAttributeNotAvailable, "Attribute not available."},
		{MsgNoItemToRepair, "No item there to repair."},
		{MsgNotEnoughMoneyToRepair, "You don't have enough money to repair."},
		{MsgLevelTooLowToEnter, "Your level is too low to enter this map."},
		// 原版 UnknownWarpIndex 的调用点不传参 → 占位符原样到客户端（照抄，见 doc/16）。
		{MsgUnknownWarpIndex, "Unknown warp index {0}"},
	}
	for _, tc := range cases {
		if got := LocalizedMessage(tc.key); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.key, got, tc.want)
		}
		if string(tc.key) == "" {
			t.Errorf("键名不得为空")
		}
	}
}

func TestLocalizedMessageFormatting(t *testing.T) {
	got := LocalizedMessage(MsgTalkingNotImplementedFormat, 8, "Potion Girl")
	want := "Talking to this NPC (8, Potion Girl) is not implemented yet."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := LocalizedMessage(MsgLevelUpCongrats, 7); got != "Congratulations, you are Level 7 now." {
		t.Fatalf("got %q", got)
	}
	if got := LocalizedMessage(MsgMasterLevelUpCongrats, 12); got != "Congratulations, you are Master Level 12 now." {
		t.Fatalf("大师祝贺文案 got %q", got)
	}
	// 参数多于占位符时不报错、原样保留多余项。
	if got := LocalizedMessage(MsgLevelUpCongrats, 7, 8); got != "Congratulations, you are Level 7 now." {
		t.Fatalf("多余参数应被忽略, got %q", got)
	}
}

func TestLocalizedMessageUnknownKey(t *testing.T) {
	if got := LocalizedMessage(MessageKey("NoSuchEntry"), "x"); got != "" {
		t.Fatalf("缺键应回空串（发包侧据此静默）, got %q", got)
	}
}

// TestChatCommandMessages 锁定 TRIM-08 新增文案与原版 resx 逐字一致。
func TestChatCommandMessages(t *testing.T) {
	cases := []struct {
		key  MessageKey
		args []any
		want string
	}{
		{MsgCommandDoesNotExist, []any{"foo"}, "The command 'foo' does not exist."},
		{MsgCommandArgumentCount, []any{2, 0}, "The command needs 2 arguments and was given 0."},
		{MsgCommandArgumentType, []any{"Amount", "UInt16"},
			"The argument Amount was given a invalid type, it expects the value to be of the type UInt16."},
		{MsgCommandRequiredArgumentMissing, []any{"Target"}, "The required argument named Target was not used."},
		{MsgUnknownAttribute, []any{"xxx"}, "Unknown attribute: 'xxx'."},
		{MsgCharacterHasNoStatAttribute, []any{"cmd"}, "The character has no stat attribute 'cmd'."},
		{MsgOnlineCountInfo, []any{"/online", 1, 7}, "[/online] 1 GM(s) and 7 player(s) online"},
	}
	for _, tc := range cases {
		if got := LocalizedMessage(tc.key, tc.args...); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestMessageKeysCatalogComplete 守住目录不变量：每个导出的键都有非空文案，且无未登记条目。
func TestMessageKeysCatalogComplete(t *testing.T) {
	exported := []MessageKey{
		MsgUsingThisItemNotImplemented, MsgItemUnknown, MsgInventoryFull, MsgNotEnoughMoney,
		MsgTalkingNotImplementedFormat, MsgNotEnoughLevelUpPoints, MsgAttributeNotAvailable,
		MsgNoItemToRepair, MsgNotEnoughMoneyToRepair, MsgNotEnoughMoneyToProceed,
		MsgUnknownWarpIndex,
		MsgLevelTooLowToEnter, MsgLevelUpCongrats, MsgMasterLevelUpCongrats,
		MsgMuHelperAlreadyRunning, MsgMuHelperMinimumLevel, MsgMuHelperMaximumLevel, MsgMuHelperRequiresMoney,
		// TRIM-08 聊天命令族。
		MsgCommandDoesNotExist, MsgCommandArgumentCount, MsgCommandArgumentType,
		MsgCommandRequiredArgumentMissing, MsgUnknownAttribute, MsgCharacterHasNoStatAttribute,
		MsgOnlineCountInfo, MsgMissingMapRequirement,
		// S-3 仓库上锁取物（MoveItemAction.cs:243）。
		MsgVaultIsLocked,
		// S-3 后续：Lahap 宝石升档合成/降档拆分（ItemStackAction.cs）。
		MsgLackingJewels, MsgStackedJewelNotFound, MsgNotStackedJewel, MsgNoInventorySpace,
	}
	if len(exported) != len(playerMessages) {
		t.Fatalf("导出的键 %d 个与目录 %d 条不一致", len(exported), len(playerMessages))
	}
	for _, k := range exported {
		if playerMessages[k] == "" {
			t.Errorf("%s 文案为空", k)
		}
	}
}
