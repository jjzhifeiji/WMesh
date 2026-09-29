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

// 改模版不改已有正文，非管理员不能改模版。
func TestContentTemplate(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}

	// 新建平台工艺「旧工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "旧工艺", []byte(`{"name":"旧","current":180,"legacy":true}`))
	// 新建平台工艺「旧工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「旧工艺」失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	oldBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}

	// 读内容模版，模版读不到就无法套新正文。
	tpl, err := h.WAN.GetTemplate(ctx, tok, global.KindProcess)
	// 读内容模版失败或修订不是1就停，不能当通过。
	if err != nil || tpl.Revision != 1 {
		// 不符即停：读内容模版失败或修订不是1。
		t.Fatalf("%+v %v", tpl, err)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var sch contenttpl.Schema
	// 解开焊道失败就停，解不开就看不到焊道字段。
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		// 不符即停：解开焊道失败。
		t.Fatal(err)
	}
	// 给字段表加一列，旧正文不该跟着变。
	sch.Fields = append(sch.Fields, contenttpl.Field{Key: "gas", Label: "气体", Type: contenttpl.TypeNumber, Unit: "L/min", Default: 15.0})
	// 把结果打成文本，以便检查有没有人员字段。
	raw, err := json.Marshal(sch)
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 不符即停：导出字段表失败。
		t.Fatal(err)
	}
	// 改内容模版，改模版失败就分不清新旧正文。
	next, err := h.WAN.UpdateTemplate(ctx, tok, global.KindProcess, tpl.Revision, raw)
	// 改内容模版失败或修订不是2就停，不能当通过。
	if err != nil || next.Revision != 2 {
		// 不符即停：改内容模版失败或修订不是2。
		t.Fatalf("%+v %v", next, err)
	}

	// 读平台正文，读不到就无法比对。
	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(body, oldBody) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("old body changed %s", body)
	}
	// 读取资产，读不到就无法核对原件。
	gotProc, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID)
	// 读取资产失败或修订没有跟上就停，不能当通过。
	if err != nil || gotProc.Revision != proc.Revision {
		// 不符即停：读取资产失败或修订没有跟上。
		t.Fatalf("proc rev %+v want %d", gotProc, proc.Revision)
	}

	// 新建平台工艺「新工艺」，建不成后面没有工艺号可对。
	created, err := h.WAN.CreatePlatformProcess(ctx, tok, "新工艺", []byte(`{"name":"新","current":190,"legacy":true}`))
	// 新建平台工艺「新工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「新工艺」失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	newBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, created.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 按字段表套正文，套失败说明样本不合法。
	want, err := contenttpl.Apply(next.Schema, []byte(`{"name":"新","current":190,"legacy":true}`))
	// 按字段表套正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(newBody, want) {
		// 不符即停：按字段表套正文失败或正文不一致。
		t.Fatalf("new %s want %s err %v", newBody, want, err)
	}

	// 准备正文样本，后面入库和比对都用它。
	extra := []byte(`{"name":"旧","current":180,"junk":true}`)
	// 改平台正文，正文改不了修订对不上。
	updated, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proc.ID, gotProc.Revision, extra)
	// 改平台正文失败就停，正文改不了修订对不上。
	if err != nil {
		// 不符即停：改平台正文失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	gotExtra, err := h.WAN.ReadPlatformAssetContent(ctx, tok, updated.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(gotExtra, extra) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("update apply %s", gotExtra)
	}

	// 改内容模版应被拒为未登录，放行或错类都算没拦住。
	if _, err := h.WAN.UpdateTemplate(ctx, "bad", global.KindProcess, next.Revision, raw); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		// 不符即停：改内容模版应被拒为未登录。
		t.Fatalf("anon: %v", err)
	}

	// 登记工厂「厂A」，建不成后面没有厂可授权。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	src := []byte(`{"name":"升档","current":210,"junk":true}`)
	// 升档，升不上去档位就断了。
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 3, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "升档工艺", Content: src, Digest: digest.Sum(src),
		Copyable: true, Status: global.AssetAvailable,
	})
	// 升档失败就停，升不上去档位就断了。
	if err != nil {
		// 不符即停：升档失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	pbody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, promoted.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(pbody, src) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("promote apply %s", pbody)
	}
}
