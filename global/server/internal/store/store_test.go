// 第 3 圈：WAN 库约束（单管理员、一厂一名初始超管、不见厂内表）。
package store_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// 验云端只有一名管理员、一厂一名初始超管，且没有厂内表。
func TestWANConstraints(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 每次打开干净库，避免别的用例留下的行。
	s := store.Open(testpg.Fresh(t))

	// 空库里不该已经有管理员。
	if n, err := s.AdminCount(ctx); err != nil || n != 0 {
		// 人数不对就停，空库前提不成立。
		t.Fatalf("empty wan admins: n=%d err=%v", n, err)
	}
	// 先写入唯一管理员，会话和审计都靠他。
	admin, err := s.CreateAdmin(ctx, "wan", "hash-1")
	// 管理员没写成就停，后面的断言没有操作者。
	if err != nil {
		// 管理员没写成就停，后面的单人约束没法验。
		t.Fatalf("create admin: %v", err)
	}
	// 第二名管理员必须被单行约束拒绝。
	if _, err := s.CreateAdmin(ctx, "other", "hash-2"); err != domain.ErrWANAdminExists {
		// 没被拒绝就停，云端不能有两名管理员。
		t.Fatalf("second admin: %v", err)
	}
	// 拒绝之后仍然只能有一名管理员。
	if n, err := s.AdminCount(ctx); err != nil || n != 1 {
		// 人数不是一就停，单行约束没守住。
		t.Fatalf("singleton admin: n=%d err=%v", n, err)
	}

	// 登记一家厂，用来验初始超管只能一名。
	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	// 厂没登记上就停，后面的步骤没有名录。
	if err != nil {
		// 厂没建成就停，后面没有名录可挂。
		t.Fatalf("factory: %v", err)
	}
	// 同一厂再绑一名初始超管必须被拒绝。
	if err := s.BindInitialSuperAdmin(ctx, fac.ID, id.New(), "sa-a-2", "超管2"); err != domain.ErrInitialSAExists {
		// 没被拒绝就停，一厂不能有两名初始超管。
		t.Fatalf("second initial sa: %v", err)
	}
	// 同一身份重复注册会整体回滚，名录里不会多出一行。
	if _, err := s.RegisterFactory(ctx, fac.ID, "厂A重复", id.New(), "sa-a-3", "超管3", "enroll-dup"); err == nil {
		// 重复身份居然成功就停，回滚没有发生。
		t.Fatalf("duplicate factory id must fail")
	}
	// 回滚之后名录里仍然只能有刚才那一家。
	if facs, err := s.ListFactories(ctx); err != nil || len(facs) != 1 {
		// 家数不对就停，重复注册没有整笔撤销。
		t.Fatalf("factories after rollback: n=%d err=%v", len(facs), err)
	}

	// 第一把令牌应该能开出会话。
	if _, err := s.CreateSession(ctx, admin.ID, "tok-hash", time.Now().Add(time.Hour)); err != nil {
		// 会话没写成就停，重复令牌还没法验。
		t.Fatalf("session: %v", err)
	}
	// 同一令牌哈希不能再开一会话。
	if _, err := s.CreateSession(ctx, admin.ID, "tok-hash", time.Now().Add(time.Hour)); err != domain.ErrDuplicateSession {
		// 没被拒绝就停，同一令牌不能有两份会话。
		t.Fatalf("dup session: %v", err)
	}

	// 建厂这件事应该能写进云端审计。
	if err := s.AppendAudit(ctx, audit.Event{Action: "create_factory", Target: fac.ID.String(), Result: audit.Allow, ActorID: &admin.ID, FactoryID: &fac.ID}); err != nil {
		// 审计没落下就停，云端留不住这件事。
		t.Fatalf("audit: %v", err)
	}

	// 这些都是厂内表，云端库里不该出现。
	for _, table := range []string{"people", "org_types", "org_units", "assignments", "role_grants", "person_offline_grants", "signing_keys"} {
		// 逐张问这些厂内表在不在，云端不该有。
		ok, err := s.HasTable(ctx, table)
		// 查表失败就停，不能把故障当成没有这张表。
		if err != nil {
			// 查表出错就停，这一张还没验完。
			t.Fatalf("has %s: %v", table, err)
		}
		// 云端出现厂内表就停，说明库串了。
		if ok {
			// 表真的在就停，云端不该有厂内人员组织。
			t.Fatalf("wan must not have table %s", table)
		}
	}
}

// 验公钥、识别号和改厂都按绑定修订拒绝重复。
func TestWANClientBinding(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 每次打开干净库，避免别的用例留下的行。
	s := store.Open(testpg.Fresh(t))

	// 管理员应该能建出来，失败就不用往下验。
	if _, err := s.CreateAdmin(ctx, "wan", "hash"); err != nil {
		// 管理员没建成就停，后面的步骤没有操作者。
		t.Fatal(err)
	}
	// 登记第一家厂，作为绑定的起点。
	a, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	// 厂没登记上就停，后面的步骤没有名录。
	if err != nil {
		// 厂没建成就停，后面没有名录可挂。
		t.Fatal(err)
	}
	// 登记第二家厂，作为改绑的目标。
	b, err := s.RegisterFactory(ctx, id.New(), "厂B", id.New(), "sa-b", "超管B", "enroll-b")
	// 第二家厂没登记上就停，改绑没有目标。
	if err != nil {
		// 第二家厂没建成就停，改绑没有目标厂。
		t.Fatal(err)
	}

	// 给这家厂准备一把签发公钥。
	pubA, _ := mustEd25519(t)
	// 第一把厂钥应该能登记上。
	if err := s.PutFactoryPublicKey(ctx, a.ID, pubA); err != nil {
		// 厂钥没登记上就停，重复登记还没法验。
		t.Fatalf("factory key: %v", err)
	}
	// 同一厂再登记公钥必须被拒绝。
	if err := s.PutFactoryPublicKey(ctx, a.ID, pubA); err != domain.ErrFactoryKeyExists {
		// 同一把钥再登记没被拒绝就停。
		t.Fatalf("dup factory key: %v", err)
	}
	// 过短的公钥必须按坏钥拒绝。
	if err := s.PutFactoryPublicKey(ctx, a.ID, []byte("short")); err != domain.ErrInvalidKey {
		// 短钥没被拒绝就停，坏钥约束没生效。
		t.Fatalf("short factory key: %v", err)
	}

	// 给第一台设备准备一把公钥。
	cPub, _ := mustEd25519(t)
	// 登记一台带公钥和识别号、尚未分配的设备。
	c, err := s.CreateClient(ctx, id.New(), "焊机-A", cPub, "ARM-A")
	// 设备没登记上就停，绑定没有对象。
	if err != nil {
		// 设备没登记上就停，后面的绑定没有对象。
		t.Fatalf("create client: %v", err)
	}
	// 空库里不该已经有管理员。
	if c.Name != "焊机-A" || c.FactoryID != nil || c.BindingRevision != 0 || c.DeviceSerial != "ARM-A" {
		// 刚登记的设备状态不对就停。
		t.Fatalf("unbound: %+v", c)
	}
	// 登记一台还没有公钥的设备，看空钥怎么落。
	pending, err := s.CreateClient(ctx, id.New(), "待上线", nil, "")
	// 没给公钥时名字要在，公钥必须是空的。
	if err != nil || pending.Name != "待上线" || len(pending.PublicKey) != 0 {
		// 空公钥没有按预期留下就停。
		t.Fatalf("no pubkey: %+v %v", pending, err)
	}
	// 空白名字必须被拒绝，不能登记成功。
	if _, err := s.CreateClient(ctx, id.New(), "  ", nil, ""); err != domain.ErrInvalidName {
		// 空白名字没被拒绝就停。
		t.Fatalf("empty name: %v", err)
	}
	// 同一把设备公钥不能再登记第二台。
	if _, err := s.CreateClient(ctx, id.New(), "焊机-A2", cPub, "ARM-A2"); err != domain.ErrClientKeyTaken {
		// 重复公钥没被拒绝就停。
		t.Fatalf("dup pubkey: %v", err)
	}
	// 同一个机械臂号不能再登记第二台。
	if _, err := s.CreateClient(ctx, id.New(), "焊机-A3", nil, "ARM-A"); err != domain.ErrDeviceSerialTaken {
		// 重复识别号没被拒绝就停。
		t.Fatalf("dup serial: %v", err)
	}

	// 把这台设备分给第一家厂。
	bound, err := s.BindClient(ctx, c.ID, a.ID)
	// 绑定没写成就停，修订有没有升高还不清楚。
	if err != nil {
		// 绑定没写成就停，修订有没有升高还不清楚。
		t.Fatalf("bind: %v", err)
	}
	// 拒绝之后仍然只能有一名管理员。
	if bound.FactoryID == nil || *bound.FactoryID != a.ID || bound.BindingRevision != 1 {
		// 绑定结果不对就停，归属或修订不符合。
		t.Fatalf("bound: %+v", bound)
	}
	// 已经绑过的设备不能再绑到另一家。
	if _, err := s.BindClient(ctx, c.ID, b.ID); err != domain.ErrClientBound {
		// 再绑到另一家没被拒绝就停。
		t.Fatalf("second bind: %v", err)
	}

	// 把已经绑定的设备改到第二家厂。
	reb, err := s.RebindClient(ctx, c.ID, b.ID)
	// 改绑没写成就停，不能把错误当成已经换厂。
	if err != nil {
		// 改绑没写成就停，不能把错误当成已经换厂。
		t.Fatalf("rebind: %v", err)
	}
	// 改绑之后应属于第二家，修订再升一档。
	if reb.FactoryID == nil || *reb.FactoryID != b.ID || reb.BindingRevision != 2 {
		// 改绑没写成就停，不能把错误当成已经换厂。
		t.Fatalf("rebind: %+v", reb)
	}
	// 还是这一家就不算改分，必须拒绝。
	if _, err := s.RebindClient(ctx, c.ID, b.ID); err != domain.ErrClientBound {
		// 原厂再改一次没被拒绝就停。
		t.Fatalf("rebind same: %v", err)
	}

	// 再准备一把公钥，用来登记尚未绑定的设备。
	unboundPub, _ := mustEd25519(t)
	// 再登记一台未绑定的，用来验不能直接改绑。
	u, err := s.CreateClient(ctx, id.New(), "焊机-U", unboundPub, "")
	// 这台未绑定设备没登记上就停。
	if err != nil {
		// 这台设备没登记上就停，改绑没有对象。
		t.Fatal(err)
	}
	// 还没绑定的设备不能走改绑。
	if _, err := s.RebindClient(ctx, u.ID, a.ID); err != domain.ErrUnbound {
		// 刚登记的设备状态不对就停。
		t.Fatalf("rebind unbound: %v", err)
	}

	// 查设备表有没有私钥列，云端不该有。
	ok, err := s.HasColumn(ctx, "clients", "private_key")
	// 没查成或者真有私钥列，都不能当云端合格。
	if err != nil || ok {
		// 出现私钥列或查询失败就停，云端不能存私钥。
		t.Fatalf("wan clients must not have private_key: ok=%v err=%v", ok, err)
	}

	// 读出管理员，审计要带上操作者。
	admin, err := s.AdminByLogin(ctx, "wan")
	// 管理员没读到就停，审计缺少操作者。
	if err != nil {
		// 管理员没读到就停，审计缺少操作者。
		t.Fatal(err)
	}
	// 改绑应该能按现场时钟记一笔审计。
	if err := s.AppendAudit(ctx, audit.Event{
		// 记的是改绑，目标是这台设备，结果按允许。
		Action: "bind_client", Target: c.ID.String(), Result: audit.Allow,
		ActorID: &admin.ID, FactoryID: &b.ID, TimeSource: audit.Local,
	}); err != nil {
		// 改绑审计没落下就停，现场时钟没记上。
		t.Fatalf("local audit: %v", err)
	}
}

// 验平台级可复制、升档来源、修订冲突，以及正文被改能发现。
func TestWANPlatformAssets(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 自己拿库连接，才能绕过接口改正文。
	db := testpg.Fresh(t)
	// 用刚才那份库，后面要直接改表里的正文。
	s := store.Open(db)
	// 先写入管理员，当作后面资产的创建人。
	admin, err := s.CreateAdmin(ctx, "wan", "hash")
	// 管理员没写成就停，后面的断言没有操作者。
	if err != nil {
		// 管理员没建成就停，后面的步骤没有操作者。
		t.Fatal(err)
	}
	// 登记来源厂，升档要带上这家的身份。
	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	// 厂没登记上就停，后面的步骤没有名录。
	if err != nil {
		// 厂没建成就停，后面没有名录可挂。
		t.Fatal(err)
	}
	// 用一小段正文，后面好对摘要。
	body := []byte(`{"voltage":40}`)
	// 摘要按这段正文现算，写入时必须对得上。
	sum := digest.Sum(body)
	// 写入一条可复制的平台工艺，修订从一开始。
	a, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "平台工艺", Status: store.AssetDraft,
		Copyable: true, Content: body, Digest: sum, CreatorID: admin.ID,
	})
	// 平台工艺没写进去就停，后面没有对象可对。
	if err != nil {
		// 工艺没写进去就停，后面没有对象可对。
		t.Fatalf("insert: %v", err)
	}
	// 拒绝之后仍然只能有一名管理员。
	if a.Level != store.AssetLevelPlatform || !a.Copyable || a.Revision != 1 || contentcrypt.IsEnvelope(a.Content) {
		// 级别、可复制或修订不对，或正文被封了就停。
		t.Fatalf("platform copyable: %+v", a)
	}
	// 升档源的身份现发一个，不必真有厂级行。
	src := id.New()
	// 源修订故意取三，用来核对升档有没有记下。
	rev := int64(3)
	// 再写入一条升档来的，带上源厂和源修订。
	promoted, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "升档来的", Status: store.AssetAvailable,
		Content: body, Digest: sum, CreatorID: admin.ID,
		SourceID: &src, SourceRevision: &rev, SourceFactoryID: &fac.ID,
	})
	// 升档来的要记下源身份，并且不可复制。
	if err != nil || promoted.SourceID == nil || *promoted.SourceID != src || promoted.Copyable {
		// 升档来源或可复制标记不对就停。
		t.Fatalf("promoted: %+v %v", promoted, err)
	}
	// 按当前修订改名并标成可用，这一次应该成功。
	if _, err := s.UpdateAsset(ctx, a.ID, 1, store.AssetWrite{
		// 把名称改掉并标成可用，正文仍用原来的摘要。
		Name: "平台工艺-2", Content: body, Digest: sum, Status: store.AssetAvailable,
	}); err != nil {
		// 按当前修订更新失败就停。
		t.Fatalf("update: %v", err)
	}
	// 用已经过期的修订再改，必须被拒绝。
	if _, err := s.UpdateAsset(ctx, a.ID, 1, store.AssetWrite{
		// 用过期修订填一份写回，应当对不上当前修订。
		Name: "旧", Content: body, Digest: sum, Status: store.AssetDraft,
	}); err != domain.ErrRevisionConflict {
		// 过期修订居然写成功就停。
		t.Fatalf("conflict: %v", err)
	}
	// 只改正文不改摘要，用来造一份被篡改的行。
	if err := db.Exec("UPDATE assets SET content = ? WHERE id = ?", []byte("dirty"), a.ID).Error; err != nil {
		// 正文没改掉就停，篡改前提不成立。
		t.Fatal(err)
	}
	// 再读这一行，看摘要还对不对得上正文。
	dirty, err := s.AssetByID(ctx, a.ID)
	// 摘要对不上正文时，读出来就应该发现。
	if err != nil || digest.Match(dirty.Content, dirty.Digest) {
		// 篡改没被发现，或这一行读失败就停。
		t.Fatalf("tamper: match=%v err=%v", digest.Match(dirty.Content, dirty.Digest), err)
	}
	// 删掉原件应成功，后面才能再插草稿。
	if err := s.DeleteAsset(ctx, a.ID); err != nil {
		// 原件没删掉就停，后面的草稿插不进去。
		t.Fatal(err)
	}
	// 绕过接口插一条可复制草稿，看库放不放行。
	if err := db.Exec(
		`INSERT INTO assets (id, kind, level, name, code, status, copyable, revision, content, digest, creator_id, deps)
		 VALUES (?, 'process', 'platform', '可复制草稿', 'GY-W-000099', 'draft', true, 1, decode('00','hex'), ?, ?, '[]'::jsonb)`,
		id.New(), sum, admin.ID,
	).Error; err != nil {
		// 可复制草稿插不进去就停，约束可能过严。
		t.Fatalf("copyable true: %v", err)
	}
	// 查有没有厂内人员表，云端不该有。
	ok, err := s.HasTable(ctx, "people")
	// 没查成或者真有人员表，都不能当云端合格。
	if err != nil || ok {
		// 出现人员表或查询失败就停。
		t.Fatalf("wan must not have people: %v %v", ok, err)
	}
}

// 现场生成一对测试钥，失败时让调用栈指回用例。
func mustEd25519(t *testing.T) ([]byte, []byte) {
	// 失败时让栈指到用例，不指到这个帮手。
	t.Helper()
	// 现场生成一对钥，不把私钥写死在用例里。
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	// 钥没生成就停，调用方拿不到测试钥。
	if err != nil {
		// 钥没生成就停，调用方拿不到测试钥。
		t.Fatal(err)
	}
	return pub, priv
}
