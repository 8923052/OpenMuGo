// Package guildserver 实现战盟服务（doc/14 P5.3，对应原版 GuildServer/GuildServer.cs + GuildContainer.cs）。
//
// 必须原样保留的语义（doc/14 §3 P5 明示）：
//   - **战盟短 id 是内存分配的**（原版 IdGenerator(1, int.MaxValue) + ReUseWhenExceeded），
//     "持久化"的只有"名字→id"映射——id 不是持久主键；
//   - 成员离线哨兵 = 0xFF（OfflineServerId）；
//   - 全员离线且无同盟时移除内存容器（同盟常驻内存）。
//
// 对游戏服的变更推送全部走 GuildChangePublisher 接口 + 回调注册（铁律）。
package guildserver

import (
	"log"
	"sort"
	"sync"

	"mugo/internal/api"
)

// offlineServerID 对应原版 GuildServer.OfflineServerId = 0xFF。
const offlineServerID = uint8(0xFF)

// GuildChangePublisher 对应原版 GuildServer/IGuildChangePublisher.cs：
// 战盟变更向游戏服的推送。实现方是 GameServer。
type GuildChangePublisher interface {
	GuildPlayerKickedAsync(playerName string) error
	GuildDeletedAsync(guildId uint32) error
	AssignGuildToPlayerAsync(serverId uint8, characterName string, status api.GuildMemberStatus) error
	AllianceCreatedAsync(masterGuildId uint32, memberGuildId uint32) error
	AllianceDisbandedAsync(masterGuildId uint32, memberGuildId uint32) error
	GuildHostilityChangedAsync(guildIdA uint32, allianceGuildIdsA []uint32, guildIdB uint32, allianceGuildIdsB []uint32, created bool) error
}

// memberEntry 是成员的内存登记（原版 GuildListEntry + 持久成员信息合并）。
type memberEntry struct {
	name     string
	position api.GuildPosition
	serverId uint8
}

// guildContainer 对应原版 GuildContainer：战盟对象 + 成员在线状态表。
type guildContainer struct {
	id    uint32
	name  string
	logo  []byte
	score int
	// allianceMaster 0 = 无同盟；否则为同盟盟主（原版 AllianceGuild 自引用）。
	allianceMaster uint32
	// members 按成员名索引（原版按 characterId Guid；内存态以名为键，见偏差登记）。
	members map[string]*memberEntry
}

// allOffline 报告是否全员离线。
func (g *guildContainer) allOffline() bool {
	for _, m := range g.members {
		if m.serverId != offlineServerID {
			return false
		}
	}
	return true
}

// Server 是战盟服务。
type Server struct {
	logger    *log.Logger
	publisher GuildChangePublisher

	mu      sync.Mutex
	guilds  map[uint32]*guildContainer // 短 id → 容器（内存缓存）
	nextID  uint32                     // IdGenerator(1, ...) 语义
	names   map[string]uint32          // "持久化"的名字→id 映射
	members map[string]membership      // "持久化"的成员关系（characterName → 归属）
	// hostilities 敌对关系（无向，以同盟盟主为键；原版存 Hostility 引用链）。
	hostilities map[hostilityKey]bool
}

// membership 是持久化的成员关系（原版 GuildMember 实体的内存等价）。
type membership struct {
	guildName string
	position  api.GuildPosition
}

// New 创建战盟服务（短 id 从 1 开始分配，对应原版 IdGenerator(1, int.MaxValue)）。
func New(publisher GuildChangePublisher, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		logger:      logger,
		publisher:   publisher,
		guilds:      make(map[uint32]*guildContainer),
		nextID:      1,
		names:       make(map[string]uint32),
		members:     make(map[string]membership),
		hostilities: make(map[hostilityKey]bool),
	}
}

// allocID 分配短 id（原版 ReUseWhenExceeded：溢出回绕复用；内存态不会到上限）。
func (s *Server) allocID() uint32 {
	id := s.nextID
	s.nextID++
	return id
}

// GuildExistsAsync 对应原版。
func (s *Server) GuildExistsAsync(guildName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.names[guildName]
	return ok
}

// CreateGuildAsync 对应原版：建档 + 盟主入会 + 经发布器把战盟分配给盟主。
// 原版还有 masterId Guid 参数（持久主键）；内存态以名为键，省略（偏差登记）。
func (s *Server) CreateGuildAsync(name, masterName string, logo []byte, serverId uint8) (bool, error) {
	s.mu.Lock()
	if _, ok := s.names[name]; ok {
		s.mu.Unlock()
		return false, nil
	}
	id := s.allocID()
	g := &guildContainer{
		id:      id,
		name:    name,
		logo:    append([]byte(nil), logo...),
		members: make(map[string]*memberEntry),
	}
	g.members[masterName] = &memberEntry{name: masterName, position: api.GuildPositionGuildMaster, serverId: serverId}
	s.guilds[id] = g
	s.names[name] = id
	s.members[masterName] = membership{guildName: name, position: api.GuildPositionGuildMaster}
	s.mu.Unlock()

	status := api.NewGuildMemberStatus(id, api.GuildPositionGuildMaster)
	if err := s.publisher.AssignGuildToPlayerAsync(serverId, masterName, status); err != nil {
		return true, err
	}
	return true, nil
}

// CreateGuildMemberAsync 对应原版：成员入会 + 分配通知。
func (s *Server) CreateGuildMemberAsync(guildId uint32, characterName string, role api.GuildPosition, serverId uint8) error {
	s.mu.Lock()
	g, ok := s.guilds[guildId]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if _, exists := g.members[characterName]; exists {
		s.mu.Unlock()
		s.logger.Printf("guildserver: 成员已存在 %s", characterName)
		return nil
	}
	g.members[characterName] = &memberEntry{name: characterName, position: role, serverId: serverId}
	s.members[characterName] = membership{guildName: g.name, position: role}
	s.mu.Unlock()

	status := api.NewGuildMemberStatus(guildId, role)
	return s.publisher.AssignGuildToPlayerAsync(serverId, characterName, status)
}

// ChangeGuildMemberPositionAsync 对应原版（原版按 characterId；内存态按名）。
func (s *Server) ChangeGuildMemberPositionAsync(guildId uint32, characterName string, role api.GuildPosition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok {
		return nil
	}
	if m, ok := g.members[characterName]; ok {
		m.position = role
		if ms, ok := s.members[characterName]; ok && ms.guildName == g.name {
			ms.position = role
			s.members[characterName] = ms
		}
	}
	return nil
}

// PlayerEnteredGameAsync 对应原版：查成员归属 → 容器登记在线 → 把战盟分配给玩家。
// characterId 原版为 Guid（Character.Id == GuildMember.Id）；内存态以名为键（偏差登记）。
func (s *Server) PlayerEnteredGameAsync(_ string, characterName string, serverId uint8) error {
	s.mu.Lock()
	ms, ok := s.members[characterName]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	guildID, guildOK := s.names[ms.guildName]
	if !guildOK {
		s.mu.Unlock()
		return nil
	}
	if g, ok := s.guilds[guildID]; ok {
		if m, ok := g.members[characterName]; ok {
			m.serverId = serverId
		}
	}
	s.mu.Unlock()

	status := api.NewGuildMemberStatus(guildID, ms.position)
	return s.publisher.AssignGuildToPlayerAsync(serverId, characterName, status)
}

// GuildMemberLeftGameAsync 对应原版：成员标记离线；全员离线且无同盟 → 移除内存容器。
func (s *Server) GuildMemberLeftGameAsync(guildId uint32, characterName string, _ uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok {
		return nil
	}
	if m, ok := g.members[characterName]; ok {
		m.serverId = offlineServerID
	}
	// 原版：AllianceGuild is null（无同盟）才移除；同盟常驻内存。
	if g.allianceMaster == 0 && g.allOffline() {
		delete(s.guilds, guildId)
	}
	return nil
}

// GetGuildListAsync 对应原版：成员列表（含在线状态）。
func (s *Server) GetGuildListAsync(guildId uint32) []api.GuildListEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok {
		return nil
	}
	out := make([]api.GuildListEntry, 0, len(g.members))
	for _, m := range g.members {
		out = append(out, api.GuildListEntry{
			PlayerName:     m.name,
			ServerId:       m.serverId,
			PlayerPosition: m.position,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlayerName < out[j].PlayerName })
	return out
}

// KickMemberAsync 对应原版：踢盟主 = 解散战盟；否则移除成员并通知游戏服。
func (s *Server) KickMemberAsync(guildId uint32, playerName string) error {
	s.mu.Lock()
	g, ok := s.guilds[guildId]
	if !ok {
		s.mu.Unlock()
		s.logger.Printf("guildserver: 战盟 %d 不存在，无法踢出 %s", guildId, playerName)
		return nil
	}
	m, ok := g.members[playerName]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if m.position == api.GuildPositionGuildMaster {
		delete(s.names, g.name)
		for name, ms := range s.members {
			if ms.guildName == g.name {
				delete(s.members, name)
			}
		}
		delete(s.guilds, guildId)
		s.mu.Unlock()
		return s.publisher.GuildDeletedAsync(guildId)
	}
	delete(g.members, playerName)
	delete(s.members, playerName)
	s.mu.Unlock()
	return s.publisher.GuildPlayerKickedAsync(playerName)
}

// GetGuildIdByNameAsync 对应原版。
func (s *Server) GetGuildIdByNameAsync(guildName string) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.names[guildName]
}

// GetGuildScoreAsync 返回战盟累计分（供 GuildList 展示；不存在时 0）。
func (s *Server) GetGuildScoreAsync(guildId uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.guilds[guildId]; ok {
		if g.score < 0 {
			return 0
		}
		return uint32(g.score)
	}
	return 0
}

// GetGuildInfoAsync 返回战盟展示信息（名字/同盟名/盟徽），不存在时 ok=false。
func (s *Server) GetGuildInfoAsync(guildId uint32) (name, allianceName string, logo []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok {
		return "", "", nil, false
	}
	allianceName = ""
	if g.allianceMaster != 0 && g.allianceMaster != guildId {
		if m := s.guilds[g.allianceMaster]; m != nil {
			allianceName = m.name
		}
	}
	return g.name, allianceName, append([]byte(nil), g.logo...), true
}

// AllianceGuildsAsync 对应原版 GuildServer.GetAllianceGuildsAsync（:454-477）：
// 返回该盟所在同盟的全部战盟条目。**没有同盟时返回空表**（原版 `AllianceGuild is null`
// → ImmutableList.Empty），调用方据此决定"同盟聊天不发给自己盟"。
func (s *Server) AllianceGuildsAsync(guildId uint32) []api.AllianceGuildEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok || g.allianceMaster == 0 {
		return nil
	}
	out := make([]api.AllianceGuildEntry, 0, len(s.guilds))
	for id, other := range s.guilds {
		if other.allianceMaster != g.allianceMaster {
			continue
		}
		out = append(out, api.AllianceGuildEntry{
			Id: id, GuildName: other.name, MemberCount: len(other.members), Logo: append([]byte(nil), other.logo...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out
}

// IncreaseGuildScoreAsync 对应原版。
func (s *Server) IncreaseGuildScoreAsync(guildId uint32, amount int) error {
	if amount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.guilds[guildId]; ok {
		g.score += amount
	}
	return nil
}

// CreateAllianceAsync 对应原版：盟主自引用建同盟 → 目标战盟加入 → 逐个通知游戏服。
func (s *Server) CreateAllianceAsync(masterGuildId, targetGuildId uint32) (api.AllianceCreationResult, error) {
	s.mu.Lock()
	master, okM := s.guilds[masterGuildId]
	_, okT := s.guilds[targetGuildId]
	if !okM {
		s.mu.Unlock()
		return api.AllianceCreationMasterGuildNotFound, nil
	}
	if !okT {
		s.mu.Unlock()
		return api.AllianceCreationTargetGuildNotFound, nil
	}
	target := s.guilds[targetGuildId]
	if target.allianceMaster != 0 {
		s.mu.Unlock()
		return api.AllianceCreationTargetGuildAlreadyInAlliance, nil
	}
	isNewAlliance := master.allianceMaster == 0
	if isNewAlliance {
		master.allianceMaster = masterGuildId
	}
	target.allianceMaster = masterGuildId
	s.mu.Unlock()

	if isNewAlliance {
		if err := s.publisher.AllianceCreatedAsync(masterGuildId, masterGuildId); err != nil {
			return api.AllianceCreationSuccess, err
		}
	}
	return api.AllianceCreationSuccess, s.publisher.AllianceCreatedAsync(masterGuildId, targetGuildId)
}

// RemoveAllianceAsync 对应原版：把目标战盟移出同盟（master==target 表示解散）。
func (s *Server) RemoveAllianceAsync(targetGuildId uint32) (bool, error) {
	s.mu.Lock()
	target, ok := s.guilds[targetGuildId]
	if !ok || target.allianceMaster == 0 {
		s.mu.Unlock()
		return false, nil
	}
	master := target.allianceMaster
	target.allianceMaster = 0
	s.mu.Unlock()

	if master == targetGuildId {
		// 解散整个同盟：所有以它为盟主的成员战盟都解除。
		s.mu.Lock()
		for _, g := range s.guilds {
			if g.allianceMaster == targetGuildId {
				g.allianceMaster = 0
			}
		}
		s.mu.Unlock()
		return true, s.publisher.AllianceDisbandedAsync(master, targetGuildId)
	}
	return true, s.publisher.AllianceDisbandedAsync(master, targetGuildId)
}

// allianceMembersLocked 返回同盟（含盟主）全部短 id（mu 内调用）。
func (s *Server) allianceMembersLocked(guildID uint32) []uint32 {
	g, ok := s.guilds[guildID]
	if !ok {
		return nil
	}
	master := guildID
	if g.allianceMaster != 0 {
		master = g.allianceMaster
	}
	out := []uint32{master}
	for id, other := range s.guilds {
		if id != master && other.allianceMaster == master {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SetHostilityAsync 对应原版：设置/解除两个战盟（或同盟）的敌对关系。
func (s *Server) SetHostilityAsync(guildIdA, guildIdB uint32, create bool) (bool, error) {
	s.mu.Lock()
	_, okA := s.guilds[guildIdA]
	_, okB := s.guilds[guildIdB]
	if !okA || !okB {
		s.mu.Unlock()
		return false, nil
	}
	allianceA := s.allianceMembersLocked(guildIdA)
	allianceB := s.allianceMembersLocked(guildIdB)
	if create {
		s.hostilities[hostilityKeyOf(guildIdA, guildIdB)] = true
	} else {
		delete(s.hostilities, hostilityKeyOf(guildIdA, guildIdB))
	}
	s.mu.Unlock()
	return true, s.publisher.GuildHostilityChangedAsync(guildIdA, allianceA, guildIdB, allianceB, create)
}

// AllianceMasterOfAsync 取该战盟所在同盟的盟主短 id（不在同盟时 ok=false）。
// 对照 Guild.AllianceMaster 引用链（原版 CommonChecksAsync 用它判"已在同盟但不是盟主"）。
func (s *Server) AllianceMasterOfAsync(guildId uint32) (masterId uint32, inAlliance bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guilds[guildId]
	if !ok || g.allianceMaster == 0 {
		return 0, false
	}
	return g.allianceMaster, true
}

// IsAllianceMasterAsync 报告该战盟是否盟主本人（原版 GuildServer.IsAllianceMasterAsync）。
func (s *Server) IsAllianceMasterAsync(guildId uint32) bool {
	master, inAlliance := s.AllianceMasterOfAsync(guildId)
	return inAlliance && master == guildId
}

// AreHostileAsync 报告两战盟（按各自同盟归并后）是否敌对。
func (s *Server) AreHostileAsync(guildA, guildB uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isHostileLocked(s.masterOfLocked(guildA), s.masterOfLocked(guildB))
}

// masterOfLocked 归并到同盟盟主（mu 内调用；不在同盟时返回自己）。
func (s *Server) masterOfLocked(guildId uint32) uint32 {
	if g, ok := s.guilds[guildId]; ok && g.allianceMaster != 0 {
		return g.allianceMaster
	}
	return guildId
}

// HasAnyHostilityAsync 报告该战盟或其所在同盟是否已卷进任何敌对关系
// （原版 RequestAsync 看的是 Guild.Hostility 引用是否为 null）。
func (s *Server) HasAnyHostilityAsync(guildId uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, members := range s.allianceMembersLocked(guildId) {
		for key := range s.hostilities {
			if key.a == members || key.b == members {
				return true
			}
		}
	}
	return false
}

// GetGuildRelationshipAsync 对应原版：两战盟关系（同盟/敌对/无）。
func (s *Server) GetGuildRelationshipAsync(guild1, guild2 uint32) (api.GuildRelationship, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g1, ok1 := s.guilds[guild1]
	g2, ok2 := s.guilds[guild2]
	if !ok1 || !ok2 {
		return api.GuildRelationshipNone, nil
	}
	master1, master2 := guild1, guild2
	if g1.allianceMaster != 0 {
		master1 = g1.allianceMaster
	}
	if g2.allianceMaster != 0 {
		master2 = g2.allianceMaster
	}
	if master1 == master2 {
		return api.GuildRelationshipUnion, nil
	}
	if s.isHostileLocked(master1, master2) {
		return api.GuildRelationshipRival, nil
	}
	return api.GuildRelationshipNone, nil
}

// hostilityKey 是无向敌对关系的键（小 id 在前）。
type hostilityKey struct{ a, b uint32 }

func hostilityKeyOf(a, b uint32) hostilityKey {
	if a > b {
		a, b = b, a
	}
	return hostilityKey{a: a, b: b}
}

// isHostileLocked 报告两个同盟盟主间是否敌对（mu 内调用）。
func (s *Server) isHostileLocked(masterA, masterB uint32) bool {
	return s.hostilities[hostilityKeyOf(masterA, masterB)]
}
