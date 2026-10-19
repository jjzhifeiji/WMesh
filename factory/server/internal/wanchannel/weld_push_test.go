package wanchannel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
