package contenttpl

import (
	"bytes"
	"encoding/json"
)

// 示教器生成包角焊道时把层参数写进工程正文（重算和焊接要用），加密包角也一样；来源工艺不记在正文里，分不出哪组加密。
// 所以给人读的入口一律遮掉包角组的几何和生成的点列，只有示教器闭包里是全的（2026-11-07 查入口时定）。

// cornerGeometryKeys 是包角组里遮掉的键：层数、长度、偏移和逐层几何。
var cornerGeometryKeys = []string{"layerCount", "initialLength", "upwardOffset", "lengthReduction", "firstPassLength", "passes"}

// cornerGroupKey 是焊道上的包角组键。
const cornerGroupKey = "cornerGroupParams"

// cornerPointsKey 是焊道上的点列键；包角焊道的点是按层参数生成的，看点能倒推层参数。
const cornerPointsKey = "points"

// decodeWelds 解开工程正文的焊道列表；数字按原文留着，不是列表就交回否。
func decodeWelds(content []byte) ([]any, bool) {
	// 没写到包角组就不用解，普通工程字节不动。
	if !bytes.Contains(content, []byte(cornerGroupKey)) {
		return nil, false
	}
	// 数字不转浮点，免得重写时走样。
	dec := json.NewDecoder(bytes.NewReader(content))
	// 大数和小数都按原文。
	dec.UseNumber()
	// 准备承接焊道列表。
	var welds []any
	// 不是列表就不管。
	if err := dec.Decode(&welds); err != nil {
		return nil, false
	}
	return welds, true
}

// encodeWelds 把焊道列表写回；名字里的尖括号不转义，和收到的一样。
func encodeWelds(welds []any) ([]byte, error) {
	// 准备接结果。
	var buf bytes.Buffer
	// 编码器才能关转义。
	enc := json.NewEncoder(&buf)
	// 不转义尖括号和与号。
	enc.SetEscapeHTML(false)
	// 写失败就交回。
	if err := enc.Encode(welds); err != nil {
		return nil, err
	}
	// 去掉编码器补的换行。
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// cornerGeometryWeld 这条焊道是否带包角几何；多层和 T 排的包角组只有工艺引用，不算。
func cornerGeometryWeld(el any) (map[string]any, map[string]any, bool) {
	// 焊道得是对象。
	weld, ok := el.(map[string]any)
	// 不是对象就不管。
	if !ok {
		return nil, nil, false
	}
	// 包角组也得是对象。
	group, ok := weld[cornerGroupKey].(map[string]any)
	// 没有包角组就不管。
	if !ok {
		return nil, nil, false
	}
	// 写到任一几何键才算。
	for _, k := range cornerGeometryKeys {
		// 有这个键就算。
		if _, has := group[k]; has {
			return weld, group, true
		}
	}
	return nil, nil, false
}

// HasCornerGeometry 工程正文里有没有包角几何。
func HasCornerGeometry(content []byte) bool {
	// 解不开就当没有。
	welds, ok := decodeWelds(content)
	// 不是焊道列表。
	if !ok {
		return false
	}
	// 逐条看。
	for _, el := range welds {
		// 有一条就算。
		if _, _, hit := cornerGeometryWeld(el); hit {
			return true
		}
	}
	return false
}

// MaskCornerGeometry 遮掉包角组的几何和生成的点列，给人读的入口用；没有包角几何就原样交回，字节不动。
func MaskCornerGeometry(content []byte) []byte {
	// 解不开就原样交回。
	welds, ok := decodeWelds(content)
	// 不是焊道列表。
	if !ok {
		return content
	}
	// 记下有没有遮过。
	changed := false
	// 逐条遮。
	for _, el := range welds {
		// 取这条的包角组。
		weld, group, hit := cornerGeometryWeld(el)
		// 不带几何的不动。
		if !hit {
			continue
		}
		// 几何键都删。
		for _, k := range cornerGeometryKeys {
			// 删掉这个键。
			delete(group, k)
		}
		// 点列留空数组，页面照常画这条焊道。
		weld[cornerPointsKey] = []any{}
		// 遮过了。
		changed = true
	}
	// 没遮就原样交回。
	if !changed {
		return content
	}
	// 写回遮后的正文。
	out, err := encodeWelds(welds)
	// 写不回就什么都不给，遮不掉不能交明文。
	if err != nil {
		// 交一份空列表，页面当没有焊道。
		return []byte("[]")
	}
	return out
}

// RestoreCornerGeometry 网页保存时把遮掉的几何和点列按焊道身份从现存正文盖回：网页拿不到也改不了这些，
// 不管它送来什么都以现存为准。现存没有包角几何就原样交回；对不上身份的焊道不动。
func RestoreCornerGeometry(stored, incoming []byte) []byte {
	// 现存没有包角就不用看。
	old, ok := decodeWelds(stored)
	// 不是焊道列表。
	if !ok {
		return incoming
	}
	// 按焊道身份收现存的包角焊道。
	byID := map[string]map[string]any{}
	// 逐条收。
	for _, el := range old {
		// 不带几何的不收。
		weld, _, hit := cornerGeometryWeld(el)
		// 没有几何。
		if !hit {
			continue
		}
		// 取焊道身份。
		id, _ := weld["id"].(string)
		// 有身份才对得上。
		if id != "" {
			// 记下这条。
			byID[id] = weld
		}
	}
	// 一条都没有就原样交回。
	if len(byID) == 0 {
		return incoming
	}
	// 解开送来的正文。
	welds, ok := decodeWelds(incoming)
	// 解不开交给后面的校验。
	if !ok {
		return incoming
	}
	// 记下有没有盖回。
	changed := false
	// 逐条盖回。
	for _, el := range welds {
		// 焊道得是对象。
		weld, isObj := el.(map[string]any)
		// 不是对象不管。
		if !isObj {
			continue
		}
		// 取焊道身份。
		id, _ := weld["id"].(string)
		// 现存对应的包角焊道。
		src, found := byID[id]
		// 没有对应的不管。
		if !found {
			continue
		}
		// 送来的包角组。
		group, isGroup := weld[cornerGroupKey].(map[string]any)
		// 网页去掉了包角组就不补。
		if !isGroup {
			continue
		}
		// 现存的包角组，上面判过一定是对象。
		srcGroup := src[cornerGroupKey].(map[string]any)
		// 几何键一律以现存为准。
		for _, k := range cornerGeometryKeys {
			// 现存有就盖回。
			if v, has := srcGroup[k]; has {
				// 盖回这个键。
				group[k] = v
				continue
			}
			// 现存没有就别留送来的。
			delete(group, k)
		}
		// 点列也以现存为准。
		weld[cornerPointsKey] = src[cornerPointsKey]
		// 盖回过了。
		changed = true
	}
	// 没盖就原样交回。
	if !changed {
		return incoming
	}
	// 写回盖好的正文。
	out, err := encodeWelds(welds)
	// 写不回就原样交回，交给后面的校验。
	if err != nil {
		return incoming
	}
	return out
}
