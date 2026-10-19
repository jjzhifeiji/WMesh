// 验收 WAN 焊汇总：只按厂/日/工程名/模式，不含人员组织。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 焊汇总只按厂、日、工程名和模式，不含人员。
func TestWeldSummariesNoPerson(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 记下这家厂，停用和上报都指它。
	fid := created.Factory.ID
	// 厂上报焊汇总，上报失败汇总不会被替换。
	err = h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		{Day: "2026-09-16", ProjectName: "单层-舷侧分段", WeldKind: "single", RunCount: 10, LengthMM: 8000, DurationSec: 400},
		{Day: "2026-09-17", ProjectName: "", WeldKind: "tbar", RunCount: 2, LengthMM: 1500, DurationSec: 90},
	})
	// 厂上报焊汇总失败就停，上报失败汇总不会被替换。
	if err != nil {
		// 不符即停：厂上报焊汇总失败。
		t.Fatal(err)
	}
	// 定查询起始日，日期错了汇总会漏或算多。
	from := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	// 定查询起始日，日期错了汇总会漏或算多。
	to := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	// 把身份收成文本，后面比对焊道引用靠它。
	rows, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), &from, &to)
	// 查焊汇总失败或条数不是2就停，不能当通过。
	if err != nil || len(rows) != 2 {
		// 不符即停：查焊汇总失败或条数不是2。
		t.Fatalf("list %d %v", len(rows), err)
	}
	// 把结果打成文本，以便检查有没有人员字段。
	raw, err := json.Marshal(rows)
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 不符即停：导出字段表失败。
		t.Fatal(err)
	}
	// 把结果打成文本，好查有没有不该出现的字段。
	s := string(raw)
	// 逐个敏感词去搜，出现人员字段就是泄密。
	for _, bad := range []string{"loginName", "displayName", "personId", "orgPath", "orgUnit"} {
		if strings.Contains(s, bad) {
			// 这里正文留着不该有的旧字段就停，这一步不能算通过。
			t.Fatalf("leaked %s: %s", bad, s)
		}
	}
	// 先当没找到，循环里命中再改成真。
	unset := false
	// 逐条查看结果，漏看一条会把结论判错。
	for _, r := range rows {
		// 条件成立才做这一支，漏进来会算错样本。
		if r.ProjectName == "未关联工程" {
			// 标成已命中，最后仍为假说明名单里没有。
			unset = true
		}
	}
	// 未关联工程名称没有回写或没有找到就停，不能当通过。
	if rows[0].FactoryName != "厂A" || !unset {
		// 不符即停：未关联工程名称没有回写或没有找到。
		t.Fatalf("views %+v", rows)
	}
	// 厂上报焊汇总失败就停，上报失败汇总不会被替换。
	if err := h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		// 准备「多层-箱体」这条汇总，字段错了对不上。
		{Day: "2026-09-17", ProjectName: "多层-箱体", WeldKind: "multilayer", RunCount: 1, LengthMM: 100, DurationSec: 10},
	}); err != nil {
		t.Fatal(err)
	}
	// 查焊汇总，汇总读不到就对不了条数。
	rows, err = h.WAN.Stats.ListWeldReports(ctx, tok, "", nil, nil)
	// 查焊汇总失败或条数不是1就停，不能当通过。
	if err != nil || len(rows) != 1 || rows[0].ProjectName != "多层-箱体" {
		// 不符即停：查焊汇总失败或条数不是1。
		t.Fatalf("replace %+v %v", rows, err)
	}
	// 模式不认识的一行只跳过，整批照收，回执里交代原因。
	receipt, err := h.WAN.Stats.PutFromFactoryReport(ctx, fid, []service.WeldSummaryIn{
		// 准备「x」这条汇总，模式不认识。
		{Day: "2026-09-17", ProjectName: "x", WeldKind: "nope", RunCount: 1, LengthMM: 1, DurationSec: 1},
	})
	// 报错或回执没记下这一行，就是还在整批拒绝。
	if err != nil || receipt.SkippedCount != 1 || len(receipt.Skipped) != 1 || receipt.Skipped[0] != (service.WeldSummarySkip{Index: 0, Reason: "invalid weld kind"}) {
		// 不符即停：坏模式没按跳过处理。
		t.Fatalf("bad kind: %+v %v", receipt, err)
	}
}

// 起云端、登录并登记一家厂，焊汇总用例共用。
func weldFactory(t *testing.T) (*Harness, string, uuid.UUID) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面查不了汇总。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面查不了汇总。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记一家厂，上送和查询都指它。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂失败就停，没有厂可上送。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	return h, tok, created.Factory.ID
}

// 超过旧上限五千行的整表照收，不再整批拒绝。
func TestWeldSummariesNoRowCap(t *testing.T) {
	// 准备本测上下文，上送和查询都用它。
	ctx := context.Background()
	// 起云端并登记一家厂。
	h, tok, fid := weldFactory(t)
	// 按六千行预留，超过旧上限一截。
	rows := make([]service.WeldSummaryIn, 0, 6000)
	// 逐行造汇总，工程名带序号免得撞键。
	for i := range 6000 {
		// 一行一个工程名，键各不相同。
		rows = append(rows, service.WeldSummaryIn{
			Day: "2026-09-17", ProjectName: fmt.Sprintf("工程-%04d", i), WeldKind: "single",
			RunCount: 1, LengthMM: 10, DurationSec: 1,
		})
	}
	// 整表上送，收下回执看有没有跳过。
	receipt, err := h.WAN.Stats.PutFromFactoryReport(ctx, fid, rows)
	// 报错或有跳过，说明行数还被卡着。
	if err != nil || receipt.SkippedCount != 0 || len(receipt.Skipped) != 0 {
		// 不符即停：六千行没被整表收下。
		t.Fatalf("put 6000: %+v %v", receipt, err)
	}
	// 查这家厂的汇总，看是否全部入库。
	got, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或条数不对就停。
	if err != nil || len(got) != 6000 {
		// 不符即停：入库条数不是六千。
		t.Fatalf("list %d %v", len(got), err)
	}
}

// 一行好一行坏，好行入库；坏行过百时回执只列一百条，总数照实。
func TestWeldSummariesSkipBadRows(t *testing.T) {
	// 准备本测上下文，上送和查询都用它。
	ctx := context.Background()
	// 起云端并登记一家厂。
	h, tok, fid := weldFactory(t)
	// 一行好、一行负数，负数那行应被跳过。
	receipt, err := h.WAN.Stats.PutFromFactoryReport(ctx, fid, []service.WeldSummaryIn{
		// 好的一行，应入库。
		{Day: "2026-09-17", ProjectName: "好", WeldKind: "single", RunCount: 1, LengthMM: 10, DurationSec: 1},
		// 坏的一行，焊长是负数。
		{Day: "2026-09-17", ProjectName: "坏", WeldKind: "single", RunCount: 1, LengthMM: -1, DurationSec: 1},
	})
	// 报错或回执不是恰好跳过第二行，就停。
	if err != nil || receipt.SkippedCount != 1 || len(receipt.Skipped) != 1 || receipt.Skipped[0] != (service.WeldSummarySkip{Index: 1, Reason: "negative value"}) {
		// 不符即停：坏行没按跳过处理。
		t.Fatalf("one bad: %+v %v", receipt, err)
	}
	// 查这家厂的汇总，应只剩好的那行。
	got, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或好行没入库就停。
	if err != nil || len(got) != 1 || got[0].ProjectName != "好" {
		// 不符即停：好行没入库或坏行混进来了。
		t.Fatalf("list %+v %v", got, err)
	}
	// 先放一行好的，后面接一百五十行坏的。
	rows := []service.WeldSummaryIn{{Day: "2026-09-18", ProjectName: "好", WeldKind: "tbar", RunCount: 2, LengthMM: 20, DurationSec: 2}}
	// 逐行造坏行，日期都不合法。
	for i := range 150 {
		// 日期写成不存在的月份，必然跳过。
		rows = append(rows, service.WeldSummaryIn{Day: "2026-13-01", ProjectName: fmt.Sprintf("坏-%d", i), WeldKind: "single", RunCount: 1})
	}
	// 整表上送，收下回执看跳过条数。
	receipt, err = h.WAN.Stats.PutFromFactoryReport(ctx, fid, rows)
	// 报错、列表不是一百条或总数不对，就停。
	if err != nil || len(receipt.Skipped) != 100 || receipt.SkippedCount != 150 {
		// 不符即停：回执条数不对。
		t.Fatalf("many bad: skipped %d count %d %v", len(receipt.Skipped), receipt.SkippedCount, err)
	}
	// 列出的是最前面的坏行，序号从第二行起。
	if receipt.Skipped[0] != (service.WeldSummarySkip{Index: 1, Reason: "invalid day"}) || receipt.Skipped[99].Index != 100 {
		// 不符即停：列出的不是最前面的坏行。
		t.Fatalf("first skips %+v %+v", receipt.Skipped[0], receipt.Skipped[99])
	}
	// 再查一次，整表替换后只剩这次的好行。
	got, err = h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或好行不对就停。
	if err != nil || len(got) != 1 || got[0].Day != "2026-09-18" || got[0].WeldKind != "tbar" {
		// 不符即停：整表替换后好行不对。
		t.Fatalf("list after many bad %+v %v", got, err)
	}
}

// 工程名只差首尾空白的合成一行，数值相加；加爆了封顶不报错。
func TestWeldSummariesMergeTrimmedNames(t *testing.T) {
	// 准备本测上下文，上送和查询都用它。
	ctx := context.Background()
	// 起云端并登记一家厂。
	h, tok, fid := weldFactory(t)
	// 「 A」和「A」同日同模式，应合成一行；B 两行都是上限，合并要封顶。
	receipt, err := h.WAN.Stats.PutFromFactoryReport(ctx, fid, []service.WeldSummaryIn{
		// 名字带前导空格的 A。
		{Day: "2026-09-17", ProjectName: " A", WeldKind: "single", RunCount: 1, LengthMM: 10, DurationSec: 2},
		// 名字干净的 A，应并进上一行。
		{Day: "2026-09-17", ProjectName: "A", WeldKind: "single", RunCount: 3, LengthMM: 30, DurationSec: 4},
		// 名字干净的 B，数值都在上限。
		{Day: "2026-09-17", ProjectName: "B", WeldKind: "single", RunCount: math.MaxInt64, LengthMM: math.MaxInt64, DurationSec: math.MaxInt64},
		// 名字带尾随空白的 B，加上去会越界。
		{Day: "2026-09-17", ProjectName: "B\t ", WeldKind: "single", RunCount: math.MaxInt64, LengthMM: math.MaxInt64, DurationSec: math.MaxInt64},
	})
	// 合并不该报错，也不该跳过任何一行。
	if err != nil || receipt.SkippedCount != 0 {
		// 不符即停：合并报错或误跳过。
		t.Fatalf("merge: %+v %v", receipt, err)
	}
	// 查这家厂的汇总，应只剩 A、B 两行。
	got, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或条数不是两行就停。
	if err != nil || len(got) != 2 {
		// 不符即停：同名没合成一行。
		t.Fatalf("list %+v %v", got, err)
	}
	// 按名字收起来，后面逐行核对数值。
	byName := map[string]service.WeldSummaryView{}
	// 逐行按工程名放好。
	for _, r := range got {
		// 名字作键，后面直接取。
		byName[r.ProjectName] = r
	}
	// A 的三项应是两行之和。
	if a := byName["A"]; a.RunCount != 4 || a.LengthMM != 40 || a.DurationSec != 6 {
		// 不符即停：A 没有相加。
		t.Fatalf("A %+v", a)
	}
	// B 的三项应封顶在上限，不溢出成负数。
	if b := byName["B"]; b.RunCount != math.MaxInt64 || b.LengthMM != math.MaxInt64 || b.DurationSec != math.MaxInt64 {
		// 不符即停：B 没有封顶。
		t.Fatalf("B %+v", b)
	}
}

// 名录里没有的厂上送汇总按已注销拒绝，不写库。
func TestWeldSummariesUnknownFactory(t *testing.T) {
	// 准备本测上下文，上送用它。
	ctx := context.Background()
	// 起云端库和服务，名录里一家厂都没有。
	h := New(t)
	// 随手生成一个名录里没有的厂身份。
	ghost := uuid.New()
	// 用这个身份上送一行，应按已注销拒绝。
	_, err := h.WAN.Stats.PutFromFactoryReport(ctx, ghost, []service.WeldSummaryIn{
		// 一行合法汇总，拒绝只能是因为厂不在名录。
		{Day: "2026-09-17", ProjectName: "A", WeldKind: "single", RunCount: 1, LengthMM: 10, DurationSec: 1},
	})
	// 没按已注销拒绝，说明还在放行未知厂。
	if !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：未知厂上送没被拒。
		t.Fatalf("unknown factory put: %v", err)
	}
}
