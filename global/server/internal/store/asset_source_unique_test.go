// 同一升档源只一条平台级：迁移把存量重复摘掉来源，之后同源再插撞索引交回专用错误。
package store_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/migrate"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
	"wmesh/global/migrations"
)

// migrationsBefore 只取文件名排在 cutoff 之前的迁移，好在新迁移之前摆存量数据。
func migrationsBefore(t *testing.T, cutoff string) fstest.MapFS {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出嵌进程序的全部向前脚本。
	names, err := fs.Glob(migrations.FS, "*.up.sql")
	// 列不出脚本就停，后面没有底数。
	if err != nil {
		// 把列脚本的错误打出来并停住。
		t.Fatal(err)
	}
	// 准备只含旧脚本的假文件系统。
	out := fstest.MapFS{}
	// 逐个脚本看编号，新迁移及之后的不放进来。
	for _, name := range names {
		// 排在截止名之后的跳过，留给后面整包套用。
		if name >= cutoff {
			continue
		}
		// 读出脚本原文，原样放进假文件系统。
		body, err := fs.ReadFile(migrations.FS, name)
		// 读不出就停，旧库搭不全。
		if err != nil {
			// 把读脚本的错误打出来并停住。
			t.Fatal(err)
		}
		// 按原文件名登记，版本号和校验和都不变。
		out[name] = &fstest.MapFile{Data: body}
	}
	return out
}

// 迁移前同一源已有三条平台级：迁移后只留最新一条带源，其余变独立平台级，索引在、同源再插被拒。
func TestAssetSourceUniqueMigration(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 连上维护库，用来建临时云端库。
	admin := testpg.Open(t)
	// 建一块测完就删的空库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 打不开库就停，后面没有地方摆数据。
	if err != nil {
		// 把开库的错误打出来并停住。
		t.Fatal(err)
	}
	// 先迁到新迁移之前，模拟上线前的旧库。
	if err := migrate.Up(db, migrationsBefore(t, "0028_"), "."); err != nil {
		// 旧库迁不上就停，前提不成立。
		t.Fatalf("migrate before: %v", err)
	}
	// 用这块旧库打开云端存储。
	s := store.Open(db)
	// 先写入管理员，当作平台级的创建人。
	wan, err := s.CreateAdmin(ctx, "wan", "hash")
	// 管理员没建成就停，后面没有创建人。
	if err != nil {
		// 管理员没建成就停。
		t.Fatal(err)
	}
	// 登记来源厂，升档行要带上这家的身份。
	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	// 厂没登记上就停，后面没有来源厂。
	if err != nil {
		// 厂没建成就停。
		t.Fatal(err)
	}
	// 准备一段正文，各条共用。
	body := []byte(`{"voltage":28}`)
	// 按给定来源插一条平台级，旧库还没有索引，同源可插多条。
	insert := func(source uuid.UUID, rev int64) store.Asset {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 写入一条升档来的平台工艺。
		a, err := s.InsertAsset(ctx, store.Asset{
			Kind: store.KindProcess, Name: "同源", Status: store.AssetDraft,
			Content: body, Digest: digest.Sum(body), CreatorID: wan.ID,
			SourceID: &source, SourceRevision: &rev, SourceFactoryID: &fac.ID,
		})
		// 插不进去就停，存量数据摆不出来。
		if err != nil {
			// 把插入的错误打出来并停住。
			t.Fatalf("insert: %v", err)
		}
		return a
	}
	// 重复的升档源身份。
	src := id.New()
	// 另一个只升过一次的源，迁移不能动它。
	other := id.New()
	// 同源第一条，后面定成较旧。
	older := insert(src, 1)
	// 同源第二条，后面定成最新，应留下带源。
	newest := insert(src, 2)
	// 同源第三条，后面定成最旧。
	oldest := insert(src, 3)
	// 只升过一次的源，迁移后仍带源。
	single := insert(other, 1)
	// 取一个基准时刻，各条按它前后错开。
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// 按条定下最近修改时间，迁移按它挑留哪条。
	for i, c := range []struct {
		assetID uuid.UUID // 要改时间的平台级身份
		at      time.Time // 定成的最近修改时间
	}{{older.ID, base.Add(time.Hour)}, {newest.ID, base.Add(2 * time.Hour)}, {oldest.ID, base}} {
		// 直接改库里的时间，模拟并发升档后各自被改过。
		if err := db.Exec("UPDATE assets SET updated_at = ? WHERE id = ?", c.at, c.assetID).Error; err != nil {
			// 改不了时间就停，挑留哪条没有依据。
			t.Fatalf("set updated_at %d: %v", i, err)
		}
	}
	// 再整包套用，只会跑新迁移。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 新迁移在有重复的旧库上跑不过就停。
		t.Fatalf("migrate: %v", err)
	}
	// 按源回读，应是最新那条。
	kept, err := s.AssetBySourceID(ctx, src)
	// 读不到、不是最新那条或源修订不对就停。
	if err != nil || kept.ID != newest.ID || kept.SourceRevision == nil || *kept.SourceRevision != 2 {
		// 迁移留下的不是最新那条。
		t.Fatalf("kept %+v %v", kept, err)
	}
	// 准备承接数出来的条数。
	var n int64
	// 数库里带这个源的平台级，必须只剩一条。
	if err := db.Raw("SELECT COUNT(*) FROM assets WHERE source_id = ?", src).Scan(&n).Error; err != nil || n != 1 {
		// 同源仍有多条就停。
		t.Fatalf("rows for source %d %v", n, err)
	}
	// 被摘掉来源的两条要还在，正文不丢，来源三列一起清。
	for _, gone := range []store.Asset{older, oldest} {
		// 按身份读回整行。
		got, err := s.AssetByID(ctx, gone.ID)
		// 行没了、来源没清或正文变了都算失败。
		if err != nil || got.SourceID != nil || got.SourceRevision != nil || got.SourceFactoryID != nil || !bytes.Equal(got.Content, body) {
			// 重复行没变成独立平台级就停。
			t.Fatalf("detached %s: %+v %v", gone.ID, got, err)
		}
	}
	// 只升过一次的源不受影响。
	if got, err := s.AssetByID(ctx, single.ID); err != nil || got.SourceID == nil || *got.SourceID != other {
		// 没有重复的行被误摘来源就停。
		t.Fatalf("single %+v %v", got, err)
	}
	// 数部分唯一索引，必须已建好。
	if err := db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'assets' AND indexname = 'assets_source_id_uq'").Scan(&n).Error; err != nil || n != 1 {
		// 索引没建就停。
		t.Fatalf("index %d %v", n, err)
	}
	// 迁移后同源再插一条，撞索引要交回同源已占用。
	rev := int64(4)
	// 同源再插，应被索引挡下。
	_, err = s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "同源再插", Status: store.AssetDraft,
		Content: body, Digest: digest.Sum(body), CreatorID: wan.ID,
		SourceID: &src, SourceRevision: &rev, SourceFactoryID: &fac.ID,
	})
	// 没被挡或错误不是同源已占用就停，升档靠它回读已有行。
	if !errors.Is(err, domain.ErrAssetSourceTaken) {
		// 同源再插没撞成专用错误。
		t.Fatalf("dup source insert: %v", err)
	}
}
