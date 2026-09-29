// 平板保存即为可用；管理后台新建仍是草稿。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 平板保存后应为可用，管理后台新建则仍是草稿。
func TestPadSaveLandsAvailable(t *testing.T) {
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
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte(`{"name":"mine","current":170}`)

	// 新建一份个人工艺，失败说明个人入口被拒。
	draft, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "后台草稿", body)
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 状态应符合这一步的预期，停在旧状态说明没流转。
	if draft.Status != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("admin create: %+v", draft)
	}

	// 新取一个编号，撞号会让两条记录分不清。
	localID := id.New()
	// 在平板上新建个人稿，失败说明会话或上下文不对。
	got, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板工艺", body, localID, "GY-C0008-000001", nil)
	// 平板建稿失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把平板建稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作者应是本人，串到别人说明归属没保住。
	if got.ID != localID || got.Status != factory.AssetAvailable || got.Level != factory.AssetLevelPersonal ||
		got.CreatorID != op.acc.ID || got.Code != "GY-C0008-000001" || !got.Copyable || got.Content != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad create: %+v", got)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	opened, err := fac.ReadAssetContent(ctx, op.tok, got.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(opened, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("read %q %v", opened, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	asSA, err := fac.ReadAssetContent(ctx, sa, got.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(asSA, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa read %q %v", asSA, err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	nextBody := []byte(`{"name":"mine","current":180}`)
	// 改写资产正文，失败说明修订冲突或越权。
	edited, err := fac.UpdateAssetContent(ctx, sa, got.ID, got.Revision, nextBody)
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 换成改过的稿，后面要核对修订已经前进。
	got = edited
	// 换成新正文，读回不一致说明没写进去。
	body = nextBody
	// 读正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ReadAssetContent(ctx, pe.tok, got.ID); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pe read: %v", err)
	}

	// 平板建稿应因编号冲突被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "重号", body, id.New(), "GY-C0008-000001", nil); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("dup code: %v", err)
	}

	// 把编号打成文本，方便和路径或正文比对。
	projBody := []byte(`[{"processId":"` + got.ID.String() + `"}]`)
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "厂工程", []byte(`[]`), nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 装上这条依赖，修订或摘要错了工程会对不上。
	dep := factory.AssetDep{ID: got.ID, Revision: got.Revision, Digest: got.Digest}
	// 改写工程依赖，失败说明工艺还不齐。
	pinned, err := fac.SetProjectDeps(ctx, op.tok, proj.ID, proj.Revision, []factory.AssetDep{dep})
	// 改依赖失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改依赖的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改写资产正文，失败说明修订冲突或越权。
	updated, err := fac.UpdateAssetContent(ctx, op.tok, pinned.ID, pinned.Revision, projBody)
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if updated.Revision <= pinned.Revision || !bytes.Equal(updated.Digest, digest.Sum(projBody)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("update %+v", updated)
	}
	// 平板建稿应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "坏工程", []byte(`{"processPath":""}`), uuid.Nil, "", nil); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("processPath: %v", err)
	}
	// 平板建稿应因找不到被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, "other", "坏", body, uuid.Nil, "", nil); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("kind: %v", err)
	}
}
