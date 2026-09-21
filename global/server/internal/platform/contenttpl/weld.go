package contenttpl

import "encoding/json"

const (
	WeldSingle     = "single"     // 单层焊道
	WeldMultilayer = "multilayer" // 多层焊缝
	WeldTBar       = "tbar"       // T排对接
)

// WeldKindOfTemplate 空库三份模版对应的作业类型；自定义份没有。
func WeldKindOfTemplate(id string) (string, bool) {
	switch id {
	case SeedTplSingle:
		return WeldSingle, true
	case SeedTplMulti:
		return WeldMultilayer, true
	case SeedTplTBar:
		return WeldTBar, true
	default:
		return "", false
	}
}

// weldKindOfItemKind 工程焊缝种类对应的作业类型；附加工艺没有。
func weldKindOfItemKind(kind string) (string, bool) {
	switch kind {
	case ItemSingle:
		return WeldSingle, true
	case ItemMulti:
		return WeldMultilayer, true
	case ItemTBar:
		return WeldTBar, true
	default:
		return "", false
	}
}

// InferWeldKind 按工程正文第一条能认的模版/种类推断作业类型；认不出当单层。
func InferWeldKind(content []byte) string {
	var items []map[string]any
	if err := json.Unmarshal(content, &items); err != nil {
		return WeldSingle
	}
	for _, it := range items {
		if id, _ := it["templateId"].(string); id != "" {
			if k, ok := WeldKindOfTemplate(id); ok {
				return k
			}
		}
		kind, _ := it["kind"].(string)
		if k, ok := weldKindOfItemKind(kind); ok {
			return k
		}
	}
	return WeldSingle
}

// ContentMatchesWeldKind 工程正文里能认的模版/种类必须和作业类型一致；认不出的自定义份放过。
func ContentMatchesWeldKind(weldKind string, content []byte) bool {
	var items []map[string]any
	if err := json.Unmarshal(content, &items); err != nil {
		return true
	}
	for _, it := range items {
		id, _ := it["templateId"].(string)
		if got, ok := WeldKindOfTemplate(id); ok && got != weldKind {
			return false
		}
		kind, _ := it["kind"].(string)
		if got, ok := weldKindOfItemKind(kind); ok && got != weldKind {
			return false
		}
	}
	return true
}
