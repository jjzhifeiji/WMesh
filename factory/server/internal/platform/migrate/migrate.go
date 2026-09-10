// Package migrate 按文件名顺序执行向前 SQL，不做生产降级。
package migrate

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gorm.io/gorm"
)

func Up(db *gorm.DB, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		for i, stmt := range splitSQL(string(body)) {
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
			}
		}
	}
	return nil
}

func splitSQL(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
