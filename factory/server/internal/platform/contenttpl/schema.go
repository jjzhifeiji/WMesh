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
	// 没能检查字段表能不能拿来套就停，避免带着残缺继续。
	if err := Validate(s); err != nil {
		return nil, err
	}
	// 交回把结构收成字节的结果。
	return json.Marshal(s)
}

// Parse 读字段表。
func Parse(raw []byte) (Schema, error) {
	// 准备放下这段文本。
	var s Schema
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &s); err != nil {
		// 字段表不合法，不能拿去套。
		return Schema{}, fmt.Errorf("content template is invalid")
	}
	// 没能检查字段表能不能拿来套就停，避免带着残缺继续。
	if err := Validate(s); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// Validate 检查模版能用来套用。
func Validate(s Schema) error {
	// 按根是对象还是数组分路处理。
	switch s.Root {
	// 根是对象时按对象字段处理。
	case RootObject:
		// 点了种类才按种类去收字段。
		if s.Item != nil || len(s.Kinds) > 0 || len(s.Templates) > 0 {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 交回检查键不重复，深度也不超限的结果。
		return validateFields(s.Fields, 1)
	// 根是数组时按元素逐项处理。
	case RootArray:
		// 有命名模版就按工程条目来处理。
		if len(s.Templates) > 0 {
			// 点了种类才按种类去收字段。
			if s.Item != nil || len(s.Fields) > 0 || len(s.Kinds) > 0 {
				// 字段表不合法，不能拿去套。
				return fmt.Errorf("content template is invalid")
			}
			// 交回校验模版，再交给后面的结果。
			return validateTemplates(s.Templates)
		}
		// 点了种类才按种类去收字段。
		if len(s.Kinds) > 0 {
			// 有字段才往下套，空表就停。
			if s.Item != nil || len(s.Fields) > 0 {
				// 字段表不合法，不能拿去套。
				return fmt.Errorf("content template is invalid")
			}
			// 交回校验，再交给后面的结果。
			return validateKinds(s.Kinds)
		}
		// 宿主配置解出来了才拿掉互相冲突的项。
		if s.Item != nil {
			// 有字段才往下套，空表就停。
			if len(s.Fields) > 0 {
				// 字段表不合法，不能拿去套。
				return fmt.Errorf("content template is invalid")
			}
			// 交回检查这个字段的类型和子结构的结果。
			return validateField(*s.Item, 1)
		}
		// 有字段才往下套，空表就停。
		if len(s.Fields) > 0 {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 交回校验模版，再交给后面的结果。
		return validateTemplates(s.Templates)
	// 别的根类型不能拿来套用。
	default:
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
}

// validateFields 键不重复、深度和个数有上限。
func validateFields(fields []Field, depth int) error {
	// 嵌套太深就拒绝，避免递归把栈撑满。
	if depth > maxDepth || len(fields) > maxFields {
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 按字段逐项处理，多出来的键不要。
	for _, f := range fields {
		// 这个键已经收过就跳过。
		if _, ok := seen[f.Key]; ok || !keyRe.MatchString(f.Key) {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 定下已经见过，再交给后面。
		seen[f.Key] = struct{}{}
		// 没能检查这个字段的类型和子结构就停，避免带着残缺继续。
		if err := validateField(f, depth); err != nil {
			return err
		}
	}
	return nil
}

// validateField 类型与子结构必须匹配。
func validateField(f Field, depth int) error {
	// 是空的就改用默认，或按没有处理。
	if f.Label == "" {
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
	// 按字段类型分路，不同类型不能混用。
	switch f.Type {
	// 这些标量不该再带下级字段。
	case TypeString, TypeNumber, TypeBool, TypeProcess:
		// 已经带了元素模版就按它往下套。
		if len(f.Fields) > 0 || f.Items != nil || (f.Type == TypeProcess && len(f.Options) > 0) {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		return nil
	// 对象要带下级字段，再往下检查。
	case TypeObject:
		// 已经带了元素模版就按它往下套。
		if f.Items != nil {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 交回检查键不重复，深度也不超限的结果。
		return validateFields(f.Fields, depth+1)
	// 数组要带元素模版，再往下检查。
	case TypeArray:
		// 数组缺了元素模版就不能用。
		if f.Items == nil || len(f.Fields) > 0 {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 交回检查这个字段的类型和子结构的结果。
		return validateField(*f.Items, depth+1)
	// 不认识的类型直接拒绝。
	default:
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
}

// Apply 按模版套正文：缺的补、多的删。schema 必须是规范字段表 JSON。
func Apply(schemaJSON, content []byte) ([]byte, error) {
	// 把输入读成后面能用的结构。
	s, err := Parse(schemaJSON)
	// 没能把输入读成后面能用的结构就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 准备放下取值，再交给后面。
	var v any
	// 去掉两头空白再使用。
	trim := bytes.TrimSpace(content)
	// 去掉空白之后还有字才当成有效文本。
	if len(trim) > 0 {
		// 没能把字节还原成结构就停，避免带着残缺继续。
		if err := json.Unmarshal(trim, &v); err != nil {
			// 定下取值，再交给后面。
			v = nil
		}
	}
	// 准备放下输出，再交给后面。
	var out any
	// 按根是对象还是数组分路处理。
	switch s.Root {
	// 根是对象时按对象字段处理。
	case RootObject:
		// 按字段表套一个对象。
		out = applyObject(s.Fields, v)
	// 根是数组时按元素逐项处理。
	case RootArray:
		// 解不开就当没有这份配置。
		if len(s.Templates) > 0 || (s.Item == nil && len(s.Kinds) == 0) {
			// 套用工程模版，再交给后面。
			out = applyProjectTemplates(s, v)
			// 点了种类才按种类去收字段。
		} else if len(s.Kinds) > 0 {
			// 套用工程，再交给后面。
			out = applyProjectKinds(s, v)
			// 条件不成立就换一路，避免误往下做。
		} else {
			// 按元素模版套整个数组。
			out = applyArray(*s.Item, v)
		}
	}
	// 交回把结构收成字节的结果。
	return json.Marshal(out)
}

// ApplyProjectItems 工程正文是数组；按 templateId 套那一份对象字段。
func ApplyProjectItems(items []ProjectItemSchema, content []byte) ([]byte, error) {
	// 定下身份，再交给后面。
	byID := map[string][]Field{}
	// 逐项处理，空列表就直接跳过。
	for _, it := range items {
		// 定下身份，再交给后面。
		byID[it.ID] = it.Fields
	}
	// 准备放下取值，再交给后面。
	var v any
	// 去掉两头空白再使用。
	trim := bytes.TrimSpace(content)
	// 去掉空白之后还有字才当成有效文本。
	if len(trim) > 0 {
		// 没能把字节还原成结构就停，避免带着残缺继续。
		if err := json.Unmarshal(trim, &v); err != nil {
			// 定下取值，再交给后面。
			v = nil
		}
	}
	// 收成数组再往下走，对不上就当不是数组。
	src, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		// 交回把结构收成字节的结果。
		return json.Marshal([]any{})
	}
	// 按需要的长度把缓冲准备好。
	out := make([]any, 0, len(src))
	// 逐个元素套用，空数组就保持空。
	for _, el := range src {
		// 收成对象再往下看，对不上就当不是对象。
		obj, _ := el.(map[string]any)
		// 这一层不是对象就不再往下走。
		if obj == nil {
			continue
		}
		// 收成文本，对不上就当这个槽是空的。
		id, _ := obj["templateId"].(string)
		// 先当还没找到，找到再改成命中。
		fields, ok := byID[id]
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			continue
		}
		// 按字段表套一个对象。
		applied := applyObject(fields, obj)
		// 定下做过，再交给后面。
		applied["templateId"] = id
		// 把这一段接进结果，顺序要保持住。
		out = append(out, applied)
	}
	// 交回把结构收成字节的结果。
	return json.Marshal(out)
}

// applyObject 只保留模版里的键，缺的补默认；正文里的空对象/数组原样留下。
func applyObject(fields []Field, v any) map[string]any {
	// 收成对象再往下看，对不上就当不是对象。
	src, _ := v.(map[string]any)
	// 是空的就换一路，避免空着往下用。
	if src == nil {
		// 准备承接解出来的对象。
		src = map[string]any{}
	}
	// 按需要的长度把缓冲准备好。
	out := make(map[string]any, len(fields))
	// 按字段逐项处理，多出来的键不要。
	for _, f := range fields {
		// 先把这一步的结果放下，后面还要用。
		val, exists := src[f.Key]
		// 是空的就换一路，避免空着往下用。
		if exists && val == nil && (f.Type == TypeObject || f.Type == TypeArray) {
			// 定下输出，再交给后面。
			out[f.Key] = nil
			continue
		}
		// 按字段类型套一个值。
		out[f.Key] = applyField(f, val)
	}
	return out
}

// applyArray 按元素模版逐项套用。
func applyArray(item Field, v any) []any {
	// 收成数组再往下走，对不上就当不是数组。
	src, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return []any{}
	}
	// 按需要的长度把缓冲准备好。
	out := make([]any, len(src))
	// 逐个元素套用，空数组就保持空。
	for i, el := range src {
		// 按字段类型套一个值。
		out[i] = applyField(item, el)
	}
	return out
}

// applyField 按类型套单值。
func applyField(f Field, v any) any {
	// 按字段类型分路，不同类型不能混用。
	switch f.Type {
	// 文本按文本规则收下。
	case TypeString:
		// 有选项才按选项收，没有就当普通文本。
		if len(f.Options) > 0 {
			// 交回按选项收，对不上就用默认的结果。
			return applyEnum(f, v)
		}
		// 交回按文本规则收下这个值的结果。
		return applyText(v, f.Default)
	// 工艺槽只留下身份字符串。
	case TypeProcess:
		// 交回只留下工艺身份字符串的结果。
		return applyProcessID(v, f.Default)
	// 数字按数值收，对不上就用默认。
	case TypeNumber:
		// 条件不成立就换一路，避免误往下做。
		if n, ok := asFloat(v); ok {
			return n
		}
		// 交回收成数字默认，再交给后面的结果。
		return asFloatDef(f.Default)
	// 是否按布尔收下，旧写法也认。
	case TypeBool:
		// 条件不成立就换一路，避免误往下做。
		if b, ok := asBool(v); ok {
			return b
		}
		// 交回收成是否默认，再交给后面的结果。
		return asBoolDef(f.Default)
	// 对象要带下级字段，再往下检查。
	case TypeObject:
		// 交回按字段表套一个对象的结果。
		return applyObject(f.Fields, v)
	// 数组要带元素模版，再往下检查。
	case TypeArray:
		// 数组缺了元素模版就不能用。
		if f.Items == nil {
			return []any{}
		}
		// 交回按元素模版套整个数组的结果。
		return applyArray(*f.Items, v)
	// 不认识的类型就退回默认值。
	default:
		return nil
	}
}

// applyProcessID 只留下字符串身份，空表示未选。
func applyProcessID(v, def any) any {
	// 条件不成立就换一路，避免误往下做。
	if s, ok := v.(string); ok {
		// 交回去掉两头空白再使用的结果。
		return strings.TrimSpace(s)
	}
	// 值是空的就按没有内容处理。
	if s, ok := def.(string); ok && v == nil {
		// 交回去掉两头空白再使用的结果。
		return strings.TrimSpace(s)
	}
	// 值是空的就按没有内容处理。
	if v == nil {
		return ""
	}
	// 交回只留下工艺身份字符串的结果。
	return applyProcessID(nil, def)
}

// applyText 文本：能当数字的保持数字，其余当字符串。
func applyText(v, def any) any {
	// 值是空的就按没有内容处理。
	if v == nil {
		// 定下取值，再交给后面。
		v = def
	}
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 数字按浮点收下，保持原来的量。
	case float64:
		return x
	// 带精度的数字先收成浮点。
	case json.Number:
		// 取出浮点数值，再交给后面。
		n, err := x.Float64()
		// 没有出错就按成功返回，不用再补救。
		if err == nil {
			return n
		}
		// 交回收成普通文本再拿去比较或拼接的结果。
		return x.String()
	// 文本按字符串收下。
	case string:
		// 条件不成立就换一路，避免误往下做。
		if n, ok := parsePlainNumber(x); ok {
			return n
		}
		return x
	// 是否按布尔收下，不再改成文本。
	case bool:
		// 交回把是否收成文本的结果。
		return strconv.FormatBool(x)
	// 其余类型收不成文本就用默认。
	default:
		// 已经有值就按有内容处理，不要覆盖成空。
		if def != nil && v != def {
			// 交回按文本规则收下这个值的结果。
			return applyText(def, nil)
		}
		return ""
	}
}

// parsePlainNumber 纯数字串收成数字，前导零不当数字。
func parsePlainNumber(s string) (float64, bool) {
	// 把文本收成数字，再交给后面。
	n, err := strconv.ParseFloat(s, 64)
	// 没能把文本收成数字就停，避免带着残缺继续。
	if err != nil {
		return 0, false
	}
	// 先把这一步的结果放下，后面还要用。
	lead := s
	// 有内容才继续，空的这一支跳过。
	if len(lead) > 0 && (lead[0] == '+' || lead[0] == '-') {
		// 把这个值定下来，后面的判断才有依据。
		lead = lead[1:]
	}
	// 对不上就换一路，避免把不符的当成通过。
	if len(lead) > 1 && lead[0] == '0' && lead[1] != '.' {
		return 0, false
	}
	return n, true
}

// applyEnum 取值必须在选项里，对不上则用默认；是/否兼容旧布尔。
func applyEnum(f Field, v any) any {
	// 值是空的就按没有内容处理。
	if v == nil {
		// 交回取出选项里要用的默认项的结果。
		return enumDefault(f)
	}
	// 逐项处理，空的就不进入循环。
	for _, c := range enumCandidates(v) {
		// 逐项处理，空的就不进入循环。
		for _, o := range f.Options {
			// 对上了才走这一路，其余分开处理。
			if o == c {
				return o
			}
		}
	}
	// 交回取出选项里要用的默认项的结果。
	return enumDefault(f)
}

// enumDefault 默认不在选项里就用第一项。
func enumDefault(f Field) string {
	// 收成文本默认，再交给后面。
	s := asStringDef(f.Default)
	// 逐项处理，空的就不进入循环。
	for _, o := range f.Options {
		// 对上了才走这一路，其余分开处理。
		if o == s {
			return s
		}
	}
	// 数量是零就按没有来处理。
	if len(f.Options) == 0 {
		return s
	}
	return f.Options[0]
}

// enumCandidates 把旧布尔/数字也收成可匹配的选项。
func enumCandidates(v any) []string {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 文本按字符串收下。
	case string:
		return []string{x}
	// 是否按布尔收下，不再改成文本。
	case bool:
		// 条件不成立就换一路，避免误往下做。
		if x {
			return []string{"是", "true"}
		}
		return []string{"否", "false"}
	// 数字按浮点收下，保持原来的量。
	case float64:
		// 交回把数字收成不带多余零的文本的结果。
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	// 其余类型没有可以对上的选项。
	default:
		// 条件不成立就换一路，避免误往下做。
		if s, ok := asString(v); ok {
			return []string{s}
		}
		return nil
	}
}

// asString 能当文本就收成字符串。
func asString(v any) (string, bool) {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 空值按没有内容来处理。
	case nil:
		return "", false
	// 文本按字符串收下。
	case string:
		return x, true
	// 数字按浮点收下，保持原来的量。
	case float64:
		// 交回把数字收成不带多余零的文本的结果。
		return strconv.FormatFloat(x, 'f', -1, 64), true
	// 带精度的数字先收成浮点。
	case json.Number:
		// 交回收成普通文本再拿去比较或拼接的结果。
		return x.String(), true
	// 是否按布尔收下，不再改成文本。
	case bool:
		// 交回把是否收成文本的结果。
		return strconv.FormatBool(x), true
	// 其余类型收不成文本。
	default:
		return "", false
	}
}

// asStringDef 收不成文本则空串。
func asStringDef(v any) string {
	// 条件不成立就换一路，避免误往下做。
	if s, ok := asString(v); ok {
		return s
	}
	return ""
}

// asFloat 能当数字就收成浮点。
func asFloat(v any) (float64, bool) {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 数字按浮点收下，保持原来的量。
	case float64:
		return x, true
	// 带精度的数字先收成浮点。
	case json.Number:
		// 取出浮点数值，再交给后面。
		n, err := x.Float64()
		return n, err == nil
	// 文本按字符串收下。
	case string:
		// 把文本收成数字，再交给后面。
		n, err := strconv.ParseFloat(x, 64)
		return n, err == nil
	// 整数先收成浮点再统一比较。
	case int:
		// 把这一步的结果交回给调用方。
		return float64(x), true
	// 长整数先收成浮点再统一比较。
	case int64:
		// 把这一步的结果交回给调用方。
		return float64(x), true
	// 其余类型收不成数字。
	default:
		return 0, false
	}
}

// asFloatDef 收不成数字则 0。
func asFloatDef(v any) float64 {
	// 条件不成立就换一路，避免误往下做。
	if n, ok := asFloat(v); ok {
		return n
	}
	return 0
}

// asBool 能当布尔就收下；是/否按设备旧写法兼容。
func asBool(v any) (bool, bool) {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 是否按布尔收下，不再改成文本。
	case bool:
		return x, true
	// 文本按字符串收下。
	case string:
		// 按取值分路处理，不把所有情况写在一处。
		switch strings.TrimSpace(x) {
		// 中文的是按打开收下。
		case "是":
			return true, true
		// 中文的否按关掉收下。
		case "否":
			return false, true
		}
		// 解析是否，再交给后面。
		b, err := strconv.ParseBool(x)
		return b, err == nil
	// 其余类型收不成是否。
	default:
		return false, false
	}
}

// asBoolDef 收不成布尔则否。
func asBoolDef(v any) bool {
	// 条件不成立就换一路，避免误往下做。
	if b, ok := asBool(v); ok {
		return b
	}
	return false
}

// Default 工艺按设备明文；工程旧登记簿只给拆行。
func Default(kind string) Schema {
	// 对上了才走这一路，其余分开处理。
	if kind == KindProject {
		// 交回默认工程，再交给后面的结果。
		return defaultProject()
	}
	// 交回默认工艺，再交给后面的结果。
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
	// 焊点字段，再交给后面。
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
	// 先把这一步的结果放下，后面还要用。
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
