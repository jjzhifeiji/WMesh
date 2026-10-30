package contenttpl

import (
	"bytes"
	"encoding/json"
)

// CornerKind 包角工艺正文顶层 kind 的取值；示教器只认这一个键分开包角和焊接参数。
const CornerKind = "cornerGeometry"

// IsCornerProcess 工艺正文顶层 kind 是包角几何就算包角工艺；解不开或不是对象都当普通工艺。
func IsCornerProcess(content []byte) bool {
	// 只取顶层 kind，别的键不看。
	var head struct {
		Kind any `json:"kind"` // 包角正文写 cornerGeometry
	}
	// 解不开就当普通工艺，和示教器一样。
	if err := json.Unmarshal(bytes.TrimSpace(content), &head); err != nil {
		return false
	}
	// 只认字符串 cornerGeometry，别的写法都不算。
	s, ok := head.Kind.(string)
	return ok && s == CornerKind
}

// HasCornerProcess 工程里任一工艺槽挂了包角工艺就交回真；认不出的身份不管，由依赖核对去拦。
// 包角几何只放在示教器本机设置里，工程正文不存它：焊道、T 排打底盖面、附加槽、包角组 processId 全是焊接参数，
// 包角组 processId 就是这组焊道的底工艺，和焊道 processId 相同。isCorner 交回（是不是包角工艺, 认不认得出）。
func HasCornerProcess(ids []string, isCorner func(id string) (bool, bool)) bool {
	// 逐个引用看。
	for _, id := range ids {
		// 认得出且是包角工艺就算挂错。
		if yes, ok := isCorner(id); ok && yes {
			return true
		}
	}
	return false
}
