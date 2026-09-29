// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 作业类型要合法，工程和工艺类型不一致则拒绝。
func TestFactoryWeldKind(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	def, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "默认工艺", []byte(`{"name":"p"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if def.WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("default %s", def.WeldKind)
	}
	// 建工艺应因焊类非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 非法输入被收下了，说明校验没有生效。
		t.Fatalf("invalid: %v", err)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	multiP, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "多层工艺", []byte(`{"name":"m"}`), "multi")
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if multiP.WeldKind != store.WeldKindMultilayer {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	// 复制出一份工艺，失败说明源稿不可复制或越权。
	copied, err := fac.CopyProcess(ctx, pe.tok, multiP.ID, "多层副本")
	// 复制工艺失败或焊类不对就停，说明没达预期。
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("copy %+v %v", copied, err)
	}

	// 发布当前修订，失败说明状态不允许发布。
	pub, err := fac.PublishAsset(ctx, pe.tok, multiP.ID, multiP.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 建工程应因类型不一致被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程钉多层", singleBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 建工程应因类型不一致被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 两边结果对不上，说明匹配或引用写偏了。
		t.Fatalf("content mismatch: %v", err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	ok, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "多层工程", multiBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if ok.WeldKind != store.WeldKindMultilayer {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project %s", ok.WeldKind)
	}

	// 修改作业类型，失败说明类型非法或冲突。
	changed, err := fac.SetAssetWeldKind(ctx, pe.tok, def.ID, def.Revision, store.WeldKindTBar)
	// 改焊类失败或焊类不对就停，说明没达预期。
	if err != nil || changed.WeldKind != store.WeldKindTBar {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("set kind %+v %v", changed, err)
	}
	// 修改作业类型，失败说明类型非法或冲突。
	switched, err := fac.SetAssetWeldKind(ctx, pe.tok, ok.ID, ok.Revision, store.WeldKindSingle)
	// 改焊类失败或焊类不对就停，说明没达预期。
	if err != nil || switched.WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project kind %+v %v", switched, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	cleared, err := fac.ReadAssetContent(ctx, pe.tok, switched.ID)
	// 打成文本失败或结果不符就停，说明没达预期。
	if err != nil || string(cleared) != "[]" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project body %s %v", cleared, err)
	}
	// 改焊类应因焊类非法被拒绝，放行说明没拦住。
	if _, err := fac.SetAssetWeldKind(ctx, pe.tok, changed.ID, changed.Revision, "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bad kind: %v", err)
	}
}
