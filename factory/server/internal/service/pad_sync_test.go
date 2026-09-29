// 平板登录拉的工艺/工程与厂端列表同一范围，不另走下发授权。
package service_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	factory "wmesh/factory/internal/service"
)

// 平板登录拉到的范围应和厂端列表一致，漏了即失败。
func TestPadInboxMatchesFactoryList(t *testing.T) {
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
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	draftProc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "草稿工艺", []byte(`{"current":1}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"current":180}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把编号打成文本，方便和路径或正文比对。
	body := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, nil)
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
	// 新建一份厂级工程，失败说明起草入口坏了。
	draftProj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "草稿工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份个人工程，失败说明个人入口被拒。
	mine, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, nil)
	// 建个人工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	mine, err = fac.PublishAsset(ctx, pe.tok, mine.ID, mine.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 用平板登录厂服，失败说明账号没有对上。
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败或条数不对就停，说明没达预期。
	if err != nil || len(sess.UnwrapKey) != 32 || len(sess.Devices) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad keys %+v %v", sess, err)
	}
	// 列出可见资产，失败说明会话或范围不对。
	listed, err := fac.ListAssets(ctx, sess.Token, "")
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 拉平板收件箱，失败说明会话失效或名单空。
	box, err := fac.PadClientInbox(ctx, sess.Token)
	// 拉收件箱失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉收件箱的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(box.Closures) != len(listed) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad %d list %d", len(box.Closures), len(listed))
	}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, a := range listed {
		// 结果应和这一步的预期一致，偏离说明行为写偏了。
		if !padInboxHas(box, a.ID) {
			// 应存在的记录没有出现，说明这一步没落下。
			t.Fatalf("missing %s %s", a.Kind, a.Name)
		}
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if !padInboxHas(box, draftProc.ID) || !padInboxHas(box, draftProj.ID) || !padInboxHas(box, mine.ID) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("draft/personal %+v", box.Closures)
	}

	// 平板拉取这份组包，失败说明没收进收件箱。
	gotProc, err := fac.PadPullClientClosure(ctx, sess.Token, proc.ID)
	// 认信封失败或结果不符就停，说明没达预期。
	if err != nil || !contentcrypt.IsEnvelope(gotProc.Wrap) || gotProc.Snapshot.Kind != factory.KindProcess {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("process pull %+v %v", gotProc, err)
	}
	// 打开传输信封，失败说明密钥或摘要不对。
	opened, err := factory.OpenTransit(fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, gotProc)
	// 拆信封失败或条数不对就停，说明没达预期。
	if err != nil || len(opened.Members) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("person key open %+v %v", opened, err)
	}
	// 平板拉取这份组包，失败说明没收进收件箱。
	gotDraft, err := fac.PadPullClientClosure(ctx, sess.Token, draftProj.ID)
	// 平板拉包失败或条数不对就停，说明没达预期。
	if err != nil || gotDraft.Snapshot.Kind != factory.KindProject || len(gotDraft.Wrap) != 0 || len(gotDraft.Snapshot.Members) != 1 {
		// 状态还停在草稿，说明保存没有落成可用。
		t.Fatalf("draft pull %+v %v", gotDraft, err)
	}
	// 传输形态应是信封或明文中的预期那种，反了即失败。
	if contentcrypt.IsEnvelope(gotDraft.Snapshot.Members[0].Content) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("project must be plaintext")
	}
	// 正文里应能对上这段，对不上说明没写进去。
	if !bytes.Contains(gotDraft.Snapshot.Members[0].Content, []byte(proc.ID.String())) {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatalf("project missing processId %s", gotDraft.Snapshot.Members[0].Content)
	}
	// 平板拉取这份组包，失败说明没收进收件箱。
	gotMine, err := fac.PadPullClientClosure(ctx, sess.Token, mine.ID)
	// 平板拉包失败或结果不符就停，说明没达预期。
	if err != nil || gotMine.Snapshot.AssetID != mine.ID {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal pull %+v %v", gotMine, err)
	}

	// 改策略失败就停，否则后面没有可靠结果。
	if _, err := fac.SetClientPolicy(ctx, sa, factory.ClientPolicy{
		// 写上缓存条数和范围，策略没生效则端上仍是旧值。
		MaxCachedProjects: 2, CacheScope: factory.CacheScopeCurrent, PersistUnwrapKey: false, KeyTTLSeconds: 0, EncryptPouch: true,
	}); err != nil {
		t.Fatal(err)
	}
	// 拉平板收件箱，失败说明会话失效或名单空。
	again, err := fac.PadClientInbox(ctx, sess.Token)
	// 拉收件箱失败或结果不符就停，说明没达预期。
	if err != nil || !padInboxHas(again, proj.ID) || !padInboxHas(again, proc.ID) {
		// 范围和预期不一致，说明该裁的工程还在。
		t.Fatalf("current scope pad %+v %v", again, err)
	}
}

// 看收件箱里有没有这份资产，没有说明名单漏了。
func padInboxHas(box factory.ClientInbox, id uuid.UUID) bool {
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, r := range box.Closures {
		// 结果应和这一步的预期一致，偏离说明行为写偏了。
		if r.AssetID == id {
			return true
		}
	}
	return false
}
