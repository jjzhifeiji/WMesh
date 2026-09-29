// Package assetcode 发只读工艺/工程编号，不当稳定身份。
package assetcode

import (
	"fmt"
	"regexp"

	"wmesh/factory/internal/platform/domain"
)

const (
	PrefixProcess = "GY" // 工艺编号前缀
	PrefixProject = "GC" // 工程编号前缀
	OriginWAN     = "W"  // 云端创建端短码
	maxSeq        = 999999
	maxFactory    = 99   // 厂短码最大序号，对应两位数字。
	maxClient     = 9999 // 本机短码最大序号，对应四位数字。
)

// 编号整串形态，种类、短码再加六位序号。
var codeRe = regexp.MustCompile(`^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$`)

// Prefix 按资产种类给出编号前缀。
func Prefix(kind string) (string, error) {
	// 按种类或身份分路，对不上就走默认。
	switch kind {
	// 工艺这一路使用工艺前缀。
	case "process":
		return PrefixProcess, nil
	// 工程这一路使用工程前缀。
	case "project":
		return PrefixProject, nil
	// 不认识的种类就拒绝编这个号。
	default:
		return "", domain.ErrAssetCodeConflict
	}
}

// Format 写成种类-短码-六位序号。
func Format(kind, origin string, n int64) (string, error) {
	// 按种类取出编号前缀。
	p, err := Prefix(kind)
	// 没能按种类取出编号前缀就停，避免带着残缺继续。
	if err != nil {
		return "", err
	}
	// 序号超出可发范围就拒绝，避免编出坏号。
	if n < 1 || n > maxSeq {
		return "", domain.ErrOriginCodeExhausted
	}
	// 短码不是云端就要再核是厂还是本机。
	if origin != OriginWAN && !ValidFactoryOrigin(origin) && !ValidClientOrigin(origin) {
		return "", domain.ErrAssetCodeConflict
	}
	// 按位数把编号或口令拼好再交回去。
	return fmt.Sprintf("%s-%s-%06d", p, origin, n), nil
}

// FormatFactory 写成 F01…F99。
func FormatFactory(n int64) (string, error) {
	// 厂序号超出两位就拒绝。
	if n < 1 || n > maxFactory {
		return "", domain.ErrOriginCodeExhausted
	}
	// 按位数把编号或口令拼好再交回去。
	return fmt.Sprintf("F%02d", n), nil
}

// FormatClient 写成 C0001…C9999。
func FormatClient(n int64) (string, error) {
	// 本机序号超出四位就拒绝。
	if n < 1 || n > maxClient {
		return "", domain.ErrOriginCodeExhausted
	}
	// 按位数把编号或口令拼好再交回去。
	return fmt.Sprintf("C%04d", n), nil
}

// Valid 编号是否符合形态。
func Valid(code string) bool {
	// 整串对上形态才算这个编号合法。
	return codeRe.MatchString(code)
}

// MatchKind 编号前缀是否对应该种类。
func MatchKind(kind, code string) bool {
	// 按种类取出编号前缀。
	p, err := Prefix(kind)
	// 出错或条件不够就停下，避免半对的结果往下用。
	if err != nil || !Valid(code) {
		return false
	}
	// 把这一步的结果交回给调用方。
	return len(code) >= 2 && code[:2] == p
}

// ValidFactoryOrigin 是否为本厂短码 F01…F99；F00 只给迁移旧行，不得再发。
func ValidFactoryOrigin(s string) bool {
	// 把这一步的结果交回给调用方。
	return len(s) == 3 && s[0] == 'F' && s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9' && s != "F00"
}

// ValidClientOrigin 是否为 Client 短码。
func ValidClientOrigin(s string) bool {
	// 按形态核对整串，再交给后面。
	ok, _ := regexp.MatchString(`^C[0-9]{4}$`, s)
	return ok
}
