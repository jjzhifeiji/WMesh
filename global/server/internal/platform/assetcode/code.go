// Package assetcode 发只读工艺/工程编号，不当稳定身份。
package assetcode

import (
	"fmt"
	"regexp"

	"wmesh/global/internal/platform/domain"
)

const (
	PrefixProcess = "GY"   // 工艺编号前缀
	PrefixProject = "GC"   // 工程编号前缀
	OriginWAN     = "W"    // 云端创建端短码
	maxSeq        = 999999 // 序号用尽前的六位上限
	maxFactory    = 99     // 厂短码两位里的上限
	maxClient     = 9999   // 设备短码四位里的上限
)

// 编号形态：种类、创建端短码、六位序号。
var codeRe = regexp.MustCompile(`^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$`)

// Prefix 按资产种类给出编号前缀。
func Prefix(kind string) (string, error) {
	// 只认工艺和工程两种，别的编号会冲突。
	switch kind {
	// 工艺种类用工艺前缀。
	case "process":
		return PrefixProcess, nil
	// 工程种类用工程前缀。
	case "project":
		return PrefixProject, nil
	// 别的种类不能发号。
	default:
		return "", domain.ErrAssetCodeConflict
	}
}

// Format 写成种类-短码-六位序号。
func Format(kind, origin string, n int64) (string, error) {
	// 先按种类取前缀，取不到就不发号。
	p, err := Prefix(kind)
	// 种类不对则整段编号作废。
	if err != nil {
		return "", err
	}
	// 序号必须落在六位正数里。
	if n < 1 || n > maxSeq {
		return "", domain.ErrOriginCodeExhausted
	}
	// 短码必须是云端、厂或设备三种之一。
	if origin != OriginWAN && !ValidFactoryOrigin(origin) && !ValidClientOrigin(origin) {
		return "", domain.ErrAssetCodeConflict
	}
	// 拼成种类、短码和六位序号。
	return fmt.Sprintf("%s-%s-%06d", p, origin, n), nil
}

// FormatFactory 写成 F01…F99。
func FormatFactory(n int64) (string, error) {
	// 厂序号必须落在两位正数里。
	if n < 1 || n > maxFactory {
		return "", domain.ErrOriginCodeExhausted
	}
	// 收成固定两位的厂短码。
	return fmt.Sprintf("F%02d", n), nil
}

// FormatClient 写成 C0001…C9999。
func FormatClient(n int64) (string, error) {
	// 设备序号必须落在四位正数里。
	if n < 1 || n > maxClient {
		return "", domain.ErrOriginCodeExhausted
	}
	// 收成四位设备短码。
	return fmt.Sprintf("C%04d", n), nil
}

// Valid 编号是否符合形态。
func Valid(code string) bool {
	// 整段必须符合编号形态。
	return codeRe.MatchString(code)
}

// MatchKind 编号前缀是否对应该种类。
func MatchKind(kind, code string) bool {
	// 先取该种类的前缀。
	p, err := Prefix(kind)
	// 种类不对或形态不对就不是这一类。
	if err != nil || !Valid(code) {
		return false
	}
	// 前两字符必须就是该种类的前缀。
	return len(code) >= 2 && code[:2] == p
}

// ValidFactoryOrigin 是否为本厂短码 F01…F99；F00 只给迁移旧行，不得再发。
func ValidFactoryOrigin(s string) bool {
	// 三位、F 加两位数字，且不是留给迁移的全零。
	return len(s) == 3 && s[0] == 'F' && s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9' && s != "F00"
}

// ValidClientOrigin 是否为 Client 短码。
func ValidClientOrigin(s string) bool {
	// 必须是字母 C 加四位数字。
	ok, _ := regexp.MatchString(`^C[0-9]{4}$`, s)
	return ok
}
