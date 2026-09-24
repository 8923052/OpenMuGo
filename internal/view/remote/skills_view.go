package remote

// skills_view.go —— T2-11 技能出站实现：
//   - C3 19 SkillAnimation（ShowSkillAnimationPlugIn）：技能号 + 施法者 + 目标；
//   - C3 1E AreaSkillAnimation（ShowAreaSkillAnimationPlugIn）：技能号 + 施法者 +
//     目标区中心 + 朝向。

import (
	s2c "mugo/internal/proto/s2c"
)

// ShowSkillAnimation 实现 action.PlayerView（C3 19）。
func (v *PlayerView) ShowSkillAnimation(playerID, skillID, targetID uint16) error {
	p := s2c.NewSkillAnimation()
	p.SetSkillId(skillID)
	p.SetPlayerId(playerID)
	p.SetTargetId(targetID)
	return v.send.Send(p.Bytes())
}

// ShowAreaSkillAnimation 实现 action.PlayerView（C3 1E）。
func (v *PlayerView) ShowAreaSkillAnimation(playerID, skillID uint16, x, y, rotation byte) error {
	p := s2c.NewAreaSkillAnimation()
	p.SetSkillId(skillID)
	p.SetPlayerId(playerID)
	p.SetPointX(x)
	p.SetPointY(y)
	p.SetRotation(rotation)
	return v.send.Send(p.Bytes())
}
