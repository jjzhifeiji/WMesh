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

func TestWeldSummariesNoPerson(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	fid := created.Factory.ID
	err = h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		{Day: "2026-09-16", ProjectName: "单层-舷侧分段", WeldKind: "single", RunCount: 10, LengthMM: 8000, DurationSec: 400},
		{Day: "2026-09-17", ProjectName: "", WeldKind: "tbar", RunCount: 2, LengthMM: 1500, DurationSec: 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	rows, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), &from, &to)
	if err != nil || len(rows) != 2 {
		t.Fatalf("list %d %v", len(rows), err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"loginName", "displayName", "personId", "orgPath", "orgUnit"} {
		if strings.Contains(s, bad) {
			t.Fatalf("leaked %s: %s", bad, s)
		}
	}
	unset := false
	for _, r := range rows {
		if r.ProjectName == "未关联工程" {
			unset = true
		}
	}
	if rows[0].FactoryName != "厂A" || !unset {
		t.Fatalf("views %+v", rows)
	}
	if err := h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		{Day: "2026-09-17", ProjectName: "多层-箱体", WeldKind: "multilayer", RunCount: 1, LengthMM: 100, DurationSec: 10},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = h.WAN.Stats.ListWeldReports(ctx, tok, "", nil, nil)
	if err != nil || len(rows) != 1 || rows[0].ProjectName != "多层-箱体" {
		t.Fatalf("replace %+v %v", rows, err)
	}
	if err := h.WAN.Stats.PutFromFactory(ctx, fid, []service.WeldSummaryIn{
		{Day: "2026-09-17", ProjectName: "x", WeldKind: "nope", RunCount: 1, LengthMM: 1, DurationSec: 1},
	}); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("bad kind: %v", err)
	}
}
