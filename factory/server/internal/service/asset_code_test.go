// 工艺工程只读编号：本厂发号、副本沿用原号、另存/升档换新号。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 本厂发号、副本沿用原号，另存和升档要换新号。
func TestFactoryAssetCodes(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	peA := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	p1, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊1", []byte(`{"name":"p","current":180}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if p1.Code != "GY-F01-000001" || !assetcode.Valid(p1.Code) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("factory process %s", p1.Code)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	j1, err := facA.CreateFactoryProject(ctx, peA.tok, direct, "工程1", []byte(`[{"name":"焊道"}]`), nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if j1.Code != "GC-F01-000001" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("factory project %s", j1.Code)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	p2, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊2", []byte(`{"name":"p2","current":181}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if p2.Code != "GY-F01-000002" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("second process %s", p2.Code)
	}

	// 修改资产名称，失败说明修订冲突或越权。
	renamed, err := facA.RenameAsset(ctx, peA.tok, p1.ID, p1.Revision, "改名")
	// 改名称失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改名称的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if renamed.Code != p1.Code || renamed.Revision != p1.Revision+1 {
		// 名称没有改成预期，说明这次改名没生效。
		t.Fatalf("rename %+v", renamed)
	}

	// 复制出一份工艺，失败说明源稿不可复制或越权。
	copied, err := facA.CopyProcess(ctx, peA.tok, p1.ID, "另存")
	// 复制工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把复制工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 编号应带本厂前缀，对不上说明发号串了厂。
	if copied.ID == p1.ID || copied.Code == p1.Code || !strings.HasPrefix(copied.Code, "GY-F01-") {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("copy %+v", copied)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	src, err := facA.GetAsset(ctx, peA.tok, p1.ID)
	// 读台账失败或编号不对就停，说明没达预期。
	if err != nil || src.Code != p1.Code {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("src after copy %+v %v", src, err)
	}

	// 新建一份个人工艺，失败说明个人入口被拒。
	pers, err := facA.CreatePersonalProcess(ctx, peA.tok, direct, "个人", []byte(`{"name":"mine","current":170}`))
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	pers, err = facA.PublishAsset(ctx, peA.tok, pers.ID, pers.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把资产升到厂级，失败说明状态不允许升。
	promoted, err := facA.PromoteToFactory(ctx, peA.tok, pers.ID)
	// 升厂级失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把升厂级的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if promoted.ID == pers.ID || promoted.Code == pers.Code || promoted.Level != factory.AssetLevelFactory {
		// 升档后的级别不对，说明没有升成厂级。
		t.Fatalf("promote %+v from %s", promoted, pers.Code)
	}
	// 把资产升到厂级，失败说明状态不允许升。
	again, err := facA.PromoteToFactory(ctx, peA.tok, pers.ID)
	// 升厂级失败或编号不对就停，说明没达预期。
	if err != nil || again.ID != promoted.ID || again.Code != promoted.Code {
		// 升档后的级别不对，说明没有升成厂级。
		t.Fatalf("re-promote %+v %v", again, err)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	stillPers, err := facA.GetAsset(ctx, peA.tok, pers.ID)
	// 读台账失败或编号不对就停，说明没达预期。
	if err != nil || stillPers.Code != pers.Code {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal after promote %+v %v", stillPers, err)
	}

	// 写入受管资产，失败说明编号或来源冲突。
	ingested, err := facA.Store().InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelPersonal, Name: "现场汇聚",
		Status: store.AssetDraft, Copyable: true, Content: []byte("c-body"), Digest: digest.Sum([]byte("c-body")),
		CreatorID: seedA.SuperAdminID, Code: "GY-C0008-000012",
	})
	// 写受管资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把写受管资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if ingested.Code != "GY-C0008-000012" || ingested.ID == p1.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("ingest %+v", ingested)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	p3, err := facA.CreateFactoryProcess(ctx, peA.tok, direct, "焊3", []byte(`{"name":"p3","current":182}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if p3.Code != "GY-F01-000006" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("seq after ingest %s", p3.Code)
	}

	// 准备这段正文字节，读回对不上说明没写进去。
	platBody := []byte("plat-body")
	// 组装一条组包成员，字段错了后面会对不上。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台焊",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := seedA.ID
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	gotRep, err := facA.GetAsset(ctx, peA.tok, m.ID)
	// 读台账失败或编号不对就停，说明没达预期。
	if err != nil || gotRep.Code != "GY-W-000001" {
		// 副本没有按预期同步，说明下发没有写进厂。
		t.Fatalf("replica %+v %v", gotRep, err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if gotRep.Code == p1.Code {
		// 副本没有按预期同步，说明下发没有写进厂。
		t.Fatal("replica collided with local")
	}

	// 再组装一条重复成员，用来验证冲突会被拒绝。
	dup := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "撞号",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	// 收平台稿应因编号冲突被拒绝，放行说明没拦住。
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, dup, nil, &fid, nil)); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("same code other id: %v", err)
	}
	// 复制一份成员准备改坏，用来验证会拒绝。
	mismatch := m
	// 改成冲突编号再提交，应收下编号冲突的拒绝。
	mismatch.Code = "GY-W-000002"
	// 把修订改掉，用来验证对不上修订会被拒绝。
	mismatch.Revision = 2
	// 收平台稿应因编号冲突被拒绝，放行说明没拦住。
	if err := facA.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, mismatch, nil, &fid, nil)); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("same id other code: %v", err)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	keep, err := facA.GetAsset(ctx, peA.tok, m.ID)
	// 读台账失败或编号不对就停，说明没达预期。
	if err != nil || keep.Code != "GY-W-000001" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("original after conflict %+v %v", keep, err)
	}

	// 建厂并签发超管激活码，失败则没有厂可测。
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sb-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saB := mustLogin(t, ctx, facB, "sa-b", "sb-pass")
	// 新建一份厂级工艺，失败说明起草入口坏了。
	pb, err := facB.CreateFactoryProcess(ctx, saB, direct, "乙厂焊", []byte(`{"name":"b","current":160}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if pb.Code != "GY-F02-000001" || pb.Code == p1.Code {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("other factory %s vs %s", pb.Code, p1.Code)
	}
	// 资产编号应是预期这一个，对不上说明发号偏了。
	if strings.Contains(p1.Code, "-W-") || !strings.Contains(p1.Code, "-F01-") {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("want factory origin %s", p1.Code)
	}

	// 发布当前修订，失败说明状态不允许发布。
	pub, err := facA.PublishAsset(ctx, peA.tok, p1.ID, renamed.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 装上这条依赖，修订或摘要错了工程会对不上。
	dep := factory.AssetDep{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}
	// 建工程应因依赖不齐被拒绝，放行说明没拦住。
	if _, err := facA.CreateFactoryProject(ctx, peA.tok, direct, "编号当引用", []byte(`[{"processId":"`+p1.Code+`"}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("code as processId: %v", err)
	}

	// 列出可见资产，失败说明会话或范围不对。
	rows, err := facA.ListAssets(ctx, peA.tok, "process")
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 从零开始数条数，少计会把遗漏误判成通过。
	n := 0
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, row := range rows {
		// 初始化后应只有一名超管，人数不对说明建厂偏了。
		if row.Code == p1.Code {
			// 条数加一，用来核对列表有没有把这条算上。
			n++
		}
	}
	// 初始化后应只有一名超管，人数不对说明建厂偏了。
	if n != 1 {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("list by code %d", n)
	}
	// 按编号找回受管资产，找不到说明编号没落上。
	byCode, err := facA.Store().GovernedAssetByCode(ctx, p1.Code)
	// 按号查找失败或结果不符就停，说明没达预期。
	if err != nil || byCode.ID != p1.ID {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("by code %+v %v", byCode, err)
	}
	// 按号查找应因找不到被拒绝，放行说明没拦住。
	if _, err := facA.Store().GovernedAssetByCode(ctx, "GY-F01-999999"); !errors.Is(err, domain.ErrNotFound) {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatalf("missing: %v", err)
	}
}

// 缺少工厂短码时拒绝发号，避免编出空的资产号。
func TestFactoryAssetCodeMissing(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 打开数据库连接，失败说明测试库起不来。
	admin := testpg.Open(t)
	// 另开一座空库，失败说明管理库不允许建库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 新取一个编号，撞号会让两条记录分不清。
	facID := id.New()
	// 迁好库并装上厂服务，失败说明库还不能用。
	svc := factory.NewService(store.Open(testpg.OpenMigrated(t, dsn), facID))
	// 签发租约失败就停，否则后面没有可靠结果。
	if err := svc.Store().GrantLocalLease(ctx); err != nil {
		// 把签发租约的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写入首个超管，失败说明厂库还不能开张。
	_, token, err := svc.BootstrapInitial(ctx, "sa", "超管")
	// 写首个超管失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把写首个超管的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := svc.Activate(ctx, "sa", token, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	tok := mustLogin(t, ctx, svc, "sa", "sa-pass")
	// 建工艺应因缺少编号被拒绝，放行说明没拦住。
	if _, err := svc.CreateFactoryProcess(ctx, tok, factory.WorkContext{Direct: true}, "无短码", []byte(`{"name":"x","current":1}`)); !errors.Is(err, domain.ErrAssetCodeMissing) {
		t.Fatalf("missing origin: %v", err)
	}
}
