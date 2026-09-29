// 验收 WAN 焊汇总：只按厂/日/工程名/模式，不含人员组织。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
	// 厂上报焊汇总应被拒为名称不合法，放行或错类都算没拦住。
	if err := h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		// 准备「x」这条汇总，字段错了对不上。
		{Day: "2026-09-17", ProjectName: "x", WeldKind: "nope", RunCount: 1, LengthMM: 1, DurationSec: 1},
	}); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("bad kind: %v", err)
	}
}
