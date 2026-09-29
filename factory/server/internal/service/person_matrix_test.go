// 阶段2：本厂有效账号在本厂设备上登录与操作 11.1～13.4；不签发人员授权。
package service_test

import (
	"context"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 有效账号在本厂设备上登录和操作，不另发人员授权。
func testPersonMatrix(t *testing.T, run func(string, func(*testing.T))) {
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
	// 建厂并签发超管激活码，失败则没有厂可测。
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sb-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
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

	// 新建厂内人员，失败说明登录名冲突或越权。
	op, err := facA.CreatePerson(ctx, saTok, "op-a", "操作员A")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	mustAdoptPassword(t, ctx, facA, "op-a", "op-pass")
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := facA.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 装出带许可的离线包，装错则后面登录无从判断。
	bagA := func() factory.Bag {
		// 按这把密钥装离线包，装错则登录对不上厂。
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 允许使用资产，仍拒绝说明离线包没带上许可。
		b.AssetAllowed = true
		// 套用运行授权，失败说明授权对不上这台。
		b.ApplyRuntime(runtime)
		return b
	}

	// 验收条目 11.1，断言不过表示这一条没过。
	run("11.1", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 拿离线包登录，失败说明包、时钟或口令不对。
		login, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || login.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("login %+v %v", login, err)
		}
		// 列出客户端，失败说明无权查看名录。
		listed, err := facA.ListClients(ctx, saTok)
		// 列客户端失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把列客户端的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 先当成没看见这个客户端，扫到再改成看见。
		var saw bool
		// 逐条检查这一批结果，任一条偏离即判失败。
		for _, row := range listed {
			// 应是另一条记录而不是同一条，撞号说明没另立。
			if row.ID == cidA {
				// 结果应和这一步的预期一致，偏离说明行为写偏了。
				if row.OperatorLogin != "op-a" || row.OperatorDisplay != "操作员A" {
					// 条目 11.1 没对上，这一条矩阵行为偏了。
					t.Fatalf("11.1 operator %+v", row)
				}
				// 记成已经看见，没记会把在线客户端判成缺失。
				saw = true
			}
		}
		// 扫完应该能找到，还没有说明结果里漏了这条。
		if !saw {
			// 条目 11.1 没对上，这一条矩阵行为偏了。
			t.Fatal("11.1 missing client")
		}
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("op %+v %v", ev, err)
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
	// 验收条目 12.1，断言不过表示这一条没过。
	run("12.1", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 拿离线包登录，失败说明包、时钟或口令不对。
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("no password %+v %v", ev, err)
		}
	})
	// 新取一个编号，撞号会让两条记录分不清。
	cidB := id.New()
	// 生成一把设备密钥，失败则绑定没有公钥可用。
	pubB, privB, err := nodekey.Generate()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登记绑定失败就停，否则后面没有可靠结果。
	if _, err := facB.AcceptBinding(ctx, cidB, "Client-B1", pubB, 1); err != nil {
		// 把登记绑定的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 取出签名公钥，空的说明登录没把钥匙发下来。
	facPubB, err := facB.SigningPublicKey(ctx)
	// 取签名公钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把取签名公钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	cidA2 := id.New()
	// 生成一把设备密钥，失败则绑定没有公钥可用。
	pubA2, privA2, err := nodekey.Generate()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登记绑定失败就停，否则后面没有可靠结果。
	if _, err := facA.AcceptBinding(ctx, cidA2, "Client-A2", pubA2, 1); err != nil {
		// 把登记绑定的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 验收条目 12.2，断言不过表示这一条没过。
	run("12.2", func(t *testing.T) {
		// 按这把密钥装离线包，装错则登录对不上厂。
		b := bagOf(seedA.ID, cidA2, pubA2, privA2, facPubA)
		// 拿离线包登录，失败说明包、时钟或口令不对。
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("same factory other device %+v %v", ev, err)
		}
	})
	// 新建厂内人员，失败说明登录名冲突或越权。
	_, err = facA.CreatePerson(ctx, saTok, "op-c", "操作员C")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	mustAdoptPassword(t, ctx, facA, "op-c", "c-pass")
	// 验收条目 12.3，断言不过表示这一条没过。
	run("12.3", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 拿离线包登录，失败说明包、时钟或口令不对。
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "c-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("other password %+v %v", ev, err)
		}
	})
	// 验收条目 12.4，断言不过表示这一条没过。
	run("12.4", func(t *testing.T) {
		// 按这把密钥装离线包，装错则登录对不上厂。
		b := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		// 拿离线包登录，失败说明包、时钟或口令不对。
		ev, err := facB.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("other factory %+v %v", ev, err)
		}
	})

	// 验收条目 13.1，断言不过表示这一条没过。
	run("13.1", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 清掉运行授权，仍能作业说明没检查授权还在不在。
		b.Runtime = nil
		// 拿离线包登录，失败说明包、时钟或口令不对。
		login, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || login.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("person still %+v %v", login, err)
		}
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 应存在的记录没有出现，说明这一步没落下。
			t.Fatalf("node missing %+v %v", ev, err)
		}
	})
	// 新建厂内人员，失败说明登录名冲突或越权。
	aud, err := facA.CreatePerson(ctx, saTok, "aud-a", "审计员")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	mustAdoptPassword(t, ctx, facA, "aud-a", "aud-pass")
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := facA.GrantRole(ctx, saTok, aud.ID, factory.RoleAuditor, factory.ScopeFactory, nil); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 验收条目 13.2，断言不过表示这一条没过。
	run("13.2", func(t *testing.T) {
		// 按这把密钥装离线包，装错则登录对不上厂。
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 允许使用资产，仍拒绝说明离线包没带上许可。
		b.AssetAllowed = true
		// 套用运行授权，失败说明授权对不上这台。
		b.ApplyRuntime(runtime)
		// 拿离线包登录，失败说明包、时钟或口令不对。
		login, err := facA.LoginOffline(ctx, b, valid, "aud-a", "aud-pass")
		// 离线登录失败或判定不对就停，说明没达预期。
		if err != nil || login.Decision != factory.NodeAllow {
			// 审计里缺了这条记录，说明这一步没有记账。
			t.Fatalf("auditor login %+v %v", login, err)
		}
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "aud-a", "aud-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 审计里缺了这条记录，说明这一步没有记账。
			t.Fatalf("auditor op %+v %v", ev, err)
		}
	})
	// 验收条目 13.3，断言不过表示这一条没过。
	run("13.3", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("all allow %+v %v", ev, err)
		}
	})
	// 验收条目 13.4，断言不过表示这一条没过。
	run("13.4", func(t *testing.T) {
		// 装出甲厂离线包，缺许可则后面作业无从判断。
		b := bagA()
		// 禁止使用资产，仍放行说明离线没检查许可。
		b.AssetAllowed = false
		// 离线判定能否作业，失败说明包或授权不齐。
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		// 离线判定失败或判定不对就停，说明没达预期。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("asset deny %+v %v", ev, err)
		}
	})
}
