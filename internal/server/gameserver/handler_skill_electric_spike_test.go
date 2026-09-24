package gameserver

// handler_skill_electric_spike_test.go —— TRIM-11a 的另一半：Electric Spike(65) 打中目标后
// 抽掉"看得见施法者"的队友 20% 当前生命 / 5% 当前法力
// （对照 AreaSkillAttackAction.cs:361-372 与 :23 的硬编码技能号）。

import (
	"testing"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/util"
)

// newPartyWithStats 组一对已入队的队友，铺好可断言的血蓝，并在洛里亚放出怪。
func newPartyWithStats(t *testing.T) (*Server, *world.Player, *world.Player, *packetRecorder) {
	t.Helper()
	srv, sessA, sessB, _, recB := newPartyPair(t)
	sp := npc.NewSpawner(srv.deps.cfg.GameConfig, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	srv.deps.cfg.NPCs = sp
	srv.handlePartyInvite(sessA, inviteFrame(8))
	resp := c2s.NewPartyInviteResponse()
	resp.SetAccepted(true)
	srv.handlePartyResponse(sessB, resp.Bytes())

	for _, pair := range []struct {
		sess *session
		wp   *world.Player
	}{
		{sessA, sessA.getWorldPlayer()}, {sessB, sessB.getWorldPlayer()},
	} {
		c := pair.sess.getSelected()
		c.Stats = &entity.CharStats{CurrentHealth: 1000, CurrentMana: 500, CurrentAbility: 300}
		pair.wp.Stats = c.Stats
		pair.wp.IsAlive = true // 正常由 handler_enter 置真，本脚手架绕过它
	}
	return srv, sessA.getWorldPlayer(), sessB.getWorldPlayer(), recB
}

func spikeArea() *config.AreaSkillSettings {
	return &config.AreaSkillSettings{MinHitsPerTarget: 1, MaxHitsPerTarget: 1,
		HitChancePerDistanceMultiplier: 1}
}

func spikeDef() *action.SkillDef {
	return &action.SkillDef{Number: skillElectricSpike, Target: action.SkillTargetExplicit,
		DamageType: action.DamageTypePhysical, Range: 9}
}

// TestElectricSpikeDrainsVisiblePartyMember：打中过目标 → 附近可见队友被抽血抽蓝，
// 并补发一帧当前状态（原版靠属性变更事件传播，本仓显式发，见 doc/16）。
func TestElectricSpikeDrainsVisiblePartyMember(t *testing.T) {
	srv, caster, member, recMember := newPartyWithStats(t)
	sessCaster := srv.sessionOfCharacter(caster.Name)
	c := sessCaster.getSelected()
	ax, ay := placeAtNonSafezone(t, srv, caster, c)
	member.X, member.Y = ax, ay+1
	targets := areaTestTargets(t, srv, ax, ay)

	srv.applyAreaHits(sessCaster, c, caster, softCombatValues(), spikeDef(), spikeArea(), targets[:1], false)

	st := member.Stats
	if st.CurrentHealth != 800 || st.CurrentMana != 475 {
		t.Fatalf("队友应被抽成 800 血 / 475 蓝, got %d/%d", st.CurrentHealth, st.CurrentMana)
	}
	f := findFrame(recMember, 0xC1, 0x26) // CurrentStatsExtended（C1 26 FF）
	if f == nil {
		t.Fatal("应给被抽的队友补发一帧当前状态")
	}
	p := s2c.AsCurrentStatsExtended(f)
	if p.Health() != 800 || p.Mana() != 475 {
		t.Fatalf("当前状态帧应带抽后值: hp=%d mp=%d", p.Health(), p.Mana())
	}
}

// TestElectricSpikeSkipsFarAndNotHit：没打中任何目标（无目标）或队友看不见施法者时都不抽。
func TestElectricSpikeSkipsFarAndNotHit(t *testing.T) {
	srv, caster, member, _ := newPartyWithStats(t)
	sessCaster := srv.sessionOfCharacter(caster.Name)
	c := sessCaster.getSelected()
	ax, ay := placeAtNonSafezone(t, srv, caster, c)

	// ① 零目标 → attackCount 0 → 不进抽取分支。
	srv.applyAreaHits(sessCaster, c, caster, softCombatValues(), spikeDef(), spikeArea(),
		[]*npc.Npc{}, false)
	if member.Stats.CurrentHealth != 1000 {
		t.Fatalf("没打中目标不该抽队友: %d", member.Stats.CurrentHealth)
	}

	// ② 打中了，但队友在地图另一头（原版 Observers 判定）。
	member.X, member.Y = 250, 250
	targets := areaTestTargets(t, srv, ax, ay)
	srv.applyAreaHits(sessCaster, c, caster, softCombatValues(), spikeDef(), spikeArea(),
		targets[:1], false)
	if member.Stats.CurrentHealth != 1000 {
		t.Fatalf("队友不在视野内不该被抽: %d", member.Stats.CurrentHealth)
	}

	// ③ 打中了、队友也在视野内，但用的不是 65 号技能 → 不抽。
	member.X, member.Y = caster.X, caster.Y+1
	other := spikeDef()
	other.Number = 19
	srv.applyAreaHits(sessCaster, c, caster, softCombatValues(), other, spikeArea(),
		targets[:1], false)
	if member.Stats.CurrentHealth != 1000 {
		t.Fatalf("非 Electric Spike 不该抽队友: %d", member.Stats.CurrentHealth)
	}
}
