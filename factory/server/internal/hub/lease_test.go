package hub

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

// MQTT 拨不通时续租照样经 HTTPS 落地；墙钟已过期的租约也按在线给满窗，通道停后回到离线。
func TestLeaseKeptWhileMQTTDown(t *testing.T) {
	// 为本测单独分配工厂身份，避免和别的库串。
	fid := id.New()
	// 平台发的租约钥，长度要合解包钥。
	key := bytes.Repeat([]byte{7}, contentcrypt.KeySize)
	// 数平台收到几次领租约。
	var leases atomic.Int32
	// 替身只认租约路径，回一把墙钟已过期的钥，模拟厂钟偏快。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不是租约路径就拒绝，探活和问包都不必理会。
		if r.URL.Path != "/v1/channel/lease" {
			// 这条路径拒绝。
			http.NotFound(w, r)
			return
		}
		// 多记一次领租约。
		leases.Add(1)
		// 到期写成一小时前，只有在线才会给满窗。
		claimJSON(w, map[string]any{"typ": "lease", "lease": key, "notAfter": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)})
	}))
	// 先登记关替身，清理时排在枢纽之后再关。
	t.Cleanup(srv.Close)
	// 建枢纽，代理指向没人监听的端口，MQTT 只会拨不上。
	h := claimHub(t, srv.URL, fid)
	// 建出这家厂的库，像认领那样组装服务。
	svc, err := h.ensure(fid)
	// 厂库建不开就停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("ensure: %v", err)
	}
	// 生成并落下签发钥，续租要用它签名。
	if _, _, err := h.signingMaterial(context.Background(), svc); err != nil {
		// 签发钥落不下就停测。
		t.Fatalf("signing key: %v", err)
	}
	// 拉起出站：MQTT 拨不通，续租循环另跑。
	h.ensureChannel(fid)
	// 等到内存里出现平台那把钥，说明 HTTPS 续租已落地。
	waitLease(t, "lease applied while mqtt down", func() bool {
		// 取当前过站钥，取不到说明还没落地。
		got, err := svc.Store().TransitKey()
		// 取到且就是平台那把才算落地。
		return err == nil && bytes.Equal(got, key)
	})
	// 平台至少该收到一次领租约。
	if leases.Load() < 1 {
		// 没有领过却落了钥就停测。
		t.Fatalf("lease requests %d", leases.Load())
	}
	// 停掉这家厂的出站，续租循环随之离开。
	h.stopChannel(fid)
	// 离线后墙钟已过期的租约不再收下。
	waitLease(t, "offline after channel stop", func() bool {
		// 再交一次过期租约，离线应被拒。
		err := svc.ApplyContentLease(context.Background(), key, time.Now().Add(-time.Hour))
		// 按租约过期拒收才算已离线。
		return errors.Is(err, domain.ErrContentLeaseExpired)
	})
}

// MQTT 拨不通时续租遇平台说已注销，要把注销落到本厂库并停掉整条出站。
func TestLeaseRetiredClosesFactoryAndStopsChannel(t *testing.T) {
	// 为本测单独分配工厂身份，避免和别的库串。
	fid := id.New()
	// 数平台收到几次领租约。
	var leases atomic.Int32
	// 替身只认租约路径，一律回已注销，模拟云端注销了本厂。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不是租约路径就拒绝，探活和问包都不必理会。
		if r.URL.Path != "/v1/channel/lease" {
			// 这条路径拒绝。
			http.NotFound(w, r)
			return
		}
		// 多记一次领租约。
		leases.Add(1)
		// 先标明结构化正文，写状态码之后再设头就不生效了。
		w.Header().Set("Content-Type", "application/json")
		// 按平台口径回 403 和英文原因，厂端据此认出已注销。
		w.WriteHeader(http.StatusForbidden)
		// 写出注销原因，厂端按原文收成注销错误。
		claimJSON(w, map[string]any{"error": domain.ErrFactoryRetired.Error()})
	}))
	// 先登记关替身，清理时排在枢纽之后再关。
	t.Cleanup(srv.Close)
	// 建枢纽，代理指向没人监听的端口，注销只能经续租那路到达。
	h := claimHub(t, srv.URL, fid)
	// 建出这家厂的库，像认领那样组装服务。
	svc, err := h.ensure(fid)
	// 厂库建不开就停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("ensure: %v", err)
	}
	// 生成并落下签发钥，续租要用它签名。
	if _, _, err := h.signingMaterial(context.Background(), svc); err != nil {
		// 签发钥落不下就停测。
		t.Fatalf("signing key: %v", err)
	}
	// 先读一次厂况，确认起点不是已注销，免得断言白过。
	before, err := svc.Store().Lifecycle(context.Background())
	// 厂况读不出或起点已注销就停测。
	if err != nil || before.Status == store.FactoryRetired {
		// 起点不对，后面的断言没有意义。
		t.Fatalf("lifecycle before: %+v %v", before, err)
	}
	// 拉起出站：MQTT 拨不通，续租循环另跑并撞上注销。
	h.ensureChannel(fid)
	// 等到通道表里没了这家，说明续租那路已停掉出站。
	waitLease(t, "channel stopped after retired lease", func() bool {
		// 通道表里没有这家才算已停。
		return !claimHasChannel(h, fid)
	})
	// 平台至少该收到一次领租约，注销确实经续租到达。
	if leases.Load() < 1 {
		// 没领过租约却停了通道，说明走的不是这条路。
		t.Fatalf("lease requests %d", leases.Load())
	}
	// 读停通道之后的厂况，注销应已落库。
	after, err := svc.Store().Lifecycle(context.Background())
	// 厂况读不出就停测。
	if err != nil {
		// 读库失败无法判断是否已注销。
		t.Fatalf("lifecycle after: %v", err)
	}
	// 厂况须为已注销，本厂登录才会立即关掉。
	if after.Status != store.FactoryRetired {
		// 停了通道却没落注销就停测。
		t.Fatalf("status %q, want %q", after.Status, store.FactoryRetired)
	}
}

// MQTT 那路首轮补拉领到的租约也先标在线：续租还没成过一次时，墙钟已过期的钥照样收下。
func TestHoldLeaseMarksOnline(t *testing.T) {
	// 为本测单独分配工厂身份，避免和别的库串。
	fid := id.New()
	// 平台发的租约钥，长度要合解包钥。
	key := bytes.Repeat([]byte{9}, contentcrypt.KeySize)
	// 到期写成一小时前，模拟厂钟比平台快。
	past := time.Now().Add(-time.Hour)
	// 不拉起出站，平台地址只要非空即可。
	h := claimHub(t, "http://127.0.0.1:1", fid)
	// 建出这家厂的库，像认领那样组装服务。
	svc, err := h.ensure(fid)
	// 厂库建不开就停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("ensure: %v", err)
	}
	// 起点离线：直接落过期租约应被拒，确认前提成立。
	if err := svc.ApplyContentLease(context.Background(), key, past); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 起点不是离线，后面的断言没有意义。
		t.Fatalf("offline apply: %v", err)
	}
	// 已取消的通道不再标在线，也不落钥。
	gone, cancel := context.WithCancel(context.Background())
	// 先取消，模拟通道正在停。
	cancel()
	// 取消后交回取消原因，不落钥。
	if err := applyHTTPSLease(gone, svc, wanchannel.Lease{Key: key, NotAfter: past}); !errors.Is(err, context.Canceled) {
		// 取消了还往下落就停测。
		t.Fatalf("cancelled apply: %v", err)
	}
	// 取消那次不该把通道标成在线，过期租约仍应被拒。
	if err := svc.ApplyContentLease(context.Background(), key, past); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 取消后仍被标在线就停测。
		t.Fatalf("online after cancelled apply: %v", err)
	}
	// 走 Hold 回调用的同一函数，过期租约应被收下。
	if err := applyHTTPSLease(context.Background(), svc, wanchannel.Lease{Key: key, NotAfter: past}); err != nil {
		// 首轮补拉的钥被拒，厂钟偏快时要等续租先成一次。
		t.Fatalf("hold apply: %v", err)
	}
	// 取当前过站钥，确认就是平台那把。
	got, err := svc.Store().TransitKey()
	// 没取到或不是平台那把就停测。
	if err != nil || !bytes.Equal(got, key) {
		// 钥没落进内存就停测。
		t.Fatalf("transit key %x %v", got, err)
	}
}

// 续租那路撞上注销时落库失败要按退避重落，落上才交回真；取消就交回假。
func TestRetryCloseFromWAN(t *testing.T) {
	// 为日志分配一个工厂身份。
	fid := id.New()
	// 数落注销被调了几次。
	var calls atomic.Int32
	// 前两次模拟厂库瞬时出错，第三次落上。
	flaky := func(context.Context) error {
		// 前两次交回库错误。
		if calls.Add(1) <= 2 {
			// 模拟厂库瞬时出错。
			return errors.New("db down")
		}
		return nil
	}
	// 起步等十毫秒，测试不必等满一秒。
	if !retryCloseFromWAN(context.Background(), fid, flaky, 10*time.Millisecond) {
		// 落上了却交回假，调用方不会停通道。
		t.Fatal("want landed")
	}
	// 应正好落了三次：两次失败、一次落上。
	if calls.Load() != 3 {
		// 次数不对说明没有重试或多落了。
		t.Fatalf("calls %d", calls.Load())
	}
	// 一直落不上的情形，取消后要尽快离开。
	ctx, cancel := context.WithCancel(context.Background())
	// 测完放掉取消函数。
	defer cancel()
	// 一直落不上，模拟库长时间不可用。
	down := func(context.Context) error {
		// 每次都交回库错误。
		return errors.New("db down")
	}
	// 收重试的结果，主协程等它离开。
	done := make(chan bool, 1)
	// 后台重试，主协程稍后取消。
	go func() {
		// 交回是否落上。
		done <- retryCloseFromWAN(ctx, fid, down, 10*time.Millisecond)
	}()
	// 稍等让它重试几轮。
	time.Sleep(50 * time.Millisecond)
	// 取消，模拟通道正被别处停掉。
	cancel()
	// 等它离开，给足时间。
	select {
	// 取消后应交回假。
	case landed := <-done:
		// 没落上却交回真就停测。
		if landed {
			// 取消后不该算落上。
			t.Fatal("want not landed after cancel")
		}
	// 一秒内没离开就停测。
	case <-time.After(time.Second):
		// 取消后还在重试就停测。
		t.Fatal("retry did not stop after cancel")
	}
}

// 在时限内轮询等条件成立，到时还没成立就停测。
func waitLease(t *testing.T, name string, cond func() bool) {
	// 失败算在调用方行号上，便于定位哪一步没到。
	t.Helper()
	// 给十秒，拨号退避和库操作都够了。
	deadline := time.Now().Add(10 * time.Second)
	// 条件不成立就继续等。
	for !cond() {
		// 到时还没成立就停测。
		if time.Now().After(deadline) {
			// 带上这一步的名字停测。
			t.Fatalf("timeout: %s", name)
		}
		// 稍等再看，给后台循环留出时间。
		time.Sleep(20 * time.Millisecond)
	}
}
