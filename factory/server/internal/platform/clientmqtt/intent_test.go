package clientmqtt

import (
	"encoding/json"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
)

func TestIntentSignVerifyAndNoBody(t *testing.T) {
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fid, cid := id.New(), id.New()
	aid := id.New()
	in := Intent{Typ: TypClosure, Revision: 3, AssetID: aid.String(), Digest: []byte{1, 2, 3, 4}}
	raw, err := Sign(priv, fid, cid, in)
	if err != nil {
		t.Fatal(err)
	}
	if HasBody(raw) || strings.Contains(string(raw), `"content"`) || strings.Contains(string(raw), `"members"`) {
		t.Fatalf("body in mqtt: %s", raw)
	}
	got, err := Verify(pub, fid, cid, raw)
	if err != nil || got.Revision != 3 || got.AssetID != aid.String() {
		t.Fatalf("verify %+v %v", got, err)
	}
	if _, err := Verify(pub, fid, id.New(), raw); err == nil {
		t.Fatal("wrong client accepted")
	}
	pol := Intent{Typ: TypPolicy, Revision: 2, MaxCachedProjects: 3, CacheScope: "all"}
	praw, err := Sign(priv, fid, cid, pol)
	if err != nil {
		t.Fatal(err)
	}
	if HasBody(praw) {
		t.Fatalf("policy body: %s", praw)
	}
	if _, err := Verify(pub, fid, cid, praw); err != nil {
		t.Fatal(err)
	}
	bad, _ := json.Marshal(map[string]any{"typ": TypClosure, "content": "WM2xxxx"})
	if !HasBody(bad) {
		t.Fatal("content must count as body")
	}
}
