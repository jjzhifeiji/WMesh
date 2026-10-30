// 包角工艺标记：写正文时随正文算，迁移留空的行领约后按正文补上。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 新建、改正文、只改名、副本送达都按正文定包角；清空后续租补回。
func TestAssetCornerFlag(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂打开库。
	s := store.Open(facDB, facID)
	// 写本厂短码，没有短码就发不了号。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 写短码失败就停测。
		t.Fatal(err)
	}
	// 夹具自签租约，才能把正文封进厂库。
	if err := s.GrantLocalLease(ctx); err != nil {
		// 签租约失败就停测。
		t.Fatal(err)
	}
	// 建一名创建人。
	p, err := s.CreatePerson(ctx, "pe", "工艺员", false)
	// 建账号失败就停测。
	if err != nil {
		// 建账号失败就停测。
		t.Fatal(err)
	}
	// 普通焊接参数正文。
	weldBody := []byte(`{"current":180,"voltage":20}`)
	// 示教器写的包角正文。
	cornerBody := []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":5,"flatOffset":1,"upwardOffset":2}]}`)
	// 按种类和正文插一条。
	insert := func(kind, name string, body []byte) store.Asset {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 写入一条厂级。
		a, err := s.InsertGovernedAsset(ctx, store.Asset{
			Kind: kind, Level: store.AssetLevelFactory, Name: name, Status: store.AssetAvailable,
			Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: p.ID,
		})
		// 插不进去就停。
		if err != nil {
			// 把插入的错误打出来并停住。
			t.Fatalf("insert %s: %v", name, err)
		}
		return a
	}
	// 普通工艺。
	weld := insert(store.KindProcess, "焊接", weldBody)
	// 包角工艺。
	corner := insert(store.KindProcess, "包角", cornerBody)
	// 工程正文就算带了包角 kind 也不算包角。
	proj := insert(store.KindProject, "工程", cornerBody)
	// 新建时就要按正文定好。
	if weld.Corner || !corner.Corner || proj.Corner {
		// 新建没按正文定包角就停。
		t.Fatalf("insert weld=%v corner=%v project=%v", weld.Corner, corner.Corner, proj.Corner)
	}
	// 列表不带正文也要带上标记。
	rows, err := s.ListGovernedAssets(ctx)
	// 列不出就停。
	if err != nil {
		// 把列表的错误打出来并停住。
		t.Fatal(err)
	}
	// 按身份记下列表里的标记。
	listed := map[uuid.UUID]bool{}
	// 逐行记下。
	for _, a := range rows {
		// 记下这一行的包角标记。
		listed[a.ID] = a.Corner
	}
	// 列表标记和新建时一致。
	if listed[weld.ID] || !listed[corner.ID] || listed[proj.ID] {
		// 列表没带上标记就停。
		t.Fatalf("list %v", listed)
	}
	// 普通工艺改成包角正文，标记跟着变。
	weld, err = s.UpdateGovernedAsset(ctx, weld.ID, weld.Revision, store.AssetWrite{
		Name: weld.Name, Content: cornerBody, Digest: digest.Sum(cornerBody), Copyable: true, Status: weld.Status,
	})
	// 改不了或标记没变就停。
	if err != nil || !weld.Corner {
		// 改正文没重算包角就停。
		t.Fatalf("update to corner %+v %v", weld, err)
	}
	// 只改名不碰正文，标记保持。
	weld, err = s.UpdateGovernedAsset(ctx, weld.ID, weld.Revision, store.AssetWrite{
		Name: "改名", Digest: weld.Digest, Copyable: true, Status: weld.Status, KeepContent: true,
	})
	// 改不了或标记丢了就停。
	if err != nil || !weld.Corner {
		// 只改名把包角弄丢了就停。
		t.Fatalf("rename keeps corner %+v %v", weld, err)
	}
	// 再改回焊接参数，标记清掉。
	weld, err = s.UpdateGovernedAsset(ctx, weld.ID, weld.Revision, store.AssetWrite{
		Name: weld.Name, Content: weldBody, Digest: digest.Sum(weldBody), Copyable: true, Status: weld.Status,
	})
	// 改不了或标记没清就停。
	if err != nil || weld.Corner {
		// 改回焊接参数没清包角就停。
		t.Fatalf("update to weld %+v %v", weld, err)
	}
	// 平台副本送达时也按正文定。
	repID := id.New()
	// 写入一条包角副本。
	rep, err := s.InsertReplica(ctx, store.AssetReplica{
		ID: repID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台包角", Status: store.AssetAvailable, Copyable: true, Content: cornerBody, Digest: digest.Sum(cornerBody),
	})
	// 写不进或没认出包角就停。
	if err != nil || !rep.Corner {
		// 副本没按正文定包角就停。
		t.Fatalf("replica %+v %v", rep, err)
	}
	// 模拟迁移后的老库：厂库正文是信封，SQL 判不了，工艺的标记都空着。
	if err := facDB.Exec("UPDATE assets SET corner = NULL WHERE id IN ?", []uuid.UUID{weld.ID, corner.ID, proj.ID}).Error; err != nil {
		// 清不掉就停。
		t.Fatal(err)
	}
	// 副本表同样清空。
	if err := facDB.Exec("UPDATE asset_replicas SET corner = NULL WHERE id = ?", repID).Error; err != nil {
		// 清不掉就停。
		t.Fatal(err)
	}
	// 续租时按正文补回。
	if err := s.GrantLocalLease(ctx); err != nil {
		// 续租失败就停。
		t.Fatal(err)
	}
	// 逐条核对补回的标记。
	for _, c := range []struct {
		id   uuid.UUID // 资产身份
		want bool      // 期望的包角标记
	}{{weld.ID, false}, {corner.ID, true}, {proj.ID, false}} {
		// 只读元数据。
		got, err := s.GovernedAssetMetaByID(ctx, c.id)
		// 读不到或标记不对就停。
		if err != nil || got.Corner != c.want {
			// 补回的标记不对就停。
			t.Fatalf("backfill %s %+v %v", c.id, got, err)
		}
	}
	// 库里不能再留空的。
	var n int64
	// 数两张表里仍空着的行。
	if err := facDB.Raw("SELECT (SELECT COUNT(*) FROM assets WHERE corner IS NULL) + (SELECT COUNT(*) FROM asset_replicas WHERE corner IS NULL)").Scan(&n).Error; err != nil || n != 0 {
		// 还有没补上的就停。
		t.Fatalf("still null %d %v", n, err)
	}
	// 副本也补回包角。
	if got, err := s.LatestReplicaMeta(ctx, repID); err != nil || !got.Corner {
		// 副本没补上就停。
		t.Fatalf("replica backfill %+v %v", got, err)
	}
}
