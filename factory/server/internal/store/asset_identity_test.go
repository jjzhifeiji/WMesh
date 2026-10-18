// 本厂原件写入口：带身份或编号时核身份与号段。
package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 副本身份、外来原号、未登记 C 段都要拒，删过的身份只沿用原号。
func TestGovernedAssetIdentityGuard(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 夹具自签租约，才能把正文封进厂库。
	if err := s.GrantLocalLease(ctx); err != nil {
		// 签租约失败就停测。
		t.Fatal(err)
	}
	// 写本厂短码失败就停测，没有短码就发不了号。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 写短码失败就停测。
		t.Fatal(err)
	}
	// 建一名创建人，原件都记在他名下。
	p, err := s.CreatePerson(ctx, "op", "操作员", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号失败就停测。
		t.Fatal(err)
	}
	// 准备一段正文，所有写入共用。
	body := []byte(`{"current":200}`)
	// 按给定身份和编号拼一条个人工艺。
	asset := func(assetID uuid.UUID, code string) store.Asset {
		// 只有身份和编号随用例变。
		return store.Asset{
			ID: assetID, Code: code, Kind: store.KindProcess, Level: store.AssetLevelPersonal, Name: "汇聚",
			Status: store.AssetAvailable, Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: p.ID,
		}
	}

	// 先收一条带 W 号的平台副本。
	repID := id.New()
	// 副本身份和编号都来自云端。
	rep := store.AssetReplica{
		ID: repID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform, Name: "平台焊",
		Code: "GY-W-000001", Status: store.AssetAvailable, Copyable: true, Content: body, Digest: digest.Sum(body),
	}
	// 写副本失败就停测，后面没有目标可试。
	if _, err := s.InsertReplica(ctx, rep); err != nil {
		// 写副本失败就停测。
		t.Fatal(err)
	}
	// 用副本身份建原件，应当按编号冲突拒绝。
	if _, err := s.InsertGovernedAsset(ctx, asset(repID, "")); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 放行就说明能冒充平台工艺。
		t.Fatalf("replica id: %v", err)
	}

	// 带身份不带号新建一条，号由本厂现发。
	delID := id.New()
	// 新身份不带号应按本厂短码发 1 号。
	first, err := s.InsertGovernedAsset(ctx, asset(delID, ""))
	// 失败或号不对就停测。
	if err != nil || first.Code != "GY-F01-000001" {
		// 把写回的条目打出来并停测。
		t.Fatalf("fresh id: %+v %v", first, err)
	}
	// 物理删掉这一条，登记的号留在 asset_codes。
	if err := s.DeleteGovernedAsset(ctx, delID); err != nil {
		// 删除失败就停测。
		t.Fatal(err)
	}
	// 删过的身份换号重建，应当拒绝。
	if _, err := s.InsertGovernedAsset(ctx, asset(delID, "GY-F01-000009")); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 放行就说明同身份能改号。
		t.Fatalf("deleted id other code: %v", err)
	}
	// 删过的身份空号重建，应沿用原号。
	rebuilt, err := s.InsertGovernedAsset(ctx, asset(delID, ""))
	// 失败或没沿用原号就停测。
	if err != nil || rebuilt.Code != first.Code {
		// 把重建的条目打出来并停测。
		t.Fatalf("deleted id empty code: %+v %v", rebuilt, err)
	}
	// 再建一条不带身份的，号应紧接着 1 号。
	next, err := s.InsertGovernedAsset(ctx, asset(uuid.Nil, ""))
	// 失败或号不对就说明沿用原号时动了计数。
	if err != nil || next.Code != "GY-F01-000002" {
		// 把写回的条目打出来并停测。
		t.Fatalf("seq after rebuild: %+v %v", next, err)
	}

	// 造两条历史污染：已删身份登记着 W 号和他厂号。
	wanID, otherID := id.New(), id.New()
	// 直写登记表失败就停测，后面没有污染可试。
	if err := facDB.Exec("INSERT INTO asset_codes (id, code) VALUES (?, ?), (?, ?)", wanID, "GY-W-000009", otherID, "GY-F02-000001").Error; err != nil {
		// 写污染行失败就停测。
		t.Fatal(err)
	}
	// 两条污染身份带号、不带号都试一遍。
	for _, c := range []struct {
		assetID uuid.UUID // 污染的身份
		code    string    // 调用方带来的号
	}{{wanID, ""}, {wanID, "GY-W-000009"}, {otherID, ""}, {otherID, "GY-F02-000001"}} {
		// 外来原号不得沿用，应当拒绝。
		if _, err := s.InsertGovernedAsset(ctx, asset(c.assetID, c.code)); !errors.Is(err, domain.ErrAssetCodeConflict) {
			// 带上身份和号，便于看出哪一条漏了。
			t.Fatalf("polluted %s %q: %v", c.assetID, c.code, err)
		}
	}

	// 新身份带本厂 F 段号，应当拒绝。
	if _, err := s.InsertGovernedAsset(ctx, asset(id.New(), "GY-F01-000050")); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 放行就说明能占本厂未来号。
		t.Fatalf("own factory code: %v", err)
	}
	// 新身份带未登记的 C 号，应当拒绝。
	if _, err := s.InsertGovernedAsset(ctx, asset(id.New(), "GY-C0008-000001")); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 放行就说明未登记的短码也收。
		t.Fatalf("unknown client code: %v", err)
	}
	// 绑一台焊机，准备登记 C0008。
	cid := id.New()
	// 接下绑定失败就停测。
	if _, err := s.AcceptBinding(ctx, cid, "焊机", nil, 1); err != nil {
		// 绑定失败就停测。
		t.Fatal(err)
	}
	// 登记短码失败就停测。
	if err := s.PutClientShortCode(ctx, cid, "C0008"); err != nil {
		// 登记短码失败就停测。
		t.Fatal(err)
	}
	// 登记后同一个 C 号应能建成。
	pad, err := s.InsertGovernedAsset(ctx, asset(id.New(), "GY-C0008-000001"))
	// 失败或号被改就停测。
	if err != nil || pad.Code != "GY-C0008-000001" {
		// 把写回的条目打出来并停测。
		t.Fatalf("known client code: %+v %v", pad, err)
	}
	// 作废这台焊机，它的号仍要认。
	if err := s.VoidBinding(ctx, cid); err != nil {
		// 作废失败就停测。
		t.Fatal(err)
	}
	// 作废后只带号不带身份也应能建成。
	voided, err := s.InsertGovernedAsset(ctx, asset(uuid.Nil, "GY-C0008-000002"))
	// 失败或号被改就停测。
	if err != nil || voided.Code != "GY-C0008-000002" {
		// 把写回的条目打出来并停测。
		t.Fatalf("voided client code: %+v %v", voided, err)
	}
}
