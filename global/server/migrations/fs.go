// Package migrations 嵌入本侧 goose 向前 SQL。
package migrations

import "embed"

// 本侧向前迁移，启动时整包套用。
//
//go:embed *.up.sql
var FS embed.FS
