// Package migrations 嵌入本厂库 Flyway 向前 SQL。新厂套用同一套迁移。
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
