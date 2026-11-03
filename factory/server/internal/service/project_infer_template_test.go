// 经接口新建工程：没写模版身份的焊道按作业类型补种子身份，不再被悄悄吞掉；认不出的模版身份明确拒绝。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 收下 T 排模版副本后，单层和 T 排工程的焊道不带模版身份也照样落库，读回补上对应种子身份。
func TestProjectWithoutTemplateIDInfersSeed(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建管理员，厂级工程由它来建。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	// 标明直接作业上下文。
	direct := factory.WorkContext{}
	// 新建一份 T 排厂级工艺给间隙带引用。
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "打底", []byte(`{"name":"p","current":180}`), store.WeldKindTBar)
	// 建工艺失败就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 发布工艺，工程才能钉住它。
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住。
		t.Fatal(err)
	}
	// 当前 T 排种子的规范字段表，当作 WAN 送来的副本。
	tbar, _ := contenttpl.SeedProjectItem(contenttpl.SeedTplTBar)
	// 编成规范字段表。
	schema, err := contenttpl.Marshal(contenttpl.ObjectSchema(tbar.Fields))
	// 编不出就停。
	if err != nil {
		// 把编字段表的错误打出来并停住。
		t.Fatal(err)
	}
	// 记下本厂身份，副本只给本厂。
	fid := seed.ID
	// 收下 T 排模版副本，库里从此有工程模版，新建正文要过模版。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 身份、名称、修订和摘要都按 WAN 下发的样子填。
		ID: uuid.MustParse(contenttpl.SeedTplTBar), Kind: factory.KindProject, Name: tbar.Name, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}); err != nil {
		// 把收副本的错误打出来并停住。
		t.Fatal(err)
	}
	// T 排焊道去掉模版身份，只留示教器写的种类。
	tbody := strings.NewReplacer("ROOT", proc.ID.String(), "CAP", proc.ID.String(), `,"templateId":"`+contenttpl.SeedTplTBar+`"`, "").Replace(padTBarBody)
	// 确认确实去掉了模版身份，免得测成带身份的老路。
	if strings.Contains(tbody, "templateId") {
		// 替换没生效就停住。
		t.Fatalf("templateId still in body %s", tbody)
	}
	// 按 T 排新建厂级工程。
	tproj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "T排无身份", []byte(tbody), nil, store.WeldKindTBar)
	// 建工程失败就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 读回 T 排工程正文，焊道不能丢，身份补成 T 排种子。
	tarr := readItems(t, ctx, fac, pe.tok, tproj.ID, 1, contenttpl.SeedTplTBar)
	// 逐个专有字段对照。
	for k, want := range tbarPadKeys {
		// 丢了或改了就停。
		if tarr[0][k] != want {
			// 带上键和值停住。
			t.Fatalf("%s: got %v want %v", k, tarr[0][k], want)
		}
	}
	// 单层两条焊道：一条没写身份，一条写了空串，和示教器单层上传的形状一样。
	sbody := `[{"id":"s1","name":"焊道 1"},{"id":"s2","name":"焊道 2","templateId":""}]`
	// 按单层新建厂级工程；库里没有单层份，按内置种子字段套。
	sproj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层无身份", []byte(sbody), nil, store.WeldKindSingle)
	// 建工程失败就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 读回单层工程正文，两条都在，身份补成单层种子。
	sarr := readItems(t, ctx, fac, pe.tok, sproj.ID, 2, contenttpl.SeedTplSingle)
	// 名字要原样留下。
	if sarr[0]["name"] != "焊道 1" || sarr[1]["name"] != "焊道 2" {
		// 名字丢了就停住。
		t.Fatalf("names lost %v", sarr)
	}
	// 焊道点了库里没有、也不是种子的模版，必须明确拒绝。
	_, err = fac.CreateFactoryProject(ctx, pe.tok, direct, "未知模版", []byte(`[{"name":"x","templateId":"00000000-0000-4000-8000-000000000009"}]`), nil, store.WeldKindSingle)
	// 没拒或拒错了原因就停。
	if !errors.Is(err, domain.ErrProjectItemTemplate) {
		// 带上实际错误停住。
		t.Fatalf("want ErrProjectItemTemplate, got %v", err)
	}
}

// readItems 读回工程正文，核对条数和每条的模版身份，交回各条焊道。
func readItems(t *testing.T, ctx context.Context, fac *factory.Service, tok string, id uuid.UUID, n int, tpl string) []map[string]any {
	// 测试辅助，出错时报调用方的行。
	t.Helper()
	// 读回落库的正文。
	got, err := fac.ReadAssetContent(ctx, tok, id)
	// 读不回就停。
	if err != nil {
		// 把读正文的错误打出来并停住。
		t.Fatal(err)
	}
	// 准备接读回的焊道。
	var arr []map[string]any
	// 拆不开或条数不对就停。
	if err := json.Unmarshal(got, &arr); err != nil || len(arr) != n {
		// 焊道被吞或形状不对就停住。
		t.Fatalf("body %s %v", got, err)
	}
	// 每条都要补上对应的种子身份。
	for _, it := range arr {
		// 身份不对就停。
		if it["templateId"] != tpl {
			// 带上正文停住。
			t.Fatalf("templateId want %s in %s", tpl, got)
		}
	}
	return arr
}
