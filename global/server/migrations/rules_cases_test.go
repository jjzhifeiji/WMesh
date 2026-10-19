// 迁移规则门禁的反例与正例：用内存目录喂样例，确认每条规则都会拦、合规写法都放行。
package migrations

import (
	"strings"
	"testing"
	"testing/fstest"
)

// goodTable 一张注释齐全的表，给只测别的规则的样例当正文。
const goodTable = "CREATE TABLE t (id INT);\nCOMMENT ON TABLE t IS '表';\nCOMMENT ON COLUMN t.id IS '编号';"

// upFile 生成带 goose Up 注解的迁移文件，写法和仓里一致。
func upFile(body string) *fstest.MapFile {
	// 说明注释之后接 Up 注解，再接正文。
	return &fstest.MapFile{Data: []byte("-- 测试迁移\n-- +goose Up\n" + body + "\n")}
}

// rawFile 原样生成迁移文件，喂注解写错的样例。
func rawFile(text string) *fstest.MapFile {
	// 不补任何注解。
	return &fstest.MapFile{Data: []byte(text + "\n")}
}

// ruleCases 每条规则至少一个坏样例，以及违规说明里应出现的字样。
var ruleCases = []struct {
	name  string       // 子测试名，写明违反哪条规则
	files fstest.MapFS // 喂给门禁的迁移目录
	want  string       // 违规说明里应出现的字样
}{
	{"文件名带大写", fstest.MapFS{"0001_Init.up.sql": upFile(goodTable)}, "0001_Init.up.sql 文件名不合规"},
	{"版本号不足四位", fstest.MapFS{"001_init.up.sql": upFile(goodTable)}, "001_init.up.sql 文件名不合规"},
	{"写了 down 文件", fstest.MapFS{"0001_init.up.sql": upFile(goodTable), "0002_init.down.sql": upFile("SELECT 1;")}, "0002_init.down.sql 文件名不合规"},
	{"目录为空", fstest.MapFS{}, "一个 .sql 都没有"},
	{"版本不从 1 起", fstest.MapFS{"0002_init.up.sql": upFile(goodTable)}, "缺 0001"},
	{"版本跳号", fstest.MapFS{"0001_init.up.sql": upFile(goodTable), "0003_more.up.sql": upFile("SELECT 1;")}, "缺 0002"},
	{"版本重号", fstest.MapFS{"0001_init.up.sql": upFile(goodTable), "0001_more.up.sql": upFile("SELECT 1;")}, "版本号重复"},
	{"缺 Up 注解", fstest.MapFS{"0001_init.up.sql": rawFile(goodTable)}, "缺 -- +goose Up"},
	{"Up 写了两行", fstest.MapFS{"0001_init.up.sql": upFile("-- +goose Up\n" + goodTable)}, "只能一行"},
	{"SQL 在 Up 之前", fstest.MapFS{"0001_init.up.sql": rawFile("SELECT 1;\n-- +goose Up\n" + goodTable)}, "写在 -- +goose Up 之前"},
	{"写了 Down 注解", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose Down\nDROP TABLE t;")}, "禁止 down"},
	{"StatementBegin 没收", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose StatementBegin\nDO $$ BEGIN END $$;")}, "没有配对的 StatementEnd"},
	{"StatementEnd 多余", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose StatementEnd")}, "前面没有 StatementBegin"},
	{"StatementBegin 嵌套", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose StatementBegin\n-- +goose StatementBegin\nDO $$ BEGIN END $$;\n-- +goose StatementEnd")}, "嵌套"},
	{"注解缩进", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n  -- +goose StatementBegin\nDO $$ BEGIN END $$;\n-- +goose StatementEnd")}, "注解不能缩进"},
	{"不认的注解", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose Upp")}, "goose 不认的注解"},
	{"不开事务", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose NO TRANSACTION")}, "本仓迁移必须在事务里执行"},
	{"用环境变量替换", fstest.MapFS{"0001_init.up.sql": upFile("-- +goose ENVSUB ON\n" + goodTable + "\n-- +goose ENVSUB OFF")}, "不许依赖环境变量"},
	{"表缺注释", fstest.MapFS{"0001_init.up.sql": upFile("CREATE TABLE t (id INT);\nCOMMENT ON COLUMN t.id IS '编号';")}, "表 t 缺 COMMENT ON TABLE"},
	{"建表的列缺注释", fstest.MapFS{"0001_init.up.sql": upFile("CREATE TABLE t (id INT, name TEXT, PRIMARY KEY (id));\nCOMMENT ON TABLE t IS '表';\nCOMMENT ON COLUMN t.id IS '编号';")}, "列 t.name 缺 COMMENT ON COLUMN"},
	{"加列缺注释", fstest.MapFS{"0001_init.up.sql": upFile(goodTable), "0002_more.up.sql": upFile("ALTER TABLE t ADD COLUMN IF NOT EXISTS note TEXT, ADD CONSTRAINT t_note CHECK (note <> '');")}, "0002_more.up.sql 加的列 t.note 缺 COMMENT ON COLUMN"},
	{"不写 COLUMN 的加列缺注释", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\nALTER TABLE t ADD note TEXT;")}, "列 t.note 缺 COMMENT ON COLUMN"},
	{"注释置空", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\nCOMMENT ON COLUMN t.id IS NULL;")}, "列 t.id 缺 COMMENT ON COLUMN"},
	{"删表重建要重写注释", fstest.MapFS{"0001_init.up.sql": upFile(goodTable), "0002_more.up.sql": upFile("DROP TABLE t;\nCREATE TABLE t (id INT);")}, "0002_more.up.sql 建的表 t 缺 COMMENT ON TABLE"},
	{"非列清单建表", fstest.MapFS{"0001_init.up.sql": upFile("CREATE TABLE t AS SELECT 1 AS id;")}, "不是列清单写法"},
	{"LIKE 建表", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\nCREATE TABLE u (LIKE t);\nCOMMENT ON TABLE u IS '表';")}, "用了 LIKE"},
	{"引号没收尾", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\nCOMMENT ON TABLE t IS '表;")}, "拆不开语句"},
	{"美元引号没收尾", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\n-- +goose StatementBegin\nDO $$ BEGIN END;\n-- +goose StatementEnd")}, "美元引号 $$ 没有收尾"},
	{"DO 块没包 StatementBegin", fstest.MapFS{"0001_init.up.sql": upFile(goodTable + "\nDO $$ BEGIN\n    UPDATE t SET id = id WHERE false;\nEND $$;")}, "被 goose 从中间切断"},
	{"多行字符串行尾带分号", fstest.MapFS{"0001_init.up.sql": upFile("CREATE TABLE t (id INT);\nCOMMENT ON TABLE t IS '表;\n说明';\nCOMMENT ON COLUMN t.id IS '编号';")}, "被 goose 从中间切断"},
	{"末句缺分号", fstest.MapFS{"0001_init.up.sql": upFile("CREATE TABLE t (id INT);\nCOMMENT ON TABLE t IS '表';\nCOMMENT ON COLUMN t.id IS '编号'")}, "missing semicolon"},
}

// TestRuleViolations 每个坏样例都要被拦下，且说明里点到违反的规则。
func TestRuleViolations(t *testing.T) {
	// 逐个样例跑。
	for _, tc := range ruleCases {
		// 每个样例一个子测试，失败时看得出是哪条规则。
		t.Run(tc.name, func(t *testing.T) {
			// 把样例目录交给门禁。
			problems := checkRules(tc.files)
			// 违规说明里要有预期字样。
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				// 打出实际说明，方便对照。
				t.Fatalf("应报 %q，实际：%v", tc.want, problems)
			}
		})
	}
}

// TestRulesAcceptGood 各种合规写法都放行：约束、引号名、DO 块、删表删列、改名。
func TestRulesAcceptGood(t *testing.T) {
	// 一套写法尽量杂但守规则的迁移。
	files := fstest.MapFS{
		"0001_init.up.sql": upFile(`CREATE TABLE IF NOT EXISTS public.t (
    id UUID PRIMARY KEY, -- 行内说明不算库内注释，但也不碍事
    "Name" TEXT NOT NULL CHECK (char_length("Name") BETWEEN 1 AND 64),
    tmp INT,
    CONSTRAINT t_name_key UNIQUE ("Name")
);
CREATE TABLE gone (id INT);
COMMENT ON TABLE t IS '表；正文里的 CREATE TABLE x (y INT); 不算建表';
COMMENT ON COLUMN public.t.id IS '编号';
COMMENT ON COLUMN t."Name" IS E'名字\'s';
/* 块注释 /* 可嵌套 */ 里的 CREATE TABLE fake (x INT); 不算 */
-- +goose StatementBegin
DO $body$
BEGIN
    IF to_regclass('public.legacy') IS NOT NULL THEN
        COMMENT ON TABLE legacy IS '老库才有的表';
    END IF;
END $body$;
-- +goose StatementEnd`),
		"0002_more.up.sql": upFile(`ALTER TABLE t DROP COLUMN tmp;
DROP TABLE IF EXISTS gone CASCADE;
ALTER TABLE ONLY t ADD COLUMN note TEXT, ALTER COLUMN id SET NOT NULL, DROP CONSTRAINT IF EXISTS t_name_key;
COMMENT ON COLUMN t.note IS '备注';
ALTER TABLE t RENAME COLUMN note TO memo;
ALTER TABLE t RENAME TO t2;`),
	}
	// 合规样例一条违规都不该有。
	if problems := checkRules(files); len(problems) > 0 {
		// 打出被误拦的说明。
		t.Fatalf("合规样例被拦：%v", problems)
	}
}
