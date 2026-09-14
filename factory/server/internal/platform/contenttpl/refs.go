package contenttpl

import (
	"encoding/json"
	"fmt"
	"strings"
)

const processRefKey = "processId" // 工程模版里此键的文本字段即工艺引用

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

// parseJSON 读不成 JSON 则不当工程正文。
func parseJSON(content []byte) (any, bool) {
	var v any
	if err := json.Unmarshal(content, &v); err != nil {
		return nil, false
	}
	return v, true
}

// isProcessRef 模版文本字段键为 processId 即工艺引用，枚举不算。
func isProcessRef(f Field) bool {
	return f.Key == processRefKey && f.Type == TypeString && len(f.Options) == 0
}

// walkRoot 按模版根类型走正文。
func walkRoot(s Schema, v any, fn func(string) (string, error)) error {
	switch s.Root {
	case RootArray:
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
