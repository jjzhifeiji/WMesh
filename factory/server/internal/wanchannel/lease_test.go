package wanchannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
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

// Hold 只在首轮整轮同步时领一次租约，按点续租交给 KeepLease，不再双份续。
func TestHoldLeavesRenewalToKeepLease(t *testing.T) {
	// 记住原来的续租间隔，测完要改回去。
	old := leaseEvery
	// 续租间隔调短，Hold 若还在续很快就会多领。
	leaseEvery = 20 * time.Millisecond
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
	// 数 Hold 一共领了几次租约。
	var leases atomic.Int32
	// 接住索引到达的信号，重复的丢掉。
	gotIndex := make(chan struct{}, 1)
	// 替身按路径回租约和空索引。
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引或拒绝，假装平台。
		switch {
		// 租约路径每来一次都记下。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 多记一次领租约。
			leases.Add(1)
			// 回一把解包钥，首轮同步要先落它。
			writeJSON(w, leaseBody("hold-lease-key"))
		// 索引路径回空清单，并报一声首轮已走到索引。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 只送出一次，重复的信号丢掉。
			select {
			// 索引到了就通知一次。
			case gotIndex <- struct{}{}:
			// 已经送出过就不再塞，避免堵住替身。
			default:
			}
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
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能数领租约的次数。
	go func() {
		// 租约回调只收下，不报错。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, Trust{}, nil, nil, nil, nil, nil, nil, nil, nil, func(Lease) error { return nil }, nil, nil, nil)
	}()
	// 等首轮走到索引，说明首轮租约已经领过。
	waitCh(t, ctx, errCh, "index", gotIndex)
	// 等上十个旧续租间隔，Hold 若还在续就会多领。
	time.Sleep(10 * leaseEvery)
	// 通道应一直连着。
	assertHolding(t, errCh)
	// 只该有首轮那一次，多了就是 Hold 还在续租。
	if n := leases.Load(); n != 1 {
		// 领租约次数不对就停测。
		t.Fatalf("hold lease requests %d", n)
	}
	// 断言完就取消，通道应按取消退出。
	cancel()
	// 等通道真正退出，避免测试提前拆掉代理。
	<-errCh
}

// 只有 HTTPS 平台、MQTT 根本没有时，KeepLease 照样按节拍签名 GET 领到并落地租约，取消即停。
func TestKeepLeaseRenewsWithoutMQTT(t *testing.T) {
	// 记住原来的续租间隔，测完要改回去。
	old := leaseEvery
	// 续租间隔调短，免得用例等一小时。
	leaseEvery = 20 * time.Millisecond
	// 测完改回原来的间隔，避免影响别的测试。
	t.Cleanup(func() { leaseEvery = old })
	// 造一个工厂身份，核对签名头要用。
	fid := uuid.New()
	// 生成本厂签发钥，领租约要签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 假平台每次都回同一把钥，不起任何 MQTT 代理。
	hits, wanURL := startLeaseWAN(t, func(int) (int, any) { return http.StatusOK, leaseBody("keep-lease-key") })
	// 数落地了几次。
	var applied atomic.Int32
	// 给这一轮限时，避免循环挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住循环退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑续租循环，主流程等它领够次数。
	go func() {
		// 钥是替身回的那把才算落地。
		errCh <- KeepLease(ctx, wanURL, fid, priv, func(l Lease) error {
			// 钥对得上才记一次落地。
			if string(l.Key) == "keep-lease-key" {
				// 多记一次落地。
				applied.Add(1)
			}
			return nil
		})
	}()
	// 连着落地三次，说明按节拍在续。
	waitUntil(t, ctx, errCh, "lease renewals", func() bool { return applied.Load() >= 3 })
	// 抄出每次领取用的方法和厂身份。
	_, methods, factories := hits.snapshot()
	// 逐次核对：只走 GET，且带着本厂签名身份。
	for i, m := range methods {
		// 方法或厂身份不对就停测。
		if m != http.MethodGet || factories[i] != fid.String() {
			// 领取方式不对就停测。
			t.Fatalf("lease request %d: %s %s", i, m, factories[i])
		}
	}
	// 断言完就取消，循环应立刻停。
	cancel()
	// 等循环交回取消原因，等太久就停测。
	select {
	// 交回的应是取消。
	case err := <-errCh:
		// 不是取消就停测。
		if !errors.Is(err, context.Canceled) {
			// 退出原因不对就停测。
			t.Fatalf("keep lease: %v", err)
		}
	// 取消后迟迟不停就停测。
	case <-time.After(2 * time.Second):
		// 取消后循环还在跑就停测。
		t.Fatal("keep lease not stopped on cancel")
	}
}

// 领不到时按退避重试，间隔从起步逐次翻倍到封顶；领到后回到续租间隔，不再连着打。
func TestKeepLeaseBacksOffOnFailure(t *testing.T) {
	// 换上告警替身，后面数失败告警。
	warns := captureWarn(t)
	// 退避调成十毫秒起、四十毫秒封顶。
	shortRetry(t)
	// 记住原来的续租间隔，测完要改回去。
	old := leaseEvery
	// 续租间隔调成一小时，领到后本轮不该再来。
	leaseEvery = time.Hour
	// 测完改回原来的间隔，避免影响别的测试。
	t.Cleanup(func() { leaseEvery = old })
	// 生成本厂签发钥，领租约要签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 前四次回暂时故障，第五次才给钥。
	hits, wanURL := startLeaseWAN(t, func(n int) (int, any) {
		// 前四次都回暂时故障。
		if n <= 4 {
			return http.StatusServiceUnavailable, map[string]any{}
		}
		// 第五次起给一把合法的钥。
		return http.StatusOK, leaseBody("backoff-key")
	})
	// 数落地了几次。
	var applied atomic.Int32
	// 给这一轮限时，避免循环挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住循环退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑续租循环，主流程等它领到。
	go func() {
		// 每落地一次记一笔。
		errCh <- KeepLease(ctx, wanURL, uuid.New(), priv, func(Lease) error {
			// 多记一次落地。
			applied.Add(1)
			return nil
		})
	}()
	// 等第五次领到并落地。
	waitUntil(t, ctx, errCh, "lease after backoff", func() bool { return applied.Load() == 1 })
	// 再等一阵，领到后不该接着打平台。
	time.Sleep(20 * retryBase)
	// 抄出每次到达的时刻。
	at, _, _ := hits.snapshot()
	// 四次失败加一次成功，多了就是领到后还在连着打。
	if len(at) != 5 {
		// 领取次数不对就停测。
		t.Fatalf("lease requests %d", len(at))
	}
	// 各次间隔的下限：起步、翻倍、封顶、封顶。
	want := []time.Duration{retryBase, 2 * retryBase, retryCap, retryCap}
	// 逐个核对相邻两次的间隔。
	for i, floor := range want {
		// 间隔短于退避就是没在退避。
		if gap := at[i+1].Sub(at[i]); gap < floor {
			// 退避不够就停测。
			t.Fatalf("gap %d = %v, want >= %v", i, gap, floor)
		}
	}
	// 四次失败都要留告警，便于排查弱网。
	if n := warns.count("renew content lease failed"); n != 4 {
		// 告警条数不对就停测。
		t.Fatalf("renew warns %d", n)
	}
	// 断言完就取消，循环应按取消退出。
	cancel()
	// 等循环真正退出，避免测试提前关掉替身。
	<-errCh
}

// 领到了但本厂落不上，也按退避重领，不干等一个续租间隔。
func TestKeepLeaseRetriesWhenApplyFails(t *testing.T) {
	// 退避调成十毫秒起、四十毫秒封顶。
	shortRetry(t)
	// 记住原来的续租间隔，测完要改回去。
	old := leaseEvery
	// 续租间隔调成一小时，第二次领取只能来自退避。
	leaseEvery = time.Hour
	// 测完改回原来的间隔，避免影响别的测试。
	t.Cleanup(func() { leaseEvery = old })
	// 生成本厂签发钥，领租约要签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 假平台每次都给钥。
	hits, wanURL := startLeaseWAN(t, func(int) (int, any) { return http.StatusOK, leaseBody("retry-key") })
	// 数本厂被交了几次租约。
	var calls atomic.Int32
	// 给这一轮限时，避免循环挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住循环退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑续租循环，第一次落地故意失败。
	go func() {
		// 第一次落不上，之后都收下。
		errCh <- KeepLease(ctx, wanURL, uuid.New(), priv, func(Lease) error {
			// 第一次假装库出错。
			if calls.Add(1) == 1 {
				// 交回库错，循环应按退避重领。
				return errors.New("db down")
			}
			return nil
		})
	}()
	// 等第二次交到本厂，说明按退避重领了。
	waitUntil(t, ctx, errCh, "lease retry after apply failure", func() bool { return calls.Load() >= 2 })
	// 再等一阵，第二次落上后不该再领。
	time.Sleep(20 * retryBase)
	// 只该领两次：失败一次、重领一次。
	if n := hits.count(); n != 2 {
		// 领取次数不对就停测。
		t.Fatalf("lease requests %d", n)
	}
	// 断言完就取消，循环应按取消退出。
	cancel()
	// 等循环真正退出，避免测试提前关掉替身。
	<-errCh
}

// 平台说已注销就立刻交回注销，不落地、不再重领。
func TestKeepLeaseStopsOnRetired(t *testing.T) {
	// 退避调短，万一误重试也能很快看出来。
	shortRetry(t)
	// 生成本厂签发钥，领租约要签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 假平台按注销回禁止。
	hits, wanURL := startLeaseWAN(t, func(int) (int, any) {
		// 每次都回注销，和平台口径一致。
		return http.StatusForbidden, map[string]any{"error": domain.ErrFactoryRetired.Error()}
	})
	// 给这一轮限时，误重试也不会挂死。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 同步跑续租，注销应立刻交回。
	err = KeepLease(ctx, wanURL, uuid.New(), priv, func(Lease) error {
		// 注销的厂不该落任何租约。
		t.Error("lease applied for retired factory")
		return nil
	})
	// 交回的不是注销就停测。
	if !errors.Is(err, domain.ErrFactoryRetired) {
		// 退出原因不对就停测。
		t.Fatalf("keep lease: %v", err)
	}
	// 只该领过一次，注销后不再重领。
	if n := hits.count(); n != 1 {
		// 领取次数不对就停测。
		t.Fatalf("lease requests %d", n)
	}
}

// 没钥、没地址直接拒绝，没人接租约就不领。
func TestKeepLeaseNeedsKeyURLAndHandler(t *testing.T) {
	// 假平台只数次数，任何一种都不该打过来。
	hits, wanURL := startLeaseWAN(t, func(int) (int, any) { return http.StatusOK, leaseBody("unused") })
	// 收下租约的空回调。
	apply := func(Lease) error { return nil }
	// 没有签发钥要交回找不到。
	if err := KeepLease(context.Background(), wanURL, uuid.New(), nil, apply); !errors.Is(err, domain.ErrNotFound) {
		// 没钥的结果不对就停测。
		t.Fatalf("no key: %v", err)
	}
	// 没有平台地址要交回不可达。
	if err := KeepLease(context.Background(), " ", uuid.New(), []byte("k"), apply); !errors.Is(err, domain.ErrWANUnreachable) {
		// 没地址的结果不对就停测。
		t.Fatalf("no url: %v", err)
	}
	// 没人接租约就直接返回。
	if err := KeepLease(context.Background(), wanURL, uuid.New(), []byte("k"), nil); err != nil {
		// 没回调的结果不对就停测。
		t.Fatalf("no handler: %v", err)
	}
	// 三种都不该打到平台。
	if n := hits.count(); n != 0 {
		// 有请求打过来就停测。
		t.Fatalf("lease requests %d", n)
	}
}

// 替身收到的领租约请求，供用例核对次数、方法和间隔。
type leaseHits struct {
	mu        sync.Mutex  // 替身协程和用例会同时读写
	at        []time.Time // 每次到达的时刻
	methods   []string    // 每次用的方法
	factories []string    // 每次签名头里的厂身份
}

// 记一次领租约，交回这是第几次，从一起数。
func (h *leaseHits) add(r *http.Request) int {
	// 占住再写，避免和用例读取交错。
	h.mu.Lock()
	// 写完就放开。
	defer h.mu.Unlock()
	// 记下到达时刻，供核对退避间隔。
	h.at = append(h.at, time.Now())
	// 记下方法，续租只该走 GET。
	h.methods = append(h.methods, r.Method)
	// 记下签名头里的厂身份。
	h.factories = append(h.factories, r.Header.Get("X-WMesh-Factory"))
	// 交回累计次数，替身按它挑回包。
	return len(h.at)
}

// 抄一份到达记录，读的时候不和替身交错。
func (h *leaseHits) snapshot() ([]time.Time, []string, []string) {
	// 占住再读，避免和写入交错。
	h.mu.Lock()
	// 读完就放开。
	defer h.mu.Unlock()
	// 各抄一份交回，调用方随便看。
	return append([]time.Time(nil), h.at...), append([]string(nil), h.methods...), append([]string(nil), h.factories...)
}

// 到目前为止领了几次。
func (h *leaseHits) count() int {
	// 占住再读，避免和写入交错。
	h.mu.Lock()
	// 读完就放开。
	defer h.mu.Unlock()
	// 交回累计次数。
	return len(h.at)
}

// 起一个只认租约路径的假平台；reply 按第几次请求给状态和正文，别的路径一律拒绝。
func startLeaseWAN(t *testing.T, reply func(n int) (int, any)) (*leaseHits, string) {
	// 失败算在调用方行号上，不指到这个助手内部。
	t.Helper()
	// 建一份空记录，替身每次到达都记上。
	hits := &leaseHits{}
	// 替身只认租约路径，别的拒绝。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不是租约路径就拒绝，也不记次数。
		if r.URL.Path != "/v1/channel/lease" {
			// 这条路径拒绝。
			http.NotFound(w, r)
			return
		}
		// 记下这一次，并按第几次取回包。
		code, body := reply(hits.add(r))
		// 标明是结构化正文，厂端才按字段解析。
		w.Header().Set("Content-Type", "application/json")
		// 先写状态码，失败的回包也要带正文。
		w.WriteHeader(code)
		// 写出正文，写失败由厂端按不可达处理。
		_ = json.NewEncoder(w).Encode(body)
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	return hits, srv.URL
}

// 一份合法的租约回包：钥原样带回，一小时后到期。
func leaseBody(key string) map[string]any {
	// 按平台格式拼回包，到期用纳秒精度。
	return map[string]any{"typ": "lease", "lease": []byte(key), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
}
