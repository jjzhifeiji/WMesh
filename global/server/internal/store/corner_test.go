// 包角工艺标记：迁移按正文补上存量，之后写正文随正文算。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/migrate"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
	"wmesh/global/migrations"
)

// 迁移前已有焊接工艺、包角工艺和工程：迁移后只有包角工艺标成包角；改正文时标记跟着变。
func TestAssetCornerMigration(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 连上维护库，用来建临时云端库。
	admin := testpg.Open(t)
	// 建一块测完就删的空库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 打不开库就停。
	if err != nil {
		// 把开库的错误打出来并停住。
		t.Fatal(err)
	}
	// 先迁到新迁移之前，模拟上线前的旧库。
	if err := migrate.Up(db, migrationsBefore(t, "0029_"), "."); err != nil {
		// 旧库迁不上就停。
		t.Fatalf("migrate before: %v", err)
	}
	// 现行代码写资产要带包角列，旧库还没有：先借上这一列，摆完数据再拿掉。
	if err := db.Exec("ALTER TABLE assets ADD COLUMN corner BOOLEAN NOT NULL DEFAULT false").Error; err != nil {
		// 把借列的错误打出来并停住。
		t.Fatalf("borrow corner: %v", err)
	}
	// 用这块旧库打开云端存储。
	s := store.Open(db)
	// 写入管理员，当作创建人。
	wan, err := s.CreateAdmin(ctx, "wan", "hash")
	// 管理员没建成就停。
	if err != nil {
		// 管理员没建成就停。
		t.Fatal(err)
	}
	// 普通焊接参数正文。
	weldBody := []byte(`{"current":180,"oscillation":{"type":"无摆动"}}`)
	// 示教器写的包角正文，键之间带空白。
	cornerBody := []byte(`{"name":"包角", "kind" : "cornerGeometry","passes":[{"length":5}]}`)
	// 按种类和正文插一条。
	insert := func(kind, name string, body []byte) store.Asset {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 写入一条平台级。
		a, err := s.InsertAsset(ctx, store.Asset{
			Kind: kind, Name: name, Status: store.AssetAvailable, Content: body, Digest: digest.Sum(body), CreatorID: wan.ID,
		})
		// 插不进去就停。
		if err != nil {
			// 把插入的错误打出来并停住。
			t.Fatalf("insert %s: %v", name, err)
		}
		return a
	}
	// 焊接工艺。
	weld := insert(store.KindProcess, "焊接", weldBody)
	// 包角工艺。
	corner := insert(store.KindProcess, "包角", cornerBody)
	// 工程正文里带包角组也不算包角。
	proj := insert(store.KindProject, "工程", []byte(`[{"cornerGroupParams":{"processId":"x"},"kind":"cornerGeometry"}]`))
	// 拿掉借来的列，还原成旧库再迁。
	if err := db.Exec("ALTER TABLE assets DROP COLUMN corner").Error; err != nil {
		// 把拿列的错误打出来并停住。
		t.Fatalf("drop borrowed corner: %v", err)
	}
	// 整包套用，只会跑新迁移。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 新迁移跑不过就停。
		t.Fatalf("migrate: %v", err)
	}
	// 逐条核对迁移补上的标记。
	for _, c := range []struct {
		id   uuid.UUID // 资产身份
		want bool      // 期望的包角标记
	}{{weld.ID, false}, {corner.ID, true}, {proj.ID, false}} {
		// 按身份读回。
		got, err := s.AssetByID(ctx, c.id)
		// 读不到或标记不对就停。
		if err != nil || got.Corner != c.want {
			// 迁移补的标记不对就停。
			t.Fatalf("migrated %s %+v %v", c.id, got.Corner, err)
		}
	}
	// 焊接工艺改成包角正文，标记跟着变。
	weld, err = s.UpdateAsset(ctx, weld.ID, weld.Revision, store.AssetWrite{
		Name: weld.Name, Content: cornerBody, Digest: digest.Sum(cornerBody), Status: weld.Status,
	})
	// 改不了或标记没变就停。
	if err != nil || !weld.Corner {
		// 改正文没重算包角就停。
		t.Fatalf("update to corner %+v %v", weld.Corner, err)
	}
	// 包角工艺改回焊接参数，标记清掉。
	corner, err = s.UpdateAsset(ctx, corner.ID, corner.Revision, store.AssetWrite{
		Name: corner.Name, Content: weldBody, Digest: digest.Sum(weldBody), Status: corner.Status,
	})
	// 改不了或标记没清就停。
	if err != nil || corner.Corner {
		// 改回焊接参数没清包角就停。
		t.Fatalf("update to weld %+v %v", corner.Corner, err)
	}
	// 列表不带正文也要带上标记。
	rows, err := s.ListAssets(ctx)
	// 列不出就停。
	if err != nil {
		// 把列表的错误打出来并停住。
		t.Fatal(err)
	}
	// 逐行核对。
	for _, a := range rows {
		// 只有改成包角正文的那条是包角。
		if a.Corner != (a.ID == weld.ID) {
			// 列表标记不对就停。
			t.Fatalf("list %s corner=%v", a.Name, a.Corner)
		}
	}
}
