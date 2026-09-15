// Package migrate 按 Flyway 规则只向前套用 SQL：历史进 flyway_schema_history，不做 down，也不改已落库的历史数据。
package migrate

import (
	"fmt"
	"hash/crc32"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	legacySQLName = regexp.MustCompile(`^(\d+)_(.+)\.up\.sql$`) // 0001_init.up.sql
	flywaySQLName = regexp.MustCompile(`^V(\d+)__(.+)\.sql$`)   // V0001__init.sql
)

// Up 按版本顺序套用尚未成功记录的 SQL；每个文件一个事务，失败不记历史，重启可重试。
func Up(db *gorm.DB, fsys fs.FS, dir string) error {
	if err := ensureHistory(db); err != nil {
		return err
	}
	// 旧库只有 schema_migrations 时，按已套用文件登记 Flyway 历史，不重跑。
	if err := baselineLegacy(db, fsys, dir); err != nil {
		return err
	}
	names, err := listSQL(fsys, dir)
	if err != nil {
		return err
	}
	who, err := currentUser(db)
	if err != nil {
		return err
	}
	for _, name := range names {
		ok, err := alreadyApplied(db, name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		version, desc, ok := parseVersionedSQL(name)
		if !ok {
			return fmt.Errorf("invalid migration name %s", name)
		}
		start := time.Now()
		err = db.Transaction(func(tx *gorm.DB) error {
			for i, stmt := range splitSQL(string(body)) {
				if err := tx.Exec(stmt).Error; err != nil {
					return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
				}
			}
			ms := int(time.Since(start).Milliseconds())
			return insertHistory(tx, who, version, desc, name, checksumOf(body), ms)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureHistory 建 Flyway 历史表；并保留 schema_migrations 让已发布的 COMMENT ON 仍能套上。
func ensureHistory(db *gorm.DB) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error; err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS flyway_schema_history (
			installed_rank INT NOT NULL,
			version VARCHAR(50),
			description VARCHAR(200) NOT NULL,
			type VARCHAR(20) NOT NULL,
			script VARCHAR(1000) NOT NULL,
			checksum INTEGER,
			installed_by VARCHAR(100) NOT NULL,
			installed_on TIMESTAMP NOT NULL DEFAULT NOW(),
			execution_time INTEGER NOT NULL,
			success BOOLEAN NOT NULL,
			CONSTRAINT flyway_schema_history_pk PRIMARY KEY (installed_rank)
		)`,
		`CREATE INDEX IF NOT EXISTS flyway_schema_history_s_idx ON flyway_schema_history (success)`,
		// 库内注释与实体同义；已有库启动时也能补上。
		`COMMENT ON TABLE flyway_schema_history IS 'Flyway 已套用的向前迁移，禁止 down、禁止改已落库数据'`,
		`COMMENT ON COLUMN flyway_schema_history.installed_rank IS '套用顺序，从 1 起'`,
		`COMMENT ON COLUMN flyway_schema_history.version IS '脚本版本号'`,
		`COMMENT ON COLUMN flyway_schema_history.description IS '脚本描述'`,
		`COMMENT ON COLUMN flyway_schema_history.type IS '脚本类型，本仓固定 SQL'`,
		`COMMENT ON COLUMN flyway_schema_history.script IS '已执行的 SQL 文件名'`,
		`COMMENT ON COLUMN flyway_schema_history.checksum IS '脚本 CRC32，改已落库文件会与历史不符'`,
		`COMMENT ON COLUMN flyway_schema_history.installed_by IS '套用时的数据库用户'`,
		`COMMENT ON COLUMN flyway_schema_history.installed_on IS '套用成功时间'`,
		`COMMENT ON COLUMN flyway_schema_history.execution_time IS '套用耗时毫秒'`,
		`COMMENT ON COLUMN flyway_schema_history.success IS '是否成功；失败不记行，重启可重试'`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// baselineLegacy 把旧 schema_migrations 抄进 Flyway 历史，避免换历史表后重跑。
func baselineLegacy(db *gorm.DB, fsys fs.FS, dir string) error {
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history").Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var names []string
	if err := db.Raw("SELECT name FROM schema_migrations ORDER BY name").Scan(&names).Error; err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	who, err := currentUser(db)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, name := range names {
			version, desc, ok := parseVersionedSQL(name)
			if !ok {
				return fmt.Errorf("invalid legacy migration name %s", name)
			}
			sum := 0
			if body, err := fs.ReadFile(fsys, path.Join(dir, name)); err == nil {
				sum = checksumOf(body)
			}
			if err := insertHistory(tx, who, version, desc, name, sum, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// alreadyApplied 该脚本已成功套用则跳过。
func alreadyApplied(db *gorm.DB, script string) (bool, error) {
	var n int64
	err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE script = ? AND success", script).Scan(&n).Error
	return n > 0, err
}

// insertHistory 追加一条成功记录；失败的文件不进表，重启可重试。
func insertHistory(tx *gorm.DB, who, version, description, script string, checksum, elapsedMs int) error {
	var rank int
	if err := tx.Raw("SELECT COALESCE(MAX(installed_rank), 0) FROM flyway_schema_history").Scan(&rank).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO flyway_schema_history
		(installed_rank, version, description, type, script, checksum, installed_by, execution_time, success)
		VALUES (?, ?, ?, 'SQL', ?, ?, ?, ?, TRUE)`,
		rank+1, version, description, script, checksum, who, elapsedMs).Error; err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW()) ON CONFLICT (name) DO NOTHING`, script).Error
}

// currentUser 记下谁套用了脚本，给 Flyway 历史用。
func currentUser(db *gorm.DB) (string, error) {
	var who string
	if err := db.Raw("SELECT CURRENT_USER").Scan(&who).Error; err != nil {
		return "", err
	}
	if who == "" {
		who = "wmesh"
	}
	return who, nil
}

// checksumOf 用 CRC32 当 Flyway checksum。
func checksumOf(body []byte) int {
	return int(int32(crc32.ChecksumIEEE(body)))
}

// parseVersionedSQL 从 0024_software.up.sql 或 V0024__software.sql 取出版本和描述。
func parseVersionedSQL(name string) (version, description string, ok bool) {
	if m := legacySQLName.FindStringSubmatch(name); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", "", false
		}
		return strconv.Itoa(n), m[2], true
	}
	if m := flywaySQLName.FindStringSubmatch(name); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", "", false
		}
		return strconv.Itoa(n), m[2], true
	}
	return "", "", false
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
