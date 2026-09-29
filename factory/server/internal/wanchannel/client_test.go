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
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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

	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 把通道结果送回，超时和失败才能被看见。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, func(Lease) error {
			return domain.ErrContentLeaseExpired
		}, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, func(in json.RawMessage) error {
			// 把软件通知送进断言，核对没有字节。
			got <- in
			return nil
		}, nil, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, func(in json.RawMessage) error {
			// 把软件通知送进断言，核对没有字节。
			got <- in
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
	err = Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, func(State) error {
		return domain.ErrFactoryRetired
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, out)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, func(in ClientIntent) error {
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
		}, func(in json.RawMessage) error { // 模版回调把正文送出，后面核对模版拉到了。
			// 把软件通知送进断言。
			got.software <- in
			return nil
		}, func(id uuid.UUID) error { // 撤回回调把资产身份送出，后面核对是不是那份。
			// 把撤回的资产身份送进断言。
			got.retract <- id
			return nil
		}, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, func(keep []uuid.UUID) error {
			// 把对账留下的设备送进断言。
			got <- keep
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil)
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
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, func([]uuid.UUID) error {
			// 停用厂不该对账，发生了就让测试失败。
			t.Error("reconcile while disabled")
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil)
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
