// 第 7 圈：厂内侧矩阵编号。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 本文件要跑完的账号编号，漏跑就在收尾报错
var matrixIDs = []string{
	"2.1", "2.3", "2.4",
	"4.1", "4.2",
	"5.1", "5.2", "5.3", "5.4",
	"6.1", "6.2", "6.3", "6.4", "6.5", "6.6",
	"7.1", "7.2",
	"8.1", "8.2", "8.3", "8.4",
	"9.1", "9.2", "9.3", "9.4", "9.5", "9.6",
	"10.1", "10.2", "10.3", "10.4", "10.5", "10.6", "10.7", "10.8", "10.9", "10.10",
	"11.2", "11.3", "11.4", "11.5",
	"12.1",
	"13.1", "13.2", "13.3",
	"14.1", "14.2", "14.3",
	"15.1", "15.2", "15.3",
	"16.1", "16.2", "16.3",
	"17.1", "17.2", "17.3",
}

// 跑厂内账号组织矩阵，漏编号就在收尾报错
func TestMatrix(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 记下跑过的编号，收尾用来查有没有漏
	ran := map[string]bool{}
	// 按编号启动子测试并记账，漏跑收尾能发现
	run := func(id string, fn func(*testing.T)) {
		// 标成辅助函数，失败栈落到调用方
		t.Helper()
		// 把这一编号跑成独立子测试，失败只记这一条
		t.Run(id, func(t *testing.T) {
			// 标上这个编号已跑，漏标会被收尾抓住
			ran[id] = true
			// 跑这一编号的子测试，失败由它自己报出
			fn(t)
		})
	}
	// 收尾检查编号是否都跑过，漏跑就要报错
	t.Cleanup(func() {
		// 逐个核对应跑的编号，漏跑要在收尾报错
		for _, id := range matrixIDs {
			// 这个编号没跑到就要报错，矩阵不算完成
			if !ran[id] {
				// 漏跑的编号要报出来，否则矩阵不算完成
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	a, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收建厂后只有一名待启用超管，多出来即失败
	run("2.1", func(t *testing.T) {
		// 人员必须只有一名，多或少都算建厂不对
		if n, _ := facA.Store().PersonCount(ctx); n != 1 {
			// 超管人数不是一名就失败，建厂结果不对
			t.Fatalf("sa count %d", n)
		}
	})
	// 验收激活前待启用超管不算有效入口，算了即失败
	run("10.7", func(t *testing.T) {
		// 按身份读人员，失败则本步验收不能继续
		p, err := facA.Store().PersonByID(ctx, a.SuperAdminID)
		// 按身份读人员失败就停，避免带着错误继续验
		if err != nil || p.Status != factory.StatusPending {
			// 激活前不是待启用就失败，入口被提前算有效
			t.Fatalf("pending sa: %+v %v", p, err)
		}
	})
	// 验收待启用账号不能登录或操作，放行即失败
	run("10.1", func(t *testing.T) {
		// 期望登录被拒为待启用，放行即失败
		if _, err := facA.Login(ctx, "sa-a", "no"); !errors.Is(err, domain.ErrAccountPending) {
			// 登录结果不对就失败
			t.Fatalf("login: %v", err)
		}
		// 期望校验会话仍有效被拒为未授权，放行即失败
		if _, err := facA.RequireActive(ctx, "none"); !errors.Is(err, domain.ErrUnauthorized) {
			// 受保护操作没有被拒就失败
			t.Fatalf("protect: %v", err)
		}
	})
	// 留出超管令牌，激活登录成功后再写上
	var saA string
	// 验收激活后能登录本厂，登不上即失败
	run("2.4", func(t *testing.T) {
		// 激活初始超管失败就停，避免带着错误继续验
		if err := facA.Activate(ctx, "sa-a", a.ActivationToken, "sa-pass"); err != nil {
			// 激活初始超管失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 登录，失败则本步验收不能继续
		tok, err := facA.Login(ctx, "sa-a", "sa-pass")
		// 登录失败就停，避免带着错误继续验
		if err != nil {
			// 登录失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 记下登录令牌，后面的管理操作都凭它
		saA = tok
	})
	// 验收激活改密登录成功且审计无秘密，泄密即失败
	run("17.3", func(t *testing.T) {
		// 修改密码失败就停，避免带着错误继续验
		if err := facA.ChangePassword(ctx, saA, "sa-pass-2"); err != nil {
			// 修改密码失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 登录取出令牌，登不上则后面没有身份
		saA = mustLogin(t, ctx, facA, "sa-a", "sa-pass-2")
	})

	// 建厂并种初始超管，失败则本步验收不能继续
	b, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := facB.Activate(ctx, "sa-b", b.ActivationToken, "sb-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saB := mustLogin(t, ctx, facB, "sa-b", "sb-pass")

	// 验收超管凭证不能登录他厂，登进去即失败
	run("2.3", func(t *testing.T) {
		// 期望登录被拒为凭证不对，放行即失败
		if _, err := facB.Login(ctx, "sa-a", "sa-pass-2"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 他厂登录没有被拒就失败
			t.Fatalf("login B: %v", err)
		}
		// 期望校验会话仍有效被拒为未授权，放行即失败
		if _, err := facB.RequireActive(ctx, saA); !errors.Is(err, domain.ErrUnauthorized) {
			// 对他厂的管理没有被拒就失败
			t.Fatalf("manage B: %v", err)
		}
		// 期望记一条事实被拒为未授权，放行即失败
		if _, err := facB.CreateFact(ctx, saA, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("fact B: %v", err)
		}
	})

	// 留出组织位，建树成功后给分配和越权用
	var site, shop, shopB, line, team, spare factory.OrgUnit
	// 验收超管能建组织和人员，建不成即失败
	run("4.1", func(t *testing.T) {
		// 留出错误位，供这一步把失败写回来
		var err error
		// 建立组织，失败则本步验收不能继续
		site, err = facA.CreateOrgUnit(ctx, saA, "场地", nil)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收能建多层组织树，建不成即失败
	run("5.1", func(t *testing.T) {
		// 留出错误位，供这一步把失败写回来
		var err error
		// 建立组织，失败则本步验收不能继续
		shop, err = facA.CreateOrgUnit(ctx, saA, "车间", &site.ID)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 建立组织，失败则本步验收不能继续
		shopB, err = facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 建立组织，失败则本步验收不能继续
		line, err = facA.CreateOrgUnit(ctx, saA, "产线", &shop.ID)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 建立组织，失败则本步验收不能继续
		team, err = facA.CreateOrgUnit(ctx, saA, "班组", &line.ID)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 建立组织，失败则本步验收不能继续
		spare, err = facA.CreateOrgUnit(ctx, saA, "备用叶", &shopB.ID)
		// 建立组织失败就停，避免带着错误继续验
		if err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收改显示名不改稳定身份，身份变了即失败
	run("4.2", func(t *testing.T) {
		// 校验会话仍有效，失败则本步验收不能继续
		old, err := facA.RequireActive(ctx, saA)
		// 校验会话仍有效失败就停，避免带着错误继续验
		if err != nil {
			// 校验会话仍有效失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 改显示名失败就停，避免带着错误继续验
		if err := facA.Rename(ctx, saA, "超管A改名", "sa-a"); err != nil {
			// 改显示名失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 校验会话仍有效，失败则本步验收不能继续
		got, err := facA.RequireActive(ctx, saA)
		// 校验会话仍有效失败就停，避免带着错误继续验
		if err != nil || got.ID != old.ID {
			// 已下发成员修订漂了就失败，旧快照不该变
			t.Fatalf("id drifted %+v %v", got, err)
		}
	})
	// 验收组织不能挂到自己或后代，挂上即失败
	run("5.4", func(t *testing.T) {
		// 期望改挂父组织被拒为成环，放行即失败
		if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &site.ID); !errors.Is(err, domain.ErrCycle) {
			// 把自己挂到自己没有被拒就失败
			t.Fatalf("self: %v", err)
		}
		// 期望改挂父组织被拒为成环，放行即失败
		if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &team.ID); !errors.Is(err, domain.ErrCycle) {
			// 名称或说明不对就失败
			t.Fatalf("desc: %v", err)
		}
	})
	// 验收同一节点不能有两个父级，加上即失败
	run("5.3", func(t *testing.T) {
		// 期望再加一个父组织被拒为多个父级，放行即失败
		if err := facA.AddParent(ctx, saA, shop.ID, shopB.ID); !errors.Is(err, domain.ErrMultiParent) {
			// 没拒成多个父级就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 建立组织，失败则本步验收不能继续
	bUnit, err := facB.CreateOrgUnit(ctx, saB, "厂B节点", nil)
	// 建立组织失败就停，避免带着错误继续验
	if err != nil {
		// 建立组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收组织不能挂到他厂节点下，挂上即失败
	run("5.2", func(t *testing.T) {
		// 期望改挂父组织被拒为不存在，放行即失败
		if err := facA.ReparentOrgUnit(ctx, saA, shop.ID, &bUnit.ID); !errors.Is(err, domain.ErrNotFound) {
			// 没拒成不存在就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 建立人员，失败则本步验收不能继续
	lone, err := facA.CreatePerson(ctx, saA, "lone", "未分配")
	// 建立人员失败就停，避免带着错误继续验
	if err != nil {
		// 建立人员失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收人员可以不分配组织，这一步被拒即失败
	run("6.1", func(t *testing.T) {
		// 清点分配数，失败则本步验收不能继续
		n, err := facA.Store().AssignmentCount(ctx, lone.ID)
		// 清点分配数失败就停，避免带着错误继续验
		if err != nil || n != 0 {
			// 分配结果不对就失败
			t.Fatalf("assign %d %v", n, err)
		}
	})
	// 建立人员，失败则本步验收不能继续
	p, err := facA.CreatePerson(ctx, saA, "p", "人员P")
	// 建立人员失败就停，避免带着错误继续验
	if err != nil {
		// 建立人员失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 设好登录密码，设失败则这个人登不上
	pTok := mustAdoptPassword(t, ctx, facA, "p", "p-pass")
	// 验收人员可以只分配一个组织，没分上即失败
	run("6.2", func(t *testing.T) {
		// 分配到组织失败就停，避免带着错误继续验
		if err := facA.Assign(ctx, saA, p.ID, shop.ID); err != nil {
			// 分配到组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收已分配后再分第二个组织要拒绝，分上即失败
	run("6.3", func(t *testing.T) {
		// 期望分配到组织被拒为重复分配，放行即失败
		if err := facA.Assign(ctx, saA, p.ID, shopB.ID); !errors.Is(err, domain.ErrDuplicateAssignment) {
			// 没拒成重复分配就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收不能把人分配到他厂组织，分上即失败
	run("6.4", func(t *testing.T) {
		// 期望分配到组织被拒为不存在，放行即失败
		if err := facA.Assign(ctx, saA, p.ID, bUnit.ID); !errors.Is(err, domain.ErrNotFound) {
			// 没拒成不存在就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收本厂凭证不能登录他厂，登进去即失败
	run("6.5", func(t *testing.T) {
		// 期望登录被拒为凭证不对，放行即失败
		if _, err := facB.Login(ctx, "p", "p-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 没拒成凭证不对就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收他厂可以建同名登录，身份必须不同
	run("6.6", func(t *testing.T) {
		// 建立人员，失败则本步验收不能继续
		same, err := facB.CreatePerson(ctx, saB, "p", "厂B同名")
		// 建立人员失败就停，避免带着错误继续验
		if err != nil || same.ID == p.ID {
			// 结果不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, same)
		}
	})
	// 验收未分配的人不能选组织做事实，做成即失败
	run("7.1", func(t *testing.T) {
		// 期望记一条事实被拒为越权，放行即失败
		if _, err := facA.CreateFact(ctx, pTok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("fact: %v", err)
		}
		// 期望建个人资产被拒为越权，放行即失败
		if _, err := facA.CreatePersonalAsset(ctx, pTok, factory.WorkContext{OrgUnitID: &shop.ID}, "x"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("asset: %v", err)
		}
	})
	// 验收未分配的人不能在节点上操作，做成即失败
	run("7.2", func(t *testing.T) {
		// 期望在节点上操作被拒为越权，放行即失败
		if err := facA.Operate(ctx, pTok, shop.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 建人并授角色，失败则夹具缺这个身份
	oa := mustCreateRole(t, ctx, facA, saA, "oa", "oa-pass", factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shop.ID)
	// 验收组织管理员能在子树内管组织，管不成即失败
	run("8.1", func(t *testing.T) {
		// 建立组织失败就停，避免带着错误继续验
		if _, err := facA.CreateOrgUnit(ctx, oa.tok, "线2", &shop.ID); err != nil {
			// 建立组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收管理员在自己子树内可以管，管不成即失败
	run("9.5", func(t *testing.T) {
		// 取消组织分配失败就停，避免带着错误继续验
		if err := facA.Unassign(ctx, oa.tok, p.ID, shop.ID); err != nil {
			// 取消组织分配失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 分配到组织失败就停，避免带着错误继续验
		if err := facA.Assign(ctx, oa.tok, p.ID, line.ID); err != nil {
			// 分配到组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收管理员选本节点做事实要拒绝，做成即失败
	run("8.3", func(t *testing.T) {
		// 期望记一条事实被拒为工作上下文不对，放行即失败
		if _, err := facA.CreateFact(ctx, oa.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	// 建人并授角色，失败则夹具缺这个身份
	lead := mustCreateRole(t, ctx, facA, saA, "lead", "lead-pass", factory.RoleOrgLead, factory.ScopeOrgUnit, &shop.ID)
	// 建人并授角色，失败则夹具缺这个身份
	aud := mustCreateRole(t, ctx, facA, saA, "aud", "aud-pass", factory.RoleAuditor, factory.ScopeOrgUnit, &shop.ID)
	// 验收负责人只能看不能改，改成即失败
	run("8.2", func(t *testing.T) {
		// 查看组织失败就停，避免带着错误继续验
		if err := facA.ViewOrg(ctx, lead.tok, shop.ID); err != nil {
			// 查看组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 查看组织失败就停，避免带着错误继续验
		if err := facA.ViewOrg(ctx, aud.tok, shop.ID); err != nil {
			// 查看组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 建人并授角色，失败则夹具缺这个身份
	eng := mustCreateRole(t, ctx, facA, saA, "eng", "eng-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	// 验收操作员未分配该节点不能做事实，做成即失败
	run("8.4", func(t *testing.T) {
		// 期望记一条事实被拒为工作上下文不对，放行即失败
		if _, err := facA.CreateFact(ctx, eng.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	// 验收管理员不能管兄弟或父节点，管成即失败
	run("9.1", func(t *testing.T) {
		// 期望建立组织被拒为越权，放行即失败
		if _, err := facA.CreateOrgUnit(ctx, oa.tok, "越界", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收负责人不能改组织或授角色，改成即失败
	run("9.2", func(t *testing.T) {
		// 期望建立组织被拒为越权，放行即失败
		if _, err := facA.CreateOrgUnit(ctx, lead.tok, "x", &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 期望建立人员被拒为越权，放行即失败
		if _, err := facA.CreatePerson(ctx, lead.tok, "x", "x"); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, facA, saA, "op", "op-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	// 验收操作员不能建账号或授角色，做成即失败
	run("9.3", func(t *testing.T) {
		// 期望建立人员被拒为越权，放行即失败
		if _, err := facA.CreatePerson(ctx, op.tok, "y", "y"); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 期望授予角色被拒为越权，放行即失败
		if _, err := facA.GrantRole(ctx, op.tok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 期望重置密码被拒为越权，放行即失败
		if _, err := facA.ResetPassword(ctx, op.tok, p.ID); !errors.Is(err, domain.ErrForbidden) {
			// 重置口令的结果不对就失败
			t.Fatalf("reset: %v", err)
		}
	})
	// 验收审计员不能做事实或改组织，做成即失败
	run("9.4", func(t *testing.T) {
		// 期望记一条事实被拒为这几种原因之一，放行即失败
		if _, err := facA.CreateFact(ctx, aud.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		// 期望建立组织被拒为越权，放行即失败
		if _, err := facA.CreateOrgUnit(ctx, aud.tok, "z", &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收不能把工厂超管授到子树外，授成即失败
	run("9.6", func(t *testing.T) {
		// 期望授予角色被拒为越权，放行即失败
		if _, err := facA.GrantRole(ctx, oa.tok, p.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 期望授予角色被拒为越权，放行即失败
		if _, err := facA.GrantRole(ctx, oa.tok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 验收还有有效子节点时不能停用，停掉即失败
	run("11.2", func(t *testing.T) {
		// 期望停用组织被拒为还有有效下级，放行即失败
		if err := facA.DisableOrgUnit(ctx, saA, shop.ID); !errors.Is(err, domain.ErrHasActiveChildren) {
			// 没拒成还有有效下级就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收下级已停用后可以停用本节点，停不成即失败
	run("11.3", func(t *testing.T) {
		// 停用组织失败就停，避免带着错误继续验
		if err := facA.DisableOrgUnit(ctx, saA, spare.ID); err != nil {
			// 停用组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取所属组织，失败则本步验收不能继续
		u, err := facA.Store().Unit(ctx, spare.ID)
		// 读取所属组织失败就停，避免带着错误继续验
		if err != nil || u.Status != factory.StatusDisabled {
			// 该留下的版本没留下就失败
			t.Fatalf("not kept %+v %v", u, err)
		}
	})
	// 验收已停用节点不能再分配或当上下文，做成即失败
	run("11.5", func(t *testing.T) {
		// 期望分配到组织被拒为组织已停用，放行即失败
		if err := facA.Assign(ctx, saA, p.ID, spare.ID); !errors.Is(err, domain.ErrDisabledOrgUnit) {
			// 分配结果不对就失败
			t.Fatalf("assign: %v", err)
		}
		// 期望记一条事实被拒为这几种原因之一，放行即失败
		if _, err := facA.CreateFact(ctx, pTok, factory.WorkContext{OrgUnitID: &spare.ID}); !errors.Is(err, domain.ErrDisabledOrgUnit) && !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("ctx: %v", err)
		}
	})

	// 建人并授角色，失败则夹具缺这个身份
	q := mustCreateRole(t, ctx, facA, saA, "q", "q-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 分配到组织失败就停，避免带着错误继续验
	if err := facA.Assign(ctx, saA, q.acc.ID, shop.ID); err != nil {
		// 分配到组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 留下事实桩，改挂后核对旧路径没被改写
	var factA factory.FactStub
	// 验收直属厂的事实不挂组织，挂上节点即失败
	run("16.1", func(t *testing.T) {
		// 记一条事实，失败则本步验收不能继续
		direct, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{Direct: true})
		// 记一条事实失败就停，避免带着错误继续验
		if err != nil || direct.OrgUnitID != nil || len(direct.OrgPath) != 0 {
			// 路径不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, direct)
		}
	})
	// 验收只有组织范围的人不能选直属，选成即失败
	run("16.2", func(t *testing.T) {
		// 期望记一条事实被拒为越权，放行即失败
		if _, err := facA.CreateFact(ctx, op.tok, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	// 验收未分配的人不能选组织当上下文，选成即失败
	run("16.3", func(t *testing.T) {
		// 期望记一条事实被拒为工作上下文不对，放行即失败
		if _, err := facA.CreateFact(ctx, oa.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	// 验收事实只记下当时路径，串到别的节点即失败
	run("12.1", func(t *testing.T) {
		// 留出错误位，供这一步把失败写回来
		var err error
		// 记一条事实，失败则本步验收不能继续
		factA, err = facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID})
		// 记一条事实失败就停，避免带着错误继续验
		if err != nil {
			// 记一条事实失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 路径必须符合期望，对不上即失败
		if factA.OrgUnitID == nil || *factA.OrgUnitID != shop.ID || !pathHas(factA.OrgPath, site.ID) || pathHas(factA.OrgPath, shopB.ID) {
			// 路径快照不对就失败，历史被改写了
			t.Fatalf("path %+v", factA)
		}
	})
	// 准备一段个人正文，审计里出现它即泄密失败
	const payload = "personal-secret"
	asset, err := facA.CreatePersonalAsset(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID}, payload)
	// 建个人资产失败就停，避免带着错误继续验
	if err != nil {
		// 建个人资产失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收调整分配不改历史事实，历史变了即失败
	run("13.3", func(t *testing.T) {
		// 取消组织分配失败就停，避免带着错误继续验
		if err := facA.Unassign(ctx, saA, q.acc.ID, shop.ID); err != nil {
			// 取消组织分配失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 分配到组织失败就停，避免带着错误继续验
		if err := facA.Assign(ctx, saA, q.acc.ID, shopB.ID); err != nil {
			// 分配到组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收改挂后旧事实路径不变，被改写即失败
	run("13.1", func(t *testing.T) {
		// 读取事实，失败则本步验收不能继续
		old, err := facA.GetFact(ctx, q.tok, factA.ID)
		// 读取事实失败就停，避免带着错误继续验
		if err != nil || *old.OrgUnitID != shop.ID || pathHas(old.OrgPath, shopB.ID) {
			// 路径不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, old)
		}
	})
	// 验收改挂后新事实走新路径，仍写旧路径即失败
	run("13.2", func(t *testing.T) {
		// 记一条事实，失败则本步验收不能继续
		nb, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shopB.ID})
		// 记一条事实失败就停，避免带着错误继续验
		if err != nil || *nb.OrgUnitID != shopB.ID {
			// 结果不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, nb)
		}
	})
	// 取消组织分配失败就停，避免带着错误继续验
	if err := facA.Unassign(ctx, saA, q.acc.ID, shopB.ID); err != nil {
		// 取消组织分配失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 分配到组织失败就停，避免带着错误继续验
	if err := facA.Assign(ctx, saA, q.acc.ID, shop.ID); err != nil {
		// 分配到组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收改名换父级本身不改历史，历史变了即失败
	run("14.3", func(t *testing.T) {
		// 给组织改名失败就停，避免带着错误继续验
		if err := facA.RenameOrgUnit(ctx, saA, shop.ID, "车间改名"); err != nil {
			// 给组织改名失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 改挂父组织失败就停，避免带着错误继续验
		if err := facA.ReparentOrgUnit(ctx, saA, shop.ID, &shopB.ID); err != nil {
			// 改挂父组织失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收改名后旧事实仍是当时名称，被改写即失败
	run("14.1", func(t *testing.T) {
		// 读取事实，失败则本步验收不能继续
		old, err := facA.GetFact(ctx, saA, factA.ID)
		// 读取事实失败就停，避免带着错误继续验
		if err != nil || pathName(old.OrgPath, shop.ID) != "车间" || pathHas(old.OrgPath, shopB.ID) {
			// 路径不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, old)
		}
	})
	// 验收换父级后新事实用新路径，仍写旧路径即失败
	run("14.2", func(t *testing.T) {
		// 记一条事实，失败则本步验收不能继续
		fresh, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID})
		// 记一条事实失败就停，避免带着错误继续验
		if err != nil || pathName(fresh.OrgPath, shop.ID) != "车间改名" || !pathHas(fresh.OrgPath, shopB.ID) {
			// 路径不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, fresh)
		}
	})
	// 验收个人资产仍属创建人，归属被改即失败
	run("15.1", func(t *testing.T) {
		// 读取个人资产，失败则本步验收不能继续
		got, err := facA.GetPersonalAsset(ctx, q.tok, asset.ID)
		// 读取个人资产失败就停，避免带着错误继续验
		if err != nil || got.CreatorID != q.acc.ID || pathName(got.OrgPath, shop.ID) != "车间" {
			// 路径不符就把实际值打出并停掉本例
			t.Fatalf("%v %+v", err, got)
		}
	})
	// 验收停用不得把个人资产改挂给超管，改了即失败
	run("15.2", func(t *testing.T) {
		// 期望移交个人资产被拒为越权，放行即失败
		if err := facA.TransferPersonalAsset(ctx, saA, asset.ID, a.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 期望改写事实路径被拒为越权，放行即失败
		if err := facA.RewriteFactPath(ctx, saA, factA.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收超管不能打开别人的个人资产正文，打开即失败
	run("15.3", func(t *testing.T) {
		// 期望读个人资产正文被拒为越权，放行即失败
		if _, err := facA.ReadPersonalAssetContent(ctx, saA, asset.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 读个人资产正文，失败则本步验收不能继续
		body, err := facA.ReadPersonalAssetContent(ctx, q.tok, asset.ID)
		// 读个人资产正文失败就停，避免带着错误继续验
		if err != nil || body != payload {
			// 结果不符就把实际值打出并停掉本例
			t.Fatalf("%v %q", err, body)
		}
	})
	// 验收已被引用的组织不能物理删掉，删掉即失败
	run("11.4", func(t *testing.T) {
		// 判断是不是外键挡住删除的结果必须符合期望，不符即失败
		if err := facA.Store().TryDeleteOrgUnit(ctx, shop.ID); !domain.IsForeignKeyViolation(err) {
			// 判断是不是外键挡住删除不符就把实际值打出并停掉
			t.Fatalf("got %v", err)
		}
	})

	// 验收退出后原会话不能再操作，放行即失败
	run("10.3", func(t *testing.T) {
		// 退出登录失败就停，避免带着错误继续验
		if err := facA.Logout(ctx, pTok); err != nil {
			// 退出登录失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望校验会话仍有效被拒为未授权，放行即失败
		if _, err := facA.RequireActive(ctx, pTok); !errors.Is(err, domain.ErrUnauthorized) {
			// 没拒成未授权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 登录取出令牌，登不上则后面没有身份
		pTok = mustLogin(t, ctx, facA, "p", "p-pass")
	})
	// 验收停用或收权后原会话立刻失效，还能用即失败
	run("10.4", func(t *testing.T) {
		// 收回角色失败就停，避免带着错误继续验
		if err := facA.RevokeRole(ctx, saA, op.grant); err != nil {
			// 收回角色失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望在节点上操作被拒为越权，放行即失败
		if err := facA.Operate(ctx, op.tok, shop.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 停用账号失败就停，避免带着错误继续验
		if err := facA.DisableAccount(ctx, saA, p.ID); err != nil {
			// 停用账号失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望校验会话仍有效被拒为账号已停用，放行即失败
		if _, err := facA.RequireActive(ctx, pTok); !errors.Is(err, domain.ErrAccountDisabled) {
			// 没拒成账号已停用就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收已停用账号不能再登录，登进去即失败
	run("10.2", func(t *testing.T) {
		// 期望登录被拒为账号已停用，放行即失败
		if _, err := facA.Login(ctx, "p", "p-pass"); !errors.Is(err, domain.ErrAccountDisabled) {
			// 没拒成账号已停用就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收停用账号的历史事实仍可只读查，查不到即失败
	run("10.8", func(t *testing.T) {
		// 停用账号失败就停，避免带着错误继续验
		if err := facA.DisableAccount(ctx, saA, q.acc.ID); err != nil {
			// 停用账号失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取事实失败就停，避免带着错误继续验
		if _, err := facA.GetFact(ctx, saA, factA.ID); err != nil {
			// 读取事实失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 按创建人列事实，失败则本步验收不能继续
		listed, err := facA.ListFactsByCreator(ctx, saA, q.acc.ID)
		// 按创建人列事实失败就停，避免带着错误继续验
		if err != nil || len(listed) == 0 {
			// 结果不符就把实际值打出并停掉本例
			t.Fatalf("%v %d", err, len(listed))
		}
	})
	// 验收还有别的超管时可以停用其中一名，停不成即失败
	run("10.5", func(t *testing.T) {
		// 建人并授角色，失败则夹具缺这个身份
		sa2 := mustCreateRole(t, ctx, facA, saA, "sa2", "sa2-pass", factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
		// 停用账号失败就停，避免带着错误继续验
		if err := facA.DisableAccount(ctx, saA, sa2.acc.ID); err != nil {
			// 停用账号失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 建人并授角色，失败则夹具缺这个身份
		sa3 := mustCreateRole(t, ctx, facA, saA, "sa3", "sa3-pass", factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
		// 收回角色失败就停，避免带着错误继续验
		if err := facA.RevokeRole(ctx, saA, sa3.grant); err != nil {
			// 收回角色失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收最后一个超管入口不能停用或收回，做成即失败
	run("10.6", func(t *testing.T) {
		// 期望停用账号被拒为最后一名超管，放行即失败
		if err := facA.DisableAccount(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrLastAdmin) {
			// 停用没有生效就失败
			t.Fatalf("disable: %v", err)
		}
		// 取出唯一超管授予，取不到则最后入口验不了
		g, err := onlyFactorySAGrant(ctx, facA, a.SuperAdminID)
		// 取出唯一超管授予失败就停，避免带着错误继续验
		if err != nil {
			// 取出唯一超管授予失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望收回角色被拒为最后一名超管，放行即失败
		if err := facA.RevokeRole(ctx, saA, g); !errors.Is(err, domain.ErrLastAdmin) {
			// 收回角色的结果不对就失败
			t.Fatalf("revoke: %v", err)
		}
	})
	// 验收重新启用后原密码能登录，登不上即失败
	run("10.9", func(t *testing.T) {
		// 重新启用账号失败就停，避免带着错误继续验
		if err := facA.EnableAccount(ctx, saA, q.acc.ID); err != nil {
			// 重新启用账号失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 登录失败就停，避免带着错误继续验
		if _, err := facA.Login(ctx, "q", "q-pass"); err != nil {
			// 登录失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收超管重置后旧密码失效，还能用即失败
	run("10.10", func(t *testing.T) {
		// 建立人员，失败则本步验收不能继续
		lost, err := facA.CreatePerson(ctx, saA, "lost", "忘密码")
		// 建立人员失败就停，避免带着错误继续验
		if err != nil {
			// 建立人员失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 设好登录密码，设失败则这个人登不上
		mustAdoptPassword(t, ctx, facA, "lost", "lost-pass")
		// 期望重置密码被拒为越权，放行即失败
		if _, err := facA.ResetPassword(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
			// 把自己挂到自己没有被拒就失败
			t.Fatalf("self: %v", err)
		}
		// 重置密码，失败则本步验收不能继续
		out, err := facA.ResetPassword(ctx, saA, lost.ID)
		// 重置密码失败就停，避免带着错误继续验
		if err != nil {
			// 重置密码失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 状态必须符合这一步，错了即失败
		if out.Status != factory.StatusActive {
			// 状态不对就失败，否则失败
			t.Fatalf("status %s", out.Status)
		}
		// 期望登录被拒为凭证不对，放行即失败
		if _, err := facA.Login(ctx, "lost", "lost-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 旧口令还能登录就失败，重置没生效
			t.Fatalf("old pass: %v", err)
		}
		// 取出该人的密码失败就停，避免带着错误继续验
		if _, err := facA.Login(ctx, "lost", personPass("lost")); err != nil {
			// 重置后的默认口令登不上就失败
			t.Fatalf("default pass: %v", err)
		}
	})

	// 验收错误密码登录被拒且审计无密码，放行即失败
	run("17.2", func(t *testing.T) {
		// 期望登录被拒为凭证不对，放行即失败
		if _, err := facA.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 登录失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取审计，失败则本步验收不能继续
		rows, err := facA.ListAudit(ctx)
		// 读取审计失败就停，避免带着错误继续验
		if err != nil {
			// 读取审计失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取最近拒绝登录，没成功则本步验收失败
		row, ok := audit.LastLoginDeny(rows)
		// 没找到目标就失败，说明这一步没生效
		if !ok || row.ActorID != nil || row.ClaimedLogin == nil || *row.ClaimedLogin != "ghost" {
			// 认领或归属结果不对就失败
			t.Fatalf("claimed %+v", row)
		}
	})
	// 验收审计字段齐全且不含秘密，缺字段或泄密即失败
	run("17.1", func(t *testing.T) {
		// 读取审计，失败则本步验收不能继续
		aRows, err := facA.ListAudit(ctx)
		// 读取审计失败就停，避免带着错误继续验
		if err != nil {
			// 读取审计失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取审计，失败则本步验收不能继续
		bRows, err := facB.ListAudit(ctx)
		// 读取审计失败就停，避免带着错误继续验
		if err != nil {
			// 读取审计失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把两厂审计合在一起查，漏一边会放过泄密
		all := append(aRows, bRows...)
		// 审计字段不齐就失败，这条记录不能用
		if bad := audit.Incomplete(all); len(bad) > 0 {
			// 记录字段缺了就失败，不能当完整记录
			t.Fatalf("incomplete audit %#v", bad[0])
		}
		// 把审计打成文本，没成功则本步验收失败
		dump := audit.Dump(all)
		// 文本里夹带秘密或禁词就失败，说明泄密了
		if audit.ContainsAny(dump, "sa-pass", "sa-pass-2", "p-pass", "q-pass", "lost-pass", personPass("lost"), payload, a.ActivationToken, saA) {
			// 审计里出现秘密就失败，说明正文或口令泄了
			t.Fatalf("secret leaked")
		}
	})
}

// 建好的账号、登录令牌和角色授予
type namedAcc struct {
	// 建好的人员账号，含稳定身份，串号即失败
	acc factory.Account
	// 登录令牌，后面的调用凭它，空了即失败
	tok string
	// 角色授予身份，收回时认它，收错即失败
	grant uuid.UUID
}

// 登录并取出令牌，登不上则后面的身份不成立
func mustLogin(t *testing.T, ctx context.Context, fac *factory.Service, login, pass string) string {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 登录，失败则本步验收不能继续
	tok, err := fac.Login(ctx, login, pass)
	// 登录失败就停，避免带着错误继续验
	if err != nil {
		// 登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	return tok
}

// 建人并授角色，失败则夹具缺这个身份
func mustCreateRole(t *testing.T, ctx context.Context, fac *factory.Service, saTok, login, pass, role, scope string, unit *uuid.UUID) namedAcc {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 建立人员，失败则本步验收不能继续
	acc, err := fac.CreatePerson(ctx, saTok, login, login)
	// 建立人员失败就停，避免带着错误继续验
	if err != nil {
		// 建立人员失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 授予角色，失败则本步验收不能继续
	g, err := fac.GrantRole(ctx, saTok, acc.ID, role, scope, unit)
	// 授予角色失败就停，避免带着错误继续验
	if err != nil {
		// 授予角色失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 设好登录密码，设失败则这个人登不上
	return namedAcc{acc: acc, tok: mustAdoptPassword(t, ctx, fac, login, pass), grant: g.ID}
}
