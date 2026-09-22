// 工艺工程只读编号：本厂发号、副本沿用原号、另存/升档换新号。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestFactoryAssetCodes(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	peA := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}

	p1, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊1", []byte(`{"name":"p","current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	if p1.Code != "GY-F01-000001" || !assetcode.Valid(p1.Code) {
		t.Fatalf("factory process %s", p1.Code)
	}
	j1, err := facA.CreateFactoryProject(ctx, peA.tok, direct, "工程1", []byte(`[{"name":"焊道"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if j1.Code != "GC-F01-000001" {
		t.Fatalf("factory project %s", j1.Code)
	}
	p2, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊2", []byte(`{"name":"p2","current":181}`))
	if err != nil {
		t.Fatal(err)
	}
	if p2.Code != "GY-F01-000002" {
		t.Fatalf("second process %s", p2.Code)
	}

	renamed, err := facA.RenameAsset(ctx, peA.tok, p1.ID, p1.Revision, "改名")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Code != p1.Code || renamed.Revision != p1.Revision+1 {
		t.Fatalf("rename %+v", renamed)
	}

	copied, err := facA.CopyProcess(ctx, peA.tok, p1.ID, "另存")
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == p1.ID || copied.Code == p1.Code || !strings.HasPrefix(copied.Code, "GY-F01-") {
		t.Fatalf("copy %+v", copied)
	}
	src, err := facA.GetAsset(ctx, peA.tok, p1.ID)
	if err != nil || src.Code != p1.Code {
		t.Fatalf("src after copy %+v %v", src, err)
	}

	pers, err := facA.CreatePersonalProcess(ctx, peA.tok, direct, "个人", []byte(`{"name":"mine","current":170}`))
	if err != nil {
		t.Fatal(err)
	}
	pers, err = facA.PublishAsset(ctx, peA.tok, pers.ID, pers.Revision)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := facA.PromoteToFactory(ctx, peA.tok, pers.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ID == pers.ID || promoted.Code == pers.Code || promoted.Level != factory.AssetLevelFactory {
		t.Fatalf("promote %+v from %s", promoted, pers.Code)
	}
	again, err := facA.PromoteToFactory(ctx, peA.tok, pers.ID)
	if err != nil || again.ID != promoted.ID || again.Code != promoted.Code {
		t.Fatalf("re-promote %+v %v", again, err)
	}
	stillPers, err := facA.GetAsset(ctx, peA.tok, pers.ID)
	if err != nil || stillPers.Code != pers.Code {
		t.Fatalf("personal after promote %+v %v", stillPers, err)
	}

	ingested, err := facA.Store().InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelPersonal, Name: "现场汇聚",
		Status: store.AssetDraft, Copyable: true, Content: []byte("c-body"), Digest: digest.Sum([]byte("c-body")),
		CreatorID: seedA.SuperAdminID, Code: "GY-C0008-000012",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ingested.Code != "GY-C0008-000012" || ingested.ID == p1.ID {
		t.Fatalf("ingest %+v", ingested)
	}
	p3, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊3", []byte(`{"name":"p3","current":182}`))
	if err != nil {
		t.Fatal(err)
	}
	if p3.Code != "GY-F01-000006" {
		t.Fatalf("seq after ingest %s", p3.Code)
	}

	platBody := []byte("plat-body")
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台焊",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	fid := seedA.ID
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	gotRep, err := facA.GetAsset(ctx, peA.tok, m.ID)
	if err != nil || gotRep.Code != "GY-W-000001" {
		t.Fatalf("replica %+v %v", gotRep, err)
	}
	if gotRep.Code == p1.Code {
		t.Fatal("replica collided with local")
	}

	dup := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "撞号",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, dup, nil, &fid, nil)); !errors.Is(err, domain.ErrAssetCodeConflict) {
		t.Fatalf("same code other id: %v", err)
	}
	mismatch := m
	mismatch.Code = "GY-W-000002"
	mismatch.Revision = 2
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, mismatch, nil, &fid, nil)); !errors.Is(err, domain.ErrAssetCodeConflict) {
		t.Fatalf("same id other code: %v", err)
	}
	keep, err := facA.GetAsset(ctx, peA.tok, m.ID)
	if err != nil || keep.Code != "GY-W-000001" {
		t.Fatalf("original after conflict %+v %v", keep, err)
	}

	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sb-pass"); err != nil {
		t.Fatal(err)
	}
	saB := mustLogin(t, ctx, facB, "sa-b", "sb-pass")
	pb, err := facB.CreateFactoryProcess(ctx, saB, direct, "乙厂焊", []byte(`{"name":"b","current":160}`))
	if err != nil {
		t.Fatal(err)
	}
	if pb.Code != "GY-F02-000001" || pb.Code == p1.Code {
		t.Fatalf("other factory %s vs %s", pb.Code, p1.Code)
	}
	if strings.Contains(p1.Code, "-W-") || !strings.Contains(p1.Code, "-F01-") {
		t.Fatalf("want factory origin %s", p1.Code)
	}

	pub, err := facA.PublishAsset(ctx, peA.tok, p1.ID, renamed.Revision)
	if err != nil {
		t.Fatal(err)
	}
	dep := factory.AssetDep{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}
	if _, err := facA.CreateFactoryProject(ctx, peA.tok, direct, "编号当引用", []byte(`[{"processId":"`+p1.Code+`"}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("code as processId: %v", err)
	}

	rows, err := facA.ListAssets(ctx, peA.tok, "process")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range rows {
		if row.Code == p1.Code {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("list by code %d", n)
	}
	byCode, err := facA.Store().GovernedAssetByCode(ctx, p1.Code)
	if err != nil || byCode.ID != p1.ID {
		t.Fatalf("by code %+v %v", byCode, err)
	}
	if _, err := facA.Store().GovernedAssetByCode(ctx, "GY-F01-999999"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestFactoryAssetCodeMissing(t *testing.T) {
	ctx := context.Background()
	admin := testpg.Open(t)
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	facID := id.New()
	svc := factory.NewService(store.Open(testpg.OpenMigrated(t, dsn), facID))
	if err := svc.Store().GrantLocalLease(ctx); err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.BootstrapInitial(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Activate(ctx, "sa", token, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, svc, "sa", "sa-pass")
	if _, err := svc.CreateFactoryProcess(ctx, tok, factory.WorkContext{Direct: true}, "无短码", []byte(`{"name":"x","current":1}`)); !errors.Is(err, domain.ErrAssetCodeMissing) {
		t.Fatalf("missing origin: %v", err)
	}
}
