// 焊次带工艺清单和工程修订（D-109）：旧焊次照收，幂等不变，WAN 汇总不带，下钻查得到。
package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 工艺清单照收照读；认不认识不管；同一条重发幂等，改了清单算冲突；不合规的只拒这一条。
func TestWeldFactProcesses(t *testing.T) {
	// 准备无取消的上下文。
	ctx := context.Background()
	// 拉起隔离厂库。
	h := New(t)
	// 建厂并签发超管。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 开通超管口令。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 超管登录。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 焊工。
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 焊工的个人工艺，用来核下钻时名字给不给。
	own, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "焊工工艺", []byte(`{"name":"焊工工艺"}`), id.New(), "", nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 一条本厂认识的工艺，一条已删或没见过的。
	unknown := id.New()
	// 清单只有身份、修订和摘要。
	uses := []factory.WeldProcessUse{
		{ProcessID: own.ID, Revision: own.Revision, Digest: own.Digest},
		{ProcessID: unknown, Revision: 3, Digest: digest.Sum([]byte("gone"))},
	}
	// 焊完时间。
	at := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	// 工程身份。
	proj := id.New()
	// 新口径的焊次。
	fact := factory.WeldFact{
		ID: id.New(), CreatorID: op.acc.ID, ProjectID: &proj, ProjectName: "工程甲", WeldKind: "single",
		LengthMM: 100, DurationSec: 10, OccurredAt: at, ProjectRevision: 5, Processes: uses,
	}
	// 老示教器和老焊次：两项都没有。
	old := factory.WeldFact{
		ID: id.New(), CreatorID: op.acc.ID, ProjectID: &proj, ProjectName: "工程甲", WeldKind: "single",
		LengthMM: 50, DurationSec: 5, OccurredAt: at.Add(-time.Hour),
	}
	// 摘要长度不对的只拒这一条。
	badDigest := factory.WeldFact{
		ID: id.New(), CreatorID: op.acc.ID, LengthMM: 1, DurationSec: 1, OccurredAt: at,
		Processes: []factory.WeldProcessUse{{ProcessID: unknown, Revision: 1, Digest: []byte("short")}},
	}
	// 修订为负的也只拒这一条。
	badRev := factory.WeldFact{ID: id.New(), CreatorID: op.acc.ID, LengthMM: 1, DurationSec: 1, OccurredAt: at, ProjectRevision: -1}
	// 一批发出去。
	out, err := fac.FlushWeldFacts(ctx, op.tok, []factory.WeldFact{fact, old, badDigest, badRev})
	// 两条收、两条拒。
	if err != nil || len(out.Accepted) != 2 || len(out.Rejected) != 2 || out.Rejected[0].Error != domain.ErrForbidden.Error() {
		// 把结果打出来。
		t.Fatalf("flush %+v %v", out, err)
	}
	// 同样两条再发一遍，幂等。
	again, err := fac.FlushWeldFacts(ctx, op.tok, []factory.WeldFact{fact, old})
	// 都算收下。
	if err != nil || len(again.Accepted) != 2 || len(again.Rejected) != 0 {
		// 把结果打出来。
		t.Fatalf("idempotent %+v %v", again, err)
	}
	// 同一身份换了清单。
	clash := fact
	// 少一条工艺。
	clash.Processes = uses[:1]
	// 不能覆盖。
	res, err := fac.FlushWeldFacts(ctx, op.tok, []factory.WeldFact{clash})
	// 按完整性拒绝。
	if err != nil || len(res.Rejected) != 1 || res.Rejected[0].Error != domain.ErrIntegrity.Error() {
		// 把结果打出来。
		t.Fatalf("clash processes %+v %v", res, err)
	}
	// 同一身份换了工程修订。
	clash = fact
	// 修订不同。
	clash.ProjectRevision = 6
	// 不能覆盖。
	res, err = fac.FlushWeldFacts(ctx, op.tok, []factory.WeldFact{clash})
	// 按完整性拒绝。
	if err != nil || len(res.Rejected) != 1 || res.Rejected[0].Error != domain.ErrIntegrity.Error() {
		// 把结果打出来。
		t.Fatalf("clash revision %+v %v", res, err)
	}
	// 超管下钻：看得到工艺名。
	page, err := fac.ListWeldRuns(ctx, sa, factory.WeldRunQuery{})
	// 两条，新的在前。
	if err != nil || len(page.Items) != 2 {
		// 把结果打出来。
		t.Fatalf("runs %+v %v", page, err)
	}
	// 新焊次。
	run := page.Items[0]
	// 工程修订和清单都在，名字补上了，没见过的那条只留身份。
	if run.ProjectRevision != 5 || len(run.Processes) != 2 || run.Processes[0].Name != "焊工工艺" || run.Processes[0].Code == "" || run.Processes[1].ProcessID != unknown || run.Processes[1].Name != "" || run.Processes[1].Revision != 3 {
		// 把结果打出来。
		t.Fatalf("run %+v", run)
	}
	// 老焊次两项都空。
	if page.Items[1].ProjectRevision != 0 || page.Items[1].Processes != nil {
		// 把结果打出来。
		t.Fatalf("old run %+v", page.Items[1])
	}
	// 管理员看不到别人的个人工艺名，只有身份、修订和摘要。
	admin := mustCreateRole(t, ctx, fac, sa, "admin", "admin-pass", factory.RoleOrgAdmin)
	// 管理员下钻。
	seen, err := fac.ListWeldRuns(ctx, admin.tok, factory.WeldRunQuery{})
	// 身份还在，名字没有。
	if err != nil || len(seen.Items) != 2 || seen.Items[0].Processes[0].ProcessID != own.ID || seen.Items[0].Processes[0].Name != "" {
		// 把结果打出来。
		t.Fatalf("admin runs %+v %v", seen, err)
	}
	// WAN 汇总的形状没动：只有日、工程名、模式和合计，两条并成一粒。
	sums, err := fac.Store().ListWeldWANSummaries(ctx)
	// 只有一粒，次数是 2。
	if err != nil || len(sums) != 1 || sums[0].RunCount != 2 || sums[0].LengthMM != 150 || strings.Contains(sums[0].ProjectName, own.ID.String()) {
		// 把结果打出来。
		t.Fatalf("wan %+v %v", sums, err)
	}
}
