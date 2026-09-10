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

// Up 按文件名顺序套用尚未记录的 SQL；已套用的跳过，避免进程重启把表再建一遍。
func Up(db *gorm.DB, fsys fs.FS, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error; err != nil {
		return err
	}
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
	if err := stampLegacy(db, names); err != nil {
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
		for i, stmt := range splitSQL(string(body)) {
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
			}
		}
		if err := db.Exec("INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW())", name).Error; err != nil {
			return err
		}
	}
	return nil
}

// stampLegacy 给改版前已经建好表的库补迁移记录，避免重启再执行建表 SQL。
func stampLegacy(db *gorm.DB, names []string) error {
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var exists bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name IN ('people', 'factories')
	)`).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return nil
	}
	for _, name := range names {
		if err := db.Exec("INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW())", name).Error; err != nil {
			return err
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
