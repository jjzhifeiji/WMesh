package contenttpl

import (
	"encoding/json"
	"fmt"
	"strings"
)

const processRefKey = "processId"    // 旧模版：此键的文本仍当工艺引用
const processPathKey = "processPath" // 禁止再写的路径键

var errBadProcessID = fmt.Errorf("process id is not an identity string")     // 引用不是字符串身份
var errUnknownProcessID = fmt.Errorf("process id is not in the rewrite map") // 对照表没有这条

// ErrProcessPath 工程正文不得再写路径键。
var ErrProcessPath = fmt.Errorf("process path is forbidden")

// RejectProcessPath 正文里出现路径键则拒绝。
func RejectProcessPath(content []byte) error {
	// 先看成是不是正文。
	v, ok := parseJSON(content)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 再往下递归查找路径键。
	return rejectProcessPath(v)
}

// 递归扫对象和数组里的路径键。
func rejectProcessPath(v any) error {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 对象先看自己有没有路径键。
	case map[string]any:
		// 有路径键就拒绝整篇。
		if _, ok := x[processPathKey]; ok {
			return ErrProcessPath
		}
		// 数组元素再查一层。
		for _, child := range x {
			// 子层有路径键就停。
			if err := rejectProcessPath(child); err != nil {
				return err
			}
		}
	// 数组里的每一份都要查。
	case []any:
		// 数组元素再查一层。
		for _, child := range x {
			// 子层有路径键就停。
			if err := rejectProcessPath(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// CollectProcessIDs 按工程模版收集非空工艺引用；非 JSON 或根类型对不上当没有引用。
func CollectProcessIDs(schemaJSON, content []byte) ([]string, error) {
	// 先读成字段表，不合法就不套。
	s, err := Parse(schemaJSON)
	// 模版读不成就不能套。
	if err != nil {
		return nil, err
	}
	// 先看成是不是正文。
	v, ok := parseJSON(content)
	// 不是正文就当没有引用。
	if !ok {
		return nil, nil
	}
	// 有路径键就不收集、不改写。
	if err := rejectProcessPath(v); err != nil {
		return nil, err
	}
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 按出现顺序攒引用。
	var ids []string
	// 同一身份只收一次，空的不进来。
	fn := func(id string) (string, error) {
		// 没见过才收进结果。
		if _, ok := seen[id]; !ok {
			// 记下这个身份避免再收。
			seen[id] = struct{}{}
			// 按第一次出现的顺序收下。
			ids = append(ids, id)
		}
		return id, nil
	}
	// 按模版走完，中途失败就停。
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// CollectProcessIDsFromItems 按各份自己的字段表收集工艺引用。
func CollectProcessIDsFromItems(items []ProjectItemSchema, content []byte) ([]string, error) {
	// 先看成是不是正文。
	v, ok := parseJSON(content)
	// 不是正文就当没有引用。
	if !ok {
		return nil, nil
	}
	// 有路径键就不收集、不改写。
	if err := rejectProcessPath(v); err != nil {
		return nil, err
	}
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 按出现顺序攒引用。
	var ids []string
	// 同一身份只收一次，空的不进来。
	fn := func(id string) (string, error) {
		// 没见过才收进结果。
		if _, ok := seen[id]; !ok {
			// 记下这个身份避免再收。
			seen[id] = struct{}{}
			// 按第一次出现的顺序收下。
			ids = append(ids, id)
		}
		return id, nil
	}
	// 按各份字段走完，中途失败就停。
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// RewriteProcessIDs 按工程模版改写工艺引用；找不到则失败；其它键不动。
func RewriteProcessIDs(schemaJSON, content []byte, idMap map[string]string) ([]byte, error) {
	// 先读成字段表，不合法就不套。
	s, err := Parse(schemaJSON)
	// 模版读不成就不能套。
	if err != nil {
		return nil, err
	}
	// 先看成是不是正文。
	v, ok := parseJSON(content)
	// 不是正文就当没有引用。
	if !ok {
		return content, nil
	}
	// 还没改过任何身份。
	changed := false
	// 对照表没有就失败，换过才算改过。
	fn := func(id string) (string, error) {
		// 到对照表里找新身份。
		next, ok := idMap[id]
		// 不是正文就当没有引用。
		if !ok {
			return "", errUnknownProcessID
		}
		// 新身份和旧的不同才算改过。
		if next != id {
			// 标记正文需要重新收口。
			changed = true
		}
		return next, nil
	}
	// 按模版走完，中途失败就停。
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	// 一个都没换就保持原文。
	if !changed {
		return content, nil
	}
	// 改过的正文重新收口。
	out, err := json.Marshal(v)
	// 收不成规范正文就停。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RewriteProcessIDsFromItems 按各份字段表改写工艺引用；其它键不动。
func RewriteProcessIDsFromItems(items []ProjectItemSchema, content []byte, idMap map[string]string) ([]byte, error) {
	// 先看成是不是正文。
	v, ok := parseJSON(content)
	// 不是正文就当没有引用。
	if !ok {
		return content, nil
	}
	// 还没改过任何身份。
	changed := false
	// 对照表没有就失败，换过才算改过。
	fn := func(id string) (string, error) {
		// 到对照表里找新身份。
		next, ok := idMap[id]
		// 不是正文就当没有引用。
		if !ok {
			return "", errUnknownProcessID
		}
		// 新身份和旧的不同才算改过。
		if next != id {
			// 标记正文需要重新收口。
			changed = true
		}
		return next, nil
	}
	// 按各份字段走完，中途失败就停。
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	// 一个都没换就保持原文。
	if !changed {
		return content, nil
	}
	// 改过的正文重新收口。
	out, err := json.Marshal(v)
	// 收不成规范正文就停。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseJSON 读不成 JSON 则不当工程正文。
func parseJSON(content []byte) (any, bool) {
	// 准备接拆开的正文。
	var v any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(content, &v); err != nil {
		return nil, false
	}
	return v, true
}

// isProcessRef 类型为工艺即引用；旧模版键 processId 的文本同样算。
func isProcessRef(f Field) bool {
	// 有选项就不能自由填写。
	if len(f.Options) > 0 {
		return false
	}
	// 这一处就是工艺引用。
	if f.Type == TypeProcess {
		return true
	}
	return f.Type == TypeString && f.Key == processRefKey
}

// walkRoot 按模版根类型走正文。
func walkRoot(s Schema, v any, fn func(string) (string, error)) error {
	// 按根类型决定怎么走正文。
	switch s.Root {
	// 数组根按登记方式往下走。
	case RootArray:
		// 命名模版或空登记走命名份。
		if len(s.Templates) > 0 || (s.Item == nil && len(s.Kinds) == 0) {
			// 命名份各自走自己的字段。
			return walkProjectItems(s, v, fn)
		}
		// 没有命名模版才看旧种类表。
		if len(s.Kinds) > 0 {
			// 旧种类按字段并集走。
			return walkObjectList(SelectedFields(s.Kinds), v, fn)
		}
		// 没有元素模版就没有可走的引用。
		if s.Item == nil {
			return nil
		}
		// 每个元素按同一份字段走。
		return walkArray(*s.Item, v, fn)
	// 对象根按字段表走。
	case RootObject:
		// 只走字段表里的键。
		return walkObject(s.Fields, v, fn)
	// 认不出的根或类型直接拒绝。
	default:
		return nil
	}
}

// walkProjectItems 数组里每个元素按它对应的命名模版走。
func walkProjectItems(s Schema, v any, fn func(string) (string, error)) error {
	// 不是数组就没有元素可走。
	arr, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 看点的类型里有没有坡口。
	for _, el := range arr {
		// 对不上模版的份跳过。
		tpl, ok := lookupTemplate(s, el)
		// 不是正文就当没有引用。
		if !ok {
			continue
		}
		// 多层焊缝不带附加槽。
		extra := tpl.Extra && tpl.Kind != ItemMulti
		// 这一份的字段走失败就停。
		if err := walkObject(ItemFields(tpl.Kind, extra), el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkProjectItemList 有 templateId 走那一份；没有则按各份字段并集走（旧正文升档）。
func walkProjectItemList(items []ProjectItemSchema, v any, fn func(string) (string, error)) error {
	// 不是数组就没有元素可走。
	arr, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 按模版身份记下各自的字段。
	byID := map[string][]Field{}
	// 没有身份时用各份字段的并集。
	var union []Field
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 每一份模版登记自己的字段。
	for _, it := range items {
		// 这份身份对上自己的字段表。
		byID[it.ID] = it.Fields
		// 并集里同一键只留第一次。
		for _, f := range it.Fields {
			// 这个键已经留下过。
			if _, ok := seen[f.Key]; ok {
				continue
			}
			// 记下这个键，防止后面重复。
			seen[f.Key] = struct{}{}
			// 第一次见到的键留下。
			union = append(union, f)
		}
	}
	// 看点的类型里有没有坡口。
	for _, el := range arr {
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
			// 对不上就用并集，兼容旧正文。
			fields = union
		}
		// 这一份走失败就停。
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObjectList 数组里每个对象按同一份字段表走。
func walkObjectList(fields []Field, v any, fn func(string) (string, error)) error {
	// 不是数组就没有元素可走。
	arr, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 看点的类型里有没有坡口。
	for _, el := range arr {
		// 这一份走失败就停。
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObject 只走模版里的键。
func walkObject(fields []Field, v any, fn func(string) (string, error)) error {
	// 不是对象就没有键可走。
	obj, ok := v.(map[string]any)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 并集里逐个字段看键。
	for _, f := range fields {
		// 工艺引用在这里读或改。
		if isProcessRef(f) {
			// 这一处引用失败就停。
			if err := touchProcessID(obj, f.Key, fn); err != nil {
				return err
			}
			continue
		}
		// 按字段类型查子结构是否匹配。
		switch f.Type {
		// 对象再走进子字段。
		case TypeObject:
			// 子对象走失败就停。
			if err := walkObject(f.Fields, obj[f.Key], fn); err != nil {
				return err
			}
		// 数组再走进里面的元素。
		case TypeArray:
			// 没有元素模版就交回空数组。
			if f.Items == nil {
				continue
			}
			// 数组元素走失败就停。
			if err := walkArray(*f.Items, obj[f.Key], fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkArray 数组元素跟模版 Items。
func walkArray(item Field, v any, fn func(string) (string, error)) error {
	// 不是数组就没有元素可走。
	arr, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 每个元素单独看是不是引用。
	for i, el := range arr {
		// 元素本身就是工艺引用。
		if isProcessRef(item) {
			// 引用必须是身份字符串。
			s, err := asProcessID(el)
			// 这一处引用处理失败就停。
			if err != nil {
				return err
			}
			// 空身份不计入引用。
			if s == "" {
				continue
			}
			// 把身份交给收集或改写。
			next, err := fn(s)
			// 这一处引用处理失败就停。
			if err != nil {
				return err
			}
			// 身份变了才写回正文。
			if next != s {
				// 把新身份写回这个元素。
				arr[i] = next
			}
			continue
		}
		// 这一处引用处理失败就停。
		if err := walkField(item, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkField 对象或再套数组。
func walkField(f Field, v any, fn func(string) (string, error)) error {
	// 按字段类型查子结构是否匹配。
	switch f.Type {
	// 对象再走进子字段。
	case TypeObject:
		// 对象再走它的子字段。
		return walkObject(f.Fields, v, fn)
	// 数组再走进里面的元素。
	case TypeArray:
		// 没有元素模版就交回空数组。
		if f.Items == nil {
			return nil
		}
		// 数组再走它的元素。
		return walkArray(*f.Items, v, fn)
	// 认不出的根或类型直接拒绝。
	default:
		return nil
	}
}

// touchProcessID 改或读取一处引用；空串不算。
func touchProcessID(obj map[string]any, key string, fn func(string) (string, error)) error {
	// 这个键不存在就没有引用。
	v, ok := obj[key]
	// 不是正文就当没有引用。
	if !ok {
		return nil
	}
	// 值必须是身份字符串。
	s, err := asProcessID(v)
	// 这一处引用处理失败就停。
	if err != nil {
		return err
	}
	// 空身份不计入引用。
	if s == "" {
		return nil
	}
	// 把身份交给收集或改写。
	next, err := fn(s)
	// 这一处引用处理失败就停。
	if err != nil {
		return err
	}
	// 身份变了才写回正文。
	if next != s {
		// 把新身份写回这一处。
		obj[key] = next
	}
	return nil
}

// asProcessID 只收字符串身份。
func asProcessID(v any) (string, error) {
	// 对象和数组都要往下看。
	switch x := v.(type) {
	// 空值不能当成文本。
	case nil:
		return "", nil
	// 字符串去掉空白当身份。
	case string:
		// 交回去掉空白的身份。
		return strings.TrimSpace(x), nil
	// 认不出的根或类型直接拒绝。
	default:
		return "", errBadProcessID
	}
}
