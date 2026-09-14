// Package contenttpl 按云端当前模版套工艺/工程 JSON：缺的补默认，多的删掉。
// 不管归属、修订、下发；不解析成行业等级。
package contenttpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

const (
	KindProcess = "process" // 工艺
	KindProject = "project" // 工程

	RootObject = "object" // 根是对象
	RootArray  = "array"  // 根是数组

	TypeString = "string" // 文本；有 options 当枚举
	TypeNumber = "number" // 旧数字；套用时仍收
	TypeBool   = "bool"   // 旧布尔；套用时仍收
	TypeObject = "object" // 对象
	TypeArray  = "array"  // 数组
)

const maxDepth = 8    // 嵌套上限
const maxFields = 200 // 单层字段个数上限

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`) // 字段键只许字母数字下划线

// Field 是模版里的一个字段。
type Field struct {
	Key      string   `json:"key"`                // JSON 键
	Label    string   `json:"label"`              // 给人看的名字
	Type     string   `json:"type"`               // string 文本；有 options 为枚举；number、bool 为旧值
	Unit     string   `json:"unit,omitempty"`     // 单位，如 A、V、mm
	Required bool     `json:"required,omitempty"` // 表单是否必填；套用仍补默认
	Default  any      `json:"default,omitempty"`  // 缺省时写入
	Options  []string `json:"options,omitempty"`  // 枚举取值；有则按枚举套用
	Fields   []Field  `json:"fields,omitempty"`   // object 的子字段
	Items    *Field   `json:"items,omitempty"`    // array 的元素
}

// Schema 是一份类型的当前字段表。
type Schema struct {
	Root   string  `json:"root"`             // object / array
	Fields []Field `json:"fields,omitempty"` // 根为 object
	Item   *Field  `json:"item,omitempty"`   // 根为 array 时的元素
}

// Marshal 把字段表写成规范 JSON，供摘要和落库。
func Marshal(s Schema) ([]byte, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// Parse 读字段表。
func Parse(raw []byte) (Schema, error) {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return Schema{}, fmt.Errorf("content template is invalid")
	}
	if err := Validate(s); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// Validate 检查字段表能用来套用。
func Validate(s Schema) error {
	switch s.Root {
	case RootObject:
		if s.Item != nil {
			return fmt.Errorf("content template is invalid")
		}
		return validateFields(s.Fields, 1)
	case RootArray:
		if s.Item == nil {
			return fmt.Errorf("content template is invalid")
		}
		return validateField(*s.Item, 1)
	default:
		return fmt.Errorf("content template is invalid")
	}
}

// validateFields 键不重复、深度和个数有上限。
func validateFields(fields []Field, depth int) error {
	if depth > maxDepth || len(fields) > maxFields {
		return fmt.Errorf("content template is invalid")
	}
	seen := map[string]struct{}{}
	for _, f := range fields {
		if _, ok := seen[f.Key]; ok || !keyRe.MatchString(f.Key) {
			return fmt.Errorf("content template is invalid")
		}
		seen[f.Key] = struct{}{}
		if err := validateField(f, depth); err != nil {
			return err
		}
	}
	return nil
}

// validateField 类型与子结构必须匹配。
func validateField(f Field, depth int) error {
	if f.Label == "" {
		return fmt.Errorf("content template is invalid")
	}
	switch f.Type {
	case TypeString, TypeNumber, TypeBool:
		if len(f.Fields) > 0 || f.Items != nil {
			return fmt.Errorf("content template is invalid")
		}
		return nil
	case TypeObject:
		if f.Items != nil {
			return fmt.Errorf("content template is invalid")
		}
		return validateFields(f.Fields, depth+1)
	case TypeArray:
		if f.Items == nil || len(f.Fields) > 0 {
			return fmt.Errorf("content template is invalid")
		}
		return validateField(*f.Items, depth+1)
	default:
		return fmt.Errorf("content template is invalid")
	}
}

// Apply 按模版套正文：缺的补、多的删。schema 必须是规范字段表 JSON。
func Apply(schemaJSON, content []byte) ([]byte, error) {
	s, err := Parse(schemaJSON)
	if err != nil {
		return nil, err
	}
	var v any
	trim := bytes.TrimSpace(content)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &v); err != nil {
			v = nil
		}
	}
	var out any
	switch s.Root {
	case RootObject:
		out = applyObject(s.Fields, v)
	case RootArray:
		out = applyArray(*s.Item, v)
	}
	return json.Marshal(out)
}

// applyObject 只保留模版里的键，缺的补默认。
func applyObject(fields []Field, v any) map[string]any {
	src, _ := v.(map[string]any)
	if src == nil {
		src = map[string]any{}
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f.Key] = applyField(f, src[f.Key])
	}
	return out
}

// applyArray 按元素模版逐项套用。
func applyArray(item Field, v any) []any {
	src, ok := v.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, len(src))
	for i, el := range src {
		out[i] = applyField(item, el)
	}
	return out
}

// applyField 按类型套单值。
func applyField(f Field, v any) any {
	switch f.Type {
	case TypeString:
		if len(f.Options) > 0 {
			return applyEnum(f, v)
		}
		return applyText(v, f.Default)
	case TypeNumber:
		if n, ok := asFloat(v); ok {
			return n
		}
		return asFloatDef(f.Default)
	case TypeBool:
		if b, ok := asBool(v); ok {
			return b
		}
		return asBoolDef(f.Default)
	case TypeObject:
		return applyObject(f.Fields, v)
	case TypeArray:
		if f.Items == nil {
			return []any{}
		}
		return applyArray(*f.Items, v)
	default:
		return nil
	}
}

// applyText 文本：能当数字的保持数字，其余当字符串。
func applyText(v, def any) any {
	if v == nil {
		v = def
	}
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		n, err := x.Float64()
		if err == nil {
			return n
		}
		return x.String()
	case string:
		if n, ok := parsePlainNumber(x); ok {
			return n
		}
		return x
	case bool:
		return strconv.FormatBool(x)
	default:
		if def != nil && v != def {
			return applyText(def, nil)
		}
		return ""
	}
}

// parsePlainNumber 纯数字串收成数字，前导零不当数字。
func parsePlainNumber(s string) (float64, bool) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	lead := s
	if len(lead) > 0 && (lead[0] == '+' || lead[0] == '-') {
		lead = lead[1:]
	}
	if len(lead) > 1 && lead[0] == '0' && lead[1] != '.' {
		return 0, false
	}
	return n, true
}

// applyEnum 取值必须在选项里，对不上则用默认；是/否兼容旧布尔。
func applyEnum(f Field, v any) any {
	if v == nil {
		return enumDefault(f)
	}
	for _, c := range enumCandidates(v) {
		for _, o := range f.Options {
			if o == c {
				return o
			}
		}
	}
	return enumDefault(f)
}

// enumDefault 默认不在选项里就用第一项。
func enumDefault(f Field) string {
	s := asStringDef(f.Default)
	for _, o := range f.Options {
		if o == s {
			return s
		}
	}
	if len(f.Options) == 0 {
		return s
	}
	return f.Options[0]
}

// enumCandidates 把旧布尔/数字也收成可匹配的选项。
func enumCandidates(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case bool:
		if x {
			return []string{"是", "true"}
		}
		return []string{"否", "false"}
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	default:
		if s, ok := asString(v); ok {
			return []string{s}
		}
		return nil
	}
}

// asString 能当文本就收成字符串。
func asString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	default:
		return "", false
	}
}

// asStringDef 收不成文本则空串。
func asStringDef(v any) string {
	if s, ok := asString(v); ok {
		return s
	}
	return ""
}

// asFloat 能当数字就收成浮点。
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		n, err := x.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(x, 64)
		return n, err == nil
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}

// asFloatDef 收不成数字则 0。
func asFloatDef(v any) float64 {
	if n, ok := asFloat(v); ok {
		return n
	}
	return 0
}

// asBool 能当布尔就收下。
func asBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(x)
		return b, err == nil
	default:
		return false, false
	}
}

// asBoolDef 收不成布尔则否。
func asBoolDef(v any) bool {
	if b, ok := asBool(v); ok {
		return b
	}
	return false
}

// Default 工艺按设备明文；工程按身份引用，不收路径。
func Default(kind string) Schema {
	if kind == KindProject {
		return defaultProject()
	}
	return defaultProcess()
}

// num 带单位的数字字段，套用时当文本收。
func num(key, label, unit string, def float64) Field {
	return Field{Key: key, Label: label, Type: TypeString, Unit: unit, Default: def}
}

// str 普通文本字段。
func str(key, label, def string) Field {
	return Field{Key: key, Label: label, Type: TypeString, Default: def}
}

// flag 已完成/启用这类是/否，落成可改选项的枚举。
func flag(key, label string, on bool) Field {
	def := "否"
	if on {
		def = "是"
	}
	return Field{Key: key, Label: label, Type: TypeString, Options: []string{"否", "是"}, Default: def}
}

// poseFields 位姿六个轴加外部轴。
func poseFields() []Field {
	return []Field{
		num("x", "X", "mm", 0), num("y", "Y", "mm", 0), num("z", "Z", "mm", 0),
		num("rx", "Rx", "°", 0), num("ry", "Ry", "°", 0), num("rz", "Rz", "°", 0),
		num("ext1", "外部轴", "mm", 0),
	}
}

// pointField 一个焊点：身份、类型、位姿、关节角。
func pointField() Field {
	ja := Field{Key: "jointAngles", Label: "关节角", Type: TypeArray, Items: &Field{Key: "a", Label: "角", Type: TypeString, Default: 0.0}}
	return Field{Type: TypeObject, Label: "点", Fields: []Field{
		str("id", "点身份", ""),
		str("type", "点类型", "START"),
		{Key: "pose", Label: "位姿", Type: TypeObject, Fields: poseFields()},
		ja,
	}}
}

// pathFields 一条路径：身份、点列、工艺引用。
func pathFields() []Field {
	el := pointField()
	return []Field{
		str("id", "路径身份", ""),
		str("name", "路径名", ""),
		{Key: "points", Label: "点", Type: TypeArray, Items: &el},
		str("processId", "工艺", ""),
		num("selectedPointIndex", "选中点", "", 0),
	}
}

// cornerFields 包角几何加工艺引用，不嵌工艺参数。
func cornerFields() []Field {
	return []Field{
		str("groupId", "包角组", ""),
		str("refPathAId", "参考路径 A", ""),
		str("refPathBId", "参考路径 B", ""),
		num("layerCount", "层数", "", 0),
		num("initialLength", "初始长", "mm", 0),
		num("upwardOffset", "上偏", "mm", 0),
		num("lengthReduction", "长度递减", "mm", 0),
		flag("isMaster", "主焊道", false),
		num("torchRx", "焊枪 Rx", "°", 0),
		num("torchRy", "焊枪 Ry", "°", 0),
		num("torchRz", "焊枪 Rz", "°", 0),
		str("processId", "工艺", ""),
	}
}

// defaultProcess 设备侧已有的工艺字段表。
func defaultProcess() Schema {
	osc := Field{Key: "oscillation", Label: "摆动", Type: TypeObject, Fields: []Field{
		{Key: "type", Label: "摆动类型", Type: TypeString, Options: []string{"正弦波摆动"}, Default: "正弦波摆动"},
		{Key: "positionWait", Label: "等待", Type: TypeString, Options: []string{"等待时间内位置静止"}, Default: "等待时间内位置静止"},
		num("frequency", "频率", "Hz", 2),
		num("amplitude", "振幅", "mm", 4),
		num("leftStopTime", "左停", "ms", 300),
		num("rightStopTime", "右停", "ms", 300),
		num("leftSideLength", "左侧长", "mm", 2),
		num("rightSideLength", "右侧长", "mm", 2),
		num("zeroTime", "过零时间", "ms", 10),
		num("callbackRatio", "回摆比", "%", 5),
		num("azimuth", "方位角", "°", 0),
		num("inclination", "倾角", "°", 0),
	}}
	return Schema{Root: RootObject, Fields: []Field{
		str("name", "名称", ""),
		num("offsetX", "焊枪偏移 X", "mm", 0),
		num("offsetY", "焊枪偏移 Y", "mm", 0),
		num("offsetZ", "焊枪偏移 Z", "mm", 0),
		num("current", "电流", "A", 200),
		num("voltage", "电压", "V", 24),
		num("speed", "速度", "mm/s", 6),
		num("startArcTime", "起弧时间", "ms", 200),
		num("startArcCurrent", "起弧电流", "A", 175),
		num("startArcVoltage", "起弧电压", "V", 20.5),
		num("endArcTime", "收弧时间", "ms", 800),
		num("endArcCurrent", "收弧电流", "A", 200),
		num("endArcVoltage", "收弧电压", "V", 22),
		osc,
	}}
}

// defaultProject 工程根是焊缝数组；工艺只引用身份。
func defaultProject() Schema {
	pass := Field{Type: TypeObject, Label: "焊道", Fields: []Field{
		str("id", "焊道身份", ""),
		str("name", "焊道名", ""),
		num("valX", "X", "mm", 0),
		num("valYLeft", "左 Y", "mm", 0),
		num("valYRight", "右 Y", "mm", 0),
		num("valZ", "Z", "mm", 0),
		num("valR", "R", "mm", 0),
		str("processId", "工艺", ""),
		flag("isCompleted", "已完成", false),
		flag("isEnabled", "启用", true),
	}}
	ref := Field{Type: TypeObject, Label: "参考点", Fields: []Field{
		{Key: "pose", Label: "位姿", Type: TypeObject, Fields: poseFields()},
		{Key: "jointAngles", Label: "关节角", Type: TypeArray, Items: &Field{Key: "a", Label: "角", Type: TypeString, Default: 0.0}},
	}}
	base := Field{Key: "basePath", Label: "基准路径", Type: TypeObject, Fields: pathFields()}
	item := Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		{Key: "points", Label: "点", Type: TypeArray, Items: ptr(pointField())},
		str("processId", "工艺", ""),
		num("selectedPointIndex", "选中点", "", 0),
		{Key: "extraProcesses", Label: "附加工艺", Type: TypeArray, Items: &Field{Type: TypeObject, Label: "附加", Fields: []Field{
			str("id", "身份", ""), str("processId", "工艺", ""),
		}}},
		flag("isEnabled", "启用", true),
		base,
		{Key: "passes", Label: "多层焊道", Type: TypeArray, Items: &pass},
		{Key: "cornerGroupParams", Label: "包角", Type: TypeObject, Fields: cornerFields()},
		{Key: "refPointX1", Label: "起点 X", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointZ1", Label: "起点 Z", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointXEnd", Label: "终点 X", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointZEnd", Label: "终点 Z", Type: TypeObject, Fields: ref.Fields},
		flag("isBaseCompleted", "基准完成", false),
	}}
	return Schema{Root: RootArray, Item: &item}
}

// ptr 给数组元素取地址。
func ptr(f Field) *Field { return &f }
