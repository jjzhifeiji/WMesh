// C3：按 cache_scope 与上限激活恰好一份；processId 解工艺；缺成员/串版/只拷/超上限/焊接中拒绝。
package service_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestPouchActivateAndOpenProcess(t *testing.T) {
	p, client, _, _ := loggedPouch(t)
	procBody := []byte(`{"current":180}`)
	proc := processMember("工艺", procBody)
	snap := projectSnap(client, "工程", proc)
	if err := p.CacheClosure(snap); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate(snap.AssetID); err != nil {
		t.Fatal(err)
	}
	got, err := p.OpenProcess(proc.ID)
	if err != nil || !bytes.Equal(got, procBody) {
		t.Fatalf("process %s %v", got, err)
	}
	if _, err := p.OpenProcess(snap.AssetID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("project as process: %v", err)
	}
}

func TestPouchActivateRejectsIncompleteMismatchCopyFullWeldOffline(t *testing.T) {
	p, client, _, _ := loggedPouch(t)
	proc := processMember("工艺", []byte(`{"a":1}`))
	good := projectSnap(client, "工程", proc)
	if err := p.CacheClosure(good); err != nil {
		t.Fatal(err)
	}

	missing := good
	root := missing.Members[0]
	missing.Members = []factory.ClosureMember{root}
	missing.Digest = digest.ClosureSum([]digest.Member{{
		ID: root.ID, Revision: root.Revision, Digest: root.Digest, Content: root.Content,
	}})
	if err := p.CacheClosure(missing); !errors.Is(err, domain.ErrClosureIncomplete) {
		t.Fatalf("missing: %v", err)
	}

	mismatch := projectSnap(client, "串版", processMember("工艺", []byte(`{"a":1}`)))
	mismatch.Members[0].Deps[0].Revision = 99
	if err := p.CacheClosure(mismatch); !errors.Is(err, domain.ErrClosureMismatch) {
		t.Fatalf("mismatch: %v", err)
	}

	tampered := projectSnap(client, "摘要", processMember("工艺", []byte(`{"a":1}`)))
	tampered.Members[0].Content = []byte("tampered")
	if err := p.CacheClosure(tampered); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("integrity: %v", err)
	}

	other := id.New()
	copied := projectSnap(other, "只拷", processMember("他机", []byte(`{"b":1}`)))
	if err := p.CacheClosure(copied); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("copy: %v", err)
	}

	second := projectSnap(client, "第二份", processMember("工艺2", []byte(`{"c":1}`)))
	if err := p.CacheClosure(second); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate(good.AssetID); err != nil {
		t.Fatal(err)
	}
	p.SetWelding(true)
	if err := p.Activate(second.AssetID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("weld switch: %v", err)
	}
	if p.ActiveProject() == nil || *p.ActiveProject() != good.AssetID {
		t.Fatal("active moved while welding")
	}
	p.SetWelding(false)

	p.SetPolicy(2, store.CacheScopeCurrent)
	p.SetOnline(false)
	if err := p.Activate(id.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("offline current: %v", err)
	}
}

func TestPouchCurrentScopeDropsOtherProjects(t *testing.T) {
	p, client, _, _ := loggedPouch(t)
	p.SetPolicy(2, store.CacheScopeCurrent)
	aProc := processMember("A工艺", []byte(`{"a":1}`))
	bProc := processMember("B工艺", []byte(`{"b":1}`))
	a := projectSnap(client, "A", aProc)
	b := projectSnap(client, "B", bProc)
	if err := p.CacheClosure(a); err != nil {
		t.Fatal(err)
	}
	if err := p.CacheClosure(b); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate(b.AssetID); err != nil {
		t.Fatal(err)
	}
	if p.HasClosure(a.AssetID) {
		t.Fatal("current kept other project")
	}
	if _, err := p.Open(aProc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("dropped member: %v", err)
	}
	got, err := p.OpenProcess(bProc.ID)
	if err != nil || !bytes.Equal(got, []byte(`{"b":1}`)) {
		t.Fatalf("kept %s %v", got, err)
	}
}

func TestPouchActivatePersonalFollowsOwner(t *testing.T) {
	p, client, who, key := loggedPouch(t)
	proc := processMember("工艺", []byte(`{"p":1}`))
	snap := projectSnap(client, "我的工程", proc)
	snap.Level = store.AssetLevelPersonal
	snap.Members[0].Level = store.AssetLevelPersonal
	snap.Members[0].CreatorID = &who
	if err := p.CacheClosure(snap); err != nil {
		t.Fatal(err)
	}
	p.Logout()
	other := id.New()
	if err := p.Login(key, other, false); err != nil {
		t.Fatal(err)
	}
	p.BindClient(client)
	if err := p.Activate(snap.AssetID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("personal: %v", err)
	}
}

func loggedPouch(t *testing.T) (*factory.Pouch, uuid.UUID, uuid.UUID, []byte) {
	t.Helper()
	p := factory.NewPouch()
	key, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	who := id.New()
	if err := p.Login(key, who, false); err != nil {
		t.Fatal(err)
	}
	client := id.New()
	p.BindClient(client)
	return p, client, who, key
}

func processMember(name string, body []byte) factory.ClosureMember {
	return factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelFactory, Name: name,
		Status: factory.AssetAvailable, Revision: 1, Content: body, Digest: digest.Sum(body),
	}
}

func projectSnap(client uuid.UUID, name string, procs ...factory.ClosureMember) factory.ClosureSnapshot {
	deps := make([]factory.AssetDep, len(procs))
	for i, p := range procs {
		deps[i] = factory.AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}
	}
	body := []byte(`{"items":[]}`)
	root := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelFactory, Name: name,
		Status: factory.AssetAvailable, Revision: 1, Content: body, Digest: digest.Sum(body), Deps: deps,
	}
	cid := client
	return sealSnap(factory.KindProject, root, procs, nil, &cid)
}
