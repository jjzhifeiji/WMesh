package store_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/migrations"
)

// 只取文件名排在 cutoff 之前的迁移，用来摆出旧库。
func templateMigrationsBefore(t *testing.T, cutoff string) fstest.MapFS {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出全部迁移文件。
	names, err := fs.Glob(migrations.FS, "*.up.sql")
	// 列不出就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 准备装旧迁移的内存文件系统。
	out := fstest.MapFS{}
	// 逐个文件挑出 cutoff 之前的。
	for _, name := range names {
		// cutoff 及之后的不要。
		if name >= cutoff {
			continue
		}
		// 读出这份迁移的正文。
		body, err := fs.ReadFile(migrations.FS, name)
		// 读不出就停。
		if err != nil {
			// 把错误打出来。
			t.Fatal(err)
		}
		// 放进内存文件系统。
		out[name] = &fstest.MapFile{Data: body}
	}
	return out
}

// 旧库里留着两个不同身份、修订相同的工艺模版副本（对接过重置前后的两个云端）时，
// 迁移只把最近收到的那个换成预置焊接参数的身份，另一个标成撤下，不能撞主键。
func TestProcessTemplateMigrationKeepsLatestIdentity(t *testing.T) {
	// 连上维护库。
	admin := testpg.Open(t)
	// 建一块空的厂库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 打开这块库，日志关掉。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 打不开就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 先迁到工艺模版多份之前。
	if err := migrate.Up(db, templateMigrationsBefore(t, "0056_"), "."); err != nil {
		// 旧库迁不上就停。
		t.Fatalf("migrate before: %v", err)
	}
	// 两个旧身份：先收到的一份和后收到的一份，修订都是 1；后收到的还有修订 2。
	const older, newer = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	// 逐条摆进旧副本，摘要随便填满 32 字节。
	for _, row := range []struct {
		id  string // 旧身份
		rev int    // 修订
		at  string // 收到时间
	}{{older, 1, "2026-10-01T00:00:00Z"}, {newer, 1, "2026-10-20T00:00:00Z"}, {newer, 2, "2026-10-21T00:00:00Z"}} {
		// 写入一条工艺模版副本。
		if err := db.Exec(`INSERT INTO content_template_replicas (id, revision, kind, name, schema, digest, received_at) VALUES (?, ?, 'process', '', '{"root":"object"}', decode(repeat('00', 32), 'hex'), ?)`, row.id, row.rev, row.at).Error; err != nil {
			// 摆不进去就停。
			t.Fatalf("seed replica: %v", err)
		}
	}
	// 再整包套用，只会跑新迁移；撞主键会在这里失败。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 新迁移跑不过就停。
		t.Fatalf("migrate: %v", err)
	}
	// 读回的一行。
	type got struct {
		ID       string // 迁移后的身份
		Revision int    // 修订
		Name     string // 名称
		Retired  bool   // 是否已撤下
	}
	// 准备接住全部工艺模版副本。
	var rows []got
	// 按身份和修订读回。
	if err := db.Raw(`SELECT id::text AS id, revision, name, retired_at IS NOT NULL AS retired FROM content_template_replicas WHERE kind = 'process' ORDER BY id, revision`).Scan(&rows).Error; err != nil {
		// 读不回就停。
		t.Fatal(err)
	}
	// 期望：后收到的两条换成预置焊接参数身份、仍在用；先收到的那条身份不变、已撤下。
	want := []got{
		{contenttpl.SeedTplWeld, 1, "焊接参数", false},
		{contenttpl.SeedTplWeld, 2, "焊接参数", false},
		{older, 1, "", true},
	}
	// 条数先对上。
	if len(rows) != len(want) {
		// 把读到的打出来。
		t.Fatalf("rows %+v", rows)
	}
	// 逐条核对。
	for i, w := range want {
		// 有一条不同就停。
		if rows[i] != w {
			// 把这一条的实得和期望打出来。
			t.Fatalf("row %d got %+v want %+v", i, rows[i], w)
		}
	}
}
