// 可复制工艺另存为新草稿，原件不动。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 可复制工艺另存为新草稿，原件内容不得被改动。
func TestCopyProcess(t *testing.T) {
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
	other := mustCreateRole(t, ctx, fac, sa, "pe-b", "pe-b-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("copy-src-body")

	// 新建一份厂级工艺，失败说明起草入口坏了。
	src, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "源工艺", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制出一份工艺，失败说明源稿不可复制或越权。
	got, err := fac.CopyProcess(ctx, pe.tok, src.ID, "新工艺")
	// 复制工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把复制工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 可复制标记应符合预期，不对说明开关没写上。
	if got.ID == src.ID || got.Revision != 1 || got.Status != factory.AssetDraft ||
		got.Level != factory.AssetLevelFactory || got.Name != "新工艺" || !got.Copyable || got.SourceID != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", got)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	still, err := fac.GetAsset(ctx, pe.tok, src.ID)
	// 读台账失败或修订不对就停，说明没达预期。
	if err != nil || still.Revision != src.Revision || still.Name != src.Name {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("src %+v %v", still, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	copied, err := fac.ReadAssetContent(ctx, pe.tok, got.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(copied, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%q %v", copied, err)
	}
	// 复制工艺应因名称非法被拒绝，放行说明没拦住。
	if _, err := fac.CopyProcess(ctx, pe.tok, src.ID, "  "); !errors.Is(err, domain.ErrInvalidName) {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("empty name: %v", err)
	}

	// 切换可否复制，失败说明越权或状态不允许。
	tight, err := fac.SetAssetCopyable(ctx, pe.tok, src.ID, src.Revision, false)
	// 改可复制失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改可复制的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制工艺应因不可复制被拒绝，放行说明没拦住。
	if _, err := fac.CopyProcess(ctx, pe.tok, src.ID, "应拒绝"); !errors.Is(err, domain.ErrAssetNotCopyable) {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("not copyable: %v", err)
	}

	// 发布当前修订，失败说明状态不允许发布。
	pub, err := fac.PublishAsset(ctx, pe.tok, tight.ID, tight.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 停用这份资产，失败说明仍被引用或越权。
	off, err := fac.DisableAsset(ctx, pe.tok, pub.ID, pub.Revision)
	// 停用资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把停用资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制工艺应因资产不可用被拒绝，放行说明没拦住。
	if _, err := fac.CopyProcess(ctx, pe.tok, off.ID, "停用"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disabled: %v", err)
	}

	// 新建一份个人工艺，失败说明个人入口被拒。
	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人源", body)
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制工艺应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CopyProcess(ctx, other.tok, mine.ID, "偷复制"); !errors.Is(err, domain.ErrForbidden) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("other personal: %v", err)
	}
	// 复制出一份工艺，失败说明源稿不可复制或越权。
	pers, err := fac.CopyProcess(ctx, pe.tok, mine.ID, "个人副本")
	// 复制工艺失败或级别不对就停，说明没达预期。
	if err != nil || pers.Level != factory.AssetLevelPersonal || pers.ID == mine.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", pers, err)
	}

	// 准备这段正文字节，读回对不上说明没写进去。
	platBody := []byte("plat-copy")
	// 组装一条组包成员，字段错了后面会对不上。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台可复制",
		Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: platBody, Digest: digest.Sum(platBody),
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := seed.ID
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制出一份工艺，失败说明源稿不可复制或越权。
	fromPlat, err := fac.CopyProcess(ctx, pe.tok, m.ID, "从平台复制")
	// 复制工艺失败或状态不对就停，说明没达预期。
	if err != nil || fromPlat.Level != factory.AssetLevelFactory || fromPlat.Status != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", fromPlat, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	gotPlat, err := fac.ReadAssetContent(ctx, pe.tok, fromPlat.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(gotPlat, platBody) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%q %v", gotPlat, err)
	}

	// 准备这段正文字节，读回对不上说明没写进去。
	secretBody := []byte("plat-secret")
	// 再装一条带密文的成员，用来验证不可复制。
	secret := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台保密",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: secretBody, Digest: digest.Sum(secretBody),
	}
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, secret, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 复制工艺应因不可复制被拒绝，放行说明没拦住。
	if _, err := fac.CopyProcess(ctx, pe.tok, secret.ID, "偷看"); !errors.Is(err, domain.ErrAssetNotCopyable) {
		// 审计或汇聚里出现口令或正文，说明没隔离开。
		t.Fatalf("secret copy %v", err)
	}
}

// 关掉可复制后应拒绝另存，仍能复制说明开关无效。
func TestCreateProcessCopyable(t *testing.T) {
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
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("create-copyable-body")

	// 新建一份工艺，失败说明名称或类型不合法。
	tight, err := fac.CreateProcess(ctx, pe.tok, direct, factory.AssetLevelFactory, "密焊", body, false)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 可复制标记应符合预期，不对说明开关没写上。
	if tight.Copyable || tight.Revision != 1 || tight.Status != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", tight)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	wide, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "默认可复制", body)
	// 建工艺失败或修订不对就停，说明没达预期。
	if err != nil || !wide.Copyable || wide.Revision != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", wide, err)
	}
}
