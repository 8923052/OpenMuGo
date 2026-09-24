package gameserver

// player_death.go —— 玩家死亡重生（T2-7 前置，对照原版 OnDeathAsync +
// RespawnAtAsync）：死亡 3s 后满血回出生门（safezone 内可走格），下发
// MapChanged（C3 1C 0F）→ 客户端 F3 12 重入世界。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
)

// respawnDeadPlayer 把死亡玩家的角色复活：满血、回出生门可走格、
// 下发 MapChanged 并等待客户端 F3 12 重入（对照原版 RespawnAtAsync 的新客户端分支）。
func (s *Server) respawnDeadPlayer(sess *session) {
	c := sess.getSelected()
	if c == nil || c.Stats == nil {
		return
	}
	// 尚在世界中（死亡时未摘除）则先摘除。
	if wp := sess.getWorldPlayer(); wp != nil {
		s.world.Map(wp.MapNumber).Leave(wp.ID)
		s.world.FreeID(wp.ID)
		sess.setWorldPlayer(nil)
	}

	// 满状态复活（原版 SetReclaimableAttributesToMaximum）：四项 Current 全部顶到 Maximum。
	// 实测坑：只回血不回蓝/AG/护盾 → 复活后 HP 满但 AG、SD 一直空，客户端条也不动。
	// 死亡清除：StopByDeath 的 buff（生命之光等）移除并去激活（ClearEffectsAfterDeathAsync）。
	if el := sess.peekEffects(); el != nil {
		for _, e := range el.ClearByDeath() {
			if view := s.viewFor(sess); view != nil {
				_ = view.ShowMagicEffectStatus(false, world.ConstantPlayerID, byte(e.Definition.Number))
			}
		}
	}
	c.Stats.CurrentHealth = c.Stats.MaximumHealth
	c.Stats.CurrentMana = c.Stats.MaximumMana
	c.Stats.CurrentAbility = c.Stats.MaximumAbility
	c.Stats.CurrentShield = c.Stats.MaximumShield
	// 护盾 hiatus 归零：刚复活视为"未被打断"，回到安全区即开始回盾。
	sess.endRegen(action.RegenRemainder{}, 0)
	// 复活=移动（原版 AttackableMoved 清 IsResting）；重生态快照重算。
	sess.setResting(false)
	if cv, err := player.ResolveCombatValues(s.deps.cfg.GameConfig, c); err == nil {
		sess.setCombatValues(&cv)
	}
	c.MapNumber = 0 // Lorencia 出生门；多地图出生门泛化随 T2-7

	// 出生门内第一个可走+安全区格（确定性扫描；对照 GetSpawnGateOfCurrentMap 的随机点语义，MVP 取首个）。
	if rx, ry, ok := s.spawnCellFor(c.MapNumber); ok {
		c.X, c.Y = rx, ry
	}

	// 角色实体位置已更新；下发 MapChanged → 客户端回 F3 12 → enterWorld 重建视野。
	sess.setState(entity.StateEnteringMap)
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowMapChanged(c.MapNumber, c.X, c.Y, c.Rotation)
	}
	s.deps.logger.Printf("gameserver: 玩家重生 %s map=%d (%d,%d)", c.Name, c.MapNumber, c.X, c.Y)
}

// spawnCellFor 在地图 safezone 内返回第一个可走格（出生门语义的 MVP 近似：
// Lorencia 的 safezone 即城镇范围，玩家出生/重生都在其中）。
func (s *Server) spawnCellFor(mapNumber uint16) (byte, byte, bool) {
	terrain := s.world.Map(mapNumber).Terrain()
	if terrain == nil {
		return 0, 0, false
	}
	// 从城镇中心向外扫描（确定性）。
	for y := byte(96); y < 200; y++ {
		for x := byte(96); x < 200; x++ {
			if terrain.Walkable(x, y) && terrain.Safezone(x, y) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}
