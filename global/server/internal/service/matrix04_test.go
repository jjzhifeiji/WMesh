// 阶段4第7圈：WAN 侧下发授权与平台级闭包 5.1～5.4、15.2、17.1～17.2。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

// 下发闭包要跑的编号，收尾用它查漏格。
var matrix04IDs = []string{
	"5.1", "5.2", "5.3", "5.4",
	"15.2",
	"17.1", "17.2",
}

// 把下发与闭包矩阵逐格跑完，漏号就失败。
func TestMatrix04(t *testing.T) {
	// 记下已跑编号，收尾靠它发现漏掉的格。
	ran := map[string]bool{}
	// 按编号开子测试并记已跑，漏记收尾会误报。
	run := func(id string, fn func(*testing.T)) {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 这个编号单独成格，失败不连坐其它格。
		t.Run(id, func(t *testing.T) {
			// 标这一格已跑，漏标会在收尾被当成没跑。
			ran[id] = true
			// 执行这一格的断言，失败只记在这个编号。
			fn(t)
		})
	}
	// 收尾核对矩阵编号都跑过，漏号就失败。
	t.Cleanup(func() {
		// 逐个矩阵编号核对，漏一个就不算覆盖完。
		for _, id := range matrix04IDs {
			// 这个编号必须跑过，漏跑矩阵就不完整。
			if !ran[id] {
				// 报出没跑到的编号，漏格不能当成已覆盖。
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	facA, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 登记工厂「厂B」，建不成后面没有厂可授权。
	facB, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	body := []byte("wan-closure-secret")
	// 新建平台工艺「平台工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "平台工艺", body)
	// 新建平台工艺「平台工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「平台工艺」失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 读取资产，读不到就无法核对原件。
	proc, err = h.WAN.GetPlatformAsset(ctx, tok, proc.ID)
	// 读取资产失败就停，读不到就无法核对原件。
	if err != nil {
		// 不符即停：读取资产失败。
		t.Fatal(err)
	}
	// 新建平台工程「平台工程」，建不成后面没有工程可引用。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "平台工程", []byte("proj"), []global.AssetDep{{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}})
	// 新建平台工程「平台工程」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「平台工程」失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	proj, err = h.WAN.PublishPlatformAsset(ctx, tok, proj.ID, proj.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}

	// 5.2：下发到厂应被拒为越权，放行或错类都算没拦住。
	run("5.2", func(t *testing.T) {
		// 下发到厂应被拒为越权，放行或错类都算没拦住。
		if _, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
			// 不符即停：下发到厂应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 5.4：下发到厂失败或包种类不对就停，不能当通过。
	run("5.4", func(t *testing.T) {
		// 授权给厂失败就停，授权失败厂侧用不了。
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proc.ID, facA.Factory.ID); err != nil {
			// 不符即停：授权给厂失败。
			t.Fatal(err)
		}
		// 下发到厂，下发失败厂侧没有这份。
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proc.ID, facA.Factory.ID)
		// 下发到厂失败或包种类不对就停，不能当通过。
		if err != nil || snap.Kind != global.KindProcess || snap.AssetID != proc.ID || len(snap.Members) != 1 {
			// 不符即停：下发到厂失败或包种类不对。
			t.Fatalf("%+v %v", snap, err)
		}
	})
	// 5.1：组包多项不符预期就停，说明没有按规则落下。
	run("5.1", func(t *testing.T) {
		// 授权给厂失败就停，授权失败厂侧用不了。
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proj.ID, facA.Factory.ID); err != nil {
			// 不符即停：授权给厂失败。
			t.Fatal(err)
		}
		// 下发到厂，下发失败厂侧没有这份。
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID)
		// 组包多项不符预期就停，说明没有按规则落下。
		if err != nil || snap.Kind != global.KindProject || snap.AssetID != proj.ID || len(snap.Members) != 2 || !snap.Copyable {
			// 不符即停：组包多项不符预期。
			t.Fatalf("%+v %v", snap, err)
		}
		// 组包目标厂不对就停，这一步不能算通过。
		if snap.TargetFactoryID == nil || *snap.TargetFactoryID != facA.Factory.ID {
			// 不符即停：组包目标厂不对。
			t.Fatalf("target %+v", snap.TargetFactoryID)
		}
	})
	// 5.3：查是否已下发失败就停，查不到是否下发就分不清。
	run("5.3", func(t *testing.T) {
		// 授权给厂失败就停，授权失败厂侧用不了。
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proj.ID, facB.Factory.ID); err != nil {
			// 不符即停：授权给厂失败。
			t.Fatal(err)
		}
		// 下发到厂失败就停，下发失败厂侧没有这份。
		if _, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facB.Factory.ID); err != nil {
			// 不符即停：下发到厂失败。
			t.Fatal(err)
		}
		// 查是否已下发，查不到是否下发就分不清。
		okA, err := h.WAN.HasDistributedTo(ctx, proj.ID, facA.Factory.ID)
		// 查是否已下发失败就停，查不到是否下发就分不清。
		if err != nil || !okA {
			// 不符即停：查是否已下发失败。
			t.Fatalf("A %v %v", okA, err)
		}
		// 查是否已下发，查不到是否下发就分不清。
		okB, err := h.WAN.HasDistributedTo(ctx, proj.ID, facB.Factory.ID)
		// 查是否已下发失败就停，查不到是否下发就分不清。
		if err != nil || !okB {
			// 不符即停：查是否已下发失败。
			t.Fatalf("B %v %v", okB, err)
		}
	})
	// 15.2：下发到厂失败或修订没有跟上就停，不能当通过。
	run("15.2", func(t *testing.T) {
		// 下发到厂，下发失败厂侧没有这份。
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID)
		// 下发到厂失败或修订没有跟上就停，不能当通过。
		if err != nil || snap.Revision != proj.Revision {
			// 不符即停：下发到厂失败或修订没有跟上。
			t.Fatalf("%+v %v", snap, err)
		}
	})
	// 17.1：审计记录缺字段就停，以后对不了账。
	run("17.1", func(t *testing.T) {
		// 读审计，读不到就无法核对有没有记。
		rows, err := h.WAN.ListAudit(ctx)
		// 读审计失败就停，读不到就无法核对有没有记。
		if err != nil {
			// 不符即停：读审计失败。
			t.Fatal(err)
		}
		// 审计记录缺字段就停，以后对不了账。
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			// 不符即停：审计记录缺字段。
			t.Fatalf("incomplete %#v", bad[0])
		}
		// 审计里不能出现口令或正文，出现就是泄密。
		if audit.ContainsAny(audit.Dump(rows), string(body), wanPass, tok) {
			// 不符即停：审计里不能出现口令或正文。
			t.Fatalf("secret leaked")
		}
	})
	// 17.2：审计必须记下这一笔，缺了就没法对账。
	run("17.2", func(t *testing.T) {
		// 读审计，读不到就无法核对有没有记。
		rows, err := h.WAN.ListAudit(ctx)
		// 读审计失败就停，读不到就无法核对有没有记。
		if err != nil {
			// 不符即停：读审计失败。
			t.Fatal(err)
		}
		// 审计必须记下这一笔，缺了就没法对账。
		if !audit.HasResult(rows, "distribute_closure", audit.Deny) {
			// 不符即停：审计必须记下这一笔。
			t.Fatal("need unauthorized deny")
		}
	})
}
