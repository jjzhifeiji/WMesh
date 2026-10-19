// 厂端接收模版副本不改已有正文；之后新建才套用。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 接收模版副本不得改掉已有正文，只把模版存下。
func TestAcceptTemplateDelivery(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte(`{"name":"厂内","current":190,"legacy":true}`)
	// 新建一份厂级工艺，失败说明起草入口坏了。
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂内工艺", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	got, err := fac.ReadAssetContent(ctx, pe.tok, proc.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, body) {
		// 模版没有按预期收下，说明下发没进厂。
		t.Fatalf("before template %q %v", got, err)
	}

	// 编成文本再查敏感词，编失败就无法判断泄密。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 编成文本失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把编成文本的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := seed.ID
	// 填上这条记录的字段，填错会让后面的比对失败。
	snap := factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProcess, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}
	// 复制一份快照再改坏，用来验证下发会被拒。
	bad := snap
	// 重复填满这段字节，长度不对说明缓冲不够。
	bad.Digest = bytes.Repeat([]byte{1}, 32)
	// 收模版应因完整性失败被拒绝，放行说明没拦住。
	if err := fac.AcceptTemplateDelivery(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bad digest: %v", err)
	}
	// 收模版失败就停，否则后面没有可靠结果。
	if err := fac.AcceptTemplateDelivery(ctx, snap); err != nil {
		// 把收模版的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	got, err = fac.ReadAssetContent(ctx, pe.tok, proc.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("after %s want %s err %v", got, body, err)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	still, err := fac.GetAsset(ctx, pe.tok, proc.ID)
	// 读台账失败或修订不对就停，说明没达预期。
	if err != nil || still.Revision != proc.Revision {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rev %+v", still)
	}
	// 按编号取模版，失败说明没下发到本厂。
	tpl, err := fac.GetTemplate(ctx, pe.tok, factory.KindProcess)
	// 取模版失败或修订不对就停，说明没达预期。
	if err != nil || tpl.Revision != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("tpl %+v %v", tpl, err)
	}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	created, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "新工艺", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	applied, err := fac.ReadAssetContent(ctx, pe.tok, created.ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 套用这次变更，失败说明当前状态不接受。
	want, err := contenttpl.Apply(schema, body)
	// 套用变更失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(applied, want) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("new %s want %s err %v", applied, want, err)
	}

	// 数组根字段表，误导入时工艺会被套成数组。
	arrayRoot := []byte(`{"root":"array","templates":[]}`)
	// 摘要对得上的数组根工艺快照，只靠根类型拦下。
	arraySnap := factory.TemplateSnapshot{
		ID: snap.ID, Kind: factory.KindProcess, Revision: 2,
		Schema: json.RawMessage(arrayRoot), Digest: digest.Sum(arrayRoot), TargetFactoryID: &fid,
	}
	// 数组根工艺应被拒为模版不合法，放行说明没拦住。
	if err := fac.AcceptTemplateDelivery(ctx, arraySnap); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 断言没通过就停，数组根工艺被收下了。
		t.Fatalf("array root process: %v", err)
	}
	// 再取工艺模版，被拒后应仍是第一版。
	tpl, err = fac.GetTemplate(ctx, pe.tok, factory.KindProcess)
	// 取模版失败或修订变了就停，说明拒收前已写副本。
	if err != nil || tpl.Revision != 1 {
		// 断言没通过就停，被拒的快照仍落了库。
		t.Fatalf("tpl after array root %+v %v", tpl, err)
	}
	// 拒收后再新建一份厂级工艺，看正文仍按旧模版套。
	later, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "拒收后工艺", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回新建正文，看有没有被套成数组。
	laterBody, err := fac.ReadAssetContent(ctx, pe.tok, later.ID)
	// 读正文失败或不是旧模版套出的正文就停。
	if err != nil || !bytes.Equal(laterBody, want) {
		// 断言没通过就停，新建正文没按旧模版套。
		t.Fatalf("after array root %s want %s err %v", laterBody, want, err)
	}
	// 同样的数组根当工程送来，旧登记簿仍照收。
	arraySnap.ID, arraySnap.Kind, arraySnap.Revision = id.New(), factory.KindProject, 1
	// 工程数组根被拒就停，说明根类型检查误伤了工程。
	if err := fac.AcceptTemplateDelivery(ctx, arraySnap); err != nil {
		// 把收工程模版的错误打出来并停住。
		t.Fatalf("array root project: %v", err)
	}
}

// 列出已接收模版，缺了说明下发没有收进厂里。
func TestListProjectTemplates(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 列出工程模版，失败说明无权或库不可用。
	empty, err := fac.ListProjectTemplates(ctx, sa)
	// 列模版失败或条数不对就停，说明没达预期。
	if err != nil || len(empty) != 0 {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("empty %+v %v", empty, err)
	}
	// 留出单层焊道，解析到后再核对模版编号。
	var single contenttpl.ProjectItemSchema
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, it := range contenttpl.SeedProjectItems() {
		// 编成文本失败就停，否则后面没有可靠结果。
		if it.Name == "单层焊道" {
			// 记下这条焊道，模版或工艺引用不对会在这里露馅。
			single = it
			break
		}
	}
	// 编成文本再查敏感词，编失败就无法判断泄密。
	raw, err := contenttpl.Marshal(contenttpl.ObjectSchema(single.Fields))
	// 编成文本失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把编成文本的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := seed.ID
	// 收模版失败就停，否则后面没有可靠结果。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 新取一个编号，撞号会让两条记录分不清。
		ID: id.New(), Kind: factory.KindProject, Name: single.Name, Revision: 3, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	// 列出工程模版，失败说明无权或库不可用。
	rows, err := fac.ListProjectTemplates(ctx, sa)
	// 列模版失败或修订不对就停，说明没达预期。
	if err != nil || len(rows) != 1 || rows[0].Name != single.Name || rows[0].Revision != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("list %+v %v", rows, err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	anonID := id.New()
	// 装一条未具名模版，用来验证列表仍能按内容认出。
	anon := factory.TemplateSnapshot{
		ID: anonID, Kind: factory.KindProject, Revision: 1, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}
	// 收模版失败就停，否则后面没有可靠结果。
	if err := fac.AcceptTemplateDelivery(ctx, anon); err != nil {
		// 把收模版的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 补上坡口这个名字，列表里没有它说明没收下。
	anon.Name = "坡口"
	// 收模版失败就停，否则后面没有可靠结果。
	if err := fac.AcceptTemplateDelivery(ctx, anon); err != nil {
		// 把收模版的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 列出工程模版，失败说明无权或库不可用。
	rows, err = fac.ListProjectTemplates(ctx, sa)
	// 列模版失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列模版的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 先当成没找到，扫到再改，避免沿用旧结果。
	found := false
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, row := range rows {
		// 修订号应按这次保存前进，没变说明写没落库。
		if row.ID == anonID && row.Name == "坡口" && row.Revision == 1 {
			// 记成已经找到，最后仍是假说明列表里没有。
			found = true
		}
	}
	// 扫完应该能找到，还没有说明结果里漏了这条。
	if !found {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("name not filled %+v", rows)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	stale := []byte(`{"root":"array","templates":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"corner","name":"包角","extra":true}]}`)
	// 写模版副本失败就停，否则后面没有可靠结果。
	if _, _, err := fac.Store().InsertTemplateReplica(ctx, factory.ContentTemplate{
		// 新取一个编号，撞号会让两条记录分不清。
		ID: id.New(), Kind: factory.KindProject, Revision: 9, Schema: stale, Digest: digest.Sum(stale),
	}); err != nil {
		t.Fatal(err)
	}
	// 列出工程模版，失败说明无权或库不可用。
	rows, err = fac.ListProjectTemplates(ctx, sa)
	// 列模版失败或条数不对就停，说明没达预期。
	if err != nil || len(rows) != 2 {
		// 旧修订盖住了新稿，说明先后比较写反了。
		t.Fatalf("stale catalog %+v %v", rows, err)
	}
}

// 读模版不留允许、同修订重送只记一次收下，被拒的读仍留拒绝。
func TestTemplateReadAndRedeliveryAudit(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 编出默认工艺字段表，后面拿它当送达正文。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 编字段表失败就停，否则没有可送的模版。
	if err != nil {
		// 把编字段表的错误打出来并停住。
		t.Fatal(err)
	}
	// 留下工厂编号，送达快照要指明收件厂。
	fid := seed.ID
	// 拼一份工艺模版快照，两次送达用同一份。
	snap := factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProcess, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}
	// 同一修订连送两次，模拟通道重推。
	for i := 0; i < 2; i++ {
		// 收模版失败就停，重送不该被拒。
		if err := fac.AcceptTemplateDelivery(ctx, snap); err != nil {
			// 把第几次收模版的错误打出来并停住。
			t.Fatalf("deliver #%d: %v", i+1, err)
		}
	}
	// 同修订同摘要只该记一条收下，多了说明重送也在写审计。
	if got := softwareAudit(t, ctx, fac, "accept_template", audit.Allow); len(got) != 1 {
		// 条数不对就停，把实际条数打出来。
		t.Fatalf("accept_template allow rows %d want 1", len(got))
	}
	// 记下读模版前的审计全量，事后核对没有新增。
	before := auditIDs(t, ctx, fac)
	// 像前端轮询那样连读两轮工艺和工程模版。
	for i := 0; i < 2; i++ {
		// 读工艺模版失败就停，轮询读不该出错。
		if _, err := fac.GetTemplate(ctx, sa, factory.KindProcess); err != nil {
			// 把读工艺模版的错误打出来并停住。
			t.Fatalf("get template #%d: %v", i+1, err)
		}
		// 列工程模版失败就停，轮询读不该出错。
		if _, err := fac.ListProjectTemplates(ctx, sa); err != nil {
			// 把列工程模版的错误打出来并停住。
			t.Fatalf("list project templates #%d: %v", i+1, err)
		}
	}
	// 读完后审计条数应不变，多了说明轮询读仍在记允许。
	if after := auditIDs(t, ctx, fac); len(after) != len(before) {
		// 条数变了就停，把前后条数打出来。
		t.Fatalf("audit rows %d -> %d after reads", len(before), len(after))
	}
	// 读不存在的模版种类应被拒。
	if _, err := fac.GetTemplate(ctx, sa, "bogus"); !errors.Is(err, domain.ErrNotFound) {
		// 没被拒就停，种类检查失效了。
		t.Fatalf("bogus kind: %v", err)
	}
	// 被拒的读仍要留拒绝，少了说明把拒绝也一并去掉了。
	if got := softwareAudit(t, ctx, fac, "get_template", audit.Deny); len(got) != 1 {
		// 拒绝条数不对就停，把实际条数打出来。
		t.Fatalf("get_template deny rows %d want 1", len(got))
	}
}
