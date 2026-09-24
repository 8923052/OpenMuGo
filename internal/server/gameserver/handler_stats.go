package gameserver

// handler_stats.go —— T2-8 属性点分配（F3 06 IncreaseCharacterStatPoint），
// 对照原版：
//   - GameServer/MessageHandler/Character/CharacterStatIncreasePacketHandlerPlugIn
//     （F3 组 sub 0x06，amount 固定 1）；
//   - GameLogic/PlayerActions/Character/IncreaseStatsAction.IncreaseStatsAsync
//     （判定在 action.IncreaseStat：点数不足拒绝、未知属性拒绝）；
//   - GameServer/RemoteView/Character/StatIncreaseResultExtendedPlugIn
//     （C1 F3 06 扩展 24B，[MinimumClient(106,3)] → S6 客户端恒发扩展）。
//
// 分配后属性图联动：c.Stats 的 Base* 增量 → resolveCharStats 重建 →
// Maximum Health/Mana/Shield/Ability 实时变化（随 F3 06 的四项新上限下发）。
// 原版 MaximumValue 钳制分支因 Base* 属性无上限而不触发（本仓同）。

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	c2s "mugo/internal/proto/c2s"
)

// handleStatIncrease 处理 F3 06：分配一个属性点。
func (s *Server) handleStatIncrease(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	if c == nil {
		return
	}
	if len(frame) < c2s.IncreaseCharacterStatPointLength {
		return
	}
	req := c2s.AsIncreaseCharacterStatPoint(frame)
	stat := action.StatType(req.StatType())
	// 客户端点一次 +1（F3 06 无数量字段）。
	s.allocateStatPoints(sess, c, stat, 1)
}

// allocateStatPoints 分配 amount 点到一条属性上：判定在 action.IncreaseStat，
// 成功后重建属性图并发 C1 F3 06 结果包（对照 IncreaseStatsAction.IncreaseStatsAsync）。
// 返回 false 表示未分配（点数不足/属性不可加/数量为 0），失败时按原版发对应蓝字。
func (s *Server) allocateStatPoints(sess *session, c *entity.Character, stat action.StatType, amount uint16) bool {
	// 当前态：无 Stats 先解析（与搬运处理器同一守卫语义）。
	st := c.Stats
	if st == nil {
		resolved, err := s.resolveCharStats(c)
		if err != nil {
			s.deps.logger.Printf("gameserver: 加点前解析角色属性失败 %s: %v", c.Name, err)
			return false
		}
		st = resolved
		c.Stats = st
	}

	baseValue := stat.StatBaseValue(st.Strength, st.Agility, st.Vitality, st.Energy, st.Leadership)
	res := action.IncreaseStat(action.StatAllocation{
		Stat:          stat,
		Amount:        amount,
		LevelUpPoints: st.LevelUpPoints,
		BaseValue:     baseValue,
	})
	if !res.OK {
		// 原版蓝字分支（IncreaseStatsAction:37/63），且**不回确认包**（客户端数值不动）。
		// 两者皆 false 的第三种情形是 amount==0 → 原版抛 ArgumentOutOfRangeException，
		// 被命令基类 catch 成日志 → 客户端无感（/add 0 走这条）。
		switch {
		case res.NotEnoughPoints:
			s.showLocalizedMessage(sess, player.MsgNotEnoughLevelUpPoints)
		case res.UnknownStat:
			s.showLocalizedMessage(sess, player.MsgAttributeNotAvailable)
		}
		s.deps.logger.Printf("gameserver: 加点拒绝 %s stat=%d points=%d", c.Name, stat, st.LevelUpPoints)
		return false
	}

	// 落账：Base* 增量 + 点数扣减（原版 attributes[attr] += amount; LevelUpPoints -= amount）。
	switch stat {
	case action.StatStrength:
		st.Strength = res.NewBaseValue
	case action.StatAgility:
		st.Agility = res.NewBaseValue
	case action.StatVitality:
		st.Vitality = res.NewBaseValue
	case action.StatEnergy:
		st.Energy = res.NewBaseValue
	case action.StatLeadership:
		st.Leadership = res.NewBaseValue
	}
	st.LevelUpPoints = res.NewLevelUpPoints

	// 属性图联动：重建 92B（charStatOverrides 以 Base* 覆盖 → 派生上限实时更新）。
	if resolved, err := s.resolveCharStats(c); err == nil {
		c.Stats = resolved
	} else {
		s.deps.logger.Printf("gameserver: 加点后解析属性失败 %s: %v", c.Name, err)
	}
	if wp := sess.getWorldPlayer(); wp != nil {
		wp.Stats = c.Stats
	}

	// 下发（C1 F3 06 扩展 24B：属性 + 本次点数 + 四项新上限）。
	if view := s.viewFor(sess); view != nil && c.Stats != nil {
		_ = view.ShowStatIncreaseResult(stat, res.NewBaseValue-baseValue, action.MaximumStats{
			Health:  c.Stats.MaximumHealth,
			Mana:    c.Stats.MaximumMana,
			Shield:  c.Stats.MaximumShield,
			Ability: c.Stats.MaximumAbility,
		})
	}
	return true
}
