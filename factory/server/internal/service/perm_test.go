// 第 5 圈：三种固定角色、默认拒绝、所属组织不给权、最后管理员保护。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 验收三种角色的边界、默认拒绝和最后超管保护。
func TestPermCircle(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	a, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 开通本厂，供后面步骤使用，失败则前提断了。
	_, facB, err := h.Provision(ctx, "sa-b", "超管B")
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
	// 改测试口令没成功，后面的断言就没有依据。
	if err := seedPass(ctx, facB, "sa-b", "sb-pass"); err != nil {
		// 改测试口令失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 口令登录，供后面步骤使用，失败则前提断了。
	saA, err := facA.Login(ctx, "sa-a", "sa-pass")
	// 口令登录没成功，后面的断言就没有依据。
	if err != nil {
		// 口令登录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 口令登录，供后面步骤使用，失败则前提断了。
	saB, err := facB.Login(ctx, "sa-b", "sb-pass")
	// 口令登录没成功，后面的断言就没有依据。
	if err != nil {
		// 口令登录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 新建组织节点，供后面步骤使用，失败则前提断了。
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	// 新建组织节点没成功，后面的断言就没有依据。
	if err != nil {
		// 新建组织节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 新建组织节点，供后面步骤使用，失败则前提断了。
	shopA, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
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
	// 新建组织节点，供后面步骤使用，失败则前提断了。
	line, err := facA.CreateOrgUnit(ctx, saA, "产线", &shopA.ID)
	// 新建组织节点没成功，后面的断言就没有依据。
	if err != nil {
		// 新建组织节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 组织要能建到班组这一层，失败说明层级被截断。
	if _, err := facA.CreateOrgUnit(ctx, saA, "班组", &line.ID); err != nil {
		// 班组建不出来就停，五层组织没有验收成。
		t.Fatalf("5.1: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	p, err := facA.CreatePerson(ctx, saA, "p", "无角色")
	// 没有角色的人必须能建出来，否则后面没法分配。
	if err != nil {
		// 人没建出来就停，分配没有对象。
		t.Fatalf("4.1 person: %v", err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	pTok := mustAdoptPassword(t, ctx, facA, "p", "p-pass")
	// 把这个人归到车间A必须成功。
	if _, err := facA.SetPersonOrg(ctx, saA, p.ID, &shopA.ID); err != nil {
		// 归组织失败就停，这个人没有落在车间里。
		t.Fatalf("6.2: %v", err)
	}
	// 再归到车间B直接覆盖，每人至多一个所属组织。
	acc, err := facA.SetPersonOrg(ctx, saA, p.ID, &shopB.ID)
	// 改组织失败或没改成车间B就停。
	if err != nil || acc.OrgUnitID == nil || *acc.OrgUnitID != shopB.ID {
		// 所属组织没覆盖就停，一个人挂了两处或没挂上。
		t.Fatalf("6.3: %+v %v", acc.OrgUnitID, err)
	}
	// 归了组织也不给权，没有角色的人建节点必须被拒。
	if _, err := facA.CreateOrgUnit(ctx, pTok, "无权", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 无角色还能改组织就停，所属组织被当成了权限。
		t.Fatalf("7.2 org: %v", err)
	}
	// 没有角色的人建厂级工艺必须被拒。
	if _, err := facA.CreateFactoryProcess(ctx, pTok, factory.WorkContext{}, "无权工艺", []byte(`{"current":1}`)); !errors.Is(err, domain.ErrForbidden) {
		// 无角色还能建厂级就停，默认变成允许了。
		t.Fatalf("7.2 author: %v", err)
	}
	// 个人级谁都能建，没有角色也行。
	if _, err := facA.CreatePersonalProcess(ctx, pTok, factory.WorkContext{}, "个人工艺", []byte(`{"current":1}`)); err != nil {
		// 个人级建不了就停，有效账号的个人空间被收了。
		t.Fatalf("7.3 personal: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	oa, err := facA.CreatePerson(ctx, saA, "oa", "管理员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把管理员角色授出去必须成功。
	oaGrant, err := facA.GrantRole(ctx, saA, oa.ID, factory.RoleOrgAdmin)
	// 管理员没授上就停，后面的越权没有身份。
	if err != nil {
		// 管理员没授上就停，后面的越权没有身份。
		t.Fatalf("4.1 grant: %v", err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	oaTok := mustAdoptPassword(t, ctx, facA, "oa", "oa-pass")
	// 管理员建厂级工艺必须成功，厂级制作归管理员。
	if _, err := facA.CreateFactoryProcess(ctx, oaTok, factory.WorkContext{}, "管理员工艺", []byte(`{"current":2}`)); err != nil {
		// 管理员建不了厂级就停，制作权没给到。
		t.Fatalf("8.1 author: %v", err)
	}
	// 管理员建组织节点必须因越权被拒绝，组织只归超管。
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "越界", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 管理员能建节点就停，组织权被放出去了。
		t.Fatalf("9.1 org: %v", err)
	}
	// 管理员改挂节点必须因越权被拒绝。
	if err := facA.ReparentOrgUnit(ctx, oaTok, shopA.ID, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能把车间挂走就停，组织权被放出去了。
		t.Fatalf("9.1 reparent: %v", err)
	}
	// 管理员给别人归组织必须因越权被拒绝。
	if _, err := facA.SetPersonOrg(ctx, oaTok, p.ID, &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 管理员能改人的组织就停，人事权被放开了。
		t.Fatalf("9.1 person org: %v", err)
	}
	// 管理员授超管必须因越权被拒绝。
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleFactorySuperAdmin); !errors.Is(err, domain.ErrForbidden) {
		// 管理员能授超管就停，权限被放大了。
		t.Fatalf("9.6 oa grant sa: %v", err)
	}
	// 管理员授操作员也必须因越权被拒绝，授角色只归超管。
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleOperator); !errors.Is(err, domain.ErrForbidden) {
		// 管理员能授角色就停，授权权被放出去了。
		t.Fatalf("9.6 oa grant op: %v", err)
	}
	// 管理员新建账号必须因越权被拒绝。
	if _, err := facA.CreatePerson(ctx, oaTok, "nope", "nope"); !errors.Is(err, domain.ErrForbidden) {
		// 管理员能建账号就停，人事权被放开了。
		t.Fatalf("9.6 oa create person: %v", err)
	}
	// 已去掉的负责人角色不能再授。
	if _, err := facA.GrantRole(ctx, saA, p.ID, "org_lead"); !errors.Is(err, domain.ErrInvalidRoleScope) {
		// 还能授负责人就停，旧角色没收干净。
		t.Fatalf("4.3 org_lead: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	op, err := facA.CreatePerson(ctx, saA, "op", "操作员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色没成功，后面的断言就没有依据。
	if _, err := facA.GrantRole(ctx, saA, op.ID, factory.RoleOperator); err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	opTok := mustAdoptPassword(t, ctx, facA, "op", "op-pass")
	// 操作员建厂级工艺必须因越权被拒绝，制作不归岗位。
	if _, err := facA.CreateFactoryProcess(ctx, opTok, factory.WorkContext{}, "操作员工艺", []byte(`{"current":3}`)); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能建厂级就停，制作权漏给了岗位。
		t.Fatalf("9.3 author: %v", err)
	}
	// 操作员新建账号必须因越权被拒绝。
	if _, err := facA.CreatePerson(ctx, opTok, "y", "y"); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能建账号就停，人事权漏给了岗位。
		t.Fatalf("9.3 create: %v", err)
	}
	// 操作员停用别人必须因越权被拒绝。
	if err := facA.DisableAccount(ctx, opTok, p.ID); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能停用别人就停，账号权漏出去了。
		t.Fatalf("9.3 disable: %v", err)
	}
	// 操作员重置别人的口令必须因越权被拒绝。
	if _, err := facA.ResetPassword(ctx, opTok, p.ID); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能重置口令就停，凭证权漏出去了。
		t.Fatalf("9.3 reset: %v", err)
	}
	// 操作员给别人授角色必须因越权被拒绝。
	if _, err := facA.GrantRole(ctx, opTok, p.ID, factory.RoleOperator); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能授角色就停，授权权漏给了岗位。
		t.Fatalf("9.3 grant: %v", err)
	}
	// 操作员建组织节点必须因越权被拒绝。
	if _, err := facA.CreateOrgUnit(ctx, opTok, "z", &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能改组织就停，组织权漏给了岗位。
		t.Fatalf("9.4 org: %v", err)
	}

	// 把节点改挂到自己下面必须因成环被拒绝。
	if err := facA.ReparentOrgUnit(ctx, saA, shopA.ID, &shopA.ID); !errors.Is(err, domain.ErrCycle) {
		// 能挂到自己下面就停，组织被绕成了环。
		t.Fatalf("5.4 self: %v", err)
	}
	// 把祖先改挂到子孙下面必须因成环被拒绝。
	if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &line.ID); !errors.Is(err, domain.ErrCycle) {
		// 祖先能挂到子孙下就停，树被绕环了。
		t.Fatalf("5.4 desc: %v", err)
	}
	// 改挂到不存在的父节点必须因找不到被拒绝。
	if err := facA.ReparentOrgUnit(ctx, saA, shopA.ID, ptr(uuid.MustParse("00000000-0000-0000-0000-0000000000bb"))); !errors.Is(err, domain.ErrNotFound) {
		// 挂到空标识也成功就停，父节点没有校验。
		t.Fatalf("5.2: %v", err)
	}
	// 把人归到不存在的组织必须因找不到被拒绝。
	if _, err := facA.SetPersonOrg(ctx, saA, p.ID, ptr(uuid.MustParse("00000000-0000-0000-0000-0000000000bb"))); !errors.Is(err, domain.ErrNotFound) {
		// 能归到空组织就停，归属目标没有校验。
		t.Fatalf("6.4: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	same, err := facB.CreatePerson(ctx, saB, "p", "厂B同名")
	// 另一家厂用同一个登录名建人必须成功。
	if err != nil {
		// 他厂建同名的人失败就停，登录名被做成全局的了。
		t.Fatalf("6.6: %v", err)
	}
	// 两厂同名的人必须是两个不同的标识。
	if same.ID == p.ID {
		// 两厂撞了同一个标识就停，人没有按厂分开。
		t.Fatalf("6.6 same id")
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	mustAdoptPassword(t, ctx, facB, "p", "pb-pass")

	// 收回管理员授权没成功，后面的断言就没有依据。
	if err := facA.RevokeRole(ctx, saA, oaGrant.ID); err != nil {
		// 收回这条授权失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 收回管理员之后再建厂级必须因越权被拒绝。
	if _, err := facA.CreateFactoryProcess(ctx, oaTok, factory.WorkContext{}, "收回后", []byte(`{"current":4}`)); !errors.Is(err, domain.ErrForbidden) {
		// 收回后还能建厂级就停，授权没有失效。
		t.Fatalf("10.4 revoke: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	sa2, err := facA.CreatePerson(ctx, saA, "sa2", "第二超管")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色没成功，后面的断言就没有依据。
	if _, err := facA.GrantRole(ctx, saA, sa2.ID, factory.RoleFactorySuperAdmin); err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	mustAdoptPassword(t, ctx, facA, "sa2", "sa2-pass")
	// 还有别的超管时，停用其中一人必须成功。
	if err := facA.DisableAccount(ctx, saA, sa2.ID); err != nil {
		// 多余的超管停不掉就停，最后一人的保护误伤了。
		t.Fatalf("10.5 disable extra sa: %v", err)
	}
	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	sa3, err := facA.CreatePerson(ctx, saA, "sa3", "第三超管")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色，供后面步骤使用，失败则前提断了。
	g3, err := facA.GrantRole(ctx, saA, sa3.ID, factory.RoleFactorySuperAdmin)
	// 授予角色没成功，后面的断言就没有依据。
	if err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	mustAdoptPassword(t, ctx, facA, "sa3", "sa3-pass")
	// 还有别的超管时，收回其中一人必须成功。
	if err := facA.RevokeRole(ctx, saA, g3.ID); err != nil {
		// 多余的超管收不回就停，保护的范围过大了。
		t.Fatalf("10.5 revoke extra sa: %v", err)
	}
	// 停用最后一名超管必须因最后管理员被拒绝。
	if err := facA.DisableAccount(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrLastAdmin) {
		// 最后一名超管能被停用就停，厂会没有入口。
		t.Fatalf("10.6 disable last: %v", err)
	}
	// 找出超管授权，供后面步骤使用，失败则前提断了。
	saGrant, err := onlyFactorySAGrant(ctx, facA, a.SuperAdminID)
	// 找出超管授权没成功，后面的断言就没有依据。
	if err != nil {
		// 找出超管授权失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 收回最后一名超管必须因最后管理员被拒绝。
	if err := facA.RevokeRole(ctx, saA, saGrant); !errors.Is(err, domain.ErrLastAdmin) {
		// 最后一名超管能被收回就停，厂会没有入口。
		t.Fatalf("10.6 revoke last: %v", err)
	}

	// 先记下原来的标识，改名之后必须还是同一个人。
	oldID := p.ID
	// 本人修改显示名必须成功，标识留到下一步再对。
	if _, err := facA.SetMyDisplayName(ctx, pTok, "改名"); err != nil {
		// 改名失败就停，同一个人换名字没法验。
		t.Fatalf("4.2: %v", err)
	}
	// 核对仍是原账号，供后面步骤使用，失败则前提断了。
	got, err := facA.RequireActive(ctx, pTok)
	// 改名之后必须还是原来那个账号标识。
	if err != nil || got.ID != oldID || got.DisplayName != "改名" {
		// 标识变了就停，改名被当成了换成另一个人。
		t.Fatalf("4.2 id drifted: %v %+v", err, got)
	}
}

// 把标识变成指针，方便传给要指针的参数。
func ptr(id uuid.UUID) *uuid.UUID { return &id }

// 从有效授权里找出这个人的工厂超管那条。
func onlyFactorySAGrant(ctx context.Context, fac *factory.Service, personID uuid.UUID) (uuid.UUID, error) {
	// 列出有效授权，供后面步骤使用，失败则前提断了。
	grants, err := fac.Store().ActiveGrants(ctx, personID)
	// 列出有效授权没成功，后面的断言就没有依据。
	if err != nil {
		return uuid.Nil, err
	}
	// 在仍然有效的授权里查找工厂超管那一条。
	for _, g := range grants {
		// 碰到工厂超管授权，就把这条授权的标识返回。
		if g.Role == factory.RoleFactorySuperAdmin {
			return g.ID, nil
		}
	}
	return uuid.Nil, domain.ErrNotFound
}
