// 示教器新建只收新身份：不得冒充平台工艺，不得占用外来号段。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 已占身份、外来号段都要拒绝且不占计数，删过的身份只能沿用原号。
func TestPadCreateIdentityGuard(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出超管令牌，后面建厂级和删条目要用。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一名现场操作员，示教器新建都用他。
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 不指定文件夹，网页新建都挂在对应树根。
	direct := factory.WorkContext{}
	// 登记 C0008 这台焊机，它的 C 段号才是合规号。
	cid := mustClientShortCode(t, ctx, fac, "C0008")
	// 准备示教器新建用的正文。
	body := []byte(`{"name":"pad","current":170}`)
	// 留出本厂身份，收平台下发时要指明目标厂。
	fid := seed.ID

	// 准备一条平台工艺的正文。
	platBody := []byte("plat-body")
	// 组装平台工艺 P，号是云端 W 段。
	plat := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台焊",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	// 收下平台工艺失败就停，后面没有可冒充的目标。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 带平台号、合规 C 号、空号三种都试冒充 P。
	for _, code := range []string{"GY-W-000001", "GY-C0008-000001", ""} {
		// 用 P 的身份新建，应当按编号冲突拒绝。
		_, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "冒充", body, plat.ID, code, nil)
		// 放行或错误不对就说明冒充没拦住。
		if !errors.Is(err, domain.ErrAssetCodeConflict) {
			// 带上用的号，便于看出哪一种漏了。
			t.Fatalf("impersonate platform with %q: %v", code, err)
		}
	}
	// 读回 P，应仍是平台级原文。
	gotPlat, err := fac.GetAsset(ctx, sa, plat.ID)
	// 级别或摘要变了就说明 P 被盖掉了。
	if err != nil || gotPlat.Level != factory.AssetLevelPlatform || !bytes.Equal(gotPlat.Digest, plat.Digest) {
		// 把读回的 P 打出来并停住。
		t.Fatalf("platform after impersonation %+v %v", gotPlat, err)
	}
	// 再造一条没带号的老副本 Q。
	oldID := id.New()
	// 老副本只带身份和正文，不带编号。
	oldRep := store.AssetReplica{
		ID: oldID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "老副本", Status: store.AssetAvailable, Copyable: true, Content: platBody, Digest: digest.Sum(platBody),
	}
	// 写老副本失败就停，后面没有目标可试。
	if _, err := fac.Store().InsertReplica(ctx, oldRep); err != nil {
		// 把写副本的错误打出来并停住。
		t.Fatal(err)
	}
	// 空号和合规 C 号都试冒充 Q。
	for _, code := range []string{"", "GY-C0008-000001"} {
		// 用 Q 的身份新建，应当按编号冲突拒绝。
		_, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "冒充老副本", body, oldID, code, nil)
		// 放行或错误不对就说明老副本没护住。
		if !errors.Is(err, domain.ErrAssetCodeConflict) {
			// 带上用的号，便于看出哪一种漏了。
			t.Fatalf("impersonate old replica with %q: %v", code, err)
		}
	}

	// 本厂未来号、他厂号、迁移号、云端号、未登记 C 号都不收。
	for _, code := range []string{"GY-F01-000003", "GY-F02-000001", "GY-F00-000001", "GY-W-000009", "GY-C0009-000001", "GC-C0008-000001"} {
		// 用新身份带外来号新建，应当按编号冲突拒绝。
		_, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "外来号", body, id.New(), code, nil)
		// 放行或错误不对就说明号段没核。
		if !errors.Is(err, domain.ErrAssetCodeConflict) {
			// 带上用的号，便于看出哪一段漏了。
			t.Fatalf("foreign origin %q: %v", code, err)
		}
	}
	// 连建三条厂级工艺，号应从 1 连着发，证明计数没被占。
	for i, want := range []string{"GY-F01-000001", "GY-F01-000002", "GY-F01-000003"} {
		// 网页新建一条厂级工艺。
		p, err := fac.CreateFactoryProcess(ctx, sa, direct, "厂焊", []byte(`{"name":"f","current":180}`))
		// 失败或号不对就说明计数被拒绝的请求动过。
		if err != nil || p.Code != want {
			// 带上第几条和拿到的号。
			t.Fatalf("factory seq %d: %+v %v", i, p, err)
		}
	}
	// 再组装一条平台工艺，号就是刚才被拒的 W 号。
	plat9 := plat
	// 换成新身份，免得和 P 重复。
	plat9.ID = id.New()
	// 换成刚才被拒的 W 号，证明没被占住。
	plat9.Code = "GY-W-000009"
	// 收下这条平台工艺，失败说明 W 号被占死了。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat9, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatalf("wan code taken: %v", err)
	}
	// 登记过的 C0008 号应能建成，且原样保留。
	okPad, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "合规号", body, id.New(), "GY-C0008-000001", nil)
	// 失败或号被改就说明合规号也被拦了。
	if err != nil || okPad.Code != "GY-C0008-000001" {
		// 把建成的条目打出来并停住。
		t.Fatalf("registered client code %+v %v", okPad, err)
	}
	// 作废这台焊机，它以前发出的号仍要认。
	if err := fac.VoidBinding(ctx, cid); err != nil {
		// 把作废的错误打出来并停住。
		t.Fatal(err)
	}
	// 作废后的 C0008 新号仍应能建成。
	voided, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "作废前离线", body, id.New(), "GY-C0008-000002", nil)
	// 失败或号被改就说明作废设备的号不认了。
	if err != nil || voided.Code != "GY-C0008-000002" {
		// 把建成的条目打出来并停住。
		t.Fatalf("voided client code %+v %v", voided, err)
	}

	// 网页新建一条个人工艺，身份已在本厂，号接着 3 号发。
	web, err := fac.CreatePersonalProcess(ctx, op.tok, direct, "网页稿", []byte(`{"name":"web","current":160}`))
	// 建网页稿失败或号不对就停，后面没有目标可试。
	if err != nil || web.Code != "GY-F01-000004" {
		// 把建网页稿的结果打出来并停住。
		t.Fatalf("web asset %+v %v", web, err)
	}
	// 空号和合规 C 号都试盖掉网页稿。
	for _, code := range []string{"", "GY-C0008-000003"} {
		// 用网页稿的身份新建，应当按编号冲突拒绝。
		_, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "盖网页稿", body, web.ID, code, nil)
		// 放行或错误不对就说明已有身份没护住。
		if !errors.Is(err, domain.ErrAssetCodeConflict) {
			// 带上用的号，便于看出哪一种漏了。
			t.Fatalf("existing id with %q: %v", code, err)
		}
	}
	// 读回网页稿，修订和摘要都不能变。
	webNow, err := fac.GetAsset(ctx, op.tok, web.ID)
	// 修订或摘要变了就说明被盖写过。
	if err != nil || webNow.Revision != web.Revision || !bytes.Equal(webNow.Digest, web.Digest) || webNow.Code != web.Code {
		// 把读回的网页稿打出来并停住。
		t.Fatalf("web asset after reject %+v %v", webNow, err)
	}

	// 示教器新建 X，删后再按原身份重建。
	padID := id.New()
	// 首次新建 X，带合规 C 号。
	x, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "删后重建", body, padID, "GY-C0008-000010", nil)
	// 首次新建失败就停，后面没有可删的条目。
	if err != nil || x.Code != "GY-C0008-000010" {
		// 把建成的条目打出来并停住。
		t.Fatalf("create x %+v %v", x, err)
	}
	// 删掉 X，删后才能按原身份重建。
	mustDeleteAsset(t, ctx, fac, op.tok, padID)
	// 同身份同号重建应当成功。
	x, err = fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "删后重建", body, padID, "GY-C0008-000010", nil)
	// 失败或号不对就说明原身份重建被误拦。
	if err != nil || x.Code != "GY-C0008-000010" {
		// 把重建的条目打出来并停住。
		t.Fatalf("rebuild same code %+v %v", x, err)
	}
	// 再删一次，试不带号重建。
	mustDeleteAsset(t, ctx, fac, op.tok, padID)
	// 同身份空号重建应沿用原号。
	x, err = fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "删后重建", body, padID, "", nil)
	// 失败或没沿用原号就说明登记没接上。
	if err != nil || x.Code != "GY-C0008-000010" {
		// 把重建的条目打出来并停住。
		t.Fatalf("rebuild empty code %+v %v", x, err)
	}
	// 再建一条厂级工艺，号应紧接着网页稿的 4 号。
	f4, err := fac.CreateFactoryProcess(ctx, sa, direct, "厂焊4", []byte(`{"name":"f4","current":181}`))
	// 失败或号不对就说明沿用原号时动了计数。
	if err != nil || f4.Code != "GY-F01-000005" {
		// 把建成的条目打出来并停住。
		t.Fatalf("seq after rebuild %+v %v", f4, err)
	}
	// 第三次删掉 X，试换号与换种类。
	mustDeleteAsset(t, ctx, fac, op.tok, padID)
	// 换一个登记过的 C0008 号重建，应当拒绝。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "换号", body, padID, "GY-C0008-000011", nil); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 换号被放行就说明同身份能改号。
		t.Fatalf("rebuild other code: %v", err)
	}
	// 改成工程种类重建，原号前缀对不上，应当拒绝。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "换种类", []byte(`[]`), padID, "", nil); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 换种类被放行就说明种类没核。
		t.Fatalf("rebuild other kind: %v", err)
	}

	// 网页新建的厂级 F 号条目，删后按原身份原号重建。
	f5, err := fac.CreateFactoryProcess(ctx, sa, direct, "厂焊5", []byte(`{"name":"f5","current":182}`))
	// 建厂级工艺失败就停，后面没有可删的条目。
	if err != nil || f5.Code != "GY-F01-000006" {
		// 把建成的条目打出来并停住。
		t.Fatalf("create f5 %+v %v", f5, err)
	}
	// 删掉这条厂级工艺。
	mustDeleteAsset(t, ctx, fac, sa, f5.ID)
	// 用原身份带原 F 号重建，本厂号段应当认。
	again, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "厂焊5重建", body, f5.ID, f5.Code, nil)
	// 失败或号不对就说明本厂原号被误拦。
	if err != nil || again.Code != f5.Code {
		// 把重建的条目打出来并停住。
		t.Fatalf("rebuild factory code %+v %v", again, err)
	}
}

// mustDeleteAsset 删一条本厂原件，删不掉就停住。
func mustDeleteAsset(t *testing.T, ctx context.Context, fac *factory.Service, tok string, assetID uuid.UUID) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 删除失败就停，后面的重建没有前提。
	if err := fac.DeleteAsset(ctx, tok, assetID); err != nil {
		// 把删除的错误打出来并停住。
		t.Fatalf("delete %s: %v", assetID, err)
	}
}
