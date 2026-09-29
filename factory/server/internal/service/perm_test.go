// 第 5 圈：六种角色、作用域、默认拒绝、最后管理员保护。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 验收六种角色的范围、默认拒绝和最后超管保护。
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
	b, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := facA.Activate(ctx, "sa-a", a.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := facB.Activate(ctx, "sa-b", b.ActivationToken, "sb-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
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
	// 把这个人分到车间必须成功，否则作用域没挂上。
	if err := facA.Assign(ctx, saA, p.ID, shopA.ID); err != nil {
		// 分配失败就停，这个人没有落在车间里。
		t.Fatalf("6.2: %v", err)
	}
	// 同一个人再分到另一车间必须因重复分配被拒绝。
	if err := facA.Assign(ctx, saA, p.ID, shopB.ID); !errors.Is(err, domain.ErrDuplicateAssignment) {
		// 还能分到第二个车间就停，一个人挂了两处。
		t.Fatalf("6.3: %v", err)
	}
	// 解除组织分配没成功，后面的断言就没有依据。
	if err := facA.Unassign(ctx, saA, p.ID, shopA.ID); err != nil {
		// 解除组织分配失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把人分进组织没成功，后面的断言就没有依据。
	if err := facA.Assign(ctx, saA, p.ID, shopB.ID); err != nil {
		// 把人分进组织失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 没有角色的人去操作车间必须因越权被拒绝。
	if err := facA.Operate(ctx, pTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 无角色还能操作就停，默认变成允许了。
		t.Fatalf("7.2: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	oa, err := facA.CreatePerson(ctx, saA, "oa", "组织管理员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把组织管理员授到这个车间必须成功。
	if _, err := facA.GrantRole(ctx, saA, oa.ID, factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 组织管理员没授上就停，后面的越权没有身份。
		t.Fatalf("4.1 grant: %v", err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	oaTok := mustAdoptPassword(t, ctx, facA, "oa", "oa-pass")
	// 车间组织管理员在自己的树下建子节点必须成功。
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "线2", &shopA.ID); err != nil {
		// 在自己树下建节点失败就停，管辖范围被缩掉了。
		t.Fatalf("8.1/9.5: %v", err)
	}
	// 车间管理员到兄弟车间建节点必须因越权被拒绝。
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "越界", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能在兄弟车间建节点就停，范围穿到旁边去了。
		t.Fatalf("9.1 sibling: %v", err)
	}
	// 车间管理员在厂根上建节点必须因越权被拒绝。
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "根", nil); !errors.Is(err, domain.ErrForbidden) {
		// 能在厂根建节点就停，管到了自己的上级。
		t.Fatalf("9.1 parent/root: %v", err)
	}
	// 把本车间改挂到兄弟车间下面必须因越权被拒绝。
	if err := facA.ReparentOrgUnit(ctx, oaTok, shopA.ID, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能把车间挂走就停，树可以被拆到别处。
		t.Fatalf("9.1 reparent out: %v", err)
	}
	// 车间管理员授予工厂超管必须因越权被拒绝。
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); !errors.Is(err, domain.ErrForbidden) {
		// 车间管理员能授超管就停，权限被放大了。
		t.Fatalf("9.6 oa grant sa: %v", err)
	}
	// 把操作员授到管辖树之外必须因越权被拒绝。
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能授到树外就停，作用域没有封在树里。
		t.Fatalf("9.6 outside tree: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	oaFac, err := facA.CreatePerson(ctx, saA, "oafac", "整厂管理员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把整厂组织管理员授出去必须成功，否则没有对照身份。
	if _, err := facA.GrantRole(ctx, saA, oaFac.ID, factory.RoleOrgAdmin, factory.ScopeFactory, nil); err != nil {
		// 整厂组织管理员没授上就停，后面没法对照范围。
		t.Fatalf("grant factory org admin: %v", err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	oaFacTok := mustAdoptPassword(t, ctx, facA, "oafac", "oafac-pass")
	// 整厂组织管理员在厂根上建节点必须成功。
	if _, err := facA.CreateOrgUnit(ctx, oaFacTok, "整厂根", nil); err != nil {
		// 厂根建不出来就停，整厂管辖没有生效。
		t.Fatalf("factory org admin root: %v", err)
	}
	// 整厂组织管理员在别的车间建节点也必须成功。
	if _, err := facA.CreateOrgUnit(ctx, oaFacTok, "越界也可", &shopB.ID); err != nil {
		// 别的车间建不出来就停，整厂范围被缩小了。
		t.Fatalf("factory org admin sibling: %v", err)
	}
	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	q, err := facA.CreatePerson(ctx, saA, "q", "待授")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 整厂组织管理员授予厂级操作员必须成功。
	if _, err := facA.GrantRole(ctx, oaFacTok, q.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		// 厂级操作员没授上就停，授权范围不对。
		t.Fatalf("factory org admin grant factory op: %v", err)
	}
	// 整厂组织管理员授予超管必须因越权被拒绝。
	if _, err := facA.GrantRole(ctx, oaFacTok, q.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); !errors.Is(err, domain.ErrForbidden) {
		// 组织管理员能授超管就停，超管资格被放出去了。
		t.Fatalf("factory org admin grant sa: %v", err)
	}
	// 整厂组织管理员新建账号必须因越权被拒绝。
	if _, err := facA.CreatePerson(ctx, oaFacTok, "nope", "nope"); !errors.Is(err, domain.ErrForbidden) {
		// 组织管理员能建账号就停，人事权被放开了。
		t.Fatalf("factory org admin create person: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	lead, err := facA.CreatePerson(ctx, saA, "lead", "负责人")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色没成功，后面的断言就没有依据。
	if _, err := facA.GrantRole(ctx, saA, lead.ID, factory.RoleOrgLead, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	leadTok := mustAdoptPassword(t, ctx, facA, "lead", "lead-pass")
	// 车间负责人必须能查看本车间，否则只读权丢了。
	if err := facA.ViewOrg(ctx, leadTok, shopA.ID); err != nil {
		// 负责人看不了本车间就停，查看权没有给到。
		t.Fatalf("8.2 lead: %v", err)
	}
	// 车间负责人新建子节点必须因越权被拒绝。
	if _, err := facA.CreateOrgUnit(ctx, leadTok, "x", &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 负责人能改组织就停，他只有查看的权。
		t.Fatalf("9.2 lead org: %v", err)
	}
	// 车间负责人新建账号必须因越权被拒绝。
	if _, err := facA.CreatePerson(ctx, leadTok, "x", "x"); !errors.Is(err, domain.ErrForbidden) {
		// 负责人能建账号就停，人事权不该有。
		t.Fatalf("9.2 lead account: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	op, err := facA.CreatePerson(ctx, saA, "op", "操作员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色，供后面步骤使用，失败则前提断了。
	opGrant, err := facA.GrantRole(ctx, saA, op.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID)
	// 授予角色没成功，后面的断言就没有依据。
	if err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	opTok := mustAdoptPassword(t, ctx, facA, "op", "op-pass")
	// 车间操作员在本车间操作必须成功，否则岗位是空的。
	if err := facA.Operate(ctx, opTok, shopA.ID); err != nil {
		// 操作员操作失败就停，本车间的岗位没有生效。
		t.Fatalf("operator operate: %v", err)
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
	if _, err := facA.GrantRole(ctx, opTok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 操作员能授角色就停，授权权漏给了岗位。
		t.Fatalf("9.3 grant: %v", err)
	}

	// 新建本厂人员，供后面步骤使用，失败则前提断了。
	aud, err := facA.CreatePerson(ctx, saA, "aud", "审计员")
	// 新建本厂人员没成功，后面的断言就没有依据。
	if err != nil {
		// 新建本厂人员失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 授予角色没成功，后面的断言就没有依据。
	if _, err := facA.GrantRole(ctx, saA, aud.ID, factory.RoleAuditor, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 授予角色失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 设口令并登录，供后面步骤使用，失败则前提断了。
	audTok := mustAdoptPassword(t, ctx, facA, "aud", "aud-pass")
	// 车间审计员必须能查看本车间，否则只读权丢了。
	if err := facA.ViewOrg(ctx, audTok, shopA.ID); err != nil {
		// 审计员看不了本车间就停，查看权没有给到。
		t.Fatalf("8.2 aud: %v", err)
	}
	// 审计员做现场操作必须因越权被拒绝。
	if err := facA.Operate(ctx, audTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 审计员能操作就停，只读被放大成了干活。
		t.Fatalf("9.4 operate: %v", err)
	}
	// 审计员新建组织节点必须因越权被拒绝。
	if _, err := facA.CreateOrgUnit(ctx, audTok, "z", &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 审计员能改组织就停，只读的边界破了。
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
	// 再给节点加一个父必须因多父被拒绝。
	if err := facA.AddParent(ctx, saA, shopA.ID, shopB.ID); !errors.Is(err, domain.ErrMultiParent) {
		// 能加上第二个父就停，组织不再是一棵树。
		t.Fatalf("5.3: %v", err)
	}
	// 改挂到不存在的父节点必须因找不到被拒绝。
	if err := facA.ReparentOrgUnit(ctx, saA, shopA.ID, ptr(uuid.MustParse("00000000-0000-0000-0000-0000000000bb"))); !errors.Is(err, domain.ErrNotFound) {
		// 挂到空标识也成功就停，父节点没有校验。
		t.Fatalf("5.2: %v", err)
	}
	// 把人分到不存在的组织必须因找不到被拒绝。
	if err := facA.Assign(ctx, saA, p.ID, uuid.MustParse("00000000-0000-0000-0000-0000000000bb")); !errors.Is(err, domain.ErrNotFound) {
		// 能分到空组织就停，分配目标没有校验。
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

	// 收回这条授权没成功，后面的断言就没有依据。
	if err := facA.RevokeRole(ctx, saA, opGrant.ID); err != nil {
		// 收回这条授权失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 收回操作权之后再操作必须因越权被拒绝。
	if err := facA.Operate(ctx, opTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 收回后还能操作就停，授权没有失效。
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
	if _, err := facA.GrantRole(ctx, saA, sa2.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); err != nil {
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
	g3, err := facA.GrantRole(ctx, saA, sa3.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
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
	if err := facA.Rename(ctx, pTok, "改名", "p"); err != nil {
		// 改名失败就停，同一个人换名字没法验。
		t.Fatalf("4.2: %v", err)
	}
	// 核对仍是原账号，供后面步骤使用，失败则前提断了。
	got, err := facA.RequireActive(ctx, pTok)
	// 改名之后必须还是原来那个账号标识。
	if err != nil || got.ID != oldID {
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
