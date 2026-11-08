package contenttpl

import (
	"bytes"
	"encoding/json"
	"errors"
)

// FillMissing 只补键不删键：以原正文为准，把套过模版的结果里原正文没有的键补进来；
// 原正文写了的键和值原样留着，多出来的键也不删。示教器新建时用它，免得示教器自己的字段被模版吃掉。
// 原正文不是合法 JSON、或一个键都没补，就把原字节原样交回，摘要不变。
func FillMissing(original, applied []byte) ([]byte, error) {
	// 准备接原正文。
	src, err := decodeKeepNumber(original)
	// 原正文解不开就不动它。
	if err != nil {
		return original, nil
	}
	// 套过的结果按同样口径解开。
	tpl, err := decodeKeepNumber(applied)
	// 套过的结果解不开就不补。
	if err != nil {
		return original, nil
	}
	// 逐层补缺的键。
	out, added := fillValue(src, tpl)
	// 一个键都没补就交回原字节，免得只因重排键序改了摘要。
	if !added {
		return original, nil
	}
	// 补过了再收成正文。
	return json.Marshal(out)
}

// decodeKeepNumber 解成通用值，数字按原文留着，免得补键时把 177.0 写成 177、大整数丢精度。
func decodeKeepNumber(raw []byte) (any, error) {
	// 按原文读数字。
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	// 数字留成原文，不转浮点。
	dec.UseNumber()
	// 准备接解出来的值。
	var v any
	// 解不开就交回错误。
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// 后面还有多余内容不算合法正文。
	if dec.More() {
		return nil, errTrailingJSON
	}
	return v, nil
}

// errTrailingJSON 正文后面还跟着多余内容。
var errTrailingJSON = errors.New("trailing data after json")

// fillValue 对象逐键补缺、递归往下；数组等长才逐项补；其余一律留原值。第二个返回值标明补过键。
func fillValue(src, tpl any) (any, bool) {
	// 按原值的形状分路。
	switch sv := src.(type) {
	// 对象：缺的键从模版结果补，两边都有的往下递归。
	case map[string]any:
		// 模版结果也得是对象才补。
		tv, ok := tpl.(map[string]any)
		// 形状对不上就留原值。
		if !ok {
			return sv, false
		}
		// 先当没补过。
		added := false
		// 逐键看模版结果。
		for k, v := range tv {
			// 原正文已有这个键就往下补，值本身不换。
			if cur, has := sv[k]; has {
				// 往下一层补缺，记下有没有补。
				next, deeper := fillValue(cur, v)
				// 写回这一键。
				sv[k] = next
				// 下层补过也算补过。
				added = added || deeper
				continue
			}
			// 原正文没有就补上模版给的值。
			sv[k] = v
			// 记下补过。
			added = true
		}
		return sv, added
	// 数组：等长才逐项补，长度对不上说明模版丢过项，留原值。
	case []any:
		// 模版结果也得是数组。
		tv, ok := tpl.([]any)
		// 形状或长度对不上就留原值。
		if !ok || len(tv) != len(sv) {
			return sv, false
		}
		// 先当没补过。
		added := false
		// 逐项往下补。
		for i := range sv {
			// 这一项按同位置的模版结果补。
			next, deeper := fillValue(sv[i], tv[i])
			// 写回这一项。
			sv[i] = next
			// 有一项补过就算补过。
			added = added || deeper
		}
		return sv, added
	// 标量和空值都留原值。
	default:
		return src, false
	}
}
