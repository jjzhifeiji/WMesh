// 每台设备最近登录：只取对上本机的最新一条，没对上设备的不算。
package store_test

import (
	"context"
	"testing"
	"time"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 同一台登录两次取后一次；没对上设备的登录不出现。
func TestLatestClientLogins(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建一名登录人，登录现场要挂在人上。
	p, err := s.CreatePerson(ctx, "op", "操作员", "", false)
	// 建人失败就停测，后面没有前提。
	if err != nil {
		// 建人失败就停测。
		t.Fatal(err)
	}
	// 现发一台设备身份。
	cid := id.New()
	// 先记一次旧版本登录。
	if _, err := s.InsertPersonLogin(ctx, p.ID, store.LoginSnap{Kind: store.LoginKindPad, AppVersion: 1, AppVersionName: "1.0.0", ClientID: &cid}); err != nil {
		// 写登录失败就停测。
		t.Fatal(err)
	}
	// 隔一下，两次登录时间分得开。
	time.Sleep(10 * time.Millisecond)
	// 再记一次新版本登录，应取这一条。
	if _, err := s.InsertPersonLogin(ctx, p.ID, store.LoginSnap{Kind: store.LoginKindPad, AppVersion: 2, AppVersionName: "1.0.1", ClientID: &cid}); err != nil {
		// 写登录失败就停测。
		t.Fatal(err)
	}
	// 再记一次没对上设备的登录，不该出现。
	if _, err := s.InsertPersonLogin(ctx, p.ID, store.LoginSnap{Kind: store.LoginKindPad, AppVersion: 3}); err != nil {
		// 写登录失败就停测。
		t.Fatal(err)
	}
	// 取每台最近登录。
	rows, err := s.LatestClientLogins(ctx)
	// 读失败、条数或取到的不是后一次都不对。
	if err != nil || len(rows) != 1 || rows[0].ClientID != cid || rows[0].AppVersion != 2 || rows[0].AppVersionName != "1.0.1" || rows[0].OccurredAt.IsZero() {
		// 结果不对就停测。
		t.Fatalf("latest %+v %v", rows, err)
	}
}
