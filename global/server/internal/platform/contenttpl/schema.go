// Package contenttpl 按云端当前模版套工艺/工程 JSON：缺的补默认，多的删掉。
// 工程模版登记若干命名项；每种内部字段在代码目录。不管归属、修订、下发。
package contenttpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	KindProcess = "process" // 工艺
	KindProject = "project" // 工程

	RootObject = "object" // 根是对象
	RootArray  = "array"  // 根是数组

	TypeString  = "string"  // 文本；有 options 当枚举
	TypeNumber  = "number"  // 旧数字；套用时仍收
	TypeBool    = "bool"    // 启用/完成；套用时收布尔，兼容是/否
	TypeObject  = "object"  // 对象
	TypeArray   = "array"   // 数组
	TypeProcess = "process" // 工艺引用，值为身份字符串
)

const maxDepth = 8    // 嵌套上限
const maxFields = 200 // 单层字段个数上限

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`) // 字段键只许字母数字下划线

// Field 是模版里的一个字段。
type Field struct {
	Key      string   `json:"key"`                // JSON 键
	Label    string   `json:"label"`              // 给人看的名字
	Type     string   `json:"type"`               // string 文本；process 工艺引用；有 options 为枚举；bool 启用类；number 为旧数字
	Unit     string   `json:"unit,omitempty"`     // 单位，如 A、V、mm
	Required bool     `json:"required,omitempty"` // 表单是否必填；套用仍补默认
	Default  any      `json:"default,omitempty"`  // 缺省时写入
	Options  []string `json:"options,omitempty"`  // 枚举取值；有则按枚举套用
	Fields   []Field  `json:"fields,omitempty"`   // object 的子字段
	Items    *Field   `json:"items,omitempty"`    // array 的元素
}

// Schema 是一份类型的当前模版：工艺为字段表，工程为若干命名项。
type Schema struct {
	Root      string            `json:"root"`                // object / array
	Fields    []Field           `json:"fields,omitempty"`    // 工艺：根对象字段
	Item      *Field            `json:"item,omitempty"`      // 旧工程表：一条焊缝的字段
	Kinds     []string          `json:"kinds,omitempty"`     // 旧工程：种类表；与 Templates 互斥
	Templates []ProjectTemplate `json:"templates,omitempty"` // 工程：命名模版
}

// Marshal 把字段表写成规范 JSON，供摘要和落库。
func Marshal(s Schema) ([]byte, error) {
	// 模版不合法就不能落库或套用。
	if err := Validate(s); err != nil {
		return nil, err
	}
	// 收成规范正文，供摘要和落库。
	return json.Marshal(s)
}

// Parse 读字段表。
func Parse(raw []byte) (Schema, error) {
	// 准备接拆开的字段表。
	var s Schema
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(raw, &s); err != nil {
		// 字段表坏了就拒绝。
		return Schema{}, fmt.Errorf("content template is invalid")
	}
	// 模版不合法就不能落库或套用。
	if err := Validate(s); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// Validate 检查模版能用来套用。
func Validate(s Schema) error {
	// 按根类型决定怎么走正文。
	switch s.Root {
	// 对象根按字段表走。
	case RootObject:
		// 对象根再带数组登记就混了。
		if s.Item != nil || len(s.Kinds) > 0 || len(s.Templates) > 0 {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 对象根去查字段表本身。
		return validateFields(s.Fields, 1)
	// 数组根按登记方式往下走。
	case RootArray:
		// 有命名模版就不再看旧种类和元素。
		if len(s.Templates) > 0 {
			// 命名模版不能再混旧登记。
			if s.Item != nil || len(s.Fields) > 0 || len(s.Kinds) > 0 {
				// 这一种组合不能用来套用。
				return fmt.Errorf("content template is invalid")
			}
			// 去查命名模版的名称和身份。
			return validateTemplates(s.Templates)
		}
		// 没有命名模版才看旧种类表。
		if len(s.Kinds) > 0 {
			// 旧种类表不能再带元素或字段。
			if s.Item != nil || len(s.Fields) > 0 {
				// 这一种组合不能用来套用。
				return fmt.Errorf("content template is invalid")
			}
			// 去查旧种类是否重复、有没有根项。
			return validateKinds(s.Kinds)
		}
		// 再没有种类表，才允许旧的单元素。
		if s.Item != nil {
			// 单元素不能再带根字段。
			if len(s.Fields) > 0 {
				// 这一种组合不能用来套用。
				return fmt.Errorf("content template is invalid")
			}
			// 去查这一条元素的类型。
			return validateField(*s.Item, 1)
		}
		// 单元素不能再带根字段。
		if len(s.Fields) > 0 {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 去查命名模版的名称和身份。
		return validateTemplates(s.Templates)
	// 认不出的根或类型直接拒绝。
	default:
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
}

// validateFields 键不重复、深度和个数有上限。
func validateFields(fields []Field, depth int) error {
	// 嵌套或个数超过上限就拒绝。
	if depth > maxDepth || len(fields) > maxFields {
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 并集里逐个字段看键。
	for _, f := range fields {
		// 键重复或形态不对就拒绝。
		if _, ok := seen[f.Key]; ok || !keyRe.MatchString(f.Key) {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 记下这个键，防止后面重复。
		seen[f.Key] = struct{}{}
		// 子结构不合法就整份拒绝。
		if err := validateField(f, depth); err != nil {
			return err
		}
	}
	return nil
}

// validateField 类型与子结构必须匹配。
func validateField(f Field, depth int) error {
	// 没有给人看的名字就不能用。
	if f.Label == "" {
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
	// 按字段类型查子结构是否匹配。
	switch f.Type {
	// 叶子类型不能再带子字段或元素。
	case TypeString, TypeNumber, TypeBool, TypeProcess:
		// 叶子再套结构，或工艺引用带选项，都不合法。
		if len(f.Fields) > 0 || f.Items != nil || (f.Type == TypeProcess && len(f.Options) > 0) {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		return nil
	// 对象再走进子字段。
	case TypeObject:
		// 数组元素的键也要算。
		if f.Items != nil {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 子字段再查一层深度。
		return validateFields(f.Fields, depth+1)
	// 数组再走进里面的元素。
	case TypeArray:
		// 数组缺元素或又写了子字段就不合法。
		if f.Items == nil || len(f.Fields) > 0 {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 元素再按字段查一层。
		return validateField(*f.Items, depth+1)
	// 认不出的根或类型直接拒绝。
	default:
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
}

// Apply 按模版套正文：缺的补、多的删。schema 必须是规范字段表 JSON。
func Apply(schemaJSON, content []byte) ([]byte, error) {
	// 先读成字段表，不合法就不套。
	s, err := Parse(schemaJSON)
	// 模版读不成就不能套。
	if err != nil {
		return nil, err
	}
	// 准备接拆开的正文。
	var v any
	// 先去掉空白，空正文当没有。
	trim := bytes.TrimSpace(content)
	// 有正文才尝试拆开。
	if len(trim) > 0 {
		// 拆不开就不是套用后的正文。
		if err := json.Unmarshal(trim, &v); err != nil {
			// 非法正文退回空，好补缺省。
			v = nil
		}
	}
	// 准备接套完的结果。
	var out any
	// 按根类型决定怎么走正文。
	switch s.Root {
	// 对象根按字段表走。
	case RootObject:
		// 对象根按字段表套。
		out = applyObject(s.Fields, v)
	// 数组根按登记方式往下走。
	case RootArray:
		// 命名模版或空登记走命名份。
		if len(s.Templates) > 0 || (s.Item == nil && len(s.Kinds) == 0) {
			// 按命名模版套每一份。
			out = applyProjectTemplates(s, v)
			// 旧种类表只套选中的种类。
		} else if len(s.Kinds) > 0 {
			// 按旧种类表套每一份。
			out = applyProjectKinds(s, v)
			// 剩下的按单元素数组套。
		} else {
			// 每个元素按同一份字段套。
			out = applyArray(*s.Item, v)
		}
	}
	// 套完的结果再收成正文。
	return json.Marshal(out)
}

// ApplyProjectItems 工程正文是数组；按 templateId 套那一份对象字段。
func ApplyProjectItems(items []ProjectItemSchema, content []byte) ([]byte, error) {
	// 按模版身份记下各自的字段。
	byID := map[string][]Field{}
	// 每一份模版登记自己的字段。
	for _, it := range items {
		// 这份身份对上自己的字段表。
		byID[it.ID] = it.Fields
	}
	// 准备接拆开的正文。
	var v any
	// 先去掉空白，空正文当没有。
	trim := bytes.TrimSpace(content)
	// 有正文才尝试拆开。
	if len(trim) > 0 {
		// 拆不开就不是套用后的正文。
		if err := json.Unmarshal(trim, &v); err != nil {
			// 非法正文退回空，好补缺省。
			v = nil
		}
	}
	// 工程正文必须是数组。
	src, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		// 对不上就交回空数组。
		return json.Marshal([]any{})
	}
	// 按原份数预留结果。
	out := make([]any, 0, len(src))
	// 每一份对上模版才留下。
	for _, el := range src {
		// 这一份要是对象才套。
		obj, _ := el.(map[string]any)
		// 不是对象就丢掉这一份。
		if obj == nil {
			continue
		}
		// 用模版身份找这一份的字段。
		id, _ := obj["templateId"].(string)
		// 对上身份就用那一份字段。
		fields, ok := byID[id]
		// 不是正文就当没有引用。
		if !ok {
			continue
		}
		// 只留这份字段表里的键。
		applied := applyObject(fields, obj)
		// 套完把模版身份写回去。
		applied["templateId"] = id
		// 收下套完的这一份。
		out = append(out, applied)
	}
	// 套完的结果再收成正文。
	return json.Marshal(out)
}

// applyObject 只保留模版里的键，缺的补默认；正文里的空对象/数组原样留下。
func applyObject(fields []Field, v any) map[string]any {
	// 正文不是对象就当空对象。
	src, _ := v.(map[string]any)
	// 没有对象就从空表开始补缺省。
	if src == nil {
		// 空表上补模版要求的键。
		src = map[string]any{}
	}
	// 只给模版里的键留位置。
	out := make(map[string]any, len(fields))
	// 并集里逐个字段看键。
	for _, f := range fields {
		// 看正文里有没有这个键。
		val, exists := src[f.Key]
		// 显式空对象或空数组要原样留下。
		if exists && val == nil && (f.Type == TypeObject || f.Type == TypeArray) {
			// 空值留下，不要补成缺省结构。
			out[f.Key] = nil
			continue
		}
		// 按这个字段的类型套值。
		out[f.Key] = applyField(f, val)
	}
	return out
}

// applyArray 按元素模版逐项套用。
func applyArray(item Field, v any) []any {
	// 工程正文必须是数组。
	src, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return []any{}
	}
	// 元素个数保持和正文一样。
	out := make([]any, len(src))
	// 每个元素按同一份模版套。
	for i, el := range src {
		// 套完写进对应的位置。
		out[i] = applyField(item, el)
	}
	return out
}

// applyField 按类型套单值。
func applyField(f Field, v any) any {
	// 按字段类型查子结构是否匹配。
	switch f.Type {
	// 文本有选项就当枚举。
	case TypeString:
		// 有选项就不能自由填写。
		if len(f.Options) > 0 {
			// 按选项收，对不上用默认。
			return applyEnum(f, v)
		}
		// 没有选项就按文本收。
		return applyText(v, f.Default)
	// 工艺引用只留身份字符串。
	case TypeProcess:
		// 收成身份，空表示未选。
		return applyProcessID(v, f.Default)
	// 旧数字能解析就留下，否则用默认。
	case TypeNumber:
		// 能当数字就用这个数。
		if n, ok := asFloat(v); ok {
			return n
		}
		// 解析不了就用字段默认。
		return asFloatDef(f.Default)
	// 启用类收布尔，并兼容是与否。
	case TypeBool:
		// 能当成布尔就用这个值。
		if b, ok := asBool(v); ok {
			return b
		}
		// 认不出就用字段默认。
		return asBoolDef(f.Default)
	// 对象再走进子字段。
	case TypeObject:
		// 只留子字段里的键。
		return applyObject(f.Fields, v)
	// 数组再走进里面的元素。
	case TypeArray:
		// 没有元素模版就交回空数组。
		if f.Items == nil {
			return []any{}
		}
		// 每个元素再套一层。
		return applyArray(*f.Items, v)
	// 认不出的根或类型直接拒绝。
	default:
		return nil
	}
}

// applyProcessID 只留下字符串身份，空表示未选。
func applyProcessID(v, def any) any {
	// 本身是字符串就去掉空白。
	if s, ok := v.(string); ok {
		// 去掉空白后的身份才落正文。
		return strings.TrimSpace(s)
	}
	// 空值才用默认身份。
	if s, ok := def.(string); ok && v == nil {
		// 去掉空白后的身份才落正文。
		return strings.TrimSpace(s)
	}
	// 空值没有默认就当未选。
	if v == nil {
		return ""
	}
	// 别的类型改走默认身份。
	return applyProcessID(nil, def)
}

// applyText 文本：能当数字的保持数字，其余当字符串。
func applyText(v, def any) any {
	// 空值没有默认就当未选。
	if v == nil {
		// 空值改用默认再收。
		v = def
	}
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 数字收成不带多余小数的文本。
	case float64:
		return x
	// 延迟解析的数字先试着转。
	case json.Number:
		// 延迟数字试着转成浮点。
		n, err := x.Float64()
		// 转成功就留下数字。
		if err == nil {
			return n
		}
		// 转不了就保留原来的文本。
		return x.String()
	// 字符串去掉空白当身份。
	case string:
		// 纯数字串收成数字。
		if n, ok := parsePlainNumber(x); ok {
			return n
		}
		return x
	// 旧布尔展开成是或否。
	case bool:
		// 收成文本，和旧枚举对齐。
		return strconv.FormatBool(x)
	// 认不出的根或类型直接拒绝。
	default:
		// 还有默认就再试一次默认。
		if def != nil && v != def {
			// 用默认再收，避免丢值。
			return applyText(def, nil)
		}
		return ""
	}
}

// parsePlainNumber 纯数字串收成数字，前导零不当数字。
func parsePlainNumber(s string) (float64, bool) {
	// 先按浮点数把文本解析开。
	n, err := strconv.ParseFloat(s, 64)
	// 解析失败就不当纯数字。
	if err != nil {
		return 0, false
	}
	// 准备看前导零，符号先拿开。
	lead := s
	// 正负号不参与前导零判断。
	if len(lead) > 0 && (lead[0] == '+' || lead[0] == '-') {
		// 拿掉符号再看数字本体。
		lead = lead[1:]
	}
	// 前导零不当数字，避免和文本身份混淆。
	if len(lead) > 1 && lead[0] == '0' && lead[1] != '.' {
		return 0, false
	}
	return n, true
}

// applyEnum 取值必须在选项里，对不上则用默认；是/否兼容旧布尔。
func applyEnum(f Field, v any) any {
	// 空值没有默认就当未选。
	if v == nil {
		// 空或对不上就用默认选项。
		return enumDefault(f)
	}
	// 旧布尔和数字也展开成可选项。
	for _, c := range enumCandidates(v) {
		// 逐个选项看有没有对上。
		for _, o := range f.Options {
			// 对上就用选项里的规范写法。
			if o == c {
				return o
			}
		}
	}
	// 空或对不上就用默认选项。
	return enumDefault(f)
}

// enumDefault 默认不在选项里就用第一项。
func enumDefault(f Field) string {
	// 默认值先收成文本。
	s := asStringDef(f.Default)
	// 逐个选项看有没有对上。
	for _, o := range f.Options {
		// 默认就在选项里，可以用。
		if o == s {
			return s
		}
	}
	// 没有选项就只能交回默认文本。
	if len(f.Options) == 0 {
		return s
	}
	return f.Options[0]
}

// enumCandidates 把旧布尔/数字也收成可匹配的选项。
func enumCandidates(v any) []string {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 字符串去掉空白当身份。
	case string:
		return []string{x}
	// 旧布尔展开成是或否。
	case bool:
		// 真对应是和 true。
		if x {
			return []string{"是", "true"}
		}
		return []string{"否", "false"}
	// 数字收成不带多余小数的文本。
	case float64:
		// 收成一段文本再去对选项。
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	// 认不出的根或类型直接拒绝。
	default:
		// 能收成文本就用它。
		if s, ok := asString(v); ok {
			return []string{s}
		}
		return nil
	}
}

// asString 能当文本就收成字符串。
func asString(v any) (string, bool) {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 空值不能当成文本。
	case nil:
		return "", false
	// 字符串去掉空白当身份。
	case string:
		return x, true
	// 数字收成不带多余小数的文本。
	case float64:
		// 数字收成一段文本。
		return strconv.FormatFloat(x, 'f', -1, 64), true
	// 延迟解析的数字先试着转。
	case json.Number:
		// 延迟数字收成一段文本。
		return x.String(), true
	// 旧布尔展开成是或否。
	case bool:
		// 布尔收成一段文本。
		return strconv.FormatBool(x), true
	// 认不出的根或类型直接拒绝。
	default:
		return "", false
	}
}

// asStringDef 收不成文本则空串。
func asStringDef(v any) string {
	// 能收成文本就用它。
	if s, ok := asString(v); ok {
		return s
	}
	return ""
}

// asFloat 能当数字就收成浮点。
func asFloat(v any) (float64, bool) {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 数字收成不带多余小数的文本。
	case float64:
		return x, true
	// 延迟解析的数字先试着转。
	case json.Number:
		// 延迟数字试着转成浮点。
		n, err := x.Float64()
		return n, err == nil
	// 字符串去掉空白当身份。
	case string:
		// 文本试着解析成数字。
		n, err := strconv.ParseFloat(x, 64)
		return n, err == nil
	// 整数直接当成浮点。
	case int:
		// 整数直接当成浮点。
		return float64(x), true
	// 整数直接当成浮点。
	case int64:
		// 整数直接当成浮点。
		return float64(x), true
	// 认不出的根或类型直接拒绝。
	default:
		return 0, false
	}
}

// asFloatDef 收不成数字则 0。
func asFloatDef(v any) float64 {
	// 能当数字就用这个数。
	if n, ok := asFloat(v); ok {
		return n
	}
	return 0
}

// asBool 能当布尔就收下；是/否按设备旧写法兼容。
func asBool(v any) (bool, bool) {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 旧布尔展开成是或否。
	case bool:
		return x, true
	// 字符串去掉空白当身份。
	case string:
		// 去掉空白后再认是与否。
		switch strings.TrimSpace(x) {
		// 旧写法的是当成真。
		case "是":
			return true, true
		// 旧写法的否当成假。
		case "否":
			return false, true
		}
		// 其余文本再试标准布尔。
		b, err := strconv.ParseBool(x)
		return b, err == nil
	// 认不出的根或类型直接拒绝。
	default:
		return false, false
	}
}

// asBoolDef 收不成布尔则否。
func asBoolDef(v any) bool {
	// 能当成布尔就用这个值。
	if b, ok := asBool(v); ok {
		return b
	}
	return false
}

// Default 工艺按设备明文；工程旧登记簿只给拆行。
func Default(kind string) Schema {
	// 工程走旧登记簿，只给拆行。
	if kind == KindProject {
		// 交回旧工程登记簿。
		return defaultProject()
	}
	// 其余当工艺字段表。
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

// procRef 工程正文里的工艺身份槽，空表示未选。
func procRef(key, label string) Field {
	return Field{Key: key, Label: label, Type: TypeProcess, Default: ""}
}

// flag 启用/完成按 App 布尔落地。
func flag(key, label string, on bool) Field {
	return Field{Key: key, Label: label, Type: TypeBool, Default: on}
}

// poseFields 位姿六个轴加外部轴。
func poseFields() []Field {
	return []Field{
		num("x", "X", "mm", 0), num("y", "Y", "mm", 0), num("z", "Z", "mm", 0),
		num("rx", "Rx", "°", 0), num("ry", "Ry", "°", 0), num("rz", "Rz", "°", 0),
		num("ext1", "外部轴", "mm", 0),
	}
}

// anglesField 点或参考点上的关节角。
func anglesField() Field {
	return Field{Key: "jointAngles", Label: "关节角", Type: TypeArray, Items: &Field{Key: "a", Label: "角", Type: TypeString, Default: 0.0}}
}

// refFields 参考点：位姿加关节角。
func refFields() []Field {
	return []Field{
		{Key: "pose", Label: "位姿", Type: TypeObject, Fields: poseFields()},
		anglesField(),
	}
}

// pointField 一个焊点：身份、类型、位姿、关节角、执行偏移、X 向参考。
func pointField() Field {
	return Field{Type: TypeObject, Label: "点", Fields: []Field{
		str("id", "点身份", ""),
		str("type", "点类型", "START"),
		{Key: "pose", Label: "位姿", Type: TypeObject, Fields: poseFields()},
		anglesField(),
		{Key: "executionOffsets", Label: "执行偏移", Type: TypeArray, Items: &Field{Key: "a", Label: "偏移", Type: TypeString, Default: 0.0}},
		{Key: "refPointX", Label: "X 向参考", Type: TypeObject, Fields: refFields()},
	}}
}

// pathFields 一条路径：身份、点列、工艺引用、启用。
func pathFields() []Field {
	// 先做出一个焊点，再放进点列。
	el := pointField()
	return []Field{
		str("id", "路径身份", ""),
		str("name", "路径名", ""),
		{Key: "points", Label: "点", Type: TypeArray, Items: &el},
		procRef("processId", "工艺"),
		num("selectedPointIndex", "选中点", "", 0),
		flag("isEnabled", "启用", true),
	}
}

// defaultProcess 设备侧已有的工艺字段表。
func defaultProcess() Schema {
	// 摆动参数收成一个对象字段。
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
