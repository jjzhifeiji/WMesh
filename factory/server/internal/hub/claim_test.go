package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

// 确认回包丢了，签发钥已落本厂，也要挂上出站通道。
func TestClaimReplyLostStillHoldsChannel(t *testing.T) {
	// 为本测单独分配工厂身份，避免和别的库串。
	fid := id.New()
	// 约定初始超管身份，认领回执要对得上。
	pid := id.New()
	// 替身收到确认就掐断连接，模拟回包丢失。
	srv := claimWAN(t, fid, pid, true)
	// 先登记关替身，清理时排在枢纽之后再关。
	t.Cleanup(srv.Close)
	// 建只认本测平台地址的枢纽，测完自己回收。
	h := claimHub(t, srv.URL, fid)
	// 用建厂码认领，确认这步应当拿不到回包。
	_, err := h.Claim(context.Background(), srv.URL, "code", "secret")
	// 回包丢了应收成平台不可达，页面才会提示重试。
	if !errors.Is(err, domain.ErrWANUnreachable) {
		// 错误不对就停测，后面断言没有意义。
		t.Fatalf("claim: %v", err)
	}
	// 回包丢了也要挂上通道，平台已登记就能自己连上。
	if !claimHasChannel(h, fid) {
		// 没挂通道就停测，这正是要修的问题。
		t.Fatal("channel not held after lost confirm reply")
	}
	// 打开本厂服务，核对签发钥确实已经落库。
	svc, err := h.Service(context.Background(), fid)
	// 厂库打不开就停测，认领前几步没有落地。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("service: %v", err)
	}
	// 签发钥必须已在本厂，通道才有钥可拨。
	if _, err := svc.Store().SigningKey(context.Background()); err != nil {
		// 没有签发钥就停测，通道挂上也拨不出去。
		t.Fatalf("signing key: %v", err)
	}
}

// 确认成功时照旧挂上出站通道，并交回本厂信息。
func TestClaimConfirmedHoldsChannel(t *testing.T) {
	// 为本测单独分配工厂身份，避免和别的库串。
	fid := id.New()
	// 约定初始超管身份，认领回执要对得上。
	pid := id.New()
	// 替身正常回确认，表示公钥已经收下。
	srv := claimWAN(t, fid, pid, false)
	// 先登记关替身，清理时排在枢纽之后再关。
	t.Cleanup(srv.Close)
	// 建只认本测平台地址的枢纽，测完自己回收。
	h := claimHub(t, srv.URL, fid)
	// 用建厂码认领，这次应当整次成功。
	got, err := h.Claim(context.Background(), srv.URL, "code", "secret")
	// 认领失败就停测，后面断言没有意义。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("claim: %v", err)
	}
	// 交回的厂身份或登录名不对就停测。
	if got.ID != fid || got.SALogin != "sa" {
		// 认领结果和替身不一致就停测。
		t.Fatalf("site %+v", got)
	}
	// 确认成功必须挂上通道，后台才会去领租约。
	if !claimHasChannel(h, fid) {
		// 没挂通道就停测，登录会一直等租约。
		t.Fatal("channel not held after confirm")
	}
}

// 假平台按认领两步回包；lost 为真时收到确认就断开不回包。
func claimWAN(t *testing.T, fid, pid uuid.UUID, lost bool) *httptest.Server {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 替身分认领询问和确认，其余路径一律拒绝。
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径分发认领两步，其余拒绝。
		switch r.URL.Path {
		// 认领询问回工厂身份，建厂码先不作废。
		case "/v1/channel/enroll":
			// 回工厂身份和初始超管，认领方才能落库。
			claimJSON(w, map[string]any{
				"factoryId": fid.String(), "name": "厂A", "factoryShortCode": "F01",
				"saPersonId": pid.String(), "saLogin": "sa", "saDisplay": "超管",
			})
		// 确认时按开关决定正常回包还是掐断连接。
		case "/v1/channel/claim":
			// 要模拟回包丢失就掐断连接，不回任何字节。
			if lost {
				// 接管底层连接，才能不回包直接断开。
				conn, _, err := http.NewResponseController(w).Hijack()
				// 接管不了就回错，测试能看出替身失灵。
				if err != nil {
					// 替身接管失败就回服务器错误。
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				// 直接断开，厂端只看到连接被掐。
				_ = conn.Close()
				return
			}
			// 回已启用，表示公钥已经收下。
			claimJSON(w, map[string]any{"factoryId": fid.String(), "status": "active", "revision": 0, "factoryShortCode": "F01"})
		// 其余路径拒绝，通道补送汇总等请求不必理会。
		default:
			// 这条路径拒绝，避免替身把别的请求当成功。
			http.NotFound(w, r)
		}
	}))
}

// 建只挂本测平台地址的枢纽；不调 StartChannel，免得扫到别的临时库。
func claimHub(t *testing.T, wanURL string, fid uuid.UUID) *Hub {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 连上测试库并建枢纽，连不上就停测。
	h, err := New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 给通道一个可取消的进程生命周期，测完先停重连。
	run, cancel := context.WithCancel(context.Background())
	// 占住通道表，再直接写入平台地址和生命周期。
	h.presenceMu.Lock()
	// 记下平台根地址，通道才肯拉起。
	h.wanURL = wanURL
	// 代理指向没人监听的端口，出站只会拨不上再退避。
	h.mqttURL = claimDeadMQTT(t)
	// 记下进程生命周期，重连跟随它取消。
	h.run = run
	// 地址记完就放开通道锁。
	h.presenceMu.Unlock()
	// 测完先停重连，再删厂库，最后关枢纽。
	t.Cleanup(func() {
		// 先停进程生命周期，通道不再重拨。
		cancel()
		// 删掉测试厂库，它会先停这家通道。
		_ = h.Drop(fid)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	return h
}

// 取一个刚放开、没人监听的本机端口当代理地址。
func claimDeadMQTT(t *testing.T) string {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 先占一个空闲端口，拿到地址后马上放开。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// 端口占不到就停测，没有地址可填。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("listen: %v", err)
	}
	// 记下这个端口地址，放开后就没人监听。
	addr := ln.Addr().String()
	// 放开端口，通道拨过去只会被拒绝。
	_ = ln.Close()
	return "tcp://" + addr
}

// 看枢纽里这家厂是否已挂上出站通道。
func claimHasChannel(h *Hub, fid uuid.UUID) bool {
	// 占住通道表，避免和后台重连交错读。
	h.presenceMu.Lock()
	// 离开时放开通道锁。
	defer h.presenceMu.Unlock()
	// 通道表里有这家就算已挂上。
	_, ok := h.presence[fid]
	return ok
}

// 替身按结构化正文回包。
func claimJSON(w http.ResponseWriter, v any) {
	// 标明是结构化正文，厂端才按字段解析。
	w.Header().Set("Content-Type", "application/json")
	// 写出正文，写失败由厂端按不可达处理。
	_ = json.NewEncoder(w).Encode(v)
}
