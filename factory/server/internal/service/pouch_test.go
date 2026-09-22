// C1：本机袋未登录解不开；默认钥不落盘；退出换人清库。
package service_test

import (
	"bytes"
	"testing"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestPouchRequiresLogin(t *testing.T) {
	p := factory.NewPouch()
	aid := id.New()
	who := id.New()
	if err := p.PutPlain(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`)); err != domain.ErrUnauthorized {
		t.Fatalf("put: %v", err)
	}
	if _, err := p.Open(aid); err != domain.ErrUnauthorized {
		t.Fatalf("open: %v", err)
	}
	key, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Login(key, who, false); err != nil {
		t.Fatal(err)
	}
	if err := p.PutPlain(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := p.Open(aid)
	if err != nil || !bytes.Equal(got, []byte(`{"n":1}`)) {
		t.Fatalf("open %s %v", got, err)
	}
	p.Logout()
	if p.HasUnwrapKey() {
		t.Fatal("key still in memory")
	}
	if _, err := p.Open(aid); err != domain.ErrUnauthorized {
		t.Fatalf("after logout: %v", err)
	}
	if err := p.RelockFromMaterial(); err != domain.ErrUnauthorized {
		t.Fatalf("persist false relock: %v", err)
	}
	for _, env := range p.DiskSnapshot() {
		if bytes.Contains(env.Blob, key) || bytes.Contains(env.Blob, []byte(`{"n":1}`)) {
			t.Fatal("disk leaked key or plaintext")
		}
	}
}

func TestPouchPersonalFollowsOwner(t *testing.T) {
	p := factory.NewPouch()
	key, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	a := id.New()
	b := id.New()
	pid := id.New()
	if err := p.Login(key, a, false); err != nil {
		t.Fatal(err)
	}
	if err := p.PutPlain(pid, store.AssetLevelPersonal, "我的", 1, &a, []byte(`{"p":1}`)); err != nil {
		t.Fatal(err)
	}
	fid := id.New()
	if err := p.PutPlain(fid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"f":1}`)); err != nil {
		t.Fatal(err)
	}
	p.Logout()
	if err := p.Login(key, b, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open(pid); err != domain.ErrNotFound {
		t.Fatalf("personal: %v", err)
	}
	if _, err := p.Open(fid); err != domain.ErrNotFound {
		t.Fatalf("factory after wipe: %v", err)
	}
}

func TestPouchPutPlainIfNewer(t *testing.T) {
	p := factory.NewPouch()
	key, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	who := id.New()
	if err := p.Login(key, who, false); err != nil {
		t.Fatal(err)
	}
	aid := id.New()
	ok, err := p.PutPlainIfNewer(aid, store.AssetLevelFactory, "厂级", 2, nil, []byte(`{"n":2}`))
	if err != nil || !ok {
		t.Fatalf("put2 %v %v", ok, err)
	}
	ok, err = p.PutPlainIfNewer(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`))
	if err != nil || ok {
		t.Fatalf("stale %v %v", ok, err)
	}
	got, err := p.Open(aid)
	if err != nil || !bytes.Equal(got, []byte(`{"n":2}`)) {
		t.Fatalf("held %s %v", got, err)
	}
}
