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

// 模版身份必须是这种十六进制格式。
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
	// 在空库三份里找这个身份。
	for _, it := range SeedProjectItems() {
		// 身份对上就交回这一份。
		if it.ID == id {
			return it, true
		}
	}
	return ProjectItemSchema{}, false
}

// LegacyProjectItems 已有库拆行用的两份，不含 T 排。
func LegacyProjectItems() []ProjectItemSchema {
	// 先攒下拆出来的份。
	var out []ProjectItemSchema
	// 在空库三份里找这个身份。
	for _, it := range SeedProjectItems() {
		// 旧库拆行不要 T 排。
		if it.ID == SeedTplTBar {
			continue
		}
		// 留下单层和多层这两份。
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
	// 不是数组根就无从拆成独立份。
	if s.Root != RootArray {
		return nil
	}
	// 有命名模版就不再看旧种类和元素。
	if len(s.Templates) > 0 {
		// 按命名份数预留拆行结果。
		out := make([]ProjectItemSchema, 0, len(s.Templates))
		// 每一份命名模版单独拆。
		for _, t := range s.Templates {
			// 不是根项的旧种类（如包角）拆行时丢掉。
			if !IsRootItem(t.Kind) {
				continue
			}
			// 收下这一份自己的字段。
			out = append(out, ProjectItemSchema{ID: t.ID, Name: t.Name, Fields: ItemFields(t.Kind, t.Extra && t.Kind != ItemMulti)})
		}
		return out
	}
	// 没有命名模版才看旧种类表。
	if len(s.Kinds) > 0 {
		// 旧表选了附加，单层才带附加槽。
		extra := HasKind(s.Kinds, ItemExtra)
		// 结果最多留三份根项。
		out := make([]ProjectItemSchema, 0, 3)
		// 只在旧库那两份里拆。
		for _, seed := range LegacyProjectItems() {
			// 空库身份对上根种类。
			kind := seedKind(seed.ID)
			// 种类表没选的份丢掉。
			if !HasKind(s.Kinds, kind) {
				continue
			}
			// 按种类带上该有的字段。
			out = append(out, ProjectItemSchema{ID: seed.ID, Name: seed.Name, Fields: ItemFields(kind, extra && kind != ItemMulti)})
		}
		// 拆出了至少一份就用拆的结果。
		if len(out) > 0 {
			return out
		}
	}
	// 对不上就退回旧库两份，不插入 T 排。
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
	// 份数超过上限就拒绝。
	if len(list) > maxProjectTpls {
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
	// 记下已经出现的身份。
	ids := map[string]struct{}{}
	// 记下已经出现的名称。
	names := map[string]struct{}{}
	// 每一份都要有名称、身份和根种类。
	for _, t := range list {
		// 名称去掉空白再比。
		name := strings.TrimSpace(t.Name)
		// 名称、身份或种类有一项不合格就拒绝。
		if name == "" || utf8.RuneCountInString(name) > maxTplNameRunes || !tplIDRe.MatchString(t.ID) || !IsRootItem(t.Kind) {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 身份重复就不能当模版。
		if _, ok := ids[t.ID]; ok {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 名称重复就不能当模版。
		if _, ok := names[name]; ok {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 记下这份身份防止重复。
		ids[t.ID] = struct{}{}
		// 记下这份名称防止重复。
		names[name] = struct{}{}
	}
	return nil
}

// validateKinds 旧种类表：至少一种根项，不重复、不超出目录。
func validateKinds(kinds []string) error {
	// 空表或超出目录就拒绝。
	if len(kinds) == 0 || len(kinds) > len(CatalogKinds) {
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 还没见到可作焊缝的根项。
	root := false
	// 每个种类都要在目录里且不重复。
	for _, k := range kinds {
		// 目录外的种类不能进旧表。
		if !isCatalogKind(k) {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 种类重复就不能进旧表。
		if _, ok := seen[k]; ok {
			// 这一种组合不能用来套用。
			return fmt.Errorf("content template is invalid")
		}
		// 记下这个种类防止重复。
		seen[k] = struct{}{}
		// 附加不算根项，别的种类算。
		if k != ItemExtra {
			// 见到根项，这张表才能用。
			root = true
		}
	}
	// 只有附加、没有焊缝根项就拒绝。
	if !root {
		// 这一种组合不能用来套用。
		return fmt.Errorf("content template is invalid")
	}
	return nil
}

// isCatalogKind 是否旧种类表里的取值。
func isCatalogKind(kind string) bool {
	// 只认目录里的旧种类。
	for _, k := range CatalogKinds {
		// 对上目录里的一项。
		if k == kind {
			return true
		}
	}
	return false
}

// HasKind 旧种类表是否选用了该种类。
func HasKind(kinds []string, kind string) bool {
	// 每个种类都要在目录里且不重复。
	for _, k := range kinds {
		// 对上目录里的一项。
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
	// 按空库身份对根种类。
	switch id {
	// 空库多层模版对上多层。
	case SeedTplMulti:
		return ItemMulti
	// 空库 T 排模版对上 T 排。
	case SeedTplTBar:
		return ItemTBar
	// 认不出的根或类型直接拒绝。
	default:
		return ItemSingle
	}
}

// ItemFields 某一种基础项的字段；extra 打开才带附加工艺槽。
func ItemFields(kind string, extra bool) []Field {
	// 按焊缝种类取字段。
	switch kind {
	// 单层用轨迹加主工艺。
	case ItemSingle:
		// 附加打开才带附加槽。
		return itemSingle(extra)
	// 多层用基准路径加各层。
	case ItemMulti:
		// 多层字段不含附加槽。
		return itemMulti()
	// T 排用坡口点和间隙带。
	case ItemTBar:
		// 交回 T 排这一份字段。
		return itemTBar()
	// 认不出的根或类型直接拒绝。
	default:
		return nil
	}
}

// SelectedFields 旧种类表字段并集，给收集/改写工艺引用用。
func SelectedFields(kinds []string) []Field {
	// 选了附加，单层才合并附加槽。
	extra := HasKind(kinds, ItemExtra)
	// 攒下并集，同一键只留一次。
	var out []Field
	// 同一身份只留一次。
	seen := map[string]struct{}{}
	// 并集里同一键只留第一次。
	add := func(fields []Field) {
		// 并集里逐个字段看键。
		for _, f := range fields {
			// 这个键已经留下过。
			if _, ok := seen[f.Key]; ok {
				continue
			}
			// 记下这个键，防止后面重复。
			seen[f.Key] = struct{}{}
			// 第一次见到的字段留下。
			out = append(out, f)
		}
	}
	// 选了单层才并进单层字段。
	if HasKind(kinds, ItemSingle) {
		// 单层字段并进并集。
		add(itemSingle(extra))
	}
	// 选了多层才并进多层字段。
	if HasKind(kinds, ItemMulti) {
		// 多层字段并进并集。
		add(itemMulti())
	}
	return out
}

// InferItemKind 读正文上的 kind；没有则按形状判断。
func InferItemKind(v any) string {
	// 不是对象就没有键可走。
	obj, ok := v.(map[string]any)
	// 不是正文就当没有引用。
	if !ok {
		return ItemSingle
	}
	// 正文写了根种类就信它。
	if k, ok := obj["kind"].(string); ok && IsRootItem(k) {
		return k
	}
	// 有基准层就是多层。
	if _, ok := obj["basePath"]; ok {
		return ItemMulti
	}
	// 有填充层也是多层。
	if _, ok := obj["passes"]; ok {
		return ItemMulti
	}
	// 有间隙带就是 T 排。
	if _, ok := obj["gapBands"]; ok {
		return ItemTBar
	}
	// 坡口四点也当 T 排。
	if hasGroovePoint(obj) {
		return ItemTBar
	}
	return ItemSingle
}

// hasGroovePoint 点列里有坡口四点则当 T 排。
func hasGroovePoint(obj map[string]any) bool {
	// 没有点列就不是坡口。
	arr, _ := obj["points"].([]any)
	// 看点的类型里有没有坡口。
	for _, el := range arr {
		// 不是对象的点跳过。
		pt, _ := el.(map[string]any)
		// 取出这个点的类型。
		typ, _ := pt["type"].(string)
		// 只认这四种坡口点。
		switch typ {
		// 这四种点类型属于坡口。
		case "GROOVE_A_LOWER", "GROOVE_B_LOWER", "GROOVE_A_UPPER", "GROOVE_B_UPPER":
			return true
		}
	}
	return false
}

// lookupTemplate 按 templateId，没有则按种类对上第一份。
func lookupTemplate(s Schema, el any) (ProjectTemplate, bool) {
	// 先看成不是对象，不是就对不上。
	obj, ok := el.(map[string]any)
	// 不是正文就当没有引用。
	if !ok {
		return ProjectTemplate{}, false
	}
	// 有模版身份就先按身份找。
	if id, _ := obj["templateId"].(string); id != "" {
		// 每一份命名模版单独拆。
		for _, t := range s.Templates {
			// 身份对上就用这一份。
			if t.ID == id {
				return t, true
			}
		}
	}
	// 先看出这一份是哪种。
	kind := InferItemKind(el)
	// 每一份命名模版单独拆。
	for _, t := range s.Templates {
		// 种类对上的第一份可以用。
		if t.Kind == kind {
			return t, true
		}
	}
	return ProjectTemplate{}, false
}

// applyProjectTemplates 按元素对应的命名模版只套那一种；对不上的丢掉。
func applyProjectTemplates(s Schema, v any) []any {
	// 工程正文必须是数组。
	src, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return []any{}
	}
	// 按原份数预留结果。
	out := make([]any, 0, len(src))
	// 每一份对上模版才留下。
	for _, el := range src {
		// 对不上模版的份跳过。
		tpl, ok := lookupTemplate(s, el)
		// 不是正文就当没有引用。
		if !ok {
			continue
		}
		// 多层焊缝不带附加槽。
		extra := tpl.Extra && tpl.Kind != ItemMulti
		// 只套这一类的字段。
		obj := applyObject(ItemFields(tpl.Kind, extra), el)
		// 种类写回，和模版一致。
		obj["kind"] = tpl.Kind
		// 把模版身份写回这一份。
		obj["templateId"] = tpl.ID
		// 收下套完的这一份。
		out = append(out, obj)
	}
	return out
}

// applyProjectKinds 旧种类表：按元素种类只套那一种；未选的丢掉。
func applyProjectKinds(s Schema, v any) []any {
	// 工程正文必须是数组。
	src, ok := v.([]any)
	// 不是正文就当没有引用。
	if !ok {
		return []any{}
	}
	// 旧表选了附加，单层才带附加槽。
	extra := HasKind(s.Kinds, ItemExtra)
	// 按原份数预留结果。
	out := make([]any, 0, len(src))
	// 每一份对上模版才留下。
	for _, el := range src {
		// 先看出这一份是哪种。
		kind := InferItemKind(el)
		// 种类表没选的份丢掉。
		if !HasKind(s.Kinds, kind) {
			continue
		}
		// 只套这一种的字段。
		obj := applyObject(ItemFields(kind, extra), el)
		// 把种类写回这一份。
		obj["kind"] = kind
		// 收下套完的这一份。
		out = append(out, obj)
	}
	return out
}

// itemSingle 一条轨迹加主工艺。
func itemSingle(extra bool) []Field {
	// 先摆单层必有的字段。
	fields := []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		str("kind", "种类", ItemSingle),
		{Key: "points", Label: "点", Type: TypeArray, Items: ptr(pointField())},
		procRef("processId", "工艺"),
		num("selectedPointIndex", "选中点", "", 0),
		flag("isEnabled", "启用", true),
	}
	// 要附加槽才挂上去。
	if extra {
		// 附加工艺槽加在焊道上。
		fields = append(fields, extraProcessesField())
	}
	return fields
}

// itemMulti 基准路径加各层偏移。
func itemMulti() []Field {
	// 一层焊道的字段先摆好。
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
	// 一条间隙带的字段先摆好。
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
