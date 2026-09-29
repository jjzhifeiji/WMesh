// 第 7 圈：WAN 侧矩阵编号（账号 1.1～18.3 与节点 0.1～0.4）。
package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 账号和焊机要跑的编号，收尾用它查漏格。
var matrixIDs = []string{
	"0.1", "0.2", "0.3", "0.4",
	"1.1", "1.2", "1.3",
	"2.1", "2.2",
	"3.1", "3.2", "3.3",
	"17.1", "17.2", "17.3",
	"18.1", "18.2", "18.3",
}

// 把账号和焊机矩阵逐格跑完，漏号就失败。
func TestMatrix(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
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
		for _, id := range matrixIDs {
			// 这个编号必须跑过，漏跑矩阵就不完整。
			if !ran[id] {
				// 报出没跑到的编号，漏格不能当成已覆盖。
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 1.1：立云端超管应被拒为已有管理员，放行或错类都算没拦住。
	run("1.1", func(t *testing.T) {
		// 立云端超管应被拒为已有管理员，放行或错类都算没拦住。
		if err := h.WAN.BootstrapAdmin(ctx, "w2", "x"); !errors.Is(err, domain.ErrWANAdminExists) {
			// 不符即停：立云端超管应被拒为已有管理员。
			t.Fatalf("got %v", err)
		}
	})
	// 1.2：邀请云端管理员应被拒为越权，放行或错类都算没拦住。
	run("1.2", func(t *testing.T) {
		// 邀请云端管理员应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.InviteWANAdmin(ctx, "", "anyone"); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：邀请云端管理员应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 先留出位置，循环里找到再填，找不到就失败。
	var wanTok string
	// 1.3：登录拿会话失败就停，没有票后面接口都进不去。
	run("1.3", func(t *testing.T) {
		// 登录拿会话，没有票后面接口都进不去。
		tok, err := h.WAN.Login(ctx, "w", wanPass)
		// 登录拿会话失败就停，没有票后面接口都进不去。
		if err != nil {
			// 不符即停：登录拿会话失败。
			t.Fatal(err)
		}
		// 留下这一步的结果，后面几格接着用它。
		wanTok = tok
	})

	// 先留出位置，循环里找到再填，找不到就失败。
	var a global.CreatedFactory
	// 2.1：登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	run("2.1", func(t *testing.T) {
		// 预留错误位，后面这一步失败再停。
		var err error
		// 登记工厂「厂A」，建不成后面没有厂可授权。
		a, err = h.WAN.CreateFactory(ctx, wanTok, "厂A", "sa-a", "超管A")
		// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
		if err != nil {
			// 不符即停：登记工厂「厂A」失败。
			t.Fatal(err)
		}
	})
	// 2.2：再发初始超管应被拒为超管已发过，放行或错类都算没拦住。
	run("2.2", func(t *testing.T) {
		// 再发初始超管应被拒为超管已发过，放行或错类都算没拦住。
		if err := h.WAN.IssueInitialSuperAdmin(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrInitialSAExists) {
			// 不符即停：再发初始超管应被拒为超管已发过。
			t.Fatalf("got %v", err)
		}
	})

	// 登记工厂「厂B」，建不成后面没有厂可授权。
	facB, err := h.WAN.CreateFactory(ctx, wanTok, "厂B", "sa-b", "超管B")
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatal(err)
	}
	// 3.1：代建厂内人员应被拒为越权，放行或错类都算没拦住。
	run("3.1", func(t *testing.T) {
		// 代建厂内人员应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.CreateFactoryPerson(ctx, wanTok, a.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：代建厂内人员应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 3.2：代建厂内组织应被拒为越权，放行或错类都算没拦住。
	run("3.2", func(t *testing.T) {
		// 代建厂内组织应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.CreateFactoryOrg(ctx, wanTok, a.Factory.ID, "车间"); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：代建厂内组织应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 3.3：代授厂内角色应被拒为越权，放行或错类都算没拦住。
	run("3.3", func(t *testing.T) {
		// 代授厂内角色应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.GrantFactoryRole(ctx, wanTok, a.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：代授厂内角色应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 17.2：登录拿会话应被拒为口令不对，放行或错类都算没拦住。
	run("17.2", func(t *testing.T) {
		// 登录拿会话应被拒为口令不对，放行或错类都算没拦住。
		if _, err := h.WAN.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 不符即停：登录拿会话应被拒为口令不对。
			t.Fatal(err)
		}
	})
	// 17.3：登录拿会话应被拒为口令不对，放行或错类都算没拦住。
	run("17.3", func(t *testing.T) {
		// 修改口令失败就停，口令改不了旧票失效验不成。
		if err := h.WAN.ChangePassword(ctx, wanTok, "wan-secret-2"); err != nil {
			// 不符即停：修改口令失败。
			t.Fatal(err)
		}
		// 登录拿会话应被拒为口令不对，放行或错类都算没拦住。
		if _, err := h.WAN.Login(ctx, "w", wanPass); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 不符即停：登录拿会话应被拒为口令不对。
			t.Fatal(err)
		}
		// 登录拿会话，没有票后面接口都进不去。
		tok, err := h.WAN.Login(ctx, "w", "wan-secret-2")
		// 登录拿会话失败就停，没有票后面接口都进不去。
		if err != nil {
			// 不符即停：登录拿会话失败。
			t.Fatal(err)
		}
		// 留下这一步的结果，后面几格接着用它。
		wanTok = tok
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
			t.Fatalf("incomplete audit %#v", bad[0])
		}
		// 审计里不能出现口令或正文，出现就是泄密。
		if audit.ContainsAny(audit.Dump(rows), wanPass, "wan-secret-2", wanTok) {
			// 不符即停：审计里不能出现口令或正文。
			t.Fatalf("secret leaked")
		}
	})
	// 18.1：列厂内人员应被拒为越权，放行或错类都算没拦住。
	run("18.1", func(t *testing.T) {
		// 列厂内人员应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ListFactoryPeople(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：列厂内人员应被拒为越权。
			t.Fatalf("got %v", err)
		}
		// 逐项检查，漏一项这一步就不能算通过。
		for _, table := range []string{"people", "org_types", "org_units", "role_grants", "fact_stubs"} {
			ok, err := h.WAN.HasTable(ctx, table)
			if err != nil || ok {
				// 查库表是否存在失败就停，表不在就说明库被串了。
				t.Fatalf("%s present=%v err=%v", table, ok, err)
			}
		}
	})
	// 18.2：读厂内口令应被拒为越权，放行或错类都算没拦住。
	run("18.2", func(t *testing.T) {
		// 读厂内口令应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ReadAuthSecret(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂内口令应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 18.3：读工厂名录失败或条数不是2就停，不能当通过。
	run("18.3", func(t *testing.T) {
		// 读工厂名录，名录读不到就无法对厂。
		dir, err := h.WAN.Directory(ctx, wanTok)
		// 读工厂名录失败或条数不是2就停，不能当通过。
		if err != nil || len(dir.Factories) != 2 || len(dir.Initials) != 2 {
			// 不符即停：读工厂名录失败或条数不是2。
			t.Fatalf("%v %+v", err, dir)
		}
	})

	// 生成焊机密钥，没有公钥就无法登记。
	cPub, _, err := ed25519.GenerateKey(rand.Reader)
	// 生成焊机密钥失败就停，没有公钥就登记不了焊机。
	if err != nil {
		// 不符即停：生成焊机密钥失败。
		t.Fatal(err)
	}
	// 新造一个身份，后面用来区分是不是同一份。
	cid := id.New()
	// 先留出位置，循环里找到再填，找不到就失败。
	var bound global.Client
	// 0.1：绑定结果多项不符预期就停，说明没有按规则落下。
	run("0.1", func(t *testing.T) {
		// 预留错误位，后面这一步失败再停。
		var err error
		// 绑定焊机，绑不上厂焊机就没有归属。
		bound, err = h.WAN.BindClient(ctx, wanTok, cid, a.Factory.ID, "Client-A1", cPub)
		// 绑定焊机失败就停，绑不上厂焊机就没有归属。
		if err != nil {
			// 不符即停：绑定焊机失败。
			t.Fatal(err)
		}
		// 绑定结果多项不符预期就停，说明没有按规则落下。
		if bound.FactoryID == nil || *bound.FactoryID != a.Factory.ID || bound.BindingRevision != 1 || bound.Name != "Client-A1" {
			// 不符即停：绑定结果多项不符预期。
			t.Fatalf("bound %+v", bound)
		}
	})
	// 0.2：绑定焊机应被拒为焊机已绑厂，放行或错类都算没拦住。
	run("0.2", func(t *testing.T) {
		// 绑定焊机应被拒为焊机已绑厂，放行或错类都算没拦住。
		if _, err := h.WAN.BindClient(ctx, wanTok, cid, facB.Factory.ID, "Client-A1", cPub); !errors.Is(err, domain.ErrClientBound) {
			// 不符即停：绑定焊机应被拒为焊机已绑厂。
			t.Fatalf("got %v", err)
		}
		// 按身份读焊机，按身份读不到焊机就对不上。
		cur, err := h.WAN.ClientByID(ctx, cid)
		// 按身份读焊机失败或不该是空的就停，不能当通过。
		if err != nil || cur.FactoryID == nil || *cur.FactoryID != a.Factory.ID {
			// 不符即停：按身份读焊机失败或不该是空的。
			t.Fatalf("still A: %+v %v", cur, err)
		}
	})
	// 0.3：这里多项不符预期就停，说明没有按规则落下。
	run("0.3", func(t *testing.T) {
		// 改绑焊机，改绑失败焊机还挂在原厂。
		reb, err := h.WAN.RebindClient(ctx, wanTok, cid, facB.Factory.ID)
		// 改绑焊机失败就停，改绑失败焊机还挂在原厂。
		if err != nil {
			// 不符即停：改绑焊机失败。
			t.Fatal(err)
		}
		// 这里多项不符预期就停，说明没有按规则落下。
		if reb.FactoryID == nil || *reb.FactoryID != facB.Factory.ID || reb.BindingRevision <= bound.BindingRevision {
			// 不符即停：这里多项不符预期。
			t.Fatalf("rebind %+v", reb)
		}
	})
	// 0.4：这里应被拒为越权，放行或错类都算没拦住。
	run("0.4", func(t *testing.T) {
		// 这里应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ListPersonOfflineGrants(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：这里应被拒为越权。
			t.Fatalf("got %v", err)
		}
		// 代建厂内组织应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.CreateFactoryOrg(ctx, wanTok, a.Factory.ID, "车间"); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：代建厂内组织应被拒为越权。
			t.Fatalf("org %v", err)
		}
		// 读厂内口令应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ReadAuthSecret(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂内口令应被拒为越权。
			t.Fatalf("secret %v", err)
		}
		// 查库表是否存在，表不在就说明库被串了。
		ok, err := h.WAN.HasTable(ctx, "person_offline_grants")
		// 查库表是否存在失败就停，表不在就说明库被串了。
		if err != nil || ok {
			// 不符即停：查库表是否存在失败。
			t.Fatalf("offline grants table present=%v err=%v", ok, err)
		}
	})
}
