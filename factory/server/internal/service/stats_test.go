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

func TestWeldStatsSeparatePeople(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok, err := fac.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	shop, err := fac.CreateOrgUnit(ctx, saTok, "车间A", nil)
	if err != nil {
		t.Fatal(err)
	}
	shopB, err := fac.CreateOrgUnit(ctx, saTok, "车间B", nil)
	if err != nil {
		t.Fatal(err)
	}
	pa, err := fac.CreatePerson(ctx, saTok, "op-a", "焊工A")
	if err != nil {
		t.Fatal(err)
	}
	pb, err := fac.CreatePerson(ctx, saTok, "op-b", "焊工B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, pa.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, pb.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, pa.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, pb.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}
	aTok := mustAdoptPassword(t, ctx, fac, "op-a", "a-pass")
	bTok := mustAdoptPassword(t, ctx, fac, "op-b", "b-pass")
	sess, err := fac.LoginPad(ctx, "op-a", "a-pass")
	if err != nil || sess.Work.OrgUnitID == nil || *sess.Work.OrgUnitID != shop.ID {
		t.Fatalf("a work: %+v %v", sess.Work, err)
	}

	idA := id.New()
	idB := id.New()
	projA := id.New()
	projB := id.New()
	day1 := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	factA := factory.WeldFact{
		ID: idA, CreatorID: pa.ID, OrgUnitID: &shop.ID, OrgPath: sess.Work.OrgPath,
		ProjectID: &projA, ProjectName: "单层-梁1", WeldKind: "single",
		LengthMM: 1200, DurationSec: 60, OccurredAt: day1,
	}
	sessB, err := fac.LoginPad(ctx, "op-b", "b-pass")
	if err != nil {
		t.Fatal(err)
	}
	factB := factory.WeldFact{
		ID: idB, CreatorID: pb.ID, OrgUnitID: &shopB.ID, OrgPath: sessB.Work.OrgPath,
		ProjectID: &projB, ProjectName: "多层-箱体", WeldKind: "multilayer",
		LengthMM: 800, DurationSec: 40, OccurredAt: day2,
	}
	// B 回连把 A、B 一起送，厂端按创建人分开。
	out, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{factA, factB})
	if err != nil || len(out.Accepted) != 2 || len(out.Rejected) != 0 {
		t.Fatalf("flush: %+v %v", out, err)
	}
	again, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{factA, factB})
	if err != nil || len(again.Accepted) != 2 {
		t.Fatalf("idempotent: %+v %v", again, err)
	}
	bad := factA
	bad.LengthMM = 9999
	clash, err := fac.FlushWeldFacts(ctx, bTok, []factory.WeldFact{bad})
	if err != nil || len(clash.Rejected) != 1 || clash.Rejected[0].Error != domain.ErrIntegrity.Error() {
		t.Fatalf("integrity: %+v %v", clash, err)
	}

	aStats, err := fac.MyWeldStats(ctx, aTok)
	if err != nil || aStats.LengthMM != 1200 || aStats.DurationSec != 60 || aStats.RunCount != 1 {
		t.Fatalf("a totals: %+v %v", aStats, err)
	}
	if len(aStats.Projects) != 1 || aStats.Projects[0].ProjectName != "单层-梁1" || len(aStats.Recent) != 1 {
		t.Fatalf("a projects: %+v", aStats)
	}
	bStats, err := fac.MyWeldStats(ctx, bTok)
	if err != nil || bStats.LengthMM != 800 || bStats.DurationSec != 40 {
		t.Fatalf("b totals: %+v %v", bStats, err)
	}

	all, err := fac.ListWeldReport(ctx, saTok, "", nil, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("sa report %d %v", len(all), err)
	}
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil)
	if err != nil || len(byProj) != 2 {
		t.Fatalf("project report %d %v", len(byProj), err)
	}
	runs, err := fac.ListWeldRuns(ctx, saTok, factory.WeldRunQuery{})
	if err != nil || len(runs) != 2 || runs[0].ProjectName == "" {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	own, err := fac.ListWeldReport(ctx, aTok, "", nil, nil)
	if err != nil || len(own) != 1 || own[0].PersonID == nil || *own[0].PersonID != pa.ID || own[0].LengthMM != 1200 {
		t.Fatalf("a report: %+v %v", own, err)
	}
	lead, err := fac.CreatePerson(ctx, saTok, "lead-a", "车间A负责人")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, lead.ID, factory.RoleOrgLead, factory.ScopeOrgUnit, &shop.ID); err != nil {
		t.Fatal(err)
	}
	leadTok := mustAdoptPassword(t, ctx, fac, "lead-a", "lead-pass")
	scoped, err := fac.ListWeldReport(ctx, leadTok, "", nil, nil)
	if err != nil || len(scoped) != 1 || scoped[0].PersonID == nil || *scoped[0].PersonID != pa.ID {
		t.Fatalf("lead report: %+v %v", scoped, err)
	}
	got, err := fac.Store().WeldFactByID(ctx, idA)
	if err != nil || got.LengthMM != 1200 {
		t.Fatalf("kept original: %+v %v", got, err)
	}
	if errors.Is(err, domain.ErrIntegrity) {
		t.Fatal("unexpected")
	}
}

func TestWeldDemoSeed(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-d", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-d", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok, err := fac.Login(ctx, "sa-d", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	op, err := fac.CreatePerson(ctx, saTok, "op-d", "焊工")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	opTok := mustAdoptPassword(t, ctx, fac, "op-d", "op-pass")
	if _, err := fac.SeedWeldDemo(ctx, opTok); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op seed: %v", err)
	}
	first, err := fac.SeedWeldDemo(ctx, saTok)
	if err != nil || first.Created != 45 || first.Total != 45 {
		t.Fatalf("seed: %+v %v", first, err)
	}
	again, err := fac.SeedWeldDemo(ctx, saTok)
	if err != nil || again.Created != 0 || again.Total != 45 {
		t.Fatalf("idempotent seed: %+v %v", again, err)
	}
	byProj, err := fac.ListWeldReport(ctx, saTok, "project", nil, nil)
	if err != nil || len(byProj) != 3 {
		t.Fatalf("demo projects %d %v", len(byProj), err)
	}
	runs, err := fac.ListWeldRuns(ctx, opTok, factory.WeldRunQuery{})
	if err != nil || len(runs) == 0 {
		t.Fatalf("op runs %d %v", len(runs), err)
	}
	p := &captureWAN{}
	fac.SetWeldWANPoster(p)
	if _, err := fac.SeedWeldDemo(ctx, saTok); err != nil {
		t.Fatal(err)
	}
	if len(p.rows) == 0 {
		t.Fatal("wan summaries empty")
	}
	raw, err := json.Marshal(p.rows)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"loginName", "displayName", "personId", "orgPath", "orgUnit", "creatorId"} {
		if strings.Contains(s, bad) {
			t.Fatalf("leaked %s: %s", bad, s)
		}
	}
}

type captureWAN struct {
	rows []factory.WeldWANSummary
}

func (c *captureWAN) PostWeldSummaries(_ context.Context, _ uuid.UUID, rows []factory.WeldWANSummary) error {
	c.rows = append([]factory.WeldWANSummary(nil), rows...)
	return nil
}
