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
	// 历史表必须先在，否则没法记套用。
	if err := ensureHistory(db); err != nil {
		return err
	}
	// 旧库只有 schema_migrations 时，按已套用文件登记 Flyway 历史，不重跑。
	if err := baselineLegacy(db, fsys, dir); err != nil {
		return err
	}
	// 列出待套用的脚本。
	names, err := listSQL(fsys, dir)
	// 列不出来就停，避免漏套。
	if err != nil {
		return err
	}
	// 记下当前库用户，写进历史。
	who, err := currentUser(db)
	// 用户取不到就不套，历史不能空着操作者。
	if err != nil {
		return err
	}
	// 按文件名顺序，跳过已经成功的。
	for _, name := range names {
		// 查这支脚本是否已经成功套过。
		ok, err := alreadyApplied(db, name)
		// 查失败则停住，避免重复执行。
		if err != nil {
			return err
		}
		// 成功过的不再执行。
		if ok {
			continue
		}
		// 把这支脚本的正文读出来。
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		// 读不到就不能套这一支。
		if err != nil {
			// 带上文件名交回，方便对是哪一支。
			return fmt.Errorf("read %s: %w", name, err)
		}
		// 从文件名取版本和描述。
		version, desc, ok := parseVersionedSQL(name)
		// 名字不合规则就不能套。
		if !ok {
			// 带上文件名交回，避免悄悄跳过。
			return fmt.Errorf("invalid migration name %s", name)
		}
		// 记下开始时间，用来算耗时。
		start := time.Now()
		// 一个文件放进一个事务，失败整支回滚。
		err = db.Transaction(func(tx *gorm.DB) error {
			// 按切开的语句逐条执行。
			for i, stmt := range splitSQL(string(body)) {
				// 这一条失败则整文件回滚。
				if err := tx.Exec(stmt).Error; err != nil {
					// 带上文件名和序号交回。
					return fmt.Errorf("apply %s #%d: %w", name, i+1, err)
				}
			}
			// 耗时收成毫秒再记历史。
			ms := int(time.Since(start).Milliseconds())
			// 全部成功才追加历史，失败不留行。
			return insertHistory(tx, who, version, desc, name, checksumOf(body), ms)
		})
		// 事务失败则停住，重启可重试这一支。
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureHistory 建 Flyway 历史表；并保留 schema_migrations 让已发布的 COMMENT ON 仍能套上。
func ensureHistory(db *gorm.DB) error {
	// 先保住旧表，已发布的注释迁移才套得上。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error; err != nil {
		return err
	}
	// 历史表、索引和库内注释一次列齐。
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
	// 逐条执行，任一条失败就停。
	for _, stmt := range stmts {
		// 这一条没建成，后面的历史也不可信。
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// baselineLegacy 把旧 schema_migrations 抄进 Flyway 历史，避免换历史表后重跑。
func baselineLegacy(db *gorm.DB, fsys fs.FS, dir string) error {
	// 准备数新历史里已有多少行。
	var n int64
	// 数失败就不敢抄，以免重跑已套用的脚本。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history").Scan(&n).Error; err != nil {
		return err
	}
	// 已经有历史就不用再抄旧表。
	if n > 0 {
		return nil
	}
	// 没有旧表就无从基线。
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	// 准备接旧表里的文件名。
	var names []string
	// 按名字读出已套用的文件。
	if err := db.Raw("SELECT name FROM schema_migrations ORDER BY name").Scan(&names).Error; err != nil {
		return err
	}
	// 旧表是空的就不用抄。
	if len(names) == 0 {
		return nil
	}
	// 记下当前库用户，写进新历史。
	who, err := currentUser(db)
	// 用户取不到就不抄。
	if err != nil {
		return err
	}
	// 整批抄进新历史，中途失败就都不算。
	return db.Transaction(func(tx *gorm.DB) error {
		// 每个旧文件名补一行成功历史。
		for _, name := range names {
			// 从旧文件名取版本和描述。
			version, desc, ok := parseVersionedSQL(name)
			// 名字不合规则就不能当基线。
			if !ok {
				// 带上文件名交回，避免悄悄跳过。
				return fmt.Errorf("invalid legacy migration name %s", name)
			}
			// 读不到原文就先记零校验和。
			sum := 0
			// 读得到才用正文算校验和。
			if body, err := fs.ReadFile(fsys, path.Join(dir, name)); err == nil {
				// 用正文算出校验和，改文件能对出来。
				sum = checksumOf(body)
			}
			// 写入失败则整批回滚。
			if err := insertHistory(tx, who, version, desc, name, sum, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// alreadyApplied 该脚本已成功套用则跳过。
func alreadyApplied(db *gorm.DB, script string) (bool, error) {
	// 准备数这支脚本的成功行。
	var n int64
	// 只数成功行，失败不记所以不会挡住重试。
	err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE script = ? AND success", script).Scan(&n).Error
	return n > 0, err
}

// insertHistory 追加一条成功记录；失败的文件不进表，重启可重试。
func insertHistory(tx *gorm.DB, who, version, description, script string, checksum, elapsedMs int) error {
	// 准备接当前最大顺序。
	var rank int
	// 取不到顺序就不能追加，避免序号撞车。
	if err := tx.Raw("SELECT COALESCE(MAX(installed_rank), 0) FROM flyway_schema_history").Scan(&rank).Error; err != nil {
		return err
	}
	// 顺序加一，记下这一支成功。
	if err := tx.Exec(`INSERT INTO flyway_schema_history
		(installed_rank, version, description, type, script, checksum, installed_by, execution_time, success)
		VALUES (?, ?, ?, 'SQL', ?, ?, ?, ?, TRUE)`,
		rank+1, version, description, script, checksum, who, elapsedMs).Error; err != nil {
		return err
	}
	// 旧表也记一笔，已发布注释仍能对上文件名。
	return tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, NOW()) ON CONFLICT (name) DO NOTHING`, script).Error
}

// currentUser 记下谁套用了脚本，给 Flyway 历史用。
func currentUser(db *gorm.DB) (string, error) {
	// 准备接当前库用户。
	var who string
	// 取不到用户就不记历史。
	if err := db.Raw("SELECT CURRENT_USER").Scan(&who).Error; err != nil {
		return "", err
	}
	// 空名补一个固定名字，历史列不能空。
	if who == "" {
		// 用本系统的名字占位。
		who = "wmesh"
	}
	return who, nil
}

// checksumOf 用 CRC32 当 Flyway checksum。
func checksumOf(body []byte) int {
	// 收成有符号整数，跟历史表整数列对齐。
	return int(int32(crc32.ChecksumIEEE(body)))
}

// parseVersionedSQL 从 0024_software.up.sql 或 V0024__software.sql 取出版本和描述。
func parseVersionedSQL(name string) (version, description string, ok bool) {
	// 先认旧式「序号_描述.up.sql」。
	if m := legacySQLName.FindStringSubmatch(name); m != nil {
		// 文件名里的序号必须是整数。
		n, err := strconv.Atoi(m[1])
		// 序号解析失败则这个名字作废。
		if err != nil {
			return "", "", false
		}
		// 去掉前导零再交出版本和描述。
		return strconv.Itoa(n), m[2], true
	}
	// 再认 Flyway 式「V序号__描述.sql」。
	if m := flywaySQLName.FindStringSubmatch(name); m != nil {
		// 文件名里的序号必须是整数。
		n, err := strconv.Atoi(m[1])
		// 序号解析失败则这个名字作废。
		if err != nil {
			return "", "", false
		}
		// 去掉前导零再交出版本和描述。
		return strconv.Itoa(n), m[2], true
	}
	return "", "", false
}

// listSQL 列出待套用的 .sql，按文件名排序。
func listSQL(fsys fs.FS, dir string) ([]string, error) {
	// 读出目录里的条目。
	entries, err := fs.ReadDir(fsys, dir)
	// 目录读不到就没有可套用的脚本。
	if err != nil {
		return nil, err
	}
	// 按条目数预留名字表。
	names := make([]string, 0, len(entries))
	// 只要普通的 SQL 文件。
	for _, e := range entries {
		// 目录和别的后缀跳过。
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		// 把这个脚本文件名收下。
		names = append(names, e.Name())
	}
	// 按文件名排序，版本顺序才稳定。
	sort.Strings(names)
	return names, nil
}

// splitSQL 按分号切语句，但不切开单引号串、$$ 函数体和行注释里的分号。
func splitSQL(s string) []string {
	// 先攒下已经切开的语句。
	var out []string
	// 当前还没切完的一段。
	var cur strings.Builder
	// 一段收口：去掉空白，空段丢掉。
	flush := func() {
		// 去掉首尾空白后还有字才算一句。
		if t := strings.TrimSpace(cur.String()); t != "" {
			// 收进结果，顺序保持原文。
			out = append(out, t)
		}
		// 清空缓冲，准备下一句。
		cur.Reset()
	}
	// 逐字符看，引号和函数体里的分号不算结束。
	for i := 0; i < len(s); {
		// 按注释、引号、美元标记和分号分流。
		switch {
		// 行注释一直抄到换行。
		case strings.HasPrefix(s[i:], "--"):
			// 行注释整行照抄，里面的分号不算语句结束。
			end := strings.IndexByte(s[i:], '\n')
			// 没有换行就抄到结尾。
			if end < 0 {
				// 整段余下都算注释。
				end = len(s) - i
			}
			// 注释原文留下，不丢掉。
			cur.WriteString(s[i : i+end])
			// 指针跳过这段注释。
			i += end
		// 单引号串整段照抄。
		case s[i] == '\'':
			// 单引号串，'' 是转义；没闭合就照抄到结尾交给数据库报错。
			j := i + 1
			// 找到成对的引号，双写引号不当结束。
			for j < len(s) {
				// 碰到引号才可能是结束或转义。
				if s[j] == '\'' {
					// 两个引号是转义，要一起跳过。
					if j+1 < len(s) && s[j+1] == '\'' {
						// 跳过这一对转义引号。
						j += 2
						continue
					}
					break
				}
				// 普通字符继续往前找。
				j++
			}
			// 找到了结束引号就把它算进串里。
			if j < len(s) {
				// 把结束引号纳入这一段。
				j++
			}
			// 整段字符串照抄，里面的分号不算结束。
			cur.WriteString(s[i:j])
			// 指针跳到串的后面。
			i = j
		// 美元引号可能是函数体。
		case s[i] == '$':
			// 认出 $$ 或 $tag$，认不出就当普通字符。
			tag := dollarTag(s[i:])
			// 不是标记就只抄这一个字符。
			if tag == "" {
				// 普通美元符号留在语句里。
				cur.WriteByte(s[i])
				// 指针前进一格，继续往下看。
				i++
				continue
			}
			// 找不到结束标记就抄到文件尾。
			j := len(s)
			// 找到成对标记才在那里结束。
			if end := strings.Index(s[i+len(tag):], tag); end >= 0 {
				// 结束位置含两边的标记。
				j = i + len(tag) + end + len(tag)
			}
			// 函数体整段照抄，里面的分号不算结束。
			cur.WriteString(s[i:j])
			// 指针跳到函数体后面。
			i = j
		// 这个分号表示一句结束了。
		case s[i] == ';':
			// 把这一句收进结果。
			flush()
			// 分号本身不留在下一句里。
			i++
		// 普通字符留在当前句里。
		default:
			// 把这一个普通字符抄下来。
			cur.WriteByte(s[i])
			// 指针前进一格继续扫描。
			i++
		}
	}
	// 最后一段没有分号也要收下。
	flush()
	return out
}

// dollarTag 识别 $$ 或 $tag$ 开头，返回完整标记；不是则返回空。
func dollarTag(s string) string {
	// 至少要有起始美元符和结束美元符。
	if len(s) < 2 || s[0] != '$' {
		return ""
	}
	// 在下一个美元符前只允许标记名用的字符。
	for j := 1; j < len(s); j++ {
		// 取出这一位，看是结束还是非法字符。
		c := s[j]
		// 再次碰到美元符，这一段就是完整标记。
		if c == '$' {
			return s[:j+1]
		}
		// 字母、下划线，以及非首位的数字才合法。
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 1 && c >= '0' && c <= '9') {
			return ""
		}
	}
	return ""
}
