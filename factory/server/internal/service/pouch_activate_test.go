// C3：按 cache_scope 与上限激活恰好一份；processId 解工艺；缺成员/串版/只拷/超上限/焊接中拒绝。
package service_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 按范围激活恰好一份，并按工艺编号解开工艺。
func TestPouchActivateAndOpenProcess(t *testing.T) {
	// 装好已登录工艺包，失败则激活用例没有包。
	p, client, _, _ := loggedPouch(t)
	// 准备这段正文字节，读回对不上说明没写进去。
	procBody := []byte(`{"current":180}`)
	// 组装工艺成员，字段缺了激活会对不上。
	proc := processMember("工艺", procBody)
	// 组装工程快照，缺依赖则范围里看不到工程。
	snap := projectSnap(client, "工程", proc)
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(snap); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := p.Activate(snap.AssetID); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 打开工艺正文，失败说明包没激活或对不上。
	got, err := p.OpenProcess(proc.ID)
	// 开工艺失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, procBody) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("process %s %v", got, err)
	}
	// 开工艺应因找不到被拒绝，放行说明没拦住。
	if _, err := p.OpenProcess(snap.AssetID); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project as process: %v", err)
	}
}

// 缺成员、串修订、只拷、超上限和焊接中都要拒绝。
func TestPouchActivateRejectsIncompleteMismatchCopyFullWeldOffline(t *testing.T) {
	// 装好已登录工艺包，失败则激活用例没有包。
	p, client, _, _ := loggedPouch(t)
	// 组装工艺成员，字段缺了激活会对不上。
	proc := processMember("工艺", []byte(`{"a":1}`))
	// 组装工程快照，缺依赖则范围里看不到工程。
	good := projectSnap(client, "工程", proc)
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(good); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 复制一份完整包再抠掉成员，用来验证不完整被拒。
	missing := good
	// 只留根成员，用来模拟组包被拆残。
	root := missing.Members[0]
	// 把成员收成残包，完整包仍能激活说明没校验。
	missing.Members = []factory.ClosureMember{root}
	// 计算组包摘要，对不上说明成员被改过。
	missing.Digest = digest.ClosureSum([]digest.Member{{
		ID: root.ID, Revision: root.Revision, Digest: root.Digest, Content: root.Content,
	}})
	// 缓存组包应因组包不完整被拒绝，放行说明没拦住。
	if err := p.CacheClosure(missing); !errors.Is(err, domain.ErrClosureIncomplete) {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatalf("missing: %v", err)
	}

	// 组装工程快照，缺依赖则范围里看不到工程。
	mismatch := projectSnap(client, "串版", processMember("工艺", []byte(`{"a":1}`)))
	// 把依赖修订改错，仍能激活说明没核对修订。
	mismatch.Members[0].Deps[0].Revision = 99
	// 缓存组包应因组包对不上被拒绝，放行说明没拦住。
	if err := p.CacheClosure(mismatch); !errors.Is(err, domain.ErrClosureMismatch) {
		// 两边结果对不上，说明匹配或引用写偏了。
		t.Fatalf("mismatch: %v", err)
	}

	// 组装工程快照，缺依赖则范围里看不到工程。
	tampered := projectSnap(client, "摘要", processMember("工艺", []byte(`{"a":1}`)))
	// 准备这段正文字节，读回对不上说明没写进去。
	tampered.Members[0].Content = []byte("tampered")
	// 缓存组包应因完整性失败被拒绝，放行说明没拦住。
	if err := p.CacheClosure(tampered); !errors.Is(err, domain.ErrIntegrity) {
		// 完整性没有通过，说明摘要或租约已经坏了。
		t.Fatalf("integrity: %v", err)
	}

	// 新取一个编号，撞号会让两条记录分不清。
	other := id.New()
	// 组装工程快照，缺依赖则范围里看不到工程。
	copied := projectSnap(other, "只拷", processMember("他机", []byte(`{"b":1}`)))
	// 缓存组包应因越权被拒绝，放行说明没拦住。
	if err := p.CacheClosure(copied); !errors.Is(err, domain.ErrForbidden) {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("copy: %v", err)
	}

	// 组装工程快照，缺依赖则范围里看不到工程。
	second := projectSnap(client, "第二份", processMember("工艺2", []byte(`{"c":1}`)))
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(second); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := p.Activate(good.AssetID); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 标记是否正在焊接，失败说明状态不允许。
	p.SetWelding(true)
	// 开通应因越权被拒绝，放行说明没拦住。
	if err := p.Activate(second.AssetID); !errors.Is(err, domain.ErrForbidden) {
		// 作业类型和预期不一致，说明类型没有保住。
		t.Fatalf("weld switch: %v", err)
	}
	// 当前激活应是这份工程，空或串了说明没按范围激活。
	if p.ActiveProject() == nil || *p.ActiveProject() != good.AssetID {
		// 作业类型和预期不一致，说明类型没有保住。
		t.Fatal("active moved while welding")
	}
	// 标记是否正在焊接，失败说明状态不允许。
	p.SetWelding(false)

	// 把策略写进包里，失败说明字段不合法。
	p.SetPolicy(2, store.CacheScopeCurrent)
	// 切换在线或离线，失败说明状态不接受这次切换。
	p.SetOnline(false)
	// 开通应因找不到被拒绝，放行说明没拦住。
	if err := p.Activate(id.New()); !errors.Is(err, domain.ErrNotFound) {
		// 离线判定和预期相反，说明授权或时钟没看对。
		t.Fatalf("offline current: %v", err)
	}
}

// 当前范围只留这一份工程，别的工程还在就算没裁。
func TestPouchCurrentScopeDropsOtherProjects(t *testing.T) {
	// 装好已登录工艺包，失败则激活用例没有包。
	p, client, _, _ := loggedPouch(t)
	// 把策略写进包里，失败说明字段不合法。
	p.SetPolicy(2, store.CacheScopeCurrent)
	// 组装工艺成员，字段缺了激活会对不上。
	aProc := processMember("A工艺", []byte(`{"a":1}`))
	// 组装工艺成员，字段缺了激活会对不上。
	bProc := processMember("B工艺", []byte(`{"b":1}`))
	// 组装工程快照，缺依赖则范围里看不到工程。
	a := projectSnap(client, "A", aProc)
	// 组装工程快照，缺依赖则范围里看不到工程。
	b := projectSnap(client, "B", bProc)
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(a); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(b); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := p.Activate(b.AssetID); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按范围裁过之后不应再留这份，还在说明没丢掉。
	if p.HasClosure(a.AssetID) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("current kept other project")
	}
	// 开袋内稿应因找不到被拒绝，放行说明没拦住。
	if _, err := p.Open(aProc.ID); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("dropped member: %v", err)
	}
	// 打开工艺正文，失败说明包没激活或对不上。
	got, err := p.OpenProcess(bProc.ID)
	// 开工艺失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, []byte(`{"b":1}`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("kept %s %v", got, err)
	}
}

// 个人工艺包只跟所有者走，他人能激活说明没绑人。
func TestPouchActivatePersonalFollowsOwner(t *testing.T) {
	// 装好已登录工艺包，失败则激活用例没有包。
	p, client, who, key := loggedPouch(t)
	// 组装工艺成员，字段缺了激活会对不上。
	proc := processMember("工艺", []byte(`{"p":1}`))
	// 组装工程快照，缺依赖则范围里看不到工程。
	snap := projectSnap(client, "我的工程", proc)
	// 改成个人级别，厂级规则仍收下说明级别没生效。
	snap.Level = store.AssetLevelPersonal
	// 成员也改成个人级，级别不一致应被拒绝。
	snap.Members[0].Level = store.AssetLevelPersonal
	// 写上所有者，他人能打开说明个人稿没跟主人。
	snap.Members[0].CreatorID = &who
	// 缓存组包失败就停，否则后面没有可靠结果。
	if err := p.CacheClosure(snap); err != nil {
		// 把缓存组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 退出本机袋，之后钥匙和明文都不应还在。
	p.Logout()
	// 新取一个编号，撞号会让两条记录分不清。
	other := id.New()
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, other, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把工艺包绑到这台客户端，失败说明已绑别人。
	p.BindClient(client)
	// 开通应因找不到被拒绝，放行说明没拦住。
	if err := p.Activate(snap.AssetID); !errors.Is(err, domain.ErrNotFound) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %v", err)
	}
}

// 准备一只已登录的工艺包，登录失败则无法继续。
func loggedPouch(t *testing.T) (*factory.Pouch, uuid.UUID, uuid.UUID, []byte) {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 建立一只空的本地工艺包，失败则无法装包。
	p := factory.NewPouch()
	// 生成正文密钥，失败则工艺包无法加密。
	key, err := contentcrypt.RandomKey()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	who := id.New()
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, who, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	client := id.New()
	// 把工艺包绑到这台客户端，失败说明已绑别人。
	p.BindClient(client)
	return p, client, who, key
}

// 按名称和正文装一条工艺成员，缺字段会对不上。
func processMember(name string, body []byte) factory.ClosureMember {
	return factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelFactory, Name: name,
		Status: factory.AssetAvailable, Revision: 1, Content: body, Digest: digest.Sum(body),
	}
}

// 把若干工艺收成一份工程快照，缺依赖会是空包。
func projectSnap(client uuid.UUID, name string, procs ...factory.ClosureMember) factory.ClosureSnapshot {
	// 按条数划出切片，短了会把依赖或成员漏掉。
	deps := make([]factory.AssetDep, len(procs))
	// 逐条检查这一批结果，任一条偏离即判失败。
	for i, p := range procs {
		// 填上这条工艺依赖，漏了工程会引用空工艺。
		deps[i] = factory.AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte(`{"items":[]}`)
	// 组装工程根成员，缺依赖则快照不是完整工程。
	root := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelFactory, Name: name,
		Status: factory.AssetAvailable, Revision: 1, Content: body, Digest: digest.Sum(body), Deps: deps,
	}
	// 留下客户端编号，封包时要能对上是哪一台。
	cid := client
	// 交回封好的快照，封不上则调用方没有包可用。
	return sealSnap(factory.KindProject, root, procs, nil, &cid)
}
