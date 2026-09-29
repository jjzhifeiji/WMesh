// 用 HTTP 通知厂端写入初始超管，不连厂库。
package factoryboot_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"wmesh/global/internal/factoryboot"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// 口令正确时应带回账号和激活码，错口令或厂端不可达都算引导失败。
func TestClientBootstrap(t *testing.T) {
	// 造一个账号身份，回执要对得上。
	person := id.New()
	// 替身只受理引导路径，口令不对就拒绝。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路径或口令不对就拒绝，避免误建超管。
		if r.URL.Path != "/internal/bootstrap" || r.Header.Get("Authorization") != "Bearer secret" {
			// 口令不对回未授权，不能当成已经建成。
			w.WriteHeader(http.StatusUnauthorized)
			// 带上未授权原因，调用方要收成引导失败。
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		// 口令对了回已创建。
		w.WriteHeader(http.StatusCreated)
		// 带回账号和一次性激活码，且不写入平台库。
		_ = json.NewEncoder(w).Encode(map[string]string{
			"personId":        person.String(),
			"activationToken": "act-once",
		})
	}))
	// 测完关掉替身，避免端口占住。
	t.Cleanup(srv.Close)
	// 用替身地址和共享口令去引导。
	c := &factoryboot.Client{BaseURL: srv.URL, Token: "secret"}
	// 发起引导，失败就没有激活码。
	got, token, err := c.Bootstrap(context.Background(), id.New(), "sa", "超管")
	// 引导失败就停测，避免把空账号当成通过。
	if err != nil {
		// 准备失败就停测，后面的比对没有意义。
		t.Fatal(err)
	}
	// 账号或激活码对不上就停测。
	if got != person || token != "act-once" {
		// 回执和替身不一致就停测。
		t.Fatalf("got %s %s", got, token)
	}
	// 密码不对或厂端不可达都收成同一个业务错误，WAN 不落名录。
	bad := &factoryboot.Client{BaseURL: srv.URL, Token: "wrong"}
	// 口令错误却没收成引导失败就停测。
	if _, _, err := bad.Bootstrap(context.Background(), id.New(), "sa", "超管"); !errors.Is(err, domain.ErrFactoryBootstrap) {
		// 错口令必须收成引导失败，别的错误不能放过。
		t.Fatalf("wrong token: %v", err)
	}
	// 先关掉替身，再验证不可达也是同一错误。
	srv.Close()
	// 厂端不可达却没收成引导失败就停测。
	if _, _, err := c.Bootstrap(context.Background(), id.New(), "sa", "超管"); !errors.Is(err, domain.ErrFactoryBootstrap) {
		// 连不上也必须收成引导失败。
		t.Fatalf("unreachable: %v", err)
	}
}
