package contenttpl

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ItemSingle = "single" // 单层焊道
	ItemMulti  = "multi"  // 多层焊缝
	ItemTBar   = "tbar"   // T排对接
	ItemExtra  = "extra"  // 旧种类表：附加工艺开关
)

const (
	SeedTplSingle    = "11111111-1111-4111-8111-111111111111" // 空库单层模版身份
	SeedTplMulti     = "22222222-2222-4222-8222-222222222222" // 空库多层模版身份
	SeedTplTBar      = "44444444-4444-4444-8444-444444444444" // 空库 T 排模版身份
	defaultTplSingle = SeedTplSingle                          // 与 SeedTplSingle 相同
	defaultTplMulti  = SeedTplMulti                           // 与 SeedTplMulti 相同
	maxProjectTpls   = 50                                     // 命名模版份数上限
	maxTplNameRunes  = 80                                     // 名称字数上限
)

// CatalogKinds 旧种类表顺序；新模版不再把 extra 当独立份。
var CatalogKinds = []string{ItemSingle, ItemMulti, ItemExtra}

// 模版身份必须是这种分段的十六进制。
var tplIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ProjectTemplate 一份命名工程模版：独立，不依赖其它份。
type ProjectTemplate struct {
	ID    string `json:"id"`    // 这份模版的身份
	Name  string `json:"name"`  // 给人看的名称
	Kind  string `json:"kind"`  // single / multi / tbar
	Extra bool   `json:"extra"` // 是否带附加工艺槽
}

// ProjectItemSchema 一份独立工程模版：对象字段表。
type ProjectItemSchema struct {
	ID     string  // 这份模版的身份
	Name   string  // 给人看的名称
	Fields []Field // 这份自己的字段
}

// SeedProjectItems 空库三份，字段是现在各份自己的明细。
func SeedProjectItems() []ProjectItemSchema {
	return []ProjectItemSchema{
		{ID: SeedTplSingle, Name: "单层焊道", Fields: itemSingle(true)},
		{ID: SeedTplMulti, Name: "多层焊缝", Fields: itemMulti()},
		{ID: SeedTplTBar, Name: "T排对接", Fields: itemTBar()},
	}
}

// SeedProjectItem 按空库身份取一份明细；没有则 false。
func SeedProjectItem(id string) (ProjectItemSchema, bool) {
	// 逐项处理，空的就不进入循环。
	for _, it := range SeedProjectItems() {
		// 对上了才走这一路，其余分开处理。
		if it.ID == id {
			return it, true
		}
	}
	return ProjectItemSchema{}, false
}

// LegacyProjectItems 已有库拆行用的两份，不含 T 排。
func LegacyProjectItems() []ProjectItemSchema {
	// 准备放下输出，再交给后面。
	var out []ProjectItemSchema
	// 逐项处理，空的就不进入循环。
	for _, it := range SeedProjectItems() {
		// 对上了才走这一路，其余分开处理。
		if it.ID == SeedTplTBar {
			continue
		}
		// 把这一段接进结果，顺序要保持住。
		out = append(out, it)
	}
	return out
}

// ObjectSchema 把一份字段表收成对象根。
func ObjectSchema(fields []Field) Schema {
	return Schema{Root: RootObject, Fields: fields}
}

// ExpandLegacyProject 旧登记簿拆成独立份；对不上则用已有三份，不插入 T 排。
func ExpandLegacyProject(s Schema) []ProjectItemSchema {
	// 对不上就换一路，避免把不符的当成通过。
	if s.Root != RootArray {
		return nil
	}
	// 有命名模版就按工程条目来处理。
	if len(s.Templates) > 0 {
		// 按需要的长度把缓冲准备好。
		out := make([]ProjectItemSchema, 0, len(s.Templates))
		// 逐项处理，空的就不进入循环。
		for _, t := range s.Templates {
			// 不是根项的旧种类（如包角）拆行时丢掉。
			if !IsRootItem(t.Kind) {
				continue
			}
			// 把这一段接进结果，顺序要保持住。
			out = append(out, ProjectItemSchema{ID: t.ID, Name: t.Name, Fields: ItemFields(t.Kind, t.Extra && t.Kind != ItemMulti)})
		}
		return out
	}
	// 点了种类才按种类去收字段。
	if len(s.Kinds) > 0 {
		// 看这种条目有没有被选中。
		extra := HasKind(s.Kinds, ItemExtra)
		// 按需要的长度把缓冲准备好。
		out := make([]ProjectItemSchema, 0, 3)
		// 逐项处理，空的就不进入循环。
		for _, seed := range LegacyProjectItems() {
			// 种子种类，再交给后面。
			kind := seedKind(seed.ID)
			// 这种没被选中就不要它的字段。
			if !HasKind(s.Kinds, kind) {
				continue
			}
			// 接上种类，原文里才分得清是什么包。
			out = append(out, ProjectItemSchema{ID: seed.ID, Name: seed.Name, Fields: ItemFields(kind, extra && kind != ItemMulti)})
		}
		// 有内容才继续，空的这一支跳过。
		if len(out) > 0 {
			return out
		}
	}
	// 交回旧记录工程条目，再交给后面的结果。
	return LegacyProjectItems()
}

// defaultProject 旧登记簿形状，只给拆行用。
func defaultProject() Schema {
	return Schema{Root: RootArray, Templates: []ProjectTemplate{
		{ID: defaultTplSingle, Name: "单层焊道", Kind: ItemSingle, Extra: true},
		{ID: defaultTplMulti, Name: "多层焊缝", Kind: ItemMulti, Extra: false},
	}}
}

// validateTemplates 名称必填且不重复，种类只许根项，身份不重复。
func validateTemplates(list []ProjectTemplate) error {
	// 条件不成立就换一路，避免误往下做。
	if len(list) > maxProjectTpls {
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
	// 用空表记下见过的键，避免重复计入。
	ids := map[string]struct{}{}
	// 用空表记下见过的键，避免重复计入。
	names := map[string]struct{}{}
	// 逐项处理，空的就不进入循环。
	for _, t := range list {
		// 去掉两头空白再使用。
		name := strings.TrimSpace(t.Name)
		// 名字是空的就不能再继续。
		if name == "" || utf8.RuneCountInString(name) > maxTplNameRunes || !tplIDRe.MatchString(t.ID) || !IsRootItem(t.Kind) {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 条件不成立就换一路，避免误往下做。
		if _, ok := ids[t.ID]; ok {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 条件不成立就换一路，避免误往下做。
		if _, ok := names[name]; ok {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 定下身份列表，再交给后面。
		ids[t.ID] = struct{}{}
		// 定下文件名列表，再交给后面。
		names[name] = struct{}{}
	}
	return nil
}

// validateKinds 旧种类表：至少一种根项，不重复、不超出目录。
func validateKinds(kinds []string) error {
	// 数量是零就按没有来处理。
	if len(kinds) == 0 || len(kinds) > len(CatalogKinds) {
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 先把这一步的结果放下，后面还要用。
	root := false
	// 逐项处理，空的就不进入循环。
	for _, k := range kinds {
		// 不满足就停住或跳过，避免做错下一步。
		if !isCatalogKind(k) {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 条件不成立就换一路，避免误往下做。
		if _, ok := seen[k]; ok {
			// 字段表不合法，不能拿去套。
			return fmt.Errorf("content template is invalid")
		}
		// 定下已经见过，再交给后面。
		seen[k] = struct{}{}
		// 对不上就换一路，避免把不符的当成通过。
		if k != ItemExtra {
			// 把这个值定下来，后面的判断才有依据。
			root = true
		}
	}
	// 不满足就停住或跳过，避免做错下一步。
	if !root {
		// 字段表不合法，不能拿去套。
		return fmt.Errorf("content template is invalid")
	}
	return nil
}

// isCatalogKind 是否旧种类表里的取值。
func isCatalogKind(kind string) bool {
	// 逐项处理，空的就不进入循环。
	for _, k := range CatalogKinds {
		// 种类对上了才采用这一份。
		if k == kind {
			return true
		}
	}
	return false
}

// HasKind 旧种类表是否选用了该种类。
func HasKind(kinds []string, kind string) bool {
	// 逐项处理，空的就不进入循环。
	for _, k := range kinds {
		// 种类对上了才采用这一份。
		if k == kind {
			return true
		}
	}
	return false
}

// IsRootItem 单层/多层/T 排可作焊缝数组元素。
func IsRootItem(kind string) bool {
	return kind == ItemSingle || kind == ItemMulti || kind == ItemTBar
}

// seedKind 空库身份对应的根种类。
func seedKind(id string) string {
	// 按种类或身份分路，对不上就走默认。
	switch id {
	// 这份模版身份对应多层。
	case SeedTplMulti:
		return ItemMulti
	// 这份模版身份对应丁字排。
	case SeedTplTBar:
		return ItemTBar
	// 其余取值走这里，避免漏掉没点名的情况。
	default:
		return ItemSingle
	}
}

// ItemFields 某一种基础项的字段；extra 打开才带附加工艺槽。
func ItemFields(kind string, extra bool) []Field {
	// 按种类或身份分路，对不上就走默认。
	switch kind {
	// 单层走单层那一组字段。
	case ItemSingle:
		// 交回取出单层条目的字段的结果。
		return itemSingle(extra)
	// 多层走多层那一组字段。
	case ItemMulti:
		// 交回取出多层条目的字段的结果。
		return itemMulti()
	// 丁字排走丁字排那一组字段。
	case ItemTBar:
		// 交回取出丁字排条目的字段的结果。
		return itemTBar()
	// 不认识的种类就没有字段可给。
	default:
		return nil
	}
}

// SelectedFields 旧种类表字段并集，给收集/改写工艺引用用。
func SelectedFields(kinds []string) []Field {
	// 看这种条目有没有被选中。
	extra := HasKind(kinds, ItemExtra)
	// 准备放下输出，再交给后面。
	var out []Field
	// 用空表记下见过的键，避免重复计入。
	seen := map[string]struct{}{}
	// 把这份字段并进集合，同一个键只留一次。
	add := func(fields []Field) {
		// 按字段逐项处理，多出来的键不要。
		for _, f := range fields {
			// 这个键已经收过就跳过。
			if _, ok := seen[f.Key]; ok {
				continue
			}
			// 定下已经见过，再交给后面。
			seen[f.Key] = struct{}{}
			// 把这一段接进结果，顺序要保持住。
			out = append(out, f)
		}
	}
	// 条件不成立就换一路，避免误往下做。
	if HasKind(kinds, ItemSingle) {
		// 取出单层条目的字段。
		add(itemSingle(extra))
	}
	// 条件不成立就换一路，避免误往下做。
	if HasKind(kinds, ItemMulti) {
		// 取出多层条目的字段。
		add(itemMulti())
	}
	return out
}

// InferItemKind 读正文上的 kind；没有则按形状判断。
func InferItemKind(v any) string {
	// 收成对象再往下看，对不上就当不是对象。
	obj, ok := v.(map[string]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return ItemSingle
	}
	// 条件不成立就换一路，避免误往下做。
	if k, ok := obj["kind"].(string); ok && IsRootItem(k) {
		return k
	}
	// 条件不成立就换一路，避免误往下做。
	if _, ok := obj["basePath"]; ok {
		return ItemMulti
	}
	// 条件不成立就换一路，避免误往下做。
	if _, ok := obj["passes"]; ok {
		return ItemMulti
	}
	// 条件不成立就换一路，避免误往下做。
	if _, ok := obj["gapBands"]; ok {
		return ItemTBar
	}
	// 条件不成立就换一路，避免误往下做。
	if hasGroovePoint(obj) {
		return ItemTBar
	}
	return ItemSingle
}

// hasGroovePoint 点列里有坡口四点则当 T 排。
func hasGroovePoint(obj map[string]any) bool {
	// 收成数组再往下走，对不上就当不是数组。
	arr, _ := obj["points"].([]any)
	// 逐个元素套用，空数组就保持空。
	for _, el := range arr {
		// 收成对象再往下看，对不上就当不是对象。
		pt, _ := el.(map[string]any)
		// 收成文本，对不上就当这个槽是空的。
		typ, _ := pt["type"].(string)
		// 按字段类型分路，不同类型不能混用。
		switch typ {
		// 这几种坡口都算坡口槽。
		case "GROOVE_A_LOWER", "GROOVE_B_LOWER", "GROOVE_A_UPPER", "GROOVE_B_UPPER":
			return true
		}
	}
	return false
}

// lookupTemplate 按 templateId，没有则按种类对上第一份。
func lookupTemplate(s Schema, el any) (ProjectTemplate, bool) {
	// 收成对象再往下看，对不上就当不是对象。
	obj, ok := el.(map[string]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return ProjectTemplate{}, false
	}
	// 有内容才继续处理这一支。
	if id, _ := obj["templateId"].(string); id != "" {
		// 逐项处理，空的就不进入循环。
		for _, t := range s.Templates {
			// 对上了才走这一路，其余分开处理。
			if t.ID == id {
				return t, true
			}
		}
	}
	// 从正文推断是哪一种条目。
	kind := InferItemKind(el)
	// 逐项处理，空的就不进入循环。
	for _, t := range s.Templates {
		// 对上了才走这一路，其余分开处理。
		if t.Kind == kind {
			return t, true
		}
	}
	return ProjectTemplate{}, false
}

// applyProjectTemplates 按元素对应的命名模版只套那一种；对不上的丢掉。
func applyProjectTemplates(s Schema, v any) []any {
	// 收成数组再往下走，对不上就当不是数组。
	src, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return []any{}
	}
	// 按需要的长度把缓冲准备好。
	out := make([]any, 0, len(src))
	// 逐个元素套用，空数组就保持空。
	for _, el := range src {
		// 按身份找到对应的那一份模版。
		tpl, ok := lookupTemplate(s, el)
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			continue
		}
		// 定下附加，再交给后面。
		extra := tpl.Extra && tpl.Kind != ItemMulti
		// 取出这种条目要用的字段。
		obj := applyObject(ItemFields(tpl.Kind, extra), el)
		// 把这个值定下来，后面的判断才有依据。
		obj["kind"] = tpl.Kind
		// 把这个值定下来，后面的判断才有依据。
		obj["templateId"] = tpl.ID
		// 把这一段接进结果，顺序要保持住。
		out = append(out, obj)
	}
	return out
}

// applyProjectKinds 旧种类表：按元素种类只套那一种；未选的丢掉。
func applyProjectKinds(s Schema, v any) []any {
	// 收成数组再往下走，对不上就当不是数组。
	src, ok := v.([]any)
	// 实际类型对不上就换一种收法。
	if !ok {
		return []any{}
	}
	// 看这种条目有没有被选中。
	extra := HasKind(s.Kinds, ItemExtra)
	// 按需要的长度把缓冲准备好。
	out := make([]any, 0, len(src))
	// 逐个元素套用，空数组就保持空。
	for _, el := range src {
		// 从正文推断是哪一种条目。
		kind := InferItemKind(el)
		// 这种没被选中就不要它的字段。
		if !HasKind(s.Kinds, kind) {
			continue
		}
		// 取出这种条目要用的字段。
		obj := applyObject(ItemFields(kind, extra), el)
		// 把这个值定下来，后面的判断才有依据。
		obj["kind"] = kind
		// 把这一段接进结果，顺序要保持住。
		out = append(out, obj)
	}
	return out
}

// itemSingle 一条轨迹加主工艺。
func itemSingle(extra bool) []Field {
	// 定下字段，再交给后面。
	fields := []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		str("kind", "种类", ItemSingle),
		{Key: "points", Label: "点", Type: TypeArray, Items: ptr(pointField())},
		procRef("processId", "工艺"),
		num("selectedPointIndex", "选中点", "", 0),
		flag("isEnabled", "启用", true),
	}
	// 条件不成立就换一路，避免误往下做。
	if extra {
		// 把这一段接进结果，顺序要保持住。
		fields = append(fields, extraProcessesField())
	}
	return fields
}

// itemMulti 基准路径加各层偏移。
func itemMulti() []Field {
	// 先把这一步的结果放下，后面还要用。
	pass := Field{Type: TypeObject, Label: "焊道", Fields: []Field{
		str("id", "焊道身份", ""),
		str("name", "焊道名", ""),
		num("valX", "X", "mm", 0),
		num("valYLeft", "Y左", "mm", 0),
		num("valYRight", "Y右", "mm", 0),
		num("valZ", "Z", "mm", 0),
		num("valR", "R", "mm", 0),
		procRef("processId", "工艺"),
		flag("isCompleted", "已完成", false),
		flag("isEnabled", "启用", true),
	}}
	return []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		str("kind", "种类", ItemMulti),
		{Key: "basePath", Label: "基准层", Type: TypeObject, Fields: pathFields()},
		{Key: "passes", Label: "填充层", Type: TypeArray, Items: &pass},
		{Key: "refPointX1", Label: "X1", Type: TypeObject, Fields: refFields()},
		{Key: "refPointZ1", Label: "Z1", Type: TypeObject, Fields: refFields()},
		{Key: "refPointXMiddle", Label: "X中", Type: TypeObject, Fields: refFields()},
		{Key: "refPointZMiddle", Label: "Z中", Type: TypeObject, Fields: refFields()},
		{Key: "refPointXEnd", Label: "X2", Type: TypeObject, Fields: refFields()},
		{Key: "refPointZEnd", Label: "Z2", Type: TypeObject, Fields: refFields()},
		flag("isBaseCompleted", "基准完成", false),
		flag("isEnabled", "启用", true),
	}
}

// itemTBar 坡口点列加间隙带工艺引用，不嵌打底/盖面参数。
func itemTBar() []Field {
	// 先把这一步的结果放下，后面还要用。
	band := Field{Type: TypeObject, Label: "间隙带", Fields: []Field{
		num("minGap", "最小间隙", "mm", 0),
		num("maxGap", "最大间隙", "mm", 0),
		num("layer", "层", "", 1),
		procRef("rootProcessId", "打底工艺"),
		procRef("capProcessId", "盖面工艺"),
	}}
	return []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		str("kind", "种类", ItemTBar),
		{Key: "points", Label: "点", Type: TypeArray, Items: ptr(pointField())},
		num("selectedPointIndex", "选中点", "", 0),
		flag("isEnabled", "启用", true),
		{Key: "gapBands", Label: "间隙带", Type: TypeArray, Items: &band},
	}
}

// extraProcessesField 挂在焊道上的额外工艺槽。
func extraProcessesField() Field {
	return Field{Key: "extraProcesses", Label: "附加工艺", Type: TypeArray, Items: &Field{Type: TypeObject, Label: "附加", Fields: []Field{
		str("id", "身份", ""), procRef("processId", "工艺"), flag("isEnabled", "启用", true),
	}}}
}

// ptr 给数组元素取地址。
func ptr(f Field) *Field { return &f }
