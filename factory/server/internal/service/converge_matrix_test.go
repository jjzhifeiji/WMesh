// 阶段2：在线收敛与事实归属 14.1～16.3；人员状态以厂库为准。
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 在线收敛后事实归属以厂库为准，串厂或漂移即失败。
func testConvergeMatrix(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()

	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err := facA.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 取当前时间，用来比较修订和时钟的先后。
	now := time.Now().UTC()
	// 准备一组对齐的时钟，时间倒了离线登录会被拒。
	valid := factory.Clocks{Server: now, Local: now}
	// 把时间拨到指定偏移，用来制造过期或未生效。
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	// 新取一个编号，撞号会让两条记录分不清。
	cidA := id.New()
	// 生成一把设备密钥，失败则绑定没有公钥可用。
	pubA, privA, err := nodekey.Generate()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登记绑定失败就停，否则后面没有可靠结果。
	if _, err := facA.AcceptBinding(ctx, cidA, "Client-A1", pubA, 1); err != nil {
		// 把登记绑定的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 取出签名公钥，空的说明登录没把钥匙发下来。
	facPubA, err := facA.SigningPublicKey(ctx)
	// 取签名公钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把取签名公钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 签发运行授权，失败说明越权或客户端不对。
	runtime, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
	// 签发授权失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把签发授权的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 新建组织节点，失败说明名称或上级不合法。
	site, err := facA.CreateOrgUnit(ctx, saTok, "场地", nil)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopA, err := facA.CreateOrgUnit(ctx, saTok, "车间A", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopB, err := facA.CreateOrgUnit(ctx, saTok, "车间B", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	op, err := facA.CreatePerson(ctx, saTok, "op-a", "操作员A")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	mustAdoptPassword(t, ctx, facA, "op-a", "op-pass")
	// 授予角色和作用域，失败说明越权或范围不对。
	opGrant, err := facA.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil)
	// 授角色失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := facA.Assign(ctx, saTok, op.ID, shopA.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 按这把密钥装离线包，装错则登录对不上厂。
	offline := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
	// 放行资产，若仍拒绝说明授权没看这面旗。
	offline.AssetAllowed = true
	// 套用运行授权，失败说明授权对不上这台。
	offline.ApplyRuntime(runtime)
	// 留下操作员编号，写进离线包才能对上是谁。
	oid := op.ID
	// 写上操作员，缺了离线登录会对不上人。
	offline.OperatorID = &oid

	// 按这把密钥装离线包，装错则登录对不上厂。
	online := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
	// 标成已连接，否则在线作业会被当成离线。
	online.Connected = true
	// 在线也放行资产，仍拒绝说明这面旗没生效。
	online.AssetAllowed = true
	// 套用运行授权，失败说明授权对不上这台。
	online.ApplyRuntime(runtime)
	// 写上在线操作员，缺了会把作业记成没人。
	online.OperatorID = &oid

	// 先留出事实变量，写入成功后再拿来比对。
	var factA16 factory.FactStub

	// 验收条目 16.1，断言不过表示这一条没过。
	run("16.1", func(t *testing.T) {
		// 先备好错误位，失败分支再写入具体原因。
		var err error
		// 写下离线作业事实，失败说明包或人没对上。
		factA16, err = facA.CreateOfflineFact(ctx, offline, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID})
		// 写离线事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把写离线事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 事实应落在预期组织上，串了车间说明上下文偏了。
		if factA16.OrgUnitID == nil || *factA16.OrgUnitID != shopA.ID {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("not A: %+v", factA16)
		}
		// 路径上的节点和名称应与当时组织一致，漂移即失败。
		if !pathHas(factA16.OrgPath, site.ID) || !pathHas(factA16.OrgPath, shopA.ID) || pathHas(factA16.OrgPath, shopB.ID) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("path drifted to B: %+v", factA16.OrgPath)
		}
	})

	// 摘组织失败就停，否则后面没有可靠结果。
	if err := facA.Unassign(ctx, saTok, op.ID, shopA.ID); err != nil {
		// 把摘组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := facA.Assign(ctx, saTok, op.ID, shopB.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改上级失败就停，否则后面没有可靠结果。
	if err := facA.ReparentOrgUnit(ctx, saTok, shopA.ID, &shopB.ID); err != nil {
		// 把改上级的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改组织名失败就停，否则后面没有可靠结果。
	if err := facA.RenameOrgUnit(ctx, saTok, shopB.ID, "车间B改名"); err != nil {
		// 把改组织名的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 验收条目 16.2，断言不过表示这一条没过。
	run("16.2", func(t *testing.T) {
		// 按编号读工作事实，失败说明无权或已不存在。
		got, err := facA.GetFact(ctx, saTok, factA16.ID)
		// 读事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 路径上的节点和名称应与当时组织一致，漂移即失败。
		if pathName(got.OrgPath, shopA.ID) != "车间A" || pathHas(got.OrgPath, shopB.ID) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("rewritten: %+v", got.OrgPath)
		}
		// 改写路径应因越权被拒绝，放行说明没拦住。
		if err := facA.RewriteFactPath(ctx, saTok, factA16.ID); !errors.Is(err, domain.ErrForbidden) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("rewrite: %v", err)
		}
	})

	// 验收条目 16.3，断言不过表示这一条没过。
	run("16.3", func(t *testing.T) {
		// 按作者列出事实，失败说明无权查看名单。
		listed, err := facA.ListFactsByCreator(ctx, saTok, op.ID)
		// 列事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把列事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 留出乙侧计数，汇聚后用来核对有没有串厂。
		var nB int
		// 逐条检查这一批结果，任一条偏离即判失败。
		for _, f := range listed {
			// 路径上的节点和名称应与当时组织一致，漂移即失败。
			if f.ID == factA16.ID && (f.OrgUnitID == nil || *f.OrgUnitID != shopA.ID || pathHas(f.OrgPath, shopB.ID)) {
				// 条目 16.1 没对上，这一条矩阵行为偏了。
				t.Fatalf("16.1 counted as B: %+v", f)
			}
			// 事实应落在预期组织上，串了车间说明上下文偏了。
			if f.OrgUnitID != nil && *f.OrgUnitID == shopB.ID {
				// 乙侧条数加一，少计说明这条汇聚被漏掉。
				nB++
			}
		}
		// 条数应和预期一致，多或少说明名单没算对。
		if nB != 0 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("unselected B counted: %d", nB)
		}
	})

	// 验收条目 14.2，断言不过表示这一条没过。
	run("14.2", func(t *testing.T) {
		// 写离线事实应因上下文非法被拒绝，放行说明没拦住。
		if _, err := facA.CreateOfflineFact(ctx, offline, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("old unit A still allowed: %v", err)
		}
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, offline, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("node still %+v %v", ev, err)
		}
		// 拉出审计流水，失败则无法核对有没有记账。
		rows, err := facA.ListAudit(ctx)
		// 拉审计失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把拉审计的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 审计里不应出现口令或正文，出现了就算泄密。
		if audit.ContainsAny(audit.Dump(rows), "op-pass") {
			// 结果里出现了不该有的敏感词，说明已经泄密。
			t.Fatal("secret leaked")
		}
	})

	// 验收条目 14.1，断言不过表示这一条没过。
	run("14.1", func(t *testing.T) {
		// 写离线事实应因上下文非法被拒绝，放行说明没拦住。
		if _, err := facA.CreateOfflineFact(ctx, online, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("old unit A: %v", err)
		}
		// 写下离线作业事实，失败说明包或人没对上。
		factB, err := facA.CreateOfflineFact(ctx, online, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopB.ID})
		// 写离线事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把写离线事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 路径上的节点和名称应与当时组织一致，漂移即失败。
		if factB.OrgUnitID == nil || *factB.OrgUnitID != shopB.ID || pathName(factB.OrgPath, shopB.ID) != "车间B改名" || pathHas(factB.OrgPath, shopA.ID) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("new tree not applied: %+v", factB.OrgPath)
		}
		// 收回角色失败就停，否则后面没有可靠结果。
		if err := facA.RevokeRole(ctx, saTok, opGrant.ID); err != nil {
			// 把收回角色的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, online, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny || ev.TimeSource != audit.Server {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("revoke %+v %v", ev, err)
		}
		// 停用账号失败就停，否则后面没有可靠结果。
		if err := facA.DisableAccount(ctx, saTok, op.ID); err != nil {
			// 把停用账号的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 拿离线包登录，失败说明包、时钟或口令不对。
		login, err := facA.LoginOffline(ctx, online, valid, "op-a", "op-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || login.Decision != factory.NodeDeny {
			// 停用之后仍能操作，说明停用没有生效。
			t.Fatalf("disable %+v %v", login, err)
		}
		// 按作者列出事实，失败说明无权查看名单。
		listed, err := facA.ListFactsByCreator(ctx, saTok, op.ID)
		// 列事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把列事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 留出甲侧计数，汇聚后用来核对有没有串厂。
		var nA int
		// 逐条检查这一批结果，任一条偏离即判失败。
		for _, f := range listed {
			// 事实应落在预期组织上，串了车间说明上下文偏了。
			if f.OrgUnitID != nil && *f.OrgUnitID == shopA.ID {
				// 甲侧条数加一，少计说明这条汇聚被漏掉。
				nA++
			}
		}
		// 条数应和预期一致，多或少说明名单没算对。
		if nA != 1 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("new old-range facts: %d", nA)
		}
		// 拉出审计流水，失败则无法核对有没有记账。
		rows, err := facA.ListAudit(ctx)
		// 拉审计失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把拉审计的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 审计应留下允许或拒绝，缺了说明这步没记账。
		if !audit.HasResult(rows, "create_fact", audit.Deny) || !audit.HasResult(rows, "create_fact", audit.Allow) {
			// 条目 14.1 没对上，这一条矩阵行为偏了。
			t.Fatal("14.1 audit missing")
		}
		// 审计应留下允许或拒绝，缺了说明这步没记账。
		if !audit.HasResult(rows, "person_open", audit.Deny) || !audit.HasResult(rows, "person_login", audit.Deny) {
			// 条目 14.1 没对上，这一条矩阵行为偏了。
			t.Fatal("14.1 intent audit missing")
		}
	})

	// 验收条目 14.3，断言不过表示这一条没过。
	run("14.3", func(t *testing.T) {
		// 标成已连接，用来区分在线作业和纯离线。
		offline.Connected = true
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, offline, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("reconnect %+v %v", ev, err)
		}
		// 按编号读工作事实，失败说明无权或已不存在。
		got, err := facA.GetFact(ctx, saTok, factA16.ID)
		// 读事实失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读事实的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 路径上的节点和名称应与当时组织一致，漂移即失败。
		if got.OrgUnitID == nil || *got.OrgUnitID != shopA.ID || pathName(got.OrgPath, shopA.ID) != "车间A" || pathHas(got.OrgPath, shopB.ID) {
			// 条目 16.1 没对上，这一条矩阵行为偏了。
			t.Fatalf("16.1 rewritten: %+v", got.OrgPath)
		}
	})
}
