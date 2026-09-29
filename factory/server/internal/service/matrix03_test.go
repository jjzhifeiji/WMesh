// 阶段3第4圈：厂内侧身份、修订、状态、完整性（1.1～4.3、16.1～16.2）。
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

// 验收身份、修订、状态和完整性，对不上即失败。
func testAssetIdentity(t *testing.T, run func(string, func(*testing.T))) {
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
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("weld-body-v1")
	// 准备这段正文字节，读回对不上说明没写进去。
	next := []byte("weld-body-v2")

	// 先留出工艺变量，建好后再放进来给后续用。
	var weld factory.Asset
	// 验收条目 1.1，断言不过表示这一条没过。
	run("1.1", func(t *testing.T) {
		// 先备好错误位，失败分支再写入具体原因。
		var err error
		// 新建一份厂级工艺，失败说明起草入口坏了。
		weld, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "焊接", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 可复制标记应符合预期，不对说明开关没写上。
		if weld.ID.String() == "" || weld.Revision != 1 || weld.Status != factory.AssetDraft ||
			weld.Level != factory.AssetLevelFactory || weld.Kind != factory.KindProcess ||
			!weld.Copyable || weld.Name != "焊接" || weld.FactoryID != seed.ID || weld.Content != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", weld)
		}
	})
	// 验收条目 1.2，断言不过表示这一条没过。
	run("1.2", func(t *testing.T) {
		// 修改资产名称，失败说明修订冲突或越权。
		got, err := facA.RenameAsset(ctx, pe.tok, weld.ID, weld.Revision, "焊接-2")
		// 改名称失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把改名称的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 修订号应按这次保存前进，没变说明写没落库。
		if got.ID != weld.ID || got.Revision != weld.Revision+1 || got.Name != "焊接-2" {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", got)
		}
		// 记下刚建好的工艺，后面用例靠它继续。
		weld = got
	})

	// 验收条目 2.1，断言不过表示这一条没过。
	run("2.1", func(t *testing.T) {
		// 改写资产正文，失败说明修订冲突或越权。
		got, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, next)
		// 改正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把改正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 修订号应按这次保存前进，没变说明写没落库。
		if got.ID != weld.ID || got.Revision != weld.Revision+1 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", got)
		}
		// 读回资产正文，失败说明无权读或稿已经没了。
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		// 读正文失败或正文不同就停，说明没达预期。
		if err != nil || !bytes.Equal(gotBody, next) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%q %v", gotBody, err)
		}
		// 记下刚建好的工艺，后面用例靠它继续。
		weld = got
	})
	// 验收条目 2.2，断言不过表示这一条没过。
	run("2.2", func(t *testing.T) {
		// 改正文应因修订冲突被拒绝，放行说明没拦住。
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision-1, []byte("stale")); !errors.Is(err, domain.ErrRevisionConflict) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("got %v", err)
		}
		// 读取资产台账，失败说明无权看或已经删除。
		still, err := facA.GetAsset(ctx, pe.tok, weld.ID)
		// 读台账失败或修订不对就停，说明没达预期。
		if err != nil || still.Revision != weld.Revision || still.Name != weld.Name {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", still, err)
		}
		// 读回资产正文，失败说明无权读或稿已经没了。
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		// 读正文失败或正文不同就停，说明没达预期。
		if err != nil || !bytes.Equal(gotBody, next) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%q %v", gotBody, err)
		}
	})
	// 验收条目 2.3，断言不过表示这一条没过。
	run("2.3", func(t *testing.T) {
		// 改写资产正文，失败说明修订冲突或越权。
		first, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, []byte("weld-body-v3"))
		// 改正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把改正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 改正文应因修订冲突被拒绝，放行说明没拦住。
		if _, err := facA.UpdateAssetContent(ctx, pe2.tok, weld.ID, weld.Revision, []byte("lost")); !errors.Is(err, domain.ErrRevisionConflict) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("got %v", err)
		}
		// 读取资产台账，失败说明无权看或已经删除。
		still, err := facA.GetAsset(ctx, pe.tok, weld.ID)
		// 读台账失败或修订不对就停，说明没达预期。
		if err != nil || still.Revision != first.Revision {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", still, err)
		}
		// 记下第一份工艺，用来和后一份比修订。
		weld = first
	})

	// 验收条目 3.1，断言不过表示这一条没过。
	run("3.1", func(t *testing.T) {
		// 新建一份厂级工艺，失败说明起草入口坏了。
		draft, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "草稿工艺", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 建工程应因资产不可用被拒绝，放行说明没拦住。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, []factory.AssetDep{{
			ID: draft.ID, Revision: draft.Revision, Digest: draft.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("got %v", err)
		}
	})
	// 验收条目 3.2，断言不过表示这一条没过。
	run("3.2", func(t *testing.T) {
		// 新建一份个人工艺，失败说明个人入口被拒。
		draft, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人草稿", body)
		// 建个人工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建个人工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 把资产升到厂级，失败说明状态不允许升。
		got, err := facA.PromoteToFactory(ctx, pe.tok, draft.ID)
		// 升厂级失败或状态不对就停，说明没达预期。
		if err != nil || got.ID == draft.ID || got.Level != factory.AssetLevelFactory || got.Status != factory.AssetAvailable || got.Content != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", got, err)
		}
		// 读取资产台账，失败说明无权看或已经删除。
		still, err := facA.GetAsset(ctx, pe.tok, draft.ID)
		// 读台账失败或修订不对就停，说明没达预期。
		if err != nil || still.Level != factory.AssetLevelPersonal || still.Status != factory.AssetDraft || still.Revision != 1 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", still, err)
		}
	})
	// 验收条目 3.3，断言不过表示这一条没过。
	run("3.3", func(t *testing.T) {
		// 发布当前修订，失败说明状态不允许发布。
		got, err := facA.PublishAsset(ctx, pe.tok, weld.ID, weld.Revision)
		// 发布失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把发布的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 修订号应按这次保存前进，没变说明写没落库。
		if got.Status != factory.AssetAvailable || got.Revision != weld.Revision+1 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", got)
		}
		// 记下刚建好的工艺，后面用例靠它继续。
		weld = got
	})
	// 验收条目 16.2，断言不过表示这一条没过。
	run("16.2", func(t *testing.T) {
		// 读回资产正文，失败说明无权读或稿已经没了。
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		// 读正文失败或正文不同就停，说明没达预期。
		if err != nil || !bytes.Equal(gotBody, []byte("weld-body-v3")) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%q %v", gotBody, err)
		}
		// 拉出审计流水，失败则无法核对有没有记账。
		rows, err := facA.ListAudit(ctx)
		// 拉审计失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把拉审计的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 审计应留下允许或拒绝，缺了说明这步没记账。
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("incomplete %#v", bad[0])
		}
		// 审计里不应出现口令或正文，出现了就算泄密。
		if audit.ContainsAny(audit.Dump(rows), "weld-body-v1", "weld-body-v2", "weld-body-v3", "pe-pass", "sa-pass") {
			// 结果里出现了不该有的敏感词，说明已经泄密。
			t.Fatalf("secret or body leaked")
		}
	})
	// 验收条目 3.4，断言不过表示这一条没过。
	run("3.4", func(t *testing.T) {
		// 停用这份资产，失败说明仍被引用或越权。
		got, err := facA.DisableAsset(ctx, pe.tok, weld.ID, weld.Revision)
		// 停用资产失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把停用资产的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 记下刚建好的工艺，后面用例靠它继续。
		weld = got
		// 改正文应因资产不可用被拒绝，放行说明没拦住。
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, []byte("after-disable")); !errors.Is(err, domain.ErrAssetNotAvailable) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("update: %v", err)
		}
		// 建工程应因资产不可用被拒绝，放行说明没拦住。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "依赖停用", body, []factory.AssetDep{{
			ID: weld.ID, Revision: weld.Revision, Digest: weld.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("dep: %v", err)
		}
		// 新建一份个人工艺，失败说明个人入口被拒。
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "停用再升", body)
		// 建个人工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建个人工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 发布当前修订，失败说明状态不允许发布。
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		// 发布失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把发布的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 停用这份资产，失败说明仍被引用或越权。
		p, err = facA.DisableAsset(ctx, pe.tok, p.ID, p.Revision)
		// 停用资产失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把停用资产的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 升厂级应因资产不可用被拒绝，放行说明没拦住。
		if _, err := facA.PromoteToFactory(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			// 升档后的级别不对，说明没有升成厂级。
			t.Fatalf("promote: %v", err)
		}
	})
	// 验收条目 3.5，断言不过表示这一条没过。
	run("3.5", func(t *testing.T) {
		// 重新启用资产，失败说明状态不允许恢复。
		got, err := facA.ReenableAsset(ctx, pe.tok, weld.ID, weld.Revision)
		// 重新启用失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把重新启用的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 修订号应按这次保存前进，没变说明写没落库。
		if got.Status != factory.AssetAvailable || got.Revision != weld.Revision+1 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v", got)
		}
		// 记下刚建好的工艺，后面用例靠它继续。
		weld = got
	})
	// 验收条目 3.6，断言不过表示这一条没过。
	run("3.6", func(t *testing.T) {
		// 删除资产失败就停，否则后面没有可靠结果。
		if err := facA.DeleteAsset(ctx, pe.tok, weld.ID); err != nil {
			// 把删除资产的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 读台账应因找不到被拒绝，放行说明没拦住。
		if _, err := facA.GetAsset(ctx, pe.tok, weld.ID); !errors.Is(err, domain.ErrNotFound) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("got %v", err)
		}
	})
	// 验收条目 3.7，断言不过表示这一条没过。
	run("3.7", func(t *testing.T) {
		// 新建一份厂级工艺，失败说明起草入口坏了。
		p, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "被依赖", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 发布当前修订，失败说明状态不允许发布。
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		// 发布失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把发布的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 建工程失败就停，否则后面没有可靠结果。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "钉死依赖", body, []factory.AssetDep{{
			ID: p.ID, Revision: p.Revision, Digest: p.Digest,
		}}); err != nil {
			t.Fatal(err)
		}
		// 删除资产应因仍被引用被拒绝，放行说明没拦住。
		if err := facA.DeleteAsset(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrReferenced) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("got %v", err)
		}
		// 读台账失败就停，否则后面没有可靠结果。
		if _, err := facA.GetAsset(ctx, pe.tok, p.ID); err != nil {
			// 把读台账的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
	})
	// 验收条目 4.2，断言不过表示这一条没过。
	run("4.2", func(t *testing.T) {
		// 建平台工艺应因越权被拒绝，放行说明没拦住。
		if err := facA.CreatePlatformProcess(ctx, pe.tok, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("pe: %v", err)
		}
		// 建平台工艺应因越权被拒绝，放行说明没拦住。
		if err := facA.CreatePlatformProcess(ctx, saA, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("sa: %v", err)
		}
	})
	// 验收条目 1.3，断言不过表示这一条没过。
	run("1.3", func(t *testing.T) {
		// 新建一份个人工艺，失败说明个人入口被拒。
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		// 建个人工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建个人工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 发布当前修订，失败说明状态不允许发布。
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		// 发布失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把发布的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 把资产升到厂级，失败说明状态不允许升。
		promoted, err := facA.PromoteToFactory(ctx, pe.tok, src.ID)
		// 升厂级失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把升厂级的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 可复制标记应符合预期，不对说明开关没写上。
		if promoted.ID == src.ID || promoted.Level != factory.AssetLevelFactory || promoted.Revision != 1 ||
			promoted.Status != factory.AssetAvailable || !promoted.Copyable || promoted.Content != nil ||
			promoted.SourceID == nil || *promoted.SourceID != src.ID ||
			promoted.SourceRevision == nil || *promoted.SourceRevision != src.Revision {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v from %+v", promoted, src)
		}
		// 读取资产台账，失败说明无权看或已经删除。
		orig, err := facA.GetAsset(ctx, pe.tok, src.ID)
		// 读台账失败或修订不对就停，说明没达预期。
		if err != nil || orig.Level != factory.AssetLevelPersonal || orig.Revision != src.Revision {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("%+v %v", orig, err)
		}
	})
	// 验收条目 16.1，断言不过表示这一条没过。
	run("16.1", func(t *testing.T) {
		// 新建一份厂级工艺，失败说明起草入口坏了。
		a, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "脏数据", body)
		// 建工艺失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 发布当前修订，失败说明状态不允许发布。
		a, err = facA.PublishAsset(ctx, pe.tok, a.ID, a.Revision)
		// 发布失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把发布的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 篡改正文失败就停，否则后面没有可靠结果。
		if err := facA.Store().TamperAssetContent(ctx, a.ID, []byte("tampered")); err != nil {
			// 把篡改正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 读台账失败就停，否则后面没有可靠结果。
		if _, err := facA.GetAsset(ctx, pe.tok, a.ID); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("get meta: %v", err)
		}
		// 读正文应因完整性失败被拒绝，放行说明没拦住。
		if _, err := facA.ReadAssetContent(ctx, pe.tok, a.ID); !errors.Is(err, domain.ErrIntegrity) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("read: %v", err)
		}
	})
}
