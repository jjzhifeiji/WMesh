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
	// 把正文读成结构，读不成就当没有。
	v, ok := parseJSON(content)
	// 读不成结构就当没有正文，不再往下拆。
	if !ok {
		return nil
	}
	// 交回发现路径键就拒绝这份正文的结果。
	return rejectProcessPath(v)
}

// 递归扫对象和数组里的路径键。
func rejectProcessPath(v any) error {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 碰到对象就再往里面找。
	case map[string]any:
		// 条件不成立就换一路，避免误往下做。
		if _, ok := x[processPathKey]; ok {
			return ErrProcessPath
		}
		// 逐项处理，空的就不进入循环。
		for _, child := range x {
			// 没能发现路径键就拒绝这份正文就停，避免带着残缺继续。
			if err := rejectProcessPath(child); err != nil {
				return err
			}
		}
	// 碰到数组就逐个再往下找。
	case []any:
		// 逐项处理，空的就不进入循环。
		for _, child := range x {
			// 没能发现路径键就拒绝这份正文就停，避免带着残缺继续。
			if err := rejectProcessPath(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// CollectProcessIDs 按工程模版收集非空工艺引用；非 JSON 或根类型对不上当没有引用。
func CollectProcessIDs(schemaJSON, content []byte) ([]string, error) {
	// 把输入读成后面能用的结构。
	s, err := Parse(schemaJSON)
	// 没能把输入读成后面能用的结构就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 把正文读成结构，读不成就当没有。
	v, ok := parseJSON(content)
	// 读不成结构就当没有正文，不再往下拆。
	if !ok {
		return nil, nil
	}
	// 没能发现路径键就拒绝这份正文就停，避免带着残缺继续。
	if err := rejectProcessPath(v); err != nil {
		return nil, err
	}
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 准备收集字符串结果。
	var ids []string
	// 收进没见过的工艺身份，重复的只留一次。
	fn := func(id string) (string, error) {
		// 没有命中就走另一路，不用零值冒充有值。
		if _, ok := seen[id]; !ok {
			// 定下已经见过，再交给后面。
			seen[id] = struct{}{}
			// 把这一段接进结果，顺序要保持住。
			ids = append(ids, id)
		}
		return id, nil
	}
	// 没能从根按模版往下走就停，避免带着残缺继续。
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// CollectProcessIDsFromItems 按各份自己的字段表收集工艺引用。
func CollectProcessIDsFromItems(items []ProjectItemSchema, content []byte) ([]string, error) {
	// 把正文读成结构，读不成就当没有。
	v, ok := parseJSON(content)
	// 读不成结构就当没有正文，不再往下拆。
	if !ok {
		return nil, nil
	}
	// 没能发现路径键就拒绝这份正文就停，避免带着残缺继续。
	if err := rejectProcessPath(v); err != nil {
		return nil, err
	}
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 准备收集字符串结果。
	var ids []string
	// 收进没见过的工艺身份，重复的只留一次。
	fn := func(id string) (string, error) {
		// 没有命中就走另一路，不用零值冒充有值。
		if _, ok := seen[id]; !ok {
			// 定下已经见过，再交给后面。
			seen[id] = struct{}{}
			// 把这一段接进结果，顺序要保持住。
			ids = append(ids, id)
		}
		return id, nil
	}
	// 没能按各份字段表走工程条目就停，避免带着残缺继续。
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// RewriteProcessIDs 按工程模版改写工艺引用；找不到则失败；其它键不动。
func RewriteProcessIDs(schemaJSON, content []byte, idMap map[string]string) ([]byte, error) {
	// 把输入读成后面能用的结构。
	s, err := Parse(schemaJSON)
	// 没能把输入读成后面能用的结构就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 把正文读成结构，读不成就当没有。
	v, ok := parseJSON(content)
	// 读不成结构就当没有正文，不再往下拆。
	if !ok {
		return content, nil
	}
	// 先当没改过，真换了身份再重新序列化。
	changed := false
	// 按对照表换成新身份，对不上就失败。
	fn := func(id string) (string, error) {
		// 先当还没找到，找到再改成命中。
		next, ok := idMap[id]
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			return "", errUnknownProcessID
		}
		// 新身份和旧的不一样才算真正改过。
		if next != id {
			// 先当没改过，真换了身份再重新序列化。
			changed = true
		}
		return next, nil
	}
	// 没能从根按模版往下走就停，避免带着残缺继续。
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	// 一个身份都没改就交回原文，不再序列化。
	if !changed {
		return content, nil
	}
	// 把结构收成字节，再交给后面。
	out, err := json.Marshal(v)
	// 没能把结构收成字节就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RewriteProcessIDsFromItems 按各份字段表改写工艺引用；其它键不动。
func RewriteProcessIDsFromItems(items []ProjectItemSchema, content []byte, idMap map[string]string) ([]byte, error) {
	// 把正文读成结构，读不成就当没有。
	v, ok := parseJSON(content)
	// 读不成结构就当没有正文，不再往下拆。
	if !ok {
		return content, nil
	}
	// 先当没改过，真换了身份再重新序列化。
	changed := false
	// 按对照表换成新身份，对不上就失败。
	fn := func(id string) (string, error) {
		// 先当还没找到，找到再改成命中。
		next, ok := idMap[id]
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			return "", errUnknownProcessID
		}
		// 新身份和旧的不一样才算真正改过。
		if next != id {
			// 先当没改过，真换了身份再重新序列化。
			changed = true
		}
		return next, nil
	}
	// 没能按各份字段表走工程条目就停，避免带着残缺继续。
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	// 一个身份都没改就交回原文，不再序列化。
	if !changed {
		return content, nil
	}
	// 把结构收成字节，再交给后面。
	out, err := json.Marshal(v)
	// 没能把结构收成字节就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseJSON 读不成 JSON 则不当工程正文。
func parseJSON(content []byte) (any, bool) {
	// 准备放下取值，再交给后面。
	var v any
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(content, &v); err != nil {
		return nil, false
	}
	return v, true
}

// isProcessRef 类型为工艺即引用；旧模版键 processId 的文本同样算。
func isProcessRef(f Field) bool {
	// 有选项才按选项收，没有就当普通文本。
	if len(f.Options) > 0 {
		return false
	}
	// 这个槽是工艺引用，要按身份来收。
	if f.Type == TypeProcess {
		return true
	}
	return f.Type == TypeString && f.Key == processRefKey
}

// walkRoot 按模版根类型走正文。
func walkRoot(s Schema, v any, fn func(string) (string, error)) error {
	// 按根是对象还是数组分路处理。
	switch s.Root {
	// 根是数组时按元素逐项处理。
	case RootArray:
		// 解不开就当没有这份配置。
		if len(s.Templates) > 0 || (s.Item == nil && len(s.Kinds) == 0) {
			// 交回顺着走工程条目，再交给后面的结果。
			return walkProjectItems(s, v, fn)
		}
		// 点了种类才按种类去收字段。
		if len(s.Kinds) > 0 {
			// 交回选中字段，再交给后面的结果。
			return walkObjectList(SelectedFields(s.Kinds), v, fn)
		}
		// 解不开就当没有这份配置。
		if s.Item == nil {
			return nil
		}
		// 交回按元素模版走数组里的每一项的结果。
		return walkArray(*s.Item, v, fn)
	// 根是对象时按对象字段处理。
	case RootObject:
		// 交回只顺着字段表里的键往下走的结果。
		return walkObject(s.Fields, v, fn)
	// 根类型对不上就当没有可走的内容。
	default:
		return nil
	}
}

// walkProjectItems 数组里每个元素按它对应的命名模版走。
func walkProjectItems(s Schema, v any, fn func(string) (string, error)) error {
	// 收成数组再往下走，对不上就当不是数组。
	arr, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return nil
	}
	// 逐个元素套用，空数组就保持空。
	for _, el := range arr {
		// 按身份找到对应的那一份模版。
		tpl, ok := lookupTemplate(s, el)
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			continue
		}
		// 定下附加，再交给后面。
		extra := tpl.Extra && tpl.Kind != ItemMulti
		// 没能取出这种条目要用的字段就停，避免带着残缺继续。
		if err := walkObject(ItemFields(tpl.Kind, extra), el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkProjectItemList 有 templateId 走那一份；没有则按各份字段并集走（旧正文升档）。
func walkProjectItemList(items []ProjectItemSchema, v any, fn func(string) (string, error)) error {
	// 收成数组再往下走，对不上就当不是数组。
	arr, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return nil
	}
	// 定下身份，再交给后面。
	byID := map[string][]Field{}
	// 准备放下合并，再交给后面。
	var union []Field
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 逐项处理，空列表就直接跳过。
	for _, it := range items {
		// 定下身份，再交给后面。
		byID[it.ID] = it.Fields
		// 逐项处理，空的就不进入循环。
		for _, f := range it.Fields {
			// 这个键已经收过就跳过。
			if _, ok := seen[f.Key]; ok {
				continue
			}
			// 定下已经见过，再交给后面。
			seen[f.Key] = struct{}{}
			// 把这一段接进结果，顺序要保持住。
			union = append(union, f)
		}
	}
	// 逐个元素套用，空数组就保持空。
	for _, el := range arr {
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
			// 定下字段，再交给后面。
			fields = union
		}
		// 没能只顺着字段表里的键往下走就停，避免带着残缺继续。
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObjectList 数组里每个对象按同一份字段表走。
func walkObjectList(fields []Field, v any, fn func(string) (string, error)) error {
	// 收成数组再往下走，对不上就当不是数组。
	arr, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return nil
	}
	// 逐个元素套用，空数组就保持空。
	for _, el := range arr {
		// 没能只顺着字段表里的键往下走就停，避免带着残缺继续。
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObject 只走模版里的键。
func walkObject(fields []Field, v any, fn func(string) (string, error)) error {
	// 收成对象再往下看，对不上就当不是对象。
	obj, ok := v.(map[string]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return nil
	}
	// 按字段逐项处理，多出来的键不要。
	for _, f := range fields {
		// 条件不成立就换一路，避免误往下做。
		if isProcessRef(f) {
			// 没能处理工艺身份，再交给后面就停，避免带着残缺继续。
			if err := touchProcessID(obj, f.Key, fn); err != nil {
				return err
			}
			continue
		}
		// 按字段类型分路，不同类型不能混用。
		switch f.Type {
		// 对象要带下级字段，再往下检查。
		case TypeObject:
			// 没能只顺着字段表里的键往下走就停，避免带着残缺继续。
			if err := walkObject(f.Fields, obj[f.Key], fn); err != nil {
				return err
			}
		// 数组要带元素模版，再往下检查。
		case TypeArray:
			// 数组缺了元素模版就不能用。
			if f.Items == nil {
				continue
			}
			// 没能按元素模版走数组里的每一项就停，避免带着残缺继续。
			if err := walkArray(*f.Items, obj[f.Key], fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkArray 数组元素跟模版 Items。
func walkArray(item Field, v any, fn func(string) (string, error)) error {
	// 收成数组再往下走，对不上就当不是数组。
	arr, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return nil
	}
	// 逐个元素套用，空数组就保持空。
	for i, el := range arr {
		// 条件不成立就换一路，避免误往下做。
		if isProcessRef(item) {
			// 只接受字符串形式的工艺身份。
			s, err := asProcessID(el)
			// 没能只接受字符串形式的工艺身份就停，避免带着残缺继续。
			if err != nil {
				return err
			}
			// 文本是空的就按没有来处理。
			if s == "" {
				continue
			}
			// 做完这一步，再交给后面。
			next, err := fn(s)
			// 这一步没做成就停，避免带着残缺继续。
			if err != nil {
				return err
			}
			// 收出来的和原来不同才覆盖。
			if next != s {
				// 把这个值定下来，后面的判断才有依据。
				arr[i] = next
			}
			continue
		}
		// 没能顺着走字段，再交给后面就停，避免带着残缺继续。
		if err := walkField(item, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkField 对象或再套数组。
func walkField(f Field, v any, fn func(string) (string, error)) error {
	// 按字段类型分路，不同类型不能混用。
	switch f.Type {
	// 对象要带下级字段，再往下检查。
	case TypeObject:
		// 交回只顺着字段表里的键往下走的结果。
		return walkObject(f.Fields, v, fn)
	// 数组要带元素模版，再往下检查。
	case TypeArray:
		// 数组缺了元素模版就不能用。
		if f.Items == nil {
			return nil
		}
		// 交回按元素模版走数组里的每一项的结果。
		return walkArray(*f.Items, v, fn)
	// 标量没有下级，不用再往下走。
	default:
		return nil
	}
}

// touchProcessID 改或读取一处引用；空串不算。
func touchProcessID(obj map[string]any, key string, fn func(string) (string, error)) error {
	// 先当还没找到，找到再改成命中。
	v, ok := obj[key]
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return nil
	}
	// 只接受字符串形式的工艺身份。
	s, err := asProcessID(v)
	// 没能只接受字符串形式的工艺身份就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 文本是空的就按没有来处理。
	if s == "" {
		return nil
	}
	// 做完这一步，再交给后面。
	next, err := fn(s)
	// 这一步没做成就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 收出来的和原来不同才覆盖。
	if next != s {
		// 把这个值定下来，后面的判断才有依据。
		obj[key] = next
	}
	return nil
}

// asProcessID 只收字符串身份。
func asProcessID(v any) (string, error) {
	// 按实际类型收下，对不上的另作处理。
	switch x := v.(type) {
	// 空值按没有内容来处理。
	case nil:
		return "", nil
	// 文本按字符串收下。
	case string:
		// 交回去掉两头空白再使用的结果。
		return strings.TrimSpace(x), nil
	// 不是字符串就不是工艺身份。
	default:
		return "", errBadProcessID
	}
}
