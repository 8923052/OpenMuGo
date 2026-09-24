package gameserver

// handler_master.go —— 大师域编排（TRIM-09）：入站 F3 0x52 加点、进图后的大师状态、
// 大师经验入账与大师升级。判定在 action（master_points.go / master_experience.go），
// 出站编码在 view/remote（master_view.go）；本文件只做装配与顺序。
//
// 对照原版：
//   - MessageHandler/Character/CharacterAddMasterPointPacketHandlerPlugIn.cs（0x52，无版本门槛）
//   - PlayerActions/Character/AddMasterPointAction.cs（失败**只记日志**，客户端无感）
//   - RemoteView/Character/UpdateCharacterStatsPlugIn.cs:70-74（IsMasterClass 才发大师状态）
//   - RemoteView/Character/UpdateMasterStatsExtendedPlugIn.cs:52（F3 50 之后紧跟 F3 53）
//   - PlayerExperience.AddMasterExperienceCoreAsync（大师经验分支）
//   - RemoteView/Character/UpdateLevelPlugIn.cs:59-80（F3 51 + MasterLevelUpCongrats）

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// desMasterPointsPerLevelUp 是"每升一级大师给几点"的属性 designation
// （原版 Stats.MasterPointsPerLevelUp，职业表里初值 1）。
const desMasterPointsPerLevelUp = "Master points per master Level up"

// handleAddMasterPoint 处理 C1 F3 0x52。成功才发包；失败与原版一致只写日志。
func (s *Server) handleAddMasterPoint(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	c := sess.getSelected()
	if c == nil || len(frame) < c2s.AddMasterSkillPointLength {
		return
	}
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	res := action.AddMasterPoint(gc, c, c2s.AsAddMasterSkillPoint(frame).SkillId())
	if !res.OK() {
		s.deps.logger.Printf("gameserver: 大师加点未生效 %s skill=%d → %s",
			c.Name, c2s.AsAddMasterSkillPoint(frame).SkillId(), res.Outcome)
		return
	}
	view := s.viewFor(sess)
	if view == nil {
		return
	}
	// 被动大师技能的效果直接进属性系统（原版 CreatePowerUpForPassiveSkill），
	// 上限随之变化：静默重建角色属性快照（原版同样不下发属性包）。
	if wp := sess.getWorldPlayer(); wp != nil {
		s.refreshCharStats(c, wp)
	}
	// 新学一条**主动**大师技会替换低级技能（SkillList.cs:224 起：PassiveBoost 不进列表、
	// 被替换者从列表移除）；本仓走"整体重发技能列表"（原版是 add/remove 两包，
	// 索引语义一致），登记见 doc/16 TRIM-09。
	if res.Outcome == action.MasterPointLearned && res.Master != nil && !res.Master.PassiveBoost {
		_ = view.ShowSkillList(s.skillListViewOf(c))
	}
	if res.Master != nil {
		_ = view.ShowMasterSkillLevelUpdate(masterSkillUpdateView(gc, c, res))
	}
}

// masterSkillUpdateView 组装 C1 F3 52（槽位号 + 展示值都取自导出表）。
func masterSkillUpdateView(gc *config.GameConfig, c *entity.Character, res action.MasterPointResult) action.MasterSkillUpdateView {
	return action.MasterSkillUpdateView{
		Points:              uint16(res.Points),
		SkillIndex:          byte(gc.MasterSkillIndex(int(c.ClassNumber), res.Master.Number)),
		SkillNumber:         uint16(res.Master.Number),
		Level:               res.Level,
		DisplayValue:        res.Master.DisplayAt(int(res.Level)),
		DisplayValueOfNextL: res.Master.NextDisplayValue(int(res.Level)),
	}
}

// sendMasterStats 在角色信息包之后下发大师状态（F3 50）+ 已学大师技能列表（C2 F3 53）。
// 非大师职业整段跳过（对照 UpdateCharacterStatsPlugIn.cs:71 的 IsMasterClass 门）。
func (s *Server) sendMasterStats(sess *session, c *entity.Character, st *entity.CharStats) {
	if c == nil || st == nil {
		return
	}
	gc := s.deps.cfg.GameConfig
	view := s.viewFor(sess)
	if gc == nil || view == nil {
		return
	}
	cls, ok := gc.Class(int(c.ClassNumber))
	if !ok || !cls.IsMasterClass {
		return
	}
	_ = view.ShowMasterStats(action.MasterStatsView{
		MasterLevel:           c.MasterLevel,
		MasterExperience:      uint64(c.MasterExperience),
		ExperienceOfNextLevel: uint64(masterExperienceAt(gc, int(c.MasterLevel)+1)),
		MasterLevelUpPoints:   uint16(c.MasterLevelUpPoints),
		MaximumHealth:         st.MaximumHealth,
		MaximumMana:           st.MaximumMana,
		MaximumShield:         st.MaximumShield,
		MaximumAbility:        st.MaximumAbility,
	})
	_ = view.ShowMasterSkillList(masterSkillListView(gc, c))
}

// masterSkillListView 把已学大师技能投影为 C2 F3 53 条目。
// 原版遍历 SkillList 字典（插入序）；本仓按技能号升序，保证同一角色态的下发可复现。
func masterSkillListView(gc *config.GameConfig, c *entity.Character) []action.MasterSkillEntryView {
	out := make([]action.MasterSkillEntryView, 0, len(c.LearnedSkills))
	for _, e := range c.LearnedSkills {
		m, ok := gc.MasterSkillByNumber(int(e.SkillNumber))
		if !ok {
			continue
		}
		out = append(out, action.MasterSkillEntryView{
			Index:                 byte(gc.MasterSkillIndex(int(c.ClassNumber), m.Number)),
			Level:                 e.Level,
			DisplayValue:          m.DisplayAt(int(e.Level)),
			DisplayValueOfNextLev: m.NextDisplayValue(int(e.Level)),
		})
	}
	return out
}

// settleMasterExperience 是大师经验分支（对照 AddMasterExperienceCoreAsync）。
// 返回**计算出的**经验量（掉落金钱按计算值，与普通分支同一口径）。
func (s *Server) settleMasterExperience(sess *session, c *entity.Character, wp *world.Player,
	target *npc.Npc, calculated int64) int64 {
	gc := s.deps.cfg.GameConfig
	view := s.viewFor(sess)
	g := action.ApplyMasterExperience(c.MasterLevel, c.MasterExperience, calculated,
		gc.Experience.MasterTable, gc.Experience.MaximumMasterLevel, int(target.Attribute("Level")),
		gc.Globals.MinimumMonsterLevelForMasterExperience)
	c.MasterExperience = g.NewExperience
	if view != nil {
		_ = view.ShowExperienceGained(action.ExperienceGainPacket{
			Result: g.Kind,
			// 两个门（32/33）的 Amount 都是 0，原版照样下发一包。
			Experience:     uint32(g.Amount),
			Damage:         0,
			KilledObjectID: target.ID,
			KillerObjectID: constantPlayerID,
		})
	}
	if g.LeveledUp {
		s.applyMasterLevelUp(c, wp, view)
	}
	return calculated
}

// applyMasterLevelUp 对照大师升级分支：等级 +1 → 加点 → 四项补满 → F3 51 → 蓝字 → 光效。
func (s *Server) applyMasterLevelUp(c *entity.Character, wp *world.Player, view action.PlayerView) {
	gc := s.deps.cfg.GameConfig
	c.MasterLevel++
	prevPoints := c.MasterLevelUpPoints
	per := 0.0
	if gc != nil {
		per = player.AttributeValue(gc, c, desMasterPointsPerLevelUp)
	}
	c.MasterLevelUpPoints = prevPoints + int(per)
	st := s.refreshCharStats(c, wp)
	if st == nil {
		return
	}
	// SetReclaimableAttributesToMaximum：大师升级同样把四项当前值补满。
	st.CurrentHealth = st.MaximumHealth
	st.CurrentMana = st.MaximumMana
	st.CurrentAbility = st.MaximumAbility
	st.CurrentShield = st.MaximumShield
	if view != nil {
		_ = view.ShowMasterCharacterLevel(action.MasterLevelView{
			MasterLevel:    c.MasterLevel,
			GainedPoints:   uint16(per),
			CurrentPoints:  uint16(c.MasterLevelUpPoints),
			MaximumPoints:  uint16(gc.Experience.MaximumMasterLevel),
			MaximumHealth:  st.MaximumHealth,
			MaximumMana:    st.MaximumMana,
			MaximumShield:  st.MaximumShield,
			MaximumAbility: st.MaximumAbility,
		})
		_ = view.ShowMessage(player.LocalizedMessage(player.MsgMasterLevelUpCongrats, c.MasterLevel),
			action.MessageBlueNormal)
	}
	s.showLevelUpEffect(wp, view)
}

// refreshCharStats 重建角色属性快照并同步到世界实体，返回新快照（失败时 nil）。
func (s *Server) refreshCharStats(c *entity.Character, wp *world.Player) *entity.CharStats {
	if wp == nil {
		return nil
	}
	st, err := s.resolveCharStats(c)
	if err != nil {
		s.deps.logger.Printf("gameserver: 大师域重建属性失败 %s: %v", c.Name, err)
		return nil
	}
	c.Stats = st
	wp.Stats = st
	return st
}

// masterExperienceAt 安全取大师经验表（表长 = 最大大师等级 + 2，同原版 CreateExpTable）。
func masterExperienceAt(gc *config.GameConfig, level int) int64 {
	t := gc.Experience.MasterTable
	if level < 0 || level >= len(t) {
		return 0
	}
	return t[level]
}
