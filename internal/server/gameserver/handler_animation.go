package gameserver

// handler_animation.go —— C1 18 AnimationRequest（原版 AnimationHandlerPlugIn）。
//
// 布局（ClientToServerPacketsRef.cs）：[3]=Rotation、[4]=AnimationNumber。
// 0x80/0x6C=坐下、0x81/0x6D=倚靠、0x82/0x6E=悬挂 → 原版置 Pose 并把
// IsResting 属性置 1（休息恢复：HP 只在休息时回，MP 休息加成）。
// 广播用同一 C1 18（S2C ObjectAnimation）发给观察者；方向字节往返恒等
// （ParseAsDirection=+1、ToPacketByte=-1），直接回显客户端字节。

// restAnimations 是触发 IsResting 的动画号（原版 switch 的 Pose > Standing 分支）。
var restAnimations = map[byte]bool{
	0x80: true, 0x6C: true, // Sitting
	0x81: true, 0x6D: true, // Leaning
	0x82: true, 0x6E: true, // Hanging
}

func (s *Server) handleAnimation(sess *session, frame []byte) {
	if len(frame) < 5 {
		return
	}
	rotation, animation := frame[3], frame[4]
	c := sess.getSelected()
	wp := sess.getWorldPlayer()
	if c == nil || wp == nil {
		return
	}
	wp.Rotation = rotation
	c.Rotation = rotation
	// 原版只在此处置 1、无 else 分支（清除在移动插值 UpdateIsInSafezoneAfterPlayerMoved）。
	if restAnimations[animation] && sess.setResting(true) {
		// 休息态改变恢复倍率（0.03×IsResting 关系），快照必须重算。
		s.refreshCombatValues(sess, c, true)
	}
	// 原版 ForEachWorldObserverAsync ShowAnimationAsync：发给观察者（不含自己），
	// 目标为空 → TargetId=0。
	for _, o := range s.world.Map(wp.MapNumber).PlayersInRangeFor(wp.X, wp.Y) {
		if o.View == nil || o.ID == wp.ID {
			continue
		}
		_ = o.View.ShowObjectAnimation(wp.ID, rotation, animation, 0)
	}
}
