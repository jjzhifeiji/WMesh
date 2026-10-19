// 焊长时长：同机多人分账、B 回连带上 A、报表按人不混。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
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
	all, err := fac.ListWeldReport(ctx, saTok, "", nil, nil, time.UTC)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(all) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa report %d %v", len(all), err)
	}
	// 拉取焊接统计，失败说明无权或范围是空的。
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil, time.UTC)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(byProj) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project report %d %v", len(byProj), err)
	}
	// 拉取焊接流水，失败说明无权查看记录。
	runs, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{})
	// 拉焊流水失败或条数不对就停，说明没达预期。
	if err != nil || len(runs.Items) != 2 || runs.Items[0].ProjectName == "" || runs.Total != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("runs: %+v %v", runs, err)
	}
	// 拉取焊接统计，失败说明无权或范围是空的。
	own, err := fac.ListWeldReport(ctx, aTok, "", nil, nil, time.UTC)
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
	scoped, err := fac.ListWeldReport(ctx, leadTok, "", nil, nil, time.UTC)
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
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil, time.UTC)
	// 拉焊统计失败或条数不对就停，说明没达预期。
	if err != nil || len(byProj) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("demo projects %d %v", len(byProj), err)
	}
	// 拉取焊接流水，失败说明无权查看记录。
	runs, err := fac.ListWeldRuns(ctx, opTok, factory.WeldRunQuery{})
	// 拉焊流水失败或条数不对就停，说明没达预期。
	if err != nil || len(runs.Items) == 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("op runs %d %v", len(runs.Items), err)
	}
	// 接上只记录的汇聚口，摘要里不该出现人员身份。
	p := &captureWAN{}
	// 挂上只记录的汇聚口，后面用来查有没有泄密。
	fac.SetWeldWANPoster(p)
	// 起后台上送循环，灌演示只发信号，由它推给汇聚口。
	startWANPush(t, fac)
	// 灌演示数据失败就停，否则后面没有可靠结果。
	if _, err := fac.SeedWeldDemo(ctx, saTok); err != nil {
		// 把灌演示数据的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 等后台推上来再取摘要，加锁免得和上送并发读写。
	sent := p.waitRows(t)
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(sent) == 0 {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatal("wan summaries empty")
	}
	// 编成文本再查敏感词，编失败就无法判断泄密。
	raw, err := json.Marshal(sent)
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
	// 护住收下的摘要，上送和断言可能不在同一协程。
	mu sync.Mutex
	// 替平台收下的焊接摘要，供泄密检查。
	rows []factory.WeldWANSummary
}

// 留下待汇聚的摘要，里面不该出现人员身份。
func (c *captureWAN) PostWeldSummaries(_ context.Context, _ uuid.UUID, rows []factory.WeldWANSummary) error {
	// 加锁再写，免得和断言并发读写。
	c.mu.Lock()
	// 用完就放锁，别把下一次上送卡住。
	defer c.mu.Unlock()
	// 装一条汇聚摘要，多了人员字段就算泄密。
	c.rows = append([]factory.WeldWANSummary(nil), rows...)
	return nil
}

// 加锁拷出最近一次收到的摘要，供断言读。
func (c *captureWAN) snapshot() []factory.WeldWANSummary {
	// 加锁再读，免得读到写了一半的切片。
	c.mu.Lock()
	// 拷完就放锁，别把上送卡住。
	defer c.mu.Unlock()
	// 交回一份拷贝，断言改它不影响夹具。
	return append([]factory.WeldWANSummary(nil), c.rows...)
}

// 单条焊长、时长超上限或工程名超长都要拒收，入口和库层都拦。
func TestWeldFlushRejectsOutOfRange(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-cap", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-cap", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err := fac.Login(ctx, "sa-cap", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 取一个固定焊完时间，断言不随当前时刻变。
	at := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	// 造一条焊事实，各用例只改要测的字段。
	mk := func(length, dur int64, name string) factory.WeldFact {
		// 交回带新身份的焊事实，创建人是超管本人。
		return factory.WeldFact{
			ID: id.New(), CreatorID: created.SuperAdminID, OrgPath: []factory.PathNode{},
			ProjectName: name, WeldKind: "single", LengthMM: length, DurationSec: dur, OccurredAt: at,
		}
	}
	// 正好卡在上限、名字两头带空白的一条应收下。
	edge := mk(store.WeldMaxLengthMM, store.WeldMaxDurationSec, " "+strings.Repeat("工", store.WeldMaxProjectName)+"\t")
	// 焊长超上限一毫米，应拒收。
	longRun := mk(store.WeldMaxLengthMM+1, 1, "工程A")
	// 时长超上限一秒，应拒收。
	slowRun := mk(1, store.WeldMaxDurationSec+1, "工程A")
	// 工程名去空白后多一个字，应拒收。
	wideName := mk(1, 1, strings.Repeat("工", store.WeldMaxProjectName+1))
	// 一批送上去，看哪些收下哪些拒收。
	out, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{edge, longRun, slowRun, wideName})
	// 汇聚口报错或收下的不是卡边那条就停。
	if err != nil || len(out.Accepted) != 1 || out.Accepted[0] != edge.ID || len(out.Rejected) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("flush: %+v %v", out, err)
	}
	// 按身份记下期望的拒收原因。
	want := map[uuid.UUID]string{
		longRun.ID:  domain.ErrForbidden.Error(),
		slowRun.ID:  domain.ErrForbidden.Error(),
		wideName.ID: domain.ErrInvalidName.Error(),
	}
	// 逐条核对拒收原因，错种不对说明拦错了地方。
	for _, r := range out.Rejected {
		// 原因和期望不符就停。
		if want[r.ID] != r.Error {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("reject %s: %q want %q", r.ID, r.Error, want[r.ID])
		}
	}
	// 库层也要拦，绕过汇聚口直接写超长焊应拒收。
	if _, err := fac.Store().MergeWeldFact(ctx, longRun); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("store long: %v", err)
	}
	// 库层也要拦，绕过汇聚口直接写超时长焊应拒收。
	if _, err := fac.Store().MergeWeldFact(ctx, slowRun); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("store slow: %v", err)
	}
	// 库层也要拦，超长工程名应按名字不合规拒收。
	if _, err := fac.Store().MergeWeldFact(ctx, wideName); !errors.Is(err, domain.ErrInvalidName) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("store name: %v", err)
	}
	// 被拒的那条不该落库，查到就说明拦晚了。
	if _, err := fac.Store().WeldFactByID(ctx, longRun.ID); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("long stored: %v", err)
	}
}

// 库里已有超界值和脏工程名时，个人统计不报错且封顶，上送 WAN 的行不撞键。
func TestWeldSumsCapAndWANNames(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 另开一座厂并拿到库连接，坏数据只能用原生 SQL 写。
	fac, db, saTok, saID := weldRawFactory(t, ctx)
	// 取出本厂身份，原生写入的行要归到这座厂。
	facID := fac.Store().FactoryID()
	// 取一个固定焊完时间，所有行落在同一天。
	at := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	// 第 200 字处是空白的超长名，截断后要再去一次空白。
	long := strings.Repeat("焊", store.WeldMaxProjectName-1) + " " + strings.Repeat("段", 10)
	// 两条 int64 最大值同组，求和会超出 int64。
	insertRawWeld(t, db, facID, saID, "工程A", "single", math.MaxInt64, math.MaxInt64, at)
	// 第二条最大值，和上一条落在同一组。
	insertRawWeld(t, db, facID, saID, "工程A", "single", math.MaxInt64, math.MaxInt64, at)
	// 只差首尾制表符和全角空格的同名，规范后要并进工程A。
	insertRawWeld(t, db, facID, saID, "\t工程A\u3000", "single", 5, 5, at)
	// 超长名，上送时截到上限。
	insertRawWeld(t, db, facID, saID, long, "multilayer", 7, 3, at)
	// 只有空白的名字，规范后归到未关联工程。
	insertRawWeld(t, db, facID, saID, "\u3000\t", "tbar", 2, 2, at)
	// 空名字，库里查询就归到未关联工程。
	insertRawWeld(t, db, facID, saID, "", "tbar", 3, 3, at)
	// 拉本人统计，超界值也不该让查询报错。
	stats, err := fac.MyWeldStats(ctx, saTok)
	// 报错或合计没封顶就停。
	if err != nil || stats.LengthMM != math.MaxInt64 || stats.DurationSec != math.MaxInt64 || stats.RunCount != 6 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("my stats: %+v %v", stats, err)
	}
	// 记下按工程合计里有没有工程A。
	found := false
	// 逐个工程核对，封顶后不该出现负数。
	for _, p := range stats.Projects {
		// 出现负数说明求和回绕了。
		if p.LengthMM < 0 || p.DurationSec < 0 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("project wrapped: %+v", p)
		}
		// 工程A两条最大值要封顶，次数照实算。
		if p.ProjectName == "工程A" {
			// 标记找到了工程A。
			found = true
			// 没封顶或次数不对就停。
			if p.LengthMM != math.MaxInt64 || p.DurationSec != math.MaxInt64 || p.RunCount != 2 {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("project cap: %+v", p)
			}
		}
	}
	// 找不到工程A说明分组不对。
	if !found {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("projects: %+v", stats.Projects)
	}
	// 接上只记录的上送口，同步推一次。
	p := &captureWAN{}
	// 挂上假上送口，后面看推出去的行。
	fac.SetWeldWANPoster(p)
	// 同步推汇总，超界值不该让汇总查询报错。
	if err := fac.PushWANSummaries(ctx); err != nil {
		// 把上送的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 加锁取出推出去的行。
	sent := p.snapshot()
	// 规范后只剩工程A、超长名、未关联三粒。
	if len(sent) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sent rows: %+v", sent)
	}
	// 记下见过的日、工程名、模式，查有没有撞键。
	seen := map[[3]string]bool{}
	// 逐行核对名字和合计。
	for _, r := range sent {
		// 拼出这一行的键。
		key := [3]string{r.Day, r.ProjectName, r.WeldKind}
		// 同键出现两次 WAN 会撞唯一索引。
		if seen[key] {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("dup key %v: %+v", key, sent)
		}
		// 记下这个键。
		seen[key] = true
		// 名字为空、超长或两头带空白，WAN 都会拒收。
		if r.ProjectName == "" || strings.TrimSpace(r.ProjectName) != r.ProjectName || utf8.RuneCountInString(r.ProjectName) > store.WeldMaxProjectName {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("bad name %q", r.ProjectName)
		}
		// 出现负数说明合并时回绕了。
		if r.RunCount < 0 || r.LengthMM < 0 || r.DurationSec < 0 {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("wrapped: %+v", r)
		}
	}
	// 工程A三条合并后封顶，次数照实加。
	if got := findWANRow(sent, "工程A", "single"); got == nil || got.RunCount != 3 || got.LengthMM != math.MaxInt64 || got.DurationSec != math.MaxInt64 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project A: %+v", got)
	}
	// 超长名截断后去掉落在末尾的空白。
	if got := findWANRow(sent, strings.Repeat("焊", store.WeldMaxProjectName-1), "multilayer"); got == nil || got.LengthMM != 7 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("long name: %+v", sent)
	}
	// 空名和纯空白名并成一粒未关联工程。
	if got := findWANRow(sent, store.WeldUnsetProject, "tbar"); got == nil || got.RunCount != 2 || got.LengthMM != 5 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("unset: %+v", sent)
	}
}

// 按工程名和模式找出上送的一行，找不到交回空。
func findWANRow(rows []factory.WeldWANSummary, name, kind string) *factory.WeldWANSummary {
	// 逐行比对工程名和模式。
	for i := range rows {
		// 名字和模式都对上就是这一行。
		if rows[i].ProjectName == name && rows[i].WeldKind == kind {
			return &rows[i]
		}
	}
	return nil
}

// 另开一座厂并交回库连接、超管令牌和身份，供原生 SQL 写坏数据。
func weldRawFactory(t *testing.T, ctx context.Context) (*factory.Service, *gorm.DB, string, uuid.UUID) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 打开管理库，用来另开一座空库。
	admin := testpg.Open(t)
	// 另开一座空库，测完自动删掉。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 迁好库，留着连接给原生 SQL 用。
	db := testpg.OpenMigrated(t, dsn)
	// 装上厂服务，工厂身份新取一个。
	fac := factory.NewService(store.Open(db, id.New()))
	// 夹具不连 WAN，自签一把租约才能写厂库正文。
	if err := fac.Store().GrantLocalLease(ctx); err != nil {
		// 把签发租约的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 编一个工厂短码，和建厂夹具一致。
	short, err := assetcode.FormatFactory(1)
	// 编短码失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把编短码的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写工厂短码失败就停，否则后面没有可靠结果。
	if err := fac.Store().PutFactoryShortCode(ctx, short); err != nil {
		// 把写短码的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写入首个超管并拿激活码。
	acc, token, err := fac.BootstrapInitial(ctx, "sa-raw", "超管")
	// 写首个超管失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把写超管的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-raw", token, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err := fac.Login(ctx, "sa-raw", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return fac, db, tok, acc.ID
}

// 绕过入口校验直接写一条焊事实，模拟库里已有的坏数据。
func insertRawWeld(t *testing.T, db *gorm.DB, facID, creator uuid.UUID, name, kind string, length, dur int64, at time.Time) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 用原生 SQL 写一行厂直属焊事实，不经汇聚口的上限校验。
	err := db.Exec(`INSERT INTO weld_facts (id, creator_id, factory_id, org_path, project_name, weld_kind, length_mm, duration_sec, occurred_at)
		VALUES (?, ?, ?, CAST('[]' AS jsonb), ?, ?, ?, ?, ?)`, id.New(), creator, facID, name, kind, length, dur, at).Error
	// 写不进去就停，后面断言都没意义。
	if err != nil {
		// 把写库的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
}
