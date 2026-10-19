// 迁移规则门禁：文件名、版本连续、goose 注解与库内注释，开发期跑 go test 即拦住。
package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

// migrationName 迁移文件名：四位版本号、小写名、只向前的 .up.sql。
var migrationName = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.up\.sql$`)

// tableConstraints 表约束的打头词；建表与加约束时跳过，不当列名。
var tableConstraints = []string{"constraint", "primary", "unique", "check", "foreign", "exclude"}

// tableModifiers CREATE 与 TABLE 之间可出现的修饰词。
var tableModifiers = []string{"global", "local", "temp", "temporary", "unlogged"}

// TestMigrationRules 仓里现有的全部迁移都要守住规则，新文件同样。
func TestMigrationRules(t *testing.T) {
	// 逐条列出违规，方便一次改完。
	for _, p := range checkRules(os.DirFS(".")) {
		// 每条违规单独报一行。
		t.Error(p)
	}
}

// sqlNames 列出目录顶层的全部 .sql 文件名并排序；后缀不论大小写，免得漏掉写错的。
func sqlNames(fsys fs.FS) ([]string, error) {
	// 只看顶层，子目录既不嵌入也不归 goose 读。
	entries, err := fs.ReadDir(fsys, ".")
	// 读不了目录就没法核对。
	if err != nil {
		return nil, err
	}
	// 准备收集 SQL 文件名。
	var names []string
	// 逐个挑出 SQL 文件。
	for _, e := range entries {
		// 目录和非 SQL 文件不归迁移管。
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			continue
		}
		// 记下这个 SQL 文件。
		names = append(names, e.Name())
	}
	// 再排一次，防别的 FS 实现乱序。
	slices.Sort(names)
	return names, nil
}

// checkRules 核对迁移目录的全部规则，返回违规说明；为空表示全部通过。
func checkRules(fsys fs.FS) []string {
	// 先列出全部 SQL 文件。
	names, err := sqlNames(fsys)
	// 目录读不了就只报这一条。
	if err != nil {
		// 交回读目录的错误。
		return []string{fmt.Sprintf("读迁移目录失败：%v", err)}
	}
	// 文件名和版本号先核一遍。
	problems := checkVersions(names)
	// 准备按顺序重放各文件的建表、加列与注释。
	sch := &schema{tables: map[string]*table{}}
	// 按文件名即版本顺序逐个核对。
	for _, name := range names {
		// 读出脚本正文。
		data, err := fs.ReadFile(fsys, name)
		// 读不了就记下，接着看下一个。
		if err != nil {
			// 记下读失败的文件。
			problems = append(problems, fmt.Sprintf("%s 读不出：%v", name, err))
			continue
		}
		// 核对 goose 注解。
		annotated := checkAnnotations(name, string(data))
		// 注解问题照记。
		problems = append(problems, annotated...)
		// 文件名和注解都对时，再让 goose 自己断句空跑一遍。
		if len(annotated) == 0 && migrationName.MatchString(name) {
			// 记下 goose 解析失败或把语句切断的地方。
			problems = append(problems, checkGooseSplit(name, data)...)
		}
		// 重放本文件的表结构变化。
		problems = append(problems, sch.replay(name, string(data))...)
	}
	// 最后看现存的表和列是否都有库内注释。
	return append(problems, sch.missingComments()...)
}

// checkVersions 核对文件名格式，以及版本从 1 起连续、不重号。
func checkVersions(names []string) []string {
	// 一个迁移都没有，goose 也会拒绝启动。
	if len(names) == 0 {
		return []string{"迁移目录里一个 .sql 都没有"}
	}
	// 收集违规说明。
	var problems []string
	// 版本号到文件名，查重号用。
	seen := map[int]string{}
	// 合规文件的版本号，查连续用。
	var versions []int
	// 逐个文件核对名字。
	for _, name := range names {
		// 名字不合规就报，版本号也不取。
		if !migrationName.MatchString(name) {
			// 写明要求的格式。
			problems = append(problems, fmt.Sprintf("%s 文件名不合规：要写成 NNNN_小写字母数字下划线.up.sql", name))
			continue
		}
		// 开头四位就是版本号，goose 也按它排。
		v, _ := strconv.Atoi(name[:4])
		// 版本号已被别的文件占了就是重号。
		if other, ok := seen[v]; ok {
			// 两个文件都点名，方便挑一个改号。
			problems = append(problems, fmt.Sprintf("%s 与 %s 版本号重复", other, name))
			continue
		}
		// 记下这个版本号归谁。
		seen[v] = name
		// 留着查连续。
		versions = append(versions, v)
	}
	// 按版本从小到大排。
	slices.Sort(versions)
	// 第 i 个版本必须正好是 i+1。
	for i, v := range versions {
		// 对不上就是从 1 起有缺号。
		if v != i+1 {
			// 只报第一处缺号，后面都会跟着错位。
			problems = append(problems, fmt.Sprintf("版本要从 0001 起连续：缺 %04d，下一个是 %s", i+1, seen[v]))
			break
		}
	}
	return problems
}

// checkAnnotations 照 goose 口径核注解：一行 Up 在前，无 Down，Begin/End 成对，不许关事务或用 envsub。
func checkAnnotations(name, src string) []string {
	// 收集本文件的违规。
	var problems []string
	// Up 注解出现的次数。
	ups := 0
	// 是否正处在 StatementBegin 与 StatementEnd 之间。
	open := false
	// Up 之前是否已经写了 SQL。
	sqlBeforeUp := false
	// 逐行看，goose 也是按行认注解。
	for i, line := range strings.Split(src, "\n") {
		// 去掉首尾空白再判断。
		trimmed := strings.TrimSpace(line)
		// 和 goose 同口径：注释行里带 +goose 才算注解。
		if !strings.HasPrefix(trimmed, "--") || !strings.Contains(line, "+goose") {
			// 非空非注释行出现在 Up 之前，goose 会拒绝。
			if ups == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "--") {
				// 记下有 SQL 抢在 Up 前面。
				sqlBeforeUp = true
			}
			continue
		}
		// goose 不认缩进的注解行，整个文件会解析失败。
		if trimmed != strings.TrimRight(line, " \t\r") {
			// 记下缩进的注解行。
			problems = append(problems, fmt.Sprintf("%s 第 %d 行注解不能缩进", name, i+1))
		}
		// 去掉 -- 和 +goose，剩下的就是注解名，大小写不论。
		cmd := strings.ToLower(strings.TrimSpace(strings.Replace(strings.ReplaceAll(line, "--", ""), "+goose", "", 1)))
		// 按注解名分别核对。
		switch cmd {
		// Up 只记次数，最后统一核。
		case "up":
			// 记一次 Up。
			ups++
		// 本仓只向前，Down 一律不许。
		case "down":
			// 记下写了 Down 的位置。
			problems = append(problems, fmt.Sprintf("%s 第 %d 行写了 -- +goose Down：只向前迁移，禁止 down", name, i+1))
		// 整段语句开头，不能嵌套。
		case "statementbegin":
			// 已经开着又开就是嵌套。
			if open {
				// 记下嵌套的位置。
				problems = append(problems, fmt.Sprintf("%s 第 %d 行 StatementBegin 嵌套了，前一段还没 StatementEnd", name, i+1))
			}
			// 进入整段语句。
			open = true
		// 整段语句收尾，要有对应的开头。
		case "statementend":
			// 没开就收是多余的 End。
			if !open {
				// 记下多余 End 的位置。
				problems = append(problems, fmt.Sprintf("%s 第 %d 行 StatementEnd 前面没有 StatementBegin", name, i+1))
			}
			// 整段语句结束。
			open = false
		// 不开事务失败会留半套要人工修，envsub 让表结构随环境变，都不许。
		case "no transaction", "envsub on", "envsub off":
			// 记下禁用注解的位置。
			problems = append(problems, fmt.Sprintf("%s 第 %d 行写了 %q：本仓迁移必须在事务里执行、不许依赖环境变量", name, i+1, trimmed))
		// 其余注解 goose 都不认。
		default:
			// goose 不认的注解会让整个文件解析失败。
			problems = append(problems, fmt.Sprintf("%s 第 %d 行是 goose 不认的注解 %q", name, i+1, trimmed))
		}
	}
	// 一行 Up 都没有，goose 不认这个文件。
	if ups == 0 {
		// 记下缺 Up。
		problems = append(problems, fmt.Sprintf("%s 缺 -- +goose Up", name))
	}
	// 多行 Up，goose 报重复。
	if ups > 1 {
		// 记下 Up 重复。
		problems = append(problems, fmt.Sprintf("%s 写了 %d 行 -- +goose Up，只能一行", name, ups))
	}
	// 有 Up 但 SQL 写在它前面，goose 同样解析不了。
	if ups > 0 && sqlBeforeUp {
		// 记下 SQL 抢在 Up 前。
		problems = append(problems, fmt.Sprintf("%s 有 SQL 写在 -- +goose Up 之前", name))
	}
	// 开了 StatementBegin 到文件尾都没收。
	if open {
		// 记下缺 End。
		problems = append(problems, fmt.Sprintf("%s 的 StatementBegin 没有配对的 StatementEnd", name))
	}
	return problems
}

// checkGooseSplit 让 goose 按它自己的口径解析并空跑这个文件，拦住解析失败和把语句从中间切断的写法。
// 没包 StatementBegin 的 $$ 块、多行字符串里行尾的分号，goose 都会在行尾分号处切开。
func checkGooseSplit(name string, data []byte) []string {
	// 只记录不执行的假连接，不碰真库。
	rec := &recorder{}
	// 把假连接包成标准库的库句柄交给 goose。
	db := sql.OpenDB(rec)
	// 用完关掉假连接池。
	defer db.Close()
	// 只放这一个文件；关掉版本表后 goose 会把全部 Up 语句逐句发出来。
	p, err := goose.NewProvider(goose.DialectPostgres, db, fstest.MapFS{name: {Data: data}}, goose.WithDisableVersioning(true), goose.WithDisableGlobalRegistry(true))
	// goose 不认这个文件就照它的原话报。
	if err != nil {
		// 交回 goose 的原话。
		return []string{fmt.Sprintf("%s goose 不认：%v", name, err)}
	}
	// 空跑一遍，解析失败（比如末句缺分号）在这里报。
	if _, err := p.Up(context.Background()); err != nil {
		// 交回 goose 的原话，末句缺分号会点明 missing semicolon。
		return []string{fmt.Sprintf("%s goose 解析失败：%v", name, err)}
	}
	// 收集被切断的语句。
	var problems []string
	// 逐句看 goose 断出来的语句是否完整。
	for i, stmt := range rec.stmts {
		// 引号或 $$ 没收尾，说明 goose 在正文中间断了句。
		if _, err := lexSQL(stmt); err != nil {
			// 记下第几句被切断，并提示改法。
			problems = append(problems, fmt.Sprintf("%s 第 %d 句被 goose 从中间切断（%v）：含 $$ 或多行字符串的语句要包在 -- +goose StatementBegin/End 之间", name, i+1, err))
		}
	}
	return problems
}

// recorder 冒充数据库连接：goose 发来的语句只记下、不执行，事务一律成功。
type recorder struct {
	stmts []string // goose 依次发出的语句
}

// Connect 交回自身当连接，供 sql.OpenDB 用。
func (r *recorder) Connect(context.Context) (driver.Conn, error) {
	// 每次都交回同一个记录器。
	return r, nil
}

// Driver 交回自身当驱动，满足 driver.Connector。
func (r *recorder) Driver() driver.Driver {
	// 记录器自己就是驱动。
	return r
}

// Open 同 Connect，满足 driver.Driver。
func (r *recorder) Open(string) (driver.Conn, error) {
	// 不认连接串，交回同一个记录器。
	return r, nil
}

// Prepare 不支持预编译；goose 跑 SQL 迁移只用 ExecContext。
func (r *recorder) Prepare(string) (driver.Stmt, error) {
	// 真用到了就报错，免得悄悄漏记。
	return nil, errors.New("recorder: prepare not supported")
}

// Close 没有资源要放。
func (r *recorder) Close() error {
	// 什么都不用做。
	return nil
}

// Begin 开一个假事务，提交和回滚都不做事。
func (r *recorder) Begin() (driver.Tx, error) {
	// 记录器自己兼当事务。
	return r, nil
}

// Commit 假事务提交。
func (r *recorder) Commit() error {
	// 没有要落库的东西。
	return nil
}

// Rollback 假事务回滚。
func (r *recorder) Rollback() error {
	// 没有要撤回的东西。
	return nil
}

// ExecContext 记下 goose 发来的语句，回报零行受影响。
func (r *recorder) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	// 按发出顺序记下这一句。
	r.stmts = append(r.stmts, query)
	// 不执行，回报零行。
	return driver.RowsAffected(0), nil
}

// token SQL 拆出的一个词：关键字和未加引号的标识符一律小写，字符串只留占位。
type token struct {
	kind byte   // w 词或数字，q 双引号标识符，s 字符串，p 单个符号
	text string // 词的小写、引号里的原名或符号本身；字符串为空
}

// lexSQL 把脚本拆成语句：去掉注释，字符串与 $$ 正文各当一个占位，按顶层分号断句。
func lexSQL(src string) ([][]token, error) {
	// 已断好的语句。
	var stmts [][]token
	// 正在攒的这一句。
	var cur []token
	// 逐字节扫；非 ASCII 只会出现在注释、字符串或标识符里。
	for i := 0; i < len(src); {
		// 当前字节。
		c := src[i]
		// 按开头的字节分情况。
		switch {
		// 行注释跳到行尾。
		case strings.HasPrefix(src[i:], "--"):
			// 找这一行的换行。
			n := strings.IndexByte(src[i:], '\n')
			// 没有换行就是文件尾。
			if n < 0 {
				// 直接扫到末尾。
				i = len(src)
				continue
			}
			// 跳到换行处。
			i += n
		// 块注释可嵌套，跳到配对的收尾。
		case strings.HasPrefix(src[i:], "/*"):
			// 找配对的 */。
			j, err := skipBlockComment(src, i)
			// 没收尾就整份拆不开。
			if err != nil {
				return nil, err
			}
			// 跳过整段注释。
			i = j
		// 单引号字符串，'' 是转义。
		case c == '\'':
			// 找字符串收尾。
			j, err := skipQuoted(src, i, false)
			// 没收尾就整份拆不开。
			if err != nil {
				return nil, err
			}
			// 只留占位，正文里的关键字不算。
			cur = append(cur, token{kind: 's'})
			// 跳过整个字符串。
			i = j
		// 双引号标识符，大小写原样保留。
		case c == '"':
			// 找标识符收尾。
			j, err := skipQuoted(src, i, false)
			// 没收尾就整份拆不开。
			if err != nil {
				return nil, err
			}
			// 去掉两端引号，"" 还原成一个。
			cur = append(cur, token{kind: 'q', text: strings.ReplaceAll(src[i+1:j-1], `""`, `"`)})
			// 跳过整个标识符。
			i = j
		// $tag$ 开头是美元引号正文，否则是 $1 这类参数。
		case c == '$':
			// 取美元引号的标签。
			tag := dollarTag(src, i)
			// 不是美元引号就当一个符号。
			if tag == "" {
				// 记下这个符号。
				cur = append(cur, token{kind: 'p', text: "$"})
				// 往后挪一格。
				i++
				continue
			}
			// 找配对的结束标签。
			n := strings.Index(src[i+len(tag):], tag)
			// 没收尾就整份拆不开。
			if n < 0 {
				// 交回没收尾的标签。
				return nil, fmt.Errorf("美元引号 %s 没有收尾", tag)
			}
			// 函数体与 DO 块正文只留占位。
			cur = append(cur, token{kind: 's'})
			// 跳过整段正文和两端标签。
			i += len(tag) + n + len(tag)
		// 顶层分号断句。
		case c == ';':
			// 空句不留。
			if len(cur) > 0 {
				// 收下这一句。
				stmts = append(stmts, cur)
			}
			// 开始攒下一句。
			cur = nil
			// 跳过分号。
			i++
		// 连续的词字节组成一个词。
		case isWordByte(c):
			// 找词尾，词中可带 $。
			j := i
			// 一直扫到不是词字节为止。
			for j < len(src) && (isWordByte(src[j]) || src[j] == '$') {
				// 往后挪一格。
				j++
			}
			// 词统一小写，与库里未加引号的名字同口径。
			word := strings.ToLower(src[i:j])
			// E'...' 是带反斜杠转义的字符串。
			if word == "e" && j < len(src) && src[j] == '\'' {
				// 按反斜杠转义找收尾。
				k, err := skipQuoted(src, j, true)
				// 没收尾就整份拆不开。
				if err != nil {
					return nil, err
				}
				// 只留占位。
				cur = append(cur, token{kind: 's'})
				// 跳过整个字符串。
				i = k
				continue
			}
			// 收下这个词。
			cur = append(cur, token{kind: 'w', text: word})
			// 跳到词尾。
			i = j
		// 空白只起分隔作用。
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			// 往后挪一格。
			i++
		// 其余都是单个符号。
		default:
			// 记下这个符号。
			cur = append(cur, token{kind: 'p', text: string(c)})
			// 往后挪一格。
			i++
		}
	}
	// 末尾没写分号的半句也收下。
	if len(cur) > 0 {
		// 收下最后一句。
		stmts = append(stmts, cur)
	}
	return stmts, nil
}

// isWordByte 能组成词的字节：字母、数字、下划线和非 ASCII。
func isWordByte(c byte) bool {
	// 照 PG 未加引号标识符的字符集。
	return c == '_' || c >= 0x80 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// skipBlockComment 从 i 处的 /* 跳到配对的 */ 之后；PG 的块注释可嵌套。
func skipBlockComment(src string, i int) (int, error) {
	// 已进入的层数。
	depth := 0
	// 扫到层数归零为止。
	for i < len(src) {
		// 按两字节标记分情况。
		switch {
		// 又进一层。
		case strings.HasPrefix(src[i:], "/*"):
			// 层数加一并跳过标记。
			depth++
			// 跳过开头标记。
			i += 2
		// 右收尾，出一层。
		case strings.HasPrefix(src[i:], "*/"):
			// 层数减一并跳过标记。
			depth--
			// 跳过收尾标记。
			i += 2
			// 最外层收尾就结束。
			if depth == 0 {
				return i, nil
			}
		// 注释正文。
		default:
			// 往后挪一格。
			i++
		}
	}
	// 交回没收尾的错误。
	return 0, errors.New("块注释没有收尾")
}

// skipQuoted 从 i 处的引号跳到配对引号之后；两个引号连写算转义，backslash 时 \ 也转义。
func skipQuoted(src string, i int, backslash bool) (int, error) {
	// 开头的引号字符。
	q := src[i]
	// 从引号后一格开始找。
	for j := i + 1; j < len(src); j++ {
		// 按当前字节分情况。
		switch {
		// E 字符串里反斜杠吃掉下一个字节。
		case backslash && src[j] == '\\':
			// 连同被转义的字节一起跳过。
			j++
		// 两个引号连写是转义，不是收尾。
		case src[j] == q && j+1 < len(src) && src[j+1] == q:
			// 跳过成对的引号。
			j++
		// 单个引号就是收尾。
		case src[j] == q:
			return j + 1, nil
		}
	}
	// 交回没收尾的错误。
	return 0, fmt.Errorf("引号 %c 没有收尾", q)
}

// dollarTag 取 i 处美元引号的标签（含两端 $），如 $$ 或 $body$；不是美元引号时为空。
func dollarTag(src string, i int) string {
	// 标签名从 $ 后一格开始。
	j := i + 1
	// 标签名不能以数字开头，$1 是参数。
	if j < len(src) && src[j] >= '0' && src[j] <= '9' {
		return ""
	}
	// 标签名由词字节组成。
	for j < len(src) && isWordByte(src[j]) {
		// 往后挪一格。
		j++
	}
	// 后面紧跟 $ 才是美元引号。
	if j < len(src) && src[j] == '$' {
		// 交回含两端 $ 的标签。
		return src[i : j+1]
	}
	return ""
}

// schema 按迁移顺序重放出的表结构，只记核对库内注释要用的部分。
type schema struct {
	tables map[string]*table // 现存的表，键为表名
}

// table 一张现存的表。
type table struct {
	file      string             // 建这张表的迁移；不是迁移建的为空，只核它的新列
	commented bool               // 已有 COMMENT ON TABLE
	columns   map[string]*column // 现存的列，键为列名
}

// column 一个现存的列。
type column struct {
	file      string // 建出这一列的迁移
	commented bool   // 已有 COMMENT ON COLUMN
}

// replay 重放一个文件里的建表、改表、删表和注释语句；拆不开的写法记为违规。
func (s *schema) replay(file, src string) []string {
	// 先拆成语句。
	stmts, err := lexSQL(src)
	// 拆不开就核不了注释，直接报。
	if err != nil {
		// 交回拆分失败的原因。
		return []string{fmt.Sprintf("%s 拆不开语句：%v", file, err)}
	}
	// 收集本文件的违规。
	var problems []string
	// 逐句重放。
	for _, st := range stmts {
		// 按句首关键字分情况。
		switch {
		// CREATE 开头，可能是建表。
		case isWord(st, 0, "create"):
			// 记下新表和它的列。
			problems = append(problems, s.createTable(file, st[1:])...)
		// 改表：加列、删列、改名。
		case isWord(st, 0, "alter") && isWord(st, 1, "table"):
			// 重放改表动作。
			problems = append(problems, s.alterTable(file, st[2:])...)
		// DROP TABLE 删表。
		case isWord(st, 0, "drop") && isWord(st, 1, "table"):
			// 删掉的表不再要求注释。
			s.dropTables(st[2:])
		// 写库内注释。
		case isWord(st, 0, "comment") && isWord(st, 1, "on"):
			// 记下表或列已有注释。
			s.comment(st[2:])
		}
	}
	return problems
}

// createTable 记下新建的表与各列；st 从 CREATE 之后开始，不是建表就不管。
func (s *schema) createTable(file string, st []token) []string {
	// 从修饰词之后开始。
	i := 0
	// 跳过 TEMP、UNLOGGED 这类修饰。
	for i < len(st) && st[i].kind == 'w' && slices.Contains(tableModifiers, st[i].text) {
		// 往后挪一格。
		i++
	}
	// 建索引、函数、触发器等不归这里管。
	if !isWord(st, i, "table") {
		return nil
	}
	// 跳过 IF NOT EXISTS 取表名。
	name, i := lastName(st, skipWords(st, i+1, "if", "not", "exists"))
	// 取不出表名就核不了。
	if name == "" {
		// 交回拆不出表名的违规。
		return []string{fmt.Sprintf("%s 有 CREATE TABLE 拆不出表名", file)}
	}
	// 后面要紧跟列清单。
	inner, ok := group(st, i)
	// AS SELECT、PARTITION OF 等写法列不明，核不了列注释。
	if !ok {
		// 交回写法不支持的违规。
		return []string{fmt.Sprintf("%s 的 CREATE TABLE %s 不是列清单写法，核不了列注释", file, name)}
	}
	// 新表（含删后重建）从零记注释。
	t := &table{file: file, columns: map[string]*column{}}
	// 收集本句的违规。
	var problems []string
	// 列清单按顶层逗号拆开，一项是一列或一条约束。
	for _, el := range splitTop(inner) {
		// 按打头的词分情况。
		switch {
		// 空项或表约束不是列。
		case len(el) == 0 || (el[0].kind == 'w' && slices.Contains(tableConstraints, el[0].text)):
			continue
		// LIKE 复制别的表的列，列名不明。
		case isWord(el, 0, "like"):
			// 记下核不了的写法。
			problems = append(problems, fmt.Sprintf("%s 的 CREATE TABLE %s 用了 LIKE，核不了列注释", file, name))
		// 打头的是列名。
		case el[0].kind == 'w' || el[0].kind == 'q':
			// 新列还没有注释。
			t.columns[el[0].text] = &column{file: file}
		// 其余写法认不出。
		default:
			// 记下认不出的列定义。
			problems = append(problems, fmt.Sprintf("%s 的 CREATE TABLE %s 有认不出的列定义", file, name))
		}
	}
	// 登记这张表，同名旧表的记录作废。
	s.tables[name] = t
	return problems
}

// alterTable 重放 ALTER TABLE 的加列、删列和改名；st 从 TABLE 之后开始。
func (s *schema) alterTable(file string, st []token) []string {
	// 跳过 IF EXISTS 与 ONLY 取表名。
	name, i := lastName(st, skipWords(st, skipWords(st, 0, "if", "exists"), "only"))
	// 取不出表名就核不了。
	if name == "" {
		// 交回拆不出表名的违规。
		return []string{fmt.Sprintf("%s 有 ALTER TABLE 拆不出表名", file)}
	}
	// 表名后的 * 表示含子表，与列无关。
	if i < len(st) && st[i].kind == 'p' && st[i].text == "*" {
		// 跳过星号。
		i++
	}
	// 不是迁移建的表也记下，只核迁移给它加的列。
	t := s.tables[name]
	// 头一回见到就补一条空记录。
	if t == nil {
		// 建空记录，不要求表注释。
		t = &table{columns: map[string]*column{}}
		// 登记这张表。
		s.tables[name] = t
	}
	// 收集本句的违规。
	var problems []string
	// 多个动作用顶层逗号分开。
	for _, act := range splitTop(st[i:]) {
		// 按动作打头的词分情况。
		switch {
		// ADD 后面是约束就跳过，否则是加列。
		case isWord(act, 0, "add"):
			// 加的是约束，不是列。
			if len(act) > 1 && act[1].kind == 'w' && slices.Contains(tableConstraints, act[1].text) {
				continue
			}
			// 跳过 COLUMN 与 IF NOT EXISTS 取列名。
			col, ok := identAt(act, skipWords(act, skipWords(act, 1, "column"), "if", "not", "exists"))
			// 取不出列名就记违规。
			if !ok {
				// 记下认不出的加列。
				problems = append(problems, fmt.Sprintf("%s 的 ALTER TABLE %s ADD 认不出列名", file, name))
				continue
			}
			// 新列还没有注释。
			t.columns[col] = &column{file: file}
		// DROP 后面是约束就跳过，否则是删列。
		case isWord(act, 0, "drop"):
			// 删约束与列无关。
			if isWord(act, 1, "constraint") {
				continue
			}
			// 跳过 COLUMN 与 IF EXISTS 取列名。
			col, ok := identAt(act, skipWords(act, skipWords(act, 1, "column"), "if", "exists"))
			// 取到列名就删掉，删掉的列不再要求注释。
			if ok {
				// 删掉这一列的记录。
				delete(t.columns, col)
			}
		// RENAME 改表名或列名，注释随对象走。
		case isWord(act, 0, "rename"):
			// 重放改名。
			s.rename(name, t, act)
		}
	}
	return problems
}

// rename 重放 RENAME TO 与 RENAME [COLUMN] a TO b；PG 改名后注释照留。
func (s *schema) rename(name string, t *table, act []token) {
	// RENAME TO 是改表名。
	if isWord(act, 1, "to") {
		// 取新表名。
		newName, ok := identAt(act, 2)
		// 取到才搬记录。
		if ok {
			// 旧名作废。
			delete(s.tables, name)
			// 记录搬到新名下。
			s.tables[newName] = t
		}
		return
	}
	// 改约束名与列无关。
	if isWord(act, 1, "constraint") {
		return
	}
	// 跳过 COLUMN 取旧列名。
	i := skipWords(act, 1, "column")
	// 取旧列名。
	oldName, ok := identAt(act, i)
	// 取新列名。
	newName, ok2 := identAt(act, i+2)
	// 写法对得上且旧列在，才搬记录。
	if c := t.columns[oldName]; ok && ok2 && isWord(act, i+1, "to") && c != nil {
		// 旧名作废。
		delete(t.columns, oldName)
		// 记录搬到新名下。
		t.columns[newName] = c
	}
}

// dropTables 删掉 DROP TABLE 列出的表；st 从 TABLE 之后开始。
func (s *schema) dropTables(st []token) {
	// 跳过 IF EXISTS，按顶层逗号逐个表名。
	for _, el := range splitTop(st[skipWords(st, 0, "if", "exists"):]) {
		// 取表名，后面的 CASCADE 不管。
		name, _ := lastName(el, 0)
		// 删掉的表连同列都不再要求注释。
		delete(s.tables, name)
	}
}

// comment 记下表或列的 COMMENT ON；IS NULL 算删注释。st 从 ON 后开始。
func (s *schema) comment(st []token) {
	// 按注释对象分情况。
	switch {
	// COMMENT ON TABLE 表注释。
	case isWord(st, 0, "table"):
		// 取被注释的表名。
		name, i := lastName(st, 1)
		// 只认迁移里见过的表。
		if t := s.tables[name]; t != nil && isWord(st, i, "is") {
			// IS NULL 是删注释，其余算写上。
			t.commented = !isWord(st, i+1, "null")
		}
	// COMMENT ON COLUMN 列注释。
	case isWord(st, 0, "column"):
		// 取出 表.列 全名。
		parts, i := dotted(st, 1)
		// 至少要有表和列两段。
		if len(parts) < 2 || !isWord(st, i, "is") {
			return
		}
		// 倒数第二段是表名。
		t := s.tables[parts[len(parts)-2]]
		// 表不认识就不管。
		if t == nil {
			return
		}
		// 最后一段是列名，只认现存的列。
		if c := t.columns[parts[len(parts)-1]]; c != nil {
			// IS NULL 是删注释，其余算写上。
			c.commented = !isWord(st, i+1, "null")
		}
	}
}

// missingComments 列出现存却没有库内注释的表与列，按名排序。
func (s *schema) missingComments() []string {
	// 收集缺注释的对象。
	var problems []string
	// 表按名排序，报错顺序稳定。
	for _, name := range slices.Sorted(maps.Keys(s.tables)) {
		// 取这张表的记录。
		t := s.tables[name]
		// 迁移建的表要有表注释。
		if t.file != "" && !t.commented {
			// 记下缺表注释。
			problems = append(problems, fmt.Sprintf("%s 建的表 %s 缺 COMMENT ON TABLE", t.file, name))
		}
		// 列同样按名排序。
		for _, col := range slices.Sorted(maps.Keys(t.columns)) {
			// 取这一列的记录。
			c := t.columns[col]
			// 每一列都要有列注释。
			if !c.commented {
				// 记下缺列注释。
				problems = append(problems, fmt.Sprintf("%s 加的列 %s.%s 缺 COMMENT ON COLUMN", c.file, name, col))
			}
		}
	}
	return problems
}

// isWord 第 i 个词是不是给定的关键字。
func isWord(st []token, i int, word string) bool {
	// 越界或不是词都不算。
	return i < len(st) && st[i].kind == 'w' && st[i].text == word
}

// skipWords 从 i 起正好是这串关键字就跳过它们，否则原地不动。
func skipWords(st []token, i int, words ...string) int {
	// 逐个比对关键字。
	for k, w := range words {
		// 有一个对不上就整串不算。
		if !isWord(st, i+k, w) {
			return i
		}
	}
	// 整串对上，跳到其后。
	return i + len(words)
}

// identAt 第 i 个词当标识符取出，词或双引号标识符都行。
func identAt(st []token, i int) (string, bool) {
	// 越界或不是名字就取不出。
	if i >= len(st) || (st[i].kind != 'w' && st[i].kind != 'q') {
		return "", false
	}
	// 交回这个名字。
	return st[i].text, true
}

// dotted 从 i 起取 a.b.c 这样带点的全名，交回各段与其后的位置。
func dotted(st []token, i int) ([]string, int) {
	// 先取第一段。
	first, ok := identAt(st, i)
	// 第一段都没有就是空名。
	if !ok {
		return nil, i
	}
	// 收集各段。
	parts := []string{first}
	// 跳过第一段。
	i++
	// 后面跟着 . 名字 就接着收。
	for i+1 < len(st) && st[i].kind == 'p' && st[i].text == "." {
		// 取点后的那一段。
		next, ok := identAt(st, i+1)
		// 点后不是名字就停。
		if !ok {
			break
		}
		// 收下这一段。
		parts = append(parts, next)
		// 跳过点和名字。
		i += 2
	}
	return parts, i
}

// lastName 取带模式前缀的表名的最后一段；本仓只用 public 模式。
func lastName(st []token, i int) (string, int) {
	// 取出全名各段。
	parts, next := dotted(st, i)
	// 没有名字就交回空。
	if len(parts) == 0 {
		return "", next
	}
	// 只留最后一段。
	return parts[len(parts)-1], next
}

// group 第 i 个是左括号时，交回到配对右括号之间的词。
func group(st []token, i int) ([]token, bool) {
	// 不是左括号就不是列清单。
	if i >= len(st) || st[i].kind != 'p' || st[i].text != "(" {
		return nil, false
	}
	// 当前括号层数。
	depth := 0
	// 从左括号往后找配对。
	for j := i; j < len(st); j++ {
		// 只看符号。
		if st[j].kind != 'p' {
			continue
		}
		// 按括号方向调层数。
		switch st[j].text {
		// 左括号，进一层。
		case "(":
			// 层数加一。
			depth++
		// 出一层，回到外面就是配对的右括号。
		case ")":
			// 层数减一。
			depth--
			// 回到最外层就交回括号里的词。
			if depth == 0 {
				return st[i+1 : j], true
			}
		}
	}
	return nil, false
}

// splitTop 按括号外的逗号把一串词拆成几项。
func splitTop(st []token) [][]token {
	// 收集拆出的各项。
	var parts [][]token
	// 当前括号层数。
	depth := 0
	// 当前项的起点。
	start := 0
	// 逐个看符号。
	for i, tk := range st {
		// 只看符号。
		if tk.kind != 'p' {
			continue
		}
		// 按符号调层数或断项。
		switch {
		// 左括号，进一层。
		case tk.text == "(":
			// 层数加一。
			depth++
		// 右括号，出一层。
		case tk.text == ")":
			// 层数减一。
			depth--
		// 最外层的逗号断开一项。
		case tk.text == "," && depth == 0:
			// 收下这一项。
			parts = append(parts, st[start:i])
			// 下一项从逗号后开始。
			start = i + 1
		}
	}
	// 收下最后一项。
	return append(parts, st[start:])
}
