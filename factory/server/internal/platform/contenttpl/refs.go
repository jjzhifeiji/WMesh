package contenttpl

import (
	"encoding/json"
	"fmt"
	"strings"
)

const processRefKey = "processId" // 旧模版：此键的文本仍当工艺引用

var errBadProcessID = fmt.Errorf("process id is not an identity string")     // 引用不是字符串身份
var errUnknownProcessID = fmt.Errorf("process id is not in the rewrite map") // 对照表没有这条

// CollectProcessIDs 按工程模版收集非空工艺引用；非 JSON 或根类型对不上当没有引用。
func CollectProcessIDs(schemaJSON, content []byte) ([]string, error) {
	s, err := Parse(schemaJSON)
	if err != nil {
		return nil, err
	}
	v, ok := parseJSON(content)
	if !ok {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var ids []string
	fn := func(id string) (string, error) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return id, nil
	}
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// CollectProcessIDsFromItems 按各份自己的字段表收集工艺引用。
func CollectProcessIDsFromItems(items []ProjectItemSchema, content []byte) ([]string, error) {
	v, ok := parseJSON(content)
	if !ok {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var ids []string
	fn := func(id string) (string, error) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return id, nil
	}
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	return ids, nil
}

// RewriteProcessIDs 按工程模版改写工艺引用；找不到则失败；其它键不动。
func RewriteProcessIDs(schemaJSON, content []byte, idMap map[string]string) ([]byte, error) {
	s, err := Parse(schemaJSON)
	if err != nil {
		return nil, err
	}
	v, ok := parseJSON(content)
	if !ok {
		return content, nil
	}
	changed := false
	fn := func(id string) (string, error) {
		next, ok := idMap[id]
		if !ok {
			return "", errUnknownProcessID
		}
		if next != id {
			changed = true
		}
		return next, nil
	}
	if err := walkRoot(s, v, fn); err != nil {
		return nil, err
	}
	if !changed {
		return content, nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RewriteProcessIDsFromItems 按各份字段表改写工艺引用；其它键不动。
func RewriteProcessIDsFromItems(items []ProjectItemSchema, content []byte, idMap map[string]string) ([]byte, error) {
	v, ok := parseJSON(content)
	if !ok {
		return content, nil
	}
	changed := false
	fn := func(id string) (string, error) {
		next, ok := idMap[id]
		if !ok {
			return "", errUnknownProcessID
		}
		if next != id {
			changed = true
		}
		return next, nil
	}
	if err := walkProjectItemList(items, v, fn); err != nil {
		return nil, err
	}
	if !changed {
		return content, nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseJSON 读不成 JSON 则不当工程正文。
func parseJSON(content []byte) (any, bool) {
	var v any
	if err := json.Unmarshal(content, &v); err != nil {
		return nil, false
	}
	return v, true
}

// isProcessRef 类型为工艺即引用；旧模版键 processId 的文本同样算。
func isProcessRef(f Field) bool {
	if len(f.Options) > 0 {
		return false
	}
	if f.Type == TypeProcess {
		return true
	}
	return f.Type == TypeString && f.Key == processRefKey
}

// walkRoot 按模版根类型走正文。
func walkRoot(s Schema, v any, fn func(string) (string, error)) error {
	switch s.Root {
	case RootArray:
		if len(s.Templates) > 0 || (s.Item == nil && len(s.Kinds) == 0) {
			return walkProjectItems(s, v, fn)
		}
		if len(s.Kinds) > 0 {
			return walkObjectList(SelectedFields(s.Kinds), v, fn)
		}
		if s.Item == nil {
			return nil
		}
		return walkArray(*s.Item, v, fn)
	case RootObject:
		return walkObject(s.Fields, v, fn)
	default:
		return nil
	}
}

// walkProjectItems 数组里每个元素按它对应的命名模版走。
func walkProjectItems(s Schema, v any, fn func(string) (string, error)) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	for _, el := range arr {
		tpl, ok := lookupTemplate(s, el)
		if !ok {
			continue
		}
		extra := tpl.Extra && tpl.Kind != ItemMulti
		if err := walkObject(ItemFields(tpl.Kind, extra), el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkProjectItemList 有 templateId 走那一份；没有则按各份字段并集走（旧正文升档）。
func walkProjectItemList(items []ProjectItemSchema, v any, fn func(string) (string, error)) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	byID := map[string][]Field{}
	var union []Field
	seen := map[string]struct{}{}
	for _, it := range items {
		byID[it.ID] = it.Fields
		for _, f := range it.Fields {
			if _, ok := seen[f.Key]; ok {
				continue
			}
			seen[f.Key] = struct{}{}
			union = append(union, f)
		}
	}
	for _, el := range arr {
		obj, _ := el.(map[string]any)
		if obj == nil {
			continue
		}
		id, _ := obj["templateId"].(string)
		fields, ok := byID[id]
		if !ok {
			fields = union
		}
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObjectList 数组里每个对象按同一份字段表走。
func walkObjectList(fields []Field, v any, fn func(string) (string, error)) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	for _, el := range arr {
		if err := walkObject(fields, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkObject 只走模版里的键。
func walkObject(fields []Field, v any, fn func(string) (string, error)) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, f := range fields {
		if isProcessRef(f) {
			if err := touchProcessID(obj, f.Key, fn); err != nil {
				return err
			}
			continue
		}
		switch f.Type {
		case TypeObject:
			if err := walkObject(f.Fields, obj[f.Key], fn); err != nil {
				return err
			}
		case TypeArray:
			if f.Items == nil {
				continue
			}
			if err := walkArray(*f.Items, obj[f.Key], fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkArray 数组元素跟模版 Items。
func walkArray(item Field, v any, fn func(string) (string, error)) error {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	for i, el := range arr {
		if isProcessRef(item) {
			s, err := asProcessID(el)
			if err != nil {
				return err
			}
			if s == "" {
				continue
			}
			next, err := fn(s)
			if err != nil {
				return err
			}
			if next != s {
				arr[i] = next
			}
			continue
		}
		if err := walkField(item, el, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkField 对象或再套数组。
func walkField(f Field, v any, fn func(string) (string, error)) error {
	switch f.Type {
	case TypeObject:
		return walkObject(f.Fields, v, fn)
	case TypeArray:
		if f.Items == nil {
			return nil
		}
		return walkArray(*f.Items, v, fn)
	default:
		return nil
	}
}

// touchProcessID 改或读取一处引用；空串不算。
func touchProcessID(obj map[string]any, key string, fn func(string) (string, error)) error {
	v, ok := obj[key]
	if !ok {
		return nil
	}
	s, err := asProcessID(v)
	if err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	next, err := fn(s)
	if err != nil {
		return err
	}
	if next != s {
		obj[key] = next
	}
	return nil
}

// asProcessID 只收字符串身份。
func asProcessID(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(x), nil
	default:
		return "", errBadProcessID
	}
}
