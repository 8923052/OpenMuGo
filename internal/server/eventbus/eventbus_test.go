package eventbus

import (
	"testing"
)

// recordingGS / recordingFriend / recordingGuild 记录调用序列（验证扇出顺序与条件）。
type recordingGS struct {
	log      *[]string
	guildMsg []string
}

func (r *recordingGS) GuildChatMessageAsync(guildId uint32, sender, message string) error {
	*r.log = append(*r.log, "guild-chat")
	r.guildMsg = append(r.guildMsg, sender)
	return nil
}
func (r *recordingGS) AllianceChatMessageAsync(guildId uint32, sender, message string) error {
	*r.log = append(*r.log, "alliance-chat")
	return nil
}
func (r *recordingGS) PlayerAlreadyLoggedInAsync(serverId uint8, loginName string) error {
	*r.log = append(*r.log, "already-logged-in:"+loginName)
	return nil
}

type recordingFriend struct{ log *[]string }

func (r *recordingFriend) PlayerEnteredGameAsync(serverId uint8, characterId, characterName string) error {
	*r.log = append(*r.log, "friend-enter")
	return nil
}
func (r *recordingFriend) PlayerLeftGameAsync(characterId, characterName string) error {
	*r.log = append(*r.log, "friend-left")
	return nil
}

type recordingGuild struct{ log *[]string }

func (r *recordingGuild) PlayerEnteredGameAsync(characterId, characterName string, serverId uint8) error {
	*r.log = append(*r.log, "guild-enter")
	return nil
}
func (r *recordingGuild) GuildMemberLeftGameAsync(guildId uint32, characterId string, serverId uint8) error {
	*r.log = append(*r.log, "guild-left")
	return nil
}

// TestEnterOrderGuildFirst 锁定原版扇出顺序：进场 **先战盟后好友**。
func TestEnterOrderGuildFirst(t *testing.T) {
	var log []string
	gs := &recordingGS{log: &log}
	p := NewInMemoryPublisher(
		func() []GameServerView { return []GameServerView{gs} },
		&recordingFriend{log: &log},
		&recordingGuild{log: &log},
	)
	if err := p.PlayerEnteredGameAsync(0, "cid", "Bob"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0] != "guild-enter" || log[1] != "friend-enter" {
		t.Fatalf("进场顺序应为 guild → friend: %v", log)
	}
}

// TestLeftConditionGuildOnlyIfInGuild 锁定原版条件：离场时 **guildId > 0 才**通知战盟，好友必通知。
func TestLeftConditionGuildOnlyIfInGuild(t *testing.T) {
	var log []string
	p := NewInMemoryPublisher(
		func() []GameServerView { return nil },
		&recordingFriend{log: &log},
		&recordingGuild{log: &log},
	)

	// 无战盟：只有好友收到。
	if err := p.PlayerLeftGameAsync(0, "cid", "Bob", 0); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "friend-left" {
		t.Fatalf("guildId=0 应只通知好友: %v", log)
	}

	// 有战盟：先战盟后好友。
	log = nil
	if err := p.PlayerLeftGameAsync(0, "cid", "Bob", 7); err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0] != "guild-left" || log[1] != "friend-left" {
		t.Fatalf("guildId>0 应 guild → friend: %v", log)
	}
}

// TestChatMessagesFanOutToAllGS 锁定：聊天消息扇出到**全部**游戏服。
func TestChatMessagesFanOutToAllGS(t *testing.T) {
	var log []string
	gs0 := &recordingGS{log: &log}
	gs1 := &recordingGS{log: &log}
	servers := []GameServerView{gs0, gs1}
	p := NewInMemoryPublisher(
		func() []GameServerView { return servers },
		&recordingFriend{log: &log},
		&recordingGuild{log: &log},
	)
	if err := p.GuildMessageAsync(7, "Bob", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := p.PlayerAlreadyLoggedInAsync(0, "bob"); err != nil {
		t.Fatal(err)
	}
	if len(gs0.guildMsg) != 1 || len(gs1.guildMsg) != 1 {
		t.Fatalf("两个 GS 都应收到战盟聊天")
	}
	already := 0
	for _, e := range log {
		if e == "already-logged-in:bob" {
			already++
		}
	}
	if already != 2 {
		t.Fatalf("重复登录事件应扇出到 2 个 GS: %v", log)
	}
}
