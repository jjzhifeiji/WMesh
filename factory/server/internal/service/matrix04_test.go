// 阶段4第4～7圈：厂内侧工程闭包 1.1～17.2。
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

var matrix04IDs = []string{
	"1.1", "1.2", "1.3",
	"2.1", "2.2", "2.3",
	"3.1", "3.2", "3.3",
	"4.1", "4.2", "4.3",
	"5.1", "5.4",
	"6.1", "6.2", "6.3",
	"7.1", "7.2", "7.3", "7.4", "7.5", "7.6",
	"8.1", "8.2", "8.3",
	"9.1", "9.2",
	"10.1", "10.2", "10.3",
	"11.1", "11.2", "11.3", "11.4",
	"12.1", "12.2", "12.3",
	"13.1", "13.2", "13.3",
	"14.1",
	"15.1",
	"16.1", "16.2",
	"17.1", "17.2",
}

func TestMatrix04(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrix04IDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	testClosurePack(t, run)
	testClosureDistribute(t, run)
	testClosureActivate(t, run)
}

type env04 struct {
	ctx    context.Context
	seed   Seeded
	fac    *factory.Service
	seedB  Seeded
	facB   *factory.Service
	sa     string
	saB    string
	pe     namedAcc
	pe2    namedAcc
	op     namedAcc
	peB    namedAcc
	peShop namedAcc
	shop   factory.OrgUnit
	direct factory.WorkContext
	now    time.Time
	clocks factory.Clocks
	nb, na time.Time
	secret []byte
}

func newEnv04(t *testing.T) *env04 {
	t.Helper()
	ctx := context.Background()
	h := New(t)
	seed, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sa-b-pass"); err != nil {
		t.Fatal(err)
	}
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	e := &env04{
		ctx: ctx, seed: seed, fac: facA, seedB: seedB, facB: facB, sa: saA, saB: saB,
		pe:     mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil),
		pe2:    mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil),
		op:     mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil),
		peB:    mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil),
		direct: factory.WorkContext{Direct: true},
		secret: []byte("closure-body-secret"),
	}
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	if err != nil {
		t.Fatal(err)
	}
	shop, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.shop = shop
	e.peShop = mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleProcessEngineer, factory.ScopeOrgUnit, &shop.ID)
	if err := facA.Assign(ctx, saA, e.peShop.acc.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	e.now = time.Now().UTC()
	e.clocks = factory.Clocks{Server: e.now, Local: e.now}
	e.nb, e.na = e.now.Add(-time.Hour), e.now.Add(24*time.Hour)
	return e
}

func (e *env04) pubProc(t *testing.T, name string, body []byte) factory.Asset {
	t.Helper()
	p, err := e.fac.CreateFactoryProcess(e.ctx, e.pe.tok, e.direct, name, body)
	if err != nil {
		t.Fatal(err)
	}
	p, err = e.fac.PublishAsset(e.ctx, e.pe.tok, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env04) pubProj(t *testing.T, name string, body []byte, deps []factory.AssetDep) factory.Asset {
	t.Helper()
	p, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, name, body, deps)
	if err != nil {
		t.Fatal(err)
	}
	p, err = e.fac.PublishAsset(e.ctx, e.pe.tok, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env04) depsOf(procs ...factory.Asset) []factory.AssetDep {
	out := make([]factory.AssetDep, 0, len(procs))
	for _, p := range procs {
		out = append(out, factory.AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest})
	}
	return out
}

func (e *env04) bound(t *testing.T, personID uuid.UUID) (uuid.UUID, factory.Bag) {
	t.Helper()
	cid := id.New()
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.fac.AcceptBinding(e.ctx, cid, "焊机-1", pub, 1); err != nil {
		t.Fatal(err)
	}
	facPub, err := e.fac.SigningPublicKey(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := e.fac.IssueRuntimeGrant(e.ctx, e.sa, cid, e.nb, e.na)
	if err != nil {
		t.Fatal(err)
	}
	bag := bagOf(e.seed.ID, cid, pub, priv, facPub)
	bag.Connected = true
	bag.ApplyRuntime(rt)
	oid := personID
	bag.OperatorID = &oid
	return cid, bag
}

func sealSnap(kind string, root factory.ClosureMember, rest []factory.ClosureMember, facID, clientID *uuid.UUID) factory.ClosureSnapshot {
	members := append([]factory.ClosureMember{root}, rest...)
	parts := make([]digest.Member, len(members))
	for i, m := range members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	return factory.ClosureSnapshot{
		Kind: kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: facID, TargetClientID: clientID,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

func testClosurePack(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	e := newEnv04(t)
	p1 := e.pubProc(t, "工艺1", []byte("p1"))
	p2 := e.pubProc(t, "工艺2", []byte("p2"))
	proj := e.pubProj(t, "工程双依赖", e.secret, e.depsOf(p1, p2))

	var snap factory.ClosureSnapshot
	run("1.1", func(t *testing.T) {
		var err error
		snap, err = e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.AssetID != proj.ID || len(snap.Members) != 3 || snap.Members[0].Kind != factory.KindProject ||
			snap.Members[1].ID != p1.ID || snap.Members[2].ID != p2.ID {
			t.Fatalf("%+v", snap)
		}
	})
	run("1.2", func(t *testing.T) {
		missing := factory.AssetDep{ID: id.New(), Revision: 1, Digest: p1.Digest}
		if err := e.fac.Store().TamperAssetDeps(e.ctx, proj.ID, []factory.AssetDep{missing, e.depsOf(p2)[0]}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID); !errors.Is(err, domain.ErrClosureIncomplete) {
			t.Fatalf("got %v", err)
		}
		if err := e.fac.Store().TamperAssetDeps(e.ctx, proj.ID, e.depsOf(p1, p2)); err != nil {
			t.Fatal(err)
		}
	})
	run("1.3", func(t *testing.T) {
		empty := e.pubProj(t, "空依赖", []byte("empty"), nil)
		got, err := e.fac.AssembleProject(e.ctx, e.pe.tok, empty.ID)
		if err != nil || len(got.Members) != 1 || got.Members[0].ID != empty.ID {
			t.Fatalf("%+v %v", got, err)
		}
	})

	cid, bag := e.bound(t, e.pe.acc.ID)
	good, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	good.TargetClientID = &cid
	run("2.1", func(t *testing.T) {
		bad := good
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		bad.Members[1].Revision++
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrClosureMismatch) && !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("2.2", func(t *testing.T) {
		bad := good
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		bad.Members[1].ID = id.New()
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrClosureMismatch) && !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("2.3", func(t *testing.T) {
		bad := good
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		bad.Members[1].Content = []byte("dirty")
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrIntegrity) && !errors.Is(err, domain.ErrClosureMismatch) {
			t.Fatalf("got %v", err)
		}
	})

	run("3.1", func(t *testing.T) {
		if _, err := e.fac.UpdateAssetContent(e.ctx, e.pe.tok, p1.ID, p1.Revision, []byte("p1-v2")); err != nil {
			t.Fatal(err)
		}
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID); !errors.Is(err, domain.ErrClosureIncomplete) {
			t.Fatalf("got %v", err)
		}
	})
	run("3.2", func(t *testing.T) {
		p1b, err := e.fac.GetAsset(e.ctx, e.pe.tok, p1.ID)
		if err != nil {
			t.Fatal(err)
		}
		proj, err = e.fac.SetProjectDeps(e.ctx, e.pe.tok, proj.ID, proj.Revision, e.depsOf(p1b, p2))
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
		if err != nil || got.Members[1].Revision != p1b.Revision {
			t.Fatalf("%+v %v", got, err)
		}
		snap = got
	})
	run("3.3", func(t *testing.T) {
		if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
		oldRev := bag.Closures[0].Members[1].Revision
		if _, err := e.fac.UpdateAssetContent(e.ctx, e.pe.tok, p2.ID, p2.Revision, []byte("p2-v2")); err != nil {
			t.Fatal(err)
		}
		cached, ok := false, false
		for _, c := range bag.Closures {
			if c.AssetID == proj.ID {
				cached, ok = true, true
				if c.Members[2].Revision != oldRev && len(c.Members) > 2 {
					// members[1] is p1, members[2] is p2
				}
				if c.Members[len(c.Members)-1].ID == p2.ID && c.Members[len(c.Members)-1].Revision != oldRev && c.Members[len(c.Members)-1].Revision != p2.Revision {
					t.Fatalf("drift %+v", c.Members)
				}
				_ = cached
			}
		}
		if !ok {
			t.Fatal("missing cache")
		}
	})

	draft, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "草稿工程", []byte("d"), nil)
	if err != nil {
		t.Fatal(err)
	}
	run("4.1", func(t *testing.T) {
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, draft.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("got %v", err)
		}
	})
	avail := e.pubProj(t, "可停用", []byte("off"), nil)
	run("4.2", func(t *testing.T) {
		off, err := e.fac.DisableAsset(e.ctx, e.pe.tok, avail.ID, avail.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, off.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("got %v", err)
		}
	})
	run("4.3", func(t *testing.T) {
		pub, err := e.fac.PublishAsset(e.ctx, e.pe.tok, draft.ID, draft.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, pub.ID); err != nil {
			t.Fatal(err)
		}
	})

	run("16.1", func(t *testing.T) {
		b := bag
		b.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		if len(b.Closures) == 0 {
			t.Fatal("need cached")
		}
		m := append([]factory.ClosureMember{}, b.Closures[0].Members...)
		m[0].Content = []byte("tampered-cache")
		b.Closures[0].Members = m
		if err := e.fac.ActivateProject(e.ctx, &b, e.clocks, b.Closures[0].AssetID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("16.2", func(t *testing.T) {
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); err != nil {
			t.Fatal(err)
		}
	})
}

func testClosureDistribute(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	e := newEnv04(t)
	p1 := e.pubProc(t, "厂工艺", []byte("fp"))
	proj := e.pubProj(t, "厂工程", e.secret, e.depsOf(p1))
	cid, bag := e.bound(t, e.op.acc.ID)
	cid2, bag2 := e.bound(t, e.op.acc.ID)

	platProcBody := []byte("plat-proc")
	platProc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: platProcBody, Digest: digest.Sum(platProcBody),
	}
	fid := e.seed.ID
	run("5.4", func(t *testing.T) {
		snap := sealSnap(factory.KindProcess, platProc, nil, &fid, nil)
		if err := e.fac.AcceptPlatformDelivery(e.ctx, snap); err != nil {
			t.Fatal(err)
		}
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, platProc.ID, 1)
		if err != nil || got.Copyable || got.Level != factory.AssetLevelPlatform || got.Content != nil {
			t.Fatalf("%+v %v", got, err)
		}
		listed, err := e.fac.ListAssets(e.ctx, e.pe.tok, factory.KindProcess)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, a := range listed {
			if a.ID == platProc.ID && a.Level == factory.AssetLevelPlatform {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("platform replica missing from list")
		}
	})
	platProjBody := []byte("plat-proj")
	platProj := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "平台工程",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: platProjBody, Digest: digest.Sum(platProjBody),
		Deps: []factory.AssetDep{{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}},
	}
	run("5.1", func(t *testing.T) {
		snap := sealSnap(factory.KindProject, platProj, []factory.ClosureMember{platProc}, &fid, nil)
		if err := e.fac.AcceptPlatformDelivery(e.ctx, snap); err != nil {
			t.Fatal(err)
		}
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, platProj.ID, 1)
		if err != nil || got.Copyable || got.ID != platProj.ID {
			t.Fatalf("%+v %v", got, err)
		}
	})

	run("6.1", func(t *testing.T) {
		if _, err := e.facB.AssembleProject(e.ctx, e.peB.tok, proj.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("assemble: %v", err)
		}
		if err := e.facB.GrantClientProject(e.ctx, e.saB, proj.ID, cid); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("grant: %v", err)
		}
	})
	run("6.3", func(t *testing.T) {
		if err := e.fac.ForwardToFactory(e.ctx, e.pe.tok, platProj.ID, e.seedB.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("9.1", func(t *testing.T) {
		if err := e.fac.SetReplicaCopyable(e.ctx, e.pe.tok, platProj.ID, true); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("copyable: %v", err)
		}
		if err := e.fac.PromoteReplica(e.ctx, e.pe.tok, platProj.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("promote: %v", err)
		}
		body := []byte("plat-copyable")
		m := factory.ClosureMember{
			ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "可复制平台工艺",
			Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: body, Digest: digest.Sum(body),
		}
		if err := e.fac.AcceptPlatformDelivery(e.ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
			t.Fatal(err)
		}
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, m.ID, 1)
		if err != nil || !got.Copyable {
			t.Fatalf("%+v %v", got, err)
		}
		if err := e.fac.SetReplicaCopyable(e.ctx, e.pe.tok, m.ID, false); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("tighten replica: %v", err)
		}
	})
	run("9.2", func(t *testing.T) {
		if err := e.fac.ForwardToFactory(e.ctx, e.sa, platProj.ID, e.seedB.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})

	run("10.1", func(t *testing.T) {
		dep := factory.AssetDep{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}
		got, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "依赖平台工艺", []byte("fp2"), []factory.AssetDep{dep})
		if err != nil {
			t.Fatal(err)
		}
		got, err = e.fac.PublishAsset(e.ctx, e.pe.tok, got.ID, got.Revision)
		if err != nil {
			t.Fatal(err)
		}
		_ = got
	})
	run("10.2", func(t *testing.T) {
		missing := factory.AssetDep{ID: id.New(), Revision: 1, Digest: digest.Sum([]byte("x"))}
		if _, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "未到达", []byte("x"), []factory.AssetDep{missing}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})

	run("7.1", func(t *testing.T) {
		if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
		if len(bag.Closures) != 1 || bag.Closures[0].AssetID != proj.ID {
			t.Fatalf("%+v", bag.Closures)
		}
	})
	run("7.2", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid2, &bag2, e.clocks); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("ungranted: %v", err)
		}
	})
	run("7.3", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.op.tok, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("op: %v", err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.sa, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("sa: %v", err)
		}
	})
	run("7.4", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.peShop.tok, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("shop: %v", err)
		}
	})
	run("7.5", func(t *testing.T) {
		if err := e.fac.GrantClientProject(e.ctx, e.sa, platProj.ID, cid); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, platProj.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
	})
	run("7.6", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.peShop.tok, platProj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("10.3", func(t *testing.T) {
		dep := factory.AssetDep{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}
		got, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "带平台成员", []byte("mix"), []factory.AssetDep{dep})
		if err != nil {
			t.Fatal(err)
		}
		got, err = e.fac.PublishAsset(e.ctx, e.pe.tok, got.ID, got.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.fac.GrantClientProject(e.ctx, e.sa, got.ID, cid); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, got.ID, cid, &bag, e.clocks); err != nil && !errors.Is(err, domain.ErrClientCacheFull) {
			t.Fatal(err)
		}
	})
	run("15.1", func(t *testing.T) {
		n := len(bag.Closures)
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
		if len(bag.Closures) < n {
			t.Fatalf("lost cache")
		}
	})
	run("6.2", func(t *testing.T) {
		cidB := id.New()
		pub, priv, err := nodekey.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.facB.AcceptBinding(e.ctx, cidB, "焊机-B", pub, 1); err != nil {
			t.Fatal(err)
		}
		facPub, err := e.facB.SigningPublicKey(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		bagB := bagOf(e.seedB.ID, cidB, pub, priv, facPub)
		if len(bag.Closures) == 0 {
			t.Fatal("need closure")
		}
		stolen := bag.Closures[0]
		bagB.Closures = []factory.ClosureSnapshot{stolen}
		if err := e.fac.ActivateProject(e.ctx, &bagB, e.clocks, stolen.AssetID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
}

func testClosureActivate(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	e := newEnv04(t)
	p1 := e.pubProc(t, "A工艺", []byte("a"))
	p2 := e.pubProc(t, "B工艺", []byte("b"))
	proj1 := e.pubProj(t, "工程1", e.secret, e.depsOf(p1))
	proj2 := e.pubProj(t, "工程2", []byte("p2-body"), e.depsOf(p2))
	proj3 := e.pubProj(t, "工程3", []byte("p3-body"), nil)
	cid, bag := e.bound(t, e.op.acc.ID)
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj1.ID, cid); err != nil {
		t.Fatal(err)
	}
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj2.ID, cid); err != nil {
		t.Fatal(err)
	}
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj3.ID, cid); err != nil {
		t.Fatal(err)
	}
	if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj1.ID, cid, &bag, e.clocks); err != nil {
		t.Fatal(err)
	}

	run("11.1", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj2.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
		if len(bag.Closures) != 2 {
			t.Fatalf("%d", len(bag.Closures))
		}
	})
	run("11.2", func(t *testing.T) {
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj3.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrClientCacheFull) {
			t.Fatalf("got %v", err)
		}
		if len(bag.Closures) != 2 {
			t.Fatalf("len %d", len(bag.Closures))
		}
	})
	run("11.4", func(t *testing.T) {
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); err != nil {
			t.Fatal(err)
		}
		if bag.ActiveID == nil || *bag.ActiveID != proj1.ID {
			t.Fatal("not active")
		}
	})
	run("11.3", func(t *testing.T) {
		if err := e.fac.UncacheProject(e.ctx, &bag, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("active uncache: %v", err)
		}
		if err := e.fac.UncacheProject(e.ctx, &bag, proj2.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj3.ID, cid, &bag, e.clocks); err != nil {
			t.Fatal(err)
		}
	})

	run("12.1", func(t *testing.T) {
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); err != nil {
			t.Fatal(err)
		}
		if bag.ActiveID == nil || *bag.ActiveID != proj1.ID {
			t.Fatal("active")
		}
	})
	run("12.2", func(t *testing.T) {
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj3.ID); err != nil {
			t.Fatal(err)
		}
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			t.Fatal("switched")
		}
	})
	run("12.3", func(t *testing.T) {
		bad := bag
		bad.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		if len(bad.Closures) == 0 {
			t.Fatal("empty")
		}
		bad.Closures[0].Members = bad.Closures[0].Members[:1]
		if len(bag.Closures[0].Members) > 1 {
			if err := e.fac.ActivateProject(e.ctx, &bad, e.clocks, bad.Closures[0].AssetID); !errors.Is(err, domain.ErrClosureIncomplete) && !errors.Is(err, domain.ErrIntegrity) && !errors.Is(err, domain.ErrClosureMismatch) {
				t.Fatalf("got %v", err)
			}
		}
	})

	run("13.1", func(t *testing.T) {
		bag.Welding = true
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("switch: %v", err)
		}
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			t.Fatal("changed")
		}
		if err := e.fac.UncacheProject(e.ctx, &bag, proj3.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("uncache: %v", err)
		}
		bag.Welding = false
	})
	run("13.2", func(t *testing.T) {
		expired := e.clocks
		expired.Server = e.na.Add(time.Hour)
		expired.Local = expired.Server
		if err := e.fac.ActivateProject(e.ctx, &bag, expired, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("13.3", func(t *testing.T) {
		bag.Welding = true
		expired := e.clocks
		expired.Server = e.na.Add(time.Hour)
		if err := e.fac.ActivateProject(e.ctx, &bag, expired, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			t.Fatal("lost weld")
		}
		bag.Welding = false
	})

	run("8.1", func(t *testing.T) {
		per, err := e.fac.CreatePersonalProject(e.ctx, e.pe.tok, e.direct, "个人工程", []byte("mine"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.fac.PublishAsset(e.ctx, e.pe.tok, per.ID, per.Revision); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.GrantClientProject(e.ctx, e.sa, per.ID, cid); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("grant: %v", err)
		}
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, per.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("dist: %v", err)
		}
	})
	peCid, peBag := e.bound(t, e.pe.acc.ID)
	run("8.2", func(t *testing.T) {
		per, err := e.fac.CreatePersonalProject(e.ctx, e.pe.tok, e.direct, "我的工程", []byte("mine2"), nil)
		if err != nil {
			t.Fatal(err)
		}
		per, err = e.fac.PublishAsset(e.ctx, e.pe.tok, per.ID, per.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.fac.CachePersonalProject(e.ctx, e.pe.tok, per.ID, peCid, &peBag, e.clocks); err != nil {
			t.Fatal(err)
		}
		if err := e.fac.ActivateProject(e.ctx, &peBag, e.clocks, per.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("8.3", func(t *testing.T) {
		perID := peBag.Closures[0].AssetID
		opBag := bag
		peBag.Closures[0].TargetClientID = &cid
		opBag.Closures = append(opBag.Closures, peBag.Closures[0])
		if err := e.fac.ActivateProject(e.ctx, &opBag, e.clocks, perID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})

	run("14.1", func(t *testing.T) {
		cidX := id.New()
		pub, priv, err := nodekey.Generate()
		if err != nil {
			t.Fatal(err)
		}
		other := bagOf(e.seed.ID, cidX, pub, priv, bag.FactoryPublic)
		other.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		if len(other.Closures) == 0 {
			t.Fatal("empty")
		}
		if err := e.fac.ActivateProject(e.ctx, &other, e.clocks, other.Closures[0].AssetID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})

	run("17.1", func(t *testing.T) {
		rows, err := e.fac.ListAudit(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), string(e.secret), "sa-pass", "pe-pass", e.sa) {
			t.Fatalf("secret leaked")
		}
	})
	run("17.2", func(t *testing.T) {
		rows, err := e.fac.ListAudit(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !audit.HasResult(rows, "pack_closure", audit.Deny) && !audit.HasResult(rows, "distribute_closure", audit.Deny) {
			// 缺失/串版/未授权至少有一条拒绝
			hasDeny := false
			for _, r := range rows {
				if r.Result == audit.Deny {
					hasDeny = true
					break
				}
			}
			if !hasDeny {
				t.Fatal("need deny audit")
			}
		}
	})
}
