// Package friendserver 实现好友服务（doc/14 P5.2，对应原版 FriendServer/FriendServer.cs）。
//
// 职责：好友列表（请求/接受）、信件转发、在线状态（含 0xFF/0xFE 哨兵服务器号）、
// 聊天室创建的协调（通过 IChatServer + FriendNotifier 通知游戏服）。
// 对游戏服的推送全部走 FriendNotifier 接口 + 回调注册（铁律：不引入消息队列）。
//
// 存储：内存实现（对应先手修复 #3 的"按实体分组、仍只做内存实现"）；
// 接 DB 时替换 store 部分，服务逻辑不变。
package friendserver

import (
	"log"
	"sort"
	"sync"

	"mugo/internal/api"
)

const (
	// offlineServerID 对应原版 FriendServer.OfflineServerId = SpecialServerId.Offline (0xFF)。
	offlineServerID = int(api.SpecialServerIdOffline)
	// invisibleServerID 对应原版 InvisibleServerId = SpecialServerId.Invisible (0xFE)。
	invisibleServerID = int(api.SpecialServerIdInvisible)
)

// FriendNotifier 对应原版 FriendServer/IFriendNotifier.cs：
// 向"某玩家所在的游戏服"推送好友系统事件。实现方是 GameServer。
type FriendNotifier interface {
	// FriendRequestAsync 通知接收方所在游戏服：有人向 receiver 发了好友请求。
	FriendRequestAsync(requester string, receiver string, serverId int) error
	// LetterReceivedAsync 通知游戏服：某在线玩家收到信件。
	LetterReceivedAsync(letter api.LetterHeader) error
	// FriendOnlineStateChangedAsync 通知游戏服：player 的好友 friend 状态变化。
	FriendOnlineStateChangedAsync(playerServerId int, player string, friend string, friendServerId int) error
	// ChatRoomCreatedAsync 通知游戏服：为 player 建好了聊天室（携带认证信息）。
	ChatRoomCreatedAsync(serverId int, playerAuthenticationInfo api.ChatServerAuthenticationInfo, friendName string) error
	// InitializeMessengerAsync 为刚进图的玩家初始化信使（好友列表 + 未处理请求）。
	InitializeMessengerAsync(serverId int, initializationData api.MessengerInitializationData) error
}

// ChatServerView 是好友服需要的聊天服最小视图（原版直接依赖 IChatServer；
// 原版代码注释自己都说 TODO 应该解耦——Go 侧一开始就用窄接口）。
type ChatServerView interface {
	CreateChatRoomAsync() (uint16, error)
	// RegisterClientAsync 把玩家注册进房间并返回认证信息；房间不存在时 ok=false。
	RegisterClientAsync(roomId uint16, clientName string) (info api.ChatServerAuthenticationInfo, ok bool, err error)
}

// friendRecord 是一条好友关系（双向各存一条）。
type friendRecord struct {
	accepted    bool
	requestOpen bool
}

// Server 是好友服务。
type Server struct {
	logger   *log.Logger
	notifier FriendNotifier
	chat     ChatServerView

	// ---- 内存仓储（接 DB 时替换为持久层上下文）----
	storeMu sync.Mutex
	// friends[player][friend] = 关系记录（双向各一条，键为角色名）。
	friends map[string]map[string]friendRecord
	// letters[receiver] = 该收件人信箱（按到达顺序，下标即 Index）。P2 信件。
	letters map[string][]storedLetter

	// ---- 在线好友（原版 OnlineFriends 字典）----
	onlineMu sync.RWMutex
	online   map[string]*onlineFriend
}

// New 创建好友服务。
func New(notifier FriendNotifier, chat ChatServerView, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		logger:   logger,
		notifier: notifier,
		chat:     chat,
		friends:  make(map[string]map[string]friendRecord),
		letters:  make(map[string][]storedLetter),
		online:   make(map[string]*onlineFriend),
	}
}

// getFriend 查一条关系记录（storeMu 内调用）。
func (s *Server) getFriend(player, friend string) (friendRecord, bool) {
	m, ok := s.friends[player]
	if !ok {
		return friendRecord{}, false
	}
	r, ok := m[friend]
	return r, ok
}

// setFriend 写一条关系记录（storeMu 内调用）。
func (s *Server) setFriend(player, friend string, r friendRecord) {
	m, ok := s.friends[player]
	if !ok {
		m = make(map[string]friendRecord)
		s.friends[player] = m
	}
	m[friend] = r
}

// deleteFriend 删除一条关系记录（storeMu 内调用）。
func (s *Server) deleteFriend(player, friend string) {
	if m, ok := s.friends[player]; ok {
		delete(m, friend)
	}
}

// ForwardLetterAsync 对应原版：直接转发给玩家所在游戏服。
func (s *Server) ForwardLetterAsync(letter api.LetterHeader) error {
	return s.notifier.LetterReceivedAsync(letter)
}

// FriendRequestAsync 对应原版：新建请求（已存在则不算新）；好友在线则实时推送请求。
// 返回值 = 是否新建成功（原版 friendIsNew && saveSuccess）。
func (s *Server) FriendRequestAsync(playerName, friendName string) (bool, error) {
	s.storeMu.Lock()
	_, exists := s.getFriend(playerName, friendName)
	friendIsNew := !exists
	if friendIsNew {
		s.setFriend(playerName, friendName, friendRecord{accepted: false, requestOpen: true})
	}
	s.storeMu.Unlock()

	if friendIsNew {
		if of := s.onlineFriend(friendName); of != nil {
			// 好友在线：直接推送请求（带其所在服务器号）。
			if err := s.notifier.FriendRequestAsync(playerName, friendName, of.serverID()); err != nil {
				return true, err
			}
		}
	}
	return friendIsNew, nil
}

// IsFriendAsync 对应原版：双方在线看订阅关系，否则查存储的 accepted。
func (s *Server) IsFriendAsync(characterName, friendName string) bool {
	a, b := s.onlineFriend(characterName), s.onlineFriend(friendName)
	if a != nil && b != nil {
		return a.hasSubscriber(b)
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	r, ok := s.getFriend(characterName, friendName)
	return ok && r.accepted
}

// DeleteFriendAsync 对应原版：解除双向订阅 + 删除双向存储。
func (s *Server) DeleteFriendAsync(playerName, friendName string) error {
	if a, b := s.onlineFriend(playerName), s.onlineFriend(friendName); a != nil && b != nil {
		a.removeSubscriber(b)
		b.removeSubscriber(a)
	}
	s.storeMu.Lock()
	s.deleteFriend(playerName, friendName)
	s.deleteFriend(friendName, playerName)
	s.storeMu.Unlock()
	return nil
}

// FriendResponseAsync 对应原版：处理请求（characterName=响应方，friendName=请求方）。
// 接受：请求方记录置 accepted，响应方补建反向记录，双方在线则互相订阅并互相同步状态。
func (s *Server) FriendResponseAsync(characterName, friendName string, accepted bool) error {
	s.storeMu.Lock()
	// 原版参数顺序说明：GetFriendByNamesAsync(friendName, characterName)——请求方视角的记录。
	req, ok := s.getFriend(friendName, characterName)
	if !ok {
		s.storeMu.Unlock()
		return nil
	}
	req.requestOpen = false
	req.accepted = accepted
	s.setFriend(friendName, characterName, req)
	if accepted {
		// 响应方（反向）记录：原版缺失时补建，直接 accepted。
		rev, ok := s.getFriend(characterName, friendName)
		if !ok {
			rev = friendRecord{}
		}
		rev.requestOpen = false
		rev.accepted = true
		s.setFriend(characterName, friendName, rev)
	}
	s.storeMu.Unlock()

	if accepted {
		s.addSubscriptions(friendName, characterName)
	}
	return nil
}

// addSubscriptions 对应原版 AddSubscriptions(requester, responder)：
// 双方都在线时互相订阅，并互相同步一次在线状态（原版 OnNext 两次）。
func (s *Server) addSubscriptions(requester, responder string) {
	responderFriend := s.onlineFriend(responder)
	if responderFriend == nil {
		return
	}
	requesterFriend := s.onlineFriend(requester)
	if requesterFriend == nil {
		return
	}
	responderFriend.subscribe(requesterFriend)
	requesterFriend.subscribe(responderFriend)

	// 互相通知在线服务器号：
	responderFriend.changeServer(responderFriend.serverID())
	requesterFriend.changeServer(requesterFriend.serverID())
}

// CreateChatRoomAsync 对应原版：双方在线且是好友时，
// 建聊天室并把两个玩家分别注册进去，经游戏服推送 ChatRoomCreated。
func (s *Server) CreateChatRoomAsync(playerName, friendName string) error {
	player := s.onlineFriend(playerName)
	if player == nil {
		return nil
	}
	friend := s.onlineFriend(friendName)
	if friend == nil {
		return nil
	}
	if !friend.hasSubscriber(player) {
		return nil
	}

	roomID, err := s.chat.CreateChatRoomAsync()
	if err != nil {
		return err
	}
	if info, ok, err := s.chat.RegisterClientAsync(roomID, playerName); err == nil && ok {
		if err := s.notifier.ChatRoomCreatedAsync(player.serverID(), info, friendName); err != nil {
			return err
		}
	}
	if info, ok, err := s.chat.RegisterClientAsync(roomID, friendName); err == nil && ok {
		if err := s.notifier.ChatRoomCreatedAsync(friend.serverID(), info, playerName); err != nil {
			return err
		}
	}
	return nil
}

// InviteFriendToChatRoomAsync 对应原版：好友在线、是好友、可见时注册进房间并通知。
func (s *Server) InviteFriendToChatRoomAsync(playerName, friendName string, roomID uint16) (bool, error) {
	player := s.onlineFriend(playerName)
	if player == nil {
		return false, nil
	}
	friend := s.onlineFriend(friendName)
	if friend == nil {
		return false, nil
	}
	if !friend.hasSubscriber(player) {
		return false, nil
	}
	if friend.isOnlineAndVisible() == false {
		return false, nil
	}
	info, ok, err := s.chat.RegisterClientAsync(roomID, friendName)
	if err != nil || !ok {
		return false, err
	}
	if err := s.notifier.ChatRoomCreatedAsync(friend.serverID(), info, playerName); err != nil {
		return true, err
	}
	return true, nil
}

// PlayerEnteredGameAsync 对应原版：初始化信使（好友名单 + 未处理请求）+ 上线。
// characterId 原版为 Guid；内存态以角色名为键（见 doc/14 偏差登记），参数保留对齐契约。
func (s *Server) PlayerEnteredGameAsync(serverId uint8, _ string, characterName string) error {
	friends, requesters := s.friendLists(characterName)
	initData := api.MessengerInitializationData{
		PlayerName:         characterName,
		Friends:            friends,
		OpenFriendRequests: requesters,
	}
	if err := s.notifier.InitializeMessengerAsync(int(serverId), initData); err != nil {
		s.logger.Printf("friendserver: 信使初始化通知失败 %s: %v", characterName, err)
	}
	return s.setOnlineState(characterName, int(serverId))
}

// PlayerLeftGameAsync 对应原版：下线（0xFF）。characterId 内存态以名代（偏差登记）。
func (s *Server) PlayerLeftGameAsync(_ string, characterName string) error {
	return s.setOnlineState(characterName, offlineServerID)
}

// SetPlayerVisibilityStateAsync 对应原版：隐身切换（0xFE）。
func (s *Server) SetPlayerVisibilityStateAsync(serverId uint8, characterName string, isVisible bool) error {
	target := int(serverId)
	if !isVisible {
		target = invisibleServerID
	}
	return s.setOnlineState(characterName, target)
}

// setOnlineState 对应原版 SetOnlineStateAsync：
// 不在线则新建在线对象（离线/隐身哨兵值不新建）→ changeServer → 下线时移除。
func (s *Server) setOnlineState(characterName string, serverId int) error {
	of := s.onlineFriend(characterName)
	if of == nil {
		if isSpecialOffline(serverId) {
			return nil // 原版：本来就不在线的人"下线"是 no-op
		}
		of = newOnlineFriend(s.notifier, characterName, serverId)
		s.onlineMu.Lock()
		s.online[characterName] = of
		s.onlineMu.Unlock()
		// 建立与既有在线好友的订阅关系（原版 AddSubscriptions(friends)）。
		s.addSubscriptionsForFriendsOf(characterName)
	}

	// 原版：ChangeServer(serverId == Invisible ? Offline : serverId)
	target := serverId
	if target == invisibleServerID {
		target = offlineServerID
	}
	of.changeServer(target)

	if serverId == offlineServerID {
		s.onlineMu.Lock()
		delete(s.online, characterName)
		s.onlineMu.Unlock()
		of.complete()
	}
	return nil
}

// addSubscriptionsForFriendsOf 对应原版 AddSubscriptions(IEnumerable<FriendViewItem>)：
// 对角色的每个 accepted 且 request 已关闭的好友，若双方都在线则互相订阅并互相同步。
func (s *Server) addSubscriptionsForFriendsOf(characterName string) {
	for _, friendName := range s.acceptedFriendNames(characterName) {
		if of := s.onlineFriend(friendName); of != nil {
			if me := s.onlineFriend(characterName); me != nil {
				of.subscribe(me)
				me.subscribe(of)
				// 原版：characterFriend.OnNext(onlineFriend) + onlineFriend.OnNext(characterFriend)
				of.changeServer(of.serverID())
				me.changeServer(me.serverID())
			}
		}
	}
}

// friendLists 返回（accepted 好友名列表, 未处理请求方列表）——信使初始化数据。
func (s *Server) friendLists(characterName string) (friends, requesters []string) {
	friends = s.acceptedFriendNames(characterName)
	requesters = s.openRequesters(characterName)
	return friends, requesters
}

// openRequesters 返回向 characterName 发过且未处理的请求方列表。
func (s *Server) openRequesters(characterName string) []string {
	var out []string
	for requester, m := range s.friends {
		if r, ok := m[characterName]; ok && r.requestOpen {
			out = append(out, requester)
		}
	}
	return out
}

// acceptedFriendNames 返回 characterName 的全部 accepted 好友名。
func (s *Server) acceptedFriendNames(characterName string) []string {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	var out []string
	for friend, r := range s.friends[characterName] {
		if r.accepted && !r.requestOpen {
			out = append(out, friend)
		}
	}
	sort.Strings(out)
	return out
}

// onlineFriend 取在线对象（可空）。
func (s *Server) onlineFriend(name string) *onlineFriend {
	s.onlineMu.RLock()
	defer s.onlineMu.RUnlock()
	return s.online[name]
}
