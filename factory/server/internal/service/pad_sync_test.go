// 示教器清单与拉包按登录人裁：别人的个人级不给，钉住的也不给。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 示教器清单应是厂端列表去掉别人的个人级，别人的个人工程也拉不到。
func TestPadInboxTrimsOthersPersonal(t *testing.T) {
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
	// 网页列表仍列别人的个人级元数据，网页口径不受这次裁剪影响。
	if !padSyncListHas(listed, mine.ID) {
		// 网页列表少了别人的个人级，说明裁剪误伤了网页。
		t.Fatalf("factory list lost personal meta %+v", listed)
	}
	// 拉平板收件箱，失败说明会话失效或名单空。
	box, err := fac.PadClientInbox(ctx, sess.Token)
	// 拉收件箱失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉收件箱的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 厂端列表去掉别人的个人级，就是示教器该拿到的清单。
	want := padSyncTrim(listed, sess.Account.ID)
	// 条数应和裁过的列表一致，多了说明别人的个人级漏出去了。
	if len(box.Closures) != len(want) {
		// 断言没通过就停，清单条数和裁过的列表不一致。
		t.Fatalf("pad %d want %d", len(box.Closures), len(want))
	}
	// 逐条按位比对，清单应与厂端列表同序。
	for i, a := range want {
		// 同一位应是同一份资产，对不上说明裁剪或顺序写偏了。
		if box.Closures[i].AssetID != a.ID {
			// 断言没通过就停，清单和裁过的列表不一致。
			t.Fatalf("pad[%d] %s want %s %s", i, box.Closures[i].AssetID, a.Kind, a.Name)
		}
	}
	// 草稿仍应在清单里，缺了说明裁多了。
	if !padInboxHas(box, draftProc.ID) || !padInboxHas(box, draftProj.ID) {
		// 草稿被裁掉了，说明裁剪误伤了厂级。
		t.Fatalf("draft %+v", box.Closures)
	}
	// 别人的个人工程不应进清单，进了说明没按登录人裁。
	if padInboxHas(box, mine.ID) {
		// 个人稿漏给了别人，说明归属没保住。
		t.Fatalf("others personal leaked %+v", box.Closures)
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
	// 拉别人的个人工程应按找不到拒绝，与清单同一套裁剪。
	if _, err := fac.PadPullClientClosure(ctx, sess.Token, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		// 拉到了或错因不对，说明拉包绕过了清单裁剪。
		t.Fatalf("others personal pull %v", err)
	}
	// 拉出审计流水，核对拒绝有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 应记下这人拉这份被拒，缺了说明拒绝没留痕。
	if !padSyncDenied(rows, sess.Account.ID, mine.ID) {
		// 审计里没有这条拒绝，说明越权尝试没记账。
		t.Fatalf("pad_pull deny audit missing %s", audit.Dump(rows))
	}

	// 创建人用平板登录，自己的个人工程应照常拿到。
	peSess := padSyncLogin(t, ctx, fac, "pe", "pe-pass")
	// 拉创建人的清单，失败则后面无从比对。
	peBox := padSyncInbox(t, ctx, fac, peSess.Token)
	// 创建人的清单里应有自己的个人工程，没有说明裁多了。
	if !padInboxHas(peBox, mine.ID) {
		// 本人的个人工程被裁掉，说明归属判断写反了。
		t.Fatalf("own personal missing %+v", peBox.Closures)
	}
	// 创建人拉自己的个人工程，应当成功。
	gotMine, err := fac.PadPullClientClosure(ctx, peSess.Token, mine.ID)
	// 平板拉包失败或结果不符就停，说明没达预期。
	if err != nil || gotMine.Snapshot.AssetID != mine.ID {
		// 本人拉不到自己的个人工程，说明裁剪误伤了本人。
		t.Fatalf("own personal pull %+v %v", gotMine, err)
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

// 厂级工程钉住别人的个人工艺：示教器只给工程和引用，不给那份工艺。
func TestPadPinnedPersonalProcess(t *testing.T) {
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
	// 建个人工艺的创建人，整厂操作员。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 另一位整厂操作员，用来建厂级工程并试拉。
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 整厂组织管理员，示教器上也不该拿到别人的个人级。
	_ = mustCreateRole(t, ctx, fac, sa, "oa", "oa-pass", factory.RoleOrgAdmin, factory.ScopeFactory, nil)

	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// pe 新建一份个人工艺，失败说明个人入口被拒。
	pinned, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"current":160}`))
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，可用后才能被厂级工程钉住。
	pinned, err = fac.PublishAsset(ctx, pe.tok, pinned.ID, pinned.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 工程正文引用这份个人工艺，建工程时据此钉依赖。
	body := []byte(`[{"name":"w","processId":"` + pinned.ID.String() + `"}]`)
	// op 建厂级工程钉住 pe 的个人工艺，本厂有效账号都能这样建。
	proj, err := fac.CreateFactoryProject(ctx, op.tok, direct, "钉个人工艺的工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 依赖里应正好是那份个人工艺，没钉上则后面断言不成立。
	if len(proj.Deps) != 1 || proj.Deps[0].ID != pinned.ID {
		// 依赖没钉上这份个人工艺，用例前提不成立。
		t.Fatalf("deps %+v", proj.Deps)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proj, err = fac.PublishAsset(ctx, op.tok, proj.ID, proj.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// op 用平板登录，领人钥和会话。
	opSess := padSyncLogin(t, ctx, fac, "op", "op-pass")
	// 拉 op 的示教器清单。
	opBox := padSyncInbox(t, ctx, fac, opSess.Token)
	// 清单应有厂级工程、没有被钉住的个人工艺。
	if !padInboxHas(opBox, proj.ID) || padInboxHas(opBox, pinned.ID) {
		// 工程缺了或个人工艺漏了，说明钉住被当成了例外。
		t.Fatalf("op inbox %+v", opBox.Closures)
	}
	// op 拉那份个人工艺应按找不到拒绝。
	if _, err := fac.PadPullClientClosure(ctx, opSess.Token, pinned.ID); !errors.Is(err, domain.ErrNotFound) {
		// 拉到了或错因不对，说明钉住开了口子。
		t.Fatalf("op pull pinned %v", err)
	}
	// op 拉厂级工程应成功，工程照常进袋。
	gotProj, err := fac.PadPullClientClosure(ctx, opSess.Token, proj.ID)
	// 拉包失败或成员不止根，说明工程包带了工艺正文。
	if err != nil || gotProj.Snapshot.AssetID != proj.ID || len(gotProj.Snapshot.Members) != 1 {
		// 工程拉不到或成员不对，说明裁剪误伤了厂级工程。
		t.Fatalf("op pull project %+v %v", gotProj, err)
	}
	// 工程正文里只留工艺引用，示教器靠它对上焊道。
	if !bytes.Contains(gotProj.Snapshot.Members[0].Content, []byte(pinned.ID.String())) {
		// 正文里没有工艺引用，说明工程正文不完整。
		t.Fatalf("project missing processId %s", gotProj.Snapshot.Members[0].Content)
	}

	// 组织管理员用平板登录，看示教器上是否仍不给。
	oaSess := padSyncLogin(t, ctx, fac, "oa", "oa-pass")
	// 拉组织管理员的示教器清单。
	oaBox := padSyncInbox(t, ctx, fac, oaSess.Token)
	// 清单应有厂级工程、没有别人的个人工艺。
	if !padInboxHas(oaBox, proj.ID) || padInboxHas(oaBox, pinned.ID) {
		// 管理员拿到了别人的个人级，说明按角色放了行。
		t.Fatalf("oa inbox %+v", oaBox.Closures)
	}
	// 组织管理员拉那份个人工艺也应按找不到拒绝。
	if _, err := fac.PadPullClientClosure(ctx, oaSess.Token, pinned.ID); !errors.Is(err, domain.ErrNotFound) {
		// 拉到了或错因不对，说明管理员成了例外。
		t.Fatalf("oa pull pinned %v", err)
	}

	// 创建人用平板登录，自己的个人工艺应照常拿到。
	peSess := padSyncLogin(t, ctx, fac, "pe", "pe-pass")
	// 拉创建人的示教器清单。
	peBox := padSyncInbox(t, ctx, fac, peSess.Token)
	// 创建人的清单应同时有个人工艺和厂级工程。
	if !padInboxHas(peBox, pinned.ID) || !padInboxHas(peBox, proj.ID) {
		// 本人的个人工艺或厂级工程缺了，说明裁多了。
		t.Fatalf("pe inbox %+v", peBox.Closures)
	}
	// 创建人拉自己的个人工艺，应拿到过站包。
	gotPinned, err := fac.PadPullClientClosure(ctx, peSess.Token, pinned.ID)
	// 拉包失败或身份不符就停，说明本人被误拒。
	if err != nil || gotPinned.Snapshot.AssetID != pinned.ID {
		// 本人拉不到自己的个人工艺，说明裁剪误伤了本人。
		t.Fatalf("pe pull pinned %+v %v", gotPinned, err)
	}
	// 用创建人自己的人钥打开过站包。
	opened, err := factory.OpenTransit(fac.Store().FactoryID(), peSess.Account.ID, peSess.UnwrapKey, gotPinned)
	// 解不开或成员不对，说明过站包没封给本人。
	if err != nil || len(opened.Members) != 1 || opened.Members[0].ID != pinned.ID {
		// 本人解不开自己的工艺，说明封包绑错了人。
		t.Fatalf("pe open pinned %+v %v", opened, err)
	}

	// 拉出审计流水，核对两次越权拉取都记了拒绝。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// op 与组织管理员的拒绝都应留痕。
	if !padSyncDenied(rows, opSess.Account.ID, pinned.ID) || !padSyncDenied(rows, oaSess.Account.ID, pinned.ID) {
		// 审计缺了拒绝，说明越权尝试没记账。
		t.Fatalf("pad_pull deny audit missing %s", audit.Dump(rows))
	}

	// 三人清单里的每一条都应拉得到，免得同步卡在被拒。
	padSyncPullAll(t, ctx, fac, opSess.Token, opBox)
	// 组织管理员清单里的每一条都应拉得到。
	padSyncPullAll(t, ctx, fac, oaSess.Token, oaBox)
	// 创建人清单里的每一条都应拉得到。
	padSyncPullAll(t, ctx, fac, peSess.Token, peBox)
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

// 看网页列表里有没有这份资产，用来确认网页口径没变。
func padSyncListHas(rows []factory.AssetView, id uuid.UUID) bool {
	// 逐条找这份资产，找到即停。
	for _, a := range rows {
		// 身份对上就算列出来了。
		if a.ID == id {
			return true
		}
	}
	return false
}

// 按示教器口径裁网页列表：别人的个人级去掉，顺序不变。
func padSyncTrim(rows []factory.AssetView, me uuid.UUID) []factory.AssetView {
	// 备好结果，容量按原列表条数。
	out := make([]factory.AssetView, 0, len(rows))
	// 逐条看归属，别人的个人级跳过。
	for _, a := range rows {
		// 别人的个人级不进示教器清单。
		if a.Level == factory.AssetLevelPersonal && a.CreatorID != me {
			continue
		}
		// 其余条目按原顺序收进来。
		out = append(out, a)
	}
	return out
}

// 审计里有没有这人拉这份被拒的记录，缺了说明没留痕。
func padSyncDenied(rows []audit.Row, actor, assetID uuid.UUID) bool {
	// 逐行找对应的拉包拒绝。
	for _, r := range rows {
		// 动作、结果、对象和操作者都要对上。
		if r.Action == "pad_pull" && r.Result == audit.Deny && r.Target == assetID.String() && r.ActorID != nil && *r.ActorID == actor {
			return true
		}
	}
	return false
}

// 平板登录并核对人钥，失败则后面没有会话可用。
func padSyncLogin(t *testing.T, ctx context.Context, fac *factory.Service, login, pass string) factory.PadSession {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 用平板登录厂服，拿会话和人钥。
	sess, err := fac.LoginPad(ctx, login, pass)
	// 登录失败或人钥长度不对就停。
	if err != nil || len(sess.UnwrapKey) != 32 {
		// 平板登录没成，后面无从验证。
		t.Fatalf("pad login %s %+v %v", login, sess, err)
	}
	return sess
}

// 拉示教器清单，失败则后面无从比对。
func padSyncInbox(t *testing.T, ctx context.Context, fac *factory.Service, tok string) factory.ClientInbox {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 拉平板收件箱，失败说明会话失效。
	box, err := fac.PadClientInbox(ctx, tok)
	// 拉收件箱失败就停。
	if err != nil {
		// 把拉收件箱的错误打出来并停住。
		t.Fatal(err)
	}
	return box
}

// 清单里的每一条都应拉得到，免得示教器同步停在被拒。
func padSyncPullAll(t *testing.T, ctx context.Context, fac *factory.Service, tok string, box factory.ClientInbox) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 逐条按清单拉包。
	for _, r := range box.Closures {
		// 清单里有的这一条应能拉到。
		got, err := fac.PadPullClientClosure(ctx, tok, r.AssetID)
		// 拉不到或身份不符，说明清单和拉包裁剪不一致。
		if err != nil || got.Snapshot.AssetID != r.AssetID {
			// 清单列了却拉不到，示教器会卡在被拒。
			t.Fatalf("listed but not pullable %s %s %v", r.AssetID, r.Name, err)
		}
	}
}
