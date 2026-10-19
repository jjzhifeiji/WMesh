package wanchannel

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/testtls"
)

// 换上测例 CA，测完退回系统根证书，别的用例不受影响。
func useRootCAs(t *testing.T, pki testtls.PKI) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 只信这份 CA。
	UseRootCAs(pki.Pool)
	// 测完退回系统根证书。
	t.Cleanup(func() { UseRootCAs(nil) })
}

// 起一个带测例证书的 HTTPS 替身。
func startTLSServer(t *testing.T, pki testtls.PKI, h http.Handler) *httptest.Server {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 先不启动，换上测例证书再开。
	srv := httptest.NewUnstartedServer(h)
	// 用测例 CA 签的证书，厂端按 CA 校验。
	srv.TLS = pki.ServerConfig()
	// 按 TLS 启动。
	srv.StartTLS()
	// 测完关掉，端口让出来。
	t.Cleanup(srv.Close)
	return srv
}

// 起一个只开 TLS 监听的放行代理，交回地址。
func startTLSAllowBroker(t *testing.T, pki testtls.PKI) string {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 占一个本机空端口。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// 端口占不上就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatal(err)
	}
	// 建一个放行的本地代理，测试不验口令。
	server := mochimqtt.New(&mochimqtt.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	// 挂上放行钩子，任何连接都能订主题。
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		// 放行钩子挂不上就停测。
		t.Fatal(err)
	}
	// 端口外面包一层 TLS，证书由测例 CA 签。
	if err := server.AddListener(listeners.NewNet("tls", tls.NewListener(ln, pki.ServerConfig()))); err != nil {
		// 监听挂不上就停测。
		t.Fatal(err)
	}
	// 后台跑代理，测试才能去拨号。
	go func() { _ = server.Serve() }()
	// 测完关掉代理，重复关也不让测试失败。
	t.Cleanup(func() {
		// 已经停了就吞掉恐慌。
		defer func() { _ = recover() }()
		// 关掉代理并释放端口。
		_ = server.Close()
	})
	// 交回实际地址，调用方按它拨号。
	return ln.Addr().String()
}

// 只带地址和厂钥跑 Hold，回调全空；Hold 改参数时只改这一处。
func holdBare(ctx context.Context, mqttURL, wanHTTP string, fid uuid.UUID, priv []byte) error {
	// 回调全空，只看能不能连上并拉索引。
	return Hold(ctx, mqttURL, wanHTTP, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

// 替身平台：回租约和空索引，拉到索引就记一笔。
func indexHandler(sawIndex *atomic.Bool) http.Handler {
	// 按路径回租约、索引或拒绝。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径分发。
		switch {
		// 租约路径回解包钥。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回空清单并记下被拉过。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 记下索引已被拉到。
			sawIndex.Store(true)
			// 回空指令清单。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 其余路径拒绝。
		default:
			// 未知路径回找不到。
			http.NotFound(w, r)
		}
	})
}

// https 平台默认拼 ssl 加密端口，http 照旧拼明文端口，显式地址不改写。
func TestResolveMQTTByScheme(t *testing.T) {
	// 逐种平台地址列出来。
	cases := []struct {
		mqtt string // 显式 MQTT 地址
		wan  string // 平台网页地址
		want string // 期望拼出的代理地址
	}{
		{"", "https://wan.example:52443", "ssl://wan.example:52883"},
		{"", "HTTPS://10.0.0.1:52443/", "ssl://10.0.0.1:52883"},
		{"", "http://wan.example:52080", "tcp://wan.example:52183"},
		{"ssl://other:8883", "https://wan.example:52443", "ssl://other:8883"},
	}
	// 每种都按预期拼。
	for _, tc := range cases {
		// 拼代理地址。
		got, err := resolveMQTT(tc.mqtt, tc.wan)
		// 拼错或报错都停测。
		if err != nil || got != tc.want {
			// 带上输入和结果。
			t.Fatalf("resolve %q %q = %q, %v; want %q", tc.mqtt, tc.wan, got, err, tc.want)
		}
	}
}

// 加密协议才给 TLS 配置，主机名按代理地址，CA 跟着 UseRootCAs 走。
func TestMQTTTLSConfig(t *testing.T) {
	// 明文协议不该套 TLS。
	if cfg := mqttTLS("tcp://127.0.0.1:52183"); cfg != nil {
		// 明文被套了 TLS。
		t.Fatal("tls config for tcp")
	}
	// 现生成一套证书并换上它的 CA。
	pki := testtls.New(t)
	// 换上测例 CA。
	useRootCAs(t, pki)
	// ssl 地址要给 TLS 配置。
	cfg := mqttTLS("ssl://127.0.0.1:52883")
	// 主机名和 CA 都要对上。
	if cfg == nil || cfg.ServerName != "127.0.0.1" || cfg.RootCAs != pki.Pool || cfg.MinVersion != tls.VersionTLS12 {
		// TLS 配置不对。
		t.Fatalf("mqtt tls %+v", cfg)
	}
}

// 认领走 HTTPS：配了 CA 能认领，没配 CA 校验不过收成不可达。
func TestEnrollOverTLS(t *testing.T) {
	// 现生成一套证书。
	pki := testtls.New(t)
	// 工厂和超管身份，认领回执要对得上。
	fid, pid := uuid.New(), uuid.New()
	// 替身只回认领身份。
	srv := startTLSServer(t, pki, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 回工厂身份和初始超管。
		writeJSON(w, map[string]any{"factoryId": fid.String(), "name": "厂A", "factoryShortCode": "F01", "saPersonId": pid.String(), "saLogin": "sa", "saDisplay": "超管"})
	}))
	// 没换 CA 时只信系统根证书，自建 CA 签的证书校验不过。
	if _, err := Enroll(context.Background(), srv.URL, "code"); !errors.Is(err, domain.ErrWANUnreachable) {
		// 没校验证书就认领上了。
		t.Fatalf("enroll without ca: %v", err)
	}
	// 换上测例 CA。
	useRootCAs(t, pki)
	// 配了 CA 就该认领成功。
	offer, err := Enroll(context.Background(), srv.URL, "code")
	// 认领失败或身份不对都停测。
	if err != nil || offer.FactoryID != fid {
		// 带上结果。
		t.Fatalf("enroll with ca: %+v, %v", offer, err)
	}
}

// Hold 走 ssl 和 https：CA 对就连上并拉到索引，CA 不对收成不可达。
func TestHoldOverTLS(t *testing.T) {
	// 现生成一套证书，代理和平台都用它。
	pki := testtls.New(t)
	// 工厂身份。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatal(err)
	}
	// 起只开 TLS 的代理。
	mqttAddr := startTLSAllowBroker(t, pki)
	// 记下索引有没有被拉过。
	var sawIndex atomic.Bool
	// 起 HTTPS 替身平台。
	srv := startTLSServer(t, pki, indexHandler(&sawIndex))
	// 换上测例 CA。
	useRootCAs(t, pki)
	// 给这一轮限时，避免挂死。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 接住通道退出原因。
	errCh := make(chan error, 1)
	// 后台跑通道。
	go func() { errCh <- holdBare(ctx, "ssl://"+mqttAddr, srv.URL, fid, priv) }()
	// 等索引被拉到，通道先退出就停测。
	waitUntil(t, ctx, errCh, "index over tls", sawIndex.Load)
	// 结果到了就取消。
	cancel()
	// 等通道退出。
	<-errCh

	// 换一套不相干的 CA，同样的代理就该校验不过。
	useRootCAs(t, testtls.New(t))
	// 再限一次时。
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	// 离开时取消。
	defer cancel2()
	// CA 不对就连不上，收成不可达。
	if err := holdBare(ctx2, "ssl://"+mqttAddr, srv.URL, fid, priv); !errors.Is(err, domain.ErrWANUnreachable) {
		// 证书没校验就连上了。
		t.Fatalf("hold with wrong ca: %v", err)
	}
}
