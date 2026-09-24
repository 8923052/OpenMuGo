package guildserver

import (
	"testing"

	"mugo/internal/api"
)

// recorder 记录战盟变更推送（替代真 GS）。
type recorder struct {
	events []string
}

func (r *recorder) GuildPlayerKickedAsync(playerName string) error {
	r.events = append(r.events, "kicked:"+playerName)
	return nil
}
func (r *recorder) GuildDeletedAsync(guildId uint32) error {
	r.events = append(r.events, "deleted")
	return nil
}
func (r *recorder) AssignGuildToPlayerAsync(serverId uint8, characterName string, status api.GuildMemberStatus) error {
	r.events = append(r.events, "assign:"+characterName)
	return nil
}
func (r *recorder) AllianceCreatedAsync(masterGuildId, memberGuildId uint32) error {
	r.events = append(r.events, "alliance-created")
	return nil
}
func (r *recorder) AllianceDisbandedAsync(masterGuildId, memberGuildId uint32) error {
	r.events = append(r.events, "alliance-disbanded")
	return nil
}
func (r *recorder) GuildHostilityChangedAsync(guildIdA uint32, allianceA []uint32, guildIdB uint32, allianceB []uint32, created bool) error {
	r.events = append(r.events, "hostility")
	return nil
}

// TestCreateGuildAssignsToMaster 建盟：短 id 内存分配（从 1 起），盟主获 GuildMaster 职位。
func TestCreateGuildAssignsToMaster(t *testing.T) {
	rec := &recorder{}
	s := New(rec, nil)

	created, err := s.CreateGuildAsync("Knights", "Alice", make([]byte, 32), 0)
	if err != nil || !created {
		t.Fatalf("建盟失败: %v", err)
	}
	id := s.GetGuildIdByNameAsync("Knights")
	if id != 1 {
		t.Fatalf("首个短 id 应为 1（IdGenerator(1,...) 语义）: %d", id)
	}
	if last := rec.events[len(rec.events)-1]; last != "assign:Alice" {
		t.Fatalf("应把战盟分配给盟主: %v", rec.events)
	}
}

// TestMemberLifecycle 成员入会 → 进图登记在线 → 离图登记离线（0xFF）。
func TestMemberLifecycle(t *testing.T) {
	rec := &recorder{}
	s := New(rec, nil)
	_, _ = s.CreateGuildAsync("Knights", "Alice", nil, 0)
	gid := s.GetGuildIdByNameAsync("Knights")

	if err := s.CreateGuildMemberAsync(gid, "Bob", api.GuildPositionNormalMember, 0); err != nil {
		t.Fatal(err)
	}
	if last := rec.events[len(rec.events)-1]; last != "assign:Bob" {
		t.Fatalf("成员入会应通知: %v", rec.events)
	}

	// 进图：成员在线登记 + 分配战盟给玩家。
	rec.events = nil
	if err := s.PlayerEnteredGameAsync("cid-bob", "Bob", 0); err != nil {
		t.Fatal(err)
	}
	list := s.GetGuildListAsync(gid)
	bobOnline := false
	for _, e := range list {
		if e.PlayerName == "Bob" && e.ServerId == 0 {
			bobOnline = true
		}
	}
	if !bobOnline {
		t.Fatalf("进图后应在线: %+v", list)
	}

	// 离图：标记 0xFF（全员离线 + 无同盟 → 容器移除，但列表查询仍基于容器，先断言移除语义）。
	if err := s.GuildMemberLeftGameAsync(gid, "Bob", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.GuildMemberLeftGameAsync(gid, "Alice", 0); err != nil {
		t.Fatal(err)
	}
	// 全员离线：容器被移除（原版语义），列表为空。
	if list := s.GetGuildListAsync(gid); len(list) != 0 {
		t.Fatalf("全员离线后容器应移除: %+v", list)
	}
}

// TestKickMasterDeletesGuild 锁定：踢盟主 = 解散战盟（原版语义）。
func TestKickMasterDeletesGuild(t *testing.T) {
	rec := &recorder{}
	s := New(rec, nil)
	_, _ = s.CreateGuildAsync("Knights", "Alice", nil, 0)
	gid := s.GetGuildIdByNameAsync("Knights")

	if err := s.KickMemberAsync(gid, "Alice"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range rec.events {
		if e == "deleted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("踢盟主应解散战盟: %v", rec.events)
	}
	if s.GuildExistsAsync("Knights") {
		t.Fatal("解散后名字映射应移除")
	}
}

// TestAlliance 同盟：建立 → 目标已入盟拒绝 → 解除。
func TestAlliance(t *testing.T) {
	rec := &recorder{}
	s := New(rec, nil)
	_, _ = s.CreateGuildAsync("A", "a1", nil, 0)
	_, _ = s.CreateGuildAsync("B", "b1", nil, 0)
	_, _ = s.CreateGuildAsync("C", "c1", nil, 0)
	idA := s.GetGuildIdByNameAsync("A")
	idB := s.GetGuildIdByNameAsync("B")
	idC := s.GetGuildIdByNameAsync("C")

	res, err := s.CreateAllianceAsync(idA, idB)
	if err != nil || res != api.AllianceCreationSuccess {
		t.Fatalf("建同盟失败: %v %v", res, err)
	}
	// B 已在同盟：再次加入应拒绝。
	res, _ = s.CreateAllianceAsync(idA, idB)
	if res != api.AllianceCreationTargetGuildAlreadyInAlliance {
		t.Fatalf("已在同盟应拒绝: %v", res)
	}
	// C 加入同一同盟。
	res, _ = s.CreateAllianceAsync(idA, idC)
	if res != api.AllianceCreationSuccess {
		t.Fatalf("C 应可加入: %v", res)
	}

	// B 移出同盟。
	ok, err := s.RemoveAllianceAsync(idB)
	if err != nil || !ok {
		t.Fatalf("移出同盟失败: %v", err)
	}
}

// TestHostilityAndRelationship 敌对：设置 → 关系 Rival → 解除 → 关系 None。
func TestHostilityAndRelationship(t *testing.T) {
	rec := &recorder{}
	s := New(rec, nil)
	_, _ = s.CreateGuildAsync("A", "a1", nil, 0)
	_, _ = s.CreateGuildAsync("B", "b1", nil, 0)
	idA := s.GetGuildIdByNameAsync("A")
	idB := s.GetGuildIdByNameAsync("B")

	ok, err := s.SetHostilityAsync(idA, idB, true)
	if err != nil || !ok {
		t.Fatal("设敌对失败")
	}
	rel, _ := s.GetGuildRelationshipAsync(idA, idB)
	if rel != api.GuildRelationshipRival {
		t.Fatalf("应敌对: %v", rel)
	}
	if _, err := s.SetHostilityAsync(idA, idB, false); err != nil {
		t.Fatal(err)
	}
	rel, _ = s.GetGuildRelationshipAsync(idA, idB)
	if rel != api.GuildRelationshipNone {
		t.Fatalf("解除后应无关系: %v", rel)
	}
}
