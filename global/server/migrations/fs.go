// Package migrations 嵌入本侧 Flyway 向前 SQL。
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
