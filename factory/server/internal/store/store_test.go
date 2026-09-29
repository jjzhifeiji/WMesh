// 第 3 圈：厂库模型约束（无环、唯一登录名、角色作用域、停用保护、禁止物理删）。
package store_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 验收登录名唯一、组织无环、停用和禁删。
func TestFactoryConstraints(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)

	// 先拿到这一步的结果，后面断言还要用。
	sa, err := s.CreatePerson(ctx, "sa", "初始超管", true)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建初始超管失败就停测。
		t.Fatalf("sa: %v", err)
	}
	// 新建账号必须停在待启用，还不能登录。
	if sa.Status != store.StatusPending {
		// 初始账号必须停在待启用。
		t.Fatalf("sa status %s", sa.Status)
	}
	// 第二名初始超管必须被拒绝。
	if _, err := s.CreatePerson(ctx, "sa2", "第二初始", true); err != domain.ErrInitialSAExists {
		// 不能再写出第二名初始超管。
		t.Fatalf("second initial: %v", err)
	}
	// 本厂登录名重复必须被拒绝。
	if _, err := s.CreatePerson(ctx, "sa", "重名", false); err != domain.ErrLoginNameTaken {
		// 重名没有被拒绝就停测。
		t.Fatalf("dup login: %v", err)
	}

	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "op", "操作员", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号失败就停测。
		t.Fatalf("person: %v", err)
	}
	// 记下原来的身份，确认后面没有被换掉。
	oldID := p.ID
	// 改登录名失败就停测，避免后面没有前提。
	if err := s.RenamePerson(ctx, p.ID, "改名", "op-new"); err != nil {
		// 改名失败就停测。
		t.Fatalf("rename: %v", err)
	}
	// 改名或更新不能把稳定身份换掉。
	if p.ID != oldID {
		// 改名把稳定身份换掉了就停测。
		t.Fatalf("id changed")
	}

	// 先建组织节点，结果留给紧跟着的判断。
	site, err := s.CreateOrgUnit(ctx, "一号场地", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建场地失败就停测。
		t.Fatalf("site: %v", err)
	}
	// 先建组织节点，结果留给紧跟着的判断。
	shop, err := s.CreateOrgUnit(ctx, "车间", &site.ID)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建车间失败就停测。
		t.Fatalf("shop: %v", err)
	}
	// 先建组织节点，结果留给紧跟着的判断。
	line, err := s.CreateOrgUnit(ctx, "产线", &shop.ID)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建产线失败就停测。
		t.Fatalf("line: %v", err)
	}
	// 先建组织节点，结果留给紧跟着的判断。
	team, err := s.CreateOrgUnit(ctx, "班组", &line.ID)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建班组失败就停测。
		t.Fatalf("team: %v", err)
	}

	// 成环的改挂必须被拒绝。
	if err := s.ReparentOrgUnit(ctx, site.ID, &site.ID); err != domain.ErrCycle {
		// 节点挂到自己身上必须被拒绝。
		t.Fatalf("self parent: %v", err)
	}
	// 成环的改挂必须被拒绝。
	if err := s.ReparentOrgUnit(ctx, site.ID, &team.ID); err != domain.ErrCycle {
		// 挂到子孙下面必须被拒绝。
		t.Fatalf("descendant parent: %v", err)
	}
	// 改挂父节点失败就停测，避免后面没有前提。
	if err := s.ReparentOrgUnit(ctx, team.ID, &shop.ID); err != nil {
		// 合法改挂失败就停测。
		t.Fatalf("reparent ok: %v", err)
	}
	// 改节点名失败就停测，避免后面没有前提。
	if err := s.RenameOrgUnit(ctx, team.ID, "班组改名"); err != nil {
		// 改节点名失败就停测。
		t.Fatalf("rename unit: %v", err)
	}

	// 分配到节点失败就停测，避免后面没有前提。
	if _, err := s.Assign(ctx, p.ID, site.ID); err != nil {
		// 分配到节点失败就停测。
		t.Fatalf("assign: %v", err)
	}
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.Assign(ctx, p.ID, shop.ID); err != domain.ErrDuplicateAssignment {
		// 同一人再挂第二个节点必须被拒绝。
		t.Fatalf("second unit: %v", err)
	}
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.Assign(ctx, p.ID, site.ID); err != domain.ErrDuplicateAssignment {
		// 重复分配必须被拒绝。
		t.Fatalf("dup assign: %v", err)
	}
	// 取消分配失败就停测，避免后面没有前提。
	if err := s.Unassign(ctx, p.ID, site.ID); err != nil {
		// 取消分配失败就停测。
		t.Fatalf("unassign: %v", err)
	}
	// 分配到节点失败就停测，避免后面没有前提。
	if _, err := s.Assign(ctx, p.ID, site.ID); err != nil {
		// 结束后应允许再次分配。
		t.Fatalf("reassign after end: %v", err)
	}

	// 授予角色失败就停测，避免后面没有前提。
	if _, err := s.GrantRole(ctx, sa.ID, store.RoleFactorySuperAdmin, store.ScopeFactory, nil); err != nil {
		// 给初始超管授角失败就停测。
		t.Fatalf("sa role: %v", err)
	}
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.GrantRole(ctx, sa.ID, store.RoleFactorySuperAdmin, store.ScopeOrgUnit, &site.ID); err != domain.ErrInvalidRoleScope {
		// 超管挂到节点上必须被拒绝。
		t.Fatalf("sa org scope: %v", err)
	}
	// 授予角色失败就停测，避免后面没有前提。
	if _, err := s.GrantRole(ctx, p.ID, store.RoleOrgAdmin, store.ScopeFactory, nil); err != nil {
		// 组织管理员挂整厂应被允许。
		t.Fatalf("org admin factory scope: %v", err)
	}
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.GrantRole(ctx, p.ID, store.RoleOrgLead, store.ScopeFactory, nil); err != domain.ErrInvalidRoleScope {
		// 负责人挂整厂必须被拒绝。
		t.Fatalf("org lead factory scope: %v", err)
	}
	// 先授予角色，结果留给紧跟着的判断。
	grant, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &shop.ID)
	// 授予角色失败就停测，避免后面没有前提。
	if err != nil {
		// 授予操作员失败就停测。
		t.Fatalf("operator: %v", err)
	}
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &shop.ID); err != domain.ErrDuplicateRoleGrant {
		// 重复授予必须被拒绝。
		t.Fatalf("dup grant: %v", err)
	}
	// 收回角色失败就停测，避免后面没有前提。
	if err := s.RevokeRole(ctx, grant.ID); err != nil {
		// 收回角色失败就停测。
		t.Fatalf("revoke: %v", err)
	}

	// 停用节点失败就停测，避免后面没有前提。
	if err := s.DisableOrgUnit(ctx, shop.ID); err != domain.ErrHasActiveChildren {
		// 还有下级时停用必须被拒绝。
		t.Fatalf("disable with children: %v", err)
	}
	// 停用节点失败就停测，避免后面没有前提。
	if err := s.DisableOrgUnit(ctx, team.ID); err != nil {
		// 没有下级的节点应能停用。
		t.Fatalf("disable leaf: %v", err)
	}
	// 停用节点下面不能再挂人或子级。
	if _, err := s.Assign(ctx, p.ID, team.ID); err != domain.ErrDisabledOrgUnit {
		// 不能把人分到已停用的节点。
		t.Fatalf("assign disabled: %v", err)
	}
	// 停用节点下面不能再挂人或子级。
	if _, err := s.CreateOrgUnit(ctx, "新班组", &team.ID); err != domain.ErrDisabledOrgUnit {
		// 不能在停用节点下再建子级。
		t.Fatalf("child of disabled: %v", err)
	}

	// 取当前时刻失败就停测，避免后面没有前提。
	if _, err := s.CreateSession(ctx, p.ID, "sess", time.Now().Add(time.Hour)); err != nil {
		// 开会话失败就停测。
		t.Fatalf("session: %v", err)
	}
	// 同一令牌不能再开一场会话。
	if _, err := s.CreateSession(ctx, p.ID, "sess", time.Now().Add(time.Hour)); err != domain.ErrDuplicateSession {
		// 重复会话必须被拒绝。
		t.Fatalf("dup session: %v", err)
	}

	// 收成文本失败就停测，避免后面没有前提。
	if err := s.AppendAudit(ctx, audit.Event{Action: "assign", Target: site.ID.String(), Result: audit.Allow, ActorID: &sa.ID}); err != nil {
		t.Fatalf("audit: %v", err)
	}

	// 外键对不上就当被指的对象不存在。
	if err := facDB.Exec("DELETE FROM people WHERE id = ?", sa.ID).Error; !domain.IsForeignKeyViolation(err) {
		// 人还被引用时删除必须被外键挡住。
		t.Fatalf("expected fk when deleting referenced person, got %v", err)
	}
}

// 登录名只在这一套厂库里面唯一。
func TestLoginNameUniquePerFactoryDB(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 按这家厂的稳定身份打开库。
	admin := testpg.Open(t)
	// 先另起一套库，结果留给紧跟着的判断。
	_, dsnA := testpg.CreateDB(t, admin, "wmesh_fac")
	// 先另起一套库，结果留给紧跟着的判断。
	_, dsnB := testpg.CreateDB(t, admin, "wmesh_fac")
	// 按这家厂的稳定身份打开库。
	a := store.Open(testpg.OpenMigrated(t, dsnA), uuid.MustParse("00000000-0000-0000-0000-00000000000a"))
	// 按这家厂的稳定身份打开库。
	b := store.Open(testpg.OpenMigrated(t, dsnB), uuid.MustParse("00000000-0000-0000-0000-00000000000b"))
	// 建账号失败就停测，避免后面没有前提。
	if _, err := a.CreatePerson(ctx, "same", "甲", false); err != nil {
		// 第一套厂库建号失败就停测。
		t.Fatalf("a: %v", err)
	}
	// 建账号失败就停测，避免后面没有前提。
	if _, err := b.CreatePerson(ctx, "same", "乙", false); err != nil {
		// 第二套厂库同名应能建，库是分开的。
		t.Fatalf("b: %v", err)
	}
}

// 节点上还有人时禁止物理删除。
func TestPhysicalDeleteOrgUnitBlockedWhenAssigned(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "p", "人", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 分配到节点失败就停测，避免后面没有前提。
	if _, err := s.Assign(ctx, p.ID, u.ID); err != nil {
		// 分配到节点不符合预期就停测。
		t.Fatal(err)
	}
	// 外键对不上就当被指的对象不存在。
	if err := facDB.Exec("DELETE FROM org_units WHERE id = ?", u.ID).Error; !domain.IsForeignKeyViolation(err) {
		// 还有引用时删除必须被外键挡住。
		t.Fatalf("expected fk, got %v", err)
	}
	// 还有引用时不能物理删除。
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		// 节点上还有人时删除必须被拒绝。
		t.Fatalf("delete assigned: %v", err)
	}
}

// 没有下级和引用的节点允许删除。
func TestDeleteOrgWhenUnreferenced(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 先建组织节点，结果留给紧跟着的判断。
	child, err := s.CreateOrgUnit(ctx, "child", &u.ID)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 还有引用时不能物理删除。
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		// 还有下级时删除父节点必须被拒绝。
		t.Fatalf("delete parent: %v", err)
	}
	// 删除节点失败就停测，避免后面没有前提。
	if err := s.DeleteOrgUnit(ctx, child.ID); err != nil {
		// 没有引用的子节点应能删除。
		t.Fatalf("delete unused child: %v", err)
	}
	// 删除节点失败就停测，避免后面没有前提。
	if err := s.DeleteOrgUnit(ctx, u.ID); err != nil {
		// 下级清空后父节点应能删除。
		t.Fatalf("delete emptied parent: %v", err)
	}
}

// 摘掉分配和角色之后才允许删除。
func TestDeleteOrgAfterUnassignAndRevoke(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "p", "人", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 分配到节点失败就停测，避免后面没有前提。
	if _, err := s.Assign(ctx, p.ID, u.ID); err != nil {
		// 分配到节点不符合预期就停测。
		t.Fatal(err)
	}
	// 先授予角色，结果留给紧跟着的判断。
	g, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &u.ID)
	// 授予角色失败就停测，避免后面没有前提。
	if err != nil {
		// 授予角色不符合预期就停测。
		t.Fatal(err)
	}
	// 还有引用时不能物理删除。
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		// 还有有效引用时删除必须被拒绝。
		t.Fatalf("active refs: %v", err)
	}
	// 取消分配失败就停测，避免后面没有前提。
	if err := s.Unassign(ctx, p.ID, u.ID); err != nil {
		// 取消分配不符合预期就停测。
		t.Fatal(err)
	}
	// 收回角色失败就停测，避免后面没有前提。
	if err := s.RevokeRole(ctx, g.ID); err != nil {
		// 收回角色不符合预期就停测。
		t.Fatal(err)
	}
	// 删除节点失败就停测，避免后面没有前提。
	if err := s.DeleteOrgUnit(ctx, u.ID); err != nil {
		// 分配结束且角色收回后应能删除。
		t.Fatalf("after end/revoke: %v", err)
	}
}

// 停用的节点可以再启用并挂子级。
func TestEnableOrgAfterDisable(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 停用节点失败就停测，避免后面没有前提。
	if err := s.DisableOrgUnit(ctx, u.ID); err != nil {
		// 停用节点不符合预期就停测。
		t.Fatal(err)
	}
	// 停用节点下面不能再挂人或子级。
	if _, err := s.CreateOrgUnit(ctx, "child", &u.ID); err != domain.ErrDisabledOrgUnit {
		// 停用节点下再建子级必须被拒绝。
		t.Fatalf("create under disabled: %v", err)
	}
	// 启用节点失败就停测，避免后面没有前提。
	if err := s.EnableOrgUnit(ctx, u.ID); err != nil {
		// 重新启用节点失败就停测。
		t.Fatalf("enable unit: %v", err)
	}
	// 建组织节点失败就停测，避免后面没有前提。
	if _, err := s.CreateOrgUnit(ctx, "child", &u.ID); err != nil {
		// 启用之后应能再挂子级。
		t.Fatalf("create after enable: %v", err)
	}
}

// 验收设备绑定和运行凭证只向前。
func TestClientBindingAndGrants(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)

	// 准备这步测试要用的库或密钥。
	pub, priv := mustEd25519(t)
	// 生成密钥对失败就停测，避免后面没有前提。
	if _, err := s.PutSigningKey(ctx, pub, priv); err != nil {
		// 写入签发密钥失败就停测。
		t.Fatalf("signing key: %v", err)
	}
	// 签发密钥已经存在就不能再写。
	if _, err := s.PutSigningKey(ctx, pub, priv); err != domain.ErrSigningKeyExists {
		// 第二把签发密钥必须被拒绝。
		t.Fatalf("dup signing key: %v", err)
	}

	// 先生成密钥对，结果留给紧跟着的判断。
	cPub, _ := mustEd25519(t)
	// 现发一个新身份，不沿用空值。
	cid := id.New()
	// 先接受绑定，结果留给紧跟着的判断。
	c, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 1)
	// 接受绑定失败就停测，避免后面没有前提。
	if err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept: %v", err)
	}
	// 不是有效绑定就不能继续签发或改登录人。
	if c.Status != store.ClientStatusBound || c.BindingRevision != 1 || c.Name != "焊机-1" || len(c.UnwrapKey) != 0 {
		// 读回的绑定和写入不一致就停测。
		t.Fatalf("client: %+v", c)
	}
	// 先建账号，结果留给紧跟着的判断。
	who, err := s.CreatePerson(ctx, "op-a", "操作员A", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号失败就停测。
		t.Fatalf("person: %v", err)
	}
	// 出错就停测，避免在坏状态上继续。
	if err := s.SetClientOperator(ctx, cid, who.ID); err != nil {
		// 记下当前登录人失败就停测。
		t.Fatalf("set operator: %v", err)
	}
	// 先列出现场设备，结果留给紧跟着的判断。
	listed, err := s.ListClients(ctx)
	// 列出现场设备失败就停测，避免后面没有前提。
	if err != nil || len(listed) != 1 || listed[0].OperatorID == nil || *listed[0].OperatorID != who.ID {
		// 名单里的登录人和写入不一致就停测。
		t.Fatalf("list operator: %+v %v", listed, err)
	}
	// 先改设备名，结果留给紧跟着的判断。
	ren, err := s.RenameClient(ctx, cid, "一线焊机")
	// 改设备名失败就停测，避免后面没有前提。
	if err != nil || ren.Name != "一线焊机" {
		// 改名失败就停测。
		t.Fatalf("rename: %+v %v", ren, err)
	}
	// 接受绑定失败就停测，避免后面没有前提。
	if _, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 1); err != nil {
		// 更旧的绑定修订必须被拒绝。
		t.Fatalf("stale bind: %v", err)
	}
	// 先生成密钥对，结果留给紧跟着的判断。
	other, _ := mustEd25519(t)
	// 必须得到预期的业务错误，否则停测。
	if _, err := s.AcceptBinding(ctx, cid, "焊机-1", other, 2); err != domain.ErrClientKeyMismatch {
		// 公钥对不上必须被拒绝。
		t.Fatalf("key mismatch: %v", err)
	}

	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 先取当前时刻，结果留给紧跟着的判断。
	later := now.Add(24 * time.Hour)
	// 按需要预留位置，避免后面反复扩容。
	sig := make([]byte, 64)
	// 先写入运行凭证，结果留给紧跟着的判断。
	g, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		ClientID: cid, Revision: 1, CanRun: true,
		NotBefore: now, NotAfter: later, Payload: []byte("run-1"), Signature: sig,
	})
	// 出错就停测，避免在坏状态上继续。
	if err != nil {
		// 写入运行凭证失败就停测。
		t.Fatalf("runtime grant: %v", err)
	}
	// 写入运行凭证这一支不成立就换路。
	if _, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		// 先写入运行凭证，结果留给紧跟着的判断。
		ClientID: cid, Revision: 1, CanRun: false,
		NotBefore: now, NotAfter: later, Payload: []byte("run-1b"), Signature: sig,
	}); err != domain.ErrStaleRevision {
		t.Fatalf("stale runtime: %v", err)
	}
	// 先取最新运行凭证，结果留给紧跟着的判断。
	got, err := s.LatestRuntimeGrant(ctx, cid)
	// 改名或更新不能把稳定身份换掉。
	if err != nil || got.ID != g.ID || !got.CanRun {
		// 最新运行凭证和写入不一致就停测。
		t.Fatalf("latest runtime: %+v %v", got, err)
	}

	// 出错就停测，避免在坏状态上继续。
	if err := s.VoidBinding(ctx, cid); err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 先按身份读设备，结果留给紧跟着的判断。
	voided, err := s.ClientByID(ctx, cid)
	// 按身份读设备失败就停测，避免后面没有前提。
	if err != nil || voided.OperatorID != nil {
		// 作废后必须清掉当前登录人。
		t.Fatalf("void clears operator: %+v %v", voided, err)
	}
	// 写入运行凭证这一支不成立就换路。
	if _, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		// 先写入运行凭证，结果留给紧跟着的判断。
		ClientID: cid, Revision: 2, CanRun: false,
		NotBefore: now, NotAfter: later, Payload: []byte("run-2"), Signature: sig,
	}); err != domain.ErrBindingVoid {
		t.Fatalf("void runtime: %v", err)
	}
	// 先接受绑定，结果留给紧跟着的判断。
	reb, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 3)
	// 接受绑定失败就停测，避免后面没有前提。
	if err != nil || reb.Status != store.ClientStatusBound || reb.BindingRevision != 3 || len(reb.UnwrapKey) != 0 {
		// 重新接受绑定失败就停测。
		t.Fatalf("re-accept: %+v %v", reb, err)
	}

	// 准备承接查到的那一行。
	var hasPriv bool
	// 现场设备不能落下私钥。
	if err := facDB.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='clients' AND column_name='private_key')").Scan(&hasPriv).Error; err != nil || hasPriv {
		// 现场设备表不能存私钥。
		t.Fatalf("clients must not have private_key: %v %v", hasPriv, err)
	}
	// 收成文本失败就停测，避免后面没有前提。
	if err := s.AppendAudit(ctx, audit.Event{Action: "issue", Target: cid.String(), Result: audit.Allow, TimeSource: audit.Local}); err != nil {
		t.Fatalf("local audit: %v", err)
	}
}

// 没有解封钥的账号要补一把且保持稳定。
func TestEnsurePersonUnwrapKeyBackfills(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "op", "操作员", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 新建账号此时还不应有解封钥。
	if len(p.UnwrapKey) != 0 {
		// 建账号之后应还没有解封钥。
		t.Fatalf("create %+v", p)
	}
	// 先补解封钥，结果留给紧跟着的判断。
	filled, err := s.EnsurePersonUnwrapKey(ctx, p.ID)
	// 补解封钥失败就停测，避免后面没有前提。
	if err != nil || len(filled.UnwrapKey) != 32 {
		// 补解封钥失败就停测。
		t.Fatalf("ensure %+v %v", filled, err)
	}
	// 出错就停测，避免在坏状态上继续。
	if err := facDB.Exec("UPDATE people SET unwrap_key = NULL WHERE id = ?", p.ID).Error; err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 先补解封钥，结果留给紧跟着的判断。
	again, err := s.EnsurePersonUnwrapKey(ctx, p.ID)
	// 补解封钥失败就停测，避免后面没有前提。
	if err != nil || len(again.UnwrapKey) != 32 {
		// 再次补钥不应换成另一把。
		t.Fatalf("backfill %+v %v", again, err)
	}
	// 先补解封钥，结果留给紧跟着的判断。
	stable, err := s.EnsurePersonUnwrapKey(ctx, p.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(stable.UnwrapKey, again.UnwrapKey) {
		// 两次读到的解封钥必须相同。
		t.Fatalf("stable %+v %v", stable, err)
	}
}

// 验收工艺工程的写入、封装、修订和删除。
func TestFactoryAssets(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 夹具自签租约，才能把正文封进厂库。
	if err := s.GrantLocalLease(ctx); err != nil {
		// 打开本厂库不符合预期就停测。
		t.Fatal(err)
	}
	// 读本厂短码失败就停测，避免后面没有前提。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 读本厂短码不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 先建组织节点，结果留给紧跟着的判断。
	unit, err := s.CreateOrgUnit(ctx, "车间", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 建组织节点不符合预期就停测。
		t.Fatal(err)
	}
	// 先收集当时路径，结果留给紧跟着的判断。
	path, err := s.PathSnapshot(ctx, unit.ID)
	// 收集当时路径失败就停测，避免后面没有前提。
	if err != nil {
		// 收集当时路径不符合预期就停测。
		t.Fatal(err)
	}
	// 准备一段正文，用来核对封装和解开。
	body := []byte(`{"current":200}`)
	// 按正文算摘要，写入时要和正文对上。
	sum := digest.Sum(body)
	// 先拿到这一步的结果，后面断言还要用。
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum,
		CreatorID: p.ID, OrgUnitID: &unit.ID, OrgPath: path,
	})
	// 出错就停测，避免在坏状态上继续。
	if err != nil {
		// 写入工艺失败就停测。
		t.Fatalf("insert: %v", err)
	}
	// 解开的正文必须和当初写入的一致。
	if a.ID == uuid.Nil || a.Revision != 1 || a.FactoryID != facID || a.Name != "焊接" || !bytes.Equal(a.Content, body) {
		// 写回的工艺和入参不一致就停测。
		t.Fatalf("asset: %+v", a)
	}
	// 先读库内信封原文，结果留给紧跟着的判断。
	raw, err := s.RawGovernedContent(ctx, a.ID)
	// 还是明文就先封上，库里不能再留明文。
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, body) {
		// 正文必须封成信封，不能留明文。
		t.Fatalf("want WM2 got %q %v", raw, err)
	}
	// 记下原来的身份，确认后面没有被换掉。
	oldID := a.ID
	// 先修改工艺或工程，结果留给紧跟着的判断。
	renamed, err := s.UpdateGovernedAsset(ctx, a.ID, 1, store.AssetWrite{
		Name: "焊接-2", Content: body, Digest: sum, Copyable: true, Status: store.AssetDraft,
	})
	// 改名或更新不能把稳定身份换掉。
	if err != nil || renamed.ID != oldID || renamed.Revision != 2 || renamed.Name != "焊接-2" {
		// 改名失败就停测。
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	// 先读库内信封原文，结果留给紧跟着的判断。
	raw2, err := s.RawGovernedContent(ctx, a.ID)
	// 还是明文就先封上，库里不能再留明文。
	if err != nil || !contentcrypt.IsEnvelope(raw2) || bytes.Contains(raw2, body) {
		// 改名后正文仍必须是信封。
		t.Fatalf("rename must stay WM2 got %q %v", raw2, err)
	}
	// 修改工艺或工程这一支不成立就换路。
	if _, err := s.UpdateGovernedAsset(ctx, a.ID, 1, store.AssetWrite{
		// 先修改工艺或工程，结果留给紧跟着的判断。
		Name: "旧修订", Content: body, Digest: sum, Copyable: true, Status: store.AssetDraft,
	}); err != domain.ErrRevisionConflict {
		t.Fatalf("conflict: %v", err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err := s.GovernedAssetByID(ctx, a.ID)
	// 按身份读正文失败就停测，避免后面没有前提。
	if err != nil || got.Name != "焊接-2" || got.Revision != 2 {
		// 冲突之后库里的修订不能变。
		t.Fatalf("unchanged after conflict: %+v %v", got, err)
	}

	// 写入工艺或工程这一支不成立就换路。
	if _, err := s.InsertGovernedAsset(ctx, store.Asset{
		// 先写入工艺或工程，结果留给紧跟着的判断。
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "坏依赖",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum,
		CreatorID: p.ID, Deps: []store.AssetDep{{ID: a.ID, Revision: 1, Digest: sum}},
	}); err != domain.ErrAssetDependency {
		t.Fatalf("process deps: %v", err)
	}
	// 写入工艺或工程这一支不成立就换路。
	if _, err := s.InsertGovernedAsset(ctx, store.Asset{
		// 先写入工艺或工程，结果留给紧跟着的判断。
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "短摘要",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: []byte("short"),
		CreatorID: p.ID,
	}); err != domain.ErrIntegrity {
		t.Fatalf("short digest: %v", err)
	}

	// 先写入工艺或工程，结果留给紧跟着的判断。
	proj, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProject, Level: store.AssetLevelFactory, Name: "作业",
		Status: store.AssetAvailable, Copyable: true, Content: []byte(`{"beads":1}`),
		Digest: digest.Sum([]byte(`{"beads":1}`)), CreatorID: p.ID,
		Deps: []store.AssetDep{{ID: a.ID, Revision: 2, Digest: sum}},
	})
	// 改名或更新不能把稳定身份换掉。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != a.ID {
		// 写入工程失败就停测。
		t.Fatalf("project: %+v %v", proj, err)
	}
	// 先导出升档快照，结果留给紧跟着的判断。
	snap, err := s.ExportAssetSnapshot(ctx, a.ID)
	// 带了源修订才覆盖，避免把升档来源抹掉。
	if err != nil || snap.SourceID != a.ID || snap.SourceFactoryID != facID || snap.SourceRevision != 2 {
		// 导出的升档快照和源不一致就停测。
		t.Fatalf("snapshot: %+v %v", snap, err)
	}

	// 出错就停测，避免在坏状态上继续。
	if err := facDB.Exec("UPDATE assets SET content = ? WHERE id = ?", []byte("dirty"), a.ID).Error; err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	dirty, err := s.GovernedAssetByID(ctx, a.ID)
	// 核对摘要失败就停测，避免后面没有前提。
	if err != nil || digest.Match(dirty.Content, dirty.Digest) {
		// 按身份读正文不符合预期就停测。
		t.Fatalf("tamper should break digest: match=%v err=%v", digest.Match(dirty.Content, dirty.Digest), err)
	}
	// 先检查是否被引用，结果留给紧跟着的判断。
	used, err := s.AssetIsReferenced(ctx, a.ID)
	// 检查是否被引用失败就停测，避免后面没有前提。
	if err != nil || !used {
		// 被工程引用时删除必须被拒绝。
		t.Fatalf("referenced: %v %v", used, err)
	}
	// 删除工艺或工程失败就停测，避免后面没有前提。
	if err := s.DeleteGovernedAsset(ctx, proj.ID); err != nil {
		// 删除工艺或工程不符合预期就停测。
		t.Fatal(err)
	}
	// 删除工艺或工程失败就停测，避免后面没有前提。
	if err := s.DeleteGovernedAsset(ctx, a.ID); err != nil {
		// 删除工艺或工程不符合预期就停测。
		t.Fatal(err)
	}
	// 准备承接查到的那一行。
	var stubExists bool
	// 扫描查询结果失败就停测，避免后面没有前提。
	if err := facDB.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='personal_asset_stubs')").Scan(&stubExists).Error; err != nil || !stubExists {
		// 删工艺不能把事实桩一起删掉。
		t.Fatalf("stubs must remain: %v %v", stubExists, err)
	}
}

// 生成一对签发密钥，失败就停测。
func mustEd25519(t *testing.T) ([]byte, []byte) {
	// 标成测试辅助，失败行号指向真正用例。
	t.Helper()
	// 填不满随机数就不能把半截当密钥。
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	// 生成密钥对失败就停测，避免后面没有前提。
	if err != nil {
		// 生成密钥对不符合预期就停测。
		t.Fatal(err)
	}
	return pub, priv
}

// 清掉内存租约后，同一把钥能再解开正文。
func TestContentMasterRestore(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 读本厂短码失败就停测，避免后面没有前提。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 打开本厂库不符合预期就停测。
		t.Fatal(err)
	}
	// 准备密钥或摘要，后面套租约或写入要用。
	l, err := contentcrypt.RandomKey()
	// 生成随机钥失败就停测，避免后面没有前提。
	if err != nil {
		// 生成随机钥不符合预期就停测。
		t.Fatal(err)
	}
	// 取当前时刻失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 取当前时刻不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 准备一段正文，用来核对封装和解开。
	body := []byte(`{"current":200}`)
	// 按正文算摘要，写入时要和正文对上。
	sum := digest.Sum(body)
	// 先拿到这一步的结果，后面断言还要用。
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum, CreatorID: p.ID,
	})
	// 出错就停测，避免在坏状态上继续。
	if err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 清掉内存租约，用来验收没钥解不开。
	s.ClearContentLease()
	// 读元数据失败就停测，避免后面没有前提。
	if _, err := s.GovernedAssetMetaByID(ctx, a.ID); err != nil {
		// 没有租约也应能读元数据。
		t.Fatalf("meta %v", err)
	}
	// 没有有效租约时必须解不开正文。
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 没有租约时必须解不开正文。
		t.Fatalf("open without lease %v", err)
	}
	// 取当前时刻失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 取当前时刻不符合预期就停测。
		t.Fatal(err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err := s.GovernedAssetByID(ctx, a.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(got.Content, body) {
		// 同一把钥应能把正文原样解开。
		t.Fatalf("restore %q %v", got.Content, err)
	}
	// 标成在线，墙上钟过了也收新租约。
	s.SetContentChannelOnline(true)
	// 取当前时刻失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(-time.Minute)); err != nil {
		// 标记通道在线不符合预期就停测。
		t.Fatal(err)
	}
	// 按身份读正文失败就停测，避免后面没有前提。
	if _, err := s.GovernedAssetByID(ctx, a.ID); err != nil {
		// 在线时墙上钟过了仍应能解开。
		t.Fatalf("online past notAfter %v", err)
	}
	// 标成离线，过期租约不能再续期。
	s.SetContentChannelOnline(false)
	// 按身份读正文失败就停测，避免后面没有前提。
	if _, err := s.GovernedAssetByID(ctx, a.ID); err != nil {
		// 离线后单调窗口内仍应能解开。
		t.Fatalf("offline must keep monotonic window %v", err)
	}
}

// 用夹具钟验收租约窗口、倒拨和过期。
func TestLeaseClockWindows(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 读本厂短码失败就停测，避免后面没有前提。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 打开本厂库不符合预期就停测。
		t.Fatal(err)
	}
	// 钉死一个起点，后面按偏移卡窗口。
	base := time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC)
	// 准备承接查到的那一行。
	var mu sync.Mutex
	// 偏移从零开始，表示还停在起点。
	offset := time.Duration(0)
	// 夹具读钟时先占锁，避免和拨钟交错。
	s.SetContentClock(func() time.Time {
		// 先占住内存租约，避免并发把钥拆散。
		mu.Lock()
		// 放开内存租约，后面的读写才进得来。
		defer mu.Unlock()
		// 做完放开内存锁后把结果交回。
		return base.Add(offset)
	})
	// 把夹具钟拨到指定偏移，用来卡租约窗口。
	advance := func(d time.Duration) {
		// 先占住内存租约，避免并发把钥拆散。
		mu.Lock()
		// 记下新的偏移，下次读钟就用它。
		offset = d
		// 放开内存租约，后面的读写才进得来。
		mu.Unlock()
	}
	// 准备密钥或摘要，后面套租约或写入要用。
	l, err := contentcrypt.RandomKey()
	// 生成随机钥失败就停测，避免后面没有前提。
	if err != nil {
		// 生成随机钥不符合预期就停测。
		t.Fatal(err)
	}
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, base.Add(24*time.Hour)); err != nil {
		// 套上内容租约不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 准备一段正文，用来核对封装和解开。
	body := []byte(`{"current":200}`)
	// 先拿到这一步的结果，后面断言还要用。
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: p.ID,
	})
	// 出错就停测，避免在坏状态上继续。
	if err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 标成离线，过期租约不能再续期。
	s.SetContentChannelOnline(false)
	// 先把夹具钟拨到这个偏移，结果留给紧跟着的判断。
	advance(23 * time.Hour)
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err := s.GovernedAssetByID(ctx, a.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(got.Content, body) {
		// 未到二十四小时应仍能解开。
		t.Fatalf("23h %q %v", got.Content, err)
	}
	// 清掉内存租约，用来验收没钥解不开。
	s.ClearContentLease()
	// 没有有效租约时必须解不开正文。
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 清掉租约后必须解不开。
		t.Fatalf("cleared %v", err)
	}
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, base.Add(24*time.Hour)); err != nil {
		// 套上内容租约不符合预期就停测。
		t.Fatal(err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err = s.GovernedAssetByID(ctx, a.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(got.Content, body) {
		// 同一把钥在窗口内应能再解开。
		t.Fatalf("same L at 23h %q %v", got.Content, err)
	}
	// 先把夹具钟拨到这个偏移，结果留给紧跟着的判断。
	advance(0)
	// 按身份读正文失败就停测，避免后面没有前提。
	if _, err := s.GovernedAssetByID(ctx, a.ID); err != nil {
		// 时钟倒拨不能把还有效的租约判死。
		t.Fatalf("rewind %v", err)
	}
	// 先把夹具钟拨到这个偏移，结果留给紧跟着的判断。
	advance(25 * time.Hour)
	// 没有有效租约时必须解不开正文。
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 过了窗口必须解不开。
		t.Fatalf("25h %v", err)
	}
	// 没有有效租约时必须解不开正文。
	if err := s.ApplyContentLease(ctx, l, base.Add(24*time.Hour)); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 离线不能把已经过期的租约续上。
		t.Fatalf("offline cannot extend %v", err)
	}
	// 先把夹具钟拨到这个偏移，结果留给紧跟着的判断。
	advance(0)
	// 没有有效租约时必须解不开正文。
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 过期后再倒拨也不能重新有效。
		t.Fatalf("rewind after expiry %v", err)
	}
	// 先把夹具钟拨到这个偏移，结果留给紧跟着的判断。
	advance(25 * time.Hour)
	// 标成在线，墙上钟过了也收新租约。
	s.SetContentChannelOnline(true)
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, base.Add(49*time.Hour)); err != nil {
		// 标记通道在线不符合预期就停测。
		t.Fatal(err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err = s.GovernedAssetByID(ctx, a.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(got.Content, body) {
		// 在线续租后应能再解开正文。
		t.Fatalf("online renew %q %v", got.Content, err)
	}
}

// 通道来回抖动不应清掉仍在窗口内的租约。
func TestChannelFlapKeepsLease(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 读本厂短码失败就停测，避免后面没有前提。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 打开本厂库不符合预期就停测。
		t.Fatal(err)
	}
	// 准备密钥或摘要，后面套租约或写入要用。
	l, err := contentcrypt.RandomKey()
	// 生成随机钥失败就停测，避免后面没有前提。
	if err != nil {
		// 生成随机钥不符合预期就停测。
		t.Fatal(err)
	}
	// 取当前时刻失败就停测，避免后面没有前提。
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 取当前时刻不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 准备一段正文，用来核对封装和解开。
	body := []byte(`{"current":200}`)
	// 先拿到这一步的结果，后面断言还要用。
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: p.ID,
	})
	// 出错就停测，避免在坏状态上继续。
	if err != nil {
		// 失败就停测，避免后面的断言误判通过。
		t.Fatal(err)
	}
	// 来回切换几次在线，看租约会不会被清掉。
	for i := 0; i < 3; i++ {
		// 标成离线，过期租约不能再续期。
		s.SetContentChannelOnline(false)
		// 标成在线，墙上钟过了也收新租约。
		s.SetContentChannelOnline(true)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err := s.GovernedAssetByID(ctx, a.ID)
	// 解开的正文必须和当初写入的一致。
	if err != nil || !bytes.Equal(got.Content, body) {
		// 通道抖动之后正文仍应能解开。
		t.Fatalf("after flap %q %v", got.Content, err)
	}
}
