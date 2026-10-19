package wanchannel

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// 出站 TLS 的一套设置：HTTPS 共享传输和 MQTT 用的基础配置，用同一个 CA 池。
type wanTLS struct {
	transport *http.Transport // 认领、拉索引、拉包、上报共用，弱网下复用连接
	config    *tls.Config     // 基础 TLS 配置，MQTT 克隆后补上主机名
}

var activeTLS atomic.Pointer[wanTLS] // 当前出站 TLS 设置；启动时可换成带自建 CA 的

// 包加载时先用系统根证书，没配 CA 也能连公网证书的 WAN。
func init() {
	// 先装一套只信系统根证书的设置。
	activeTLS.Store(newWANTLS(nil))
}

// newWANTLS 按 CA 池拼出共享传输和基础配置；pool 为 nil 用系统根证书。
func newWANTLS(pool *x509.CertPool) *wanTLS {
	// 至少 TLS1.2，并缓存会话，重连时少握几次手。
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, ClientSessionCache: tls.NewLRUClientSessionCache(32)}
	// 从标准默认传输克隆，代理和超时设置照旧。
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// 换上自己的 TLS 配置，https 时按它校验证书。
	tr.TLSClientConfig = cfg
	return &wanTLS{transport: tr, config: cfg}
}

// UseRootCAs 换成只信这份 CA 池来校验 WAN 证书；启动时、拨号前调一次。
func UseRootCAs(pool *x509.CertPool) {
	// 装上新设置，之后的请求和拨号都按它校验。
	old := activeTLS.Swap(newWANTLS(pool))
	// 旧传输上空闲的连接不再复用，免得绕过新 CA。
	old.transport.CloseIdleConnections()
}

// 把请求交给当前共享传输；包级客户端建好后换 CA 也能生效。
type wanRoundTripper struct{}

// RoundTrip 用当前共享传输送出请求，https 时校验 WAN 证书。
func (wanRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// 每次取当前设置，换过 CA 立刻按新的走。
	return activeTLS.Load().transport.RoundTrip(req)
}

// mqttTLS 代理地址是 ssl 等加密协议时克隆一份 TLS 配置，按代理主机名校验证书；明文协议给 nil。
func mqttTLS(broker string) *tls.Config {
	// 解析代理地址，取协议和主机名。
	u, err := url.Parse(broker)
	// 解析不了就不套 TLS，拨号时自然会报错。
	if err != nil {
		return nil
	}
	// 只有 paho 认作加密的几种协议才套。
	switch strings.ToLower(u.Scheme) {
	// 这几种都要握手并校验 WAN 证书。
	case "ssl", "tls", "mqtts", "mqtt+ssl", "tcps", "wss":
	// 其余协议不套 TLS。
	default:
		return nil
	}
	// 克隆一份，免得改到共享配置。
	cfg := activeTLS.Load().config.Clone()
	// 按代理主机名校验证书上的 SAN。
	cfg.ServerName = u.Hostname()
	return cfg
}

// logSendFail 记下发往 WAN 的请求为何没送出，证书配错时运维能看到；调用方取消的不记。
func logSendFail(req *http.Request, err error) {
	// 调用方取消或停机退出不算链路故障，不刷日志。
	if req.Context().Err() != nil {
		return
	}
	// 只记路径和原始错误，签名头不进日志。
	slog.Warn("wan http send failed", "path", req.URL.Path, "err", err)
}

// isHTTPS 判断 WAN 地址是不是 https。
func isHTTPS(wanHTTP string) bool {
	// 解析平台地址，只看协议。
	u, err := url.Parse(strings.TrimSpace(wanHTTP))
	// 解析不了就当不是 https。
	return err == nil && strings.EqualFold(u.Scheme, "https")
}
