// 厂库正文必须是 WM2；租约到期后读拒绝且密文仍在；他厂信封解不开。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestContentLeaseAndEnvelope(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fac.Store().RawGovernedContent(ctx, row.ID)
	if err != nil || !contentcrypt.IsEnvelope(raw) {
		t.Fatalf("want WM2 got %q %v", raw, err)
	}
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		t.Fatalf("read %q %v", body, err)
	}

	fac.Store().ClearContentLease()
	if _, err := fac.ReadAssetContent(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("expired read %v", err)
	}
	raw2, err := fac.Store().RawGovernedContent(ctx, row.ID)
	if err != nil || !bytes.Equal(raw, raw2) {
		t.Fatalf("ciphertext changed after expire")
	}

	other, facB, err := h.Provision(ctx, "sb", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sb", other.ActivationToken, "sb-pass"); err != nil {
		t.Fatal(err)
	}
	tokB := mustLogin(t, ctx, facB, "sb", "sb-pass")
	rowB, err := facB.CreateFactoryProcess(ctx, tokB, store.WorkContext{Direct: true}, "焊B", []byte(`{"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Store().TamperAssetContent(ctx, rowB.ID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := facB.ReadAssetContent(ctx, tokB, rowB.ID); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("cross factory %v", err)
	}

	l, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(-time.Minute)); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("past lease %v", err)
	}
}

func TestContentLeaseMetaAndRestore(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	l, err := fac.Store().TransitKey()
	if err != nil {
		t.Fatal(err)
	}
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	fac.Store().ClearContentLease()
	if _, err := fac.GetAsset(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("get after expire %v", err)
	}
	if _, err := fac.ReadAssetContent(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("read %v", err)
	}
	if _, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊2", []byte(`{"b":1}`)); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("create without lease %v", err)
	}
	if _, err := fac.RenameAsset(ctx, tok, row.ID, row.Revision, "焊改"); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("rename without lease %v", err)
	}
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		t.Fatalf("restore %q %v", body, err)
	}
	wrong, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Store().ApplyContentLease(ctx, wrong, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("wrong L must rewrap live mk: %v", err)
	}
	gotL, err := fac.Store().TransitKey()
	if err != nil || !bytes.Equal(gotL, wrong) {
		t.Fatalf("rotated L %v", err)
	}
	body, err = fac.ReadAssetContent(ctx, tok, row.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		t.Fatalf("kept mk %q %v", body, err)
	}
	fac.Store().ClearContentLease()
	if err := fac.Store().ApplyContentLease(ctx, wrong, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("restore rotated L %v", err)
	}
	body, err = fac.ReadAssetContent(ctx, tok, row.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		t.Fatalf("after rotate restore %q %v", body, err)
	}
}

func TestLegacyPlainSealedOnLease(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	plain := []byte(`{"legacy":1}`)
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "旧明文", plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Store().TamperAssetContent(ctx, row.ID, plain); err != nil {
		t.Fatal(err)
	}
	l, err := fac.Store().TransitKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	raw, err := fac.Store().RawGovernedContent(ctx, row.ID)
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		t.Fatalf("want sealed got %q %v", raw, err)
	}
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	if err != nil || !bytes.Equal(body, plain) {
		t.Fatalf("read after seal %q %v", body, err)
	}
}

func TestAcceptLegacyPlainClosure(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	fid := seed.ID
	plain := []byte(`{"plat":1}`)
	m := factory.ClosureMember{
		ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "旧闭包", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: plain, Digest: digest.Sum(plain),
	}
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	raw, err := fac.Store().RawReplicaContent(ctx, m.ID, 1)
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		t.Fatalf("legacy closure disk %q %v", raw, err)
	}
	got, err := fac.GetAsset(ctx, tok, m.ID)
	if err != nil || got.Name != "旧闭包" {
		t.Fatalf("get replica %v %+v", err, got)
	}
	body, err := fac.ReadAssetContent(ctx, tok, m.ID)
	if err != nil || !bytes.Equal(body, plain) {
		t.Fatalf("read replica %q %v", body, err)
	}
}

func TestAcceptMixedTransitClosure(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	fid := seed.ID
	plain := []byte(`{"plain":1}`)
	envBody := []byte(`{"env":1}`)
	projBody := []byte(`{"proj":1}`)
	l, err := fac.Store().TransitKey()
	if err != nil {
		t.Fatal(err)
	}
	plainM := factory.ClosureMember{
		ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "明文成员", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: plain, Digest: digest.Sum(plain),
	}
	envM := factory.ClosureMember{
		ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "密文成员", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: envBody, Digest: digest.Sum(envBody),
	}
	proj := factory.ClosureMember{
		ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), Kind: factory.KindProject, Level: factory.AssetLevelPlatform,
		Name: "混合闭包", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: projBody, Digest: digest.Sum(projBody),
		Deps: []factory.AssetDep{
			{ID: plainM.ID, Revision: 1, Digest: plainM.Digest},
			{ID: envM.ID, Revision: 1, Digest: envM.Digest},
		},
	}
	snap := sealSnap(factory.KindProject, proj, []factory.ClosureMember{plainM, envM}, &fid, nil)
	env, err := contentcrypt.Seal(l, envBody, contentcrypt.TransitAAD(fid, envM.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	snap.Members[2].Content = env
	if err := fac.AcceptPlatformDelivery(ctx, snap); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{plainM.ID, envM.ID, proj.ID} {
		raw, err := fac.Store().RawReplicaContent(ctx, id, 1)
		if err != nil || !contentcrypt.IsEnvelope(raw) {
			t.Fatalf("disk %s %q %v", id, raw, err)
		}
	}
	gotPlain, err := fac.ReadAssetContent(ctx, tok, plainM.ID)
	if err != nil || !bytes.Equal(gotPlain, plain) {
		t.Fatalf("plain member %q %v", gotPlain, err)
	}
	gotEnv, err := fac.ReadAssetContent(ctx, tok, envM.ID)
	if err != nil || !bytes.Equal(gotEnv, envBody) {
		t.Fatalf("env member %q %v", gotEnv, err)
	}
}

func TestInsertReplicaSealsAndNeedsLease(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	plain := []byte(`{"current":1}`)
	sum := digest.Sum(plain)
	in := store.AssetReplica{
		ID: id, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: true, Content: plain, Digest: sum,
	}
	if _, err := fac.Store().InsertReplica(ctx, in); err != nil {
		t.Fatal(err)
	}
	raw, err := fac.Store().RawReplicaContent(ctx, id, 1)
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		t.Fatalf("replica disk %q %v", raw, err)
	}
	if _, err := fac.Store().LatestReplicaMeta(ctx, id); err != nil {
		t.Fatalf("replica meta %v", err)
	}
	fac.Store().ClearContentLease()
	if _, err := fac.Store().LatestReplica(ctx, id); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("sealed replica readable without lease: %v", err)
	}
	if _, err := fac.Store().InsertReplica(ctx, in); err != nil {
		t.Fatalf("same revision replay without lease %v", err)
	}
	if _, err := fac.Store().InsertReplica(ctx, store.AssetReplica{
		ID: id, Revision: 2, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: true, Content: plain, Digest: sum,
	}); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("insert without lease %v", err)
	}
}
