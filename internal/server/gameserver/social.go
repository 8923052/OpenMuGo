package gameserver

import (
	"strconv"
	"strings"
	"time"

	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
)

// 契约层 api.GameServer 的社交推送方法（P5 接线）。
//
// 原版这些方法把推送落到"对应在线玩家的出站包"上；M6 尚无信使的
// 客户端协议实现（handler/messenger 已占位），因此：
//   - AssignGuildToPlayerAsync：把战盟归属记录到会话（进/离场事件要用 guildId）；
//   - 战盟/同盟聊天（TRIM-08）：已落到真出站包；
//   - 其余推送：记录日志（协议接入后替换为真实出站包）。
// 方法签名与契约层逐一对应，不裁剪。

// FriendRequestAsync 实现 api.FriendSystemSubscriber：向接收方本地会话下发好友请求帧。
func (s *Server) FriendRequestAsync(requester string, receiver string) error {
	if sess := s.sessionOfCharacter(receiver); sess != nil {
		return s.viewFor(sess).SendFriendRequest(requester)
	}
	return nil
}

// LetterReceivedAsync 实现 api.FriendSystemSubscriber：向在线收件人本地会话下发 AddLetter（新信）。
func (s *Server) LetterReceivedAsync(letter api.LetterHeader) error {
	sess := s.sessionOfCharacter(letter.ReceiverName)
	if sess == nil {
		return nil
	}
	ts := time.Unix(0, letter.LetterDate).UTC().Format("2006-01-02 15:04:05")
	return s.viewFor(sess).SendAddLetter(letter.Index, letter.SenderName, ts, letter.Subject, api.LetterStateNew)
}

// FriendOnlineStateChangedAsync 实现 api.FriendSystemSubscriber：向 player 本地会话下发好友在线服务器号。
func (s *Server) FriendOnlineStateChangedAsync(player string, friend string, serverId int) error {
	if sess := s.sessionOfCharacter(player); sess != nil {
		return s.viewFor(sess).SendFriendOnlineStateUpdate(friend, byte(uint8(serverId)))
	}
	return nil
}

// ChatRoomCreatedAsync 实现 api.FriendSystemSubscriber：向 info.ClientName 对应的本地会话下发进房信息。
func (s *Server) ChatRoomCreatedAsync(playerAuthenticationInfo api.ChatServerAuthenticationInfo, friendName string) error {
	sess := s.sessionOfCharacter(playerAuthenticationInfo.ClientName)
	if sess == nil {
		return nil
	}
	pw, _ := strconv.ParseUint(playerAuthenticationInfo.AuthenticationToken, 10, 32)
	return s.viewFor(sess).SendChatRoomConnectionInfo(
		playerAuthenticationInfo.HostAddress, playerAuthenticationInfo.RoomId,
		uint32(pw), playerAuthenticationInfo.Index, friendName, true)
}

// InitializeMessengerAsync 实现 api.FriendSystemSubscriber：进图信使初始化（好友名单 + 未处理请求）。
// 名单先按离线哨兵下发；在线好友的服务器号随后由 FriendOnlineStateChanged 流式补正。
func (s *Server) InitializeMessengerAsync(initializationData api.MessengerInitializationData) error {
	sess := s.sessionOfCharacter(initializationData.PlayerName)
	if sess == nil {
		return nil
	}
	friends := make([]action.FriendEntry, 0, len(initializationData.Friends))
	for _, f := range initializationData.Friends {
		friends = append(friends, action.FriendEntry{Name: f, ServerID: byte(api.SpecialServerIdOffline)})
	}
	view := s.viewFor(sess)
	if err := view.SendMessengerInitialization(friends, 0, maxLetters); err != nil {
		return err
	}
	for _, requester := range initializationData.OpenFriendRequests {
		if err := view.SendFriendRequest(requester); err != nil {
			return err
		}
	}
	return nil
}

// setSessionGuild 记录角色的战盟归属（AssignGuildToPlayerAsync 的存储侧）。
func (s *Server) setSessionGuild(characterName string, status api.GuildMemberStatus) {
	s.guildMu.Lock()
	s.guildByCharacter[characterName] = status
	s.guildMu.Unlock()
}

// guildOfCharacter 查角色的战盟归属（0 = 无战盟）。
func (s *Server) guildOfCharacter(characterName string) uint32 {
	s.guildMu.RLock()
	defer s.guildMu.RUnlock()
	return s.guildByCharacter[characterName].GuildId
}

// AssignGuildToPlayerAsync 实现 api.GameServer：记录战盟归属（进/离场事件依赖它）。
func (s *Server) AssignGuildToPlayerAsync(characterName string, guildStatus api.GuildMemberStatus) error {
	s.setSessionGuild(characterName, guildStatus)
	s.deps.logger.Printf("gameserver: 战盟分配 %s → guildId=%d", characterName, guildStatus.GuildId)
	return nil
}

// GuildChatMessageAsync 实现 api.GameServer：把战盟频道发言发给**本服**该盟的在线成员
// （对照 GameServer.cs:218-227 + GameServerContext.ForEachGuildPlayerAsync:134-143）。
// 原版在这一步补前缀：消息不以 @ 开头就补上（客户端按前缀着色，出站类型字节仍是 Normal）。
func (s *Server) GuildChatMessageAsync(guildId uint32, sender string, message string) error {
	s.broadcastChatToGuilds([]uint32{guildId}, sender, message, "@")
	return nil
}

// AllianceChatMessageAsync 实现 api.GameServer：同盟频道发言（对照 GameServer.cs:229-241 +
// ForEachAlliancePlayerAsync:146-161）——先向战盟服要同盟内的全部战盟号，再逐个遍历本服成员。
// 战盟不在同盟时战盟服返回空表 → 一条都不发（原版行为，含发言者自己）。
func (s *Server) AllianceChatMessageAsync(guildId uint32, sender string, message string) error {
	if s.deps.guild == nil {
		return nil
	}
	entries := s.deps.guild.AllianceGuildsAsync(guildId)
	ids := make([]uint32, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Id)
	}
	s.broadcastChatToGuilds(ids, sender, message, "@@")
	return nil
}

// broadcastChatToGuilds 给这些战盟的本服在线成员各发一条聊天（前缀缺失时按原版补上）。
func (s *Server) broadcastChatToGuilds(guildIDs []uint32, sender, message, prefix string) {
	if !strings.HasPrefix(message, prefix) {
		message = prefix + message
	}
	wanted := make(map[uint32]bool, len(guildIDs))
	for _, id := range guildIDs {
		wanted[id] = true
	}
	for _, sess := range s.trackedSessions() {
		wp := sess.getWorldPlayer()
		if wp == nil || sess.getState() != entity.StateEnteredWorld {
			continue
		}
		if !wanted[s.guildOfCharacter(wp.Name)] {
			continue
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowChatMessage(sender, message, action.ChatNormal)
		}
	}
}

// GuildDeletedAsync 实现 api.GameServer：战盟解散→清除本地映射（成员客户端的下发归 S6b）。
func (s *Server) GuildDeletedAsync(guildId uint32) error {
	s.guildMu.Lock()
	for name, st := range s.guildByCharacter {
		if st.GuildId == guildId {
			delete(s.guildByCharacter, name)
		}
	}
	s.guildMu.Unlock()
	return nil
}

// GuildPlayerKickedAsync 实现 api.GameServer：成员被踢→清除其本地映射。
func (s *Server) GuildPlayerKickedAsync(playerName string) error {
	s.guildMu.Lock()
	delete(s.guildByCharacter, playerName)
	s.guildMu.Unlock()
	return nil
}

// AllianceCreatedAsync 实现 api.GameServer：同盟建立通知。
func (s *Server) AllianceCreatedAsync(masterGuildId uint32, memberGuildId uint32) error {
	s.deps.logger.Printf("gameserver: 同盟建立 master=%d member=%d", masterGuildId, memberGuildId)
	return nil
}

// AllianceDisbandedAsync 实现 api.GameServer：同盟解散通知。
func (s *Server) AllianceDisbandedAsync(masterGuildId uint32, memberGuildId uint32) error {
	s.deps.logger.Printf("gameserver: 同盟解散 master=%d member=%d", masterGuildId, memberGuildId)
	return nil
}

// GuildHostilityChangedAsync 实现 api.GameServer：敌对关系变化通知。
func (s *Server) GuildHostilityChangedAsync(guildIdA uint32, allianceGuildIdsA []uint32, guildIdB uint32, allianceGuildIdsB []uint32, created bool) error {
	s.deps.logger.Printf("gameserver: 敌对关系 %v ↔ %v created=%v", allianceGuildIdsA, allianceGuildIdsB, created)
	return nil
}

// SendGlobalMessageAsync 实现 api.GameServer：全服公告。
func (s *Server) SendGlobalMessageAsync(message string, messageType api.MessageType) error {
	s.deps.logger.Printf("gameserver: 全服公告 [%d] %s", messageType, message)
	return nil
}

// DisconnectPlayerAsync 实现 api.GameServer：断开指定玩家（管理面板用）。
func (s *Server) DisconnectPlayerAsync(playerName string) (bool, error) {
	s.deps.logger.Printf("gameserver: 请求断开玩家 %s", playerName)
	return false, nil
}

// DisconnectAccountAsync 实现 api.GameServer：断开指定账号。
func (s *Server) DisconnectAccountAsync(accountName string) (bool, error) {
	s.deps.logger.Printf("gameserver: 请求断开账号 %s", accountName)
	return false, nil
}

// BanPlayerAsync 实现 api.GameServer：封禁玩家。
func (s *Server) BanPlayerAsync(playerName string) (bool, error) {
	s.deps.logger.Printf("gameserver: 请求封禁玩家 %s", playerName)
	return false, nil
}

// PlayerAlreadyLoggedInAsync 实现 api.GameServer：重复登录提示（原版广播给全部 GS；
// 目标 GS 上的既有玩家应收到提示——M6 仅记录）。
func (s *Server) PlayerAlreadyLoggedInAsync(_ uint8, loginName string) error {
	s.deps.logger.Printf("gameserver: 账号 %s 重复登录提示", loginName)
	return nil
}
