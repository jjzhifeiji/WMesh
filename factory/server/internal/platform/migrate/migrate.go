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
	// 没能保证历史，再交给后面就停，避免带着残缺继续。
	if err := ensureHistory(db); err != nil {
		return err
	}
	// 旧库只有 schema_migrations 时，按已套用文件登记 Flyway 历史，不重跑。
	if err := baselineLegacy(db, fsys, dir); err != nil {
		return err
	}
	// 列出语句，再交给后面。
	names, err := listSQL(fsys, dir)
	// 没能列出语句，再交给后面就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 查出当前连库用的账号。
	who, err := currentUser(db)
	// 没能查出当前连库用的账号就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 按文件名逐个处理，已经做过的跳过。
	for _, name := range names {
		// 已经做过，再交给后面。
		ok, err := alreadyApplied(db, name)
		// 没能已经做过，再交给后面就停，避免带着残缺继续。
		if err != nil {
			return err
		}
		// 命中了才走这一支，没有就换下一种处理。
		if ok {
			continue
		}
		// 拼出同一目录下的路径。
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		// 没能拼出同一目录下的路径就停，避免带着残缺继续。
		if err != nil {
			// 脚本读不出来，带上文件名交回。
			return fmt.Errorf("read %s: %w", name, err)
		}
		// 从文件名取出版本号和说明。
		version, desc, ok := parseVersionedSQL(name)
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			// 迁移文件名不合法，不能执行。
			return fmt.Errorf("invalid migration name %s", name)
		}
		// 取当前这一刻的时间。
		start := time.Now()
		// 在一个事务里执行本版并记历史，失败整段回滚。
		err = db.Transaction(func(tx *gorm.DB) error {
			// 逐项处理，空的就不进入循环。
			for i, stmt := range splitSQL(string(body)) {
				// 没能把这一句交给库去执行就停，避免带着残缺继续。
				if err := tx.Exec(stmt).Error; err != nil {
					// 某一句脚本失败，带上位置交回。
					return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
				}
			}
			// 把耗时收成毫秒写进历史。
			ms := int(time.Since(start).Milliseconds())
			// 交回算这份脚本的校验和的结果。
			return insertHistory(tx, who, version, desc, name, checksumOf(body), ms)
		})
		// 没能算这份脚本的校验和就停，避免带着残缺继续。
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureHistory 建 Flyway 历史表；并保留 schema_migrations 让已发布的 COMMENT ON 仍能套上。
func ensureHistory(db *gorm.DB) error {
	// 没能把这一句交给库去执行就停，避免带着残缺继续。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error; err != nil {
		return err
	}
	// 准备收集字符串结果。
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
	// 逐项处理，空的就不进入循环。
	for _, stmt := range stmts {
		// 没能把这一句交给库去执行就停，避免带着残缺继续。
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// baselineLegacy 把旧 schema_migrations 抄进 Flyway 历史，避免换历史表后重跑。
func baselineLegacy(db *gorm.DB, fsys fs.FS, dir string) error {
	// 准备承接数出来的条数。
	var n int64
	// 没能把查询结果扫进变量就停，避免带着残缺继续。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history").Scan(&n).Error; err != nil {
		return err
	}
	// 有内容才继续，空的这一支跳过。
	if n > 0 {
		return nil
	}
	// 不满足就停住或跳过，避免做错下一步。
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	// 准备收集字符串结果。
	var names []string
	// 没能把查询结果扫进变量就停，避免带着残缺继续。
	if err := db.Raw("SELECT name FROM schema_migrations ORDER BY name").Scan(&names).Error; err != nil {
		return err
	}
	// 数量是零就按没有来处理。
	if len(names) == 0 {
		return nil
	}
	// 查出当前连库用的账号。
	who, err := currentUser(db)
	// 没能查出当前连库用的账号就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 在一个事务里把旧记录垫进历史，失败整段回滚。
	return db.Transaction(func(tx *gorm.DB) error {
		// 按文件名逐个处理，已经做过的跳过。
		for _, name := range names {
			// 从文件名取出版本号和说明。
			version, desc, ok := parseVersionedSQL(name)
			// 没有命中就走另一路，不用零值冒充有值。
			if !ok {
				// 旧迁移文件名不合法，不能垫基线。
				return fmt.Errorf("invalid legacy migration name %s", name)
			}
			// 定下摘要字节，再交给后面。
			sum := 0
			// 没有出错就按成功返回，不用再补救。
			if body, err := fs.ReadFile(fsys, path.Join(dir, name)); err == nil {
				// 算这份脚本的校验和。
				sum = checksumOf(body)
			}
			// 没能把这一版记进迁移历史就停，避免带着残缺继续。
			if err := insertHistory(tx, who, version, desc, name, sum, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// alreadyApplied 该脚本已成功套用则跳过。
func alreadyApplied(db *gorm.DB, script string) (bool, error) {
	// 准备承接数出来的条数。
	var n int64
	// 把查询结果扫进变量。
	err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE script = ? AND success", script).Scan(&n).Error
	return n > 0, err
}

// insertHistory 追加一条成功记录；失败的文件不进表，重启可重试。
func insertHistory(tx *gorm.DB, who, version, description, script string, checksum, elapsedMs int) error {
	// 准备给迁移文件排一个顺序。
	var rank int
	// 没能把查询结果扫进变量就停，避免带着残缺继续。
	if err := tx.Raw("SELECT COALESCE(MAX(installed_rank), 0) FROM flyway_schema_history").Scan(&rank).Error; err != nil {
		return err
	}
	// 没能把这一句交给库去执行就停，避免带着残缺继续。
	if err := tx.Exec(`INSERT INTO flyway_schema_history
		(installed_rank, version, description, type, script, checksum, installed_by, execution_time, success)
		VALUES (?, ?, ?, 'SQL', ?, ?, ?, ?, TRUE)`,
		rank+1, version, description, script, checksum, who, elapsedMs).Error; err != nil {
		return err
	}
	// 交回把这一句交给库去执行的结果。
	return tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW()) ON CONFLICT (name) DO NOTHING`, script).Error
}

// currentUser 记下谁套用了脚本，给 Flyway 历史用。
func currentUser(db *gorm.DB) (string, error) {
	// 准备放下当前账号。
	var who string
	// 没能把查询结果扫进变量就停，避免带着残缺继续。
	if err := db.Raw("SELECT CURRENT_USER").Scan(&who).Error; err != nil {
		return "", err
	}
	// 是空的就改用默认，或按没有处理。
	if who == "" {
		// 定下当前账号，再交给后面。
		who = "wmesh"
	}
	return who, nil
}

// checksumOf 用 CRC32 当 Flyway checksum。
func checksumOf(body []byte) int {
	// 交回校验和，再交给后面的结果。
	return int(int32(crc32.ChecksumIEEE(body)))
}

// parseVersionedSQL 从 0024_software.up.sql 或 V0024__software.sql 取出版本和描述。
func parseVersionedSQL(name string) (version, description string, ok bool) {
	// 宿主配置解出来了才拿掉互相冲突的项。
	if m := legacySQLName.FindStringSubmatch(name); m != nil {
		// 转整数，再交给后面。
		n, err := strconv.Atoi(m[1])
		// 没能转整数，再交给后面就停，避免带着残缺继续。
		if err != nil {
			return "", "", false
		}
		// 交回转文本，再交给后面的结果。
		return strconv.Itoa(n), m[2], true
	}
	// 宿主配置解出来了才拿掉互相冲突的项。
	if m := flywaySQLName.FindStringSubmatch(name); m != nil {
		// 转整数，再交给后面。
		n, err := strconv.Atoi(m[1])
		// 没能转整数，再交给后面就停，避免带着残缺继续。
		if err != nil {
			return "", "", false
		}
		// 交回转文本，再交给后面的结果。
		return strconv.Itoa(n), m[2], true
	}
	return "", "", false
}

// listSQL 列出待套用的 .sql，按文件名排序。
func listSQL(fsys fs.FS, dir string) ([]string, error) {
	// 列出目录中的名字。
	entries, err := fs.ReadDir(fsys, dir)
	// 没能列出目录中的名字就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 按需要的长度把缓冲准备好。
	names := make([]string, 0, len(entries))
	// 逐个看目录项，只要脚本文件。
	for _, e := range entries {
		// 对不上就换一路，避免把不符的当成通过。
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		// 把这一段接进结果，顺序要保持住。
		names = append(names, e.Name())
	}
	// 做完这一步，再交给后面。
	sort.Strings(names)
	return names, nil
}

// splitSQL 按分号切语句，但不切开单引号串、$$ 函数体和行注释里的分号。
func splitSQL(s string) []string {
	// 准备收集字符串结果。
	var out []string
	// 准备拼文本，避免来回分配。
	var cur strings.Builder
	// 把积攒的一段送进结果，空段丢掉。
	flush := func() {
		// 有内容才继续处理这一支。
		if t := strings.TrimSpace(cur.String()); t != "" {
			// 把这一段接进结果，顺序要保持住。
			out = append(out, t)
		}
		// 清空后重新攒，再交给后面。
		cur.Reset()
	}
	// 按下标扫过去，直到越界或提前结束。
	for i := 0; i < len(s); {
		// 看当前字符落在哪一种片段里，分号才好切。
		switch {
		// 注释里面的分号不能当成语句结束。
		case strings.HasPrefix(s[i:], "--"):
			// 行注释整行照抄，里面的分号不算语句结束。
			end := strings.IndexByte(s[i:], '\n')
			// 这一行没有换行，就一直取到末尾。
			if end < 0 {
				// 看有多长，空的和超限的要分开处理。
				end = len(s) - i
			}
			// 把这段文字接进缓冲。
			cur.WriteString(s[i : i+end])
			// 定下位置，再交给后面。
			i += end
		// 引号里面的分号不能当成语句结束。
		case s[i] == '\'':
			// 单引号串，'' 是转义；没闭合就照抄到结尾交给数据库报错。
			j := i + 1
			// 逐项处理，空的就不进入循环。
			for j < len(s) {
				// 对上了才走这一路，其余分开处理。
				if s[j] == '\'' {
					// 对上了才走这一路，其余分开处理。
					if j+1 < len(s) && s[j+1] == '\'' {
						// 跳过注释开头的两个横线。
						j += 2
						continue
					}
					break
				}
				// 普通字符前进一步。
				j++
			}
			// 条件不成立就换一路，避免误往下做。
			if j < len(s) {
				// 普通字符前进一步。
				j++
			}
			// 把这段文字接进缓冲。
			cur.WriteString(s[i:j])
			// 定下位置，再交给后面。
			i = j
		// 函数体里的分号不能当成语句结束。
		case s[i] == '$':
			// 做完这一步，再交给后面。
			tag := dollarTag(s[i:])
			// 是空的就改用默认，或按没有处理。
			if tag == "" {
				// 往当前这段里写入一个字节。
				cur.WriteByte(s[i])
				// 定下位置，再交给后面。
				i++
				continue
			}
			// 看有多长，空的和超限的要分开处理。
			j := len(s)
			// 条件不成立就换一路，避免误往下做。
			if end := strings.Index(s[i+len(tag):], tag); end >= 0 {
				// 看有多长，空的和超限的要分开处理。
				j = i + len(tag) + end + len(tag)
			}
			// 把这段文字接进缓冲。
			cur.WriteString(s[i:j])
			// 定下位置，再交给后面。
			i = j
		// 到这里才是一条语句的结束。
		case s[i] == ';':
			// 送出，再交给后面，再交给后面。
			flush()
			// 定下位置，再交给后面。
			i++
		// 其余字符原样抄进当前这一段。
		default:
			// 往当前这段里写入一个字节。
			cur.WriteByte(s[i])
			// 定下位置，再交给后面。
			i++
		}
	}
	// 送出，再交给后面，再交给后面。
	flush()
	return out
}

// dollarTag 识别 $$ 或 $tag$ 开头，返回完整标记；不是则返回空。
func dollarTag(s string) string {
	// 对不上就换一路，避免把不符的当成通过。
	if len(s) < 2 || s[0] != '$' {
		return ""
	}
	// 按下标扫过去，直到越界或提前结束。
	for j := 1; j < len(s); j++ {
		// 先把这一步的结果放下，后面还要用。
		c := s[j]
		// 对上了才走这一路，其余分开处理。
		if c == '$' {
			return s[:j+1]
		}
		// 对上了才走这一路，其余分开处理。
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 1 && c >= '0' && c <= '9') {
			return ""
		}
	}
	return ""
}
