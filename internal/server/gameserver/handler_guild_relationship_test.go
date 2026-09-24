package gameserver

// handler_guild_relationship_test.go —— 战盟同盟/敌对协议入口（doc/17 S-3 批次 5）：
// C1 E5 请求 → 对方团长收 E5 → C1 E6 应答 → 双方各收 E6 结果 → C1 E9 同盟清单 →
// C1 EB 01 移出同盟。帧一律手工按字节构造，顺便锁住字段偏移。

import (
	"testing"

	"mugo/internal/api"
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/view/remote"

	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
)

// relRequestFrame 构造 C1 E5（rel/req 用线值：同盟=1 敌对=2；建=1 解=2）。
func relRequestFrame(rel, req byte, target uint16) []byte {
	return []byte{0xC1, 7, 0xE5, rel, req, byte(target >> 8), byte(target)}
}

// relResponseFrame 构造 C1 E6（accepted 落在第 6 字节的 bit0）。
func relResponseFrame(rel, req byte, accepted bool, master uint16) []byte {
	var a byte
	if accepted {
		a = 1
	}
	return []byte{0xC1, 8, 0xE6, rel, req, a, byte(master >> 8), byte(master)}
}

// removeAllyFrame 构造 C1 EB 01 + 8B 战盟名。
func removeAllyFrame(guildName string) []byte {
	f := make([]byte, 12)
	f[0], f[1], f[2], f[3] = 0xC1, 12, 0xEB, 1
	copy(f[4:], guildName)
	return f
}

// createGuild 让某会话成为团长。
func createGuild(t *testing.T, srv *Server, sess *session, name string) {
	t.Helper()
	r := c2s.NewGuildCreateRequest()
	r.SetGuildName(name)
	srv.handleGuildCreate(sess, r.Bytes())
	if srv.guildStatusOf(sess.getSelected().Name).Position != api.GuildPositionGuildMaster {
		t.Fatalf("前置：%s 应是 %s 的团长", sess.getSelected().Name, name)
	}
}

func findRelRequest(rec *packetRecorder) *s2c.GuildRelationshipRequest {
	f := findFrame(rec, 0xC1, 0xE5)
	if f == nil {
		return nil
	}
	return s2c.AsGuildRelationshipRequest(f)
}

// findRelResult 取最后一条 C1 E6 关系变更结果。
func findRelResult(rec *packetRecorder) *s2c.GuildRelationshipChangeResult {
	f := findFrame(rec, 0xC1, 0xE6)
	if f == nil {
		return nil
	}
	return s2c.AsGuildRelationshipChangeResult(f)
}

// TestAllianceRequestReachesTargetMaster 验证请求转发：alpha 向 beta 发起同盟 →
// beta 收到 E5，且包里的发送者 ID 是 alpha 的真实对象 ID。
func TestAllianceRequestReachesTargetMaster(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Al1")
	createGuild(t, srv, sessB, "Al2")

	srv.handleGuildRelationshipRequest(sessA, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessB.getWorldPlayer().ID))

	got := findRelRequest(recB)
	if got == nil {
		t.Fatal("beta 作为对方团长应收到 C1 E5")
	}
	if byte(got.RelationshipType()) != action.GuildRelationshipAlliance ||
		byte(got.RequestType()) != action.GuildRequestJoin {
		t.Fatalf("E5 类型错：rel=%d req=%d", got.RelationshipType(), got.RequestType())
	}
	if id := sessA.getWorldPlayer().ID; got.SenderId() != id {
		t.Fatalf("E5 发送者 ID=%d，期望 alpha 的 %d", got.SenderId(), id)
	}
	if sessB.pendingRelationship == nil || sessB.pendingRelationship.from != "alpha" {
		t.Fatalf("beta 身上应挂着来自 alpha 的待应答请求: %+v", sessB.pendingRelationship)
	}
}

// TestAllianceAcceptFormsAlliance 验证应答通过后同盟成立：双方各收 E6 Success，
// 且请求者随后拉 E9 能看到两个战盟。
func TestAllianceAcceptFormsAlliance(t *testing.T) {
	srv, sessA, sessB, recA, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Ok1")
	createGuild(t, srv, sessB, "Ok2")
	srv.handleGuildRelationshipRequest(sessA, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessB.getWorldPlayer().ID))

	srv.handleGuildRelationshipResponse(sessB, relResponseFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, true, sessA.getWorldPlayer().ID))

	for name, rec := range map[string]*packetRecorder{"alpha": recA, "beta": recB} {
		r := findRelResult(rec)
		if r == nil {
			t.Fatalf("%s 应收到 C1 E6 结果", name)
		}
		if byte(r.Result()) != action.ResultGuildRelationshipSuccess {
			t.Fatalf("%s 收到的结果=%d，期望 Success(1)", name, r.Result())
		}
	}
	master, inAlliance := srv.deps.guild.AllianceMasterOfAsync(srv.guildOfCharacter("alpha"))
	if !inAlliance || master != srv.guildOfCharacter("alpha") {
		t.Fatalf("alpha 应是同盟盟主，master=%d inAlliance=%v", master, inAlliance)
	}

	srv.handleAllianceListRequest(sessA)
	list := findFrame(recA, 0xC2, 0xE9)
	if list == nil {
		t.Fatal("应收到 C2 E9 同盟清单")
	}
	if n := s2c.AsAllianceList(list).GuildCount(); n != 2 {
		t.Fatalf("清单条数=%d，期望 2", n)
	}
}

// TestAllianceRefuseCancelsRequest 验证拒绝分支：请求者收到 RequestCancelled，
// 且对方身上的待应答请求被清掉。
func TestAllianceRefuseCancelsRequest(t *testing.T) {
	srv, sessA, sessB, recA, _ := newGuildPair(t)
	createGuild(t, srv, sessA, "Rf1")
	createGuild(t, srv, sessB, "Rf2")
	srv.handleGuildRelationshipRequest(sessA, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessB.getWorldPlayer().ID))

	srv.handleGuildRelationshipResponse(sessB, relResponseFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, false, sessA.getWorldPlayer().ID))

	r := findRelResult(recA)
	if r == nil || byte(r.Result()) != action.ResultGuildRelationshipRequestCancelled {
		t.Fatalf("请求者应收到 RequestCancelled(32)，got %+v", r)
	}
	if sessB.pendingRelationship != nil {
		t.Fatal("应答后待应答请求应被清掉")
	}
	if _, inAlliance := srv.deps.guild.AllianceMasterOfAsync(srv.guildOfCharacter("alpha")); inAlliance {
		t.Fatal("拒绝后不应形成同盟")
	}
}

// TestRelationshipResponseMismatchIgnored 验证原版语义：应答的类型与挂着的请求不符
// → 什么都不发（也不清 pending 之外的状态）。
func TestRelationshipResponseMismatchIgnored(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Ms1")
	createGuild(t, srv, sessB, "Ms2")
	srv.handleGuildRelationshipRequest(sessA, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessB.getWorldPlayer().ID))
	before := len(recB.frames)

	srv.handleGuildRelationshipResponse(sessB, relResponseFrame(
		action.GuildRelationshipHostility, action.GuildRequestJoin, true, sessA.getWorldPlayer().ID))

	if len(recB.frames) != before {
		t.Fatalf("类型不符的应答不应产生任何出站，新增 %d 帧", len(recB.frames)-before)
	}
}

// TestRelationshipRequiresGuildMaster 验证非团长发起直接吃 NoAuthorization。
func TestRelationshipRequiresGuildMaster(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Nt1")
	// beta 建自己的盟后降为普通成员：改用"未建盟"路径——无盟即 Failed，
	// 这里用另一条：先把 beta 变成 alpha 盟里的普通成员。
	if err := srv.deps.guild.CreateGuildMemberAsync(srv.guildOfCharacter("alpha"), "beta",
		api.GuildPositionNormalMember, uint8(srv.serverID)); err != nil {
		t.Fatal(err)
	}

	srv.handleGuildRelationshipRequest(sessB, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessA.getWorldPlayer().ID))

	r := findRelResult(recB)
	if r == nil || byte(r.Result()) != action.ResultGuildRelationshipNoAuthorization {
		t.Fatalf("普通成员应收到 NoAuthorization(17)，got %+v", r)
	}
}

// TestRelationshipRejectedWhenAlreadyInAlliance 验证"目标已在同盟"分支：
// 同盟已成立后，成员盟的团长再发 Join → AlreadyInAlliance。
func TestRelationshipRejectedWhenAlreadyInAlliance(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "In1")
	createGuild(t, srv, sessB, "In2")
	if res, err := srv.deps.guild.CreateAllianceAsync(srv.guildOfCharacter("alpha"), srv.guildOfCharacter("beta")); err != nil ||
		res != api.AllianceCreationSuccess {
		t.Fatalf("前置建盟失败: %v %v", res, err)
	}

	// beta 向 alpha 再发一次 Join：目标（alpha）已在同盟 → AlreadyInAlliance。
	srv.handleGuildRelationshipRequest(sessB, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessA.getWorldPlayer().ID))

	r := findRelResult(recB)
	if r == nil || byte(r.Result()) != action.ResultGuildRelationshipAlreadyInAlliance {
		t.Fatalf("应收到 AlreadyInAlliance(21)，got %+v", r)
	}
}

// TestRelationshipBlockedByPendingRequest 验证一次只允许一个待应答请求：
// beta 身上挂着 alpha 的请求时，beta 自己再发起 → 自己收到 RequestCancelled。
func TestRelationshipBlockedByPendingRequest(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Pd1")
	createGuild(t, srv, sessB, "Pd2")
	srv.handleGuildRelationshipRequest(sessA, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessB.getWorldPlayer().ID))

	srv.handleGuildRelationshipRequest(sessB, relRequestFrame(
		action.GuildRelationshipAlliance, action.GuildRequestJoin, sessA.getWorldPlayer().ID))

	r := findRelResult(recB)
	if r == nil || byte(r.Result()) != action.ResultGuildRelationshipRequestCancelled {
		t.Fatalf("有待应答请求时再发起应收到 RequestCancelled(32)，got %+v", r)
	}
}

// TestRemoveAllianceOwnGuild 验证 EB 01：盟主把自己盟移出 → 结果 true，同盟解除。
func TestRemoveAllianceOwnGuild(t *testing.T) {
	srv, sessA, sessB, recA, _ := newGuildPair(t)
	createGuild(t, srv, sessA, "Rm1")
	createGuild(t, srv, sessB, "Rm2")
	if res, err := srv.deps.guild.CreateAllianceAsync(srv.guildOfCharacter("alpha"), srv.guildOfCharacter("beta")); err != nil ||
		res != api.AllianceCreationSuccess {
		t.Fatalf("前置建盟失败: %v %v", res, err)
	}

	srv.handleRemoveAllianceGuild(sessA, removeAllyFrame("Rm1"))

	f := findFrame(recA, 0xC1, 0xEB)
	if f == nil {
		t.Fatal("应收到 C1 EB 01 结果")
	}
	if !s2c.AsRemoveAllianceGuildResult(f).Result() {
		t.Fatal("移出自己的战盟应回成功")
	}
	if _, inAlliance := srv.deps.guild.AllianceMasterOfAsync(srv.guildOfCharacter("beta")); inAlliance {
		t.Fatal("盟主退出后整个同盟应已解散（beta 也不再在同盟里）")
	}
}

// TestRemoveAllianceRequiresAllianceMaster 验证非盟主移别人家的盟 → NoAuthorization。
func TestRemoveAllianceRequiresAllianceMaster(t *testing.T) {
	srv, sessA, sessB, _, recB := newGuildPair(t)
	createGuild(t, srv, sessA, "Ns1")
	createGuild(t, srv, sessB, "Ns2")

	srv.handleRemoveAllianceGuild(sessB, removeAllyFrame("Ns1"))

	r := findRelResult(recB)
	if r == nil || byte(r.Result()) != action.ResultGuildRelationshipNoAuthorization {
		t.Fatalf("非盟主移别人家的盟应收到 NoAuthorization(17)，got %+v", r)
	}
}

// TestAllianceListWithoutGuild 验证无战盟时不发清单（原版 AllianceListRequestHandler 的 return）。
func TestAllianceListWithoutGuild(t *testing.T) {
	srv, sessA, _, recA, _ := newGuildPair(t)

	srv.handleAllianceListRequest(sessA)

	if findFrame(recA, 0xC2, 0xE9) != nil {
		t.Fatal("无战盟时不应发 E9 清单")
	}
}

// TestCancelGuildCreationClosesDialog 验证 C1 57 只关"战盟管理员"对话，别的窗口不动。
func TestCancelGuildCreationClosesDialog(t *testing.T) {
	srv, sessA, _, _, _ := newGuildPair(t)
	guildMasterNpc := &npc.Npc{ID: 0x2C1, Number: 52, Name: "Guild Master",
		Def: &config.Monster{NpcWindow: remote.NpcWindowGuildMaster}}

	sessA.setOpenedNpc(guildMasterNpc)
	srv.handleCancelGuildCreation(sessA)
	if sessA.getOpenedNpc() != nil {
		t.Fatal("57 应关掉战盟管理员对话")
	}

	other := &npc.Npc{ID: 0x2C2, Number: 53, Name: "Trader", Def: &config.Monster{NpcWindow: 1}}
	sessA.setOpenedNpc(other)
	srv.handleCancelGuildCreation(sessA)
	if sessA.getOpenedNpc() != other {
		t.Fatal("57 不应关掉其它 NPC 的对话")
	}
}
