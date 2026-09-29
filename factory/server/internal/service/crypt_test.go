// 厂库正文必须是 WM2；租约到期后读拒绝且密文仍在；他厂信封解不开。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 验收落库是信封，过期后读拒绝且密文不变。
func TestContentLeaseAndEnvelope(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊", []byte(`{"a":1}`))
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读库内治理密文，供后面步骤使用，失败则前提断了。
	raw, err := fac.Store().RawGovernedContent(ctx, row.ID)
	// 刚写入的库内字节必须是信封，不能还是明文。
	if err != nil || !contentcrypt.IsEnvelope(raw) {
		// 不是信封就停，落库正文没有封上。
		t.Fatalf("want WM2 got %q %v", raw, err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	// 读回的正文必须和刚写入的一致，否则封装坏了。
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		// 读回对不上就停，信封或租约把正文解错了。
		t.Fatalf("read %q %v", body, err)
	}

	// 清掉内存里的内容租约，随后访问应被到期拦住。
	fac.Store().ClearContentLease()
	// 租约清掉后再读正文必须因到期被拒绝。
	if _, err := fac.ReadAssetContent(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 到期还能读就停，租约没有挡住读取。
		t.Fatalf("expired read %v", err)
	}
	// 读库内治理密文，供后面步骤使用，失败则前提断了。
	raw2, err := fac.Store().RawGovernedContent(ctx, row.ID)
	// 到期之后库内密文必须和清租约前一样。
	if err != nil || !bytes.Equal(raw, raw2) {
		// 密文变了就停，到期不该改写落盘内容。
		t.Fatalf("ciphertext changed after expire")
	}

	// 开通本厂，供后面步骤使用，失败则前提断了。
	other, facB, err := h.Provision(ctx, "sb", "超管B")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := facB.Activate(ctx, "sb", other.ActivationToken, "sb-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tokB := mustLogin(t, ctx, facB, "sb", "sb-pass")
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	rowB, err := facB.CreateFactoryProcess(ctx, tokB, store.WorkContext{Direct: true}, "焊B", []byte(`{"b":1}`))
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 改写库内正文没成功，后面的断言就没有依据。
	if err := facB.Store().TamperAssetContent(ctx, rowB.ID, raw); err != nil {
		// 改写库内正文失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 他厂拿这段密文来读必须因完整性被拒绝。
	if _, err := facB.ReadAssetContent(ctx, tokB, rowB.ID); !errors.Is(err, domain.ErrIntegrity) {
		// 他厂能解开就停，信封没有绑在本厂。
		t.Fatalf("cross factory %v", err)
	}

	// 另造一把密钥，供后面步骤使用，失败则前提断了。
	l, err := contentcrypt.RandomKey()
	// 另造一把密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 另造一把密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 装上已经过期的租约必须因到期被拒绝。
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(-time.Minute)); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 过期租约能装上就停，时间窗口没有校验。
		t.Fatalf("past lease %v", err)
	}
}

// 验收无租约时读写改名都拒绝，续租后正文还在。
func TestContentLeaseMetaAndRestore(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 读取在途密钥，供后面步骤使用，失败则前提断了。
	l, err := fac.Store().TransitKey()
	// 读取在途密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 读取在途密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊", []byte(`{"a":1}`))
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 清掉内存里的内容租约，随后访问应被到期拦住。
	fac.Store().ClearContentLease()
	// 租约清掉后读元数据必须因到期被拒绝。
	if _, err := fac.GetAsset(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 到期还能读元数据就停，读路径没有守住。
		t.Fatalf("get after expire %v", err)
	}
	// 租约清掉后再读正文必须因到期被拒绝。
	if _, err := fac.ReadAssetContent(ctx, tok, row.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 到期还能读正文就停，租约没有挡住。
		t.Fatalf("read %v", err)
	}
	// 没有租约时新建工艺必须因到期被拒绝。
	if _, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "焊2", []byte(`{"b":1}`)); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("create without lease %v", err)
	}
	// 没有租约时给资产改名必须因到期被拒绝。
	if _, err := fac.RenameAsset(ctx, tok, row.ID, row.Revision, "焊改"); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 无租约还能改名就停，改名没有被挡住。
		t.Fatalf("rename without lease %v", err)
	}
	// 装上内容租约没成功，后面的断言就没有依据。
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 装上内容租约失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	// 租约装回去之后，正文必须仍是原来那一份。
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		// 续租后正文变了就停，主密钥没有保住。
		t.Fatalf("restore %q %v", body, err)
	}
	// 另造一把密钥，供后面步骤使用，失败则前提断了。
	wrong, err := contentcrypt.RandomKey()
	// 另造一把密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 另造一把密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 用另一把密钥换租约必须成功，并重包主密钥。
	if err := fac.Store().ApplyContentLease(ctx, wrong, time.Now().Add(time.Hour)); err != nil {
		// 换钥失败就停，活着的主密钥没有重包。
		t.Fatalf("wrong L must rewrap live mk: %v", err)
	}
	// 读取在途密钥，供后面步骤使用，失败则前提断了。
	gotL, err := fac.Store().TransitKey()
	// 换钥之后当前在途密钥必须变成新的那把。
	if err != nil || !bytes.Equal(gotL, wrong) {
		// 密钥没有换上就停，轮换没有生效。
		t.Fatalf("rotated L %v", err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err = fac.ReadAssetContent(ctx, tok, row.ID)
	// 换了在途密钥之后，正文必须仍能按原样解开。
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		// 换钥后正文解不开就停，主密钥被换丢了。
		t.Fatalf("kept mk %q %v", body, err)
	}
	// 清掉内存里的内容租约，随后访问应被到期拦住。
	fac.Store().ClearContentLease()
	// 清掉后再装回这把已轮换的密钥必须成功。
	if err := fac.Store().ApplyContentLease(ctx, wrong, time.Now().Add(time.Hour)); err != nil {
		// 轮换后的密钥装不回去就停，租约恢复失败。
		t.Fatalf("restore rotated L %v", err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err = fac.ReadAssetContent(ctx, tok, row.ID)
	// 轮换后的密钥装回后，正文必须仍是原件。
	if err != nil || !bytes.Equal(body, []byte(`{"a":1}`)) {
		// 装回后正文变了就停，轮换把内容弄坏了。
		t.Fatalf("after rotate restore %q %v", body, err)
	}
}

// 验收旧明文在装上租约后被封成信封且能读回。
func TestLegacyPlainSealedOnLease(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 准备明文样本，封存之后盘上不应再看见原文。
	plain := []byte(`{"legacy":1}`)
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{Direct: true}, "旧明文", plain)
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 改写库内正文没成功，后面的断言就没有依据。
	if err := fac.Store().TamperAssetContent(ctx, row.ID, plain); err != nil {
		// 改写库内正文失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读取在途密钥，供后面步骤使用，失败则前提断了。
	l, err := fac.Store().TransitKey()
	// 读取在途密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 读取在途密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 装上内容租约没成功，后面的断言就没有依据。
	if err := fac.Store().ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		// 装上内容租约失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读库内治理密文，供后面步骤使用，失败则前提断了。
	raw, err := fac.Store().RawGovernedContent(ctx, row.ID)
	// 装上租约后旧明文必须变成信封，且不含原文。
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		// 仍是明文或正文露在外面就停，没有封上。
		t.Fatalf("want sealed got %q %v", raw, err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err := fac.ReadAssetContent(ctx, tok, row.ID)
	// 封上之后读回的正文必须还是原来的明文。
	if err != nil || !bytes.Equal(body, plain) {
		// 封存后读不回原文就停，封存把内容弄坏了。
		t.Fatalf("read after seal %q %v", body, err)
	}
}

// 验证明文闭包收下后落盘成信封，并能读回原文。
func TestAcceptLegacyPlainClosure(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 记下本厂标识，这条明文闭包要下发到这家厂。
	fid := seed.ID
	// 准备明文样本，封存之后盘上不应再看见原文。
	plain := []byte(`{"plat":1}`)
	// 拼一条明文的平台成员，收下之后应当被封上。
	m := factory.ClosureMember{
		ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "旧闭包", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: plain, Digest: digest.Sum(plain),
	}
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读副本落盘字节，供后面步骤使用，失败则前提断了。
	raw, err := fac.Store().RawReplicaContent(ctx, m.ID, 1)
	// 明文闭包收下后，落盘必须是信封且不含原文。
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		// 落盘仍是明文就停，收下的时候没有封上。
		t.Fatalf("legacy closure disk %q %v", raw, err)
	}
	// 读资产元数据，供后面步骤使用，失败则前提断了。
	got, err := fac.GetAsset(ctx, tok, m.ID)
	// 收下后必须能读到名称是旧闭包的那条元数据。
	if err != nil || got.Name != "旧闭包" {
		// 名称不对或读失败就停，闭包没有入库。
		t.Fatalf("get replica %v %+v", err, got)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	body, err := fac.ReadAssetContent(ctx, tok, m.ID)
	// 收下后读回的正文必须还是下发时的明文。
	if err != nil || !bytes.Equal(body, plain) {
		// 读回不是原文就停，收下之后解不开。
		t.Fatalf("read replica %q %v", body, err)
	}
}

// 验证明文和在途密文混在同一闭包里都能解开。
func TestAcceptMixedTransitClosure(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 记下本厂标识，这份混合闭包要下发到这家厂。
	fid := seed.ID
	// 准备明文样本，封存之后盘上不应再看见原文。
	plain := []byte(`{"plain":1}`)
	// 准备即将用在途密钥封起来的那段正文。
	envBody := []byte(`{"env":1}`)
	// 准备工程自己的正文，和两个成员分开核对。
	projBody := []byte(`{"proj":1}`)
	// 读取在途密钥，供后面步骤使用，失败则前提断了。
	l, err := fac.Store().TransitKey()
	// 读取在途密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 读取在途密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 拼一条明文成员，收下后磁盘上不应再是原文。
	plainM := factory.ClosureMember{
		ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "明文成员", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: plain, Digest: digest.Sum(plain),
	}
	// 拼一条即将先被在途密钥封上的成员。
	envM := factory.ClosureMember{
		ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform,
		Name: "密文成员", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: envBody, Digest: digest.Sum(envBody),
	}
	// 拼工程成员，并钉住明文和密文那两条依赖。
	proj := factory.ClosureMember{
		ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), Kind: factory.KindProject, Level: factory.AssetLevelPlatform,
		Name: "混合闭包", Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: projBody, Digest: digest.Sum(projBody),
		Deps: []factory.AssetDep{
			{ID: plainM.ID, Revision: 1, Digest: plainM.Digest},
			{ID: envM.ID, Revision: 1, Digest: envM.Digest},
		},
	}
	// 把成员封成平台下发快照，供本厂收下。
	snap := sealSnap(factory.KindProject, proj, []factory.ClosureMember{plainM, envM}, &fid, nil)
	// 封成在途信封，供后面步骤使用，失败则前提断了。
	env, err := contentcrypt.Seal(l, envBody, contentcrypt.TransitAAD(fid, envM.ID, 1))
	// 封成在途信封没成功，后面的断言就没有依据。
	if err != nil {
		// 封成在途信封失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把工程那一格换成已经封好的在途密文。
	snap.Members[2].Content = env
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, snap); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 逐个成员核对落盘字节是不是已经封成信封。
	for _, id := range []uuid.UUID{plainM.ID, envM.ID, proj.ID} {
		raw, err := fac.Store().RawReplicaContent(ctx, id, 1)
		if err != nil || !contentcrypt.IsEnvelope(raw) {
			// 有成员不是信封就停，混在一起的包没有封全。
			t.Fatalf("disk %s %q %v", id, raw, err)
		}
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	gotPlain, err := fac.ReadAssetContent(ctx, tok, plainM.ID)
	// 明文成员收下后必须能读回原来的正文。
	if err != nil || !bytes.Equal(gotPlain, plain) {
		// 明文成员读不对就停，封存把原文弄坏了。
		t.Fatalf("plain member %q %v", gotPlain, err)
	}
	// 读出资产正文，供后面步骤使用，失败则前提断了。
	gotEnv, err := fac.ReadAssetContent(ctx, tok, envM.ID)
	// 在途密文成员必须能解回原来的那段正文。
	if err != nil || !bytes.Equal(gotEnv, envBody) {
		// 密文成员读不对就停，在途信封没有解开。
		t.Fatalf("env member %q %v", gotEnv, err)
	}
}

// 验收副本写入即封装，无租约不能读也不能升修订。
func TestInsertReplicaSealsAndNeedsLease(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 解析写死的标识，格式不对这条会直接中断。
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	// 准备明文样本，封存之后盘上不应再看见原文。
	plain := []byte(`{"current":1}`)
	// 按正文计算摘要，依赖和收包都要拿它对上。
	sum := digest.Sum(plain)
	// 按当前明文拼第一修订，写入的时候应当被封上。
	in := store.AssetReplica{
		ID: id, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: true, Content: plain, Digest: sum,
	}
	// 写入平台副本没成功，后面的断言就没有依据。
	if _, err := fac.Store().InsertReplica(ctx, in); err != nil {
		// 写入平台副本失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读副本落盘字节，供后面步骤使用，失败则前提断了。
	raw, err := fac.Store().RawReplicaContent(ctx, id, 1)
	// 副本写入后落盘必须是信封，且不再含明文。
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, plain) {
		// 落盘仍见明文就停，写入的时候没有封装。
		t.Fatalf("replica disk %q %v", raw, err)
	}
	// 不读正文时，副本元数据也必须能够读到。
	if _, err := fac.Store().LatestReplicaMeta(ctx, id); err != nil {
		// 元数据读失败就停，名录被租约误伤了。
		t.Fatalf("replica meta %v", err)
	}
	// 清掉内存里的内容租约，随后访问应被到期拦住。
	fac.Store().ClearContentLease()
	// 租约清掉后读副本正文必须因到期被拒绝。
	if _, err := fac.Store().LatestReplica(ctx, id); !errors.Is(err, domain.ErrContentLeaseExpired) {
		// 无租约还能读副本就停，正文没有被守住。
		t.Fatalf("sealed replica readable without lease: %v", err)
	}
	// 同一修订在无租约时重放必须仍然成功。
	if _, err := fac.Store().InsertReplica(ctx, in); err != nil {
		// 同修订重放失败就停，幂等被租约卡住了。
		t.Fatalf("same revision replay without lease %v", err)
	}
	// 无租约时写入下一修订必须因到期被拒绝。
	if _, err := fac.Store().InsertReplica(ctx, store.AssetReplica{
		// 填第二修订的副本，用来试没有租约不能升版。
		ID: id, Revision: 2, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: true, Content: plain, Digest: sum,
	}); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("insert without lease %v", err)
	}
}
