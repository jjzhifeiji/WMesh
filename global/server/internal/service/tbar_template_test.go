// D-37：T 排工程经接口新建不丢专有字段，没采的点保持空；库里没人改过的旧版种子升成当前种子。
package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/store"
)

// tbarSeedID 空库 T 排模版身份。
const tbarSeedID = "44444444-4444-4444-8444-444444444444"

// padTBarBody 示教器 TBarProject.encode 存下的形状；ROOT/CAP 换成真实工艺身份。
const padTBarBody = `[{"id":"5a0c6a52-8d1e-4f0b-9b55-3f0f7c1d2e01","name":"焊道 1","points":[` +
	`{"id":"p1","type":"START_SAFE","pose":{"x":812.5,"y":-120.25,"z":430.0,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[0.1,-20.5,90.0,0.0,45.0,10.0]},` +
	`{"id":"p2","type":"GROOVE_A_LOWER","pose":{"x":820.0,"y":-118.0,"z":402.5,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[1.0,-21.0,91.0,0.0,44.0,10.0],` +
	`"scanPose":{"x":820.0,"y":-118.0,"z":480.0,"rx":180.0,"ry":0.0,"rz":90.0},"scanJoints":[1.0,-25.0,88.0,0.0,47.0,10.0]},` +
	`{"id":"p3","type":"GROOVE_B_LOWER"},{"id":"p4","type":"GROOVE_A_UPPER"},{"id":"p5","type":"GROOVE_B_UPPER"},` +
	`{"id":"p6","type":"LASER_BASE_1"},{"id":"p7","type":"LASER_BASE_2"},{"id":"p8","type":"START"},{"id":"p9","type":"END"},{"id":"p10","type":"END_SAFE"}],` +
	`"gapBands":[{"minGap":0.0,"maxGap":2.5,"rootProcessId":"ROOT","capProcessId":"CAP","capProcessIds":["CAP"]}],` +
	`"kind":"tbar","templateId":"` + tbarSeedID + `",` +
	`"plateThickness":12.0,"grooveAngleDeg":45.0,"teachMode":"LASER","laserLowerWidthMm":3.2,"laserUpperWidthMm":14.8,` +
	`"extendUpMm":5.0,"extendDownMm":3.0,"laserWeldLengthMm":600.0}]`

// tbarPadKeys 示教器 T 排焊道上的专有标量，接口新建后都要原样在。
var tbarPadKeys = map[string]any{
	"plateThickness": 12.0, "grooveAngleDeg": 45.0, "teachMode": "LASER", "laserLowerWidthMm": 3.2,
	"laserUpperWidthMm": 14.8, "extendUpMm": 5.0, "extendDownMm": 3.0, "laserWeldLengthMm": 600.0,
}

// 平台 T 排工程经接口新建：专有字段读回都在，没采的点不补零位姿、不补 X 向参考。
func TestTBarProjectKeepsPadFields(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停。
	if err != nil {
		// 不符即停：登录失败。
		t.Fatal(err)
	}
	// 新建一份平台工艺给间隙带引用。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "打底", []byte(`{"name":"p","current":180}`), store.WeldKindTBar)
	// 建工艺失败就停。
	if err != nil {
		// 不符即停：建工艺失败。
		t.Fatal(err)
	}
	// 发布工艺，工程才能钉住它。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布失败就停。
	if err != nil {
		// 不符即停：发布失败。
		t.Fatal(err)
	}
	// 把打底和盖面都换成这份工艺。
	body := strings.NewReplacer("ROOT", proc.ID.String(), "CAP", proc.ID.String()).Replace(padTBarBody)
	// 按 T 排新建平台工程，正文走 normalizeContent。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "T排", []byte(body), nil, store.WeldKindTBar)
	// 建工程失败就停。
	if err != nil {
		// 不符即停：建工程失败。
		t.Fatal(err)
	}
	// 读回落库的正文。
	got, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 读不回就停。
	if err != nil {
		// 不符即停：读正文失败。
		t.Fatal(err)
	}
	// 核对读回的 T 排正文。
	assertPadTBar(t, got)
}

// assertPadTBar 专有字段原样在，已采点留下，没采点每个可空槽都是空。
func assertPadTBar(t *testing.T, raw []byte) {
	// 测试辅助，出错时报调用方的行。
	t.Helper()
	// 准备接读回的焊道。
	var arr []map[string]any
	// 拆不开就停。
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 1 {
		// 不符即停：正文形状不对。
		t.Fatalf("body %s %v", raw, err)
	}
	// 逐个专有字段对照。
	for k, want := range tbarPadKeys {
		// 丢了或改了就停。
		if arr[0][k] != want {
			// 不符即停：带上键和值。
			t.Fatalf("%s: got %v want %v in %s", k, arr[0][k], want, raw)
		}
	}
	// 取出点列。
	pts, _ := arr[0]["points"].([]any)
	// 十个点都要在。
	if len(pts) != 10 {
		// 不符即停：点数不对。
		t.Fatalf("points %s", raw)
	}
	// 第二个点已采，拍摄位也要在。
	p2, _ := pts[1].(map[string]any)
	// 已采的丢了就停。
	if p2["pose"] == nil || p2["scanPose"] == nil || p2["scanJoints"] == nil {
		// 不符即停：已采点丢了。
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
				// 不符即停：带上点。
				t.Fatalf("uncaptured %v stuffed %s: %v", pt["type"], k, pt[k])
			}
		}
	}
}

// stripTBarToRetired 去掉这次新加的键、可空标记和元素键，还原成上一版 T 排种子。
func stripTBarToRetired(fields []contenttpl.Field) []contenttpl.Field {
	// 这次新加的键，上一版没有。
	added := map[string]bool{"scanPose": true, "scanJoints": true, "plateThickness": true, "grooveAngleDeg": true, "teachMode": true,
		"laserLowerWidthMm": true, "laserUpperWidthMm": true, "extendUpMm": true, "extendDownMm": true, "laserWeldLengthMm": true,
		"extendLengthMm": true, "cornerGroupParams": true}
	// 准备放下还原后的字段。
	out := make([]contenttpl.Field, 0, len(fields))
	// 逐个字段还原。
	for _, f := range fields {
		// 新加的键跳过。
		if added[f.Key] {
			continue
		}
		// 上一版没有可空标记。
		f.Nullable = false
		// 子字段一并还原。
		if f.Fields != nil {
			// 递归还原子字段。
			f.Fields = stripTBarToRetired(f.Fields)
		}
		// 数组元素一并还原。
		if f.Items != nil {
			// 元素只有一个，包一层再还原。
			it := stripTBarToRetired([]contenttpl.Field{*f.Items})[0]
			// 上一版数组元素除关节角/偏移的 a 以外都不带键。
			if it.Key != "a" {
				// 清掉这次补上的元素键。
				it.Key = ""
			}
			// 换上还原后的元素。
			f.Items = &it
		}
		// 收下还原后的字段。
		out = append(out, f)
	}
	return out
}

// 库里 T 排种子仍是上一版原样：读模版时升一版成当前种子，之后新建 T 排也不丢字段；改过的种子不动。
func TestRetiredTBarSeedUpgraded(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立超管失败。
		t.Fatal(err)
	}
	// 登录拿会话。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停。
	if err != nil {
		// 不符即停：登录失败。
		t.Fatal(err)
	}
	// 先读一次，空库落下三份当前种子。
	if _, err := h.WAN.ListProjectTemplates(ctx, tok); err != nil {
		// 不符即停：落种子失败。
		t.Fatal(err)
	}
	// 取当前 T 排种子的字段表。
	seed, _ := contenttpl.SeedProjectItem(tbarSeedID)
	// 还原出上一版的规范字段表。
	old, err := contenttpl.Marshal(contenttpl.ObjectSchema(stripTBarToRetired(seed.Fields)))
	// 编不出就停。
	if err != nil {
		// 不符即停：还原失败。
		t.Fatal(err)
	}
	// 把库里的 T 排摆回上一版原样，修订记成 3，像是旧库升级前的样子。
	if err := h.DB.Exec("UPDATE content_templates SET schema = ?::jsonb, digest = ?, revision = 3 WHERE id = ?", string(old), digest.Sum(old), tbarSeedID).Error; err != nil {
		// 不符即停：摆旧数据失败。
		t.Fatal(err)
	}
	// 再读一次，没人改过的旧版种子应升一版。
	rows, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 读失败就停。
	if err != nil {
		// 不符即停：读模版失败。
		t.Fatal(err)
	}
	// 当前种子的规范字段表。
	want, _ := contenttpl.Marshal(contenttpl.ObjectSchema(seed.Fields))
	// 先当没找到。
	found := false
	// 在列表里找 T 排那一份。
	for _, r := range rows {
		// 不是 T 排就看下一份。
		if r.ID.String() != tbarSeedID {
			continue
		}
		// 记下找到了。
		found = true
		// 修订要升到 4，字段表要是当前种子。
		if r.Revision != 4 || string(r.Schema) != string(want) {
			// 不符即停：没升或升错。
			t.Fatalf("not upgraded rev=%d %s", r.Revision, r.Schema)
		}
	}
	// 找不到 T 排就停。
	if !found {
		// 不符即停：T 排不见了。
		t.Fatal("tbar template missing")
	}
	// 再读一次不能再升，修订保持 4。
	rows, err = h.WAN.ListProjectTemplates(ctx, tok)
	// 读失败就停。
	if err != nil {
		// 不符即停：读模版失败。
		t.Fatal(err)
	}
	// 逐份看修订。
	for _, r := range rows {
		// T 排修订又变了就停。
		if r.ID.String() == tbarSeedID && r.Revision != 4 {
			// 不符即停：重复升级。
			t.Fatalf("upgraded twice rev=%d", r.Revision)
		}
	}
	// 管理员改过的上一版：多一个自定义键，摘要认不出。
	edited, err := contenttpl.Marshal(contenttpl.ObjectSchema(append(stripTBarToRetired(seed.Fields), contenttpl.Field{Key: "note", Label: "备注", Type: contenttpl.TypeString, Default: ""})))
	// 编不出就停。
	if err != nil {
		// 不符即停：编改过的字段表失败。
		t.Fatal(err)
	}
	// 摆成改过的样子。
	if err := h.DB.Exec("UPDATE content_templates SET schema = ?::jsonb, digest = ?, revision = 7 WHERE id = ?", string(edited), digest.Sum(edited), tbarSeedID).Error; err != nil {
		// 不符即停：摆数据失败。
		t.Fatal(err)
	}
	// 再读一次，改过的不能动。
	rows, err = h.WAN.ListProjectTemplates(ctx, tok)
	// 读失败就停。
	if err != nil {
		// 不符即停：读模版失败。
		t.Fatal(err)
	}
	// 逐份看改过的那一份。
	for _, r := range rows {
		// 改过的被覆盖就停。
		if r.ID.String() == tbarSeedID && (r.Revision != 7 || string(r.Schema) != string(edited)) {
			// 不符即停：改过的被升了。
			t.Fatalf("edited overwritten rev=%d", r.Revision)
		}
	}
}
