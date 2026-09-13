// 验收厂内应用服务，不连 WAN 库。
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

type Seeded struct {
	ID              uuid.UUID // 本厂稳定身份
	SuperAdminID    uuid.UUID
	ActivationToken string // 一次性 8 位激活码，只给夹具
}

type Harness struct {
	t         *testing.T
	admin     *gorm.DB
	factories map[uuid.UUID]*service.Service
}

func New(t *testing.T) *Harness {
	t.Helper()
	return &Harness{t: t, admin: testpg.Open(t), factories: map[uuid.UUID]*service.Service{}}
}

func (h *Harness) Provision(ctx context.Context, saLogin, saDisplay string) (Seeded, *service.Service, error) {
	facID := id.New()
	_, dsn := testpg.CreateDB(h.t, h.admin, "wmesh_fac")
	svc := service.NewService(store.Open(testpg.OpenMigrated(h.t, dsn), facID))
	// 夹具不连 WAN，自签一把租约才能写厂库正文。
	if err := svc.Store().GrantLocalLease(ctx); err != nil {
		return Seeded{}, nil, err
	}
	acc, token, err := svc.BootstrapInitial(ctx, saLogin, saDisplay)
	if err != nil {
		return Seeded{}, nil, err
	}
	h.factories[facID] = svc
	return Seeded{ID: facID, SuperAdminID: acc.ID, ActivationToken: token}, svc, nil
}

func (h *Harness) Factory(id uuid.UUID) *service.Service {
	svc, ok := h.factories[id]
	if !ok {
		h.t.Fatalf("factory %s not provisioned", id)
	}
	return svc
}

func personPass(login string) string {
	return secret.DefaultPersonPassword(login)
}

// mustAdoptPassword 用厂内默认密码登录后改成测试用密码，返回新会话。
func mustAdoptPassword(t *testing.T, ctx context.Context, fac *service.Service, login, pass string) string {
	t.Helper()
	tok, err := fac.Login(ctx, login, personPass(login))
	if err != nil {
		t.Fatalf("login default %s: %v", login, err)
	}
	if pass == personPass(login) {
		return tok
	}
	if err := fac.ChangePassword(ctx, tok, pass); err != nil {
		t.Fatalf("set pass %s: %v", login, err)
	}
	tok, err = fac.Login(ctx, login, pass)
	if err != nil {
		t.Fatalf("login new %s: %v", login, err)
	}
	return tok
}
