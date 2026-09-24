package remote

// master_view.go —— 大师域出站（TRIM-09），对应 OpenMU
// `GameServer/RemoteView/Character/{UpdateMasterStats,UpdateMasterSkills,MasterSkillLevelChanged}`
// 三个插件 + `UpdateLevel(Extended)PlugIn.UpdateMasterLevelAsync`。
//
// 版本轴（铁律：变体只在 view/remote 里选）：F3 50 与 F3 51 各有普通与 Extended 两种布局，
// Extended 实现挂 [MinimumClient(106, 3)]（UpdateMasterStatsExtendedPlugIn.cs:21、
// UpdateLevelExtendedPlugIn.cs:23），差别是四项上限由 u16 变 u32；真机 MuMain(106/3) 收 Extended。
// F3 52 与 C2 F3 53 只有一个形态（原版无版本门槛插件）。

import (
	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// masterExtendedMin 是 F3 50/51 Extended 变体的客户端下限（对照 MinimumClient(106, 3)）。
var masterExtendedMin = version.AtLeast(version.ClientVersion{
	Season: 106, Episode: 3, Language: version.LanguageInvariant,
})

// usesExtendedMasterStats 报告大师状态两包是否应发 Extended 变体。
func (v *PlayerView) usesExtendedMasterStats() bool {
	return masterExtendedMin.Suitable(v.clientVersion)
}

// ShowMasterStats 实现 action.PlayerView（F3 50）。
// 原版发完这一包必然紧跟大师技能列表（UpdateMasterStatsPlugIn.cs:52 的插件链），
// 顺序由编排层保证（见 gameserver 的 sendMasterStats）。
func (v *PlayerView) ShowMasterStats(info action.MasterStatsView) error {
	if v.usesExtendedMasterStats() {
		p := s2c.NewMasterStatsUpdateExtended()
		p.SetMasterLevel(info.MasterLevel)
		p.SetMasterExperience(info.MasterExperience)
		p.SetMasterExperienceOfNextLevel(info.ExperienceOfNextLevel)
		p.SetMasterLevelUpPoints(info.MasterLevelUpPoints)
		p.SetMaximumHealth(info.MaximumHealth)
		p.SetMaximumMana(info.MaximumMana)
		p.SetMaximumShield(info.MaximumShield)
		p.SetMaximumAbility(info.MaximumAbility)
		return v.send.Send(p.Bytes())
	}
	p := s2c.NewMasterStatsUpdate()
	p.SetMasterLevel(info.MasterLevel)
	p.SetMasterExperience(info.MasterExperience)
	p.SetMasterExperienceOfNextLevel(info.ExperienceOfNextLevel)
	p.SetMasterLevelUpPoints(info.MasterLevelUpPoints)
	p.SetMaximumHealth(uint16(info.MaximumHealth))
	p.SetMaximumMana(uint16(info.MaximumMana))
	p.SetMaximumShield(uint16(info.MaximumShield))
	p.SetMaximumAbility(uint16(info.MaximumAbility))
	return v.send.Send(p.Bytes())
}

// ShowMasterSkillList 实现 action.PlayerView（C2 F3 53）。
// 原版在无连接/无角色时**直接返回**（UpdateMasterSkillsPlugIn.cs:40-43），空列表时
// 仍发一个 count=0 的包 —— 这里同样照发，由编排层决定何时调用。
func (v *PlayerView) ShowMasterSkillList(entries []action.MasterSkillEntryView) error {
	p := s2c.NewMasterSkillList(s2c.MasterSkillListRequiredSize(len(entries)))
	p.SetMasterSkillCount(uint32(len(entries)))
	for i, e := range entries {
		s := p.Skills(i)
		if s == nil {
			break
		}
		s.SetMasterSkillIndex(e.Index)
		s.SetLevel(e.Level)
		s.SetDisplayValue(e.DisplayValue)
		s.SetDisplayValueOfNextLevel(e.DisplayValueOfNextLev)
	}
	return v.send.Send(p.Bytes())
}

// ShowMasterCharacterLevel 实现 action.PlayerView（F3 51，大师升级）。
func (v *PlayerView) ShowMasterCharacterLevel(info action.MasterLevelView) error {
	if v.usesExtendedMasterStats() {
		p := s2c.NewMasterCharacterLevelUpdateExtended()
		p.SetMasterLevel(info.MasterLevel)
		p.SetGainedMasterPoints(info.GainedPoints)
		p.SetCurrentMasterPoints(info.CurrentPoints)
		p.SetMaximumMasterPoints(info.MaximumPoints)
		p.SetMaximumHealth(info.MaximumHealth)
		p.SetMaximumMana(info.MaximumMana)
		p.SetMaximumShield(info.MaximumShield)
		p.SetMaximumAbility(info.MaximumAbility)
		return v.send.Send(p.Bytes())
	}
	p := s2c.NewMasterCharacterLevelUpdate()
	p.SetMasterLevel(info.MasterLevel)
	p.SetGainedMasterPoints(info.GainedPoints)
	p.SetCurrentMasterPoints(info.CurrentPoints)
	p.SetMaximumMasterPoints(info.MaximumPoints)
	p.SetMaximumHealth(uint16(info.MaximumHealth))
	p.SetMaximumMana(uint16(info.MaximumMana))
	p.SetMaximumShield(uint16(info.MaximumShield))
	p.SetMaximumAbility(uint16(info.MaximumAbility))
	return v.send.Send(p.Bytes())
}

// ShowMasterSkillLevelUpdate 实现 action.PlayerView（C1 F3 52，加点成功）。
// Success 恒 true：原版只在成功分支调用（MasterSkillLevelChangedPlugIn.cs:39）。
func (v *PlayerView) ShowMasterSkillLevelUpdate(info action.MasterSkillUpdateView) error {
	p := s2c.NewMasterSkillLevelUpdate()
	p.SetSuccess(true)
	p.SetMasterLevelUpPoints(info.Points)
	p.SetMasterSkillIndex(info.SkillIndex)
	p.SetMasterSkillNumber(info.SkillNumber)
	p.SetLevel(info.Level)
	p.SetDisplayValue(info.DisplayValue)
	p.SetDisplayValueOfNextLevel(info.DisplayValueOfNextL)
	return v.send.Send(p.Bytes())
}
