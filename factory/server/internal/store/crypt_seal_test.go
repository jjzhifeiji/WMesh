// 领约补封遗留明文：库里只挑明文分批封，本进程整扫过后续租不再翻表。
package store_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 遗留明文副本的规模，乘起来超过两批，翻页要跨过同一身份的修订。
const (
	legacyReplicaIDs  = 150 // 遗留副本的身份数
	legacyReplicaRevs = 3   // 每个身份的修订数
)

// 混着明文和 WM2 首次领约只封明文且跨批封完；同进程再续租不扫，夹具改正文后再扫。
func TestSealPlainRowsBatchedOnce(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 第一个进程只用来备数据，它领约时库里还没有明文。
	s1 := store.Open(facDB, facID)
	// 写本厂短码失败就停测，避免后面没有前提。
	if err := s1.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 写本厂短码不符合预期就停测。
		t.Fatal(err)
	}
	// 准备租约钥，两个进程用同一把解开主钥。
	l, err := contentcrypt.RandomKey()
	// 生成随机钥失败就停测，避免后面没有前提。
	if err != nil {
		// 生成随机钥不符合预期就停测。
		t.Fatal(err)
	}
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s1.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 套上内容租约不符合预期就停测。
		t.Fatal(err)
	}
	// 先拿到这一步的结果，后面断言还要用。
	p, err := s1.CreatePerson(ctx, "pe", "工艺师", "", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建账号不符合预期就停测。
		t.Fatal(err)
	}
	// 这条工艺稍后改回明文，充当遗留行。
	legacyBody := []byte(`{"legacy":1}`)
	// 先写入工艺，结果留给紧跟着的判断。
	legacy, err := s1.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "旧明文",
		Status: store.AssetDraft, Copyable: true, Content: legacyBody, Digest: digest.Sum(legacyBody), CreatorID: p.ID,
	})
	// 写入工艺失败就停测，避免后面没有前提。
	if err != nil {
		// 写入工艺不符合预期就停测。
		t.Fatal(err)
	}
	// 这条工艺一直是信封，补封不该碰它。
	keepBody := []byte(`{"keep":1}`)
	// 先写入工艺，结果留给紧跟着的判断。
	keep, err := s1.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "已封",
		Status: store.AssetDraft, Copyable: true, Content: keepBody, Digest: digest.Sum(keepBody), CreatorID: p.ID,
	})
	// 写入工艺失败就停测，避免后面没有前提。
	if err != nil {
		// 写入工艺不符合预期就停测。
		t.Fatal(err)
	}
	// 平台副本也备一条信封，补封同样不该碰它。
	repID := id.New()
	// 副本正文，写入时被封成信封。
	repBody := []byte(`{"replica":1}`)
	// 拼好第一修订的副本，写入时会被封上。
	repIn := store.AssetReplica{
		ID: repID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: true, Content: repBody, Digest: digest.Sum(repBody),
	}
	// 写入平台副本失败就停测，避免后面没有前提。
	if _, err := s1.InsertReplica(ctx, repIn); err != nil {
		// 写入平台副本不符合预期就停测。
		t.Fatal(err)
	}
	// 绕过封装把工艺正文改回明文，模拟升级前的旧行。
	if err := facDB.Exec("UPDATE assets SET content = ? WHERE id = ?", legacyBody, legacy.ID).Error; err != nil {
		// 改回明文不符合预期就停测。
		t.Fatal(err)
	}
	// 一次灌进超过两批的明文副本，每个身份三个修订。
	if err := facDB.Exec(`INSERT INTO asset_replicas (id, revision, kind, level, name, status, copyable, content, digest, deps)
		SELECT md5('seal-' || g)::uuid, r, 'process', 'platform', 'legacy-' || g, 'available', true,
			convert_to('plain-' || g || '-' || r, 'UTF8'), sha256(convert_to('plain-' || g || '-' || r, 'UTF8')), '[]'::jsonb
		FROM generate_series(1, ?::int) AS g, generate_series(1, ?::int) AS r`, legacyReplicaIDs, legacyReplicaRevs).Error; err != nil {
		// 灌明文副本不符合预期就停测。
		t.Fatal(err)
	}
	// 先读已封工艺的原文，之后要核对没被重封。
	keepRaw, err := s1.RawGovernedContent(ctx, keep.ID)
	// 读库内原文失败就停测，避免后面没有前提。
	if err != nil || !contentcrypt.IsEnvelope(keepRaw) {
		// 已封工艺不是信封就停测。
		t.Fatalf("keep raw %q %v", keepRaw, err)
	}
	// 先读已封副本的原文，之后要核对没被重封。
	repRaw, err := s1.RawReplicaContent(ctx, repID, 1)
	// 读库内原文失败就停测，避免后面没有前提。
	if err != nil || !contentcrypt.IsEnvelope(repRaw) {
		// 已封副本不是信封就停测。
		t.Fatalf("replica raw %q %v", repRaw, err)
	}

	// 换一个进程，首次领约要整扫补封。
	s2 := store.Open(facDB, facID)
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s2.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 首次领约补封不符合预期就停测。
		t.Fatalf("first lease: %v", err)
	}
	// 先读遗留工艺的原文，结果留给紧跟着的判断。
	raw, err := s2.RawGovernedContent(ctx, legacy.ID)
	// 遗留明文必须封成信封，且不再露出原文。
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, legacyBody) {
		// 遗留工艺没被封上就停测。
		t.Fatalf("legacy asset not sealed %q %v", raw, err)
	}
	// 先按身份读正文，结果留给紧跟着的判断。
	got, err := s2.GovernedAssetByID(ctx, legacy.ID)
	// 封上后读回的正文和修订都不能变。
	if err != nil || !bytes.Equal(got.Content, legacyBody) || got.Revision != legacy.Revision {
		// 补封把正文或修订弄坏了就停测。
		t.Fatalf("legacy asset read %q rev %d %v", got.Content, got.Revision, err)
	}
	// 再读已封工艺的原文，结果留给紧跟着的判断。
	raw, err = s2.RawGovernedContent(ctx, keep.ID)
	// 已封工艺的字节必须原样，证明没被重封。
	if err != nil || !bytes.Equal(raw, keepRaw) {
		// 已封工艺被改动就停测。
		t.Fatalf("sealed asset rewritten %v", err)
	}
	// 再读已封副本的原文，结果留给紧跟着的判断。
	raw, err = s2.RawReplicaContent(ctx, repID, 1)
	// 已封副本的字节必须原样，证明没被重封。
	if err != nil || !bytes.Equal(raw, repRaw) {
		// 已封副本被改动就停测。
		t.Fatalf("sealed replica rewritten %v", err)
	}
	// 准备承接灌进去的遗留副本。
	var rows []struct {
		ID       uuid.UUID // 副本身份
		Revision int64     // 副本修订
		Name     string    // 带序号的名字，用来推回原文
		Content  []byte    // 库内正文原文
	}
	// 读回全部遗留副本失败就停测，避免后面没有前提。
	if err := facDB.Raw("SELECT id, revision, name, content FROM asset_replicas WHERE name LIKE 'legacy-%'").Scan(&rows).Error; err != nil {
		// 读遗留副本不符合预期就停测。
		t.Fatal(err)
	}
	// 行数必须和灌进去的一样，确认夹具到位。
	if len(rows) != legacyReplicaIDs*legacyReplicaRevs {
		// 行数不对就停测。
		t.Fatalf("legacy replicas %d", len(rows))
	}
	// 逐条核对，超过一批的明文都要封上且能读回。
	for _, row := range rows {
		// 按名字和修订推回当初灌进去的原文。
		want := []byte(fmt.Sprintf("plain-%s-%d", strings.TrimPrefix(row.Name, "legacy-"), row.Revision))
		// 还是明文就说明翻页漏了这一行。
		if !contentcrypt.IsEnvelope(row.Content) || bytes.Contains(row.Content, want) {
			// 有副本没被封上就停测。
			t.Fatalf("replica %s/%d not sealed %q", row.ID, row.Revision, row.Content)
		}
		// 先按身份和修订读副本，结果留给紧跟着的判断。
		rep, err := s2.ReplicaByIDRev(ctx, row.ID, row.Revision)
		// 解开后必须回到原文，证明绑的是这一行的身份修订。
		if err != nil || !bytes.Equal(rep.Content, want) {
			// 读回的副本不对就停测。
			t.Fatalf("replica %s/%d read %q %v", row.ID, row.Revision, rep.Content, err)
		}
	}

	// 绕过封装再塞一条明文，同进程续租不该再扫到它。
	if err := facDB.Exec("UPDATE assets SET content = ? WHERE id = ?", keepBody, keep.ID).Error; err != nil {
		// 塞明文不符合预期就停测。
		t.Fatal(err)
	}
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s2.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 续租不符合预期就停测。
		t.Fatalf("renew: %v", err)
	}
	// 先读刚塞的明文，结果留给紧跟着的判断。
	raw, err = s2.RawGovernedContent(ctx, keep.ID)
	// 仍是原样明文，证明续租跳过了整扫。
	if err != nil || !bytes.Equal(raw, keepBody) {
		// 续租还在翻表就停测。
		t.Fatalf("renew must skip scan, got %q %v", raw, err)
	}

	// 夹具直改正文会清掉标记，下次领约要重新整扫。
	if err := s2.TamperAssetContent(ctx, legacy.ID, legacyBody); err != nil {
		// 夹具改正文不符合预期就停测。
		t.Fatal(err)
	}
	// 套上内容租约失败就停测，避免后面没有前提。
	if err := s2.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 重扫续租不符合预期就停测。
		t.Fatalf("rescan lease: %v", err)
	}
	// 两条工艺都要重新封上并能读回原文。
	for _, c := range []struct {
		id   uuid.UUID // 工艺身份
		body []byte    // 当初的原文
	}{{legacy.ID, legacyBody}, {keep.ID, keepBody}} {
		// 先读库内原文，结果留给紧跟着的判断。
		raw, err := s2.RawGovernedContent(ctx, c.id)
		// 还是明文就说明夹具没清掉标记。
		if err != nil || !contentcrypt.IsEnvelope(raw) {
			// 重扫没封上就停测。
			t.Fatalf("rescan %s not sealed %q %v", c.id, raw, err)
		}
		// 先按身份读正文，结果留给紧跟着的判断。
		got, err := s2.GovernedAssetByID(ctx, c.id)
		// 封上后读回的正文不能变。
		if err != nil || !bytes.Equal(got.Content, c.body) {
			// 重扫把正文弄坏了就停测。
			t.Fatalf("rescan %s read %q %v", c.id, got.Content, err)
		}
	}
}
