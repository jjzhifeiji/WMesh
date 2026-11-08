// D-102：工程 deps 只记声明时的身份、修订、摘要；组包一律按身份取工艺当前版。
package service_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 组包跟最新用例的共用夹具。
type latestEnv struct {
	h    *Harness        // 云端服务
	ctx  context.Context // 本测上下文
	tok  string          // 云端管理员会话
	proc global.Asset    // 被工程依赖的平台工艺
	proj global.Asset    // 声明时依赖旧版工艺的平台工程
}

// 建一份已发布的平台工艺和依赖它的已发布平台工程。
func newLatestEnv(t *testing.T) latestEnv {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 新建平台工艺，建不成后面没有工艺可升版。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "跟最新工艺", []byte(`{"current":211}`))
	// 新建平台工艺失败就停，建不成后面没有工艺可升版。
	if err != nil {
		// 不符即停：新建平台工艺失败。
		t.Fatal(err)
	}
	// 发布工艺，发不出去工程就依赖不上。
	if proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision); err != nil {
		// 不符即停：发布工艺失败。
		t.Fatal(err)
	}
	// 读回带摘要的工艺，声明依赖要用。
	if proc, err = h.WAN.GetPlatformAsset(ctx, tok, proc.ID); err != nil {
		// 不符即停：读回工艺失败。
		t.Fatal(err)
	}
	// 新建依赖它的平台工程。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "跟最新工程", []byte("proj"), []global.AssetDep{{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}})
	// 新建平台工程失败就停，建不成后面没有工程可发。
	if err != nil {
		// 不符即停：新建平台工程失败。
		t.Fatal(err)
	}
	// 发布工程，草稿不下发。
	if proj, err = h.WAN.PublishPlatformAsset(ctx, tok, proj.ID, proj.Revision); err != nil {
		// 不符即停：发布工程失败。
		t.Fatal(err)
	}
	return latestEnv{h: h, ctx: ctx, tok: tok, proc: proc, proj: proj}
}

// 登记一家新厂并交回厂身份。
func (e latestEnv) newFactory(t *testing.T, name, admin string) uuid.UUID {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 登记工厂，建不成后面没有厂可发。
	fac, err := e.h.WAN.CreateFactory(e.ctx, e.tok, name, admin, "超管")
	// 登记工厂失败就停，建不成后面没有厂可发。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	return fac.Factory.ID
}

// 把工艺正文升一版并交回新的当前行。
func (e latestEnv) upgradeProc(t *testing.T, body string) global.Asset {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 改正文升修订，工艺状态保持可用。
	up, err := e.h.WAN.UpdatePlatformAssetContent(e.ctx, e.tok, e.proc.ID, e.proc.Revision, []byte(body))
	// 改正文失败就停，没有新版后面都不成立。
	if err != nil {
		// 不符即停：工艺升版失败。
		t.Fatal(err)
	}
	return up
}

// 工程包必须是根加工艺当前版，根 deps 改写成当前版，库里的工程行不动。
func (e latestEnv) assertLatestSnap(t *testing.T, snap global.ClosureSnapshot, cur global.Asset) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 包空或成员不是根加一条工艺，这厂就收不到工程。
	if snap.AssetID != e.proj.ID || snap.Revision != e.proj.Revision || len(snap.Members) != 2 {
		// 不符即停：工程包形状不对。
		t.Fatalf("snap %+v", snap)
	}
	// 取工艺成员，它必须是当前版。
	m := snap.Members[1]
	// 成员不是当前修订、摘要或正文，就是还在发旧版。
	if m.ID != cur.ID || m.Revision != cur.Revision || !bytes.Equal(m.Digest, cur.Digest) {
		// 不符即停：成员不是当前版。
		t.Fatalf("member rev=%d cur=%d", m.Revision, cur.Revision)
	}
	// 取快照里根的依赖，厂端验收按它对成员。
	deps := snap.Members[0].Deps
	// 根 deps 没改写成当前版，厂端验收就过不了。
	if len(deps) != 1 || deps[0].ID != cur.ID || deps[0].Revision != cur.Revision || !bytes.Equal(deps[0].Digest, cur.Digest) {
		// 不符即停：快照根 deps 没跟上。
		t.Fatalf("snap deps %+v", deps)
	}
	// 读库里的工程行，组包不得动它。
	row, err := e.h.WAN.Store().AssetByID(e.ctx, e.proj.ID)
	// 读工程行失败就停，无法核对。
	if err != nil {
		// 不符即停：读工程行失败。
		t.Fatal(err)
	}
	// 工程修订或声明的依赖被改，就成了「工艺升版要改工程」。
	if row.Revision != e.proj.Revision || len(row.Deps) != 1 || row.Deps[0].Revision != e.proc.Revision || !bytes.Equal(row.Deps[0].Digest, e.proc.Digest) {
		// 不符即停：库里的工程行被改了。
		t.Fatalf("project row rev=%d deps=%+v", row.Revision, row.Deps)
	}
}

// 工艺升版后工程照样发到新厂，成员和快照根 deps 都是当前版。
func TestPackProjectToNewFactoryAfterProcessUpgrade(t *testing.T) {
	// 建好已发布的工艺和工程。
	e := newLatestEnv(t)
	// 工艺升一版，工程不改。
	cur := e.upgradeProc(t, `{"current":233}`)
	// 这家厂从没收过这份工程。
	fid := e.newFactory(t, "新厂", "sa-new")
	// 按身份组包。
	snap, err := e.h.WAN.PackAssetForFactory(e.ctx, e.proj.ID, fid)
	// 组包报错就停，这厂收不到工程。
	if err != nil {
		// 不符即停：组包失败。
		t.Fatal(err)
	}
	// 核对成员、根 deps 和库里的工程行。
	e.assertLatestSnap(t, snap, cur)
	// 离线回放走全量清单，工程也要在里面。
	all, err := e.h.WAN.PackAvailableForFactory(e.ctx, fid)
	// 全量组包失败就停，离线厂上线补不齐。
	if err != nil {
		// 不符即停：全量组包失败。
		t.Fatal(err)
	}
	// 记下全量里有没有这份工程。
	found := false
	// 逐包找这份工程。
	for _, s := range all {
		// 身份对上才算。
		if s.AssetID == e.proj.ID {
			// 全量里的工程也得是当前版。
			e.assertLatestSnap(t, s, cur)
			// 记下已找到。
			found = true
		}
	}
	// 全量漏了工程，离线回放就默默发不到厂。
	if !found {
		// 不符即停：全量清单缺工程。
		t.Fatalf("available %d lacks project", len(all))
	}
	// 通道指令清单也要带这份工程，厂端才会来拉。
	cmds, err := e.h.WAN.IndexForFactory(e.ctx, fid, global.KindProject)
	// 清单组不出就停。
	if err != nil {
		// 不符即停：指令清单失败。
		t.Fatal(err)
	}
	// 记下清单里有没有这份工程。
	listed := false
	// 逐条找这份工程的闭包指令。
	for _, c := range cmds {
		// 身份和修订都对才算。
		if c.Typ == global.CmdClosure && c.AssetID == e.proj.ID.String() && c.Revision == e.proj.Revision {
			// 记下已找到。
			listed = true
		}
	}
	// 清单里没有，厂端不会去拉。
	if !listed {
		// 不符即停：指令清单缺工程。
		t.Fatalf("index lacks project %+v", cmds)
	}
}

// 已收旧版的厂再组同修订工程：能组出当前版，不重复记下发、不改已有记录，也不多记审计。
func TestPackProjectToFactoryHoldingOldProcess(t *testing.T) {
	// 建好已发布的工艺和工程。
	e := newLatestEnv(t)
	// 这家厂先收旧版。
	fid := e.newFactory(t, "老厂", "sa-old")
	// 工艺没升版时组一次，记下首次下发。
	first, err := e.h.WAN.PackAssetForFactory(e.ctx, e.proj.ID, fid)
	// 首次组包失败就停。
	if err != nil || len(first.Members) != 2 {
		// 不符即停：首次组包失败。
		t.Fatalf("%+v %v", first, err)
	}
	// 读首次下发记录，后面比对不变。
	rec, err := e.h.WAN.Store().DistributionRecord(e.ctx, e.proj.ID, e.proj.Revision, fid)
	// 没记下就停。
	if err != nil {
		// 不符即停：读下发记录失败。
		t.Fatal(err)
	}
	// 工艺升版，工程不改。
	cur := e.upgradeProc(t, `{"current":244}`)
	// 读此刻审计条数，再组不该增加。
	before, err := e.h.WAN.ListAudit(e.ctx)
	// 读审计失败就停。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 同修订再组，闭包摘要已经变了。
	snap, err := e.h.WAN.PackAssetForFactory(e.ctx, e.proj.ID, fid)
	// 再组报错就停，摘要变化不能变成故障。
	if err != nil {
		// 不符即停：再组失败。
		t.Fatal(err)
	}
	// 核对成员、根 deps 和库里的工程行。
	e.assertLatestSnap(t, snap, cur)
	// 总摘要应随成员变化，否则就是没换成员。
	if bytes.Equal(first.Digest, snap.Digest) {
		// 不符即停：总摘要没变。
		t.Fatal("closure digest unchanged")
	}
	// 工程修订没变，通道指令里的修订也不变，已收的厂不会被循环重推。
	if snap.Revision != first.Revision {
		// 不符即停：工程修订跟着变了。
		t.Fatalf("revision %d -> %d", first.Revision, snap.Revision)
	}
	// 读下发记录，同一修订只有一条。
	again, err := e.h.WAN.Store().DistributionRecord(e.ctx, e.proj.ID, e.proj.Revision, fid)
	// 记录被换或读不到，对账就乱了。
	if err != nil || again.ID != rec.ID || !bytes.Equal(again.ClosureDigest, rec.ClosureDigest) {
		// 不符即停：下发记录变了。
		t.Fatalf("record %+v %v", again, err)
	}
	// 再读审计，同修订再组不多记工程这条。
	after, err := e.h.WAN.ListAudit(e.ctx)
	// 读审计失败就停。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 多出审计说明进页补拉会刷屏。
	if len(after) != len(before) {
		// 不符即停：同修订再组多记了审计。
		t.Fatalf("audit %d -> %d", len(before), len(after))
	}
	// 管理员手动下发同一修订也不能因摘要变化报错。
	if err := e.h.WAN.GrantFactoryAsset(e.ctx, e.tok, e.proj.ID, fid); err != nil {
		// 不符即停：授权失败。
		t.Fatal(err)
	}
	// 手动下发走另一条记账路径。
	manual, err := e.h.WAN.DistributeToFactory(e.ctx, e.tok, e.proj.ID, fid)
	// 手动下发失败就停。
	if err != nil {
		// 不符即停：手动下发失败。
		t.Fatal(err)
	}
	// 手动下发的也是当前版。
	e.assertLatestSnap(t, manual, cur)
}

// 过站加封按当前版的身份和修订绑定，到站能解开，正文是新版。
func TestPullProjectTransitSealsLatestProcess(t *testing.T) {
	// 建好已发布的工艺和工程。
	e := newLatestEnv(t)
	// 工艺升一版。
	cur := e.upgradeProc(t, `{"current":255}`)
	// 这家厂从没收过这份工程。
	fid := e.newFactory(t, "加封厂", "sa-seal")
	// 签发租约，过站信封靠它。
	lease, err := e.h.WAN.IssueContentLease(e.ctx, fid)
	// 签不出就停。
	if err != nil {
		// 不符即停：签发租约失败。
		t.Fatal(err)
	}
	// 走通道同款的组包加封。
	sealed, err := e.h.WAN.PullClosureForFactory(e.ctx, fid, e.proj.ID)
	// 组包或加封失败就停。
	if err != nil {
		// 不符即停：拉工程失败。
		t.Fatal(err)
	}
	// 核对成员、根 deps 和库里的工程行。
	e.assertLatestSnap(t, sealed, cur)
	// 取工艺成员，它应已换成信封。
	m := sealed.Members[1]
	// 没加封就是明文出站。
	if !contentcrypt.IsEnvelope(m.Content) {
		// 不符即停：成员没加封。
		t.Fatal("member not sealed")
	}
	// 按当前修订绑定解开，绑错修订到站就解不开。
	plain, err := contentcrypt.Open(lease.Key, m.Content, contentcrypt.TransitAAD(fid, m.ID, cur.Revision))
	// 解不开或不是新版正文就停。
	if err != nil || !digest.Match(plain, cur.Digest) {
		// 不符即停：到站解不开或正文不对。
		t.Fatalf("open %q %v", plain, err)
	}
}

// 工艺已停用：工程照发，成员带停用状态，厂端列表不显示但工程能用。
func TestPackProjectWithDisabledProcess(t *testing.T) {
	// 建好已发布的工艺和工程。
	e := newLatestEnv(t)
	// 停用工艺，修订随之升高。
	off, err := e.h.WAN.DisablePlatformAsset(e.ctx, e.tok, e.proc.ID, e.proc.Revision)
	// 停用失败就停。
	if err != nil {
		// 不符即停：停用工艺失败。
		t.Fatal(err)
	}
	// 这家厂从没收过这份工程。
	fid := e.newFactory(t, "停用厂", "sa-off")
	// 按身份组包。
	snap, err := e.h.WAN.PackAssetForFactory(e.ctx, e.proj.ID, fid)
	// 停用不算缺失，不该挡住工程。
	if err != nil {
		// 不符即停：组包失败。
		t.Fatal(err)
	}
	// 核对成员、根 deps 和库里的工程行。
	e.assertLatestSnap(t, snap, off)
	// 成员状态要如实带停用。
	if snap.Members[1].Status != global.AssetDisabled {
		// 不符即停：成员状态不对。
		t.Fatalf("member status %s", snap.Members[1].Status)
	}
}

// 工艺回到草稿、已删除或不是工艺：工程仍拒绝组包，且不留半截记录。
func TestPackProjectRejectsUnusableProcess(t *testing.T) {
	// 组不出包时交回空，不打断其它条。
	assertNoPack := func(t *testing.T, e latestEnv, fid uuid.UUID) {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 按身份组包，应是空包。
		snap, err := e.h.WAN.PackAssetForFactory(e.ctx, e.proj.ID, fid)
		// 报错或组出包都不对。
		if err != nil || snap.AssetID != uuid.Nil {
			// 不符即停：该拒绝却组出了包。
			t.Fatalf("snap %+v %v", snap, err)
		}
	}
	// 重新上传升档会让工艺回到草稿，未发布的正文不得外发。
	t.Run("草稿", func(t *testing.T) {
		// 建好已发布的工艺和工程。
		e := newLatestEnv(t)
		// 把工艺改回草稿，模拟重新升档覆盖。
		body := []byte(`{"current":999}`)
		// 直写当前行，等同升档覆盖的效果。
		if _, err := e.h.WAN.Store().UpdateAsset(e.ctx, e.proc.ID, e.proc.Revision, store.AssetWrite{
			// 正文换新，状态回草稿。
			Name: e.proc.Name, Content: body, Digest: digest.Sum(body), Copyable: e.proc.Copyable, Status: global.AssetDraft,
		}); err != nil {
			// 不符即停：改回草稿失败。
			t.Fatal(err)
		}
		// 草稿工艺不能进包。
		assertNoPack(t, e, e.newFactory(t, "草稿厂", "sa-draft"))
	})
	// 工艺已删除，身份取不到当前行。
	t.Run("已删除", func(t *testing.T) {
		// 建好已发布的工艺和工程。
		e := newLatestEnv(t)
		// 删除保护挡着删不掉，直接删库里这一行，等同依赖丢失。
		if err := e.h.DB.WithContext(e.ctx).Exec("DELETE FROM assets WHERE id = ?", e.proc.ID).Error; err != nil {
			// 不符即停：删工艺失败。
			t.Fatal(err)
		}
		// 缺工艺不能组包。
		assertNoPack(t, e, e.newFactory(t, "缺件厂", "sa-gone"))
	})
	// 依赖指向工程，不是工艺。
	t.Run("不是工艺", func(t *testing.T) {
		// 建好已发布的工艺和工程。
		e := newLatestEnv(t)
		// 另建一份工程当错误依赖。
		other, err := e.h.WAN.CreatePlatformProject(e.ctx, e.tok, "错误依赖", []byte("x"), nil)
		// 建不成就停。
		if err != nil {
			// 不符即停：建工程失败。
			t.Fatal(err)
		}
		// 直写依赖，等同脏数据。
		up, err := e.h.WAN.Store().UpdateAsset(e.ctx, e.proj.ID, e.proj.Revision, store.AssetWrite{
			// 依赖换成工程。
			Name: e.proj.Name, Content: []byte("proj"), Digest: digest.Sum([]byte("proj")), Copyable: e.proj.Copyable, Status: e.proj.Status,
			Deps: []global.AssetDep{{ID: other.ID, Revision: other.Revision, Digest: other.Digest}},
		})
		// 写不进就停。
		if err != nil {
			// 不符即停：改依赖失败。
			t.Fatal(err)
		}
		// 夹具里的工程行跟上。
		e.proj = up
		// 依赖不是工艺不能组包。
		assertNoPack(t, e, e.newFactory(t, "错依赖厂", "sa-kind"))
	})
}
