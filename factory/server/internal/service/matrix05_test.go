// 阶段3第5圈：厂内侧厂级/个人级制作与个人保密（5.1～7.3）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 验收厂级和个人级制作，以及个人正文的保密。
func testAssetAuthorship(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facA.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	// 建厂并签发超管激活码，失败则没有厂可测。
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sa-b-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	peB := mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("personal-secret-body")
	// 新建组织节点，失败说明名称或上级不合法。
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shop, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopB, err := facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 建人并授好角色，失败说明后面没有账号可用。
	peShop := mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := facA.Assign(ctx, saA, peShop.acc.ID, shop.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 先留出厂级工艺，建好后给作者边界用例用。
	var facProc factory.Asset
	// 验收条目 5.1，断言不过表示这一条没过。
	run("5.1", func(t *testing.T) {
		// 先备好错误位，失败分支再写入具体原因。
		var err error
		// 新建一份厂级工艺，失败说明起草入口坏了。
		facProc, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "焊接", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 可复制标记应符合预期，不对说明开关没写上。
		if facProc.Level != factory.AssetLevelFactory || !facProc.Copyable || facProc.FactoryID != seed.ID ||
			facProc.Status != factory.AssetDraft || facProc.Content != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", facProc)
		}
	})
	// 验收条目 5.2，断言不过表示这一条没过。
	run("5.2", func(t *testing.T) {
		// 建工艺失败就停，否则后面没有可靠结果。
		if _, err := facA.CreateFactoryProcess(ctx, op.tok, direct, "焊接", body); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("op: %v", err)
		}
		// 建工艺失败就停，否则后面没有可靠结果。
		if _, err := facA.CreateFactoryProcess(ctx, saA, direct, "焊接", body); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("sa: %v", err)
		}
		// 建工艺应因未登录被拒绝，放行说明没拦住。
		if _, err := facA.CreateFactoryProcess(ctx, peB.tok, direct, "焊接", body); !errors.Is(err, domain.ErrUnauthorized) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("pe-b: %v", err)
		}
		// 读正文失败就停，否则后面没有可靠结果。
		if _, err := facA.ReadAssetContent(ctx, op.tok, facProc.ID); err != nil {
			// 状态还停在草稿，说明保存没有落成可用。
			t.Fatalf("op draft content: %v", err)
		}
	})
	// 验收条目 5.3，断言不过表示这一条没过。
	run("5.3", func(t *testing.T) {
		// 新建一份厂级工艺，失败说明起草入口坏了。
		got, err := facA.CreateFactoryProcess(ctx, peShop.tok, factory.WorkContext{OrgUnitID: &shop.ID}, "车间工艺", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 路径上的节点和名称应与当时组织一致，漂移即失败。
		if got.OrgUnitID == nil || *got.OrgUnitID != shop.ID || len(got.OrgPath) == 0 || got.OrgPath[len(got.OrgPath)-1].ID != shop.ID {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", got)
		}
	})
	// 验收条目 5.4，断言不过表示这一条没过。
	run("5.4", func(t *testing.T) {
		// 建工艺失败就停，否则后面没有可靠结果。
		if _, err := facA.CreateFactoryProcess(ctx, peShop.tok, direct, "直属", body); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("direct: %v", err)
		}
		// 建工艺应因上下文非法被拒绝，放行说明没拦住。
		if _, err := facA.CreateFactoryProcess(ctx, peShop.tok, factory.WorkContext{OrgUnitID: &shopB.ID}, "他车间", body); !errors.Is(err, domain.ErrWorkContext) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("shopB: %v", err)
		}
	})

	// 先留出个人资产，建好后用来核对归属。
	var personal factory.Asset
	// 验收条目 6.1，断言不过表示这一条没过。
	run("6.1", func(t *testing.T) {
		// 先备好错误位，失败分支再写入具体原因。
		var err error
		// 新建一份个人工艺，失败说明个人入口被拒。
		personal, err = facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		// 建个人工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建个人工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 事实应落在预期组织上，串了车间说明上下文偏了。
		if personal.Level != factory.AssetLevelPersonal || personal.CreatorID != pe.acc.ID ||
			personal.FactoryID != seed.ID || !personal.Copyable || personal.Status != factory.AssetDraft ||
			personal.Content != nil || personal.OrgUnitID != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", personal)
		}
	})
	// 验收条目 6.2，断言不过表示这一条没过。
	run("6.2", func(t *testing.T) {
		// 建个人工艺失败就停，否则后面没有可靠结果。
		if _, err := facA.CreatePersonalProcess(ctx, op.tok, direct, "操作员个人", body); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("got %v", err)
		}
	})
	// 验收条目 6.3，断言不过表示这一条没过。
	run("6.3", func(t *testing.T) {
		// 建个人工艺应因上下文非法被拒绝，放行说明没拦住。
		if _, err := facA.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{}, "无上下文", body); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	// 验收条目 7.1，断言不过表示这一条没过。
	run("7.1", func(t *testing.T) {
		// 读回资产正文，失败说明无权读或稿已经没了。
		got, err := facA.ReadAssetContent(ctx, pe.tok, personal.ID)
		// 读正文失败或正文不同就停，说明没达预期。
		if err != nil || !bytes.Equal(got, body) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%q %v", got, err)
		}
		// 拉出审计流水，失败则无法核对有没有记账。
		rows, err := facA.ListAudit(ctx)
		// 拉审计失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把拉审计的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 审计里不应出现口令或正文，出现了就算泄密。
		if audit.ContainsAny(audit.Dump(rows), string(body), "pe-pass") {
			// 结果里出现了不该有的敏感词，说明已经泄密。
			t.Fatalf("body leaked")
		}
	})
	// 验收条目 7.2，断言不过表示这一条没过。
	run("7.2", func(t *testing.T) {
		// 读回资产正文，失败说明无权读或稿已经没了。
		got, err := facA.ReadAssetContent(ctx, saA, personal.ID)
		// 读正文失败或正文不同就停，说明没达预期。
		if err != nil || !bytes.Equal(got, body) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("sa: %q %v", got, err)
		}
		// 读正文应因越权被拒绝，放行说明没拦住。
		if _, err := facA.ReadAssetContent(ctx, pe2.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("pe2: %v", err)
		}
		// 读正文应因越权被拒绝，放行说明没拦住。
		if _, err := facA.ReadAssetContent(ctx, op.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("op: %v", err)
		}
		// 读正文应因未登录被拒绝，放行说明没拦住。
		if _, err := facA.ReadAssetContent(ctx, peB.tok, personal.ID); !errors.Is(err, domain.ErrUnauthorized) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("pe-b: %v", err)
		}
	})
	// 验收条目 7.3，断言不过表示这一条没过。
	run("7.3", func(t *testing.T) {
		// 读取资产台账，失败说明无权看或已经删除。
		got, err := facA.GetAsset(ctx, saA, personal.ID)
		// 读台账失败或状态不对就停，说明没达预期。
		if err != nil || got.Content != nil || got.Level != factory.AssetLevelPersonal ||
			got.CreatorID != pe.acc.ID || got.Status != factory.AssetDraft {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", got, err)
		}
		// 读台账失败就停，否则后面没有可靠结果。
		if _, err := facA.GetAsset(ctx, pe2.tok, personal.ID); err != nil {
			// 把读台账的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
	})
}
