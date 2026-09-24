package gameserver

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"mugo/internal/version"
)

// errNoEndpoints 表示 Server 未配置任何监听端点。
var errNoEndpoints = errors.New("gameserver: 未配置任何端点")

// errListenerCount 表示注入的监听器数量与端点数量不一致。
var errListenerCount = errors.New("gameserver: 监听器数量与端点数量不一致")

// ClientVersion 返回端点绑定版本的版本三元组（对应 DefaultTcpGameServerListener.ClientVersion）。
func (e Endpoint) ClientVersion() version.ClientVersion { return e.Client.ClientVersion() }

// PublishedPort 返回上报用端口（AlternativePublishedPort > 0 时取代监听端口）。
// 装配层（cmd/mugo）用它喂 CS 的 ServerEntry——对应原版 listener 上报 stateObserver 的端口。
func (e Endpoint) PublishedPort() int {
	if e.AlternativePublishedPort > 0 {
		return e.AlternativePublishedPort
	}
	return portOf(e.ListenAddr)
}

// endpointAllows 报告登录包里的版本是否与**连接所属端点**绑定的版本一致。
// 端点是"预期版本"的边界（原版 DefaultTcpGameServerListener.ClientVersion）：
// 跨端点连接会使后续包形态与端点加密策略错配，必须早拒。
// 会话此刻已带端点归属（serveConn 传入），判定是单端点的。
func (s *Server) endpointAllows(sess *session, v version.ClientVersion) bool {
	return sess.getEndpoint().Client.ClientVersion() == v
}

// portOf 从 "host:port" 取端口；解析失败返回 0。
func portOf(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
}

// hostOf 从 "host:port" 取主机部分；缺 host 返回空串（由调用方回退）。
func hostOf(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return host
}

// itoa 极简整数转字符串（避免为日志与占位 id 引 fmt）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// defsOf 取端点绑定的全部客户端定义（注册表按原版顺序：第一条为默认版本）。
func defsOf(eps []Endpoint) []version.GameClientDefinition {
	defs := make([]version.GameClientDefinition, 0, len(eps))
	seen := make(map[uint64]bool, len(eps))
	for _, ep := range eps {
		if seen[ep.Client.Key()] {
			continue // 多端点可绑同一版本（同版本多端口），注册表只收一次
		}
		seen[ep.Client.Key()] = true
		defs = append(defs, ep.Client)
	}
	return defs
}
