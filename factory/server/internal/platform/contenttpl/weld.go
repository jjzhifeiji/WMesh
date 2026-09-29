package contenttpl

import "encoding/json"

const (
	WeldSingle     = "single"     // 单层焊道
	WeldMultilayer = "multilayer" // 多层焊缝
	WeldTBar       = "tbar"       // T排对接
)

// WeldKindOfTemplate 空库三份模版对应的作业类型；自定义份没有。
func WeldKindOfTemplate(id string) (string, bool) {
	// 按种类或身份分路，对不上就走默认。
	switch id {
	// 这份模版身份对应单层。
	case SeedTplSingle:
		return WeldSingle, true
	// 这份模版身份对应多层。
	case SeedTplMulti:
		return WeldMultilayer, true
	// 这份模版身份对应丁字排。
	case SeedTplTBar:
		return WeldTBar, true
	// 不认识的模版就没有焊缝种类。
	default:
		return "", false
	}
}

// weldKindOfItemKind 工程焊缝种类对应的作业类型；附加工艺没有。
func weldKindOfItemKind(kind string) (string, bool) {
	// 按种类或身份分路，对不上就走默认。
	switch kind {
	// 单层走单层那一组字段。
	case ItemSingle:
		return WeldSingle, true
	// 多层走多层那一组字段。
	case ItemMulti:
		return WeldMultilayer, true
	// 丁字排走丁字排那一组字段。
	case ItemTBar:
		return WeldTBar, true
	// 不认识的种类就没有焊缝种类。
	default:
		return "", false
	}
}

// InferWeldKind 按工程正文第一条能认的模版/种类推断作业类型；认不出当单层。
func InferWeldKind(content []byte) string {
	// 准备承接解出来的对象。
	var items []map[string]any
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(content, &items); err != nil {
		return WeldSingle
	}
	// 逐项处理，空列表就直接跳过。
	for _, it := range items {
		// 有内容才继续处理这一支。
		if id, _ := it["templateId"].(string); id != "" {
			// 条件不成立就换一路，避免误往下做。
			if k, ok := WeldKindOfTemplate(id); ok {
				return k
			}
		}
		// 收成文本，对不上就当这个槽是空的。
		kind, _ := it["kind"].(string)
		// 条件不成立就换一路，避免误往下做。
		if k, ok := weldKindOfItemKind(kind); ok {
			return k
		}
	}
	return WeldSingle
}

// ContentMatchesWeldKind 工程正文里能认的模版/种类必须和作业类型一致；认不出的自定义份放过。
func ContentMatchesWeldKind(weldKind string, content []byte) bool {
	// 准备承接解出来的对象。
	var items []map[string]any
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(content, &items); err != nil {
		return true
	}
	// 逐项处理，空列表就直接跳过。
	for _, it := range items {
		// 收成文本，对不上就当这个槽是空的。
		id, _ := it["templateId"].(string)
		// 对不上就换一路，避免把不符的当成通过。
		if got, ok := WeldKindOfTemplate(id); ok && got != weldKind {
			return false
		}
		// 收成文本，对不上就当这个槽是空的。
		kind, _ := it["kind"].(string)
		// 对不上就换一路，避免把不符的当成通过。
		if got, ok := weldKindOfItemKind(kind); ok && got != weldKind {
			return false
		}
	}
	return true
}
