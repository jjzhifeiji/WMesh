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

func TestClientBootstrap(t *testing.T) {
	person := id.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/bootstrap" || r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"personId":        person.String(),
			"activationToken": "act-once",
		})
	}))
	t.Cleanup(srv.Close)
	c := &factoryboot.Client{BaseURL: srv.URL, Token: "secret"}
	got, token, err := c.Bootstrap(context.Background(), id.New(), "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if got != person || token != "act-once" {
		t.Fatalf("got %s %s", got, token)
	}
	// 密码不对或厂端不可达都收成同一个业务错误，WAN 不落名录。
	bad := &factoryboot.Client{BaseURL: srv.URL, Token: "wrong"}
	if _, _, err := bad.Bootstrap(context.Background(), id.New(), "sa", "超管"); !errors.Is(err, domain.ErrFactoryBootstrap) {
		t.Fatalf("wrong token: %v", err)
	}
	srv.Close()
	if _, _, err := c.Bootstrap(context.Background(), id.New(), "sa", "超管"); !errors.Is(err, domain.ErrFactoryBootstrap) {
		t.Fatalf("unreachable: %v", err)
	}
}
