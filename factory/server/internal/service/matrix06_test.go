// 阶段3第6圈：厂内侧升档（每次复制新草稿）、跨厂隔离、工程依赖与归属（8.1～15.2、17.1～18.2）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 铺好两厂人员，逐条验收升档、隔离和工程依赖。
func testAssetPromote(t *testing.T, run func(string, func(*testing.T))) {
	// 标成测试辅助，失败行号才落到真正的用例。
	t.Helper()
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	_, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 改测试口令没成功，后面的断言就没有依据。
	if err := seedPass(ctx, facA, "sa-a", "sa-pass"); err != nil {
		// 改测试口令失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 改测试口令没成功，后面的断言就没有依据。
	if err := seedPass(ctx, facB, "sa-b", "sa-b-pass"); err != nil {
		// 改测试口令失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOrgAdmin)
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleOrgAdmin)
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator)
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	peB := mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleOrgAdmin)
	// 用直属上下文写资产，不把它挂到某个车间。
	direct := factory.WorkContext{}
	// 准备一段正文，读回或检索时要拿它对照。
	body := []byte("circle6-body-secret")
	// 新建组织节点，供后面步骤使用，失败则前提断了。
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	// 新建组织节点没成功，后面的断言就没有依据。
	if err != nil {
		// 新建组织节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 新建组织节点，供后面步骤使用，失败则前提断了。
	shop, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	// 新建组织节点没成功，后面的断言就没有依据。
	if err != nil {
		// 新建组织节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 新建组织节点，供后面步骤使用，失败则前提断了。
	shopB, err := facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
	// 新建组织节点没成功，后面的断言就没有依据。
	if err != nil {
		// 新建组织节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	peShop := mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleOrgAdmin)
	// 把人归进车间A，所属组织不给权，只用于统计。
	if _, err := facA.SetPersonOrg(ctx, saA, peShop.acc.ID, &shop.ID); err != nil {
		// 归组织失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 留出后面几条子测试还要接着用的资产。
	var personal, promoted, facProc, pinned factory.Asset
	// 个人工艺发布后升成厂级，源必须仍是个人级。
	run("8.1", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 记下这条个人工艺，后面还要再升一次。
		personal = src
		// 升成厂级，供后面步骤使用，失败则前提断了。
		promoted, err = facA.PromoteToFactory(ctx, pe.tok, src.ID, uuid.Nil)
		// 升成厂级没成功，后面的断言就没有依据。
		if err != nil {
			// 升成厂级失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 升档结果必须是新的厂级草稿，可复制，不带正文，并指回源。
		if promoted.ID == src.ID || promoted.Level != factory.AssetLevelFactory || promoted.Content != nil ||
			promoted.Status != factory.AssetDraft || !promoted.Copyable || promoted.SourceID == nil || *promoted.SourceID != src.ID {
			// 升档结果不对就停，原件被当成了厂级。
			t.Fatalf("%+v", promoted)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		orig, err := facA.GetAsset(ctx, pe.tok, src.ID)
		// 源工艺必须仍是个人级，而且修订不能被改掉。
		if err != nil || orig.Level != factory.AssetLevelPersonal || orig.Revision != src.Revision {
			// 源工艺变了就停，升档把原来那条带走了。
			t.Fatalf("%+v %v", orig, err)
		}
		// 读出资产正文，供后面步骤使用，失败则前提断了。
		got, err := facA.ReadAssetContent(ctx, pe.tok, src.ID)
		// 读回的个人正文必须和原来写入的一致。
		if err != nil || !bytes.Equal(got, body) {
			// 正文对不上就停，升档前的原文被读坏了。
			t.Fatalf("%q %v", got, err)
		}
	})
	// 同一条再升一次，必须另复制一份新草稿，不命中已有厂级。
	run("8.4", func(t *testing.T) {
		// 升成厂级，供后面步骤使用，失败则前提断了。
		again, err := facA.PromoteToFactory(ctx, pe.tok, personal.ID, uuid.Nil)
		// 升成厂级没成功，后面的断言就没有依据。
		if err != nil {
			// 升成厂级失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 再次升档必须是新身份的首个修订草稿。
		if again.ID == promoted.ID || again.Revision != 1 || again.Status != factory.AssetDraft {
			// 复用了已有厂级就停，升档不该回写。
			t.Fatalf("again %+v first %+v", again, promoted)
		}
	})
	// 个人正文改过后再升，新副本摘要跟上，旧副本不动。
	run("8.5", func(t *testing.T) {
		// 改写资产正文，供后面步骤使用，失败则前提断了。
		changed, err := facA.UpdateAssetContent(ctx, pe.tok, personal.ID, personal.Revision, []byte("personal-v2"))
		// 改写资产正文没成功，后面的断言就没有依据。
		if err != nil {
			// 改写资产正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 换成改过正文的个人工艺，供再次升档使用。
		personal = changed
		// 升成厂级，供后面步骤使用，失败则前提断了。
		got, err := facA.PromoteToFactory(ctx, pe.tok, personal.ID, uuid.Nil)
		// 新副本身份不同、摘要跟上源现在的正文。
		if err != nil || got.ID == promoted.ID || !bytes.Equal(got.Digest, personal.Digest) {
			// 摘要没跟上或覆盖了旧副本就停。
			t.Fatalf("copy %+v from %+v %v", got, personal, err)
		}
		// 读回第一份副本，修订和摘要都不能被改。
		first, err := facA.GetAsset(ctx, pe.tok, promoted.ID)
		// 旧副本被改了就停，升档回写了。
		if err != nil || first.Revision != promoted.Revision || !bytes.Equal(first.Digest, promoted.Digest) {
			// 旧副本被改了就停。
			t.Fatalf("first copy %+v want %+v %v", first, promoted, err)
		}
	})
	// 草稿个人工艺也能升；停用的个人工艺升档必须被拒。
	run("8.6", func(t *testing.T) {
		// 另建一条个人工艺，不动后面还要用的那条。
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "再升守门", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 还是草稿就升，应能复制。
		if _, err := facA.PromoteToFactory(ctx, pe.tok, src.ID, uuid.Nil); err != nil {
			// 草稿升不了就停。
			t.Fatalf("draft promote: %v", err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 停用这条个人工艺。
		off, err := facA.DisableAsset(ctx, pe.tok, src.ID, src.Revision)
		// 停用资产没成功，后面的断言就没有依据。
		if err != nil {
			// 停用资产失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 停用的个人工艺升档必须被拒。
		if _, err := facA.PromoteToFactory(ctx, pe.tok, off.ID, uuid.Nil); !errors.Is(err, domain.ErrAssetNotAvailable) {
			// 停用件被升了就停，问题工艺会被悄悄放开。
			t.Fatalf("disabled got %v", err)
		}
	})
	// 别人的个人工艺只有创建人和超管能升；没有厂级制作权的创建人也不能升。
	run("8.2", func(t *testing.T) {
		// 另一名管理员升 pe 的个人工艺必须被拒，个人级只给创建人和超管碰。
		if _, err := facA.PromoteToFactory(ctx, peShop.tok, personal.ID, uuid.Nil); !errors.Is(err, domain.ErrForbidden) {
			// 别人的个人工艺被升了就停。
			t.Fatalf("other admin got %v", err)
		}
		// 操作员建一条自己的个人工艺。
		src, err := facA.CreatePersonalProcess(ctx, op.tok, direct, "操作员个人", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 操作员没有厂级制作权，升自己的也必须被拒。
		if _, err := facA.PromoteToFactory(ctx, op.tok, src.ID, uuid.Nil); !errors.Is(err, domain.ErrForbidden) {
			// 操作员升上去了就停，制作权漏给了岗位。
			t.Fatalf("operator got %v", err)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		still, err := facA.GetAsset(ctx, op.tok, src.ID)
		// 被拒之后源工艺必须仍是个人级。
		if err != nil || still.Level != factory.AssetLevelPersonal {
			// 源工艺级别变了就停，升档改写了原件。
			t.Fatalf("%+v %v", still, err)
		}
	})
	// 超管同样可以把已发布的个人工艺升成厂级。
	run("8.3", func(t *testing.T) {
		// 超管升档必须成功，失败说明超管没有这条权限。
		if _, err := facA.PromoteToFactory(ctx, saA, personal.ID, uuid.Nil); err != nil {
			// 超管升档失败就停，这条权限和预期不符。
			t.Fatalf("got %v", err)
		}
	})
	// 厂端建的个人工艺都可复制。
	run("9.1", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "都可复制", body)
		// 建个人工艺没成功或不可复制就停。
		if err != nil || !p.Copyable {
			// 新建的不可复制就停，厂端不该再有保密件。
			t.Fatalf("%+v %v", p, err)
		}
	})
	// 厂级导出快照，可复制并指回源。
	run("9.2", func(t *testing.T) {
		// 建厂级工艺，供后面步骤使用，失败则前提断了。
		p, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "不可升平台", body)
		// 建厂级工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建厂级工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 导出升档快照，供后面步骤使用，失败则前提断了。
		snap, err := facA.ExportAssetSnapshot(ctx, pe.tok, p.ID)
		// 导出的快照必须可复制，并且要指回原来的源。
		if err != nil || !snap.Copyable || snap.SourceID != p.ID {
			// 快照不可复制或没有指回源就停，导出的信息不对。
			t.Fatalf("%+v %v", snap, err)
		}
	})
	// 正文被篡改后再升档，必须因完整性失败被拒绝。
	run("9.3", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "脏升档", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 改写库内正文没成功，后面的断言就没有依据。
		if err := facA.Store().TamperAssetContent(ctx, p.ID, []byte("tampered")); err != nil {
			// 改写库内正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 正文被篡改之后再升档，必须因完整性被拒绝。
		if _, err := facA.PromoteToFactory(ctx, pe.tok, p.ID, uuid.Nil); !errors.Is(err, domain.ErrIntegrity) {
			// 篡改了还能升就停，完整性没有挡住。
			t.Fatalf("got %v", err)
		}
	})
	// 厂里的人不能直接创建平台级工艺。
	run("10.2", func(t *testing.T) {
		// 厂内直接创建平台工艺必须因越权被拒绝。
		if err := facA.CreatePlatformProcess(ctx, pe.tok, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			// 厂内能建出平台工艺就停，级别被写穿了。
			t.Fatalf("got %v", err)
		}
	})

	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	facProc, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "厂级工艺", body)
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 发布成可用，供后面步骤使用，失败则前提断了。
	facProc, err = facA.PublishAsset(ctx, pe.tok, facProc.ID, facProc.Revision)
	// 发布成可用没成功，后面的断言就没有依据。
	if err != nil {
		// 发布成可用失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 他厂身份不能读改本厂工艺，本厂库也找不到对方。
	run("11.1", func(t *testing.T) {
		// 他厂会话读本厂工艺必须因未授权被拒绝。
		if _, err := facA.GetAsset(ctx, peB.tok, facProc.ID); !errors.Is(err, domain.ErrUnauthorized) {
			// 他厂能读到就停，工艺没有按厂隔开。
			t.Fatalf("get: %v", err)
		}
		// 他厂会话改本厂工艺必须因未授权被拒绝。
		if _, err := facA.UpdateAssetContent(ctx, peB.tok, facProc.ID, facProc.Revision, body); !errors.Is(err, domain.ErrUnauthorized) {
			// 他厂能改到就停，写路径没有隔开。
			t.Fatalf("update: %v", err)
		}
		// 他厂会话升本厂个人工艺必须因未授权被拒绝。
		if _, err := facA.PromoteToFactory(ctx, peB.tok, personal.ID, uuid.Nil); !errors.Is(err, domain.ErrUnauthorized) {
			// 他厂能升本厂工艺就停，升档没有隔开。
			t.Fatalf("promote: %v", err)
		}
		// 乙厂按甲厂标识去读必须因找不到被拒绝。
		if _, err := facB.GetAsset(ctx, peB.tok, facProc.ID); !errors.Is(err, domain.ErrNotFound) {
			// 乙厂能读到甲厂那一行就停，库没有分开。
			t.Fatalf("facB: %v", err)
		}
	})
	// 他厂工程不能把本厂工艺拿来当依赖。
	run("11.2", func(t *testing.T) {
		// 他厂工程依赖本厂工艺必须被拒绝，不能跨厂引用。
		if _, err := facB.CreateFactoryProject(ctx, peB.tok, direct, "跨厂工程", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})
	// 两厂同名工艺必须是不同资产，各自归各自的厂。
	run("11.3", func(t *testing.T) {
		// 建厂级工艺，供后面步骤使用，失败则前提断了。
		got, err := facB.CreateFactoryProcess(ctx, peB.tok, direct, "焊接", body)
		// 建厂级工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建厂级工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 两厂同名工艺必须是不同资产，而且要归在乙厂。
		if got.ID == facProc.ID || got.FactoryID != seedB.ID {
			// 两厂撞了同一条或归属不对就停。
			t.Fatalf("%+v", got)
		}
	})
	// 厂级工程可以钉住已发布工艺的那一个修订。
	run("13.1", func(t *testing.T) {
		// 另开一个错误变量，避免盖住外层的结果。
		var err error
		// 建厂级工程，供后面步骤使用，失败则前提断了。
		pinned, err = facA.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision, Digest: facProc.Digest,
		}})
		// 建厂级工程没成功，后面的断言就没有依据。
		if err != nil {
			// 建厂级工程失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 工程必须正好钉住那一条工艺的当前修订。
		if len(pinned.Deps) != 1 || pinned.Deps[0].ID != facProc.ID || pinned.Deps[0].Revision != facProc.Revision {
			// 依赖条数或修订不对就停，没有钉住。
			t.Fatalf("%+v", pinned)
		}
	})
	// 不存在、草稿、停用和错修订都不能当成依赖。
	run("13.2", func(t *testing.T) {
		// 依赖一个不存在的工艺时，建工程必须被拒绝。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "缺", body, []factory.AssetDep{{
			ID: uuid.New(), Revision: 1, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("missing: %v", err)
		}
		// 建厂级工艺，供后面步骤使用，失败则前提断了。
		draft, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "草稿", body)
		// 建厂级工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建厂级工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 草稿不能被工程依赖，必须因不可用被拒绝。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "草稿依赖", body, []factory.AssetDep{{
			ID: draft.ID, Revision: draft.Revision, Digest: draft.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("draft: %v", err)
		}
		// 建厂级工艺，供后面步骤使用，失败则前提断了。
		off, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "将停用", body)
		// 建厂级工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建厂级工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		off, err = facA.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 停用这条资产，供后面步骤使用，失败则前提断了。
		off, err = facA.DisableAsset(ctx, pe.tok, off.ID, off.Revision)
		// 停用这条资产没成功，后面的断言就没有依据。
		if err != nil {
			// 停用这条资产失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 已停用的工艺不能当依赖，建工程必须被拒绝。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "停用依赖", body, []factory.AssetDep{{
			ID: off.ID, Revision: off.Revision, Digest: off.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("disabled: %v", err)
		}
		// 修订对不上时建工程必须因依赖不成立被拒绝。
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "错修订", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision + 9, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("rev: %v", err)
		}
	})
	// 依赖的工艺改正文后，工程仍必须钉着旧修订。
	run("13.3", func(t *testing.T) {
		// 先记下钉住的修订，改正文之后拿来对照。
		oldRev := pinned.Deps[0].Revision
		// 改写资产正文没成功，后面的断言就没有依据。
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, facProc.ID, facProc.Revision, []byte("weld-v2")); err != nil {
			// 改写资产正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		got, err := facA.GetAsset(ctx, pe.tok, pinned.ID)
		// 工艺改正文之后，工程必须仍钉着原来的修订。
		if err != nil || len(got.Deps) != 1 || got.Deps[0].Revision != oldRev || got.Deps[0].ID != facProc.ID {
			// 工程修订跟着变了就停，钉修订失败了。
			t.Fatalf("%+v %v", got, err)
		}
	})
	// 个人工程不能依赖别人名下的个人工艺。
	run("13.4", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		other, err := facA.CreatePersonalProcess(ctx, pe2.tok, direct, "他人个人", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		other, err = facA.PublishAsset(ctx, pe2.tok, other.ID, other.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 个人工程依赖别人的个人工艺必须被拒绝。
		if _, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "借他人", body, []factory.AssetDep{{
			ID: other.ID, Revision: other.Revision, Digest: other.Digest,
		}}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})
	// 个人工程可以同时依赖自己的工艺和厂级工艺。
	run("13.5", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		own, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "自己个人", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		own, err = facA.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		facNow, err := facA.GetAsset(ctx, pe.tok, facProc.ID)
		// 读资产元数据没成功，后面的断言就没有依据。
		if err != nil {
			// 读资产元数据失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 建个人工程，供后面步骤使用，失败则前提断了。
		got, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, []factory.AssetDep{
			{ID: own.ID, Revision: own.Revision, Digest: own.Digest},
			{ID: facNow.ID, Revision: facNow.Revision, Digest: facNow.Digest},
		})
		// 个人工程必须同时钉住自己的工艺和厂级工艺。
		if err != nil || len(got.Deps) != 2 {
			// 依赖不是两条就停，这个组合没有建成。
			t.Fatalf("%+v %v", got, err)
		}
	})
	// 依赖的个人工艺还没升档时，工程不能跟着升。
	run("14.2", func(t *testing.T) {
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		own, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "未先升工艺", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		own, err = facA.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 建个人工程，供后面步骤使用，失败则前提断了。
		proj, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "个人工程待升", body, []factory.AssetDep{{
			ID: own.ID, Revision: own.Revision, Digest: own.Digest,
		}})
		// 建个人工程没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工程失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布成可用，供后面步骤使用，失败则前提断了。
		proj, err = facA.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
		// 发布成可用没成功，后面的断言就没有依据。
		if err != nil {
			// 发布成可用失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 依赖的工艺还没升档时，工程升档必须被拒绝。
		if _, err := facA.PromoteToFactory(ctx, pe.tok, proj.ID, uuid.Nil); !errors.Is(err, domain.ErrAssetDependency) {
			// 工艺没升、工程却升了就停，顺序错了。
			t.Fatalf("got %v", err)
		}
	})
	// 升出来的厂级草稿可以改正文再发布，改的是副本不碰源。
	run("14.3", func(t *testing.T) {
		// 改写厂级副本正文，供后面步骤使用，失败则前提断了。
		edited, err := facA.UpdateAssetContent(ctx, pe.tok, promoted.ID, promoted.Revision, []byte("promoted-v2"))
		// 改写失败就停，草稿副本该能改。
		if err != nil {
			// 改写资产正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 发布副本，供后面步骤使用，失败则前提断了。
		promoted, err = facA.PublishAsset(ctx, pe.tok, edited.ID, edited.Revision)
		// 发布失败或不是可用就停。
		if err != nil || promoted.Status != factory.AssetAvailable {
			// 发布失败即停，不能把这一步的失败当成通过。
			t.Fatalf("%+v %v", promoted, err)
		}
		// 读回源个人工艺，修订不能被副本改动带走。
		src, err := facA.GetAsset(ctx, pe.tok, personal.ID)
		// 源被改了就停，升档副本和源没分开。
		if err != nil || src.Revision != personal.Revision {
			// 源修订变了就停。
			t.Fatalf("%+v %v", src, err)
		}
	})
	// 个人级只给创建人和超管看：操作员读不了别人的正文，也看不到元数据。
	run("15.1", func(t *testing.T) {
		// 操作员读别人的个人正文必须因越权被拒绝。
		if _, err := facA.ReadAssetContent(ctx, op.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			// 操作员能读到个人正文就停，保密被打破了。
			t.Fatalf("read: %v", err)
		}
		// 操作员看别人个人级的元数据也必须被拒。
		if _, err := facA.GetAsset(ctx, op.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			// 操作员看到元数据就停，个人级不该露给别人。
			t.Fatalf("meta: %v", err)
		}
		// 超管能看别人个人级的元数据。
		still, err := facA.GetAsset(ctx, saA, personal.ID)
		// 超管看不到或级别变了就停。
		if err != nil || still.Level != factory.AssetLevelPersonal {
			// 超管看不到个人级就停。
			t.Fatalf("%+v %v", still, err)
		}
	})
	// 同事读不了原个人正文，作者仍能读升档后的厂级。
	run("15.2", func(t *testing.T) {
		// 同事读这条个人正文必须因越权被拒绝。
		if _, err := facA.ReadAssetContent(ctx, pe2.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			// 同事能读到个人正文就停，正文没有只对作者开放。
			t.Fatalf("orig: %v", err)
		}
		// 读资产元数据没成功，后面的断言就没有依据。
		if _, err := facA.GetAsset(ctx, pe.tok, promoted.ID); err != nil {
			// 读资产元数据失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读出资产正文没成功，后面的断言就没有依据。
		if _, err := facA.ReadAssetContent(ctx, pe.tok, promoted.ID); err != nil {
			// 读出资产正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
	})
	// 审计要完整，并且不能出现正文和口令。
	run("18.1", func(t *testing.T) {
		// 拉取审计记录，供后面步骤使用，失败则前提断了。
		rows, err := facA.ListAudit(ctx)
		// 拉取审计记录没成功，后面的断言就没有依据。
		if err != nil {
			// 拉取审计记录失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 审计行必须字段完整，缺了就不能当这条通过。
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			// 审计缺字段就停，记录没法往下追。
			t.Fatalf("incomplete %#v", bad[0])
		}
		// 审计文本里不能出现口令、正文或上传内容。
		if audit.ContainsAny(audit.Dump(rows), string(body), "pe-pass", "sa-pass", "op-pass") {
			// 审计里出现秘密就停，日志把内容写出去了。
			t.Fatalf("secret leaked")
		}
	})
	// 审计要带修订号，个人工艺的级别不能被改掉。
	run("18.2", func(t *testing.T) {
		// 拉取审计记录，供后面步骤使用，失败则前提断了。
		rows, err := facA.ListAudit(ctx)
		// 拉取审计记录没成功，后面的断言就没有依据。
		if err != nil {
			// 拉取审计记录失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 把审计行拼成文本，用来搜修订号或秘密。
		dump := audit.Dump(rows)
		// 审计文本里必须带有修订号，才能对上变更。
		if !bytes.Contains([]byte(dump), []byte("rev=")) {
			// 审计没有修订号就停，变更对不上版本。
			t.Fatalf("missing revision in audit")
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		still, err := facA.GetAsset(ctx, pe.tok, personal.ID)
		// 查完审计之后，个人工艺必须仍是个人级。
		if err != nil || still.Level != factory.AssetLevelPersonal {
			// 级别在审计之后变了就停，审计改动了资产。
			t.Fatalf("%+v %v", still, err)
		}
	})
	// 人改所属组织后，已写资产的作者不变，组织不记在资产上。
	run("17.1", func(t *testing.T) {
		// 把人归到车间B。
		if _, err := facA.SetPersonOrg(ctx, saA, pe.acc.ID, &shopB.ID); err != nil {
			// 归组织失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 建个人工艺，供后面步骤使用，失败则前提断了。
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "调岗前个人", body)
		// 建个人工艺没成功，后面的断言就没有依据。
		if err != nil {
			// 建个人工艺失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 把人调到车间A。
		if _, err := facA.SetPersonOrg(ctx, saA, pe.acc.ID, &shop.ID); err != nil {
			// 改组织失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		got, err := facA.GetAsset(ctx, saA, p.ID)
		// 调岗之后作者和级别都不能变。
		if err != nil || got.CreatorID != pe.acc.ID || got.Level != factory.AssetLevelPersonal {
			// 归属被带走就停，调人不该改已经写下的资产。
			t.Fatalf("%+v %v", got, err)
		}
		// 本人仍能看自己的个人级。
		if _, err := facA.GetAsset(ctx, pe.tok, p.ID); err != nil {
			// 调岗后看不到自己的就停。
			t.Fatal(err)
		}
	})
	// 超管能看别人的个人工艺，作者不变。
	run("17.2", func(t *testing.T) {
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		got, err := facA.GetAsset(ctx, saA, personal.ID)
		// 超管看不到或作者变了就停。
		if err != nil || got.CreatorID != pe.acc.ID || got.Level != factory.AssetLevelPersonal {
			// 作者变了就停，归属丢了。
			t.Fatalf("%+v %v", got, err)
		}
	})
	// 账号停用后不能再改正文，修订保持停用前那样。
	run("17.3", func(t *testing.T) {
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		before, err := facA.GetAsset(ctx, saA, personal.ID)
		// 读资产元数据没成功，后面的断言就没有依据。
		if err != nil {
			// 读资产元数据失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 停用这个账号没成功，后面的断言就没有依据。
		if err := facA.DisableAccount(ctx, saA, pe.acc.ID); err != nil {
			// 停用这个账号失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 停用已删掉会话，旧令牌改正文必须按未登录被拒绝。
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, personal.ID, personal.Revision, []byte("after-disable")); !errors.Is(err, domain.ErrUnauthorized) {
			// 停用之后还能改正文就停，停用没有生效。
			t.Fatalf("got %v", err)
		}
		// 读资产元数据，供后面步骤使用，失败则前提断了。
		still, err := facA.GetAsset(ctx, saA, personal.ID)
		// 停用之后修订必须保持停用前，不能被写进去。
		if err != nil || still.Revision != before.Revision {
			// 修订变了就停，停用没有把正文冻住。
			t.Fatalf("%+v %v", still, err)
		}
	})
}
