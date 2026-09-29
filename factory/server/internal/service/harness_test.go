// 验收厂内应用服务，不连 WAN 库。
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 建厂夹具带回的厂编号、超管和激活码。
type Seeded struct {
	ID uuid.UUID // 本厂稳定身份
	// 首个超管的账号编号，用来核对是谁建的厂。
	SuperAdminID    uuid.UUID
	ActivationToken string // 一次性 8 位激活码，只给夹具
}

// 隔离厂库夹具，只连本厂测试库。
type Harness struct {
	// 当前这条测试，失败时从这里报出去。
	t *testing.T
	// 管理库连接，用来给每座厂另开库。
	admin *gorm.DB
	// 已建厂编号到厂服务的对照。
	factories map[uuid.UUID]*service.Service
}

// 拉起每座厂独立的测试库，库起不来就没法验收。
func New(t *testing.T) *Harness {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 交回夹具，库没打开则这座厂测不起来。
	return &Harness{t: t, admin: testpg.Open(t), factories: map[uuid.UUID]*service.Service{}}
}

// 建一座厂并写下超管，失败则这条用例没有厂。
func (h *Harness) Provision(ctx context.Context, saLogin, saDisplay string) (Seeded, *service.Service, error) {
	// 新取一个编号，撞号会让两条记录分不清。
	facID := id.New()
	// 另开一座空库，失败说明管理库不允许建库。
	_, dsn := testpg.CreateDB(h.t, h.admin, "wmesh_fac")
	// 迁好库并装上厂服务，失败说明库还不能用。
	svc := service.NewService(store.Open(testpg.OpenMigrated(h.t, dsn), facID))
	// 夹具不连 WAN，自签一把租约才能写厂库正文。
	if err := svc.Store().GrantLocalLease(ctx); err != nil {
		return Seeded{}, nil, err
	}
	// 按序号编工厂短码，失败说明序号越界。
	short, err := assetcode.FormatFactory(int64(len(h.factories) + 1))
	// 编短码失败就把错误交回，避免留下半成品。
	if err != nil {
		return Seeded{}, nil, err
	}
	// 写工厂短码失败就把错误交回，避免留下半成品。
	if err := svc.Store().PutFactoryShortCode(ctx, short); err != nil {
		return Seeded{}, nil, err
	}
	// 写入首个超管，失败说明厂库还不能开张。
	acc, token, err := svc.BootstrapInitial(ctx, saLogin, saDisplay)
	// 写首个超管失败就把错误交回，避免留下半成品。
	if err != nil {
		return Seeded{}, nil, err
	}
	// 记下这座厂的服务，后面按编号还要取回。
	h.factories[facID] = svc
	return Seeded{ID: facID, SuperAdminID: acc.ID, ActivationToken: token}, svc, nil
}

// 按编号取回已建的厂服务，没有则说明没建过。
func (h *Harness) Factory(id uuid.UUID) *service.Service {
	// 按编号取出厂服务，取不到说明这座厂没建。
	svc, ok := h.factories[id]
	// 取不到就停，说明上一笔没有建起来。
	if !ok {
		// 断言没通过就停，结果和这条用例的预期不一致。
		h.t.Fatalf("factory %s not provisioned", id)
	}
	return svc
}

// 按登录名算出默认口令，第一次登录要用它。
func personPass(login string) string {
	// 交回该登录名的默认口令，供第一次登录使用。
	return secret.DefaultPersonPassword(login)
}

// mustAdoptPassword 用厂内默认密码登录后改成测试用密码，返回新会话。
func mustAdoptPassword(t *testing.T, ctx context.Context, fac *service.Service, login, pass string) string {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err := fac.Login(ctx, login, personPass(login))
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("login default %s: %v", login, err)
	}
	// 已经是默认口令就直接交回，不必再改一次密。
	if pass == personPass(login) {
		return tok
	}
	// 改口令失败就停，否则后面没有可靠结果。
	if err := fac.ChangePassword(ctx, tok, pass); err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("set pass %s: %v", login, err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err = fac.Login(ctx, login, pass)
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("login new %s: %v", login, err)
	}
	return tok
}
