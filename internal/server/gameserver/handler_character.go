package gameserver

import (
	"regexp"
	"sort"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/entity/item"
	"mugo/internal/gamelogic/player"
	"mugo/internal/gamelogic/storage"
	"mugo/internal/view/remote"

	c2s "mugo/internal/proto/c2s"
)

// maxCharactersPerAccount 是 GameConfiguration.MaximumCharactersPerAccount 的兜底默认（=5），
// 用于导出件缺该全局值时。运行期优先取 gc.Globals.MaximumCharactersPerAccount。
const maxCharactersPerAccount = 5

// namePattern 按配置的角色名正则编译（创角时校验，对照原版 CharacterNameRegex）。
func namePattern(expr string) *regexp.Regexp {
	if expr == "" {
		return nil
	}
	return regexp.MustCompile(expr)
}

// handleF3 处理角色组：00 请求角色列表、01 创角、02 删角、03 选角、
// 06 加点、10 背包清单、12 进图就绪、15 聚焦角色、30 存键位、52 大师加点（其余忽略）。
// 本文件只做：状态守卫 → 组装动作入参 → 调用视图出站（doc/10 T0-b 分层）。
func (s *Server) handleF3(sess *session, sub byte, frame []byte) {
	switch sub {
	case 0x00:
		s.handleCharacterList(sess, frame)
	case 0x01:
		s.handleCreateCharacter(sess, frame)
	case 0x02:
		s.handleDeleteCharacter(sess, frame)
	case 0x03:
		s.handleSelectCharacter(sess, frame)
	case 0x06:
		s.handleStatIncrease(sess, frame)
	case 0x10:
		s.handleInventoryRequest(sess, frame)
	case 0x12:
		s.handleClientReady(sess)
	case 0x15:
		s.handleFocusCharacter(sess, frame)
	case 0x30:
		s.handleSaveKeyConfiguration(sess, frame)
	case 0x52:
		s.handleAddMasterPoint(sess, frame)
	default:
		s.deps.logger.Printf("gameserver: 忽略 F3 sub=0x%02X", sub)
	}
}

// handleCharacterList 仅允许已认证会话：按槽位排序后交给视图下发
// （扩展/紧凑形态由视图层按客户端版本选，对照 OpenMU 的 IShowCharacterListPlugIn）。
func (s *Server) handleCharacterList(sess *session, frame []byte) {
	d := s.deps
	if sess.getState() != entity.StateAuthenticated {
		d.logger.Printf("gameserver: 未登录请求角色列表，忽略 %s", sess.conn.RemoteAddr())
		return
	}
	account := sess.getAccount()
	if account == nil {
		return
	}

	entries := make([]action.CharacterListEntry, 0, len(account.Characters))
	for _, c := range account.Characters {
		var appear, appearExt []byte
		if len(c.Appearance) == remote.AppearanceSize {
			appear = c.Appearance
		}
		if len(c.AppearanceExt) == remote.AppearanceExtSize {
			appearExt = c.AppearanceExt
		} else {
			var buf [remote.AppearanceExtSize]byte
			remote.EncodeAppearanceExt(&item.Appearance{ClassNumber: int(c.ClassNumber)}, buf[:])
			appearExt = buf[:]
		}
		entries = append(entries, action.CharacterListEntry{
			Slot: c.Slot, Name: c.Name, Level: c.Level,
			Status:        uint8(c.Status),
			GuildPosition: characterListGuildPosition(c.GuildPosition),
			Appearance:    appear,
			AppearanceExt: appearExt,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Slot < entries[j].Slot })

	// 解锁标记（对照 ShowCharacterListPlugIn:44-50）：同一个值既写进 F3 00 的
	// UnlockFlags，>0 时还要另发一条 C1 DE 00。
	unlockFlags := action.CharacterCreationUnlockFlags(account, s.deps.cfg.GameConfig)
	if err := s.viewFor(sess).ShowCharacterList(entries, unlockFlags); err != nil {
		d.logger.Printf("gameserver: 发送角色列表失败: %v", err)
		return
	}
	if unlockFlags > 0 {
		_ = s.viewFor(sess).ShowCharacterClassCreationUnlock(unlockFlags)
	}
}

// handleCreateCharacter 处理 C1 F3 01（对照 CreateCharacterAction）。
// 校验顺序：状态 → 职业可创建 → 同账号重名 → 空槽；通过后构造 1 级角色，
// 出生在职业 HomeMap 的 spawn gate 矩形内随机点，直接追加到账号（内存持久化）。
func (s *Server) handleCreateCharacter(sess *session, frame []byte) {
	d := s.deps
	fail := func(reason string) {
		d.logger.Printf("gameserver: 创角失败 %s: %s", sess.remoteAddr(), reason)
		_ = s.viewFor(sess).ShowCharacterCreationFailed()
	}
	if sess.getState() != entity.StateAuthenticated {
		// 对照原版 CreateCharacterAction：状态错误只记日志、直接返回，不下发任何帧。
		d.logger.Printf("gameserver: 创角失败：状态非已认证 %s", sess.remoteAddr())
		return
	}
	account := sess.getAccount()
	if account == nil {
		fail("无账号")
		return
	}
	gc := d.cfg.GameConfig
	if gc == nil {
		fail("配置缺失")
		return
	}
	req := c2s.AsCreateCharacter(frame)
	name := req.NameString()
	classNumber := int(req.Class())
	class, ok := gc.Class(classNumber)
	if !ok || !class.CanGetCreated || class.HomeMap == nil {
		fail("职业不可创建")
		return
	}
	// 角色名校验（对照原版 CreateCharacterAction 用 GameConfiguration.CharacterNameRegex）。
	if re := namePattern(gc.Globals.CharacterNameRegex); re != nil && !re.MatchString(name) {
		fail("角色名不合法")
		return
	}
	// 转职职业需账号已有达标的其他角色（对照原版 LevelRequirementByCreation；基础职业为 0 不限制）。
	if class.LevelRequirementByCreation > 0 {
		qualified := false
		for i := range account.Characters {
			if int(account.Characters[i].Level) >= class.LevelRequirementByCreation {
				qualified = true
				break
			}
		}
		if !qualified {
			fail("无满足等级要求的前置角色")
			return
		}
	}
	for i := range account.Characters {
		if account.Characters[i].Name == name {
			fail("角色名已存在")
			return
		}
	}
	slot := firstFreeCharacterSlot(account, charSlotLimit(gc))
	if slot < 0 {
		fail("角色槽已满")
		return
	}

	homeMapNumber := uint16(*class.HomeMap)
	homeMap, _ := gc.Map(int(homeMapNumber))
	var x, y byte
	if gate := spawnGateOf(homeMap); gate != nil {
		x = byte(s.world.RngInt(gate.X1, gate.X2+1))
		y = byte(s.world.RngInt(gate.Y1, gate.Y2+1))
	}

	var preview [remote.AppearanceSize]byte
	remote.EncodeAppearance(&item.Appearance{ClassNumber: classNumber}, preview[:])
	var ext [remote.AppearanceExtSize]byte
	remote.EncodeAppearanceExt(&item.Appearance{ClassNumber: classNumber}, ext[:])

	c := entity.Character{
		Slot: byte(slot), Name: name, Level: 1, ClassNumber: byte(classNumber),
		MapNumber: homeMapNumber, X: x, Y: y,
		Appearance: preview[:], AppearanceExt: ext[:],
		Stats:     player.NewCharStats(byte(classNumber), 1),
		Inventory: storage.NewInventory(0),
	}
	account.Characters = append(account.Characters, c)
	d.logger.Printf("gameserver: 创角成功 user=%s name=%s class=%d slot=%d map=%d (%d,%d)",
		account.Name, name, classNumber, slot, homeMapNumber, x, y)
	_ = s.viewFor(sess).ShowCharacterCreationSuccess(action.CreatedCharacterView{
		Name: name, Slot: byte(slot), Level: 1,
		Class: byte(classNumber), Status: 0,
	})
}

// handleDeleteCharacter 处理 C1 F3 02（对照 DeleteCharacterAction）。
// 内存账号无独立安全码，按原版 checkAsPassword 语义：输入须匹配账号密码。
// 前置：角色列表无公会职位须发 0xFF（见 characterListGuildPosition），
// 否则客户端点删除时被本地检查拦截、弹公会警告，根本不发包。
func (s *Server) handleDeleteCharacter(sess *session, frame []byte) {
	d := s.deps
	respond := func(result action.CharacterDeleteResponseResult) {
		_ = s.viewFor(sess).ShowCharacterDeleteResponse(result)
	}
	if sess.getState() != entity.StateAuthenticated {
		d.logger.Printf("gameserver: 删角失败：状态非已认证 %s", sess.remoteAddr())
		respond(action.CharacterDeleteUnsuccessful)
		return
	}
	account := sess.getAccount()
	if account == nil {
		respond(action.CharacterDeleteUnsuccessful)
		return
	}
	req := c2s.AsDeleteCharacter(frame)
	name := req.NameString()
	idx := -1
	for i := range account.Characters {
		if account.Characters[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		d.logger.Printf("gameserver: 删角失败：角色不存在 user=%s name=%q", account.Name, name)
		respond(action.CharacterDeleteUnsuccessful)
		return
	}
	if req.SecurityCodeString() != account.Password {
		respond(action.CharacterDeleteWrongCode)
		return
	}
	account.Characters = append(account.Characters[:idx], account.Characters[idx+1:]...)
	d.logger.Printf("gameserver: 删角成功 user=%s name=%s", account.Name, name)
	respond(action.CharacterDeleteSuccessful)
}

// charSlotLimit 返回每账号角色槽上限（优先取 05_game_config，缺省回落常量 5）。
func charSlotLimit(gc *config.GameConfig) int {
	if gc != nil && gc.Globals.MaximumCharactersPerAccount > 0 {
		return gc.Globals.MaximumCharactersPerAccount
	}
	return maxCharactersPerAccount
}

// firstFreeCharacterSlot 返回账号第一个空角色槽（0..limit-1），无空槽返回 -1。
func firstFreeCharacterSlot(account *entity.Account, limit int) int {
	used := make(map[byte]bool, len(account.Characters))
	for i := range account.Characters {
		used[account.Characters[i].Slot] = true
	}
	for slot := byte(0); int(slot) < limit; slot++ {
		if !used[slot] {
			return int(slot)
		}
	}
	return -1
}

// spawnGateOf 返回地图的出生门（IsSpawnGate），无则 nil。
func spawnGateOf(m *config.GameMap) *config.ExitGate {
	if m == nil {
		return nil
	}
	for i := range m.ExitGates {
		if m.ExitGates[i].IsSpawnGate {
			return &m.ExitGates[i]
		}
	}
	return nil
}

// characterListGuildPosition 把实体公会职位映射为角色列表线上字节：
// 零值（未加入公会）→ 0xFF（对照原版 EnumExtensions：GuildMemberRole.Undefined；
// 客户端 G_NONE=0xFF）。发 0 会被客户端解析成普通成员，点删除时本地
// GuildStatus!=G_NONE 检查直接拦截、弹"不能删除加入公会的角色"，不发包。
func characterListGuildPosition(position byte) byte {
	if position == 0 {
		return 0xFF
	}
	return position
}

// handleFocusCharacter 处理 C1 F3 15：对照 FocusCharacterAction——只在"角色选择"态
// （本仓 StateAuthenticated）受理，按名在本账号角色里找，找到才回 F3 15。
func (s *Server) handleFocusCharacter(sess *session, frame []byte) {
	if sess.getState() != entity.StateAuthenticated || len(frame) < c2s.FocusCharacterLength {
		return
	}
	acc := sess.getAccount()
	if acc == nil {
		return
	}
	name := string(c2s.AsFocusCharacter(frame).Name())
	for i := range acc.Characters {
		if acc.Characters[i].Name != name {
			continue
		}
		if view := s.viewFor(sess); view != nil {
			_ = view.ShowCharacterFocused(name)
		}
		return
	}
}

// handleSaveKeyConfiguration 处理 C3 F3 30：对照 SaveKeyConfigurationAction——
// 只把 blob 存到当前角色（无回包；20404 的进图属性包不回发键位，见 doc/17 S-3）。
func (s *Server) handleSaveKeyConfiguration(sess *session, frame []byte) {
	c := sess.getSelected()
	if c == nil || len(frame) < c2s.SaveKeyConfigurationRequiredSize(1) {
		return
	}
	blob := c2s.AsSaveKeyConfiguration(frame).Configuration()
	c.KeyConfiguration = append([]byte(nil), blob...)
}
