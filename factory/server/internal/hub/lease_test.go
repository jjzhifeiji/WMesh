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
