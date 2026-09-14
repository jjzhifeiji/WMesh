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
	ItemCorner = "corner" // 包角
	ItemExtra  = "extra"  // 旧种类表：附加工艺开关
)

const (
	defaultTplSingle = "11111111-1111-4111-8111-111111111111" // 默认单层模版身份
	defaultTplMulti  = "22222222-2222-4222-8222-222222222222" // 默认多层模版身份
	defaultTplCorner = "33333333-3333-4333-8333-333333333333" // 默认包角模版身份
	maxProjectTpls   = 50                                     // 命名模版份数上限
	maxTplNameRunes  = 80                                     // 名称字数上限
)

// CatalogKinds 旧种类表顺序；新模版不再把 extra 当独立份。
var CatalogKinds = []string{ItemSingle, ItemMulti, ItemCorner, ItemExtra}

var tplIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ProjectTemplate 一份命名工程模版：独立，不依赖其它份。
type ProjectTemplate struct {
	ID    string `json:"id"`    // 这份模版的身份
	Name  string `json:"name"`  // 给人看的名称
	Kind  string `json:"kind"`  // single / multi / corner
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
		{ID: defaultTplSingle, Name: "单层焊道", Fields: itemSingle(true)},
		{ID: defaultTplMulti, Name: "多层焊缝", Fields: itemMulti()},
		{ID: defaultTplCorner, Name: "包角", Fields: itemCorner(true)},
	}
}

// ObjectSchema 把一份字段表收成对象根。
func ObjectSchema(fields []Field) Schema {
	return Schema{Root: RootObject, Fields: fields}
}

// ExpandLegacyProject 旧登记簿拆成独立份；对不上则用空库三份。
func ExpandLegacyProject(s Schema) []ProjectItemSchema {
	if s.Root != RootArray {
		return nil
	}
	if len(s.Templates) > 0 {
		out := make([]ProjectItemSchema, 0, len(s.Templates))
		for _, t := range s.Templates {
			out = append(out, ProjectItemSchema{ID: t.ID, Name: t.Name, Fields: ItemFields(t.Kind, t.Extra && t.Kind != ItemMulti)})
		}
		return out
	}
	if len(s.Kinds) > 0 {
		extra := HasKind(s.Kinds, ItemExtra)
		out := make([]ProjectItemSchema, 0, 3)
		for _, seed := range SeedProjectItems() {
			kind := ItemSingle
			if seed.ID == defaultTplMulti {
				kind = ItemMulti
			}
			if seed.ID == defaultTplCorner {
				kind = ItemCorner
			}
			if !HasKind(s.Kinds, kind) {
				continue
			}
			out = append(out, ProjectItemSchema{ID: seed.ID, Name: seed.Name, Fields: ItemFields(kind, extra && kind != ItemMulti)})
		}
		if len(out) > 0 {
			return out
		}
	}
	return SeedProjectItems()
}

// defaultProject 旧登记簿形状，只给拆行用。
func defaultProject() Schema {
	return Schema{Root: RootArray, Templates: []ProjectTemplate{
		{ID: defaultTplSingle, Name: "单层焊道", Kind: ItemSingle, Extra: true},
		{ID: defaultTplMulti, Name: "多层焊缝", Kind: ItemMulti, Extra: false},
		{ID: defaultTplCorner, Name: "包角", Kind: ItemCorner, Extra: true},
	}}
}

// validateTemplates 名称必填且不重复，种类只许三种，身份不重复。
func validateTemplates(list []ProjectTemplate) error {
	if len(list) > maxProjectTpls {
		return fmt.Errorf("content template is invalid")
	}
	ids := map[string]struct{}{}
	names := map[string]struct{}{}
	for _, t := range list {
		name := strings.TrimSpace(t.Name)
		if name == "" || utf8.RuneCountInString(name) > maxTplNameRunes || !tplIDRe.MatchString(t.ID) || !IsRootItem(t.Kind) {
			return fmt.Errorf("content template is invalid")
		}
		if _, ok := ids[t.ID]; ok {
			return fmt.Errorf("content template is invalid")
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("content template is invalid")
		}
		ids[t.ID] = struct{}{}
		names[name] = struct{}{}
	}
	return nil
}

// validateKinds 旧种类表：至少一种根项，不重复、不超出目录。
func validateKinds(kinds []string) error {
	if len(kinds) == 0 || len(kinds) > len(CatalogKinds) {
		return fmt.Errorf("content template is invalid")
	}
	seen := map[string]struct{}{}
	root := false
	for _, k := range kinds {
		if !isCatalogKind(k) {
			return fmt.Errorf("content template is invalid")
		}
		if _, ok := seen[k]; ok {
			return fmt.Errorf("content template is invalid")
		}
		seen[k] = struct{}{}
		if k != ItemExtra {
			root = true
		}
	}
	if !root {
		return fmt.Errorf("content template is invalid")
	}
	return nil
}

// isCatalogKind 是否旧种类表里的取值。
func isCatalogKind(kind string) bool {
	for _, k := range CatalogKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// HasKind 旧种类表是否选用了该种类。
func HasKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// IsRootItem 单层/多层/包角可作焊缝数组元素。
func IsRootItem(kind string) bool {
	return kind == ItemSingle || kind == ItemMulti || kind == ItemCorner
}

// ItemFields 某一种基础项的字段；extra 打开才带附加工艺槽。
func ItemFields(kind string, extra bool) []Field {
	switch kind {
	case ItemSingle:
		return itemSingle(extra)
	case ItemMulti:
		return itemMulti()
	case ItemCorner:
		return itemCorner(extra)
	default:
		return nil
	}
}

// SelectedFields 旧种类表字段并集，给收集/改写工艺引用用。
func SelectedFields(kinds []string) []Field {
	extra := HasKind(kinds, ItemExtra)
	var out []Field
	seen := map[string]struct{}{}
	add := func(fields []Field) {
		for _, f := range fields {
			if _, ok := seen[f.Key]; ok {
				continue
			}
			seen[f.Key] = struct{}{}
			out = append(out, f)
		}
	}
	if HasKind(kinds, ItemSingle) {
		add(itemSingle(extra))
	}
	if HasKind(kinds, ItemMulti) {
		add(itemMulti())
	}
	if HasKind(kinds, ItemCorner) {
		add(itemCorner(extra))
	}
	return out
}

// InferItemKind 读正文上的 kind；没有则按形状判断。
func InferItemKind(v any) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return ItemSingle
	}
	if k, ok := obj["kind"].(string); ok && IsRootItem(k) {
		return k
	}
	if _, ok := obj["basePath"]; ok {
		return ItemMulti
	}
	if _, ok := obj["passes"]; ok {
		return ItemMulti
	}
	if _, ok := obj["cornerGroupParams"]; ok {
		return ItemCorner
	}
	return ItemSingle
}

// lookupTemplate 按 templateId，没有则按种类对上第一份。
func lookupTemplate(s Schema, el any) (ProjectTemplate, bool) {
	obj, ok := el.(map[string]any)
	if !ok {
		return ProjectTemplate{}, false
	}
	if id, _ := obj["templateId"].(string); id != "" {
		for _, t := range s.Templates {
			if t.ID == id {
				return t, true
			}
		}
	}
	kind := InferItemKind(el)
	for _, t := range s.Templates {
		if t.Kind == kind {
			return t, true
		}
	}
	return ProjectTemplate{}, false
}

// applyProjectTemplates 按元素对应的命名模版只套那一种；对不上的丢掉。
func applyProjectTemplates(s Schema, v any) []any {
	src, ok := v.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(src))
	for _, el := range src {
		tpl, ok := lookupTemplate(s, el)
		if !ok {
			continue
		}
		extra := tpl.Extra && tpl.Kind != ItemMulti
		obj := applyObject(ItemFields(tpl.Kind, extra), el)
		obj["kind"] = tpl.Kind
		obj["templateId"] = tpl.ID
		out = append(out, obj)
	}
	return out
}

// applyProjectKinds 旧种类表：按元素种类只套那一种；未选的丢掉。
func applyProjectKinds(s Schema, v any) []any {
	src, ok := v.([]any)
	if !ok {
		return []any{}
	}
	extra := HasKind(s.Kinds, ItemExtra)
	out := make([]any, 0, len(src))
	for _, el := range src {
		kind := InferItemKind(el)
		if !HasKind(s.Kinds, kind) {
			continue
		}
		obj := applyObject(ItemFields(kind, extra), el)
		obj["kind"] = kind
		out = append(out, obj)
	}
	return out
}

// itemSingle 一条轨迹加主工艺。
func itemSingle(extra bool) []Field {
	fields := []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		{Key: "points", Label: "点", Type: TypeArray, Items: ptr(pointField())},
		procRef("processId", "工艺"),
		num("selectedPointIndex", "选中点", "", 0),
		flag("isEnabled", "启用", true),
	}
	if extra {
		fields = append(fields, extraProcessesField())
	}
	return fields
}

// itemMulti 基准路径加各层偏移。
func itemMulti() []Field {
	pass := Field{Type: TypeObject, Label: "焊道", Fields: []Field{
		str("id", "焊道身份", ""),
		str("name", "焊道名", ""),
		num("valX", "X", "mm", 0),
		num("valYLeft", "左 Y", "mm", 0),
		num("valYRight", "右 Y", "mm", 0),
		num("valZ", "Z", "mm", 0),
		num("valR", "R", "mm", 0),
		procRef("processId", "工艺"),
		flag("isCompleted", "已完成", false),
		flag("isEnabled", "启用", true),
	}}
	ref := Field{Type: TypeObject, Label: "参考点", Fields: []Field{
		{Key: "pose", Label: "位姿", Type: TypeObject, Fields: poseFields()},
		{Key: "jointAngles", Label: "关节角", Type: TypeArray, Items: &Field{Key: "a", Label: "角", Type: TypeString, Default: 0.0}},
	}}
	return []Field{
		str("id", "身份", ""),
		str("name", "名称", ""),
		{Key: "basePath", Label: "基准路径", Type: TypeObject, Fields: pathFields()},
		{Key: "passes", Label: "多层焊道", Type: TypeArray, Items: &pass},
		{Key: "refPointX1", Label: "起点 X", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointZ1", Label: "起点 Z", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointXEnd", Label: "终点 X", Type: TypeObject, Fields: ref.Fields},
		{Key: "refPointZEnd", Label: "终点 Z", Type: TypeObject, Fields: ref.Fields},
		flag("isBaseCompleted", "基准完成", false),
		flag("isEnabled", "启用", true),
	}
}

// itemCorner 单层形状外包角几何。
func itemCorner(extra bool) []Field {
	fields := itemSingle(extra)
	fields = append(fields, Field{Key: "cornerGroupParams", Label: "包角", Type: TypeObject, Fields: cornerFields()})
	return fields
}

// extraProcessesField 挂在焊道上的额外工艺槽。
func extraProcessesField() Field {
	return Field{Key: "extraProcesses", Label: "附加工艺", Type: TypeArray, Items: &Field{Type: TypeObject, Label: "附加", Fields: []Field{
		str("id", "身份", ""), procRef("processId", "工艺"),
	}}}
}

// ptr 给数组元素取地址。
func ptr(f Field) *Field { return &f }
