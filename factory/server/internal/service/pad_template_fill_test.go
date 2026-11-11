// 示教器 /pad/assets 新建按已收内容模版补键，和网页新建一样落全字段；示教器写了的值不改（D-75）。
package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	factory "wmesh/factory/internal/service"
)

// 收下工艺模版和三份工程种子模版后，示教器只传了名字的工艺补齐模版键，工程焊道补上模版身份。
func TestCreatePadFillsTemplateKeys(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 留下工厂编号，下发要指明接收厂。
	fid := seed.ID
	// 编出默认工艺模版字段表。
	procRaw, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 编不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 收下工艺模版。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{ID: weldTplID, Kind: factory.KindProcess, Name: "焊接参数", Revision: 1, Schema: procRaw, Digest: digest.Sum(procRaw), TargetFactoryID: &fid}); err != nil {
		// 收不下就停住。
		t.Fatalf("accept process template: %v", err)
	}
	// 逐份收下工程种子模版。
	for _, it := range contenttpl.SeedProjectItems() {
		// 编成对象根字段表。
		raw, err := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
		// 编不出就停住。
		if err != nil {
			// 把错误打出来并停住。
			t.Fatal(err)
		}
		// 收下这一份。
		if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{ID: uuid.MustParse(it.ID), Kind: factory.KindProject, Name: it.Name, Revision: 1, Schema: raw, Digest: digest.Sum(raw), TargetFactoryID: &fid}); err != nil {
			// 收不下就停住。
			t.Fatalf("accept project template %s: %v", it.Name, err)
		}
	}
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass-1", factory.RoleOperator)
	// 示教器新建个人工艺，只带名字和一个改过的电流。
	proc, err := fac.CreatePad(ctx, op.tok, "", factory.KindProcess, "ciqmb", []byte(`{"name":"ciqmb","current":177}`), uuid.Nil, "", nil)
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad create process: %v", err)
	}
	// 读回正文。
	body, err := fac.ReadAssetContent(ctx, op.tok, proc.ID)
	// 读不到就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 准备解开正文。
	var m map[string]any
	// 解不开就停住。
	if err := json.Unmarshal(body, &m); err != nil {
		// 把正文打出来并停住。
		t.Fatalf("process body %s: %v", body, err)
	}
	// 示教器写的值原样留着。
	if m["name"] != "ciqmb" || m["current"] != float64(177) {
		// 被改就停住。
		t.Fatalf("pad values changed: %s", body)
	}
	// 模版里的偏移键要补上。
	for _, k := range []string{"offsetX", "offsetY", "offsetZ"} {
		// 缺键就停住。
		if _, ok := m[k]; !ok {
			// 把正文打出来并停住。
			t.Fatalf("missing %s: %s", k, body)
		}
	}
	// 示教器新建单层工程，焊道只有身份、名字和空点位。
	proj, err := fac.CreatePad(ctx, op.tok, "", factory.KindProject, "ciqmbgc", []byte(`[{"id":"p1","name":"焊道 1","points":[]}]`), uuid.Nil, "", nil, contenttpl.WeldSingle)
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad create project: %v", err)
	}
	// 读回正文。
	body, err = fac.ReadAssetContent(ctx, op.tok, proj.ID)
	// 读不到就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 准备解开焊道数组。
	var items []map[string]any
	// 解不开或焊道数不对就停住。
	if err := json.Unmarshal(body, &items); err != nil || len(items) != 1 {
		// 把正文打出来并停住。
		t.Fatalf("project body %s: %v", body, err)
	}
	// 焊道要补上单层种子模版身份，示教器写的身份和名字不改。
	if items[0]["templateId"] != contenttpl.SeedTplSingle || items[0]["id"] != "p1" || items[0]["name"] != "焊道 1" {
		// 把正文打出来并停住。
		t.Fatalf("project item not filled: %s", body)
	}
	// 补了键，回包摘要对的是补键后的正文，和示教器原正文不同：示教器要按身份重拉，不能拿本机正文配这个摘要（D-96）。
	sent := []byte(`[{"id":"p1","name":"焊道 1","points":[]}]`)
	// 摘要相同说明没补键，后面的重拉核对就没意义。
	if string(proj.Digest) == string(digest.Sum(sent)) || string(proj.Digest) != string(digest.Sum(body)) {
		// 把两份摘要打出来并停住。
		t.Fatalf("create digest %x sent %x body %x", proj.Digest, digest.Sum(sent), digest.Sum(body))
	}
	// 操作员用平板登录，按清单对账。
	sess := padSyncLogin(t, ctx, fac, "op", "op-pass-1")
	// 拉清单，找出刚建的工程那一条。
	ref, ok := padInboxRef(padSyncInbox(t, ctx, fac, sess.Token), proj.ID)
	// 清单要带编号，示教器据此纠正修复前补错的本机号（D-54）。
	if !ok || ref.Code == "" || ref.Code != proj.Code || ref.Revision != proj.Revision {
		// 把这一条打出来并停住。
		t.Fatalf("inbox ref %+v ok=%v code=%s", ref, ok, proj.Code)
	}
	// 拉过站包，整包摘要要和清单一致，示教器按修订加摘要判断要不要重拉。
	got, err := fac.PadPullClientClosure(ctx, sess.Token, proj.ID)
	// 拉不到或摘要对不上就停住。
	if err != nil || string(got.Snapshot.Digest) != string(ref.Digest) {
		// 把错误和摘要打出来并停住。
		t.Fatalf("pull %v ref %x pull %x", err, ref.Digest, got.Snapshot.Digest)
	}
	// 示教器改正文时带上原正文的模版键再传，厂端改正文不套模版、原样收下（内容模版 §7-3）。
	edited := []byte(`[{"id":"p1","name":"焊道 2","points":[],"templateId":"` + contenttpl.SeedTplSingle + `","x":1}]`)
	// 传不上就停住。
	row, err := fac.ApplyAppContent(ctx, op.tok, proj.ID, edited)
	// 改正文被拒或摘要不是原样那份就停住。
	if err != nil || string(row.Digest) != string(digest.Sum(edited)) {
		// 把错误打出来并停住。
		t.Fatalf("apply content %v", err)
	}
}
