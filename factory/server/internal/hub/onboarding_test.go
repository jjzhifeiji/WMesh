package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/secret"
)

// 假平台：认领两步照常，另记录找回单被用了几次、带没带签名。
type resetWAN struct {
	srv      *httptest.Server // 假平台
	consumed atomic.Int32     // 找回单被成功用掉的次数
	unsigned atomic.Int32     // 没带厂钥签名的找回请求数
	deny     atomic.Bool      // 为真时拒绝用单，模拟没批准
}

// 起假平台，处理认领两步和找回两步。
func newResetWAN(t *testing.T, fid, pid uuid.UUID) *resetWAN {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备承接假平台状态。
	w := &resetWAN{}
	// 按路径分发。
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// 找回请求必须带厂钥签名。
		if (r.URL.Path == "/v1/channel/sa-reset" || r.URL.Path == "/v1/channel/sa-reset/consume") && r.Header.Get("X-WMesh-Sign") == "" {
			// 记下没签名的请求。
			w.unsigned.Add(1)
		}
		// 按路径回包。
		switch r.URL.Path {
		// 认领询问回工厂身份。
		case "/v1/channel/enroll":
			// 回工厂身份和初始超管。
			claimJSON(rw, map[string]any{"factoryId": fid.String(), "name": "厂A", "factoryShortCode": "F01", "saPersonId": pid.String(), "saLogin": "sa", "saDisplay": "超管"})
		// 确认认领。
		case "/v1/channel/claim":
			// 回已启用，表示公钥已经收下。
			claimJSON(rw, map[string]any{"factoryId": fid.String(), "status": "active", "revision": 0, "factoryShortCode": "F01"})
		// 申请找回。
		case "/v1/channel/sa-reset":
			// 回申请凭据。
			claimJSON(rw, map[string]any{"requestId": id.New().String(), "secret": "reset-secret", "verifyCode": "123456", "expiresAt": "2030-01-01T00:00:00Z"})
		// 用掉找回单。
		case "/v1/channel/sa-reset/consume":
			// 模拟没批准。
			if w.deny.Load() {
				// 按 WAN 的口径回无效。
				rw.WriteHeader(http.StatusUnauthorized)
				// 正文和 WAN 一样。
				_ = json.NewEncoder(rw).Encode(map[string]string{"error": "invalid enrollment"})
				return
			}
			// 记下用掉一次。
			w.consumed.Add(1)
			// 成功无正文。
			rw.WriteHeader(http.StatusNoContent)
		// 其余路径拒绝。
		default:
			// 别的请求不当成功。
			http.NotFound(rw, r)
		}
	}))
	// 测完关掉假平台。
	t.Cleanup(w.srv.Close)
	return w
}

// 找回：弱口令和没批准都不改密码；批准后改密码、旧登录失效、请求带厂钥签名。
func TestCompleteSAReset(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 本测工厂身份。
	fid := id.New()
	// 约定的初始超管身份。
	pid := id.New()
	// 起假平台。
	wan := newResetWAN(t, fid, pid)
	// 建枢纽。
	h := claimHub(t, wan.srv.URL, fid)
	// 先走认领，初始密码 old-pass-1。
	if _, err := h.Claim(ctx, wan.srv.URL, "code", "old-pass-1"); err != nil {
		// 认领失败后面没法测。
		t.Fatalf("claim: %v", err)
	}
	// 申请找回，必须带厂钥签名。
	ticket, err := h.RequestSAReset(ctx, wan.srv.URL, fid, "1.1.37")
	// 申请失败就停测。
	if err != nil || ticket.Secret != "reset-secret" {
		// 带上现场。
		t.Fatalf("request: %+v %v", ticket, err)
	}
	// 打开本厂服务，核对密码和会话。
	svc, err := h.Service(ctx, fid)
	// 打不开就停测。
	if err != nil {
		// 带上现场。
		t.Fatalf("service: %v", err)
	}
	// 读初始超管，下面要替它写会话。
	before, err := svc.Store().InitialPerson(ctx)
	// 读不到就停测。
	if err != nil {
		// 带上现场。
		t.Fatalf("initial: %v", err)
	}
	// 摆一条旧会话，后面验证它失效；登录要租约，这里直接写库。
	if _, err := svc.Store().CreateSession(ctx, before.ID, "old-token-hash", time.Now().Add(time.Hour)); err != nil {
		// 带上现场。
		t.Fatalf("session: %v", err)
	}
	// 弱口令要在用单之前拒绝。
	if _, err := h.CompleteSAReset(ctx, wan.srv.URL, fid, ticket.Secret, "short"); !errors.Is(err, domain.ErrWeakPassword) {
		// 没拦住就停测。
		t.Fatalf("weak: %v", err)
	}
	// 弱口令不能白耗找回单。
	if n := wan.consumed.Load(); n != 0 {
		// 带上现场。
		t.Fatalf("weak password consumed %d", n)
	}
	// 模拟云端没批准。
	wan.deny.Store(true)
	// 没批准不能改密码。
	if _, err := h.CompleteSAReset(ctx, wan.srv.URL, fid, ticket.Secret, "new-pass-2"); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 没拦住就停测。
		t.Fatalf("denied: %v", err)
	}
	// 读初始超管。
	p, err := svc.Store().InitialPerson(ctx)
	// 旧密码必须还有效。
	if err != nil || p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, "old-pass-1") {
		// 带上现场。
		t.Fatalf("password changed on denied: %v", err)
	}
	// 云端批准。
	wan.deny.Store(false)
	// 批准后设新密码。
	login, err := h.CompleteSAReset(ctx, wan.srv.URL, fid, ticket.Secret, "new-pass-2")
	// 必须成功并回登录名。
	if err != nil || login != "sa" {
		// 带上现场。
		t.Fatalf("complete: %q %v", login, err)
	}
	// 再读初始超管。
	p, err = svc.Store().InitialPerson(ctx)
	// 新密码生效，旧密码失效。
	if err != nil || p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, "new-pass-2") || secret.VerifyPassword(*p.PasswordHash, "old-pass-1") {
		// 带上现场。
		t.Fatalf("password not reset: %v", err)
	}
	// 旧登录必须失效。
	if _, err := svc.Store().SessionByTokenHash(ctx, "old-token-hash"); !errors.Is(err, domain.ErrNotFound) {
		// 没失效就停测。
		t.Fatal("old session survived reset")
	}
	// 找回请求必须都带签名。
	if n := wan.unsigned.Load(); n != 0 {
		// 带上现场。
		t.Fatalf("unsigned requests %d", n)
	}
}

// 没配平台地址时明确告诉现场连不上。
func TestOnboardingNeedsWAN(t *testing.T) {
	// 空枢纽就够，走不到库。
	h := &Hub{}
	// 没配地址不能申请。
	if _, err := h.ApplyOnboarding(context.Background(), "", "厂", "sa", "超管", ""); !errors.Is(err, domain.ErrWANUnreachable) {
		// 错误不对就停测。
		t.Fatalf("apply: %v", err)
	}
	// 没配地址不能查进度。
	if _, err := h.OnboardingStatus(context.Background(), "", "x"); !errors.Is(err, domain.ErrWANUnreachable) {
		// 错误不对就停测。
		t.Fatalf("status: %v", err)
	}
}
