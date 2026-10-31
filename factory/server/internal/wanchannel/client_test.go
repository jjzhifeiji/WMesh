package wanchannel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// 建厂码能换到身份，交公钥后应记为已确认。
func TestEnrollConfirm(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 造一个超管身份，认领回执要对得上。
	pid := uuid.New()
	// 生成本厂公钥，确认认领时要交出去。
	pub, _, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 记下公钥有没有真正送出。
	var claimed bool
	// 替身分认领和确认，确认时记下公钥已送到。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径分发认领和确认，其余拒绝。
		switch r.URL.Path {
		// 认领询问回工厂身份，建厂码先不作废。
		case "/v1/channel/enroll":
			// 回工厂身份和初始超管，认领方才能落库。
			writeJSON(w, map[string]any{
				"factoryId": fid.String(), "name": "厂A", "factoryShortCode": "F01",
				"saPersonId": pid.String(), "saLogin": "sa-a", "saDisplay": "超管A",
			})
		// 收到公钥就记下，并回已经启用。
		case "/v1/channel/claim":
			// 记下公钥已送到，后面要断言这一步发生过。
			claimed = true
			// 回已启用，表示公钥已经收下。
			writeJSON(w, map[string]any{"factoryId": fid.String(), "status": "active", "revision": 0, "factoryShortCode": "F01"})
		// 其余路径拒绝，避免替身把确认误判成功。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 认领不另设超时，失败由替身立刻返回。
	ctx := context.Background()
	// 用建厂码换身份，失败就没有厂可认。
	offer, err := Enroll(ctx, srv.URL, "code")
	// 认领失败就停测，后面比对没有意义。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 身份或登录名对不上就停测。
	if offer.FactoryID != fid || offer.SAPersonID != pid || offer.SALogin != "sa-a" {
		// 认领结果和替身不一致就停测。
		t.Fatalf("offer %+v", offer)
	}
	// 交公钥失败就停测，建厂码不会作废。
	if err := Confirm(ctx, srv.URL, "code", pub); err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 公钥没送出就停测，建厂码不会被作废。
	if !claimed {
		// 公钥没送到就停测。
		t.Fatal("claim not posted")
	}
}

// 平台拒绝停用厂时，认领应收回停用错误。
func TestEnrollDisabled(t *testing.T) {
	// 替身一律拒绝，用来表示这家厂已经停用。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 用拒绝表示厂已停用，认领不该继续。
		w.WriteHeader(http.StatusForbidden)
		// 带上停用原因，调用方要收成停用错误。
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "factory is disabled"})
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 向停用厂认领，应当收回停用错误。
	_, err := Enroll(context.Background(), srv.URL, "code")
	// 不是停用错误就停测，别的失败不能当成拒绝。
	if !errors.Is(err, domain.ErrFactoryDisabled) {
		// 停用厂没有收成停用错误就停测。
		t.Fatalf("enroll: %v", err)
	}
}

// WAN 按来源限频回 429 时，认领两步都收成限频错误，不当内部故障。
func TestEnrollTooManyRequests(t *testing.T) {
	// 带英文原因和不带正文两种回包都要认。
	for _, body := range []string{`{"error":"too many requests"}`, ""} {
		// 替身一律回请求过多，模拟同一出口 IP 名额用完。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 先写请求过多的状态码。
			w.WriteHeader(http.StatusTooManyRequests)
			// 按用例带上或不带英文原因。
			_, _ = w.Write([]byte(body))
		}))
		// 用建厂码换身份，应收回限频错误。
		_, err := Enroll(context.Background(), srv.URL, "code")
		// 交公钥这一步同样应收回限频错误。
		confirmErr := Confirm(context.Background(), srv.URL, "code", make([]byte, 32))
		// 用完就关掉替身，避免端口一直占住。
		srv.Close()
		// 任一步没收成限频错误就停测。
		if !errors.Is(err, domain.ErrTooManyRequests) || !errors.Is(confirmErr, domain.ErrTooManyRequests) {
			// 带上正文和两步的实际错误便于定位。
			t.Fatalf("body %q: enroll %v confirm %v", body, err, confirmErr)
		}
	}
}

// 非网页地址不能拿来认领，应当直接拒绝。
func TestEnrollRejectsWSURL(t *testing.T) {
	// 用错误协议去认领，应当被拒绝。
	_, err := Enroll(context.Background(), "ws://127.0.0.1/v1/channel", "code")
	// 不是不可达就停测，错误地址必须被拒绝。
	if !errors.Is(err, domain.ErrWANUnreachable) {
		// 错误地址没有被拒绝就停测。
		t.Fatalf("ws url: %v", err)
	}
}

// 连上后应去拉索引，时限内要收得到。
func TestHold(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 记下索引有没有被拉过。
	var sawIndex bool
	// 替身回租约和空索引，让通道能够订上。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 索引被拉到就放开等待。
			sawIndex = true
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，超时和失败才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 给索引一个时限，过了还没有就判失败。
	deadline := time.Now().Add(2 * time.Second)
	// 时限内反复看索引，避免只查一次错过。
	for time.Now().Before(deadline) {
		// 索引到了就停止等待。
		if sawIndex {
			break
		}
		// 稍等再看，给通道留出发起请求的时间。
		time.Sleep(20 * time.Millisecond)
	}
	// 时限内没拉到索引就停测，通道没起来。
	if !sawIndex {
		// 时限内没拉到索引就停测，通道没起来。
		t.Fatal("no index pull")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 代理关掉后通道应退回，不能一直挂着。
func TestHoldDropsWhenBrokerStops(t *testing.T) {
	// 记住原来的探活间隔，测完要改回去。
	oldAlive := aliveEvery
	// 把探活改短，代理关掉后能很快发现断线。
	aliveEvery = 200 * time.Millisecond
	// 测完把探活间隔改回去，避免影响别的测试。
	t.Cleanup(func() { aliveEvery = oldAlive })

	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 留下代理句柄，后面才能把代理关掉。
	srv, mqttAddr := startAllowBroker(t)
	// 接住索引到达的信号，重复的丢掉。
	sawIndex := make(chan struct{}, 1)
	// 替身回租约和空索引，并通知索引已到。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 只送出一次，队列满了就丢掉，避免堵住替身。
			select {
			// 索引到了就通知一次，重复的信号丢掉。
			case sawIndex <- struct{}{}:
			// 已经送出过就不再塞，避免堵住替身。
			default:
			}
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，超时和失败才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 索引到了就继续，不再干等通道。
	case <-sawIndex:
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没拉到索引就停测。
		t.Fatal("no index")
	}
	// 关掉代理，通道应马上发现不可达。
	if err := srv.Close(); err != nil {
		// 代理关不掉就停测，后面的断言不准。
		t.Fatal(err)
	}
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 代理关掉后不是不可达就停测。
		if !errors.Is(err, domain.ErrWANUnreachable) {
			// 代理关掉后的错误不对就停测。
			t.Fatalf("hold after broker stop: %v", err)
		}
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 代理关掉后通道还挂着就停测。
		t.Fatal("hold hung after broker stop")
	}
}

// 连上后应报本进程版本，名录才看得到在线。
func TestHoldReportsPresence(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 接住一条在线上报，主流程不用堵住。
	got := make(chan Cmd, 1)
	// 替身回租约和空索引，方便观察在线上报。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 另开一个观察端，用来收在线上报。
	cli := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + mqttAddr).SetClientID("watch"))
	// 观察端连不上就停测，后面收不到上报。
	if tok := cli.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 观察端连不上就停测。
		t.Fatalf("watch connect: %v", tok.Error())
	}
	// 测完断开观察端，避免占着代理。
	t.Cleanup(func() { cli.Disconnect(250) })
	// 订上上行，收到报文就送去核对版本。
	if tok := cli.Subscribe("wan/"+fid.String()+"/up", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 预备装这一条上行，解不开就丢掉。
		var cmd Cmd
		// 解不开就丢掉，不断开观察端。
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		// 只送出一次，队列满了就丢掉，避免堵住替身。
		select {
		// 把在线上报送出去，队列满了就丢掉。
		case got <- cmd:
		// 已经送出过就不再塞，避免堵住替身。
		default:
		}
	}); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatal(tok.Error())
	}

	// 通道连上、断开的通知，按先后收。
	status := make(chan bool, 4)
	// 保活要带上的一台最近登录。
	login := ClientLoginUp{ClientID: uuid.NewString(), At: time.Now().UTC().Format(time.RFC3339Nano), AppVersion: 7, AppVersionName: "1.0.7"}
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，超时和失败才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			WithStatus(func(connected bool) { // 连上、断开按先后记下。
				// 记下这次状态，测试末尾核对先后。
				status <- connected
			}), WithClientLogins(func() []ClientLoginUp { // 保活时带一台最近登录。
				// 交一台固定的最近登录，核对它跟着在场报上去。
				return []ClientLoginUp{login}
			}))
	}()
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到上行就核对是不是在线上报。
	case cmd := <-got:
		// 不是在线上报或版本空着就停测。
		if cmd.Typ != CmdPresence || cmd.WebVersion < 1 || cmd.ServiceVersion < 1 || cmd.WebVersionName == "" || cmd.ServiceVersionName == "" {
			// 上报的版本不齐就停测。
			t.Fatalf("presence %+v", cmd)
		}
		// 没带上最近登录就停测，WAN 设备名录看不到。
		if len(cmd.ClientLogins) != 1 || cmd.ClientLogins[0] != login {
			// 带的登录不对就停测。
			t.Fatalf("presence logins %+v", cmd.ClientLogins)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到在线上报就停测。
		t.Fatal("no presence")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
	// 连上时先报在线，离开时再报断开，先后不能乱。
	if first, last := <-status, <-status; !first || last {
		// 状态通知不对就停测，横幅会显示错。
		t.Fatalf("status %v then %v", first, last)
	}
}

// 租约落地失败不应拆连接，索引仍要拉到。
func TestHoldLeaseFailKeepsConnection(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 接住索引到达的信号，重复的丢掉。
	gotIndex := make(chan struct{}, 1)
	// 替身回一条坏租约，索引仍要能拉到。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 租约路径回解包钥，通道才能继续拉索引。
		if strings.HasSuffix(r.URL.Path, "/lease") {
			// 回一条坏租约，用来验证落地失败不断开。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("bad-lease"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		// 索引路径回当前指令，空清单表示没有待落地的。
		if strings.HasSuffix(r.URL.Path, "/index") {
			// 只送出一次，队列满了就丢掉，避免堵住替身。
			select {
			// 索引到了就通知一次，重复的信号丢掉。
			case gotIndex <- struct{}{}:
			// 已经送出过就不再塞，避免堵住替身。
			default:
			}
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
			return
		}
		// 这条路径拒绝，避免替身把不该成功的请求放行。
		http.NotFound(w, r)
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 租约落地故意失败，用来确认连接还留着。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, func(Lease) error {
			return domain.ErrContentLeaseExpired
		}, nil, nil, nil)
	}()
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 索引到了就说明租约失败没有拆掉连接。
	case <-gotIndex:
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 租约失败把整条通道拆了就停测。
		t.Fatalf("hold dropped: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没拉到索引就停测。
		t.Fatal("no index")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 软件通知只带种类和版本，不能带上包字节。
func TestHoldSoftwareNotifyPassesMetaOnly(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 接住一条软件或正文通知。
	got := make(chan json.RawMessage, 1)
	// 替身回最高版但不提供包字节，拉包应被拒绝。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 最高版路径只回答种类和版本，不带字节。
		case strings.Contains(r.URL.Path, "/software/latest"):
			// 不是厂包就当没有，避免把别的包混进去。
			if r.URL.Query().Get("kind") != "factory_service" {
				// 这条路径拒绝，避免替身把不该成功的请求放行。
				http.NotFound(w, r)
				return
			}
			// 回厂包最高版，不带字节，通道不该来拉包。
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1})
		// 拉包路径不该被通道打到，打到就记失败。
		case strings.Contains(r.URL.Path, "/pull/software"):
			// 通道不该来拉包字节，记下来让测试失败。
			t.Error("hold must not pull software body")
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 只收下软件通知，断言里核对没有包字节。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, func(_ context.Context, in json.RawMessage) error {
			// 把软件通知送进断言，核对没有字节。
			got <- in
			return nil
		}, nil, nil, nil, nil, nil, nil)
	}()
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到通知就核对只有元数据、没有包字节。
	case raw := <-got:
		// 只收种类、版本和正文，用来断言没带字节。
		var meta struct {
			Kind    string `json:"kind"`    // 软件种类，这里应是厂服务包。
			Version int64  `json:"version"` // 整数版本，通知里应是一。
			Body    []byte `json:"body"`    // 包字节，通道通知里应为空。
		}
		// 通知带了正文或版本不对就停测。
		if json.Unmarshal(raw, &meta) != nil || meta.Kind != "factory_service" || meta.Version != 1 || len(meta.Body) > 0 {
			// 通知里带了正文或版本不对就停测。
			t.Fatalf("meta %s", raw)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到软件通知就停测。
		t.Fatal("no software")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 下行通知客户端包时只交元数据，不来拉字节。
func TestHoldPullsClientAPKFromMQTT(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 记下索引有没有被拉过。
	var sawIndex bool
	// 接住一条软件或正文通知。
	got := make(chan json.RawMessage, 1)
	// 替身不提供最高版，正文只能来自下行通知。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 索引被拉到就放开等待。
			sawIndex = true
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 最高版路径只回答种类和版本，不带字节。
		case strings.Contains(r.URL.Path, "/software/latest"):
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		// 拉包路径不该被通道打到，打到就记失败。
		case strings.Contains(r.URL.Path, "/pull/software"):
			// 通道不该来拉包字节，记下来让测试失败。
			t.Error("hold must not pull software body")
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 只收下软件通知，断言里核对没有包字节。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, func(_ context.Context, in json.RawMessage) error {
			// 把软件通知送进断言，核对没有字节。
			got <- in
			return nil
		}, nil, nil, nil, nil, nil, nil)
	}()
	// 给索引一个时限，过了还没有就判失败。
	deadline := time.Now().Add(2 * time.Second)
	// 时限内反复看索引，避免只查一次错过。
	for time.Now().Before(deadline) && !sawIndex {
		// 稍等再看，给通道留出发起请求的时间。
		time.Sleep(20 * time.Millisecond)
	}
	// 时限内没拉到索引就停测，通道没起来。
	if !sawIndex {
		// 时限内没拉到索引就停测。
		t.Fatal("no index")
	}
	// 索引订上后投客户端包通知，只应收到元数据。
	publishDown(t, broker, fid, Cmd{Typ: CmdSoftware, Kind: "client_apk", Version: 9, VersionName: "6.9.0"})
	// 等到客户端包通知，通道先死就停测。
	waitCh(t, ctx, errCh, "apk", got)
}

// 收到注销后应停通道，不能继续重连。
func TestHoldRetired(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 替身在索引里回注销，通道应当停下。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 租约路径回解包钥，通道才能继续拉索引。
		if strings.HasSuffix(r.URL.Path, "/lease") {
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		// 回注销状态，通道应收成停止重连。
		writeJSON(w, map[string]any{"cmds": []any{map[string]any{"typ": "factory_state", "status": "retired", "revision": 1}}})
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 治理回调直接报注销，通道应当因此停下。
	err = Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, func(State) error {
		return domain.ErrFactoryRetired
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// 不是注销就停测，通道不该继续重连。
	if !errors.Is(err, domain.ErrFactoryRetired) {
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	}
}

// 补拉请求应把种类带到索引查询上。
func TestHoldSendsSync(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 接住索引查询里的种类。
	gotKind := make(chan string, 1)
	// 替身记下索引查询里的种类，用来核对补拉。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 租约路径回解包钥，通道才能继续拉索引。
		if strings.HasSuffix(r.URL.Path, "/lease") {
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		// 索引路径回当前指令，空清单表示没有待落地的。
		if strings.HasSuffix(r.URL.Path, "/index") {
			// 查询里带了种类才记录，空的不算这次补拉。
			if k := r.URL.Query().Get("kind"); k != "" {
				// 只送出一次，队列满了就丢掉，避免堵住替身。
				select {
				// 把查询里的种类送出去，重复的丢掉。
				case gotKind <- k:
				// 已经送出过就不再塞，避免堵住替身。
				default:
				}
			}
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
			return
		}
		// 这条路径拒绝，避免替身把不该成功的请求放行。
		http.NotFound(w, r)
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 准备一条补拉请求，通道在线才收得到。
	out := make(chan SyncRequest, 1)
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，超时和失败才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, out)
	}()
	// 稍等再看，给通道留出发起请求的时间。
	time.Sleep(200 * time.Millisecond)
	// 送出补拉，索引查询应带上工艺这一类。
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到查询种类就核对是不是要补的那一类。
	case k := <-gotKind:
		// 补拉种类不对就停测。
		if k != "process" {
			// 补拉种类不对就停测。
			t.Fatalf("kind %s", k)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到补拉或对账就停测。
		t.Fatal("no sync")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 下行闭包、模版、撤回和绑定都要进回调。
func TestHoldAppliesDownCmds(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个闭包身份，拉取和撤回要对得上。
	assetID := uuid.New()
	// 造一个模版身份，拉取路径要对得上。
	tplID := uuid.New()
	// 造一个设备身份，绑定回执要对得上。
	cid := uuid.New()
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 记下索引有没有被拉过。
	var sawIndex bool
	// 记下替身被拉过哪些正文。
	pulled := make(chan string, 8)
	// 分路接住闭包、模版、软件、撤回和绑定。
	got := struct {
		closure, template, software chan json.RawMessage // 闭包、模版和软件通知各留一个位置。
		retract                     chan uuid.UUID       // 撤回的资产身份单独接住。
		client                      chan ClientIntent    // 绑定或作废意图单独接住。
	}{
		closure:  make(chan json.RawMessage, 1),
		template: make(chan json.RawMessage, 1),
		software: make(chan json.RawMessage, 1),
		retract:  make(chan uuid.UUID, 1),
		client:   make(chan ClientIntent, 1),
	}
	// 替身按路径回正文，并记下哪些被拉过。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回当前指令，空清单表示没有待落地的。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 索引被拉到就放开等待。
			sawIndex = true
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 闭包路径记下并回一份正文。
		case strings.Contains(r.URL.Path, "/pull/closure/"):
			// 记下闭包正文被拉过。
			pulled <- "closure"
			writeJSON(w, map[string]any{"assetId": assetID.String(), "body": "sealed"})
		// 模版路径记下并回一份正文。
		case strings.Contains(r.URL.Path, "/pull/template/"):
			// 记下模版正文被拉过。
			pulled <- "template"
			writeJSON(w, map[string]any{"id": tplID.String(), "schema": map[string]any{"root": "object"}})
		// 最高版路径只回答种类和版本，不带字节。
		case strings.Contains(r.URL.Path, "/software/latest"):
			// 不是厂包就当没有，避免把别的包混进去。
			if r.URL.Query().Get("kind") != "factory_service" {
				// 这条路径拒绝，避免替身把不该成功的请求放行。
				http.NotFound(w, r)
				return
			}
			// 回厂包最高版，不带字节，通道不该来拉包。
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1})
		// 拉包路径不该被通道打到，打到就记失败。
		case strings.Contains(r.URL.Path, "/pull/software"):
			// 记下软件包字节被拉过，通道本不该这么做。
			pulled <- "software"
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1, "body": []byte("apk-bytes")})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 绑定回调把意图送出，后面核对设备和识别号。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, func(in ClientIntent) error {
			// 把绑定意图送进断言。
			got.client <- in
			return nil
		}, nil, func(in json.RawMessage) error { // 闭包回调把正文送出，后面核对确实拉到了。
			// 把闭包正文送进断言。
			got.closure <- in
			return nil
		}, func(in json.RawMessage) error { // 模版回调把正文送出，后面核对模版拉到了。
			// 把模版正文送进断言。
			got.template <- in
			return nil
		}, func(_ context.Context, in json.RawMessage) error { // 软件回调把通知送出，后面核对收到了。
			// 把软件通知送进断言。
			got.software <- in
			return nil
		}, func(id uuid.UUID) error { // 撤回回调把资产身份送出，后面核对是不是那份。
			// 把撤回的资产身份送进断言。
			got.retract <- id
			return nil
		}, nil, nil, nil, nil, nil)
	}()
	// 给索引一个时限，过了还没有就判失败。
	deadline := time.Now().Add(2 * time.Second)
	// 时限内反复看索引，避免只查一次错过。
	for time.Now().Before(deadline) && !sawIndex {
		// 稍等再看，给通道留出发起请求的时间。
		time.Sleep(20 * time.Millisecond)
	}
	// 时限内没拉到索引就停测，通道没起来。
	if !sawIndex {
		// 时限内没拉到索引就停测。
		t.Fatal("no index")
	}
	// 索引订上后投闭包下行，回调才收得到正文。
	publishDown(t, broker, fid, Cmd{Typ: CmdClosure, AssetID: assetID.String()})
	// 再投模版下行，回调应收到模版正文。
	publishDown(t, broker, fid, Cmd{Typ: CmdTemplate, TemplateID: tplID.String()})
	// 再投撤回，回调应收到那份资产身份。
	publishDown(t, broker, fid, Cmd{Typ: CmdRetract, AssetID: assetID.String()})
	// 再投绑定，回调应收到设备名和识别号。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), ClientName: "焊机-1", DeviceSerial: "ARM-1", BindingRevision: 1})
	// 等到闭包落地，通道先死就停测。
	waitCh(t, ctx, errCh, "closure", got.closure)
	// 等到模版落地，通道先死就停测。
	waitCh(t, ctx, errCh, "template", got.template)
	// 等到软件通知，通道先死就停测。
	waitCh(t, ctx, errCh, "software", got.software)
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到撤回就核对是不是那份资产。
	case id := <-got.retract:
		// 撤回对象不对就停测。
		if id != assetID {
			// 撤回的对象不对就停测。
			t.Fatalf("retract %s", id)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到撤回就停测。
		t.Fatal("no retract")
	}
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到绑定就核对设备名和识别号。
	case in := <-got.client:
		// 绑定内容对不上就停测。
		if in.Typ != CmdClientBind || in.ClientID != cid || in.Name != "焊机-1" || in.DeviceSerial != "ARM-1" {
			// 绑定内容对不上就停测。
			t.Fatalf("client %+v", in)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到绑定就停测。
		t.Fatal("no client")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 有效厂应按索引对账，只留下仍在的设备。
func TestHoldSyncClients(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 造一个设备身份，绑定回执要对得上。
	cid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 接住对账留下的设备名单。
	got := make(chan []uuid.UUID, 1)
	// 替身回启用和一条绑定，对账应只留这一台。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 租约路径回解包钥，通道才能继续拉索引。
		if strings.HasSuffix(r.URL.Path, "/lease") {
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		// 回启用和一条仍有效的绑定，供对账核对。
		writeJSON(w, map[string]any{"cmds": []any{
			map[string]any{"typ": "factory_state", "status": "active", "revision": 1},
			map[string]any{"typ": "client_bind", "clientId": cid.String(), "clientName": "焊机-1", "bindingRevision": 1},
		}})
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 对账回调把留下的设备送出，后面核对名单。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, func(keep []uuid.UUID) error {
			// 把对账留下的设备送进断言。
			got <- keep
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 收到对账名单就核对只留下那一台。
	case keep := <-got:
		// 对账留下的设备不对就停测。
		if len(keep) != 1 || keep[0] != cid {
			// 对账留下的设备不对就停测。
			t.Fatalf("keep %+v", keep)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到补拉或对账就停测。
		t.Fatal("no sync")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 停用厂不该对账，避免误作废绑定。
func TestHoldSyncClientsSkipsDisabled(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 替身回停用状态，对账回调不该被叫到。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 租约路径回解包钥，通道才能继续拉索引。
		if strings.HasSuffix(r.URL.Path, "/lease") {
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		// 回停用状态，对账不该在这家厂发生。
		writeJSON(w, map[string]any{"cmds": []any{map[string]any{"typ": "factory_state", "status": "disabled", "revision": 1}}})
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 停用厂若来对账，就让这次测试失败。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, func([]uuid.UUID) error {
			// 停用厂不该对账，发生了就让测试失败。
			t.Error("reconcile while disabled")
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 等一会儿，确认停用厂没有去对账。
	time.Sleep(400 * time.Millisecond)
	// 结果、通道失败或超时谁先到就走谁。
	select {
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 通道还在就继续，已经退出才停测。
	default:
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 闭包第一次拉取回 503，不重连也要按种类补拉收到。
func TestHoldRetriesFailedClosurePull(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个闭包身份，拉取路径要对得上。
	assetID := uuid.New()
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 首轮整轮索引是空的，按工艺补拉时才回这条闭包。
	st := newResyncStub(t, func(kind string) []any {
		// 只有按工艺补拉才回闭包，证明是补拉收到的。
		if kind == "process" {
			// 回一条工艺闭包，补拉时再去拉正文。
			return []any{map[string]any{"typ": "closure", "assetId": assetID.String(), "kind": "process", "revision": 1}}
		}
		return nil
	}, func(n int32, w http.ResponseWriter) { // 第一次拉闭包回 503，之后回正文。
		// 第一次回 503，模拟弱网下拉取失败。
		if n == 1 {
			// 回服务不可用，通道应标脏而不是断开。
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// 之后回一份正文，补拉应收到它。
		writeJSON(w, map[string]any{"assetId": assetID.String(), "body": "sealed"})
	})
	// 接住落地的闭包正文，留够位置不堵回调。
	got := make(chan json.RawMessage, 4)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 闭包回调把正文送出，后面核对补拉收到了。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, func(in json.RawMessage) error {
			// 把闭包正文送进断言。
			got <- in
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 等首轮整轮索引拉完，下行订阅已经就绪。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 })
	// 投一条工艺闭包下行，第一次拉取会回 503。
	publishDown(t, broker, fid, Cmd{Typ: CmdClosure, AssetID: assetID.String(), Kind: "process", Revision: 1})
	// 等到补拉后的闭包落地，通道先死就停测。
	waitCh(t, ctx, errCh, "closure", got)
	// 没有按工艺补拉或只拉了一次就停测。
	if st.kindIndex.Load() < 1 || st.closure.Load() < 2 {
		// 补拉路径不对就停测。
		t.Fatalf("kind index %d closure pulls %d", st.kindIndex.Load(), st.closure.Load())
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 模版和目录回调第一次报错，补拉时都要再调一次。
func TestHoldRetriesFailedTemplateAndFS(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 索引里带一条工艺模版和一份工艺目录。
	st := newResyncStub(t, func(string) []any {
		// 整轮和按种类都回这两条，补拉才能再交给回调。
		return []any{
			map[string]any{"typ": "template", "templateId": uuid.NewString(), "kind": "process", "revision": 1},
			map[string]any{"typ": "fs_apply", "kind": "process", "snapshot": map[string]any{"kind": "process"}},
		}
	}, nil)
	// 记下模版和目录回调各被调了几次。
	var tplCalls, fsCalls atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 模版和目录回调都是第一次报库错，之后成功。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, nil, func(json.RawMessage) error {
			// 第一次模版落地报库错，应当标脏补拉。
			if tplCalls.Add(1) == 1 {
				// 报一条库错，通道应标脏不断开。
				return errors.New("db down")
			}
			return nil
		}, nil, nil, func(json.RawMessage) error { // 第一次目录对齐报库错，之后成功。
			// 第一次目录对齐报库错，应当标脏补拉。
			if fsCalls.Add(1) == 1 {
				// 报一条库错，通道应标脏不断开。
				return errors.New("db down")
			}
			return nil
		}, nil, nil, nil, nil)
	}()
	// 等两个回调都被再调一次，通道先死就停测。
	waitUntil(t, ctx, errCh, "template and fs retry", func() bool { return tplCalls.Load() >= 2 && fsCalls.Load() >= 2 })
	// 记下补齐时的补拉次数，之后不该再涨。
	settled := st.kindIndex.Load()
	// 等过几个封顶间隔，确认补齐后不再补拉。
	time.Sleep(5 * retryCap)
	// 补齐后还在补拉就停测。
	if n := st.kindIndex.Load(); n != settled || settled < 1 {
		// 补拉次数不对就停测。
		t.Fatalf("kind index %d then %d", settled, n)
	}
	// 通道应一直连着，退出了就停测。
	assertHolding(t, errCh)
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 撤回回调报错不断通道，补拉要再拉一次整轮索引。
func TestHoldRetractFailureResyncsFullIndex(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个资产身份，撤回要对得上。
	assetID := uuid.New()
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 撤回只在整轮索引里出现。
	st := newResyncStub(t, func(kind string) []any {
		// 整轮索引才带撤回，按种类不带。
		if kind == "" {
			// 回一条撤回，补拉时再交给回调。
			return []any{map[string]any{"typ": "retract", "assetId": assetID.String()}}
		}
		return nil
	}, nil)
	// 记下撤回回调被调了几次。
	var calls atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 撤回回调第一次报库错，之后成功。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, func(uuid.UUID) error {
			// 第一次撤回报库错，应当标全量补拉。
			if calls.Add(1) == 1 {
				// 报一条库错，通道应标全量不断开。
				return errors.New("db down")
			}
			return nil
		}, nil, nil, nil, nil, nil)
	}()
	// 等整轮索引被再拉一次、撤回再调一次。
	waitUntil(t, ctx, errCh, "full resync", func() bool { return st.fullIndex.Load() >= 2 && calls.Load() >= 2 })
	// 撤回补拉不该走按种类的索引。
	if n := st.kindIndex.Load(); n != 0 {
		// 走错补拉路径就停测。
		t.Fatalf("kind index %d", n)
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 回调报编号冲突是内容本身坏，不该补拉。
func TestHoldSkipsResyncOnContentRejected(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 整轮索引带一条工艺闭包，正文照常能拉到。
	st := newResyncStub(t, func(string) []any {
		// 整轮和按种类都回这条闭包。
		return []any{map[string]any{"typ": "closure", "assetId": uuid.NewString(), "kind": "process", "revision": 1}}
	}, nil)
	// 记下闭包回调被调了几次。
	var calls atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 闭包回调一律报编号冲突。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, func(json.RawMessage) error {
			// 记一次调用，再报编号冲突。
			calls.Add(1)
			return domain.ErrAssetCodeConflict
		}, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 等首轮把闭包交给回调。
	waitUntil(t, ctx, errCh, "closure", func() bool { return calls.Load() >= 1 })
	// 等过许多个补拉间隔，确认没有补拉。
	time.Sleep(20 * retryBase)
	// 补拉过或回调被再调就停测。
	if st.kindIndex.Load() != 0 || st.fullIndex.Load() != 1 || calls.Load() != 1 {
		// 内容坏的也去补拉了就停测。
		t.Fatalf("kind %d full %d calls %d", st.kindIndex.Load(), st.fullIndex.Load(), calls.Load())
	}
	// 通道应一直连着，退出了就停测。
	assertHolding(t, errCh)
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 一直失败的标记补拉八次后停下，新下行失败再从头补。
func TestHoldResyncGivesUpAfterMaxFails(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个模版身份，下行和索引共用。
	tplID := uuid.NewString()
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 整轮和按种类都回这条工艺模版。
	st := newResyncStub(t, func(string) []any {
		// 每次索引都带这条模版，回调每次都会失败。
		return []any{map[string]any{"typ": "template", "templateId": tplID, "kind": "process", "revision": 1}}
	}, nil)
	// 记下模版回调被调了几次。
	var calls atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 模版回调一律报库错。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, nil, func(json.RawMessage) error {
			// 记一次调用，再报库错。
			calls.Add(1)
			// 一律报库错，让标记一直失败。
			return errors.New("db down")
		}, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 首轮一次加八次补拉，共九次。
	want := int32(1 + retryMaxFails)
	// 等回调到九次，通道先死就停测。
	waitUntil(t, ctx, errCh, "max fails", func() bool { return calls.Load() >= want })
	// 等过几个封顶间隔，确认已经停下。
	time.Sleep(5 * retryCap)
	// 停下后还在补拉就停测。
	if n := calls.Load(); n != want || st.kindIndex.Load() != int32(retryMaxFails) {
		// 次数不对就停测。
		t.Fatalf("calls %d kind index %d", n, st.kindIndex.Load())
	}
	// 新的一条模版下行失败，算新标脏。
	publishDown(t, broker, fid, Cmd{Typ: CmdTemplate, TemplateID: tplID, Kind: "process", Revision: 2})
	// 新标脏后应从头补拉，回调次数继续涨。
	waitUntil(t, ctx, errCh, "restart", func() bool { return calls.Load() >= want+2 })
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 下行队列挤满时丢掉多余的并补拉整轮索引，不断开，取消后按时退出。
func TestHoldResyncsWhenQueueFull(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 记住原来的队列长度，测完要改回去。
	oldQueue := downQueue
	// 队列只留两格，几条下行就能挤满。
	downQueue = 2
	// 测完把队列长度改回去，避免影响别的测试。
	t.Cleanup(func() { downQueue = oldQueue })
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 索引一律回空，多出来的整轮索引只能是溢出补拉。
	st := newResyncStub(t, nil, nil)
	// 第一条绑定进了回调就通知用例。
	entered := make(chan struct{}, 1)
	// 用例关掉它，卡住的第一条绑定才放行。
	release := make(chan struct{})
	// 记下绑定回调被调了几次。
	var calls atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 第一条绑定卡在回调里，主循环不再取队列。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, func(ClientIntent) error {
			// 只卡第一条，后面的直接落地。
			if calls.Add(1) == 1 {
				// 告诉用例主循环已经卡住。
				entered <- struct{}{}
				// 等用例放开，超时也放开免得挂死。
				select {
				// 用例放开就继续落地。
				case <-release:
				// 超时也放开，避免用例失败时协程挂死。
				case <-ctx.Done():
				}
			}
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 等首轮整轮索引拉完，下行订阅已经就绪。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 })
	// 投第一条绑定，让主循环卡在回调里。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: uuid.NewString(), BindingRevision: 1})
	// 等主循环卡住，通道先死就停测。
	waitCh(t, ctx, errCh, "first bind", entered)
	// 再投八条绑定：两条进队列，其余应被丢掉。
	for i := 0; i < 8; i++ {
		// 每条换个设备，内容本身都合法。
		publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: uuid.NewString(), BindingRevision: 1})
	}
	// 给收包协程留时间，溢出丢弃先于放行发生。
	time.Sleep(300 * time.Millisecond)
	// 收包没被堵住，此时补拉还排着，通道应一直连着。
	assertHolding(t, errCh)
	// 放开卡住的绑定，主循环接着取队列和补拉。
	close(release)
	// 溢出标了全量脏，整轮索引应被再拉一次。
	waitUntil(t, ctx, errCh, "overflow resync", func() bool { return st.fullIndex.Load() >= 2 })
	// 溢出补拉不该走按种类的索引。
	if n := st.kindIndex.Load(); n != 0 {
		// 走错补拉路径就停测。
		t.Fatalf("kind index %d", n)
	}
	// 九条里有被丢掉的，回调不该九条都收到。
	if n := calls.Load(); n >= 9 {
		// 没有丢弃说明队列没生效，停测。
		t.Fatalf("calls %d", n)
	}
	// 补拉后通道仍应连着，退出了就停测。
	assertHolding(t, errCh)
	// 取消通道，应按时退出不挂住。
	cancel()
	// 退出、或等过时限，谁先到走谁。
	select {
	// 通道退出就核对是按取消退的。
	case err := <-errCh:
		// 不是取消就停测，说明别的原因断了。
		if !errors.Is(err, context.Canceled) {
			// 退出原因不对就停测。
			t.Fatalf("hold after cancel: %v", err)
		}
	// 两秒还没退出就算挂住。
	case <-time.After(2 * time.Second):
		// 取消后迟迟不退就停测。
		t.Fatal("hold hung after cancel")
	}
}

// 进页补拉只拉带种类的索引，不领租约、不问包，也不落绑定、治理状态和对账。
func TestHoldPageSyncIsLight(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 整轮和按种类都回治理状态、绑定和工艺正文三类指令。
	st := newResyncStub(t, func(string) []any {
		// 按种类的索引里平台同样带绑定和治理状态。
		return []any{
			map[string]any{"typ": "factory_state", "status": "active", "revision": 1},
			map[string]any{"typ": "client_bind", "clientId": uuid.NewString(), "bindingRevision": 1},
			map[string]any{"typ": "closure", "assetId": uuid.NewString(), "kind": "process", "revision": 1},
			map[string]any{"typ": "template", "templateId": uuid.NewString(), "kind": "process", "revision": 1},
			map[string]any{"typ": "fs_apply", "kind": "process", "snapshot": map[string]any{"kind": "process"}},
		}
	}, nil)
	// 记下各回调被调了几次。
	var states, binds, reconciles, closures, templates, layouts atomic.Int32
	// 进页补拉信箱，通道在线才收得到。
	out := make(chan SyncRequest, 1)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 各回调只计数，后面核对进页补拉碰了哪些。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, func(State) error {
			// 记一次治理状态落地。
			states.Add(1)
			return nil
		}, func(ClientIntent) error { // 绑定回调只计数。
			// 记一次绑定落地。
			binds.Add(1)
			return nil
		}, func([]uuid.UUID) error { // 对账回调只计数。
			// 记一次对账。
			reconciles.Add(1)
			return nil
		}, func(json.RawMessage) error { // 闭包回调只计数。
			// 记一次闭包落地。
			closures.Add(1)
			return nil
		}, func(json.RawMessage) error { // 模版回调只计数。
			// 记一次模版落地。
			templates.Add(1)
			return nil
		}, func(context.Context, json.RawMessage) error { // 软件回调不做事，只让整轮去问包。
			return nil
		}, nil, func(json.RawMessage) error { // 目录回调只计数。
			// 记一次目录对齐。
			layouts.Add(1)
			return nil
		}, nil, nil, nil, out)
	}()
	// 等首轮整轮同步做完对账、后台问完两类包，计数才稳定。
	waitUntil(t, ctx, errCh, "first sync", func() bool { return reconciles.Load() >= 1 && st.software.Load() >= 2 })
	// 记下首轮后的租约和问包次数，进页补拉后不该再涨。
	lease, software := st.lease.Load(), st.software.Load()
	// 首轮应领过租约也问过包，否则下面的比较没有意义。
	if lease < 1 || software < 1 {
		// 首轮没走完整流程就停测。
		t.Fatalf("first sync lease %d software %d", lease, software)
	}
	// 送一次工艺进页补拉。
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	// 等工艺闭包、模版和目录都被再落一次。
	waitUntil(t, ctx, errCh, "page sync", func() bool {
		// 三类正文都落了第二次才算补拉做完。
		return closures.Load() >= 2 && templates.Load() >= 2 && layouts.Load() >= 2
	})
	// 补拉应只走一次按种类的索引。
	if n := st.kindIndex.Load(); n != 1 {
		// 补拉路径不对就停测。
		t.Fatalf("kind index %d", n)
	}
	// 领了租约、问了包，或落了治理状态、绑定、对账就停测。
	if st.lease.Load() != lease || st.software.Load() != software || states.Load() != 1 || binds.Load() != 1 || reconciles.Load() != 1 {
		// 进页补拉做了整轮的事就停测。
		t.Fatalf("lease %d software %d states %d binds %d reconciles %d", st.lease.Load(), st.software.Load(), states.Load(), binds.Load(), reconciles.Load())
	}
	// 通道应一直连着，退出了就停测。
	assertHolding(t, errCh)
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 本厂已有同修订就不再拉闭包和模版正文，首轮和下行重复送达都一样。
func TestHoldSkipsPullWhenHave(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个闭包身份，本厂已有它的第三修订。
	assetID := uuid.New()
	// 造一个模版身份，本厂已有它的第二修订。
	tplID := uuid.New()
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 首轮整轮索引就带这两条已有的修订。
	st := newResyncStub(t, func(kind string) []any {
		// 只有整轮索引带，按种类不带。
		if kind == "" {
			// 回本厂已有的闭包和模版修订。
			return []any{
				map[string]any{"typ": "closure", "assetId": assetID.String(), "kind": "process", "revision": 3},
				map[string]any{"typ": "template", "templateId": tplID.String(), "kind": "process", "revision": 2},
			}
		}
		return nil
	}, nil)
	// 接住落地的闭包正文，留够位置不堵回调。
	got := make(chan json.RawMessage, 4)
	// 记下本厂被问了几次。
	var asked atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 闭包回调把正文送出；本厂只认那两条修订已有。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, func(in json.RawMessage) error {
			// 把闭包正文送进断言。
			got <- in
			return nil
		}, func(json.RawMessage) error { // 模版回调不做事，只让通道肯去拉。
			return nil
		}, nil, nil, nil, nil, nil, func(typ string, id uuid.UUID, revision int64) (bool, error) { // 只认那两条修订已有。
			// 记一次问本厂。
			asked.Add(1)
			// 闭包第三修订和模版第二修订算已有。
			return (typ == CmdClosure && id == assetID && revision == 3) || (typ == CmdTemplate && id == tplID && revision == 2), nil
		}, nil)
	}()
	// 等首轮整轮索引拉完、两条都问过本厂，下行订阅已经就绪。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 && asked.Load() >= 2 })
	// 下行重复送达已有的闭包修订，不该再拉。
	publishDown(t, broker, fid, Cmd{Typ: CmdClosure, AssetID: assetID.String(), Kind: "process", Revision: 3})
	// 下行重复送达已有的模版修订，不该再拉。
	publishDown(t, broker, fid, Cmd{Typ: CmdTemplate, TemplateID: tplID.String(), Kind: "process", Revision: 2})
	// 再投一条更高修订的闭包，本厂没有就该去拉。
	publishDown(t, broker, fid, Cmd{Typ: CmdClosure, AssetID: assetID.String(), Kind: "process", Revision: 4})
	// 等新修订落地，前面两条下行此时已处理完。
	waitCh(t, ctx, errCh, "new revision", got)
	// 已有的修订被拉过，或新修订没拉就停测。
	if st.closure.Load() != 1 || st.template.Load() != 0 {
		// 拉取次数不对就停测。
		t.Fatalf("closure pulls %d template pulls %d", st.closure.Load(), st.template.Load())
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 同一种类去抖期内连发两次进页补拉，只拉一次索引；别的种类照拉。
func TestHoldPageSyncDebounced(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 按种类分别记下补拉索引被拉了几次。
	var process, project atomic.Int32
	// 索引一律回空，只按种类计数。
	st := newResyncStub(t, func(kind string) []any {
		// 按种类分开记。
		switch kind {
		// 工艺补拉记一次。
		case "process":
			// 记一次工艺补拉。
			process.Add(1)
		// 工程补拉记一次。
		case "project":
			// 记一次工程补拉。
			project.Add(1)
		}
		return nil
	}, nil)
	// 进页补拉信箱，留够位置一次塞几条。
	out := make(chan SyncRequest, 4)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 回调都不要，只看补拉次数。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, out)
	}()
	// 等首轮整轮索引拉完，补拉信箱才有人收。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 })
	// 第一次进工艺页补拉。
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	// 等第一次工艺补拉拉到索引。
	waitUntil(t, ctx, errCh, "first page sync", func() bool { return process.Load() >= 1 })
	// 去抖期内再进一次工艺页，不该再拉。
	out <- SyncRequest{Typ: "sync_templates", Kind: "process"}
	// 进工程页，别的种类照拉。
	out <- SyncRequest{Typ: "sync_closures", Kind: "project"}
	// 等工程补拉拉到索引，前一条此时已处理完。
	waitUntil(t, ctx, errCh, "project page sync", func() bool { return project.Load() >= 1 })
	// 再等一会儿，确认工艺没有被补拉第二次。
	time.Sleep(200 * time.Millisecond)
	// 工艺补拉超过一次就停测。
	if n := process.Load(); n != 1 {
		// 去抖没生效就停测。
		t.Fatalf("process page sync %d", n)
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 后台补拉卡在下载大闭包时，下行的绑定照样及时落地。
func TestHoldPageSyncDoesNotBlockBind(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 后台开始下载闭包就通知用例。
	entered := make(chan struct{}, 1)
	// 用例结束时放开，卡住的下载才收尾。
	release := make(chan struct{})
	// 只有按工艺补拉才回闭包，首轮整轮是空的。
	st := newResyncStub(t, func(kind string) []any {
		// 工艺补拉才带这条闭包。
		if kind == "process" {
			// 回一条工艺闭包，后台补拉会去下载。
			return []any{map[string]any{"typ": "closure", "assetId": uuid.NewString(), "kind": "process", "revision": 1}}
		}
		return nil
	}, func(_ int32, w http.ResponseWriter) { // 闭包下载卡住，直到用例放开。
		// 通知用例下载已经开始，重复的丢掉。
		select {
		// 送出一次开始信号。
		case entered <- struct{}{}:
		// 已经送过就不再塞。
		default:
		}
		// 卡到用例放开为止，模拟弱网下载大闭包。
		<-release
		// 放开后回一份占位正文。
		writeJSON(w, map[string]any{"body": "sealed"})
	})
	// 替身关掉前先放开卡住的下载，否则关不掉。
	t.Cleanup(func() { close(release) })
	// 接住落地的绑定意图。
	got := make(chan ClientIntent, 1)
	// 进页补拉信箱，通道在线才收得到。
	out := make(chan SyncRequest, 1)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 绑定回调把意图送出，闭包回调不做事。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, func(in ClientIntent) error {
			// 把绑定意图送进断言。
			got <- in
			return nil
		}, nil, func(json.RawMessage) error { // 闭包回调不做事，只让通道肯去拉。
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, out)
	}()
	// 等首轮整轮索引拉完，补拉信箱才有人收。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 })
	// 进工艺页，后台去下载那条闭包。
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	// 等后台卡在下载里。
	waitCh(t, ctx, errCh, "closure download", entered)
	// 造一个设备身份，绑定要对得上。
	cid := uuid.New()
	// 投一条绑定下行，主循环应立刻落地。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), BindingRevision: 1})
	// 绑定、通道失败或一秒超时谁先到走谁。
	select {
	// 收到绑定就核对是不是那台。
	case in := <-got:
		// 落地的设备不对就停测。
		if in.ClientID != cid {
			// 绑定对象不对就停测。
			t.Fatalf("client %+v", in)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 一秒还没落地就算被后台下载卡住。
	case <-time.After(time.Second):
		// 绑定被卡住就停测。
		t.Fatal("bind blocked by background closure download")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 取消后 Hold 要等后台补拉落完手上这一条才返回，不和下一条连接同时落库。
func TestHoldWaitsResyncOnExit(t *testing.T) {
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个放行代理，通道才能在本机拨号。
	_, mqttAddr := startAllowBroker(t)
	// 只有按工艺补拉才回闭包，首轮整轮是空的。
	st := newResyncStub(t, func(kind string) []any {
		// 工艺补拉才带这条闭包。
		if kind == "process" {
			// 回一条工艺闭包，后台补拉会去落库。
			return []any{map[string]any{"typ": "closure", "assetId": uuid.NewString(), "kind": "process", "revision": 1}}
		}
		return nil
	}, nil)
	// 后台开始落库就通知用例。
	entered := make(chan struct{}, 1)
	// 记下后台落库是否已收尾。
	var finished atomic.Bool
	// 进页补拉信箱，通道在线才收得到。
	out := make(chan SyncRequest, 1)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 闭包落库卡到取消之后，再拖一会儿才收尾。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, nil, nil, func(json.RawMessage) error {
			// 通知用例后台已在落库，重复的丢掉。
			select {
			// 送出一次开始信号。
			case entered <- struct{}{}:
			// 已经送过就不再塞。
			default:
			}
			// 等用例取消通道。
			<-ctx.Done()
			// 取消后再拖一会儿，Hold 必须等它。
			time.Sleep(200 * time.Millisecond)
			// 记下落库已收尾。
			finished.Store(true)
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, out)
	}()
	// 等首轮整轮索引拉完，补拉信箱才有人收。
	waitUntil(t, ctx, errCh, "first index", func() bool { return st.fullIndex.Load() >= 1 })
	// 进工艺页，后台去落那条闭包。
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	// 等后台卡在落库里。
	waitCh(t, ctx, errCh, "closure apply", entered)
	// 取消通道，Hold 应等后台落完再返回。
	cancel()
	// 退出、或等过时限，谁先到走谁。
	select {
	// 通道退出就核对原因和后台是否已收尾。
	case err := <-errCh:
		// 不是取消就停测，说明别的原因断了。
		if !errors.Is(err, context.Canceled) {
			// 退出原因不对就停测。
			t.Fatalf("hold after cancel: %v", err)
		}
		// 后台还没收尾 Hold 就返回了，停测。
		if !finished.Load() {
			// 没等后台协程就返回，停测。
			t.Fatal("hold returned before background resync finished")
		}
	// 两秒还没退出就算挂住。
	case <-time.After(2 * time.Second):
		// 取消后迟迟不退就停测。
		t.Fatal("hold hung after cancel")
	}
}

// 后台整轮补拉期间经下行落地的绑定，要并进对账名单，不能被当成漏掉的作废。
func TestHoldBackgroundReconcileKeepsNewBinds(t *testing.T) {
	// 把补拉间隔调短，用例不必等上几秒。
	shortRetry(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 后台整轮开始下载闭包就通知用例。
	entered := make(chan struct{}, 1)
	// 用例放开后，卡住的下载才收尾。
	release := make(chan struct{})
	// 放开只做一次，用例和清理都可能调。
	releaseOnce := sync.OnceFunc(func() { close(release) })
	// 记下整轮索引被拉了几次。
	var fulls atomic.Int32
	// 首轮整轮带一条撤回，之后的整轮带一条会卡住的闭包。
	st := newResyncStub(t, func(kind string) []any {
		// 按种类的索引不带东西。
		if kind != "" {
			return nil
		}
		// 首轮带撤回，撤回失败会标全量补拉。
		if fulls.Add(1) == 1 {
			// 回启用和一条撤回。
			return []any{
				map[string]any{"typ": "factory_state", "status": "active", "revision": 1},
				map[string]any{"typ": "retract", "assetId": uuid.NewString()},
			}
		}
		// 之后的整轮带一条闭包，下载时卡住。
		return []any{
			map[string]any{"typ": "factory_state", "status": "active", "revision": 1},
			map[string]any{"typ": "closure", "assetId": uuid.NewString(), "kind": "process", "revision": 1},
		}
	}, func(_ int32, w http.ResponseWriter) { // 闭包下载卡住，直到用例放开。
		// 通知用例下载已经开始，重复的丢掉。
		select {
		// 送出一次开始信号。
		case entered <- struct{}{}:
		// 已经送过就不再塞。
		default:
		}
		// 卡到用例放开为止，此时整轮索引已经拉完。
		<-release
		// 放开后回一份占位正文。
		writeJSON(w, map[string]any{"body": "sealed"})
	})
	// 替身关掉前先放开卡住的下载，否则关不掉。
	t.Cleanup(releaseOnce)
	// 记下撤回回调被调了几次。
	var retracts atomic.Int32
	// 接住落地的绑定意图。
	got := make(chan ClientIntent, 4)
	// 接住每次对账的名单。
	keeps := make(chan []uuid.UUID, 4)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 绑定和对账回调把结果送出，撤回第一次报库错。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, st.srv.URL, fid, priv, Trust{}, nil, func(in ClientIntent) error {
			// 把绑定意图送进断言。
			got <- in
			return nil
		}, func(keep []uuid.UUID) error { // 对账回调抄一份名单送出。
			// 抄一份名单送进断言，免得底层数组被改。
			keeps <- append([]uuid.UUID(nil), keep...)
			return nil
		}, func(json.RawMessage) error { // 闭包回调不做事，只让通道肯去拉。
			return nil
		}, nil, nil, func(uuid.UUID) error { // 撤回第一次报库错，之后成功。
			// 第一次撤回报库错，应当标全量补拉。
			if retracts.Add(1) == 1 {
				// 报一条库错，通道应标全量不断开。
				return errors.New("db down")
			}
			return nil
		}, nil, nil, nil, nil, nil)
	}()
	// 先等首轮对账，名单应是空的。
	select {
	// 收到首轮名单就核对是空的。
	case keep := <-keeps:
		// 首轮名单不空就停测。
		if len(keep) != 0 {
			// 首轮名单不对就停测。
			t.Fatalf("first keep %+v", keep)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到首轮对账就停测。
		t.Fatal("no first reconcile")
	}
	// 等后台整轮补拉卡在下载里，此时索引已经拉完。
	waitCh(t, ctx, errCh, "background closure download", entered)
	// 造一个设备身份，它不在后台拉到的索引里。
	cid := uuid.New()
	// 投一条绑定下行，主循环应立刻落地。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), BindingRevision: 1})
	// 等绑定落地，后台还卡在下载里。
	waitCh(t, ctx, errCh, "bind", got)
	// 放开下载，后台接着走到对账。
	releaseOnce()
	// 等后台整轮的对账名单。
	select {
	// 收到名单就核对刚绑上的设备在里面。
	case keep := <-keeps:
		// 先当没找到，扫到再改。
		found := false
		// 逐台看名单里有没有刚绑上的设备。
		for _, id := range keep {
			// 对上就记下找到了。
			if id == cid {
				// 记成已经找到。
				found = true
			}
		}
		// 刚绑上的设备不在名单里就停测，它会被误作废。
		if !found {
			// 名单漏了刚绑上的设备就停测。
			t.Fatalf("keep %+v missing %s", keep, cid)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 时限内没收到后台对账就停测。
		t.Fatal("no background reconcile")
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 补拉用例的替身平台，记下各路径被打了几次。
type resyncStub struct {
	srv       *httptest.Server // 替身网页服务，通道往这里拉
	fullIndex atomic.Int32     // 不带种类的整轮索引被拉了几次
	kindIndex atomic.Int32     // 带种类的补拉索引被拉了几次
	closure   atomic.Int32     // 闭包正文被拉了几次
	template  atomic.Int32     // 模版正文被拉了几次
	lease     atomic.Int32     // 租约被领了几次
	software  atomic.Int32     // 最高版软件包被问了几次
}

// 起补拉替身：租约照发，索引按种类回，闭包按第几次拉决定回什么。
func newResyncStub(t *testing.T, index func(kind string) []any, closure func(n int32, w http.ResponseWriter)) *resyncStub {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 预备计数，替身每打一次就记一笔。
	st := &resyncStub{}
	// 按路径回租约、索引和正文，假装平台。
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径分发，未知路径拒绝。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 记一次领租约，轻量补拉不该来领。
			st.lease.Add(1)
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 问最高版软件包只计数，回没有。
		case strings.Contains(r.URL.Path, "/software/latest"):
			// 记一次问包，轻量补拉不该来问。
			st.software.Add(1)
			// 回没有，通道不会去拉包。
			http.NotFound(w, r)
		// 索引路径按种类分别计数，再回用例给的指令。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 取出补拉的种类，空的是整轮索引。
			kind := r.URL.Query().Get("kind")
			// 整轮和按种类分开计数。
			if kind == "" {
				// 记一次整轮索引。
				st.fullIndex.Add(1)
			} else { // 带种类的是补拉索引。
				// 记一次按种类补拉。
				st.kindIndex.Add(1)
			}
			// 默认回空清单。
			cmds := []any{}
			// 用例给了指令就换成它的。
			if index != nil {
				// 按种类取用例的指令。
				if c := index(kind); c != nil {
					// 换成用例给的指令。
					cmds = c
				}
			}
			// 把指令清单回给通道。
			writeJSON(w, map[string]any{"cmds": cmds})
		// 闭包路径计数后交给用例决定回什么。
		case strings.Contains(r.URL.Path, "/pull/closure/"):
			// 记一次闭包拉取。
			n := st.closure.Add(1)
			// 用例没指定就回一份正文。
			if closure == nil {
				// 回一份占位正文。
				writeJSON(w, map[string]any{"body": "sealed"})
				return
			}
			// 交给用例按次数回。
			closure(n, w)
		// 模版路径计数后回一份占位正文。
		case strings.Contains(r.URL.Path, "/pull/template/"):
			// 记一次模版拉取。
			st.template.Add(1)
			// 回一份占位模版正文。
			writeJSON(w, map[string]any{"schema": map[string]any{"root": "object"}})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(st.srv.Close)
	return st
}

// 把补拉间隔调短，测完改回去，免得用例等上几分钟。
func shortRetry(t *testing.T) {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 记住原来的间隔，测完要改回去。
	oldBase, oldCap := retryBase, retryCap
	// 起步十毫秒、封顶四十毫秒，八次补拉一秒内跑完。
	retryBase, retryCap = 10*time.Millisecond, 40*time.Millisecond
	// 测完改回原来的间隔，避免影响别的测试。
	t.Cleanup(func() { retryBase, retryCap = oldBase, oldCap })
}

// 在超时前轮询等条件成立，通道先退出就停测。
func waitUntil(t *testing.T, ctx context.Context, errCh <-chan error, name string, cond func() bool) {
	// 失败算在调用方行号上，便于定位哪一步没到。
	t.Helper()
	// 条件不成立就继续等。
	for !cond() {
		// 通道退出、超时或稍等，谁先到走谁。
		select {
		// 通道先退出就停测，说明断开重连了。
		case err := <-errCh:
			// 通道先退出就停测，并带上这一步的名字。
			t.Fatalf("%s hold: %v", name, err)
		// 到时还没成立就停测。
		case <-ctx.Done():
			// 到时还没等到这一步就停测。
			t.Fatalf("no %s", name)
		// 稍等再看，给通道留出补拉时间。
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// 通道应一直连着，已经退出就停测。
func assertHolding(t *testing.T, errCh <-chan error) {
	// 失败算在调用方行号上，便于定位哪一步断了。
	t.Helper()
	// 不等待，只看通道是不是已经退出。
	select {
	// 通道已经退出就停测。
	case err := <-errCh:
		// 通道断了就停测，并带上原因。
		t.Fatalf("hold dropped: %v", err)
	// 还连着就继续。
	default:
	}
}

// 在超时前等到回调，通道先死就停测。
func waitCh[T any](t *testing.T, ctx context.Context, errCh <-chan error, name string, ch <-chan T) {
	// 失败算在调用方行号上，便于定位哪一步没到。
	t.Helper()
	// 回调、通道失败或超时谁先到就结束等待。
	select {
	// 回调到了就算这一步成功。
	case <-ch:
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道先退出就停测，并带上这一步的名字。
		t.Fatalf("%s hold: %v", name, err)
	// 到时还没有结果就停测。
	case <-ctx.Done():
		// 到时还没等到这一步就停测。
		t.Fatalf("no %s", name)
	}
}

// 起一个放行的本地代理，测完把端口让出来。
func startAllowBroker(t *testing.T) (*mochimqtt.Server, string) {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 占一个本机空端口，避免和别人冲突。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// 端口占不上就停测，后面无法拨号。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 建一个放行的本地代理，测试不验口令。
	server := mochimqtt.New(&mochimqtt.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// 挂上放行钩子，任何连接都能订主题。
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		// 放行钩子挂不上就停测。
		t.Fatal(err)
	}
	// 让代理听在刚才占下的端口上。
	if err := server.AddListener(listeners.NewNet("t1", ln)); err != nil {
		// 端口占不上就停测，后面无法拨号。
		t.Fatal(err)
	}
	// 后台跑代理，测试才能去拨号。
	go func() { _ = server.Serve() }()
	// 测完关掉代理，重复关也不要让测试失败。
	t.Cleanup(func() {
		// 关掉时如果已经停了就吞掉恐慌，不让测试误失败。
		defer func() { _ = recover() }()
		// 关掉代理并释放端口。
		_ = server.Close()
	})
	// 把代理和地址交回，调用方用来拨号或投递。
	return server, ln.Addr().String()
}

// 等 Hold 订上后再往 down 投一条。
func publishDown(t *testing.T, srv *mochimqtt.Server, fid uuid.UUID, cmd Cmd) {
	// 失败算在调用方行号上，便于定位哪条下行。
	t.Helper()
	// 编出下行，编不出就不投给代理。
	raw, err := json.Marshal(cmd)
	// 下行编不出就停测，这一条不投了。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 投到这家厂的下行，通道订上了才收得到。
	if err := srv.Publish("wan/"+fid.String()+"/down", raw, false, 1); err != nil {
		// 下行投不出去就停测。
		t.Fatal(err)
	}
}

// 替身按网页格式回正文，调用方才能解开。
func writeJSON(w http.ResponseWriter, v any) {
	// 标明是结构化正文，调用方才能解开。
	w.Header().Set("Content-Type", "application/json")
	// 把替身要回的正文写出去。
	_ = json.NewEncoder(w).Encode(v)
}

// 本机钟比 WAN 慢八小时，密码和拉取签名按回包 Date 校正后照样拉到索引。
func TestHoldSignsWithWANClock(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 假平台比本机快八小时，模拟硬件钟按本地时间存。
	ahead := 8 * time.Hour
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 起一个按假时间验密码的代理，钟没校正就连不上。
	mqttAddr := startClockBroker(t, ahead)
	// 记下索引拉到几次、签名被拒几次。
	var index, rejected atomic.Int32
	// 替身每个回包都带假时间，并按假时间验签名时刻。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 取假平台的当前时间，回包和验签都按它。
		now := time.Now().Add(ahead)
		// 每个回包都带假时间的 Date，通道只能从这里取时差。
		w.Header().Set("Date", now.UTC().Format(http.TimeFormat))
		// 探活不验签，只回个状态。
		if r.URL.Path == "/healthz" {
			// 回探活状态，通道只看头里的 Date。
			writeJSON(w, map[string]any{"status": "ok"})
			return
		}
		// 读签名时刻，按假时间验时间窗。
		unix, err := strconv.ParseInt(r.Header.Get("X-WMesh-Time"), 10, 64)
		// 超出前后三百秒就拒绝，和 WAN 的时间窗一样。
		if err != nil || absSec(unix-now.Unix()) > 300 {
			// 记一次拒绝，后面断言不该发生。
			rejected.Add(1)
			// 回未授权，和 WAN 拒绝过期签名一样。
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，到期时间也按假时间算。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": now.Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径记一次，再回空清单。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 记一次索引，证明签名过了时间窗。
			index.Add(1)
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，被拒或超时才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	// 等索引拉到，通道被拒退出就停测。
	waitUntil(t, ctx, errCh, "index", func() bool { return index.Load() >= 1 })
	// 有签名被拒就停测，说明没按 WAN 时间签。
	if n := rejected.Load(); n != 0 {
		// 签名被拒就停测，并带上次数。
		t.Fatalf("rejected %d", n)
	}
	// 结果到了就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 代理按时间窗拒绝时交回未授权，并补一条带时差的日志。
func TestHoldLogsSkewWhenUnauthorized(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 接住告警日志，后面数被拒那一条。
	logs := captureWarn(t)
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 代理按快八小时验密码，平台回包却是正常时间，校正不了。
	mqttAddr := startClockBroker(t, 8*time.Hour)
	// 平台一律回找不到，只提供正常时间的 Date。
	httpSrv := httptest.NewServer(http.NotFoundHandler())
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 拨号应被拒，错误类型保持未授权。
	err = Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// 不是未授权就停测，外层退避依赖这个类型。
	if !errors.Is(err, domain.ErrUnauthorized) {
		// 错误类型变了就停测。
		t.Fatalf("hold: %v", err)
	}
	// 被拒日志没打或打多了就停测。
	if n := logs.count("wan mqtt connect unauthorized"); n != 1 {
		// 日志条数不对就停测。
		t.Fatalf("unauthorized logs %d", n)
	}
}

// Date 头读得出就记时差，读不出或没有就沿用旧值。
func TestSampleClockParsesDate(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 平台快八小时，时差应记成约 28800 秒。
	sampleClock(time.Now(), dateResp(time.Now().Add(8*time.Hour).UTC().Format(http.TimeFormat)))
	// 时差不对就停测。
	assertSkewNear(t, 28800)
	// 校正后的时间应比本机快八小时。
	if d := time.Until(wanNow()) - 8*time.Hour; d.Abs() > 2*time.Second {
		// 校正时间不对就停测。
		t.Fatalf("wan now off by %v", d)
	}
	// 读不懂的 Date 不取样，时差保持原值。
	sampleClock(time.Now(), dateResp("not a date"))
	// 坏 Date 改了时差就停测。
	assertSkewNear(t, 28800)
	// 没有 Date 头也不取样。
	sampleClock(time.Now(), &http.Response{Header: http.Header{}})
	// 缺 Date 改了时差就停测。
	assertSkewNear(t, 28800)
	// 平台慢八小时，时差应记成负的。
	sampleClock(time.Now(), dateResp(time.Now().Add(-8*time.Hour).UTC().Format(time.RFC850)))
	// 负时差不对就停测。
	assertSkewNear(t, -28800)
}

// 时差不到一分钟按零算，已校正的厂对上时钟后恢复本机时间。
func TestSampleClockIgnoresSmallSkew(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 先记一个八小时的时差，模拟之前钟偏过。
	noteSkew(28800)
	// 平台只快三十秒，应按零处理。
	sampleClock(time.Now(), dateResp(time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat)))
	// 没收成零就停测。
	if n := clockSkew.Load(); n != 0 {
		// 小时差没收成零就停测。
		t.Fatalf("skew %d", n)
	}
	// 平台慢五十秒，同样按零处理。
	sampleClock(time.Now(), dateResp(time.Now().Add(-50*time.Second).UTC().Format(http.TimeFormat)))
	// 没收成零就停测。
	if n := clockSkew.Load(); n != 0 {
		// 小时差没收成零就停测。
		t.Fatalf("skew %d", n)
	}
	// 时差为零时校正时间就是本机时间。
	if d := time.Until(wanNow()); d.Abs() > time.Second {
		// 校正时间偏了就停测。
		t.Fatalf("wan now off by %v", d)
	}
}

// 往返太久的样本估不准，应丢掉不改时差。
func TestSampleClockDropsSlowRoundTrip(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 假装请求在两倍上限之前发出，往返超限。
	sampleClock(time.Now().Add(-2*skewMaxRTT), dateResp(time.Now().Add(8*time.Hour).UTC().Format(http.TimeFormat)))
	// 慢样本改了时差就停测。
	if n := clockSkew.Load(); n != 0 {
		// 慢样本没被丢掉就停测。
		t.Fatalf("skew %d", n)
	}
}

// 正文还在慢慢吐时就按响应头取样，之后的签名时间按 WAN 走。
func TestPullerSamplesClockOnHeaders(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 记住原来的往返上限，测完要改回去。
	oldRTT := skewMaxRTT
	// 往返上限调短，正文吐完时早已超限。
	skewMaxRTT = 200 * time.Millisecond
	// 测完改回原来的上限，避免影响别的测试。
	t.Cleanup(func() { skewMaxRTT = oldRTT })
	// 假平台比本机快八小时。
	ahead := 8 * time.Hour
	// 每段正文的大小，八段凑成大正文。
	chunk := 64 << 10
	// 替身先写头，再分段慢慢吐正文。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 回包带假时间的 Date，通道从这里取时差。
		w.Header().Set("Date", time.Now().Add(ahead).UTC().Format(http.TimeFormat))
		// 标明是结构化正文，拉取才能解开。
		w.Header().Set("Content-Type", "application/json")
		// 先把头写出去，正文还没开始。
		w.WriteHeader(http.StatusOK)
		// 推出头，让客户端先拿到。
		w.(http.Flusher).Flush()
		// 写正文开头。
		_, _ = io.WriteString(w, `{"pad":"`)
		// 分八段慢慢吐，总耗时超过往返上限。
		for i := 0; i < 8; i++ {
			// 每段之间停一会，模拟弱网下大正文。
			time.Sleep(50 * time.Millisecond)
			// 写一段填充正文。
			_, _ = io.WriteString(w, strings.Repeat("a", chunk))
			// 推出这一段，客户端边收边读。
			w.(http.Flusher).Flush()
		}
		// 写正文结尾，凑成完整 JSON。
		_, _ = io.WriteString(w, `"}`)
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 生成本厂签发钥，拉取时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 建一个拉取会话，向替身拉正文。
	p := newPuller(srv.URL, uuid.New(), priv)
	// 预备装慢慢吐完的正文。
	var dst struct {
		Pad string `json:"pad"` // 填充正文
	}
	// 拉不下来就停测，后面没法看时差。
	if err := p.get(context.Background(), "/v1/channel/index", &dst); err != nil {
		// 拉取失败就停测。
		t.Fatal(err)
	}
	// 正文没拉全就停测，说明替身没吐完。
	if len(dst.Pad) != 8*chunk {
		// 正文长度不对就停测。
		t.Fatalf("pad %d", len(dst.Pad))
	}
	// 时差应按响应头记成约八小时。
	assertSkewNear(t, 28800)
	// 再组一个请求，看签名时刻是不是按 WAN 时间。
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	// 请求组不出来就停测。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 写上签名头，时刻应按校正后的时间。
	p.sign(req)
	// 读出签名时刻，和假平台时间比。
	unix, err := strconv.ParseInt(req.Header.Get("X-WMesh-Time"), 10, 64)
	// 签名时刻离假平台时间太远就停测。
	if err != nil || absSec(unix-time.Now().Add(ahead).Unix()) > 2 {
		// 签名时刻不对就停测。
		t.Fatalf("sign time %q err %v", req.Header.Get("X-WMesh-Time"), err)
	}
}

// 时差在 28800 和 28801 之间来回跳时只打一次告警，大变化才再打。
func TestNoteSkewWarnsOnce(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 接住告警日志，后面数条数。
	logs := captureWarn(t)
	// 来回跳十次，模拟 Date 只精确到秒的抖动。
	for i := 0; i < 10; i++ {
		// 交替记 28800 和 28801。
		noteSkew(28800 + int64(i%2))
	}
	// 抖动反复刷日志就停测。
	if n := logs.count("factory clock differs from wan"); n != 1 {
		// 告警条数不对就停测。
		t.Fatalf("warns %d", n)
	}
	// 时差再变两分钟，算新偏差要再报。
	noteSkew(28800 + 120)
	// 大变化没再报就停测。
	if n := logs.count("factory clock differs from wan"); n != 2 {
		// 告警条数不对就停测。
		t.Fatalf("warns %d", n)
	}
}

// 时差是包级状态，测前清零，测完也清零免得串到别的用例。
func resetClock(t *testing.T) {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 测前清掉时差，别的用例留下的不算。
	clockSkew.Store(0)
	// 测前清掉已报时差，告警从头算。
	clockReported.Store(0)
	// 测完再清一次，避免影响别的测试。
	t.Cleanup(func() {
		// 清掉时差，后面的用例按本机时间签。
		clockSkew.Store(0)
		// 清掉已报时差，后面的用例告警从头算。
		clockReported.Store(0)
	})
}

// 时差离期望超过一秒就停测，Date 只精确到秒。
func assertSkewNear(t *testing.T, want int64) {
	// 失败算在调用方行号上，便于定位哪一步。
	t.Helper()
	// 差得多就停测，并带上实际时差。
	if got := clockSkew.Load(); absSec(got-want) > 1 {
		// 时差不对就停测。
		t.Fatalf("skew %d want %d", got, want)
	}
}

// 只带 Date 头的假回包，取样只看头。
func dateResp(date string) *http.Response {
	// 交回只有 Date 头的回包。
	return &http.Response{Header: http.Header{"Date": []string{date}}}
}

// 按拨快后的假时间验连接密码的代理钩子，模拟 WAN 的时间窗。
type clockAuthHook struct {
	mochimqtt.HookBase               // 钩子缺省实现，只覆盖鉴权和权限
	ahead              time.Duration // 假平台比本机快多少
}

// ID 给代理区分钩子。
func (h *clockAuthHook) ID() string { return "test-clock-auth" }

// Provides 只接管鉴权和权限，其余用缺省。
func (h *clockAuthHook) Provides(b byte) bool {
	// 只接管连接鉴权和主题权限。
	return b == mochimqtt.OnConnectAuthenticate || b == mochimqtt.OnACLCheck
}

// OnConnectAuthenticate 按假时间验密码里的签名时刻。
func (h *clockAuthHook) OnConnectAuthenticate(_ *mochimqtt.Client, pk packets.Packet) bool {
	// 拆出密码里的签名时刻。
	unix, _, err := nodekey.ParseMQTTPassword(string(pk.Connect.Password))
	// 密码拆不开就拒绝。
	if err != nil {
		return false
	}
	// 超出前后三百秒就拒绝，和 WAN 一样。
	return absSec(unix-time.Now().Add(h.ahead).Unix()) <= 300
}

// OnACLCheck 测试里主题一律放行。
func (h *clockAuthHook) OnACLCheck(*mochimqtt.Client, string, bool) bool { return true }

// 起一个按假时间验密码的本地代理，测完把端口让出来。
func startClockBroker(t *testing.T, ahead time.Duration) string {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 占一个本机空端口，避免和别人冲突。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// 端口占不上就停测，后面无法拨号。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 建一个本地代理，日志丢掉不刷屏。
	server := mochimqtt.New(&mochimqtt.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// 挂上按假时间验密码的钩子。
	if err := server.AddHook(&clockAuthHook{ahead: ahead}, nil); err != nil {
		// 钩子挂不上就停测。
		t.Fatal(err)
	}
	// 让代理听在刚才占下的端口上。
	if err := server.AddListener(listeners.NewNet("t1", ln)); err != nil {
		// 端口占不上就停测，后面无法拨号。
		t.Fatal(err)
	}
	// 后台跑代理，测试才能去拨号。
	go func() { _ = server.Serve() }()
	// 测完关掉代理，重复关也不要让测试失败。
	t.Cleanup(func() {
		// 关掉时如果已经停了就吞掉恐慌，不让测试误失败。
		defer func() { _ = recover() }()
		// 关掉代理并释放端口。
		_ = server.Close()
	})
	// 把地址交回，调用方用来拨号。
	return ln.Addr().String()
}

// 接住告警日志的替身，按消息数条数。
type warnLog struct {
	mu   sync.Mutex // 通道协程和用例会同时写
	msgs []string   // 收到的告警消息
}

// 把默认日志换成替身，测完换回去。
func captureWarn(t *testing.T) *warnLog {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 记住原来的默认日志，测完要换回去。
	old := slog.Default()
	// 建一个空替身。
	w := &warnLog{}
	// 换上替身，后面的告警都进这里。
	slog.SetDefault(slog.New(w))
	// 测完换回原来的默认日志。
	t.Cleanup(func() { slog.SetDefault(old) })
	return w
}

// 数消息里带这段文字的告警有几条。
func (w *warnLog) count(sub string) int {
	// 占住再读，避免和写入交错。
	w.mu.Lock()
	// 离开时放开，写入才能继续。
	defer w.mu.Unlock()
	// 从零开始数。
	n := 0
	// 逐条看消息里有没有这段文字。
	for _, m := range w.msgs {
		// 带这段文字就记一条。
		if strings.Contains(m, sub) {
			// 记一条匹配。
			n++
		}
	}
	return n
}

// Enabled 只收告警及以上。
func (w *warnLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }

// Handle 记下这条告警的消息。
func (w *warnLog) Handle(_ context.Context, r slog.Record) error {
	// 占住再写，避免并发追加写乱。
	w.mu.Lock()
	// 离开时放开，别人才能读写。
	defer w.mu.Unlock()
	// 记下消息，后面按文字数条数。
	w.msgs = append(w.msgs, r.Message)
	return nil
}

// WithAttrs 替身不管附加字段，原样交回。
func (w *warnLog) WithAttrs([]slog.Attr) slog.Handler { return w }

// WithGroup 替身不管分组，原样交回。
func (w *warnLog) WithGroup(string) slog.Handler { return w }

// 焊汇总回执：204 空正文、带跳过的 200、坏 JSON 的 200 都算上送成功，跳过和坏回执只记告警。
func TestPostWeldSummariesReceipt(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 换上告警替身，数跳过和坏回执的告警。
	warns := captureWarn(t)
	// 生成本厂签发钥，上送时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 逐种回执看是否都算成功、告警是否对得上。
	for _, tc := range []struct {
		name    string // 用例名，失败时好认
		code    int    // 替身回的状态码
		body    string // 替身回的正文，空则不带
		skipped int    // 应记的跳过告警条数
		bad     int    // 应记的坏回执告警条数
	}{
		{"old wan 204", http.StatusNoContent, "", 0, 0},
		{"empty 200", http.StatusOK, "", 0, 0},
		{"no skips", http.StatusOK, `{"skipped":[],"skippedCount":0}`, 0, 0},
		{"with skips", http.StatusOK, `{"skipped":[{"index":3,"reason":"invalid day"}],"skippedCount":7}`, 1, 0},
		{"bad json", http.StatusOK, `{"skipped":`, 0, 1},
	} {
		// 记下这一轮之前的告警数，只看本轮新增。
		skipBefore, badBefore := warns.count("wan skipped weld summary rows"), warns.count("wan receipt undecodable")
		// 替身只回这一种状态码和正文。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 先写状态码，模拟新旧版平台的回执。
			w.WriteHeader(tc.code)
			// 写回执正文，空串则不带正文。
			_, _ = w.Write([]byte(tc.body))
		}))
		// 上送一行汇总，平台按这一种回执回。
		err := PostWeldSummaries(context.Background(), srv.URL, uuid.New(), priv, []WeldSummaryRow{
			{Day: "2026-09-18", ProjectName: "工程A", WeldKind: "single", RunCount: 1, LengthMM: 1, DurationSec: 1},
		})
		// 用完就关掉替身，避免端口一直占住。
		srv.Close()
		// 平台已 2xx 收下，报错就是把回执当成了失败。
		if err != nil {
			// 带上用例名和错误便于定位。
			t.Fatalf("%s: %v", tc.name, err)
		}
		// 跳过告警条数不对，说明跳过的行没记或误记。
		if got := warns.count("wan skipped weld summary rows") - skipBefore; got != tc.skipped {
			// 带上用例名和实际条数。
			t.Fatalf("%s: skipped warns %d want %d", tc.name, got, tc.skipped)
		}
		// 坏回执告警条数不对，说明解析失败没记或误记。
		if got := warns.count("wan receipt undecodable") - badBefore; got != tc.bad {
			// 带上用例名和实际条数。
			t.Fatalf("%s: undecodable warns %d want %d", tc.name, got, tc.bad)
		}
	}
}

// 跳过原因只带前几条，拼成「序号:原因」。
func TestFirstWeldSkips(t *testing.T) {
	// 造七条跳过，超过告警里带的条数。
	skips := make([]weldSummarySkip, 0, 7)
	// 逐条造跳过的行，序号各不相同。
	for i := range 7 {
		// 原因都写日期坏，只看序号和条数。
		skips = append(skips, weldSummarySkip{Index: i, Reason: "invalid day"})
	}
	// 收成告警用的文本。
	got := firstWeldSkips(skips)
	// 条数超出上限或首条格式不对就停测。
	if len(got) != weldSkipsLogged || got[0] != "0:invalid day" {
		// 带上实际结果便于定位。
		t.Fatalf("first skips %v", got)
	}
}
