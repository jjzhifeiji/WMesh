// T 排工程正文按示教器的真实形状过模版：专有字段一个不丢，没采的点保持空。
package contenttpl

import (
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// 打底工艺身份，间隙带引用它。
const padRootID = "0b6f9c1e-2a4d-4c8e-9f10-1a2b3c4d5e6f"

// 盖面工艺身份，间隙带引用它。
const padCapID = "7d3e2f10-5b6a-4c7d-8e9f-a0b1c2d3e4f5"

// padTBarJSON 示教器 TBarProject.encode 存下的形状：缺省值不写，没采的点只有身份和类型。
const padTBarJSON = `[{"id":"5a0c6a52-8d1e-4f0b-9b55-3f0f7c1d2e01","name":"焊道 1","points":[` +
	`{"id":"p1","type":"START_SAFE","pose":{"x":812.5,"y":-120.25,"z":430.0,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[0.1,-20.5,90.0,0.0,45.0,10.0]},` +
	`{"id":"p2","type":"GROOVE_A_LOWER","pose":{"x":820.0,"y":-118.0,"z":402.5,"rx":180.0,"ry":0.0,"rz":90.0,"ext1":15.5},"jointAngles":[1.0,-21.0,91.0,0.0,44.0,10.0],` +
	`"scanPose":{"x":820.0,"y":-118.0,"z":480.0,"rx":180.0,"ry":0.0,"rz":90.0},"scanJoints":[1.0,-25.0,88.0,0.0,47.0,10.0]},` +
	`{"id":"p3","type":"GROOVE_B_LOWER"},` +
	`{"id":"p4","type":"GROOVE_A_UPPER"},` +
	`{"id":"p5","type":"GROOVE_B_UPPER"},` +
	`{"id":"p6","type":"LASER_BASE_1","pose":{"x":815.0,"y":-110.0,"z":400.0,"rx":180.0,"ry":0.0,"rz":90.0},"jointAngles":[0.5,-22.0,92.0,0.0,43.0,10.0]},` +
	`{"id":"p7","type":"LASER_BASE_2"},` +
	`{"id":"p8","type":"START"},` +
	`{"id":"p9","type":"END"},` +
	`{"id":"p10","type":"END_SAFE"}],` +
	`"selectedPointIndex":2,` +
	`"gapBands":[{"minGap":0.0,"maxGap":2.5,"rootProcessId":"` + padRootID + `","capProcessId":"` + padCapID + `","capProcessIds":["` + padCapID + `","` + padRootID + `"]},` +
	`{"minGap":2.5,"maxGap":5.0,"layer":2,"rootProcessId":"` + padRootID + `"}],` +
	`"kind":"tbar","templateId":"44444444-4444-4444-8444-444444444444",` +
	`"plateThickness":12.0,"grooveAngleDeg":45.0,"teachMode":"LASER","laserLowerWidthMm":3.2,"laserUpperWidthMm":14.8,` +
	`"extendUpMm":5.0,"extendDownMm":3.0,"laserWeldLengthMm":600.0}]`

// optionalPointKeys 点上没采就该保持空的槽。
var optionalPointKeys = []string{"pose", "jointAngles", "executionOffsets", "refPointX", "scanPose", "scanJoints"}

// applyOne 按空库三份套一份正文，交回第一条焊道。
func applyOne(t *testing.T, body string) map[string]any {
	// 测试辅助，出错时报调用方的行。
	t.Helper()
	// 按空库三份套正文，和 normalizeContent 走同一个入口。
	out, err := ApplyProjectItems(SeedProjectItems(), "", []byte(body))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套完的各条焊道。
	var got []map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &got); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 只该留下这一条焊道。
	if len(got) != 1 {
		// 焊道被吞或多出就中止。
		t.Fatalf("items %s", out)
	}
	return got[0]
}

// T 排专有字段原样留下，间隙带每个子字段都在，没采的点不补零位姿。
func TestTBarPadContentKept(t *testing.T) {
	// 套示教器存下的 T 排正文。
	got := applyOne(t, padTBarJSON)
	// 准备接原文，逐键对照。
	var src []map[string]any
	// 原文拆不开就是用例本身写错了。
	if err := json.Unmarshal([]byte(padTBarJSON), &src); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// T 排专有的标量字段逐个对照。
	for _, k := range []string{"plateThickness", "grooveAngleDeg", "teachMode", "laserLowerWidthMm", "laserUpperWidthMm", "extendUpMm", "extendDownMm", "laserWeldLengthMm", "selectedPointIndex", "kind", "templateId", "id", "name"} {
		// 值和原文不一样就是被模版吞了或改了。
		if got[k] != src[0][k] {
			// 带上键和两边的值停住。
			t.Fatalf("%s: got %v want %v", k, got[k], src[0][k])
		}
	}
	// 缺省未写的启用要按示教器缺省补成真。
	if got["isEnabled"] != true {
		// 补错就中止。
		t.Fatalf("isEnabled %v", got["isEnabled"])
	}
	// 取出套完的间隙带。
	bands, _ := got["gapBands"].([]any)
	// 两条间隙带都要在。
	if len(bands) != 2 {
		// 少了就中止。
		t.Fatalf("gapBands %v", got["gapBands"])
	}
	// 第一条带全部子字段都写了。
	b0, _ := bands[0].(map[string]any)
	// 盖面组两道都要留下，顺序不变。
	caps, _ := b0["capProcessIds"].([]any)
	// 子字段丢了或改了就中止。
	if b0["minGap"] != 0.0 || b0["maxGap"] != 2.5 || b0["layer"] != 1.0 || b0["rootProcessId"] != padRootID || b0["capProcessId"] != padCapID || len(caps) != 2 || caps[0] != padCapID || caps[1] != padRootID {
		// 带上原值停住。
		t.Fatalf("band0 %v", b0)
	}
	// 第二条带只写了部分子字段，其余按示教器缺省补。
	b1, _ := bands[1].(map[string]any)
	// 层号留下，没写的盖面补空。
	if b1["layer"] != 2.0 || b1["maxGap"] != 5.0 || b1["capProcessId"] != "" || len(b1["capProcessIds"].([]any)) != 0 {
		// 带上原值停住。
		t.Fatalf("band1 %v", b1)
	}
	// 取出套完的点列。
	pts, _ := got["points"].([]any)
	// 十个点一个不能少。
	if len(pts) != 10 {
		// 点数不对就中止。
		t.Fatalf("points %d", len(pts))
	}
	// 逐个点核对已采和没采。
	for i, el := range pts {
		// 收成对象再看。
		pt, _ := el.(map[string]any)
		// 原文里的这个点。
		sp := src[0]["points"].([]any)[i].(map[string]any)
		// 身份和类型原样留下。
		if pt["id"] != sp["id"] || pt["type"] != sp["type"] {
			// 带上原值停住。
			t.Fatalf("point %d %v", i, pt)
		}
		// 每个可空槽：原文没有就必须留空，原文有就必须还在。
		for _, k := range optionalPointKeys {
			// 原文里有没有这个槽。
			_, had := sp[k]
			// 原文没有却补出了值，示教器会当成已采。
			if !had && pt[k] != nil {
				// 带上补出的值停住。
				t.Fatalf("point %d %s stuffed: %v", i, k, pt[k])
			}
			// 原文有却丢了。
			if had && pt[k] == nil {
				// 带上点停住。
				t.Fatalf("point %d %s lost: %v", i, k, pt)
			}
		}
	}
	// 已采点的位姿数值原样留下，外部轴也在。
	pose := pts[1].(map[string]any)["pose"].(map[string]any)
	// 数值对不上就中止。
	if pose["x"] != 820.0 || pose["z"] != 402.5 || pose["ext1"] != 15.5 {
		// 带上原值停住。
		t.Fatalf("pose %v", pose)
	}
	// 拍摄位和拍摄关节角是线激光重采用的，必须留下。
	scan := pts[1].(map[string]any)["scanPose"].(map[string]any)
	// 拍摄关节角六个都在。
	joints, _ := pts[1].(map[string]any)["scanJoints"].([]any)
	// 拍摄位或关节角丢了就中止。
	if scan["z"] != 480.0 || len(joints) != 6 || joints[1] != -25.0 {
		// 带上原值停住。
		t.Fatalf("scan %v %v", scan, joints)
	}
}

// 套过一遍的正文再套一遍不变，空槽不会第二遍被补出来。
func TestTBarPadContentIdempotent(t *testing.T) {
	// 第一遍套用。
	first, err := ApplyProjectItems(SeedProjectItems(), WeldTBar, []byte(padTBarJSON))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拿第一遍的结果再套一遍。
	second, err := ApplyProjectItems(SeedProjectItems(), WeldTBar, first)
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 两遍结果必须逐字节一样。
	if string(first) != string(second) {
		// 带上两遍结果停住。
		t.Fatalf("not idempotent:\n%s\n%s", first, second)
	}
}

// 不认识的示教方式按四点收，不把乱值写进正文。
func TestTBarTeachModeEnum(t *testing.T) {
	// 套一条示教方式写错的 T 排。
	got := applyOne(t, `[{"kind":"tbar","templateId":"`+SeedTplTBar+`","teachMode":"FOO"}]`)
	// 乱值要退回四点。
	if got["teachMode"] != TeachFourPoint {
		// 带上原值停住。
		t.Fatalf("teachMode %v", got["teachMode"])
	}
	// 缺省时也是四点，和示教器一致。
	got = applyOne(t, `[{"kind":"tbar","templateId":"`+SeedTplTBar+`"}]`)
	// 缺省没补成四点就中止。
	if got["teachMode"] != TeachFourPoint || got["plateThickness"] != 0.0 || got["laserWeldLengthMm"] != 0.0 {
		// 带上原值停住。
		t.Fatalf("defaults %v", got)
	}
}

// 单层和多层共用点字段：没采的点和多层参考点同样保持空，已采的原样留下。
func TestSingleMultiUncapturedStayNull(t *testing.T) {
	// 单层一条：一个点已采，一个点没采。
	single := applyOne(t, `[{"kind":"single","templateId":"`+SeedTplSingle+`","points":[{"id":"a","type":"START","pose":{"x":1.0,"y":2.0,"z":3.0,"rx":0.0,"ry":0.0,"rz":0.0},"jointAngles":[1,2,3,4,5,6],"refPointX":{"pose":{"x":9.0,"y":0.0,"z":0.0,"rx":0.0,"ry":0.0,"rz":0.0},"jointAngles":[1,2,3,4,5,6]}},{"id":"b","type":"END"}]}]`)
	// 取出单层点列。
	pts := single["points"].([]any)
	// 已采点的位姿和参考点。
	a := pts[0].(map[string]any)
	// 没采的那个点。
	b := pts[1].(map[string]any)
	// 已采的丢了就中止。
	if a["pose"] == nil || a["refPointX"] == nil || a["jointAngles"] == nil {
		// 带上原值停住。
		t.Fatalf("single captured lost %v", a)
	}
	// 没采的点每个可空槽都要是空。
	for _, k := range optionalPointKeys {
		// 补出值就中止。
		if b[k] != nil {
			// 带上补出的值停住。
			t.Fatalf("single %s stuffed %v", k, b[k])
		}
	}
	// 多层一条：只采了 X1 参考，基准层一个点没采。
	multi := applyOne(t, `[{"kind":"multi","templateId":"`+SeedTplMulti+`","basePath":{"id":"bp","points":[{"id":"c","type":"START"}]},"refPointX1":{"pose":{"x":5.0,"y":0.0,"z":0.0,"rx":0.0,"ry":0.0,"rz":0.0},"jointAngles":[1,2,3,4,5,6]},"refPointZ1":null}]`)
	// X1 已采要留下。
	if multi["refPointX1"] == nil {
		// 丢了就中止。
		t.Fatalf("refPointX1 lost %v", multi)
	}
	// 其余参考点没采，显式空和缺键都要保持空。
	for _, k := range []string{"refPointZ1", "refPointXMiddle", "refPointZMiddle", "refPointXEnd", "refPointZEnd"} {
		// 补出参考点就中止。
		if multi[k] != nil {
			// 带上补出的值停住。
			t.Fatalf("multi %s stuffed %v", k, multi[k])
		}
	}
	// 基准层的点也走同一套点字段。
	c := multi["basePath"].(map[string]any)["points"].([]any)[0].(map[string]any)
	// 没采的基准点不能补位姿。
	if c["pose"] != nil || c["refPointX"] != nil {
		// 带上补出的值停住。
		t.Fatalf("base point stuffed %v", c)
	}
}

// 工艺槽不能可空；可空的数字缺键不补、显式空留空、有值照收。
func TestNullableRules(t *testing.T) {
	// 一个标可空的工艺槽。
	bad := Schema{Root: RootObject, Fields: []Field{{Key: "a", Label: "甲", Type: TypeProcess, Default: "", Nullable: true}}}
	// 校验必须拒绝。
	if err := Validate(bad); err == nil {
		// 放行就中止。
		t.Fatal("nullable process accepted")
	}
	// 一个可空的数字字段。
	ok := Schema{Root: RootObject, Fields: []Field{optional(num("n", "数", "mm", 0))}}
	// 编成规范字段表，编不出就是规则写错。
	raw, err := Marshal(ok)
	// 可空数字必须能过校验。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 缺键、显式空、有值三种正文逐个套。
	for body, want := range map[string]string{`{}`: `{}`, `{"n":null}`: `{"n":null}`, `{"n":1.5}`: `{"n":1.5}`} {
		// 按字段表套正文。
		out, err := Apply(raw, []byte(body))
		// 套错就中止。
		if err != nil || string(out) != want {
			// 带上两边停住。
			t.Fatalf("%s => %s %v, want %s", body, out, err, want)
		}
	}
	// 当前三份种子都要能过校验。
	for _, it := range SeedProjectItems() {
		// 编成规范字段表，编不出就是种子写错。
		if _, err := Marshal(ObjectSchema(it.Fields)); err != nil {
			// 带上种子名停住。
			t.Fatalf("%s: %v", it.Name, err)
		}
	}
}

// stripToRetired 去掉这次新加的键、可空标记和元素键，还原成上一版内置种子的字段表。
func stripToRetired(fields []Field) []Field {
	// 这次新加的键，上一版没有。
	added := map[string]bool{"scanPose": true, "scanJoints": true, "plateThickness": true, "grooveAngleDeg": true, "teachMode": true,
		"laserLowerWidthMm": true, "laserUpperWidthMm": true, "extendUpMm": true, "extendDownMm": true, "laserWeldLengthMm": true,
		"extendLengthMm": true, "cornerGroupParams": true}
	// 准备放下还原后的字段。
	out := make([]Field, 0, len(fields))
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
			f.Fields = stripToRetired(f.Fields)
		}
		// 数组元素一并还原。
		if f.Items != nil {
			// 元素只有一个，包一层再还原。
			it := stripToRetired([]Field{*f.Items})[0]
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

// 上一版三份种子原样时能认出来并换成当前种子；当前种子和改过的认不出。
func TestUpgradeSeed(t *testing.T) {
	// 逐份种子核对，单层再加不带附加槽的那一份。
	cases := []struct {
		id     string  // 种子身份
		fields []Field // 当前代码的字段表
	}{
		{SeedTplSingle, itemSingle(true)},
		{SeedTplSingle, itemSingle(false)},
		{SeedTplMulti, itemMulti()},
		{SeedTplTBar, itemTBar()},
	}
	// 逐份核对升级前后。
	for _, c := range cases {
		// 还原出上一版的规范字段表。
		old, err := Marshal(ObjectSchema(stripToRetired(c.fields)))
		// 编不出就是还原写错了。
		if err != nil {
			// 失败就中止本用例。
			t.Fatal(err)
		}
		// 上一版的摘要。
		sum := sha256.Sum256(old)
		// 拿上一版去认。
		got, ok := UpgradeSeed(c.id, sum[:])
		// 认不出上一版就中止，说明摘要表漏了这一版。
		if !ok {
			// 带上身份停住。
			t.Fatalf("%s retired not recognized", c.id)
		}
		// 交回的字段表编成规范字节。
		want, _ := Marshal(ObjectSchema(c.fields))
		// 交回的编成规范字节。
		have, _ := Marshal(ObjectSchema(got))
		// 换成的不是当前种子就中止。
		if string(want) != string(have) {
			// 带上身份停住。
			t.Fatalf("%s upgraded to wrong fields", c.id)
		}
		// 当前种子的摘要。
		cur := sha256.Sum256(want)
		// 当前种子不能再被当成旧版。
		if _, ok := UpgradeSeed(c.id, cur[:]); ok {
			// 带上身份停住。
			t.Fatalf("%s current treated as retired", c.id)
		}
		// 换个身份就不能认，自建模版不碰。
		if _, ok := UpgradeSeed("00000000-0000-4000-8000-000000000009", sum[:]); ok {
			// 带上身份停住。
			t.Fatalf("custom id upgraded")
		}
	}
}

// padSingleJSON 示教器 SingleLayerProject.encode 存下的单层：带延长长度、包角组（逐层几何、枪姿只写了 Rx）和附加工艺；第二条没包角。
const padSingleJSON = `[{"id":"s1","name":"焊道 1","points":[` +
	`{"id":"a","type":"START","pose":{"x":1.0,"y":2.0,"z":3.0,"rx":0.0,"ry":0.0,"rz":0.0},"jointAngles":[1.0,2.0,3.0,4.0,5.0,6.0]},{"id":"b","type":"END"}],` +
	`"processId":"` + padRootID + `","selectedPointIndex":1,"extendLengthMm":35.5,` +
	`"cornerGroupParams":{"groupId":"g1","refPathAId":"ra","refPathBId":"rb","layerCount":3,"initialLength":40.0,"upwardOffset":6.0,"lengthReduction":5.0,` +
	`"firstPassLength":42.5,"isMaster":true,"torchRx":12.0,"processId":"` + padCapID + `",` +
	`"passes":[{"length":40.0,"flatOffset":3.0,"upwardOffset":6.0},{"length":35.0,"flatOffset":2.5,"upwardOffset":5.5}]},` +
	`"extraProcesses":[{"id":"x1","processId":"` + padRootID + `","isEnabled":false}]},` +
	`{"id":"s2","name":"焊道 2","isEnabled":false}]`

// 单层按示教器真实形状过模版：延长长度、包角组和附加工艺一个不丢；枪姿没写的不补零，没包角的不补出空组。
func TestSinglePadContentKept(t *testing.T) {
	// 按空库三份套正文，和 normalizeContent 走同一个入口。
	out, err := ApplyProjectItems(SeedProjectItems(), WeldSingle, []byte(strings.ReplaceAll(padSingleJSON, `{"id":"s`, `{"templateId":"`+SeedTplSingle+`","id":"s`)))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 示教器单层正文不带 templateId，按旧种类表套用同样要留下。
	legacy, err := Marshal(Default(KindProject))
	// 编不出就中止。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 不带模版身份的原文按旧登记簿套。
	out2, err := Apply(legacy, []byte(padSingleJSON))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接原文，逐键对照。
	var src []map[string]any
	// 原文拆不开就是用例本身写错了。
	if err := json.Unmarshal([]byte(padSingleJSON), &src); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 两条入口的结果都要核对。
	for _, body := range [][]byte{out, out2} {
		// 准备接套完的焊道。
		var got []map[string]any
		// 拆不开或条数不对就中止。
		if err := json.Unmarshal(body, &got); err != nil || len(got) != 2 {
			// 带上原值停住。
			t.Fatalf("items %s %v", body, err)
		}
		// 延长长度原样留下。
		if got[0]["extendLengthMm"] != 35.5 || got[0]["processId"] != padRootID || got[0]["selectedPointIndex"] != 1.0 {
			// 带上原值停住。
			t.Fatalf("path %v", got[0])
		}
		// 包角组整组和原文逐键一样，没写的枪姿 Ry/Rz 不补。
		if !reflect.DeepEqual(got[0]["cornerGroupParams"], src[0]["cornerGroupParams"]) {
			// 带上两边停住。
			t.Fatalf("corner\n got %v\nwant %v", got[0]["cornerGroupParams"], src[0]["cornerGroupParams"])
		}
		// 附加工艺原样留下。
		if !reflect.DeepEqual(got[0]["extraProcesses"], src[0]["extraProcesses"]) {
			// 带上两边停住。
			t.Fatalf("extra %v", got[0]["extraProcesses"])
		}
		// 没包角的焊道不能补出包角组，延长长度按示教器缺省补零。
		if _, has := got[1]["cornerGroupParams"]; has || got[1]["extendLengthMm"] != 0.0 || got[1]["isEnabled"] != false {
			// 带上原值停住。
			t.Fatalf("plain path %v", got[1])
		}
		// 没采的点不补位姿。
		if pt := got[0]["points"].([]any)[1].(map[string]any); pt["pose"] != nil || pt["jointAngles"] != nil {
			// 带上原值停住。
			t.Fatalf("uncaptured %v", pt)
		}
	}
}
