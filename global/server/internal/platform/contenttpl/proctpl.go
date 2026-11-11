package contenttpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	SeedTplWeld      = "55555555-5555-4555-8555-555555555555" // 空库「焊接参数」工艺模版身份
	SeedTplCorner    = "66666666-6666-4666-8666-666666666666" // 空库「包角」工艺模版身份
	ProcessTplKey    = "templateId"                           // 工艺正文顶层记所属模版的键；预置两份不写
	processKindKey   = "kind"                                 // 工艺正文顶层的包角标记键
	maxProcessTpls   = 50                                     // 工艺模版份数上限
	seedWeldTplName  = "焊接参数"                                 // 预置焊接参数模版的名称
	seedCornerTplNam = "包角"                                   // 预置包角模版的名称
)

// errProcessTemplate 工艺模版或套完的正文不合规则，和别处模版不合法同一句话。
var errProcessTemplate = fmt.Errorf("content template is invalid")

// ProcessSeed 一份预置工艺模版：身份、名称和对象字段表。
type ProcessSeed struct {
	ID     string  // 这份模版的身份
	Name   string  // 给人看的名称
	Fields []Field // 这份自己的字段
}

// MaxProcessTemplates 工艺模版份数上限。
func MaxProcessTemplates() int { return maxProcessTpls }

// SeedProcessTemplates 空库两份工艺模版：焊接参数和包角。
func SeedProcessTemplates() []ProcessSeed {
	return []ProcessSeed{
		{ID: SeedTplWeld, Name: seedWeldTplName, Fields: defaultProcess().Fields},
		{ID: SeedTplCorner, Name: seedCornerTplNam, Fields: cornerProcessFields()},
	}
}

// SeedProcessTemplate 按预置身份取一份；没有则 false。
func SeedProcessTemplate(id string) (ProcessSeed, bool) {
	// 在预置两份里找这个身份。
	for _, it := range SeedProcessTemplates() {
		// 身份对上就交回这一份，大小写不计。
		if strings.EqualFold(it.ID, id) {
			return it, true
		}
	}
	return ProcessSeed{}, false
}

// cornerProcessFields 包角工艺字段：名称和逐层的长度、平焊偏移、立焊偏移，和示教器写的一致。
func cornerProcessFields() []Field {
	// 一层的三项几何。
	pass := Field{Key: "pass", Label: "包角层", Type: TypeObject, Fields: []Field{
		num("length", "焊道长度", "mm", 0),
		num("flatOffset", "平焊偏移", "mm", 0),
		num("upwardOffset", "立焊偏移", "mm", 0),
	}}
	return []Field{
		str("name", "名称", ""),
		{Key: "passes", Label: "逐层几何", Type: TypeArray, Items: &pass},
	}
}

// ProcessTemplateOf 工艺正文属于哪份模版：有包角标记的一定是预置包角；写了自建模版身份的归那份；其余归预置焊接参数。
// 示教器只看 kind 认包角，所以 kind 说了算：templateId 写成预置两份的身份不认，免得两个键互相矛盾、绕过种类约束。
// 示教器上送、升档、老数据导入都只带正文，所以归属只能从正文算。
func ProcessTemplateOf(content []byte) string {
	// 只取顶层两个键，别的不看。
	var head struct {
		TemplateID any `json:"templateId"` // 自建种类写的模版身份
		Kind       any `json:"kind"`       // 包角正文写 cornerGeometry
	}
	// 解不开就当普通焊接参数，和示教器一样。
	if err := json.Unmarshal(bytes.TrimSpace(content), &head); err != nil {
		return SeedTplWeld
	}
	// 有包角标记就是包角，不管 templateId 写了什么。
	if s, ok := head.Kind.(string); ok && s == CornerKind {
		return SeedTplCorner
	}
	// 没有包角标记时，只认自建模版的身份。
	if s, ok := head.TemplateID.(string); ok && tplIDRe.MatchString(s) {
		// 身份统一小写，和库里记的一致。
		id := strings.ToLower(s)
		// 预置两份靠有没有包角标记区分，不靠这个键。
		if id != SeedTplWeld && id != SeedTplCorner {
			return id
		}
	}
	return SeedTplWeld
}

// IsWeldProcess 这份正文是不是焊接参数；工程工艺槽只收这一种。
func IsWeldProcess(content []byte) bool {
	// 归到预置焊接参数的才算。
	return ProcessTemplateOf(content) == SeedTplWeld
}

// ValidateProcessTemplate 工艺模版字段表的额外规则：顶层不许占用归属键和包角标记键；预置包角要留住示教器读的那几项。
func ValidateProcessTemplate(id string, s Schema) error {
	// 工艺模版根必须是对象。
	if s.Root != RootObject {
		return errProcessTemplate
	}
	// 顶层逐个字段看有没有占用保留键。
	for _, f := range s.Fields {
		// 这两个键由服务端写，模版里不许定义。
		if f.Key == ProcessTplKey || f.Key == processKindKey {
			return errProcessTemplate
		}
	}
	// 不是预置包角就没有更多要求。
	if !strings.EqualFold(id, SeedTplCorner) {
		return nil
	}
	// 包角要有名称，示教器拿它和资产名比。
	name, ok := fieldByKey(s.Fields, "name")
	// 没有名称或不是文本就拒绝。
	if !ok || !isScalarField(name) {
		return errProcessTemplate
	}
	// 包角要有逐层几何。
	passes, ok := fieldByKey(s.Fields, "passes")
	// 逐层几何必须是对象数组，示教器按这个形状读。
	if !ok || passes.Type != TypeArray || passes.Items == nil || passes.Items.Type != TypeObject || passes.Nullable {
		return errProcessTemplate
	}
	// 每层三项几何都要在，且是数字或文本。
	for _, key := range []string{"length", "flatOffset", "upwardOffset"} {
		// 找这一项。
		f, ok := fieldByKey(passes.Items.Fields, key)
		// 缺了或类型不对示教器会读错。
		if !ok || !isScalarField(f) {
			return errProcessTemplate
		}
	}
	return nil
}

// fieldByKey 在一层字段里按键找。
func fieldByKey(fields []Field, key string) (Field, bool) {
	// 逐个字段比键。
	for _, f := range fields {
		// 对上就交回。
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// isScalarField 字段是不是文本或旧数字，且没有枚举。
func isScalarField(f Field) bool {
	// 类型是文本或旧数字，且没带枚举取值。
	return (f.Type == TypeString || f.Type == TypeNumber) && len(f.Options) == 0
}

// ApplyProcess 按所属模版套工艺正文，再把服务端管的键写回：包角写 kind，自建种类写 templateId，其余不留这两个键。
// 套用只留模版里有的键，所以这两个键必须在套完之后写。
func ApplyProcess(templateID string, schemaJSON, content []byte) ([]byte, error) {
	// 先按模版套。
	out, err := Apply(schemaJSON, content)
	// 套不上就交回。
	if err != nil {
		return nil, err
	}
	// 准备接套完的对象。
	var obj map[string]any
	// 工艺正文必须是对象。
	if err := json.Unmarshal(out, &obj); err != nil || obj == nil {
		return nil, errProcessTemplate
	}
	// 先清掉两个键，再按种类写回。
	delete(obj, ProcessTplKey)
	// 包角标记也先清掉。
	delete(obj, processKindKey)
	// 按所属模版写回标记。
	switch {
	// 预置包角写 kind，示教器靠它认包角。
	case strings.EqualFold(templateID, SeedTplCorner):
		// 写上包角标记。
		obj[processKindKey] = CornerKind
	// 预置焊接参数什么都不写，正文和示教器写的一样。
	case strings.EqualFold(templateID, SeedTplWeld):
	// 自建种类写上模版身份。
	default:
		// 身份统一小写。
		obj[ProcessTplKey] = strings.ToLower(templateID)
	}
	// 收回成正文。
	return json.Marshal(obj)
}
