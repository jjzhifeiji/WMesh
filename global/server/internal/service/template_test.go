// 内容模版：改模版不改已有正文；仅新建套用；非管理员拒绝。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

func TestContentTemplate(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	if err != nil {
		t.Fatal(err)
	}

	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "旧工艺", []byte(`{"name":"旧","current":180,"legacy":true}`))
	if err != nil {
		t.Fatal(err)
	}
	oldBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
	if err != nil {
		t.Fatal(err)
	}

	tpl, err := h.WAN.GetTemplate(ctx, tok, global.KindProcess)
	if err != nil || tpl.Revision != 1 {
		t.Fatalf("%+v %v", tpl, err)
	}
	var sch contenttpl.Schema
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		t.Fatal(err)
	}
	sch.Fields = append(sch.Fields, contenttpl.Field{Key: "gas", Label: "气体", Type: contenttpl.TypeNumber, Unit: "L/min", Default: 15.0})
	raw, err := json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	next, err := h.WAN.UpdateTemplate(ctx, tok, global.KindProcess, tpl.Revision, raw)
	if err != nil || next.Revision != 2 {
		t.Fatalf("%+v %v", next, err)
	}

	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
	if err != nil || !bytes.Equal(body, oldBody) {
		t.Fatalf("old body changed %s", body)
	}
	gotProc, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID)
	if err != nil || gotProc.Revision != proc.Revision {
		t.Fatalf("proc rev %+v want %d", gotProc, proc.Revision)
	}

	created, err := h.WAN.CreatePlatformProcess(ctx, tok, "新工艺", []byte(`{"name":"新","current":190,"legacy":true}`))
	if err != nil {
		t.Fatal(err)
	}
	newBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := contenttpl.Apply(next.Schema, []byte(`{"name":"新","current":190,"legacy":true}`))
	if err != nil || !bytes.Equal(newBody, want) {
		t.Fatalf("new %s want %s err %v", newBody, want, err)
	}

	extra := []byte(`{"name":"旧","current":180,"junk":true}`)
	updated, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proc.ID, gotProc.Revision, extra)
	if err != nil {
		t.Fatal(err)
	}
	gotExtra, err := h.WAN.ReadPlatformAssetContent(ctx, tok, updated.ID)
	if err != nil || !bytes.Equal(gotExtra, extra) {
		t.Fatalf("update apply %s", gotExtra)
	}

	if _, err := h.WAN.UpdateTemplate(ctx, "bad", global.KindProcess, next.Revision, raw); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("anon: %v", err)
	}

	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	src := []byte(`{"name":"升档","current":210,"junk":true}`)
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 3, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "升档工艺", Content: src, Digest: digest.Sum(src),
		Copyable: true, Status: global.AssetAvailable,
	})
	if err != nil {
		t.Fatal(err)
	}
	pbody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, promoted.ID)
	if err != nil || !bytes.Equal(pbody, src) {
		t.Fatalf("promote apply %s", pbody)
	}
}
