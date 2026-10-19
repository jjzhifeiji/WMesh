package wanchannel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

// 平台一次回包：状态码、正文和应收成的业务错误。
type wanErrCase struct {
	code int    // 平台回的状态码
	body string // 平台回的正文，空则不带
	want error  // 应收成的业务错误
}

// WAN 实际会回的错误，认得的英文原因优先，认不出再按状态码收。
var wanErrCases = []wanErrCase{
	{http.StatusNotFound, `{"error":"not found"}`, domain.ErrNotFound},
	{http.StatusNotFound, "", domain.ErrNotFound},
	{http.StatusForbidden, `{"error":"forbidden"}`, domain.ErrForbidden},
	{http.StatusForbidden, `{"error":"factory is disabled"}`, domain.ErrFactoryDisabled},
	{http.StatusForbidden, `{"error":"factory is retired"}`, domain.ErrFactoryRetired},
	{http.StatusUnauthorized, `{"error":"unauthorized"}`, domain.ErrWANSignatureRejected},
	{http.StatusUnauthorized, "", domain.ErrWANSignatureRejected},
	{http.StatusUnauthorized, `{"error":"invalid enrollment"}`, domain.ErrInvalidEnrollment},
	{http.StatusTooManyRequests, `{"error":"too many requests"}`, domain.ErrTooManyRequests},
	{http.StatusTooManyRequests, "", domain.ErrTooManyRequests},
	{http.StatusInternalServerError, `{"error":"internal error"}`, domain.ErrWANUnreachable},
	{http.StatusBadGateway, "<html>bad gateway</html>", domain.ErrWANUnreachable},
	{http.StatusServiceUnavailable, "", domain.ErrWANUnreachable},
	{http.StatusConflict, `{"error":"factory public key already registered"}`, domain.ErrSigningKeyExists},
	{http.StatusBadRequest, `{"error":"invalid key material"}`, domain.ErrInvalidKey},
}

// 各状态码配 WAN 实际英文原因时，都要收成对应业务错误，不能变成普通错误。
func TestMapHTTPStatus(t *testing.T) {
	// 逐条状态码和正文看收成什么错误。
	for _, tc := range wanErrCases {
		// 按状态码和正文收一次错误。
		got := mapHTTPStatus(tc.code, []byte(tc.body))
		// 没收成对应业务错误就停测。
		if !errors.Is(got, tc.want) {
			// 带上状态码、正文和实际错误便于定位。
			t.Fatalf("status %d body %q: got %v want %v", tc.code, tc.body, got, tc.want)
		}
	}
}

// 认不出的 4xx 原因带码和原文交回，不冒充任何已知业务错误。
func TestMapHTTPStatusKeepsUnknownReason(t *testing.T) {
	// 平台回坏请求，原因本侧不认得。
	got := mapHTTPStatus(http.StatusBadRequest, []byte(`{"error":"invalid id"}`))
	// 没带上状态码或原文就停测，运维对照不了平台。
	if got == nil || !strings.Contains(got.Error(), "400") || !strings.Contains(got.Error(), "invalid id") {
		// 带上实际错误便于定位。
		t.Fatalf("unknown reason: %v", got)
	}
	// 逐个比对按状态码收的几种业务错误，都不该命中。
	for _, known := range []error{domain.ErrNotFound, domain.ErrForbidden, domain.ErrWANUnreachable, domain.ErrWANSignatureRejected, domain.ErrTooManyRequests} {
		// 冒充了已知业务错误就停测，调用方会走错分路。
		if errors.Is(got, known) {
			// 带上命中的错误便于定位。
			t.Fatalf("unknown reason mapped to %v", known)
		}
	}
}

// 拉包和认领走真回包时也按同一口径收，同步软件不再显示内部错误。
func TestWANErrorsReachCallers(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 时差是包级状态，测前测后都清零。
	resetClock(t)
	// 造一个工厂身份，拉包签名要用。
	fid := uuid.New()
	// 生成本厂签发钥，拉包时用来签名。
	priv := testPriv(t)
	// 逐条用例起替身，看两条路收成什么错误。
	for _, tc := range wanErrCases {
		// 替身只回这一个状态码和正文。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 先写状态码，模拟平台拒绝或出错。
			w.WriteHeader(tc.code)
			// 按用例带上或不带英文原因。
			_, _ = w.Write([]byte(tc.body))
		}))
		// 按版本拉包，平台回错误。
		_, pullErr := PullSoftwareBody(context.Background(), srv.URL, fid, priv, "client_apk", 7, nil)
		// 用建厂码换身份，平台回同样的错误。
		_, enrollErr := Enroll(context.Background(), srv.URL, "code")
		// 用完就关掉替身，避免端口一直占住。
		srv.Close()
		// 任一路没收成对应业务错误就停测。
		if !errors.Is(pullErr, tc.want) || !errors.Is(enrollErr, tc.want) {
			// 带上状态码、正文和两路实际错误便于定位。
			t.Fatalf("status %d body %q: pull %v enroll %v want %v", tc.code, tc.body, pullErr, enrollErr, tc.want)
		}
	}
}
