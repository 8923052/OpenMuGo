package loginserver

import (
	"errors"
	"sync"

	"mugo/internal/api"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/persistence"
)

// LoginService 编排认证 → 账号状态 → 会话占用，并实现契约层 api.LoginServer。
type LoginService struct {
	store    persistence.Store
	sessions *SessionRegistry

	// accounts 保存账号名 → 实体，供 GS 认证后取角色列表（契约层不暴露实体类型）。
	mu       sync.Mutex
	accounts map[string]*entity.Account
}

// NewLoginService 创建登录服务。
func NewLoginService(store persistence.Store, sessions *SessionRegistry) *LoginService {
	return &LoginService{
		store:    store,
		sessions: sessions,
		accounts: make(map[string]*entity.Account),
	}
}

// TryLoginAsync 是 api.LoginServer 的实现。
// 契约层只关心"账号是否被占用"，因此这里只做会话占用，不重复认证
// （认证 + 状态判定在 GS 登录流里由 Authenticate 完成）。
func (s *LoginService) TryLoginAsync(accountName string, serverId int) (bool, error) {
	return s.sessions.TryLoginAsync(accountName, serverId)
}

// LogOffAsync 是 api.LoginServer 的实现。
// serverId 当前未使用：单 GS 部署下注册表只按账号名记占用（多 GS 时须按 serverId 扩表）。
func (s *LoginService) LogOffAsync(accountName string, serverId int) error {
	s.forget(accountName)
	return s.sessions.LogOffAsync(accountName, serverId)
}

// GetSnapshotAsync 是 api.LoginServer 的实现。
func (s *LoginService) GetSnapshotAsync() (map[string]int, error) {
	return s.sessions.GetSnapshotAsync()
}

// Authenticate 校验用户名密码并判定账号状态；成功时占用会话。
//
// 返回 api.OnlineAccount（领域无关 DTO）而非 *entity.Account ——
// 契约层不得依赖领域包。需要完整实体的调用方走 Entity。
func (s *LoginService) Authenticate(username, password string) (api.LoginOutcome, api.OnlineAccount, bool) {
	account, err := s.store.Authenticate(username, password)
	if err != nil {
		if errors.Is(err, persistence.ErrInvalidCredentials) {
			return api.LoginInvalidPassword, api.OnlineAccount{}, false
		}
		return api.LoginConnectionError, api.OnlineAccount{}, false
	}
	switch account.State {
	case entity.AccountStateBanned:
		return api.LoginAccountBlocked, api.OnlineAccount{}, false
	case entity.AccountStateTemporarilyBanned:
		return api.LoginTemporaryBlocked, api.OnlineAccount{}, false
	}
	if err := s.sessions.TryLogin(account); err != nil {
		return api.LoginAccountAlreadyConnected, api.OnlineAccount{}, false
	}
	s.remember(account)
	return api.LoginOK, api.OnlineAccount{Name: account.Name, ServerId: 0}, true
}

// Entity 按账号名取认证成功时绑定的实体（GS 用它读角色列表）。第二个返回值表示是否在线。
func (s *LoginService) Entity(name string) (*entity.Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[name]
	return a, ok
}

// Logoff 在连接结束时释放会话（保留实体入口，模板账号不占名额）。
func (s *LoginService) Logoff(account *entity.Account) {
	s.forget(account.Name)
	s.sessions.Logoff(account)
}

func (s *LoginService) remember(a *entity.Account) {
	s.mu.Lock()
	s.accounts[a.Name] = a
	s.mu.Unlock()
}

func (s *LoginService) forget(name string) {
	s.mu.Lock()
	delete(s.accounts, name)
	s.mu.Unlock()
}
