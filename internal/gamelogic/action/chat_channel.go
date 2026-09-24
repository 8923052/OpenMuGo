package action

// chat_channel.go —— 聊天频道判定（TRIM-08），对照 ChatMessageAction.cs:22-30 的前缀表
// 与 :98-104 的 ReverseComparer。
//
// 两个与直觉不同的原版事实：
//   - 前缀**不剥离**：处理器拿到的是含前缀的整条消息，转发时也是整条
//     （出站包的类型字节除私聊外恒 Normal，见 ChatViewPlugIn.ConvertChatMessageType，
//     客户端靠消息里的前缀决定颜色）；
//   - 匹配顺序按"长者优先"，所以 @@（同盟）必须先于 @（战盟）试。

import "strings"

// ChatChannel 是原版 ChatMessageType 在本仓的对应物（入站前缀判定结果）。
// 注意与 action.ChatMessageType（出站 wire 类型，只有 Normal/Whisper）不是一回事。
type ChatChannel byte

const (
	ChatChannelNormal             ChatChannel = iota // 无已知前缀 → 公共（同图视野）
	ChatChannelWhisper                               // 私聊（独立入站包 C1 02，非前缀）
	ChatChannelParty                                 // ~ 组队
	ChatChannelGuild                                 // @ 战盟
	ChatChannelAlliance                              // @@ 同盟
	ChatChannelGens                                  // $ 血统阵营：原版注册了前缀但**没有处理器** → 丢弃
	ChatChannelGlobalNotification                    // ! GM 金屏公告
	ChatChannelCommand                               // / 命令
)

// chatChannelPrefixes 是原版 SortedDictionary(string, ReverseComparer) 的实际迭代序：
// 按字符降序、长串在前 → ~ → @@ → @ → / → $ → !。
var chatChannelPrefixes = []struct {
	prefix  string
	channel ChatChannel
}{
	{"~", ChatChannelParty},
	{"@@", ChatChannelAlliance},
	{"@", ChatChannelGuild},
	{"/", ChatChannelCommand},
	{"$", ChatChannelGens},
	{"!", ChatChannelGlobalNotification},
}

// ChatChannelOf 判这条消息属于哪个频道（isWhisper 对应入站包种类，不是前缀）。
func ChatChannelOf(message string, isWhisper bool) ChatChannel {
	if isWhisper {
		return ChatChannelWhisper
	}
	for _, p := range chatChannelPrefixes {
		if strings.HasPrefix(message, p.prefix) {
			return p.channel
		}
	}
	return ChatChannelNormal
}
