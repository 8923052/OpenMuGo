package gameserver

// party_manager.go —— S2 组队的服务端状态机，对照原版 GameLogic.Party / PartyManager。
// 以角色名为键（同一 GS 内名字唯一），成员顺序即 PartyList 槽位、[0] 为队长。
// 组队经验共享（DistributeExperienceAfterKill）为后续项，本层不含。

import "sync"

// maxPartySize 是 GameConfiguration.MaximumPartySize 的兜底默认（=5）；
// 运行期优先用从配置传入的 partyManager.maxSize。
const maxPartySize = 5

type party struct {
	members []string // 有序，索引 = PartyList 槽位；[0] = 队长
}

func (p *party) indexOf(name string) int {
	for i, m := range p.members {
		if m == name {
			return i
		}
	}
	return -1
}

func (p *party) has(name string) bool { return p.indexOf(name) >= 0 }

// partyManager 持有 name→party 归属与待应答邀请（target→inviter）。
type partyManager struct {
	mu      sync.Mutex
	byName  map[string]*party
	invite  map[string]string
	maxSize int // GameConfiguration.MaximumPartySize
}

// newPartyManager 构造组队管理器；maxSize<=0 时回落默认 5。
func newPartyManager(maxSize int) *partyManager {
	if maxSize <= 0 {
		maxSize = maxPartySize
	}
	return &partyManager{byName: map[string]*party{}, invite: map[string]string{}, maxSize: maxSize}
}

// partyOf 返回角色所在组队（nil = 无）。
func (m *partyManager) partyOf(name string) *party {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byName[name]
}

// membersOf 返回角色所在组队的成员名副本（按槽位序）；无队返回 nil。
func (m *partyManager) membersOf(name string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.byName[name]
	if p == nil {
		return nil
	}
	return append([]string(nil), p.members...)
}

// canInvite 判定角色能否发起邀请：未组队，或已是队长（index 0）。
func (m *partyManager) canInvite(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.byName[name]
	return p == nil || (len(p.members) > 0 && p.members[0] == name)
}

// setInvite 记录一条待应答邀请；takeInvite 取出并清除；clearInvite 丢弃。
func (m *partyManager) setInvite(target, inviter string) {
	m.mu.Lock()
	m.invite[target] = inviter
	m.mu.Unlock()
}

func (m *partyManager) takeInvite(target string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invite[target]
	delete(m.invite, target)
	return inv, ok
}

func (m *partyManager) hasInvite(target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.invite[target]
	return ok
}

// acceptInvite 落实一次接受的邀请：邀请者已有队 → 把受邀者加入其队；否则新建队
// （队长=邀请者，先入邀请者再入受邀者）。返回入队后的成员名副本与成功标志。
func (m *partyManager) acceptInvite(inviter, target string) ([]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byName[target]; ok {
		return nil, false // 受邀者已在别处组队
	}
	p := m.byName[inviter]
	if p == nil {
		p = &party{members: []string{inviter}}
		m.byName[inviter] = p
	}
	if len(p.members) >= m.maxSize {
		return append([]string(nil), p.members...), false
	}
	p.members = append(p.members, target)
	m.byName[target] = p
	return append([]string(nil), p.members...), true
}

// removeResult 是一次移除的结果快照（供上层决定广播形态）。
type removeResult struct {
	member     string
	index      int
	disbanded  bool     // true：剩余 <2，整队解散
	oldMembers []string // 解散时：解散前的全部成员（含离开者），逐个发移除包
	remaining  []string // 未解散时：剩余成员
}

// remove 把角色移出组队（对照 Party.ExitPartyAsync）：剩余 <2 则整队解散。
func (m *partyManager) remove(name string) (removeResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.byName[name]
	if p == nil {
		return removeResult{}, false
	}
	idx := p.indexOf(name)
	res := removeResult{member: name, index: idx}
	if len(p.members)-1 < 2 {
		res.disbanded = true
		res.oldMembers = append([]string(nil), p.members...)
		for _, mem := range p.members {
			delete(m.byName, mem)
		}
		return res, true
	}
	next := p.members[:0:0]
	for _, mem := range p.members {
		if mem == name {
			continue
		}
		next = append(next, mem)
		delete(m.byName, mem)
	}
	for _, mem := range next {
		m.byName[mem] = p
	}
	p.members = next
	res.remaining = append([]string(nil), next...)
	return res, true
}
