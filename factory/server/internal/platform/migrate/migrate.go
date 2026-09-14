// Package migrate 按文件名顺序执行向前 SQL，不做生产降级，也不改已落库的历史数据。
package migrate

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Up 按文件名顺序套用尚未记录的 SQL；每个文件连同记录行在一个事务里，失败整体回滚，重启可重试。
func Up(db *gorm.DB, fsys fs.FS, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error; err != nil {
		return err
	}
	names, err := listSQL(fsys, dir)
	if err != nil {
		return err
	}
	for _, name := range names {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			for i, stmt := range splitSQL(string(body)) {
				if err := tx.Exec(stmt).Error; err != nil {
					return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
				}
			}
			return tx.Exec("INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW())", name).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// listSQL 列出待套用的 .sql，按文件名排序。
func listSQL(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// splitSQL 按分号切语句，但不切开单引号串、$$ 函数体和行注释里的分号。
func splitSQL(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "--"):
			// 行注释整行照抄，里面的分号不算语句结束。
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				end = len(s) - i
			}
			cur.WriteString(s[i : i+end])
			i += end
		case s[i] == '\'':
			// 单引号串，'' 是转义；没闭合就照抄到结尾交给数据库报错。
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j < len(s) {
				j++
			}
			cur.WriteString(s[i:j])
			i = j
		case s[i] == '$':
			tag := dollarTag(s[i:])
			if tag == "" {
				cur.WriteByte(s[i])
				i++
				continue
			}
			j := len(s)
			if end := strings.Index(s[i+len(tag):], tag); end >= 0 {
				j = i + len(tag) + end + len(tag)
			}
			cur.WriteString(s[i:j])
			i = j
		case s[i] == ';':
			flush()
			i++
		default:
			cur.WriteByte(s[i])
			i++
		}
	}
	flush()
	return out
}

// dollarTag 识别 $$ 或 $tag$ 开头，返回完整标记；不是则返回空。
func dollarTag(s string) string {
	if len(s) < 2 || s[0] != '$' {
		return ""
	}
	for j := 1; j < len(s); j++ {
		c := s[j]
		if c == '$' {
			return s[:j+1]
		}
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 1 && c >= '0' && c <= '9') {
			return ""
		}
	}
	return ""
}
