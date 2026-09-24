package gameserver

// handler_chat.go —— S1/TRIM-08 游戏内聊天（公共 C1 00 / 私聊 C1 02），对照原版
// ChatMessageHandlerPlugIn / WhisperedChatMessageHandlerPlugIn → ChatMessageAction →
// ChatMessage{Normal,Party,Guild,Alliance,GlobalNotification,Command,Whisper}Processor → ChatViewPlugIn。
// 入站 code 即频道字节（d[2]：Public=0x00、Whisper=0x02）；出站 ChatMessage 同位只有
// Normal(0)/Whisper(2) 两种，其余频道靠消息前缀着色。
//
// 裁剪登记：原版 5 个受管频道进处理器前查 Account.ChatBanUntil（被封则回蓝字），
// 本仓尚无该字段与 /chatban 生产者 → 门未接，登记见 doc/16 TRIM-08。

import (
	"strings"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// handlePublicChat 处理 C1 00：按前缀判频道后分派（对照 ChatMessageAction.ChatMessageAsync）。
// 前缀一律**不剥离**：原版把含前缀的整条消息转发给接收方，出站包类型字节除私聊外恒 Normal，
// 客户端靠消息里的前缀决定颜色（ChatViewPlugIn.ConvertChatMessageType）。
func (s *Server) handlePublicChat(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	c := sess.getSelected()
	if wp == nil || c == nil {
		return
	}
	pkt := c2s.AsPublicChatMessage(frame)
	msg := pkt.MessageString()
	// 包内角色名与本会话不符：原版只写日志、不丢包（ChatMessageAction.cs:62-65）。
	if name := pkt.CharacterString(); !strings.EqualFold(name, wp.Name) {
		s.deps.logger.Printf("gameserver: Maybe Hacker, Charname in chat packet != charname [%s] <> [%s]",
			wp.Name, name)
	}

	switch action.ChatChannelOf(msg, false) {
	case action.ChatChannelParty:
		s.handlePartyChat(wp, msg)
	case action.ChatChannelGuild:
		// 原版 GuildStatus 为空（无盟）时静默返回，不降级为公共频道。
		if guildID := s.guildOfCharacter(wp.Name); guildID != 0 {
			s.publishGuildMessage(guildID, wp.Name, msg)
		}
	case action.ChatChannelAlliance:
		if guildID := s.guildOfCharacter(wp.Name); guildID != 0 {
			s.publishAllianceMessage(guildID, wp.Name, msg)
		}
	case action.ChatChannelGens:
		// 原版注册了 $ 前缀（ChatMessageType.Gens）却没有对应处理器 → 只记调试日志后丢弃。
		s.deps.logger.Printf("gameserver: 未实现聊天 message type: Gens %s: %q", wp.Name, msg)
	case action.ChatChannelGlobalNotification:
		// 非 GM 静默返回（ChatMessageGlobalNotificationProcessor.cs:26-28），且它不受 chat ban 管。
		if c.Status < entity.CharacterStatusGameMaster {
			return
		}
		// 原版 GameContext.SendGlobalNotificationAsync：TrimStart('!') 后走全局金字，不是聊天包。
		s.broadcastMessage(strings.TrimLeft(msg, "!"), action.MessageGoldenCenter)
	case action.ChatChannelCommand:
		s.handleChatCommand(sess, c, msg)
	default:
		for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
			if o.View != nil {
				_ = o.View.ShowChatMessage(wp.Name, msg, action.ChatNormal)
			}
		}
	}
}

// handleWhisper 处理 C1 02：按接收者角色名找在线玩家，只发给接收者
// （对照 ChatMessageWhisperProcessor——原版不回显发送者，客户端本地已有发言记录）。
func (s *Server) handleWhisper(sess *session, frame []byte) {
	wp := enteredPlayer(sess)
	if wp == nil {
		return
	}
	req := c2s.AsWhisperMessage(frame)
	target := s.sessionOfCharacter(req.ReceiverNameString())
	if target == nil {
		// 原版 ChatMessageWhisperProcessor 只在收到者在图时转发，找不到人**不发任何包**。
		s.deps.logger.Printf("gameserver: 私聊 %s→%s 收件人不在图，丢弃", wp.Name, req.ReceiverNameString())
		return
	}
	tv := s.viewFor(target)
	if tv == nil {
		return
	}
	_ = tv.ShowChatMessage(wp.Name, req.MessageString(), action.ChatWhisper)
}

// handlePartyChat 处理 ~ 前缀的组队聊天（S2 handler_party.go 实现）。

// sessionOfCharacter 按角色名（不区分大小写）查在线会话，无则 nil。
// 对照 OpenMU GameContext.GetPlayerByCharacterName。
func (s *Server) sessionOfCharacter(name string) *session {
	if name == "" {
		return nil
	}
	for _, sess := range s.trackedSessions() {
		if wp := sess.getWorldPlayer(); wp != nil && strings.EqualFold(wp.Name, name) {
			return sess
		}
	}
	return nil
}

// enteredPlayer 返回会话进入世界的世界态玩家，未进图时 nil。
func enteredPlayer(sess *session) *world.Player {
	if sess.getState() != entity.StateEnteredWorld {
		return nil
	}
	return sess.getWorldPlayer()
}
