package gameserver

// handler_warp.go —— 换图传送门（doc/15 §10 T1-7，对照原版
// GameServer/MessageHandler/WalkGateHandlerPlugIn.cs + PlayerMapTransitions.cs）。
//
// 流程（与客户端协议时序一致）：
//  1. 行走落点落在当前地图的 EnterGate 矩形内（含等级校验）；
//  2. 从旧地图 AoI 摘除（不触发"离开游戏"事件——换图不断线）；
//  3. 更新角色实体的地图/坐标（目标出口门内的第一个可走格）；
//  4. 下发 92B CharacterInformation（新地图号/坐标）→ 客户端重载地图；
//  5. 客户端回 F3 12 → handleClientReady 在新地图重建视野。
//
// 目标落点：原版在出口门矩形内随机取可走格（Terrain.GetWalkableCoordinate）；
// M7 用确定性的"门内第一个可走格"（按导出件地形解析判定）。

import (
	"fmt"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// checkGateAfterWalk 行走成功后检查落点是否触发传送门。
func (s *Server) checkGateAfterWalk(sess *session, c *entity.Character, wp *world.Player) {
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	mp, ok := gc.Map(int(wp.MapNumber))
	if !ok {
		return
	}
	gate := mp.EnterGateAt(wp.X, wp.Y)
	if gate == nil || gate.Target == nil || gate.Target.Map == nil {
		return
	}
	if !s.enterGateAllowed(sess, c, gate) {
		return
	}
	s.warpThroughGate(sess, c, wp, gate)
}

// enterGateAllowed 是原版 WarpGateAction.IsWarpLegitAsync 的等级与地图需求两道检查
// （不满足按原版发蓝字）：等级门槛按职业折减（:45-50），再查目标图属性需求（:51-55）。
func (s *Server) enterGateAllowed(sess *session, c *entity.Character, gate *config.EnterGate) bool {
	gc := s.deps.cfg.GameConfig
	if gate.Target == nil || gate.Target.Map == nil {
		return false
	}
	class, _ := gc.Class(int(c.ClassNumber))
	// 等级门槛按职业折减（WarpGateAction.cs:45 + GetEffectiveMoveLevelRequirement）。
	if need := class.EffectiveMoveLevelRequirement(gate.LevelRequirement); c.Level < uint16(need) {
		s.deps.logger.Printf("gameserver: 等级不足（需 %d，原始 %d 折减后），门 %d 不传送 %s",
			need, gate.LevelRequirement, gate.Number, c.Name)
		s.showLocalizedMessage(sess, player.MsgLevelTooLowToEnter)
		return false
	}
	// 进图属性需求（WarpGateAction.cs:51-55 → TryGetRequirementError）：不满足只拒绝传送并回蓝字。
	if target, ok := gc.Map(*gate.Target.Map); ok {
		if text, rejected := player.MapEntryRequirementFailure(gc, c, target); rejected {
			s.deps.logger.Printf("gameserver: 地图 %d 需求未满足（%s），门 %d 不传送 %s",
				*gate.Target.Map, text, gate.Number, c.Name)
			s.showBlueMessage(sess, player.LocalizedMessage(player.MsgMissingMapRequirement, text))
			return false
		}
	}
	return true
}

// handleEnterGate 处理 C3 1C（对照 WarpGateHandlerPlugIn）：客户端自己判断踩到了门，
// 按**门编号**请求进图——原版全程只有这一条 EnterGate 消费路径（`grep EnterGates` 只命中
// 本 handler），本仓 checkGateAfterWalk 的落点矩形判定是 M7 期的额外保险，两者并存。
// gateNumber==0 在原版是法师瞬间移动形态（WizardTeleportAction），随 doc/17 C 组挂账。
func (s *Server) handleEnterGate(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.EnterGateRequestLength {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	gc := s.deps.cfg.GameConfig
	if wp == nil || c == nil || gc == nil {
		return
	}
	number := int(c2s.AsEnterGateRequest(frame).GateNumber())
	if number == 0 {
		s.deps.logger.Printf("gameserver: 1C gateNumber=0（法师瞬间移动形态）未接 %s", c.Name)
		return
	}
	mp, ok := gc.Map(int(wp.MapNumber))
	if !ok {
		return
	}
	gate := mp.EnterGateByNumber(number)
	if gate == nil {
		s.deps.logger.Printf("gameserver: 门 %d 不在地图 %d，1C 忽略 %s", number, wp.MapNumber, c.Name)
		return
	}
	// 原版的失败出口统一发 MapChangeFailed（C3 1C 且 Flag=0，客户端只做落点不切图）。
	if !s.enterGateAllowed(sess, c, gate) || !enterGateInRange(wp.X, wp.Y, gate) {
		s.deps.logger.Printf("gameserver: 门 %d 拒绝进入（需求或距离）%s", number, c.Name)
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowMapChangeFailed(wp.MapNumber, wp.X, wp.Y, wp.Rotation)
		}
		return
	}
	s.warpThroughGate(sess, c, wp, gate)
}

// enterGateInRange 是原版 WarpGateAction 的距离检查（:57-63 + IsXInRange/IsYInRange）：
// 落点须在门矩形外扩 InfoRange 的范围内。
func enterGateInRange(x, y byte, gate *config.EnterGate) bool {
	ir := int(world.InfoRange)
	px, py := int(x), int(y)
	return px >= gate.X1-ir && px <= gate.X2+ir && py >= gate.Y1-ir && py <= gate.Y2+ir
}

// warpThroughGate 执行换图：摘旧图 → 更新角色位置 → 下发 MapChanged（C3 1C）→
// 等 F3 12 重建视野。
//
// 对照原版 PlayerMapTransitions.WarpToAsync：摘旧图 → PlaceAtGateAsync（更新角色
// 位置/朝向）→ IMapChangePlugIn.MapChangeAsync（**C3 1C MapChanged**，不是 92B！）→
// 客户端 LoadWorld 后回 F3 12 → ClientReadyAfterMapChangeAsync 进新图。
// 实测坑：本仓曾发 92B CharacterInformation——那是**选角登录**专用包；游戏内收到时
// MuMain 的 CurrentProtocolState 已不是 REQUEST_JOIN_MAP_SERVER，不会重载地图也不回
// F3 12 → 服务端停在 EnteringMap，所有 handler 的状态守卫拒绝入站 → "看不到怪物、
// 无法移动"。真机修复 10（2026-09-20）。
func (s *Server) warpThroughGate(sess *session, c *entity.Character, wp *world.Player, gate *config.EnterGate) {
	target := gate.Target
	// 目标落点：门矩形内第一个可走格（确定性；原版为门内随机可走格）。
	tx, ty := s.firstWalkableInRect(*target.Map, int(target.X1), int(target.Y1), int(target.X2), int(target.Y2))

	// 1. 从旧地图 AoI 摘除（不发布"离开游戏"——换图不断线）。
	s.world.Map(wp.MapNumber).Leave(wp.ID)
	s.world.FreeID(wp.ID) // 对象 ID 回收（换图 = 旧图移除 + 新图重新分配，原版同池语义）
	sess.setWorldPlayer(nil)

	// 2. 更新角色实体的位置（F3 12 重建视野时使用；原版 PlaceAtGateAsync 同序）。
	c.MapNumber = uint16(*target.Map)
	c.X, c.Y = tx, ty
	c.Rotation = byte(target.Direction)

	// 3. 下发 MapChanged → 客户端重载地图后回 F3 12。
	sess.setState(entity.StateEnteringMap)
	if view := s.viewFor(sess); view != nil {
		if err := view.ShowMapChanged(c.MapNumber, c.X, c.Y, c.Rotation); err != nil {
			s.deps.logger.Printf("gameserver: 换图下发 MapChanged 失败: %v", err)
			return
		}
	}
	s.deps.logger.Printf("gameserver: 换图 %s → 地图 %d (%d,%d) 经门 %d", c.Name, *target.Map, tx, ty, gate.Number)
}

// handleWarpCommand 处理 C1 8E 02（T2-10）：传送清单付费换图，
// 对照原版 WarpHandlerPlugIn → WarpAction.WarpToAsync.CheckRequirements（:34-65），
// 检查顺序也是原版的：
//  1. 等级：`GetEffectiveMoveLevelRequirement`（MG/DL/RF 线带 34% 折减；门槛 400 不折减），
//     蓝字打印的是**折减后**的数字；
//  2. 门目标是否初始化；
//  3. 目标地图的进图属性需求（TryGetRequirementError）；
//  4. 金币（**最后**查，原版注释原文 "avoid getting zen when other checks failed"）：
//     TryRemoveMoney → 扣款 + 金币更新帧；
//  5. Player.WarpToAsync(warpInfo.Gate) → 走 T1-7 的换图链。
//
// 拒绝原因按原版发蓝字（UnknownWarpIndex / WarpAction 里就地拼接的等级、金额文案）。
func (s *Server) handleWarpCommand(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld || len(frame) < c2s.WarpCommandRequestLength {
		return
	}
	if frame[3] != c2s.WarpCommandRequestSubCode {
		return
	}
	wp := sess.getWorldPlayer()
	c := sess.getSelected()
	if wp == nil || c == nil || c.Stats == nil {
		return
	}
	gc := s.deps.cfg.GameConfig
	if gc == nil {
		return
	}
	req := c2s.AsWarpCommandRequest(frame)
	warp, ok := gc.WarpByIndex(int(req.WarpInfoIndex()))
	if !ok {
		// 原版 WarpHandlerPlugIn:44 不传参 → 占位符原样到客户端（照抄，见 doc/16）。
		s.deps.logger.Printf("gameserver: 传送索引 %d 不存在（UnknownWarpIndex）", req.WarpInfoIndex())
		s.showLocalizedMessage(sess, player.MsgUnknownWarpIndex)
		return
	}
	class, _ := gc.Class(int(c.ClassNumber))
	// 1. 等级（原版：折减后的门槛，蓝字也打印折减后的值）。
	if need := class.EffectiveMoveLevelRequirement(warp.LevelRequirement); c.Level < uint16(need) {
		s.deps.logger.Printf("gameserver: 等级不足（需 %d，原始 %d），传送 %s 拒绝 %s",
			need, warp.LevelRequirement, warp.Name, c.Name)
		s.showBlueMessage(sess, fmt.Sprintf("You need to be level %d in order to warp", need))
		return
	}
	// 2. 门目标初始化。
	if warp.Gate == nil || warp.Gate.Map == nil {
		s.deps.logger.Printf("gameserver: 传送 %d 目标未初始化", warp.Index)
		s.showBlueMessage(sess, "The warp target is not initialized")
		return
	}
	// 3. 目标地图的进图属性需求。
	if target, ok := gc.Map(*warp.Gate.Map); ok {
		if text, rejected := player.MapEntryRequirementFailure(gc, c, target); rejected {
			s.deps.logger.Printf("gameserver: 地图 %d 需求未满足（%s），传送 %s 拒绝 %s",
				*warp.Gate.Map, text, warp.Name, c.Name)
			s.showBlueMessage(sess, player.LocalizedMessage(player.MsgMissingMapRequirement, text))
			return
		}
	}
	// 4. 金币（最后查）。
	costs := uint32(warp.Costs)
	if c.Stats.Money < costs {
		s.deps.logger.Printf("gameserver: 金额不足（需 %d），传送 %s 拒绝 %s", warp.Costs, warp.Name, c.Name)
		s.showBlueMessage(sess, fmt.Sprintf("You need %d in order to warp", warp.Costs))
		return
	}
	c.Stats.Money -= costs
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowInventoryMoneyUpdate(c.Stats.Money)
	}
	// 5. 换图（T1-7 链：摘旧图 → 更新实体 → C3 1C MapChanged → 等 F3 12）。
	s.warpThroughGate(sess, c, wp, &config.EnterGate{Number: 0, Target: warp.Gate})
}

// firstWalkableInRect 在矩形内返回第一个可走格（地形缺失时取左上角）。
func (s *Server) firstWalkableInRect(mapNumber int, x1, y1, x2, y2 int) (byte, byte) {
	m := s.world.Map(uint16(mapNumber))
	for x := x1; x <= x2; x++ {
		for y := y1; y <= y2; y++ {
			if m.Walkable(byte(x), byte(y)) {
				return byte(x), byte(y)
			}
		}
	}
	return byte(x1), byte(y1)
}
