package wanchannel

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/nodekey"
)

// HTTPS 领到的租约：没人接、空钥、坏到期时间都跳过，落不上只记告警。
func TestApplyPulledLease(t *testing.T) {
	// 换上告警替身，后面按文字数条数。
	warns := captureWarn(t)
	// 没人接也不能出事，直接跳过。
	applyPulledLease(nil, leaseResp{Lease: []byte("k"), NotAfter: time.Now().UTC().Format(time.RFC3339Nano)})
	// 记下收到的租约，后面核对钥和到期时间。
	var got []Lease
	// 收下每一条租约，不报错。
	record := func(l Lease) error {
		// 把这条租约记下来。
		got = append(got, l)
		return nil
	}
	// 空钥不该交给本厂。
	applyPulledLease(record, leaseResp{NotAfter: time.Now().UTC().Format(time.RFC3339Nano)})
	// 到期时间认不出也不该交给本厂。
	applyPulledLease(record, leaseResp{Lease: []byte("k"), NotAfter: "not-a-time"})
	// 前两条都该跳过，交了就停测。
	if len(got) != 0 {
		// 跳过的租约被交给本厂就停测。
		t.Fatalf("skipped lease applied: %+v", got)
	}
	// 坏到期时间要留一条告警，便于排查。
	if warns.count("bad notAfter") != 1 {
		// 告警条数不对就停测。
		t.Fatalf("bad notAfter warns %d", warns.count("bad notAfter"))
	}
	// 到秒的到期时间也要认得。
	na := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	// 合法租约要原样交给本厂。
	applyPulledLease(record, leaseResp{Lease: []byte("key-1"), NotAfter: na.Format(time.RFC3339)})
	// 钥或到期时间对不上就停测。
	if len(got) != 1 || string(got[0].Key) != "key-1" || !got[0].NotAfter.Equal(na) {
		// 合法租约没原样落地就停测。
		t.Fatalf("lease %+v", got)
	}
	// 本厂落不上时只记告警，不往外抛。
	applyPulledLease(func(Lease) error { return errors.New("boom") }, leaseResp{Lease: []byte("k"), NotAfter: na.Format(time.RFC3339Nano)})
	// 落不上要留一条告警。
	if warns.count("apply content lease") != 1 {
		// 告警条数不对就停测。
		t.Fatalf("apply warns %d", warns.count("apply content lease"))
	}
}

// MQTT 和索引里带来的 lease 一律丢弃，只有 HTTPS 领到的租约落地，连接不断。
func TestHoldDropsChannelLease(t *testing.T) {
	// 换上告警替身，后面核对丢弃有记录。
	warns := captureWarn(t)
	// 造一个工厂身份，下行主题按它拼。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 三把不同的钥，落地时一眼看出来自哪条路。
	httpsKey, indexKey, mqttKey := []byte("https-lease-key"), []byte("index-lease-key"), []byte("mqtt-lease-key")
	// 留下代理句柄，后面往下行投伪造租约。
	broker, mqttAddr := startAllowBroker(t)
	// 接住索引到达的信号，重复的丢掉。
	gotIndex := make(chan struct{}, 1)
	// 替身经租约路径回真钥，索引里夹一条假租约。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回真钥，这是唯一该落地的一条。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回 HTTPS 租约，通道应原样交给本厂。
			writeJSON(w, map[string]any{"typ": "lease", "lease": httpsKey, "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引里夹一条 lease 指令，通道不该认。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 只送出一次，重复的信号丢掉。
			select {
			// 索引到了就通知一次。
			case gotIndex <- struct{}{}:
			// 已经送出过就不再塞，避免堵住替身。
			default:
			}
			// 回一条混进索引的租约指令。
			writeJSON(w, map[string]any{"cmds": []any{map[string]any{"typ": "lease", "lease": indexKey, "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}}})
		// 别的路径拒绝，避免替身放行不该成功的请求。
		default:
			// 这条路径拒绝。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)
	// 接住每次落地的租约钥。
	leases := make(chan []byte, 8)
	// 接住治理状态，用作 MQTT 已处理到哪的哨兵。
	states := make(chan State, 1)
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能投下行和断言。
	go func() {
		// 治理状态和租约各自送出，后面核对。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, func(s State) error {
			// 把治理状态送进断言。
			states <- s
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, func(l Lease) error { // 租约回调把钥送出，后面核对来路。
			// 把落地的钥送进断言。
			leases <- append([]byte(nil), l.Key...)
			return nil
		}, nil, nil, nil)
	}()
	// 首轮应先落 HTTPS 租约，等不到就停测。
	select {
	// 收到第一把钥就核对是不是 HTTPS 那把。
	case k := <-leases:
		// 首轮落地的不是 HTTPS 钥就停测。
		if !bytes.Equal(k, httpsKey) {
			// 落地的钥来路不对就停测。
			t.Fatalf("first lease %q", k)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测。
		t.Fatalf("hold: %v", err)
	// 到时还没有租约就停测。
	case <-ctx.Done():
		// 时限内没落 HTTPS 租约就停测。
		t.Fatal("no https lease")
	}
	// 等索引拉过，说明首轮已经走到索引。
	waitCh(t, ctx, errCh, "index", gotIndex)
	// 往下行投一条伪造租约，通道应丢弃。
	publishDown(t, broker, fid, Cmd{Typ: CmdLease, Lease: mqttKey, NotAfter: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	// 再投一条治理状态当哨兵，主循环按序处理。
	publishDown(t, broker, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 1})
	// 等哨兵落地，说明前面那条租约已经处理过。
	waitCh(t, ctx, errCh, "factory_state", states)
	// 不等待，只看有没有多落一把钥。
	select {
	// 又落了一把钥，说明通道或索引上的租约被认了。
	case k := <-leases:
		// 非 HTTPS 的钥落地就停测。
		t.Fatalf("channel lease applied %q", k)
	// 没有多落就对了。
	default:
	}
	// 索引和 MQTT 两条租约都要留下丢弃告警。
	if n := warns.count("drop lease from channel"); n < 2 {
		// 丢弃没记全就停测。
		t.Fatalf("drop warns %d", n)
	}
	// 丢弃租约不能拆连接。
	assertHolding(t, errCh)
	// 断言完就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 每小时续租也只走 HTTPS，新钥照常落地。
func TestHoldRenewsLeaseOverHTTPS(t *testing.T) {
	// 记住原来的续租间隔，测完要改回去。
	old := leaseEvery
	// 续租间隔调短，免得用例等一小时。
	leaseEvery = 30 * time.Millisecond
	// 测完改回原来的间隔，避免影响别的测试。
	t.Cleanup(func() { leaseEvery = old })
	// 造一个工厂身份，后面用来签名。
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
	// 数续租的 POST 来了几次。
	var renews atomic.Int32
	// 替身按路径回租约和空索引。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径回钥，续租走 POST 要记一次。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 续租走 POST，记下次数。
			if r.Method == http.MethodPost {
				// 多记一次续租。
				renews.Add(1)
			}
			// 回一把解包钥，通道应交给本厂。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("renew-lease-key"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回空清单。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 别的路径拒绝，避免替身放行不该成功的请求。
		default:
			// 这条路径拒绝。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(httpSrv.Close)
	// 数租约落地了几次。
	var applied atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等续租。
	go func() {
		// 租约回调只数次数，钥不对就记成不落地。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, func(l Lease) error {
			// 钥是替身回的那把才算落地。
			if string(l.Key) == "renew-lease-key" {
				// 多记一次落地。
				applied.Add(1)
			}
			return nil
		}, nil, nil, nil)
	}()
	// 首轮一次加至少一次续租，都要落地。
	waitUntil(t, ctx, errCh, "renewed lease", func() bool { return renews.Load() >= 1 && applied.Load() >= 2 })
	// 断言完就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}
