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

// 管理员看全厂、按人现在的所属组织分组，操作员只看自己；分页和合计按同一范围，超界求和封顶。
// 一座厂测完全部情况，少开测试库连接。
func TestWeldReportScopeAndPaging(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 另开一座厂并拿到库连接，最后的超界数据只能用原生 SQL 写。
	fac, db, saTok, saID := weldRawFactory(t, ctx)
	// 车间A，两名操作员归在这里。
	shopA := mustWeldUnit(t, ctx, fac, saTok, "车间A", nil)
	// 班组A1挂在车间A下。
	teamA1 := mustWeldUnit(t, ctx, fac, saTok, "班组A1", &shopA.ID)
	// 车间B是兄弟分支。
	shopB := mustWeldUnit(t, ctx, fac, saTok, "车间B", nil)
	// 车间A的操作员。
	opA := mustWeldPerson(t, ctx, fac, saTok, "op-sa", factory.RoleOperator, &shopA.ID)
	// 车间A的另一名操作员，同组织也不能互看。
	opA2 := mustWeldPerson(t, ctx, fac, saTok, "op-sa2", factory.RoleOperator, &shopA.ID)
	// 班组A1的操作员。
	opA1 := mustWeldPerson(t, ctx, fac, saTok, "op-sa1", factory.RoleOperator, &teamA1.ID)
	// 车间B的操作员。
	opB := mustWeldPerson(t, ctx, fac, saTok, "op-sb", factory.RoleOperator, &shopB.ID)
	// 管理员，不归组织，能看全厂报表。
	mustWeldPerson(t, ctx, fac, saTok, "aud-s", factory.RoleOrgAdmin, nil)
	// 取一个固定起点，各条焊按小时错开，先后可断言。
	base := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	// 车间A最老的一条。
	fA := scopeWeld(opA, 100, base)
	// 班组A1的一条。
	fA1 := scopeWeld(opA1, 200, base.Add(time.Hour))
	// 车间A较新的一条。
	fA2 := scopeWeld(opA2, 300, base.Add(2*time.Hour))
	// 车间B的一条。
	fB := scopeWeld(opB, 400, base.Add(3*time.Hour))
	// 超管自己的一条，超管不归组织算厂直属，最新。
	fD := scopeWeld(saID, 500, base.Add(4*time.Hour))
	// 超管一次送上全部焊，发送人不必是创建人。
	out, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{fA, fA1, fA2, fB, fD})
	// 汇聚失败或有被拒的就停。
	if err != nil || len(out.Accepted) != 5 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("flush: %+v %v", out, err)
	}
	// 焊后再改车间A的名字，报表路径应按现在的树。
	if err := fac.RenameOrgUnit(ctx, saTok, shopA.ID, "车间A新"); err != nil {
		// 改名失败就停。
		t.Fatal(err)
	}

	// 管理员登录，看全厂。
	audTok := mustAdoptPassword(t, ctx, fac, "aud-s", "aud-pass")
	// 管理员下钻全部，五条都在。
	runs, err := fac.ListWeldRuns(ctx, audTok, factory.WeldRunQuery{})
	// 条数或合计不对说明管理员被裁了范围。
	if err != nil || runs.Total != 5 || runs.LengthMM != 1500 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("auditor runs: %+v %v", runs, err)
	}
	// 管理员只要厂直属，应只拿到超管那条。
	direct, err := fac.ListWeldRuns(ctx, audTok, factory.WeldRunQuery{DirectOnly: true})
	// 拿不到或混进了有组织的人的焊就停。
	if err != nil || direct.Total != 1 || len(direct.Items) != 1 || direct.Items[0].ID != fD.ID || direct.LengthMM != 500 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("auditor direct: %+v %v", direct, err)
	}
	// 管理员按组织看报表，厂直属要单独一行。
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
	// 车间A两条合计，路径取现在的树。
	if byOrg[2].RunCount != 2 || byOrg[2].LengthMM != 400 || len(byOrg[2].OrgPath) != 1 || byOrg[2].OrgPath[0].Name != "车间A新" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("shop A row: %+v", byOrg[2])
	}
	// 班组A1的路径是车间A新到班组A1两级。
	if len(byOrg[3].OrgPath) != 2 || byOrg[3].OrgPath[0].Name != "车间A新" || byOrg[3].OrgPath[1].Name != "班组A1" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("team A1 row: %+v", byOrg[3])
	}

	// 车间A操作员登录，只能看自己。
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
	// 按组织筛只取归在车间A本节点的人，子节点不算进来。
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

// 超管建一个人、授一个角色并归到组织，交回人员身份；组织空表示厂直属。
func mustWeldPerson(t *testing.T, ctx context.Context, fac *factory.Service, saTok, login, role string, unit *uuid.UUID) uuid.UUID {
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
	if _, err := fac.GrantRole(ctx, saTok, p.ID, role); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 给了组织才归进去，厂直属不用设。
	if unit != nil {
		// 归组织失败就停，否则后面的分组没有依据。
		if _, err := fac.SetPersonOrg(ctx, saTok, p.ID, unit); err != nil {
			// 把归组织的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
	}
	return p.ID
}

// 造一条焊事实，只绑人；组织按人现在的所属组织算。
func scopeWeld(creator uuid.UUID, length int64, at time.Time) factory.WeldFact {
	// 交回带新身份的焊事实，时长取焊长的十分之一。
	return factory.WeldFact{
		ID: id.New(), CreatorID: creator,
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
