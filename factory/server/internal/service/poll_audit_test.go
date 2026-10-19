// 只读轮询接口成功不记允许，免得审计表无上限增长；被拒的读仍留拒绝。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 超管和普通人连读两轮名册、设备、凭证和目录，审计条数不变；越权读设备仍记拒绝。
func TestPollReadsSkipAllowAudit(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出超管令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一名没有管理许可的普通人员。
	if _, err := fac.CreatePerson(ctx, sa, "op", "普通人"); err != nil {
		// 建人员失败就停，后面没有普通人可验。
		t.Fatal(err)
	}
	// 普通人换成测试口令并拿到会话。
	op := mustAdoptPassword(t, ctx, fac, "op", "op-pass")
	// 记下轮询前的审计全量，事后核对没有新增。
	before := auditIDs(t, ctx, fac)
	// 像前端轮询那样连读两轮。
	for i := 0; i < 2; i++ {
		// 超管读全厂名册失败就停，轮询读不该出错。
		if _, err := fac.Catalog(ctx, sa); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("sa catalog #%d: %v", i+1, err)
		}
		// 超管读设备名录失败就停，轮询读不该出错。
		if _, err := fac.ListClients(ctx, sa); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("sa clients #%d: %v", i+1, err)
		}
		// 超管读运行凭证摘要失败就停，只读查询不该出错。
		if _, err := fac.ListRuntimeGrants(ctx, sa); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("sa runtime #%d: %v", i+1, err)
		}
		// 超管读工艺目录失败就停，轮询读不该出错。
		if _, err := fac.ListFS(ctx, sa, factory.KindProcess); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("sa fs #%d: %v", i+1, err)
		}
		// 通道读目录失败就停，只读查询不该出错。
		if _, err := fac.ListFSForChannel(ctx, factory.KindProcess); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("channel fs #%d: %v", i+1, err)
		}
		// 普通人只读自己那份名册，失败就停。
		if _, err := fac.Catalog(ctx, op); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("op catalog #%d: %v", i+1, err)
		}
		// 普通人读自己能看的目录，失败就停。
		if _, err := fac.ListFS(ctx, op, factory.KindProcess); err != nil {
			// 把第几轮的错误打出来并停住。
			t.Fatalf("op fs #%d: %v", i+1, err)
		}
	}
	// 读完后审计条数应不变，多了说明轮询读仍在记允许。
	if after := auditIDs(t, ctx, fac); len(after) != len(before) {
		// 条数变了就停，把前后条数打出来。
		t.Fatalf("audit rows %d -> %d after poll reads", len(before), len(after))
	}
	// 普通人读设备名录应被拒。
	if _, err := fac.ListClients(ctx, op); !errors.Is(err, domain.ErrForbidden) {
		// 没被拒就停，许可检查失效了。
		t.Fatalf("op clients: %v", err)
	}
	// 被拒的读仍要留拒绝，少了说明把拒绝也一并去掉了。
	if got := softwareAudit(t, ctx, fac, "list_clients", audit.Deny); len(got) != 1 {
		// 拒绝条数不对就停，把实际条数打出来。
		t.Fatalf("list_clients deny rows %d want 1", len(got))
	}
}
