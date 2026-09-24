package friendserver

import (
	"sync"
)

// onlineFriend 对应原版 FriendServer/OnlineFriend.cs：
// 一个在线好友，可观察其他好友、也被其他好友订阅。
// 原版用 IObservable/IObserver + Rx 语义；Go 侧等价表达为直接的订阅者集合 +
// changeServer 通知（OnNext/OnCompleted/Unsubscriber 的展开，行为逐一对应）。
type onlineFriend struct {
	notifier FriendNotifier
	name     string

	mu          sync.RWMutex
	serverId    int
	subscribers map[string]*onlineFriend // 订阅本实例状态变化的在线好友
	disposed    bool
}

func newOnlineFriend(n FriendNotifier, name string, serverId int) *onlineFriend {
	return &onlineFriend{
		notifier:    n,
		name:        name,
		serverId:    serverId,
		subscribers: make(map[string]*onlineFriend),
	}
}

// isSpecialOffline 判断是否离线/隐身哨兵值。
func isSpecialOffline(serverId int) bool {
	return serverId == offlineServerID || serverId == invisibleServerID
}

// isOnlineAndVisible 报告是否在线且可见（非 0xFF/0xFE）。
func (o *onlineFriend) isOnlineAndVisible() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return !isSpecialOffline(o.serverId)
}

// serverID 返回当前服务器号。
func (o *onlineFriend) serverID() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.serverId
}

// subscribe 对应原版 Subscribe：注册一个想知道本实例状态变化的观察者。
func (o *onlineFriend) subscribe(other *onlineFriend) {
	o.mu.Lock()
	o.subscribers[other.name] = other
	o.mu.Unlock()
}

// unsubscribe 对应原版 Unsubscribe。
func (o *onlineFriend) unsubscribe(other *onlineFriend) {
	o.mu.Lock()
	delete(o.subscribers, other.name)
	o.mu.Unlock()
}

// hasSubscriber 对应原版 HasSubscriber。
func (o *onlineFriend) hasSubscriber(other *onlineFriend) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.subscribers[other.name] == other
}

// removeSubscriber 对应原版 RemoveSubscriber：解除订阅，并通知对方"本实例已离线"
// （原版：FriendOnlineStateChangedAsync(friend.ServerId, friend.PlayerName, this.PlayerName, Offline)）。
func (o *onlineFriend) removeSubscriber(friend *onlineFriend) {
	friend.unsubscribe(o)
	_ = o.notifier.FriendOnlineStateChangedAsync(friend.serverID(), friend.name, o.name, offlineServerID)
}

// changeServer 对应原版 ChangeServer：改状态并通知全部订阅者
// （通知内容：订阅者的 serverId/playerName + 本实例的新 serverId）。
func (o *onlineFriend) changeServer(serverId int) {
	o.mu.Lock()
	o.serverId = serverId
	subscribers := make([]*onlineFriend, 0, len(o.subscribers))
	for _, s := range o.subscribers {
		subscribers = append(subscribers, s)
	}
	o.mu.Unlock()

	for _, s := range subscribers {
		// 对应原版订阅者的 OnNext(this)：
		// FriendOnlineStateChangedAsync(this.ServerId, this.PlayerName, value.PlayerName, value.ServerId)
		// 隐身（0xFE）对订阅者显示为离线（0xFF）。
		valueServerID := serverId
		if valueServerID == invisibleServerID {
			valueServerID = offlineServerID
		}
		_ = o.notifier.FriendOnlineStateChangedAsync(s.serverID(), s.name, o.name, valueServerID)
	}
}

// complete 对应原版 OnCompleted：清理全部订阅（下线时调用）。
func (o *onlineFriend) complete() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.disposed = true
	o.subscribers = make(map[string]*onlineFriend)
}
