// Package migrate 用 goose 只向前套用 SQL：版本记在 goose_db_version，不做 down，也不改已落库的历史数据。
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/gorm"
)

// VersionTable goose 记已套版本的表，沿用 goose 默认表名。
const VersionTable = "goose_db_version"

// lockKey 迁移锁的 advisory lock 键，与厂端同值；PG 的这种锁按库区分，只在同一 WAN 库内排队。
const lockKey int64 = 0x574d657368

// lockWaitSeconds 等迁移锁的上限（秒）：过渡用的事务锁和 goose 的会话锁同一口径，测试里可缩短。
var lockWaitSeconds uint64 = 300

// legacyName 旧执行器的文件名，开头数字就是版本，如 0001_init.up.sql。
var legacyName = regexp.MustCompile(`^(\d+)_.+\.sql$`)

// versionTableComments 版本表的库内注释；老库过渡和新库建表都补上。
var versionTableComments = []string{
	`COMMENT ON TABLE goose_db_version IS 'goose 已套用的向前迁移版本；只增不删，禁止 down'`,
	`COMMENT ON COLUMN goose_db_version.id IS '登记顺序，自增'`,
	`COMMENT ON COLUMN goose_db_version.version_id IS '迁移版本号，取文件名开头数字；0 是建表时的起点'`,
	`COMMENT ON COLUMN goose_db_version.is_applied IS '是否已套用；本仓只向前，恒为真'`,
	`COMMENT ON COLUMN goose_db_version.tstamp IS '登记时间；老库过渡登记的是过渡当时'`,
}

// Error 迁移没做完：失败的版本整段回滚、不记版本，下次再迁会自动重试。
type Error struct {
	File string // 没套上的迁移文件名；锁超时、解析失败等说不清是哪支时为空
	Err  error  // 库或 goose 给的原错误
}

// Error 带上失败的文件名，方便日志里直接对到是哪支脚本。
func (e *Error) Error() string {
	// 说不清是哪支脚本时只报原错误。
	if e.File == "" {
		// 交回带前缀的原错误文字。
		return fmt.Sprintf("migrate: %v", e.Err)
	}
	// 交回带文件名的错误文字。
	return fmt.Sprintf("migrate %s: %v", e.File, e.Err)
}

// Unwrap 让调用方仍能按原错误判断，比如库连不上。
func (e *Error) Unwrap() error {
	return e.Err
}

// Up 按版本顺序套用尚未登记的 SQL；每个文件一个事务，失败回滚、不记版本，下次自动重试。
// 同一库的多路迁移（多进程、多实例或并行测试）凭库内迁移锁排队，后到的等前面迁完再看版本。
func Up(db *gorm.DB, fsys fs.FS, dir string) error {
	// 迁移跟着进程走，不设超时；只给等锁设上限。
	ctx := context.Background()
	// 取出底层连接池，goose 只认标准库的库句柄。
	sqlDB, err := db.DB()
	// 取不出连接池就没法迁，原样交回。
	if err != nil {
		return err
	}
	// 只把迁移目录交给 goose，目录外的文件不当迁移。
	sub, err := fs.Sub(fsys, dir)
	// 目录不合法就停，免得迁了一半目录。
	if err != nil {
		return err
	}
	// 老库先按旧历史登记已套版本，免得 goose 从头重跑。
	if err := adoptLegacy(ctx, sqlDB); err != nil {
		// 过渡失败整段回滚，下次启动再试。
		return &Error{Err: err}
	}
	// 用库内会话锁排队，每秒探一次，最多等五分钟。
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(lockKey), lock.WithLockTimeout(1, lockWaitSeconds))
	// 锁参数不合法属于代码错误，直接交回。
	if err != nil {
		return err
	}
	// 只认本目录里的 SQL，不掺进程里登记的 Go 迁移。
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub, goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	// 目录里没有迁移或版本重号都在这里报。
	if err != nil {
		// 带上前缀交回，调用方据此认作迁移失败。
		return &Error{Err: err}
	}
	// 套用全部未登记的版本，已登记的跳过。
	if _, err := provider.Up(ctx); err != nil {
		// 换成带文件名的错误交回。
		return wrapUpError(err)
	}
	return nil
}

// wrapUpError 从 goose 的部分失败里取出失败文件名，其余错误照样包一层。
func wrapUpError(err error) error {
	// 准备接 goose 的部分失败，里面记着失败的那支。
	var partial *goose.PartialError
	// 是某支脚本执行失败，才说得出文件名。
	if errors.As(err, &partial) && partial.Failed != nil && partial.Failed.Source != nil {
		// 只留文件名和库给的原因，goose 的版本前缀去掉。
		return &Error{File: path.Base(partial.Failed.Source.Path), Err: partial.Err}
	}
	// 锁超时、解析失败等说不清是哪支，原错误照留。
	return &Error{Err: err}
}

// Version 读库里已套到的最高版本，给探活看；还没登记过任何版本时回 0。
// 要限时就先给 db 挂上下文（db.WithContext）。
func Version(db *gorm.DB) (int64, error) {
	// 用调用方挂的上下文，没挂就不限时。
	ctx := db.Statement.Context
	// 没挂上下文就用空白的，不限时。
	if ctx == nil {
		// 换成空白上下文。
		ctx = context.Background()
	}
	// 取出底层连接池，goose 只认标准库的库句柄。
	sqlDB, err := db.DB()
	// 取不出连接池就读不了版本。
	if err != nil {
		return 0, err
	}
	// 用 goose 的版本表读法，口径与迁移时一致。
	store, err := database.NewStore(database.DialectPostgres, VersionTable)
	// 方言写死，这里出错属于代码错误。
	if err != nil {
		return 0, err
	}
	// 取版本表里的最大版本。
	v, err := store.GetLatestVersion(ctx, sqlDB)
	// 表在但一行没有，按还没迁过算。
	if errors.Is(err, database.ErrVersionNotFound) {
		return 0, nil
	}
	return v, err
}

// adoptLegacy 版本表还不在时建出来并补注释；老库按旧历史把已套版本先登记上。
// 新旧进程同时过渡凭事务级迁移锁排队，旧的 flyway_schema_history 和 schema_migrations 原样保留。
func adoptLegacy(ctx context.Context, sqlDB *sql.DB) error {
	// 用 goose 的版本表建法，与它自建的表一模一样。
	store, err := database.NewStore(database.DialectPostgres, VersionTable)
	// 方言写死，这里出错属于代码错误。
	if err != nil {
		return err
	}
	// 先不取锁看一眼，已过渡的库每次启动不必排队。
	if ok, err := tableExists(ctx, sqlDB, VersionTable); err != nil || ok {
		return err
	}
	// 建表、登记和注释放在一个事务里，失败什么都不留。
	tx, err := sqlDB.BeginTx(ctx, nil)
	// 开不了事务就没法过渡。
	if err != nil {
		return err
	}
	// 没提交就回滚；提交过再回滚只是空操作。
	defer func() {
		// 回滚失败不盖过原错误。
		_ = tx.Rollback()
	}()
	// 等锁与 goose 会话锁同一上限；持锁的进程卡死不放时报错交回，不无限挂着。
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%ds'", lockWaitSeconds)); err != nil {
		return err
	}
	// 事务级迁移锁与 goose 会话锁同键，别的进程正在迁就等它完。
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return err
	}
	// 拿到锁再看一次，等锁期间别人可能已经过渡完。
	if ok, err := tableExists(ctx, tx, VersionTable); err != nil || ok {
		return err
	}
	// 读出旧历史里已套过的版本，新库为空。
	versions, err := legacyVersions(ctx, tx)
	// 旧历史读不懂就停，不能猜着登记。
	if err != nil {
		return err
	}
	// 建版本表，表结构交给 goose 定。
	if err := store.CreateVersionTable(ctx, tx); err != nil {
		return err
	}
	// 先记版本 0，与 goose 自建表时一致。
	if err := store.Insert(ctx, tx, database.InsertRequest{Version: 0}); err != nil {
		return err
	}
	// 按版本从小到大登记旧库已套过的。
	for _, v := range versions {
		// 登记不上就整段回滚，下次再试。
		if err := store.Insert(ctx, tx, database.InsertRequest{Version: v}); err != nil {
			return err
		}
	}
	// 给版本表和各列补库内注释。
	for _, stmt := range versionTableComments {
		// 注释写不上也回滚，免得留下没注释的表。
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// 交回提交的结果。
	return tx.Commit()
}

// legacyVersions 读旧执行器记下的已套版本：先认 flyway_schema_history 的成功行，没有再认 schema_migrations。
func legacyVersions(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	// 先看旧 Flyway 历史表在不在。
	ok, err := tableExists(ctx, tx, "flyway_schema_history")
	// 查不了表就停，免得漏登记。
	if err != nil {
		return nil, err
	}
	// 表在才去读成功行。
	if ok {
		// 只认成功的行，失败的当没套过。
		got, err := scanStrings(ctx, tx, "SELECT version FROM flyway_schema_history WHERE success AND version IS NOT NULL")
		// 读不出就停，免得漏登记。
		if err != nil {
			return nil, err
		}
		// 有成功行就以它为准。
		if len(got) > 0 {
			// 交回按数字收好的版本。
			return parseVersions(got, false)
		}
	}
	// 再看更老的 schema_migrations 在不在。
	ok, err = tableExists(ctx, tx, "schema_migrations")
	// 查不了表或表不在都到此为止。
	if err != nil || !ok {
		return nil, err
	}
	// 读出已套过的文件名。
	names, err := scanStrings(ctx, tx, "SELECT name FROM schema_migrations")
	// 读不出就停，免得漏登记。
	if err != nil {
		return nil, err
	}
	// 交回按文件名开头数字收好的版本。
	return parseVersions(names, true)
}

// parseVersions 把旧历史里的版本串或文件名收成去重升序的版本号；读不懂就报错，不猜。
func parseVersions(raw []string, byFileName bool) ([]int64, error) {
	// 按版本去重，同一版本只登记一次。
	seen := make(map[int64]bool, len(raw))
	// 逐条解析。
	for _, s := range raw {
		// 先去掉首尾空白，版本串可能带空格。
		s = strings.TrimSpace(s)
		// 按文件名记的要先取开头数字。
		if byFileName {
			// 按旧文件名规则取开头数字。
			m := legacyName.FindStringSubmatch(s)
			// 文件名不合规就停，不能猜版本。
			if m == nil {
				// 报出读不懂的文件名。
				return nil, fmt.Errorf("legacy migration name %q", s)
			}
			// 换成开头数字，后面统一转版本号。
			s = m[1]
		}
		// 转成版本号。
		v, err := strconv.ParseInt(s, 10, 64)
		// 不是整数就停，不能猜版本。
		if err != nil {
			// 报出读不懂的版本串。
			return nil, fmt.Errorf("legacy migration version %q: %w", s, err)
		}
		// 版本 0 建表时已记，这里只收正数。
		if v > 0 {
			// 记下这一版。
			seen[v] = true
		}
	}
	// 按需要的长度准备结果。
	out := make([]int64, 0, len(seen))
	// 收进结果。
	for v := range seen {
		// 把这一版放进结果。
		out = append(out, v)
	}
	// 从小到大登记，与 goose 按顺序套用一致。
	sort.Slice(out, func(i, j int) bool {
		// 小的排前面。
		return out[i] < out[j]
	})
	return out, nil
}

// queryer 事务和连接池都能查，过渡前后共用一套查表写法。
type queryer interface {
	// QueryContext 执行查询并交回结果行。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext 执行查询并交回一行。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// tableExists 在当前模式里查表在不在，与建表时不带模式名的写法对得上。
// 不用 to_regclass：它走目录缓存，咨询锁不刷新缓存，排完队还会看到旧的「没表」。
func tableExists(ctx context.Context, q queryer, name string) (bool, error) {
	// 准备接查表结果。
	var ok bool
	// 直接读 pg_tables，每条语句取新快照，别人刚提交的表也看得到。
	err := q.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname = current_schema() AND tablename = $1)", name).Scan(&ok)
	return ok, err
}

// scanStrings 执行只取一列文本的查询，交回全部行。
func scanStrings(ctx context.Context, q queryer, query string) ([]string, error) {
	// 执行查询。
	rows, err := q.QueryContext(ctx, query)
	// 查不了就停。
	if err != nil {
		return nil, err
	}
	// 读完关掉结果集，连接才能还回去。
	defer func() {
		// 关闭失败不盖过原错误。
		_ = rows.Close()
	}()
	// 准备收集各行文本。
	var out []string
	// 逐行读出。
	for rows.Next() {
		// 准备接这一行。
		var s string
		// 扫不出就停。
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		// 收下这一行。
		out = append(out, s)
	}
	// 交回读行过程中的错误。
	return out, rows.Err()
}
