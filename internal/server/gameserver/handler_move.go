package gameserver

import (
	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	c2s "mugo/internal/proto/c2s"
)

// handleWalk 处理 C1 D4 WalkRequest（对照 CharacterWalkHandlerPlugIn）。
// 仅进图状态接受；坐标规划（纯函数）在动作层，世界更新在 world，出站在视图。
func (s *Server) handleWalk(sess *session, frame []byte) {
	if sess.getState() != entity.StateEnteredWorld {
		return
	}
	wp := sess.getWorldPlayer()
	if wp == nil {
		return
	}
	req := c2s.AsWalkRequest(frame)

	// 帧长防御：WalkRequest 的 SourceX/SourceY/(StepCount|TargetRotation) 固定在 d[3..5]，
	// 短帧直接读会越界（AsWalkRequest 不做长度校验）。
	if len(frame) < action.WalkRequestHeaderSize {
		return
	}

	// 无步点请求 = 只更新朝向，绝不回拉（原版 CharacterWalkBaseHandlerPlugIn.WalkAsync 的
	// `Header.Length > 6` 判据 + PlayerMovement.WalkToAsync 的 `steps.IsEmpty` 早返回）。
	// 客户端每一下普通攻击都先发 0 步方向包，回拉会经 SetPlayerStop 掐断攻击动画（doc/08 B11）。
	if action.IsNonMovingWalk(req.StepCount(), len(frame)) {
		rot := req.TargetRotation()
		// 合法方向字节 0..7（>=8 非法：原版 ParseAsDirection 会得到未定义枚举）。
		if rot < 8 {
			wp.Rotation = rot
		}
		s.deps.logger.Printf("[debug] walk-rotate %s: rot=%d (0 步方向包，原版只改朝向) frame=%X",
			wp.Name, rot, frame)
		return
	}

	// 动作层纯函数：步数钳制 + 方向串累加 + 钳制（原版 CalculateTargetPoint 语义）。
	plan, ok := action.PlanWalk(req.SourceX(), req.SourceY(), req.Directions(), req.StepCount(), req.TargetRotation())
	if !ok {
		s.deps.logger.Printf("gameserver: 行走步点不足 steps=%d len=%d", req.StepCount(), len(req.Directions()))
		return
	}

	// 起点偏移校验（原版 IsWalkRequestValidAsync：客户端起点与服务端位置
	// 欧氏距离 > MaxAllowedWalkStartOffset=5 → 回拉同步，拒绝行走）。
	offX := int(plan.SourceX) - int(wp.X)
	offY := int(plan.SourceY) - int(wp.Y)
	resync := func() {
		s.deps.logger.Printf("[debug] walk-resync %s: srv=(%d,%d) cli=(%d,%d) off=(%d,%d) frame=%X",
			wp.Name, wp.X, wp.Y, plan.SourceX, plan.SourceY, offX, offY, frame)
		if view := s.viewFor(sess); view != nil {
			// 目标 ID 按接收者视角解析：自己 = 哨兵 0x200。
			_ = view.ShowObjectMovedInstant(world.ConstantPlayerID, wp.X, wp.Y)
		}
	}
	if offX*offX+offY*offY > 25 {
		resync()
		return
	}

	// 逐步地形检查（原版 GetWalkableStepCount）：在第一个阻挡步**截断**而非整包拒绝；
	// 首步即阻挡 → 回拉同步。
	gameMap := s.world.Map(wp.MapNumber)
	walkableCount := plan.WalkableStepCount(gameMap.Walkable)
	if walkableCount == 0 {
		fx, fy := plan.StepTarget(1)
		s.deps.logger.Printf("[debug] walk-blocked %s: srv=(%d,%d) cli=(%d,%d) step1=(%d,%d) walkable=%v frame=%X",
			wp.Name, wp.X, wp.Y, plan.SourceX, plan.SourceY, fx, fy, gameMap.Walkable(fx, fy), frame)
		resync()
		return
	}
	tgtX, tgtY := plan.StepTarget(walkableCount)
	steps := plan.Directions[:walkableCount]
	if walkableCount < len(plan.Directions) {
		s.deps.logger.Printf("[debug] walk-truncated %s: (%d,%d)→(%d,%d) 原目标(%d,%d) 截断至 %d 步",
			wp.Name, plan.SourceX, plan.SourceY, tgtX, tgtY, plan.TargetX, plan.TargetY, walkableCount)
	}

	// 源坐标以客户端上报为准（与 OpenMU 一致）；Walk 返回 AoI 集合（T1-4）。
	oldX, oldY := wp.X, wp.Y
	updated, others, entered, left, ok := gameMap.Walk(wp.ID, tgtX, tgtY, plan.Rotation)
	if !ok {
		return
	}
	// 移动清休息态（原版 UpdateIsInSafezoneAfterPlayerMoved.AttackableMoved：
	// IsInSafezone 重估 + IsResting=0）。0 步旋转包不算移动，不清。
	if walkableCount > 0 && sess.setResting(false) {
		if c := sess.getSelected(); c != nil {
			s.refreshCombatValues(sess, c, false)
		}
	}
	// 行走确认（ObjectMovedExtended）：ID 按接收者视角解析（原版 movedObject.GetId(playerOfView)）——
	// **移动者自己看到 0x200**（哨兵自 ID，与 F1 00 呼应），其他观察者看到移动者真实 ID。
	// 写成动态 ID 客户端不认（自己恒为 0x200）→ 渲染出一个跟着走的"复制人"（真机事故）。
	moveForSelf := action.WalkedMove{
		ObjectID: world.ConstantPlayerID,
		SourceX:  plan.SourceX, SourceY: plan.SourceY,
		TargetX: updated.X, TargetY: updated.Y,
		Rotation: plan.Rotation, Steps: steps,
	}
	moveForOthers := action.WalkedMove{
		ObjectID: updated.ID,
		SourceX:  plan.SourceX, SourceY: plan.SourceY,
		TargetX: updated.X, TargetY: updated.Y,
		Rotation: plan.Rotation, Steps: steps,
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowObjectWalked(moveForSelf)
	}
	for _, o := range others {
		if o.View != nil {
			_ = o.View.ShowObjectWalked(moveForOthers)
		}
	}
	// 新进入视野者收到移动者的入视野包（原版 NewPlayersInScopeExtendedPlugIn）；
	// 移动者也收到新进入者的入视野包（对称补全，原版按观察者逐个计算差分）。
	mover := action.ScopeEntry{
		ID: updated.ID, X: updated.X, Y: updated.Y, Rotation: updated.Rotation,
		Name: updated.Name, ClassNumber: updated.Class, Appearance: updated.Appearance,
	}
	if view := s.viewFor(sess); view != nil {
		for _, o := range entered {
			_ = view.ShowCharacterInScope(action.ScopeEntry{
				ID: o.ID, X: o.X, Y: o.Y, Rotation: o.Rotation,
				Name: o.Name, ClassNumber: o.Class, Appearance: o.Appearance,
			})
		}
		// 新进入视野的人里若有开店的，一并补发摊位清单（对照 NewPlayersInScopePlugIn）。
		if list := s.shopsOf(updated.ID, entered); len(list) > 0 {
			_ = view.ShowPlayerShops(list)
		}
	}
	for _, o := range entered {
		if o.View != nil {
			_ = o.View.ShowCharacterInScope(mover)
			if st := sess.store; st != nil && st.open {
				_ = o.View.ShowPlayerShops([]action.PlayerShopEntry{
					{ObjectID: updated.ID, StoreName: st.name},
				})
			}
		}
	}
	// 离开视野（T2-1，C1 14）：移动者收到离开者 ID 批量移出包；
	// 每个离开者各自收到移动者的移出包（双向语义一致）。
	if len(left) > 0 {
		leftIDs := make([]uint16, 0, len(left))
		for _, o := range left {
			leftIDs = append(leftIDs, o.ID)
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowObjectsOutOfScope(leftIDs)
		}
		for _, o := range left {
			if o.View != nil {
				_ = o.View.ShowObjectsOutOfScope([]uint16{updated.ID})
			}
		}
	}

	// NPC 视野差分（原版 UpdateObservingBucketsAsync：玩家跨桶时对视野内对象
	// 逐个触发进入/离开事件）——玩家移动驱动的 NPC 进入（C2 13）与移出（C1 14）。
	// 只发**差集**：重复发已在视野的 NPC 会让客户端重新出生（"从地下钻出来"）。
	if npcs := s.deps.cfg.NPCs; npcs != nil {
		before := s.npcIDsInRange(npcs.ByMap(wp.MapNumber), oldX, oldY)
		afterEntries := s.npcsInScope(npcs.ByMap(wp.MapNumber), updated.X, updated.Y)
		var entering []action.NpcScopeEntry
		for _, e := range afterEntries {
			if !before[e.ID] {
				entering = append(entering, e)
			}
		}
		if len(entering) > 0 {
			if view := s.viewFor(sess); view != nil {
				_ = view.ShowNpcsInScope(entering)
			}
		}
		var gone []uint16
		for _, e := range afterEntries {
			delete(before, e.ID)
		}
		for id := range before {
			gone = append(gone, id)
		}
		if len(gone) > 0 {
			if view := s.viewFor(sess); view != nil {
				_ = view.ShowObjectsOutOfScope(gone)
			}
		}
	}

	// 传送门（T1-7）：落点在进门矩形内 → 换图。
	if c := sess.getSelected(); c != nil {
		s.checkGateAfterWalk(sess, c, wp)
	}
}

// npcsInScope 返回桶订阅覆盖集内的 NPC 入视野条目（原版 ObservingBuckets 语义，
// 可见距离随桶边界浮动 12~27，非精确切比雪夫 12）。
func (s *Server) npcsInScope(npcs []*npc.Npc, x, y byte) []action.NpcScopeEntry {
	var entries []action.NpcScopeEntry
	for _, n := range npcs {
		if !world.InScope(x, y, n.X, n.Y) {
			continue
		}
		entries = append(entries, action.NpcScopeEntry{
			ID: n.ID, TypeNumber: uint16(n.Number), X: n.X, Y: n.Y,
			Rotation: n.Rotation,
		})
	}
	return entries
}

// npcIDsInRange 返回视野覆盖集内 NPC 的 ID 集合（差分用）。
func (s *Server) npcIDsInRange(npcs []*npc.Npc, x, y byte) map[uint16]bool {
	out := make(map[uint16]bool)
	for _, n := range npcs {
		if world.InScope(x, y, n.X, n.Y) {
			out[n.ID] = true
		}
	}
	return out
}
