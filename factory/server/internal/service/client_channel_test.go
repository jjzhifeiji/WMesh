// C2：HTTPS 拉过站密文；MQTT 小信封无正文；低修订不覆盖；他机拒绝。
package service_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

type recDown struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (r *recDown) PublishDown(factoryID, clientID uuid.UUID, payload []byte) {
	r.mu.Lock()
	r.msgs = append(r.msgs, append([]byte(nil), payload...))
	r.mu.Unlock()
}

func (r *recDown) last() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.msgs) == 0 {
		return nil
	}
	return r.msgs[len(r.msgs)-1]
}

func (r *recDown) latestFor(pub []byte, factoryID, clientID uuid.UUID) (clientmqtt.Intent, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.msgs) - 1; i >= 0; i-- {
		in, err := clientmqtt.Verify(pub, factoryID, clientID, r.msgs[i])
		if err == nil {
			return in, r.msgs[i], nil
		}
	}
	return clientmqtt.Intent{}, nil, errNoDown
}

var errNoDown = errDown("no matching down intent")

type errDown string

func (e errDown) Error() string { return string(e) }

func TestClientChannelPullAndPolicyFanout(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	down := &recDown{}
	fac.SetClientDown(down)

	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cid := id.New()
	cidB := id.New()
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		t.Fatal(err)
	}
	pubB, _, _ := nodekey.Generate()
	if _, err := fac.AcceptBinding(ctx, cidB, "焊机B", pubB, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cidB, "ARM-B"); err != nil {
		t.Fatal(err)
	}
	sess, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.SigningPublicKey) == 0 {
		t.Fatal("missing signing key")
	}

	direct := factory.WorkContext{Direct: true}
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.GrantClientProject(ctx, sa, proj.ID, cid); err != nil {
		t.Fatal(err)
	}

	if _, err := fac.PullClientClosure(ctx, sa, cid, proj.ID); err != domain.ErrForbidden {
		t.Fatalf("web sa pull: %v", err)
	}
	if _, err := fac.PullClientClosure(ctx, sess.Token, cidB, proj.ID); err != domain.ErrForbidden && err != domain.ErrNotFound {
		t.Fatalf("other client: %v", err)
	}

	got, err := fac.PullClientClosure(ctx, sess.Token, cid, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contentcrypt.IsEnvelope(got.Wrap) || len(got.Snapshot.Members) == 0 {
		t.Fatal("missing wrap")
	}
	plain := []byte(`{"current":180}`)
	for _, m := range got.Snapshot.Members {
		if !contentcrypt.IsEnvelope(m.Content) || bytes.Contains(m.Content, plain) || bytes.Contains(m.Content, []byte("current")) {
			t.Fatal("plaintext in transit member")
		}
	}

	opened, err := factory.OpenTransit(fac.Store().FactoryID(), cid, sess.UnwrapKey, got)
	if err != nil {
		t.Fatal(err)
	}
	pouch := factory.NewPouch()
	if err := pouch.Login(sess.UnwrapKey, sess.Account.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, m := range opened.Members {
		ok, err := pouch.PutPlainIfNewer(m.ID, m.Level, m.Name, m.Revision, m.CreatorID, m.Content)
		if err != nil || !ok {
			t.Fatalf("put %v %v", ok, err)
		}
	}
	root, err := pouch.Open(proj.ID)
	if err != nil || !bytes.Contains(root, []byte(proc.ID.String())) {
		t.Fatalf("pouch %s %v", root, err)
	}
	ok, err := pouch.PutPlainIfNewer(proj.ID, store.AssetLevelFactory, "工程", proj.Revision, nil, []byte("old"))
	if err != nil || ok {
		t.Fatalf("stale overwrite %v %v", ok, err)
	}

	box, err := fac.ClientInbox(ctx, sess.Token, cid)
	if err != nil || len(box.Closures) == 0 {
		t.Fatalf("inbox %+v %v", box, err)
	}

	pol, err := fac.SetClientPolicy(ctx, sa, factory.ClientPolicy{
		MaxCachedProjects: 3, CacheScope: factory.CacheScopeAll, PersistUnwrapKey: true, KeyTTLSeconds: 60, EncryptPouch: true,
	})
	if err != nil || pol.Revision < 1 {
		t.Fatalf("policy %v %v", pol, err)
	}
	in, raw, err := down.latestFor(sess.SigningPublicKey, fac.Store().FactoryID(), cid)
	if err != nil || clientmqtt.HasBody(raw) || in.Typ != clientmqtt.TypPolicy || in.Revision != pol.Revision ||
		in.MaxCachedProjects != 3 || in.CacheScope != factory.CacheScopeAll || !in.PersistUnwrapKey || in.KeyTTLSeconds != 60 || !in.EncryptPouch {
		t.Fatalf("policy intent %+v %v %s", in, err, raw)
	}
	if err := fac.SetCacheLimit(ctx, sa, 4); err != nil {
		t.Fatal(err)
	}
	in, raw, err = down.latestFor(sess.SigningPublicKey, fac.Store().FactoryID(), cid)
	if err != nil || clientmqtt.HasBody(raw) || in.Typ != clientmqtt.TypPolicy || in.Revision != pol.Revision+1 ||
		in.MaxCachedProjects != 4 || !in.PersistUnwrapKey || in.KeyTTLSeconds != 60 {
		t.Fatalf("limit fanout %+v %v %s", in, err, raw)
	}

	nb, na := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(24*time.Hour)
	if _, err := fac.IssueRuntimeGrant(ctx, sa, cid, nb, na); err != nil {
		t.Fatal(err)
	}
	bag := factory.Bag{ClientID: cid, Connected: true}
	if err := fac.DistributeToClient(ctx, pe.tok, proj.ID, cid, &bag, factory.Clocks{}); err != nil {
		t.Fatal(err)
	}
	raw = down.last()
	if clientmqtt.HasBody(raw) {
		t.Fatalf("closure mqtt %s", raw)
	}
	cin, err := clientmqtt.Verify(sess.SigningPublicKey, fac.Store().FactoryID(), cid, raw)
	if err != nil || cin.Typ != clientmqtt.TypClosure || cin.AssetID != proj.ID.String() {
		t.Fatalf("closure intent %+v %v", cin, err)
	}
}
