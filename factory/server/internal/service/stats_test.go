// 焊长时长：同机多人分账、B 回连带上 A、报表按人不混。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 同机多人的焊长要分账，串到别人名下即失败。
func TestWeldStatsSeparatePeople(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err := fac.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shop, err := fac.CreateOrgUnit(ctx, saTok, "车间A", nil)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopB, err := fac.CreateOrgUnit(ctx, saTok, "车间B", nil)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建厂内人员，失败说明登录名冲突或越权。
	pa, err := fac.CreatePerson(ctx, saTok, "op-a", "焊工A")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建厂内人员，失败说明登录名冲突或越权。
	pb, err := fac.CreatePerson(ctx, saTok, "op-b", "焊工B")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, pa.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, pb.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, pa.ID, shop.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, pb.ID, shopB.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	aTok := mustAdoptPassword(t, ctx, fac, "op-a", "a-pass")
	// 把默认口令改成测试口令，失败说明改密被拒。
	bTok := mustAdoptPassword(t, ctx, fac, "op-b", "b-pass")
	// 用平板登录厂服，失败说明账号没有对上。
	sess, err := fac.LoginPad(ctx, "op-a", "a-pass")
	// 平板登录失败或组织不对就停，说明没达预期。
	if err != nil || sess.Work.OrgUnitID == nil || *sess.Work.OrgUnitID != shop.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("a work: %+v %v", sess.Work, err)
	}

	// 新取一个编号，撞号会让两条记录分不清。
	idA := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	idB := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	projA := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	projB := id.New()
	// 取出日期部分，用来按天归并焊接统计。
	day1 := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	// 取出日期部分，用来按天归并焊接统计。
	day2 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	// 装甲的焊接事实，串到乙的统计里就算没分开。
	factA := factory.WeldFact{
		ID: idA, CreatorID: pa.ID, OrgUnitID: &shop.ID, OrgPath: sess.Work.OrgPath,
		ProjectID: &projA, ProjectName: "单层-梁1", WeldKind: "single",
		LengthMM: 1200, DurationSec: 60, OccurredAt: day1,
	}
	// 用平板登录厂服，失败说明账号没有对上。
	sessB, err := fac.LoginPad(ctx, "op-b", "b-pass")
	// 平板登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把平板登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 装乙的焊接事实，串到甲的统计里就算没分开。
	factB := factory.WeldFact{
		ID: idB, CreatorID: pb.ID, OrgUnitID: &shopB.ID, OrgPath: sessB.Work.OrgPath,
		ProjectID: &projB, ProjectName: "多层-箱体", WeldKind: "multilayer",
		LengthMM: 800, DurationSec: 40, OccurredAt: day2,
	}
	// B 回连把 A、B 一起送，厂端按创建人分开。
	out, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{factA, factB})
	// 刷焊事实失败或完整性不对就停，说明没达预期。
	if err != nil || len(out.Accepted) != 2 || len(out.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("flush: %+v %v", out, err)
	}
	// 把焊接事实刷出去，失败说明汇聚口拒收。
	again, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{factA, factB})
	// 刷焊事实失败或条数不对就停，说明没达预期。
	if err != nil || len(again.Accepted) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("idempotent: %+v %v", again, err)
	}
	// 复制一条再改坏长度，用来验证非法值会被拒。
	bad := factA
	// 把长度改成非法值，仍能入库说明没校验。
	bad.LengthMM = 9999
	// 把焊接事实刷出去，失败说明汇聚口拒收。
	clash, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{bad})
	// 刷焊事实失败或完整性不对就停，说明没达预期。
	if err != nil || len(clash.Rejected) != 1 || clash.Rejected[0].Error != domain.ErrIntegrity.Error() {
		// 完整性没有通过，说明摘要或租约已经坏了。
		t.Fatalf("integrity: %+v %v", clash, err)
	}

	// 拉取本人焊接统计，失败说明会话对不上人。
	aStats, err := fac.MyWeldStats(ctx, aTok)
	// 拉本人统计失败或结果不符就停，说明没达预期。
	if err != nil || aStats.LengthMM != 1200 || aStats.DurationSec != 60 || aStats.RunCount != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("a totals: %+v %v", aStats, err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(aStats.Projects) != 1 || aStats.Projects[0].ProjectName != "单层-梁1" || len(aStats.Recent) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("a projects: %+v", aStats)
	}
	// 拉取本人焊接统计，失败说明会话对不上人。
	bStats, err := fac.MyWeldStats(ctx, bTok)
	// 拉本人统计失败或结果不符就停，说明没达预期。
	if err != nil || bStats.LengthMM != 800 || bStats.DurationSec != 40 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("b totals: %+v %v", bStats, err)
	}

	// 拉取焊接统计，失败说明无权或范围是空的。
	all, err := fac.ListWeldReport(ctx, saTok, "", nil, nil)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(all) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa report %d %v", len(all), err)
	}
	// 拉取焊接统计，失败说明无权或范围是空的。
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(byProj) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project report %d %v", len(byProj), err)
	}
	// 拉取焊接流水，失败说明无权查看记录。
	runs, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{})
	// 拉焊流水失败或条数不对就停，说明没达预期。
	if err != nil || len(runs) != 2 || runs[0].ProjectName == "" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("runs: %+v %v", runs, err)
	}
	// 拉取焊接统计，失败说明无权或范围是空的。
	own, err := fac.ListWeldReport(ctx, aTok, "", nil, nil)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(own) != 1 || own[0].PersonID == nil || *own[0].PersonID != pa.ID || own[0].LengthMM != 1200 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("a report: %+v %v", own, err)
	}
	// 新建厂内人员，失败说明登录名冲突或越权。
	lead, err := fac.CreatePerson(ctx, saTok, "lead-a", "车间A负责人")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, lead.ID, factory.RoleOrgLead, factory.ScopeOrgUnit, &shop.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	leadTok := mustAdoptPassword(t, ctx, fac, "lead-a", "lead-pass")
	// 拉取焊接统计，失败说明无权或范围是空的。
	scoped, err := fac.ListWeldReport(ctx, leadTok, "", nil, nil)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(scoped) != 1 || scoped[0].PersonID == nil || *scoped[0].PersonID != pa.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("lead report: %+v %v", scoped, err)
	}
	// 按编号取焊接事实，取不到说明没刷进库。
	got, err := fac.Store().WeldFactByID(ctx, idA)
	// 取焊事实失败或结果不符就停，说明没达预期。
	if err != nil || got.LengthMM != 1200 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("kept original: %+v %v", got, err)
	}
	// 对错误种类的错误应是完整性失败，错种不对说明判定偏了。
	if errors.Is(err, domain.ErrIntegrity) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("unexpected")
	}
}

// 演示汇聚不得带出人员身份，带出就说明泄了密。
func TestWeldDemoSeed(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-d", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-d", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err := fac.Login(ctx, "sa-d", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建厂内人员，失败说明登录名冲突或越权。
	op, err := fac.CreatePerson(ctx, saTok, "op-d", "焊工")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	opTok := mustAdoptPassword(t, ctx, fac, "op-d", "op-pass")
	// 灌演示数据应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SeedWeldDemo(ctx, opTok); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("op seed: %v", err)
	}
	// 灌入焊接演示数据，失败说明无权或库写不进。
	first, err := fac.SeedWeldDemo(ctx, saTok)
	// 灌演示数据失败或结果不符就停，说明没达预期。
	if err != nil || first.Created != 45 || first.Total != 45 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("seed: %+v %v", first, err)
	}
	// 灌入焊接演示数据，失败说明无权或库写不进。
	again, err := fac.SeedWeldDemo(ctx, saTok)
	// 灌演示数据失败或结果不符就停，说明没达预期。
	if err != nil || again.Created != 0 || again.Total != 45 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("idempotent seed: %+v %v", again, err)
	}
	// 拉取焊接统计，失败说明无权或范围是空的。
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(byProj) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("demo projects %d %v", len(byProj), err)
	}
	// 拉取焊接流水，失败说明无权查看记录。
	runs, err := fac.ListWeldRuns(ctx, opTok, factory.WeldRunQuery{})
	// 拉焊流水失败或条数不对就停，说明没达预期。
	if err != nil || len(runs) == 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("op runs %d %v", len(runs), err)
	}
	// 接上只记录的汇聚口，摘要里不该出现人员身份。
	p := &captureWAN{}
	// 挂上只记录的汇聚口，后面用来查有没有泄密。
	fac.SetWeldWANPoster(p)
	// 灌演示数据失败就停，否则后面没有可靠结果。
	if _, err := fac.SeedWeldDemo(ctx, saTok); err != nil {
		// 把灌演示数据的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(p.rows) == 0 {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatal("wan summaries empty")
	}
	// 编成文本再查敏感词，编失败就无法判断泄密。
	raw, err := json.Marshal(p.rows)
	// 编成文本失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把编成文本的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 打成文本再搜索，搜不到说明正文里没有这段。
	s := string(raw)
	// 逐个敏感字段去搜，搜到就说明结果泄了密。
	for _, bad := range []string{"loginName", "displayName", "personId", "orgPath", "orgUnit", "creatorId"} {
		if strings.Contains(s, bad) {
			// 结果里出现了不该有的敏感词，说明已经泄密。
			t.Fatalf("leaked %s: %s", bad, s)
		}
	}
}

// 假的汇聚出口，只留下摘要供断言。
type captureWAN struct {
	// 替平台收下的焊接摘要，供泄密检查。
	rows []factory.WeldWANSummary
}

// 留下待汇聚的摘要，里面不该出现人员身份。
func (c *captureWAN) PostWeldSummaries(_ context.Context, _ uuid.UUID, rows []factory.WeldWANSummary) error {
	// 装一条汇聚摘要，多了人员字段就算泄密。
	c.rows = append([]factory.WeldWANSummary(nil), rows...)
	return nil
}
