// 验收通道升档核对来源：快照须出自所问的厂和资产且是信封，别厂不能覆盖已升档件。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 来源厂、资产身份、明文正文、别厂覆盖四种都拒且留审计，正常信封照常升档。
func TestPromoteFactorySnapshotSourceCheck(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面升不了档。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面升不了档。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记被问的厂 A，正常升档都从这家来。
	facA, err := h.WAN.CreateFactory(ctx, tok, "来源厂A", "sa-sa", "超管SA")
	// 登记厂 A 失败就停，没有源厂可升档。
	if err != nil {
		// 不符即停：登记厂 A 失败。
		t.Fatal(err)
	}
	// 登记冒充的厂 B，用来顶替来源或覆盖。
	facB, err := h.WAN.CreateFactory(ctx, tok, "来源厂B", "sa-sb", "超管SB")
	// 登记厂 B 失败就停，没有冒充方可验。
	if err != nil {
		// 不符即停：登记厂 B 失败。
		t.Fatal(err)
	}
	// 记下两家厂的身份，后面问询和核对都用。
	fidA, fidB := facA.Factory.ID, facB.Factory.ID
	// 签发厂 A 租约，正常快照要用它封过站。
	leaseA, err := h.WAN.IssueContentLease(ctx, fidA)
	// 签不出厂 A 租约就停，封不了信封。
	if err != nil {
		// 不符即停：签发厂 A 租约失败。
		t.Fatal(err)
	}
	// 签发厂 B 租约，冒充方用自己的钥封信封。
	leaseB, err := h.WAN.IssueContentLease(ctx, fidB)
	// 签不出厂 B 租约就停，封不了信封。
	if err != nil {
		// 不符即停：签发厂 B 租约失败。
		t.Fatal(err)
	}
	// sealed 按某厂租约把正文封成过站快照，身份自报给定的厂和资产。
	sealed := func(t *testing.T, key []byte, fid, src uuid.UUID, plain []byte) global.AssetSnapshot {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 像厂端导出那样按厂、资产和修订封过站。
		env, err := contentcrypt.Seal(key, plain, contentcrypt.TransitAAD(fid, src, 1))
		// 封不上就停，没有快照可升。
		if err != nil {
			// 不符即停：封过站信封失败。
			t.Fatal(err)
		}
		// 组好快照，摘要按明文算，与厂端一致。
		return global.AssetSnapshot{
			SourceID: src, SourceRevision: 1, SourceFactoryID: fid,
			Kind: global.KindProcess, Name: "来源核对焊", Content: env, Digest: digest.Sum(plain),
			Copyable: true, Status: global.AssetAvailable,
		}
	}
	// denied 看审计里有没有按该厂该资产记下的升档拒绝。
	denied := func(t *testing.T, fid, src uuid.UUID) bool {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 读审计，读不到就无法核对有没有记。
		rows, err := h.WAN.ListAudit(ctx)
		// 读审计失败就停，读不到就无法核对有没有记。
		if err != nil {
			// 不符即停：读审计失败。
			t.Fatal(err)
		}
		// 逐条找升档拒绝，厂和对象都要对上。
		for _, r := range rows {
			// 动作、结果、厂和对象都对上才算记了这次拒绝。
			if r.Action == "promote_asset" && r.Result == audit.Deny && r.FactoryID != nil && *r.FactoryID == fid && r.Target == src.String() {
				return true
			}
		}
		return false
	}
	// platformRows 数带该源身份的平台级，被拒的升档不能落库。
	platformRows := func(t *testing.T, src uuid.UUID) int64 {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 准备承接数出来的条数。
		var n int64
		// 数库里带这个源的平台级，读不出就停。
		if err := h.DB.Raw("SELECT COUNT(*) FROM assets WHERE source_id = ?", src).Scan(&n).Error; err != nil {
			// 不符即停：数平台级失败。
			t.Fatal(err)
		}
		return n
	}

	// 快照自报厂 B，问的却是厂 A，应按篡改拒绝。
	t.Run("wrong factory", func(t *testing.T) {
		// 新造源身份，免得和别的子测串。
		src := id.New()
		// 厂 B 用自己的钥封、自报厂 B。
		snap := sealed(t, leaseB.Key, fidB, src, []byte("wrong-factory-plain"))
		// 以问厂 A 的口径升档，应被拒。
		_, err := h.WAN.PromoteFactorySnapshot(ctx, tok, fidA, src, snap)
		// 不是按损坏拒绝就停，说明冒充来源放进来了。
		if !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：来源厂不符没拦住。
			t.Fatalf("want integrity, got %v", err)
		}
		// 没按所问的厂和资产记拒绝，或竟落了库，就停。
		if !denied(t, fidA, src) || platformRows(t, src) != 0 {
			// 不符即停：缺拒绝审计或落了库。
			t.Fatalf("deny audit or row mismatch")
		}
	})

	// 问的是另一个资产，快照却自报别的身份，应拒。
	t.Run("wrong asset", func(t *testing.T) {
		// 快照里自报的源身份。
		src := id.New()
		// 管理员实际问的资产身份。
		asked := id.New()
		// 厂 A 正常封好，但身份不是所问的那个。
		snap := sealed(t, leaseA.Key, fidA, src, []byte("wrong-asset-plain"))
		// 以问 asked 的口径升档，应被拒。
		_, err := h.WAN.PromoteFactorySnapshot(ctx, tok, fidA, asked, snap)
		// 不是按损坏拒绝就停，说明串了资产。
		if !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：资产身份不符没拦住。
			t.Fatalf("want integrity, got %v", err)
		}
		// 没按所问资产记拒绝，或两个身份竟落了库，就停。
		if !denied(t, fidA, asked) || platformRows(t, src) != 0 || platformRows(t, asked) != 0 {
			// 不符即停：缺拒绝审计或落了库。
			t.Fatalf("deny audit or row mismatch")
		}
	})

	// 通道快照正文是明文，没有过站信封，应拒。
	t.Run("plaintext", func(t *testing.T) {
		// 新造源身份，免得和别的子测串。
		src := id.New()
		// 准备明文正文，摘要照样对得上。
		plain := []byte("channel-plaintext")
		// 来源和身份都对，只是正文没封。
		snap := global.AssetSnapshot{
			SourceID: src, SourceRevision: 1, SourceFactoryID: fidA,
			Kind: global.KindProcess, Name: "明文焊", Content: plain, Digest: digest.Sum(plain),
			Copyable: true, Status: global.AssetAvailable,
		}
		// 经通道口径升档，明文应被拒。
		_, err := h.WAN.PromoteFactorySnapshot(ctx, tok, fidA, src, snap)
		// 不是按损坏拒绝就停，说明明文绕过了租约。
		if !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：明文快照没拦住。
			t.Fatalf("want integrity, got %v", err)
		}
		// 没记拒绝或竟落了库，就停。
		if !denied(t, fidA, src) || platformRows(t, src) != 0 {
			// 不符即停：缺拒绝审计或落了库。
			t.Fatalf("deny audit or row mismatch")
		}
	})

	// 厂 A 正常升档成功，厂 B 再拿同一源身份来覆盖应拒。
	t.Run("ok then other factory overwrite", func(t *testing.T) {
		// 新造源身份，两家都拿它来升。
		src := id.New()
		// 厂 A 正常封好的快照。
		snapA := sealed(t, leaseA.Key, fidA, src, []byte("source-a-plain"))
		// 正常信封快照应照常升档。
		promoted, err := h.WAN.PromoteFactorySnapshot(ctx, tok, fidA, src, snapA)
		// 升档失败或来源厂没记成 A，就停。
		if err != nil || promoted.SourceFactoryID == nil || *promoted.SourceFactoryID != fidA || promoted.Revision != 1 {
			// 不符即停：正常升档失败或来源不对。
			t.Fatalf("%+v %v", promoted, err)
		}
		// 厂 B 用自己的钥封同一源身份、换了正文。
		snapB := sealed(t, leaseB.Key, fidB, src, []byte("source-b-plain"))
		// 以问厂 B 的口径升档，来源核对能过，但已升档件属厂 A。
		_, err = h.WAN.PromoteFactorySnapshot(ctx, tok, fidB, src, snapB)
		// 不是按源已被占拒绝就停，说明别厂覆盖了。
		if !errors.Is(err, domain.ErrAssetSourceTaken) {
			// 不符即停：别厂覆盖没拦住。
			t.Fatalf("want source taken, got %v", err)
		}
		// 没按厂 B 记拒绝就停，审计对不上冒充方。
		if !denied(t, fidB, src) {
			// 不符即停：缺拒绝审计。
			t.Fatalf("missing deny audit")
		}
		// 回读平台级，确认没被改写。
		row, err := h.WAN.Store().AssetByID(ctx, promoted.ID)
		// 读不到、来源厂或修订变了，就停。
		if err != nil || row.SourceFactoryID == nil || *row.SourceFactoryID != fidA || row.Revision != promoted.Revision {
			// 不符即停：已升档件被改写。
			t.Fatalf("%+v %v", row, err)
		}
	})
}
