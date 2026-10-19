// 焊报表可见范围、厂直属下钻、分页合计和超界求和。
package service_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 负责人看子树不看兄弟，审计员看厂直属，操作员只看自己；分页和合计按同一范围，超界求和封顶。
// 一座厂测完全部情况，少开测试库连接。
func TestWeldReportScopeAndPaging(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 另开一座厂并拿到库连接，最后的超界数据只能用原生 SQL 写。
	fac, db, saTok, saID := weldRawFactory(t, ctx)
	// 车间A是负责人的根节点。
	shopA := mustWeldUnit(t, ctx, fac, saTok, "车间A", nil)
	// 班组A1挂在车间A下，负责人应能看到。
	teamA1 := mustWeldUnit(t, ctx, fac, saTok, "班组A1", &shopA.ID)
	// 车间B是兄弟分支，负责人不该看到。
	shopB := mustWeldUnit(t, ctx, fac, saTok, "车间B", nil)
	// 车间A的操作员，作用域挂在车间A。
	opA := mustWeldPerson(t, ctx, fac, saTok, "op-sa", factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID)
	// 车间A的另一名操作员，同节点也不能互看。
	opA2 := mustWeldPerson(t, ctx, fac, saTok, "op-sa2", factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID)
	// 班组A1的操作员，焊次落在子节点。
	opA1 := mustWeldPerson(t, ctx, fac, saTok, "op-sa1", factory.RoleOperator, factory.ScopeOrgUnit, &teamA1.ID)
	// 车间B的操作员，焊次落在兄弟分支。
	opB := mustWeldPerson(t, ctx, fac, saTok, "op-sb", factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID)
	// 车间A负责人，只读子树。
	mustWeldPerson(t, ctx, fac, saTok, "lead-sa", factory.RoleOrgLead, factory.ScopeOrgUnit, &shopA.ID)
	// 整厂审计员，能看厂直属。
	mustWeldPerson(t, ctx, fac, saTok, "aud-s", factory.RoleAuditor, factory.ScopeFactory, nil)
	// 取一个固定起点，各条焊按小时错开，先后可断言。
	base := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	// 车间A最老的一条，路径快照还是旧名。
	fA := scopeWeld(opA, &shopA.ID, "车间A旧名", 100, base)
	// 班组A1的一条，负责人应能看到。
	fA1 := scopeWeld(opA1, &teamA1.ID, "班组A1", 200, base.Add(time.Hour))
	// 车间A较新的一条，路径快照是新名。
	fA2 := scopeWeld(opA2, &shopA.ID, "车间A", 300, base.Add(2*time.Hour))
	// 车间B的一条，负责人看不到。
	fB := scopeWeld(opB, &shopB.ID, "车间B", 400, base.Add(3*time.Hour))
	// 超管自己的厂直属一条，最新。
	fD := scopeWeld(saID, nil, "", 500, base.Add(4*time.Hour))
	// 超管一次送上全部焊，发送人不必是创建人。
	out, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{fA, fA1, fA2, fB, fD})
	// 汇聚失败或有被拒的就停。
	if err != nil || len(out.Accepted) != 5 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("flush: %+v %v", out, err)
	}

	// 负责人登录，看车间A子树。
	leadTok := mustAdoptPassword(t, ctx, fac, "lead-sa", "lead-pass")
	// 负责人下钻全部，应只有车间A和班组A1的三条。
	runs, err := fac.ListWeldRuns(ctx, leadTok, factory.WeldRunQuery{})
	// 条数、合计或身份不对，说明子树或兄弟分支判错了。
	if err != nil || runs.Total != 3 || runs.LengthMM != 600 || !sameRunIDs(runs.Items, fA2.ID, fA1.ID, fA.ID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("lead runs: %+v %v", runs, err)
	}
	// 负责人按人看报表，应是车间A子树里的三个人。
	byPerson, err := fac.ListWeldReport(ctx, leadTok, "person", nil, nil, time.UTC)
	// 条数不对说明报表范围和下钻不一致。
	if err != nil || len(byPerson) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("lead report: %+v %v", byPerson, err)
	}
	// 逐行核对，兄弟分支和厂直属的人不能出现。
	for _, r := range byPerson {
		// 出现车间B或超管就说明越权。
		if r.PersonID == nil || *r.PersonID == opB || *r.PersonID == saID {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("lead sees %+v", r)
		}
	}
	// 节点负责人不看厂直属，只要厂直属应一条没有。
	direct, err := fac.ListWeldRuns(ctx, leadTok, factory.WeldRunQuery{DirectOnly: true})
	// 看到了厂直属说明节点作用域放宽过头。
	if err != nil || direct.Total != 0 || len(direct.Items) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("lead direct: %+v %v", direct, err)
	}

	// 审计员登录，整厂作用域。
	audTok := mustAdoptPassword(t, ctx, fac, "aud-s", "aud-pass")
	// 审计员只要厂直属，应只拿到超管那条。
	direct, err = fac.ListWeldRuns(ctx, audTok, factory.WeldRunQuery{DirectOnly: true})
	// 拿不到或混进了有节点的焊就停。
	if err != nil || direct.Total != 1 || len(direct.Items) != 1 || direct.Items[0].ID != fD.ID || direct.LengthMM != 500 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("auditor direct: %+v %v", direct, err)
	}
	// 审计员按组织看报表，厂直属要单独一行。
	byOrg, err := fac.ListWeldReport(ctx, audTok, "org", nil, nil, time.UTC)
	// 四个组织行都在，第一行是最新的厂直属。
	if err != nil || len(byOrg) != 4 || byOrg[0].OrgUnitID != nil || byOrg[0].LengthMM != 500 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("auditor org: %+v %v", byOrg, err)
	}
	// 按最近一次焊排序：厂直属、车间B、车间A、班组A1。
	wantOrder := []uuid.UUID{shopB.ID, shopA.ID, teamA1.ID}
	// 逐行核对先后，排错说明没按最新焊次排。
	for i, want := range wantOrder {
		// 这一行的节点不是预期的就停。
		if byOrg[i+1].OrgUnitID == nil || *byOrg[i+1].OrgUnitID != want {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("org order %d: %+v", i+1, byOrg)
		}
	}
	// 车间A两条合计，路径取较新那条的快照。
	if byOrg[2].RunCount != 2 || byOrg[2].LengthMM != 400 || len(byOrg[2].OrgPath) != 1 || byOrg[2].OrgPath[0].Name != "车间A" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("shop A row: %+v", byOrg[2])
	}

	// 车间A操作员登录，节点作用域的操作员。
	opTok := mustAdoptPassword(t, ctx, fac, "op-sa", "op-pass")
	// 操作员下钻，只能看到自己那条，同节点的人也看不到。
	runs, err = fac.ListWeldRuns(ctx, opTok, factory.WeldRunQuery{})
	// 看到了别人的焊就停。
	if err != nil || runs.Total != 1 || len(runs.Items) != 1 || runs.Items[0].ID != fA.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("operator runs: %+v %v", runs, err)
	}
	// 操作员看报表，也只有自己一行。
	own, err := fac.ListWeldReport(ctx, opTok, "", nil, nil, time.UTC)
	// 多出别人的行就停。
	if err != nil || len(own) != 1 || own[0].PersonID == nil || *own[0].PersonID != opA {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("operator report: %+v %v", own, err)
	}

	// 超管下钻只要厂直属，有节点的一条都不能混进来。
	direct, err = fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{DirectOnly: true})
	// 不是只有超管那条就停。
	if err != nil || direct.Total != 1 || direct.Items[0].ID != fD.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa direct: %+v %v", direct, err)
	}
	// 第一页两条，总数和合计按全部五条算。
	page1, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{Limit: 2})
	// 总数、合计或本页内容不对就停。
	if err != nil || page1.Total != 5 || page1.LengthMM != 1500 || page1.DurationSec != 150 || !sameRunIDs(page1.Items, fD.ID, fB.ID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("page1: %+v %v", page1, err)
	}
	// 第二页接着往下，不重不漏。
	page2, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{Limit: 2, Offset: 2})
	// 本页不是接下来的两条就停。
	if err != nil || page2.Total != 5 || !sameRunIDs(page2.Items, fA2.ID, fA1.ID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("page2: %+v %v", page2, err)
	}
	// 最后一页只剩最老那条。
	page3, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{Limit: 2, Offset: 4})
	// 最后一页不对就停。
	if err != nil || page3.Total != 5 || !sameRunIDs(page3.Items, fA.ID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("page3: %+v %v", page3, err)
	}
	// 条数超上限不报错，按上限取，五条全在。
	wide, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{Limit: 5000})
	// 报错或条数不对就停。
	if err != nil || len(wide.Items) != 5 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("wide: %d %v", len(wide.Items), err)
	}
	// 不带条数按默认一页，五条全在。
	plain, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{})
	// 报错或条数不对就停。
	if err != nil || len(plain.Items) != 5 || plain.Total != 5 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("plain: %+v %v", plain, err)
	}
	// 按节点筛只取车间A本节点，子节点不算进来。
	both, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{OrgUnitID: &shopA.ID})
	// 车间A只有两条，混进别的节点就停。
	if err != nil || both.Total != 2 || !sameRunIDs(both.Items, fA2.ID, fA.ID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("shop A runs: %+v %v", both, err)
	}

	// 取出本厂身份，原生写入的行要归到这座厂。
	facID := fac.Store().FactoryID()
	// 两条 int64 最大值落在同一工程，求和会超出 int64。
	insertRawWeld(t, db, facID, saID, "工程超界", "single", math.MaxInt64, math.MaxInt64, base.Add(5*time.Hour))
	// 第二条最大值，和上一条同组。
	insertRawWeld(t, db, facID, saID, "工程超界", "single", math.MaxInt64, math.MaxInt64, base.Add(6*time.Hour))
	// 逐个维度看报表，超界值不该让查询报错。
	for _, group := range []string{"", "person", "project", "org", "day"} {
		// 拉这一维的报表。
		rows, err := fac.ListWeldReport(ctx, saTok, group, nil, nil, time.UTC)
		// 报错或没有行就停。
		if err != nil || len(rows) == 0 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("cap report %q: %+v %v", group, rows, err)
		}
		// 最近焊的那组带着两条最大值，应排第一且封顶。
		if rows[0].LengthMM != math.MaxInt64 || rows[0].DurationSec != math.MaxInt64 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("cap report %q first: %+v", group, rows[0])
		}
		// 逐行核对，回绕成负数说明没封顶。
		for _, r := range rows {
			// 出现负数说明求和回绕了。
			if r.LengthMM < 0 || r.DurationSec < 0 {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("cap report %q wrapped: %+v", group, r)
			}
		}
	}
	// 按工程看，超界工程两次封顶，别的工程照实算。
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil, time.UTC)
	// 不是超界工程在前两次、普通工程在后五次就停。
	if err != nil || len(byProj) != 2 || byProj[0].ProjectName != "工程超界" || byProj[0].RunCount != 2 || byProj[1].RunCount != 5 || byProj[1].LengthMM != 1500 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("cap project: %+v %v", byProj, err)
	}
	// 下钻合计同样封顶，明细照原值给。
	capped, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{})
	// 报错、条数或合计不对就停。
	if err != nil || capped.Total != 7 || len(capped.Items) != 7 || capped.LengthMM != math.MaxInt64 || capped.DurationSec != math.MaxInt64 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("cap runs: %+v %v", capped, err)
	}
}

// 超管建一个组织节点，失败就停测。
func mustWeldUnit(t *testing.T, ctx context.Context, fac *factory.Service, saTok, name string, parent *uuid.UUID) factory.OrgUnit {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 新建组织节点，失败说明名称或上级不合法。
	u, err := fac.CreateOrgUnit(ctx, saTok, name, parent)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return u
}

// 超管建一个人并授一个角色，交回人员身份。
func mustWeldPerson(t *testing.T, ctx context.Context, fac *factory.Service, saTok, login, role, scope string, unit *uuid.UUID) uuid.UUID {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 新建厂内人员，失败说明登录名冲突或越权。
	p, err := fac.CreatePerson(ctx, saTok, login, login)
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, p.ID, role, scope, unit); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return p.ID
}

// 造一条焊事实，节点空表示厂直属，路径名按快照给。
func scopeWeld(creator uuid.UUID, unit *uuid.UUID, pathName string, length int64, at time.Time) factory.WeldFact {
	// 厂直属的路径是空的。
	path := []factory.PathNode{}
	// 有节点就给一级路径快照。
	if unit != nil {
		// 路径只放这一级，名字按当时快照。
		path = []factory.PathNode{{ID: *unit, Name: pathName}}
	}
	// 交回带新身份的焊事实，时长取焊长的十分之一。
	return factory.WeldFact{
		ID: id.New(), CreatorID: creator, OrgUnitID: unit, OrgPath: path,
		ProjectName: "工程S", WeldKind: "single", LengthMM: length, DurationSec: length / 10, OccurredAt: at,
	}
}

// 下钻行的身份是否正好依次是这些。
func sameRunIDs(rows []factory.WeldRunRow, want ...uuid.UUID) bool {
	// 条数不同就不用再比。
	if len(rows) != len(want) {
		return false
	}
	// 逐条按先后比身份。
	for i := range rows {
		// 有一条对不上就算不同。
		if rows[i].ID != want[i] {
			return false
		}
	}
	return true
}
