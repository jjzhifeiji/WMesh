// D-37：收下 T 排模版副本后经接口新建厂级 T 排工程，专有字段不丢，没采的点保持空。
package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// padTBarBody 示教器 TBarProject.encode 存下的形状；ROOT/CAP 换成真实工艺身份。
const padTBarBody = `[{"id":"5a0c6a52-8d1e-4f0b-9b55-3f0f7c1d2e01","name":"焊道 1","points":[` +
	`{"id":"p1","type":"START_SAFE","pose":{"x":812.5,"y":-120.25,"z":430.0,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[0.1,-20.5,90.0,0.0,45.0,10.0]},` +
	`{"id":"p2","type":"GROOVE_A_LOWER","pose":{"x":820.0,"y":-118.0,"z":402.5,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[1.0,-21.0,91.0,0.0,44.0,10.0],` +
	`"scanPose":{"x":820.0,"y":-118.0,"z":480.0,"rx":180.0,"ry":0.0,"rz":90.0},"scanJoints":[1.0,-25.0,88.0,0.0,47.0,10.0]},` +
	`{"id":"p3","type":"GROOVE_B_LOWER"},{"id":"p4","type":"GROOVE_A_UPPER"},{"id":"p5","type":"GROOVE_B_UPPER"},` +
	`{"id":"p6","type":"LASER_BASE_1"},{"id":"p7","type":"LASER_BASE_2"},{"id":"p8","type":"START"},{"id":"p9","type":"END"},{"id":"p10","type":"END_SAFE"}],` +
	`"gapBands":[{"minGap":0.0,"maxGap":2.5,"rootProcessId":"ROOT","capProcessId":"CAP","capProcessIds":["CAP"]}],` +
	`"kind":"tbar","templateId":"` + contenttpl.SeedTplTBar + `",` +
	`"plateThickness":12.0,"grooveAngleDeg":45.0,"teachMode":"LASER","laserLowerWidthMm":3.2,"laserUpperWidthMm":14.8,` +
	`"extendUpMm":5.0,"extendDownMm":3.0,"laserWeldLengthMm":600.0}]`

// tbarPadKeys 示教器 T 排焊道上的专有标量，接口新建后都要原样在。
var tbarPadKeys = map[string]any{
	"plateThickness": 12.0, "grooveAngleDeg": 45.0, "teachMode": "LASER", "laserLowerWidthMm": 3.2,
	"laserUpperWidthMm": 14.8, "extendUpMm": 5.0, "extendDownMm": 3.0, "laserWeldLengthMm": 600.0,
}

// 收下当前 T 排模版副本后建厂级 T 排工程：专有字段读回都在，没采的点不补零位姿、不补 X 向参考。
func TestTBarProjectKeepsPadFields(t *testing.T) {
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
	// 收下 T 排模版副本。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 身份、名称、修订和摘要都按 WAN 下发的样子填。
		ID: uuid.MustParse(contenttpl.SeedTplTBar), Kind: factory.KindProject, Name: tbar.Name, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}); err != nil {
		// 把收副本的错误打出来并停住。
		t.Fatal(err)
	}
	// 把打底和盖面都换成这份工艺。
	body := strings.NewReplacer("ROOT", proc.ID.String(), "CAP", proc.ID.String()).Replace(padTBarBody)
	// 按 T 排新建厂级工程，正文走 normalizeContent。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "T排", []byte(body), nil, store.WeldKindTBar)
	// 建工程失败就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 读回落库的正文。
	got, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
	// 读不回就停。
	if err != nil {
		// 把读正文的错误打出来并停住。
		t.Fatal(err)
	}
	// 准备接读回的焊道。
	var arr []map[string]any
	// 拆不开或条数不对就停。
	if err := json.Unmarshal(got, &arr); err != nil || len(arr) != 1 {
		// 正文形状不对就停住。
		t.Fatalf("body %s %v", got, err)
	}
	// 逐个专有字段对照。
	for k, want := range tbarPadKeys {
		// 丢了或改了就停。
		if arr[0][k] != want {
			// 带上键和值停住。
			t.Fatalf("%s: got %v want %v in %s", k, arr[0][k], want, got)
		}
	}
	// 取出点列。
	pts, _ := arr[0]["points"].([]any)
	// 十个点都要在。
	if len(pts) != 10 {
		// 点数不对就停住。
		t.Fatalf("points %s", got)
	}
	// 第二个点已采，拍摄位也要在。
	p2, _ := pts[1].(map[string]any)
	// 已采的丢了就停。
	if p2["pose"] == nil || p2["scanPose"] == nil || p2["scanJoints"] == nil {
		// 已采点丢了就停住。
		t.Fatalf("captured lost %v", p2)
	}
	// 第三个点起没采，可空槽都要空。
	for _, el := range pts[2:] {
		// 收成对象再看。
		pt, _ := el.(map[string]any)
		// 逐个可空槽核对。
		for _, k := range []string{"pose", "jointAngles", "executionOffsets", "refPointX", "scanPose", "scanJoints"} {
			// 补出值就停，示教器会当已采。
			if pt[k] != nil {
				// 带上点停住。
				t.Fatalf("uncaptured %v stuffed %s: %v", pt["type"], k, pt[k])
			}
		}
	}
}
