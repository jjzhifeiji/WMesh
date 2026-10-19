package wanchannel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// 平台回 5xx 时焊汇总上送收成不可达，后台才会退避重试；4xx 仍是业务拒绝。
func TestPostWeldSummariesMapsStatus(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 生成本厂签发钥，上送时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 逐个状态码看收成什么错误。
	for _, tc := range []struct {
		code int    // 替身回的状态码
		body string // 替身回的正文，空则不带
		want error  // 应收成的错误
	}{
		{http.StatusServiceUnavailable, "", domain.ErrWANUnreachable},
		{http.StatusInternalServerError, `{"error":"invalid name"}`, domain.ErrWANUnreachable},
		{http.StatusForbidden, "", domain.ErrForbidden},
		{http.StatusUnauthorized, `{"error":"unauthorized"}`, domain.ErrWANSignatureRejected},
		{http.StatusUnauthorized, "", domain.ErrWANSignatureRejected},
	} {
		// 替身只回这一个状态码和正文。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 先写状态码，模拟平台出错或拒收。
			w.WriteHeader(tc.code)
			// 5xx 带上英文原因，确认不按原因收成业务错误。
			_, _ = w.Write([]byte(tc.body))
		}))
		// 上送一行汇总，平台按状态码回。
		err := PostWeldSummaries(context.Background(), srv.URL, uuid.New(), priv, []WeldSummaryRow{
			{Day: "2026-09-18", ProjectName: "工程A", WeldKind: "single", RunCount: 1, LengthMM: 1, DurationSec: 1},
		})
		// 用完就关掉替身，避免端口一直占住。
		srv.Close()
		// 收成的错误不对就停测。
		if !errors.Is(err, tc.want) {
			// 带上状态码和实际错误便于定位。
			t.Fatalf("status %d: got %v want %v", tc.code, err, tc.want)
		}
	}
}

// 本机钟偏八小时、还没校正就上送：WAN 回 401 收成签名被拒，回包 Date 校正时差后再推就过。
func TestPostWeldSummariesSkewRejectThenPass(t *testing.T) {
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 假平台比本机快八小时，模拟进程刚起还没探过钟。
	ahead := 8 * time.Hour
	// 记下被拒几次、收下几次。
	var rejected, accepted atomic.Int32
	// 替身按假时间验签名时刻，超出时间窗就回 401，和 WAN 一样。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 取假平台的当前时间，回包和验签都按它。
		now := time.Now().Add(ahead)
		// 每个回包都带假时间的 Date，401 也带。
		w.Header().Set("Date", now.UTC().Format(http.TimeFormat))
		// 读签名时刻，按假时间验时间窗。
		unix, err := strconv.ParseInt(r.Header.Get("X-WMesh-Time"), 10, 64)
		// 超出前后三百秒就拒绝，回 WAN 的英文原因。
		if err != nil || absSec(unix-now.Unix()) > 300 {
			// 记一次拒绝。
			rejected.Add(1)
			// 先写未授权状态。
			w.WriteHeader(http.StatusUnauthorized)
			// 正文和 WAN 拒绝过期签名时一样。
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		// 记一次收下。
		accepted.Add(1)
		// 收下整表，回 204 没有回执。
		w.WriteHeader(http.StatusNoContent)
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 生成本厂签发钥，上送时用来签名。
	_, priv, err := nodekey.Generate()
	// 签发钥生不出来就停测，后面无法签名。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	// 造一个工厂身份，两次上送共用。
	fid := uuid.New()
	// 要上送的一行汇总。
	rows := []WeldSummaryRow{{Day: "2026-09-18", ProjectName: "工程A", WeldKind: "single", RunCount: 1, LengthMM: 1, DurationSec: 1}}
	// 第一次按本机时间签，应被拒并收成签名被拒。
	if err := PostWeldSummaries(context.Background(), srv.URL, fid, priv, rows); !errors.Is(err, domain.ErrWANSignatureRejected) {
		// 收成的错误不对就停测，后台上送靠这个类型补推。
		t.Fatalf("first push: %v", err)
	}
	// 被拒的回包应已把时差记成约八小时。
	assertSkewNear(t, 28800)
	// 再推一次，这次按校正后的时间签，应被收下。
	if err := PostWeldSummaries(context.Background(), srv.URL, fid, priv, rows); err != nil {
		// 校正后仍推不上就停测。
		t.Fatalf("second push: %v", err)
	}
	// 只该被拒一次、收下一次。
	if rejected.Load() != 1 || accepted.Load() != 1 {
		// 次数不对就停测，并带上两边计数。
		t.Fatalf("rejected %d accepted %d", rejected.Load(), accepted.Load())
	}
}
