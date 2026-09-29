package contenttpl

import "encoding/json"

const (
	WeldSingle     = "single"     // 单层焊道
	WeldMultilayer = "multilayer" // 多层焊缝
	WeldTBar       = "tbar"       // T排对接
)

// WeldKindOfTemplate 空库三份模版对应的作业类型；自定义份没有。
func WeldKindOfTemplate(id string) (string, bool) {
	// 只认空库三份模版，自定义份没有作业类型。
	switch id {
	// 单层模版对单层焊道。
	case SeedTplSingle:
		return WeldSingle, true
	// 多层模版对多层焊缝。
	case SeedTplMulti:
		return WeldMultilayer, true
	// T 排模版对 T 排对接。
	case SeedTplTBar:
		return WeldTBar, true
	// 其余模版不推断作业类型。
	default:
		return "", false
	}
}

// weldKindOfItemKind 工程焊缝种类对应的作业类型；附加工艺没有。
func weldKindOfItemKind(kind string) (string, bool) {
	// 只认三种焊缝种类，附加工艺没有作业类型。
	switch kind {
	// 单层焊缝对单层焊道。
	case ItemSingle:
		return WeldSingle, true
	// 多层焊缝对上多层作业。
	case ItemMulti:
		return WeldMultilayer, true
	// T 排焊缝对 T 排对接。
	case ItemTBar:
		return WeldTBar, true
	// 其余种类不推断作业类型。
	default:
		return "", false
	}
}

// InferWeldKind 按工程正文第一条能认的模版/种类推断作业类型；认不出当单层。
func InferWeldKind(content []byte) string {
	// 准备接工程正文里的各份。
	var items []map[string]any
	// 不是数组就认不出，按单层。
	if err := json.Unmarshal(content, &items); err != nil {
		return WeldSingle
	}
	// 从第一条能认的份开始推断。
	for _, it := range items {
		// 有模版身份就先按模版认。
		if id, _ := it["templateId"].(string); id != "" {
			// 空库三份一对一，认到就停。
			if k, ok := WeldKindOfTemplate(id); ok {
				return k
			}
		}
		// 没有模版再看焊缝种类。
		kind, _ := it["kind"].(string)
		// 种类能认也停在这一份。
		if k, ok := weldKindOfItemKind(kind); ok {
			return k
		}
	}
	return WeldSingle
}

// ContentMatchesWeldKind 工程正文里能认的模版/种类必须和作业类型一致；认不出的自定义份放过。
func ContentMatchesWeldKind(weldKind string, content []byte) bool {
	// 准备接工程正文里的各份。
	var items []map[string]any
	// 不是数组就无法核对，先放过。
	if err := json.Unmarshal(content, &items); err != nil {
		return true
	}
	// 每一份能认的都要和作业类型一致。
	for _, it := range items {
		// 取出模版身份，没有就跳过这一判。
		id, _ := it["templateId"].(string)
		// 模版认得出却对不上，整篇不一致。
		if got, ok := WeldKindOfTemplate(id); ok && got != weldKind {
			return false
		}
		// 再取出这一份的焊缝种类。
		kind, _ := it["kind"].(string)
		// 种类认得出却对不上，整篇不一致。
		if got, ok := weldKindOfItemKind(kind); ok && got != weldKind {
			return false
		}
	}
	return true
}
