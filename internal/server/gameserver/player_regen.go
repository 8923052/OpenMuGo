package gameserver

// player_regen.go —— 玩家恢复的"击杀后即时恢复"分支 + 当前值下发出口。
//
// 对照原版：
//   - GameLogic/Player.cs `AfterKilledMonsterAsync`（击杀后 min(max, cur+(倍率×max+绝对值))）
//   - GameLogic/NPC/AttackableNpcBase.cs `OnDeathAsync`（顺序：0x17 → 经验 →
//     AfterKilledMonsterAsync → 掉落）
//   - GameServer/RemoteView/Character/UpdateStatsExtendedPlugIn.cs（属性变更 → C1 26 FF/FE）
//
// 周期恢复（安全区 AG 加成、护盾 hiatus）见 npc_ai.go 的 regeneratePlayers。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
)

// recoverAfterMonsterKill 击杀怪物后的一次性恢复（原版 AfterKilledMonsterAsync）。
//
// 遍历顺序沿用 Stats.AfterMonsterKillRegenerationAttributes：法力→生命→AG→护盾。
// 这些倍率/绝对值默认全 0（来自卓越 option/镶嵌/大师技能），所以默认情况下
// 击杀**不回血蓝**——与原版一致；实测"打死怪不回红蓝"是没走这条路径，不是参数缺失。
func (s *Server) recoverAfterMonsterKill(sess *session, c *entity.Character, cv *player.CombatValues) {
	if c == nil || c.Stats == nil || cv == nil {
		return
	}
	st := c.Stats
	changed := false

	if v, ok := action.AfterKillRecover(float64(st.CurrentMana), float64(st.MaximumMana),
		float64(cv.ManaAfterKillMult), float64(cv.ManaAfterKillAbs)); ok {
		st.CurrentMana, changed = uint32(v), true
	}
	if v, ok := action.AfterKillRecover(float64(st.CurrentHealth), float64(st.MaximumHealth),
		float64(cv.HealthAfterKillMult), float64(cv.HealthAfterKillAbs)); ok {
		st.CurrentHealth, changed = uint32(v), true
	}
	if v, ok := action.AfterKillRecover(float64(st.CurrentAbility), float64(st.MaximumAbility),
		float64(cv.AbilityAfterKillMult), float64(cv.AbilityAfterKillAbs)); ok {
		st.CurrentAbility, changed = uint32(v), true
	}
	if v, ok := action.AfterKillRecover(float64(st.CurrentShield), float64(st.MaximumShield),
		float64(cv.ShieldAfterKillMult), float64(cv.ShieldAfterKillAbs)); ok {
		st.CurrentShield, changed = uint32(v), true
	}
	if !changed {
		return
	}
	// 当前值只有 C1 26 FF（24B 扩展）一个出口：客户端 case 0x26 只认
	// ReceiveStatsExtended，9B 的 CurrentHealthAndShield 会被按 24B 解析而读越界。
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowCurrentStatsExtended(currentStatsOf(st))
	}
	s.deps.logger.Printf("[debug] after-kill %s: hp=%d/%d mp=%d/%d ag=%d/%d sd=%d/%d",
		c.Name, st.CurrentHealth, st.MaximumHealth, st.CurrentMana, st.MaximumMana,
		st.CurrentAbility, st.MaximumAbility, st.CurrentShield, st.MaximumShield)
}
