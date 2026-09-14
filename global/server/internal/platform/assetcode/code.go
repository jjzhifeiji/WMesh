// Package assetcode 发只读工艺/工程编号，不当稳定身份。
package assetcode

import (
	"fmt"
	"regexp"

	"wmesh/global/internal/platform/domain"
)

const (
	PrefixProcess = "GY" // 工艺编号前缀
	PrefixProject = "GC" // 工程编号前缀
	OriginWAN     = "W"  // 云端创建端短码
	maxSeq        = 999999
	maxFactory    = 99
	maxClient     = 9999
)

var codeRe = regexp.MustCompile(`^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$`)

// Prefix 按资产种类给出编号前缀。
func Prefix(kind string) (string, error) {
	switch kind {
	case "process":
		return PrefixProcess, nil
	case "project":
		return PrefixProject, nil
	default:
		return "", domain.ErrAssetCodeConflict
	}
}

// Format 写成种类-短码-六位序号。
func Format(kind, origin string, n int64) (string, error) {
	p, err := Prefix(kind)
	if err != nil {
		return "", err
	}
	if n < 1 || n > maxSeq {
		return "", domain.ErrOriginCodeExhausted
	}
	if origin != OriginWAN && !ValidFactoryOrigin(origin) && !ValidClientOrigin(origin) {
		return "", domain.ErrAssetCodeConflict
	}
	return fmt.Sprintf("%s-%s-%06d", p, origin, n), nil
}

// FormatFactory 写成 F01…F99。
func FormatFactory(n int64) (string, error) {
	if n < 1 || n > maxFactory {
		return "", domain.ErrOriginCodeExhausted
	}
	return fmt.Sprintf("F%02d", n), nil
}

// FormatClient 写成 C0001…C9999。
func FormatClient(n int64) (string, error) {
	if n < 1 || n > maxClient {
		return "", domain.ErrOriginCodeExhausted
	}
	return fmt.Sprintf("C%04d", n), nil
}

// Valid 编号是否符合形态。
func Valid(code string) bool {
	return codeRe.MatchString(code)
}

// MatchKind 编号前缀是否对应该种类。
func MatchKind(kind, code string) bool {
	p, err := Prefix(kind)
	if err != nil || !Valid(code) {
		return false
	}
	return len(code) >= 2 && code[:2] == p
}

// ValidFactoryOrigin 是否为本厂短码 F01…F99；F00 只给迁移旧行，不得再发。
func ValidFactoryOrigin(s string) bool {
	return len(s) == 3 && s[0] == 'F' && s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9' && s != "F00"
}

// ValidClientOrigin 是否为 Client 短码。
func ValidClientOrigin(s string) bool {
	ok, _ := regexp.MatchString(`^C[0-9]{4}$`, s)
	return ok
}
