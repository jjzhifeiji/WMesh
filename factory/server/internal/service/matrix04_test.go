// 阶段4第4～7圈：厂内侧工程闭包 1.1～17.2。
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 本文件要跑完的闭包编号，漏跑就在收尾报错
var matrix04IDs = []string{
	"1.1", "1.2", "1.3",
	"2.1", "2.2", "2.3",
	"3.1", "3.2", "3.3",
	"4.1", "4.2", "4.3",
	"5.1", "5.4",
	"6.1", "6.2", "6.3",
	"7.1", "7.2", "7.3", "7.4", "7.5", "7.6",
	"8.1", "8.2", "8.3",
	"9.1", "9.2",
	"10.1", "10.2", "10.3",
	"11.1", "11.2", "11.3", "11.4",
	"12.1", "12.2", "12.3",
	"13.1", "13.2", "13.3",
	"14.1",
	"15.1",
	"16.1", "16.2",
	"17.1", "17.2",
}

// 跑工程闭包矩阵，漏编号就在收尾报错
func TestMatrix04(t *testing.T) {
	// 记下跑过的编号，收尾用来查有没有漏
	ran := map[string]bool{}
	// 按编号启动子测试并记账，漏跑收尾能发现
	run := func(id string, fn func(*testing.T)) {
		// 标成辅助函数，失败栈落到调用方
		t.Helper()
		// 把这一编号跑成独立子测试，失败只记这一条
		t.Run(id, func(t *testing.T) {
			// 标上这个编号已跑，漏标会被收尾抓住
			ran[id] = true
			// 跑这一编号的子测试，失败由它自己报出
			fn(t)
		})
	}
	// 收尾检查编号是否都跑过，漏跑就要报错
	t.Cleanup(func() {
		// 逐个核对应跑的编号，漏跑要在收尾报错
		for _, id := range matrix04IDs {
			// 这个编号没跑到就要报错，矩阵不算完成
			if !ran[id] {
				// 漏跑的编号要报出来，否则矩阵不算完成
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	// 跑组包段，失败说明成员或草稿没被拦住
	testClosurePack(t, run)
	// 跑下发段，失败说明越权或泄密没被拦住
	testClosureDistribute(t, run)
	// 跑激活段，失败说明不该激活的被放行
	testClosureActivate(t, run)
}

// 闭包矩阵共用夹具，装两厂服务、角色和时钟
type env04 struct {
	// 本用例的调用上下文，取消会打断夹具
	ctx context.Context
	// 厂A建厂结果，用来核对归属是不是这家
	seed Seeded
	// 厂A服务，闭包和账号操作都打在这里
	fac *factory.Service
	// 厂B建厂结果，用来做跨厂必须被拒的核对
	seedB Seeded
	// 厂B服务，他厂不该看见厂A的数据
	facB *factory.Service
	// 厂A超管令牌，授权和下发时拿它证明身份
	sa string
	// 厂B超管令牌，跨厂操作不该被它做成
	saB string
	// 厂A工艺工程师，负责组包和下发
	pe namedAcc
	// 另一名厂A工程师，用来对照权限是否串人
	pe2 namedAcc
	// 厂A操作员，负责本机激活，越权即失败
	op namedAcc
	// 厂A审计员，执行下发应当被拒绝
	aud namedAcc
	// 厂B工程师，读厂A工程应当被拒绝
	peB namedAcc
	// 车间工程师，下发厂级工程应当被拒绝
	peShop namedAcc
	// 车间组织，用来把权限限制在本车间
	shop factory.OrgUnit
	// 直属厂的工作上下文，不挂任何车间
	direct factory.WorkContext
	// 当前时刻，用来签发还没过期的凭证
	now time.Time
	// 服务器与本机时钟，激活时拿来核对有效期
	clocks factory.Clocks
	// 凭证的生效和失效时刻，过期用来拒绝激活
	nb, na time.Time
	// 工程正文，审计里出现它就算泄密失败
	secret []byte
}

// 准备两厂人员、组织和时钟，缺一项则夹具失败
func newEnv04(t *testing.T) *env04 {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := facA.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	// 建厂并种初始超管，失败则本步验收不能继续
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sa-b-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	// 装上两厂服务和角色，缺一项则闭包夹具不完整
	e := &env04{
		ctx: ctx, seed: seed, fac: facA, seedB: seedB, facB: facB, sa: saA, saB: saB,
		pe:     mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil),
		pe2:    mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleOperator, factory.ScopeFactory, nil),
		op:     mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil),
		aud:    mustCreateRole(t, ctx, facA, saA, "aud-a", "aud-pass", factory.RoleAuditor, factory.ScopeFactory, nil),
		peB:    mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleOperator, factory.ScopeFactory, nil),
		direct: factory.WorkContext{Direct: true},
		secret: []byte("closure-body-secret"),
	}
	// 建立组织，失败则本步验收不能继续
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	// 建立组织失败就停，避免带着错误继续验
	if err != nil {
		// 建立组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 建立组织，失败则本步验收不能继续
	shop, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	// 建立组织失败就停，避免带着错误继续验
	if err != nil {
		// 建立组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 记下车间，后面按车间范围做越权验证
	e.shop = shop
	// 建人并授角色，失败则夹具缺这个身份
	e.peShop = mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	// 分配到组织失败就停，避免带着错误继续验
	if err := facA.Assign(ctx, saA, e.peShop.acc.ID, shop.ID); err != nil {
		// 分配到组织失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 取当前时刻，用来核对令牌或凭证是否过期
	e.now = time.Now().UTC()
	// 服务器和本机用同一时刻，避免时钟本身造成拒绝
	e.clocks = factory.Clocks{Server: e.now, Local: e.now}
	// 把时刻往后推，没成功则本步验收失败
	e.nb, e.na = e.now.Add(-time.Hour), e.now.Add(24*time.Hour)
	return e
}

// 起草并发布厂级工艺，失败则组包没有这条依赖
func (e *env04) pubProc(t *testing.T, name string, body []byte) factory.Asset {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 起草厂级工艺，失败则本步验收不能继续
	p, err := e.fac.CreateFactoryProcess(e.ctx, e.pe.tok, e.direct, name, body)
	// 起草厂级工艺失败就停，避免带着错误继续验
	if err != nil {
		// 起草厂级工艺失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 发布资产，失败则本步验收不能继续
	p, err = e.fac.PublishAsset(e.ctx, e.pe.tok, p.ID, p.Revision)
	// 发布资产失败就停，避免带着错误继续验
	if err != nil {
		// 发布资产失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	return p
}

// 起草并发布厂级工程，失败则没有可组的包
func (e *env04) pubProj(t *testing.T, name string, body []byte, deps []factory.AssetDep) factory.Asset {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 起草厂级工程，失败则本步验收不能继续
	p, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, name, body, deps)
	// 起草厂级工程失败就停，避免带着错误继续验
	if err != nil {
		// 起草厂级工程失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 发布资产，失败则本步验收不能继续
	p, err = e.fac.PublishAsset(e.ctx, e.pe.tok, p.ID, p.Revision)
	// 发布资产失败就停，避免带着错误继续验
	if err != nil {
		// 发布资产失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	return p
}

// 把工艺收成依赖清单，漏一条则组包会缺成员
func (e *env04) depsOf(procs ...factory.Asset) []factory.AssetDep {
	// 准备依赖切片，装不下则组包清单会缺
	out := make([]factory.AssetDep, 0, len(procs))
	// 逐条收成依赖，漏一条则组包会缺
	for _, p := range procs {
		// 把这条工艺收进依赖，漏收则组包会缺成员
		out = append(out, factory.AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest})
	}
	return out
}

// 绑定焊机并签发运行凭证，失败则袋不能激活
func (e *env04) bound(t *testing.T, personID uuid.UUID) (uuid.UUID, factory.Bag) {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 新开一个身份，避免和别的绑定撞号
	cid := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pub, priv, err := nodekey.Generate()
	// 生成焊机密钥失败就停，避免带着错误继续验
	if err != nil {
		// 生成焊机密钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := e.fac.AcceptBinding(e.ctx, cid, "焊机-1", pub, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 取厂签名公钥，失败则本步验收不能继续
	facPub, err := e.fac.SigningPublicKey(e.ctx)
	// 取厂签名公钥失败就停，避免带着错误继续验
	if err != nil {
		// 取厂签名公钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 签发运行凭证，失败则本步验收不能继续
	rt, err := e.fac.IssueRuntimeGrant(e.ctx, e.sa, cid, e.nb, e.na)
	// 签发运行凭证失败就停，避免带着错误继续验
	if err != nil {
		// 签发运行凭证失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 装好本机袋，缺密钥则签名验证会失败
	bag := bagOf(e.seed.ID, cid, pub, priv, facPub)
	// 标成已连厂网，激活时会回源核对状态
	bag.Connected = true
	// 把运行凭证装进本机袋，没装则不能激活
	bag.ApplyRuntime(rt)
	// 记下当前操作员，供本机袋绑定是谁在用
	oid := personID
	// 袋上绑上操作员，人错了激活应当被拒
	bag.OperatorID = &oid
	return cid, bag
}

// 按成员顺序算出闭包摘要，对不上则整包无效
func sealSnap(kind string, root factory.ClosureMember, rest []factory.ClosureMember, facID, clientID *uuid.UUID) factory.ClosureSnapshot {
	// 根成员放在闭包最前，顺序错则摘要失败
	members := append([]factory.ClosureMember{root}, rest...)
	// 按成员数准备摘要输入，长度不对则摘要失败
	parts := make([]digest.Member, len(members))
	// 逐个成员拼摘要，漏一个则整包对不上
	for i, m := range members {
		// 按成员拼摘要输入，漏字段则整包摘要会错
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	return factory.ClosureSnapshot{
		Kind: kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: facID, TargetClientID: clientID,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

// 验收组包、篡改和草稿，成员错或放行即失败
func testClosurePack(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备闭包夹具，失败则本段用例无法开始
	e := newEnv04(t)
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p1 := e.pubProc(t, "工艺1", []byte("p1"))
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p2 := e.pubProc(t, "工艺2", []byte("p2"))
	// 发布一条厂级工程，失败则没有可组的包
	proj := e.pubProj(t, "工程双依赖", e.secret, e.depsOf(p1, p2))

	// 留出闭包位，组包成功后给后面的下发用
	var snap factory.ClosureSnapshot
	// 验收双依赖组包含工程和两条工艺，缺成员即失败
	run("1.1", func(t *testing.T) {
		// 留出错误位，供这一步把失败写回来
		var err error
		// 组工程闭包，失败则本步验收不能继续
		snap, err = e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
		// 组工程闭包失败就停，避免带着错误继续验
		if err != nil {
			// 组工程闭包失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 闭包成员必须按序齐，缺或多即失败
		if snap.AssetID != proj.ID || len(snap.Members) != 3 || snap.Members[0].Kind != factory.KindProject ||
			snap.Members[1].ID != p1.ID || snap.Members[2].ID != p2.ID {
			// 闭包成员不符就把实际值打出并停掉本例
			t.Fatalf("%+v", snap)
		}
	})
	// 验收依赖缺失时组包被拒，放行即失败
	run("1.2", func(t *testing.T) {
		// 新开一个身份，避免和别的绑定撞号
		missing := factory.AssetDep{ID: id.New(), Revision: 1, Digest: p1.Digest}
		// 收成工程依赖失败就停，避免带着错误继续验
		if err := e.fac.Store().TamperAssetDeps(e.ctx, proj.ID, []factory.AssetDep{missing, e.depsOf(p2)[0]}); err != nil {
			t.Fatal(err)
		}
		// 期望组工程闭包被拒为闭包不完整，放行即失败
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID); !errors.Is(err, domain.ErrClosureIncomplete) {
			// 没拒成闭包不完整就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 收成工程依赖失败就停，避免带着错误继续验
		if err := e.fac.Store().TamperAssetDeps(e.ctx, proj.ID, e.depsOf(p1, p2)); err != nil {
			// 收成工程依赖失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收零依赖组包只含工程本身，多成员即失败
	run("1.3", func(t *testing.T) {
		// 发布一条厂级工程，失败则没有可组的包
		empty := e.pubProj(t, "空依赖", []byte("empty"), nil)
		// 组工程闭包，失败则本步验收不能继续
		got, err := e.fac.AssembleProject(e.ctx, e.pe.tok, empty.ID)
		// 组工程闭包失败就停，避免带着错误继续验
		if err != nil || len(got.Members) != 1 || got.Members[0].ID != empty.ID {
			// 闭包成员不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
	})

	// 绑定焊机并装袋，失败则不能下发或激活
	cid, bag := e.bound(t, e.pe.acc.ID)
	// 组工程闭包，失败则本步验收不能继续
	good, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
	// 组工程闭包失败就停，避免带着错误继续验
	if err != nil {
		// 组工程闭包失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 钉上目标焊机，下发和激活都按这台核对
	good.TargetClientID = &cid
	// 验收修订被顶替时拒绝缓存，收下即失败
	run("2.1", func(t *testing.T) {
		// 拷一份合格快照再改脏，避免弄坏原包
		bad := good
		// 拷一份成员再改，避免把合格包改脏
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		// 把成员修订改高，用来验串版必须被拒
		bad.Members[1].Revision++
		// 期望收下缓存闭包被拒为这几种原因之一，放行即失败
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrClosureMismatch) && !errors.Is(err, domain.ErrIntegrity) {
			// 没拒成这几种原因之一就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收身份被顶替时拒绝缓存，收下即失败
	run("2.2", func(t *testing.T) {
		// 拷一份合格快照再改脏，避免弄坏原包
		bad := good
		// 拷一份成员再改，避免把合格包改脏
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		// 新开一个身份，避免和别的绑定撞号
		bad.Members[1].ID = id.New()
		// 期望收下缓存闭包被拒为这几种原因之一，放行即失败
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrClosureMismatch) && !errors.Is(err, domain.ErrIntegrity) {
			// 没拒成这几种原因之一就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收正文被改脏时拒绝缓存，收下即失败
	run("2.3", func(t *testing.T) {
		// 拷一份合格快照再改脏，避免弄坏原包
		bad := good
		// 拷一份成员再改，避免把合格包改脏
		bad.Members = append([]factory.ClosureMember{}, good.Members...)
		// 把成员正文改脏，摘要对不上应当被拒
		bad.Members[1].Content = []byte("dirty")
		// 期望收下缓存闭包被拒为这几种原因之一，放行即失败
		if err := e.fac.AcceptCachedClosure(e.ctx, &bag, bad); !errors.Is(err, domain.ErrIntegrity) && !errors.Is(err, domain.ErrClosureMismatch) {
			// 没拒成这几种原因之一就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 验收工艺升修订后旧钉不能组包，放行即失败
	run("3.1", func(t *testing.T) {
		// 改正文并升修订失败就停，避免带着错误继续验
		if _, err := e.fac.UpdateAssetContent(e.ctx, e.pe.tok, p1.ID, p1.Revision, []byte("p1-v2")); err != nil {
			// 改正文并升修订失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望组工程闭包被拒为闭包不完整，放行即失败
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID); !errors.Is(err, domain.ErrClosureIncomplete) {
			// 没拒成闭包不完整就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收改钉新修订后组包跟上，仍停旧修订即失败
	run("3.2", func(t *testing.T) {
		// 读取资产，失败则本步验收不能继续
		p1b, err := e.fac.GetAsset(e.ctx, e.pe.tok, p1.ID)
		// 读取资产失败就停，避免带着错误继续验
		if err != nil {
			// 读取资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 收成工程依赖，漏一条则组包成员会缺
		proj, err = e.fac.SetProjectDeps(e.ctx, e.pe.tok, proj.ID, proj.Revision, e.depsOf(p1b, p2))
		// 收成工程依赖失败就停，避免带着错误继续验
		if err != nil {
			// 收成工程依赖失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 组工程闭包，失败则本步验收不能继续
		got, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
		// 组工程闭包失败就停，避免带着错误继续验
		if err != nil || got.Members[1].Revision != p1b.Revision {
			// 闭包成员不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
		// 记下新闭包，后面核对已下发的还是旧修订
		snap = got
	})
	// 验收源再升修订后已下发快照不漂，漂了即失败
	run("3.3", func(t *testing.T) {
		// 授权焊机接收工程失败就停，避免带着错误继续验
		if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			// 授权焊机接收工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 记下下发时的修订，源再升级后不该跟着漂
		oldRev := bag.Closures[0].Members[1].Revision
		// 改正文并升修订失败就停，避免带着错误继续验
		if _, err := e.fac.UpdateAssetContent(e.ctx, e.pe.tok, p2.ID, p2.Revision, []byte("p2-v2")); err != nil {
			// 改正文并升修订失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 先当作没找到缓存，找到了再改成有
		cached, ok := false, false
		// 逐份看缓存闭包，找不到目标即没下发
		for _, c := range bag.Closures {
			// 必须就是这份工程，指到别的身份即失败
			if c.AssetID == proj.ID {
				// 标上这份就是目标缓存，没标上即没下发
				cached, ok = true, true
				// 修订必须对上，串版或没跟上即失败
				if c.Members[2].Revision != oldRev && len(c.Members) > 2 {
					// members[1] is p1, members[2] is p2
				}
				// 修订必须对上，串版或没跟上即失败
				if c.Members[len(c.Members)-1].ID == p2.ID && c.Members[len(c.Members)-1].Revision != oldRev && c.Members[len(c.Members)-1].Revision != p2.Revision {
					// 已下发成员修订漂了就失败，旧快照不该变
					t.Fatalf("drift %+v", c.Members)
				}
				// 用一下找到标记，避免被当成没读而漏验
				_ = cached
			}
		}
		// 没找到目标就失败，说明这一步没生效
		if !ok {
			// 袋里没有这份缓存就失败，说明没下发到
			t.Fatal("missing cache")
		}
	})

	// 起草厂级工程，失败则本步验收不能继续
	draft, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "草稿工程", []byte("d"), nil)
	// 起草厂级工程失败就停，避免带着错误继续验
	if err != nil {
		// 起草厂级工程失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 验收草稿工程不能组包，组出来即失败
	run("4.1", func(t *testing.T) {
		// 期望组工程闭包被拒为资产不可用，放行即失败
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, draft.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			// 没拒成资产不可用就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 发布一条厂级工程，失败则没有可组的包
	avail := e.pubProj(t, "可停用", []byte("off"), nil)
	// 验收停用工程不能组包，组出来即失败
	run("4.2", func(t *testing.T) {
		// 停用资产，失败则本步验收不能继续
		off, err := e.fac.DisableAsset(e.ctx, e.pe.tok, avail.ID, avail.Revision)
		// 停用资产失败就停，避免带着错误继续验
		if err != nil {
			// 停用资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望组工程闭包被拒为资产不可用，放行即失败
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, off.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			// 没拒成资产不可用就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收草稿发布后可以组包，失败即本例不过
	run("4.3", func(t *testing.T) {
		// 发布资产，失败则本步验收不能继续
		pub, err := e.fac.PublishAsset(e.ctx, e.pe.tok, draft.ID, draft.Revision)
		// 发布资产失败就停，避免带着错误继续验
		if err != nil {
			// 发布资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 组工程闭包失败就停，避免带着错误继续验
		if _, err := e.fac.AssembleProject(e.ctx, e.pe.tok, pub.ID); err != nil {
			// 组工程闭包失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})

	// 验收改脏缓存后不能当有效激活，激活成功即失败
	run("16.1", func(t *testing.T) {
		// 拷一份袋再改脏，避免弄坏原来那份缓存
		b := bag
		// 拷一份缓存列表再改，避免弄坏原袋
		b.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		// 缓存份数必须符合上限，多或少即失败
		if len(b.Closures) == 0 {
			// 没有已缓存闭包就失败，后面无法改脏验证
			t.Fatal("need cached")
		}
		// 拷出成员再改脏，激活时应完整性失败
		m := append([]factory.ClosureMember{}, b.Closures[0].Members...)
		// 把根成员正文改脏，激活时应完整性失败
		m[0].Content = []byte("tampered-cache")
		// 把改脏的成员放回袋，激活时应完整性失败
		b.Closures[0].Members = m
		// 期望激活工程被拒为完整性失败，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &b, e.clocks, b.Closures[0].AssetID); !errors.Is(err, domain.ErrIntegrity) {
			// 没拒成完整性失败就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收摘要匹配的已授权闭包可以激活，失败即不过
	run("16.2", func(t *testing.T) {
		// 激活工程失败就停，避免带着错误继续验
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); err != nil {
			// 激活工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
}

// 验收下发、越权和副本保密，串厂或泄密即失败
func testClosureDistribute(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备闭包夹具，失败则本段用例无法开始
	e := newEnv04(t)
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p1 := e.pubProc(t, "厂工艺", []byte("fp"))
	// 发布一条厂级工程，失败则没有可组的包
	proj := e.pubProj(t, "厂工程", e.secret, e.depsOf(p1))
	// 绑定焊机并装袋，失败则不能下发或激活
	cid, bag := e.bound(t, e.op.acc.ID)
	// 绑定焊机并装袋，失败则不能下发或激活
	cid2, bag2 := e.bound(t, e.op.acc.ID)

	// 准备平台级工艺正文，下发后不该读出明文
	platProcBody := []byte("plat-proc")
	// 准备一条平台级工艺成员，供单独下发
	platProc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: platProcBody, Digest: digest.Sum(platProcBody),
	}
	// 记下厂A身份，送达只应落到这家厂
	fid := e.seed.ID
	// 验收平台级工艺下发成只读副本，正文被读出即失败
	run("5.4", func(t *testing.T) {
		// 封一份闭包快照，摘要错则送达会被拒
		snap := sealSnap(factory.KindProcess, platProc, nil, &fid, nil)
		// 收下平台送达失败就停，避免带着错误继续验
		if err := e.fac.AcceptPlatformDelivery(e.ctx, snap); err != nil {
			// 收下平台送达失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读平台副本，失败则本步验收不能继续
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, platProc.ID, 1)
		// 读平台副本失败就停，避免带着错误继续验
		if err != nil || got.Copyable || got.Level != factory.AssetLevelPlatform || got.Content != nil {
			// 可复制标记不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
		// 期望读资产正文被拒为越权，放行即失败
		if _, err := e.fac.ReadAssetContent(e.ctx, e.pe.tok, platProc.ID); !errors.Is(err, domain.ErrForbidden) {
			// 不可复制副本的正文被读出就失败
			t.Fatalf("secret replica content: %v", err)
		}
		// 列出资产，失败则本步验收不能继续
		listed, err := e.fac.ListAssets(e.ctx, e.pe.tok, factory.KindProcess)
		// 列出资产失败就停，避免带着错误继续验
		if err != nil {
			// 列出资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 先当作没找到，找到了再改，漏找即失败
		found := false
		// 逐台看焊机，不该还占着旧操作员
		for _, a := range listed {
			// 级别必须是平台级只读，级别错即失败
			if a.ID == platProc.ID && a.Level == factory.AssetLevelPlatform {
				// 标上已经找到，循环结束仍没有即失败
				found = true
				break
			}
		}
		// 列表里没有这份副本就失败，下发没落上
		if !found {
			// 列表里没有平台级副本就失败
			t.Fatal("platform replica missing from list")
		}
	})
	// 准备平台级工程正文，转发或升档应当被拒
	platProjBody := []byte("plat-proj")
	// 准备一条平台级工程成员，供下发成副本
	platProj := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "平台工程",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: platProjBody, Digest: digest.Sum(platProjBody),
		Deps: []factory.AssetDep{{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}},
	}
	// 验收已授权厂收下平台级工程副本，没有即失败
	run("5.1", func(t *testing.T) {
		// 封一份闭包快照，摘要错则送达会被拒
		snap := sealSnap(factory.KindProject, platProj, []factory.ClosureMember{platProc}, &fid, nil)
		// 收下平台送达失败就停，避免带着错误继续验
		if err := e.fac.AcceptPlatformDelivery(e.ctx, snap); err != nil {
			// 收下平台送达失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读平台副本，失败则本步验收不能继续
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, platProj.ID, 1)
		// 读平台副本失败就停，避免带着错误继续验
		if err != nil || got.Copyable || got.ID != platProj.ID {
			// 可复制标记不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
		// 读资产正文，失败则本步验收不能继续
		body, err := e.fac.ReadAssetContent(e.ctx, e.pe.tok, platProj.ID)
		// 读资产正文失败就停，避免带着错误继续验
		if err != nil || string(body) != "plat-proj" {
			// 工程正文读出来的内容不对就失败
			t.Fatalf("project content %q %v", body, err)
		}
	})

	// 验收他厂读不到本厂工程，读到即失败
	run("6.1", func(t *testing.T) {
		// 期望组工程闭包被拒为不存在，放行即失败
		if _, err := e.facB.AssembleProject(e.ctx, e.peB.tok, proj.ID); !errors.Is(err, domain.ErrNotFound) {
			// 组包结果不对就失败
			t.Fatalf("assemble: %v", err)
		}
		// 期望授权焊机接收工程被拒为这几种原因之一，放行即失败
		if err := e.facB.GrantClientProject(e.ctx, e.saB, proj.ID, cid); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrForbidden) {
			// 授予角色的结果不对就失败
			t.Fatalf("grant: %v", err)
		}
	})
	// 验收平台级副本不能再转发他厂，转成即失败
	run("6.3", func(t *testing.T) {
		// 期望把副本转发给他厂被拒为越权，放行即失败
		if err := e.fac.ForwardToFactory(e.ctx, e.pe.tok, platProj.ID, e.seedB.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收平台级副本不能改可复制或升档，改成即失败
	run("9.1", func(t *testing.T) {
		// 期望改副本可复制被拒为越权，放行即失败
		if err := e.fac.SetReplicaCopyable(e.ctx, e.pe.tok, platProj.ID, true); !errors.Is(err, domain.ErrForbidden) {
			// 可复制标记被改掉就失败，副本应只读
			t.Fatalf("copyable: %v", err)
		}
		// 期望把副本升档被拒为越权，放行即失败
		if err := e.fac.PromoteReplica(e.ctx, e.pe.tok, platProj.ID); !errors.Is(err, domain.ErrForbidden) {
			// 升档没有被拒就失败
			t.Fatalf("promote: %v", err)
		}
		// 准备可复制的平台正文，厂里改标记应当被拒
		body := []byte("plat-copyable")
		// 另做一份成员，用来替换或对照原闭包
		m := factory.ClosureMember{
			ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "可复制平台工艺",
			Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: body, Digest: digest.Sum(body),
		}
		// 封一份闭包快照失败就停，避免带着错误继续验
		if err := e.fac.AcceptPlatformDelivery(e.ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
			// 封一份闭包快照失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读平台副本，失败则本步验收不能继续
		got, err := e.fac.GetReplica(e.ctx, e.pe.tok, m.ID, 1)
		// 读平台副本失败就停，避免带着错误继续验
		if err != nil || !got.Copyable {
			// 可复制标记不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
		// 期望改副本可复制被拒为越权，放行即失败
		if err := e.fac.SetReplicaCopyable(e.ctx, e.pe.tok, m.ID, false); !errors.Is(err, domain.ErrForbidden) {
			// 收紧副本可复制没有被拒就失败
			t.Fatalf("tighten replica: %v", err)
		}
	})
	// 验收平台级不能转发给未授权厂，转成即失败
	run("9.2", func(t *testing.T) {
		// 期望把副本转发给他厂被拒为越权，放行即失败
		if err := e.fac.ForwardToFactory(e.ctx, e.sa, platProj.ID, e.seedB.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 验收厂级工程能依赖已到达的平台工艺，建不成即失败
	run("10.1", func(t *testing.T) {
		// 钉上平台级工艺的身份和修订，当工程依赖
		dep := factory.AssetDep{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}
		// 起草厂级工程，失败则本步验收不能继续
		got, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "依赖平台工艺", []byte("fp2"), []factory.AssetDep{dep})
		// 起草厂级工程失败就停，避免带着错误继续验
		if err != nil {
			// 起草厂级工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 发布资产，失败则本步验收不能继续
		got, err = e.fac.PublishAsset(e.ctx, e.pe.tok, got.ID, got.Revision)
		// 发布资产失败就停，避免带着错误继续验
		if err != nil {
			// 发布资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 组包结果这里不展开，调用失败则本步不算过
		_ = got
	})
	// 验收未到达的平台工艺不能当依赖，建成即失败
	run("10.2", func(t *testing.T) {
		// 新开一个身份，避免和别的绑定撞号
		missing := factory.AssetDep{ID: id.New(), Revision: 1, Digest: digest.Sum([]byte("x"))}
		// 期望起草厂级工程被拒为依赖未到达，放行即失败
		if _, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "未到达", []byte("x"), []factory.AssetDep{missing}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})

	// 验收已授权焊机能收下厂级闭包，袋里没有即失败
	run("7.1", func(t *testing.T) {
		// 授权焊机接收工程失败就停，避免带着错误继续验
		if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			// 授权焊机接收工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 缓存份数必须符合上限，多或少即失败
		if len(bag.Closures) != 1 || bag.Closures[0].AssetID != proj.ID {
			// 缓存份数不符就把实际值打出并停掉本例
			t.Fatalf("%+v", bag.Closures)
		}
	})
	// 验收未授权焊机收不到闭包，收下即失败
	run("7.2", func(t *testing.T) {
		// 期望把闭包下发到焊机被拒为这几种原因之一，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid2, &bag2, e.clocks); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrForbidden) {
			// 未授权却成功了就失败
			t.Fatalf("ungranted: %v", err)
		}
	})
	// 验收审计员和超管不能下发，下发成功即失败
	run("7.3", func(t *testing.T) {
		// 期望把闭包下发到焊机被拒为越权，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.aud.tok, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			// 这个身份的结果不符合预期，放行或做错即失败
			t.Fatalf("aud: %v", err)
		}
		// 期望把闭包下发到焊机被拒为越权，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.sa, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			// 这个身份的结果不符合预期，放行或做错即失败
			t.Fatalf("sa: %v", err)
		}
	})
	// 验收车间工程师不能下发厂级工程，下发成功即失败
	run("7.4", func(t *testing.T) {
		// 期望把闭包下发到焊机被拒为越权，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.peShop.tok, proj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			// 这个身份的结果不符合预期，放行或做错即失败
			t.Fatalf("shop: %v", err)
		}
	})
	// 验收厂级工程师能下发已收平台级，袋里没有即失败
	run("7.5", func(t *testing.T) {
		// 授权焊机接收工程失败就停，避免带着错误继续验
		if err := e.fac.GrantClientProject(e.ctx, e.sa, platProj.ID, cid); err != nil {
			// 授权焊机接收工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, platProj.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收车间工程师不能下发平台级，下发成功即失败
	run("7.6", func(t *testing.T) {
		// 期望把闭包下发到焊机被拒为越权，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.peShop.tok, platProj.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收含平台工艺的闭包能下发到焊机，没有即失败
	run("10.3", func(t *testing.T) {
		// 钉上平台级工艺的身份和修订，当工程依赖
		dep := factory.AssetDep{ID: platProc.ID, Revision: 1, Digest: platProc.Digest}
		// 起草厂级工程，失败则本步验收不能继续
		got, err := e.fac.CreateFactoryProject(e.ctx, e.pe.tok, e.direct, "带平台成员", []byte("mix"), []factory.AssetDep{dep})
		// 起草厂级工程失败就停，避免带着错误继续验
		if err != nil {
			// 起草厂级工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 发布资产，失败则本步验收不能继续
		got, err = e.fac.PublishAsset(e.ctx, e.pe.tok, got.ID, got.Revision)
		// 发布资产失败就停，避免带着错误继续验
		if err != nil {
			// 发布资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 授权焊机接收工程失败就停，避免带着错误继续验
		if err := e.fac.GrantClientProject(e.ctx, e.sa, got.ID, cid); err != nil {
			// 授权焊机接收工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, got.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收同一修订再下发仍只占一份，多一份即失败
	run("15.1", func(t *testing.T) {
		// 记下当前缓存份数，再收一份不该超上限
		n := len(bag.Closures)
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 缓存份数必须符合上限，多或少即失败
		if len(bag.Closures) < n {
			// 缓存丢了就失败，不该因超限被撤
			t.Fatalf("lost cache")
		}
	})
	// 验收他厂闭包不能在本机激活，激活成功即失败
	run("6.2", func(t *testing.T) {
		// 新开一个身份，避免和别的绑定撞号
		cidB := id.New()
		// 生成焊机密钥，失败则绑定无法开始
		pub, priv, err := nodekey.Generate()
		// 生成焊机密钥失败就停，避免带着错误继续验
		if err != nil {
			// 生成焊机密钥失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 接受焊机绑定失败就停，避免带着错误继续验
		if _, err := e.facB.AcceptBinding(e.ctx, cidB, "焊机-B", pub, 1); err != nil {
			// 接受焊机绑定失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 取厂签名公钥，失败则本步验收不能继续
		facPub, err := e.facB.SigningPublicKey(e.ctx)
		// 取厂签名公钥失败就停，避免带着错误继续验
		if err != nil {
			// 取厂签名公钥失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 装好本机袋，缺密钥则签名验证会失败
		bagB := bagOf(e.seedB.ID, cidB, pub, priv, facPub)
		// 缓存份数必须符合上限，多或少即失败
		if len(bag.Closures) == 0 {
			// 没有闭包就失败，组包或下发没有生效
			t.Fatal("need closure")
		}
		// 取出厂A的闭包，拿去别的袋上激活应当被拒
		stolen := bag.Closures[0]
		// 把别人的闭包塞进这台袋，激活应当被拒
		bagB.Closures = []factory.ClosureSnapshot{stolen}
		// 期望激活工程被拒为这几种原因之一，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &bagB, e.clocks, stolen.AssetID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
			// 没拒成这几种原因之一就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
}

// 验收激活、上限和过期，不该激活却成功即失败
func testClosureActivate(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备闭包夹具，失败则本段用例无法开始
	e := newEnv04(t)
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p1 := e.pubProc(t, "A工艺", []byte("a"))
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p2 := e.pubProc(t, "B工艺", []byte("b"))
	// 发布一条厂级工程，失败则没有可组的包
	proj1 := e.pubProj(t, "工程1", e.secret, e.depsOf(p1))
	// 发布一条厂级工程，失败则没有可组的包
	proj2 := e.pubProj(t, "工程2", []byte("p2-body"), e.depsOf(p2))
	// 发布一条厂级工程，失败则没有可组的包
	proj3 := e.pubProj(t, "工程3", []byte("p3-body"), nil)
	// 绑定焊机并装袋，失败则不能下发或激活
	cid, bag := e.bound(t, e.op.acc.ID)
	// 授权焊机接收工程失败就停，避免带着错误继续验
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj1.ID, cid); err != nil {
		// 授权焊机接收工程失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 授权焊机接收工程失败就停，避免带着错误继续验
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj2.ID, cid); err != nil {
		// 授权焊机接收工程失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 授权焊机接收工程失败就停，避免带着错误继续验
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj3.ID, cid); err != nil {
		// 授权焊机接收工程失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 把闭包下发到焊机失败就停，避免带着错误继续验
	if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj1.ID, cid, &bag, e.clocks); err != nil {
		// 把闭包下发到焊机失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 验收上限内可以缓存两份闭包，少一份即失败
	run("11.1", func(t *testing.T) {
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj2.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 缓存份数必须符合上限，多或少即失败
		if len(bag.Closures) != 2 {
			// 缓存份数不符就把实际值打出并停掉本例
			t.Fatalf("%d", len(bag.Closures))
		}
	})
	// 验收超过缓存上限的第三份被拒，收下即失败
	run("11.2", func(t *testing.T) {
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj3.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 缓存份数必须符合上限，多或少即失败
		if len(bag.Closures) != 3 {
			// 数量不对就失败，否则失败
			t.Fatalf("len %d", len(bag.Closures))
		}
	})
	// 验收已激活的缓存不会因超限被撤，被撤即失败
	run("11.4", func(t *testing.T) {
		// 激活工程失败就停，避免带着错误继续验
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); err != nil {
			// 激活工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 当前激活必须指向这份工程，指错即失败
		if bag.ActiveID == nil || *bag.ActiveID != proj1.ID {
			// 应当处于激活却没有，切换没生效
			t.Fatal("not active")
		}
	})
	// 验收撤掉未焊缓存后还能再收，收不成即失败
	run("11.3", func(t *testing.T) {
		// 期望撤掉缓存工程被拒为越权，放行即失败
		if err := e.fac.UncacheProject(e.ctx, &bag, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			// 已激活的缓存被撤掉就失败
			t.Fatalf("active uncache: %v", err)
		}
		// 撤掉缓存工程失败就停，避免带着错误继续验
		if err := e.fac.UncacheProject(e.ctx, &bag, proj2.ID); err != nil {
			// 撤掉缓存工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把闭包下发到焊机失败就停，避免带着错误继续验
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj3.ID, cid, &bag, e.clocks); err != nil {
			// 把闭包下发到焊机失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})

	// 验收完整闭包可以激活，激活失败即本例不过
	run("12.1", func(t *testing.T) {
		// 激活工程失败就停，避免带着错误继续验
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); err != nil {
			// 激活工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 当前激活必须指向这份工程，指错即失败
		if bag.ActiveID == nil || *bag.ActiveID != proj1.ID {
			// 激活状态不对就失败
			t.Fatal("active")
		}
	})
	// 验收未焊接时可以改激活另一份，没换即失败
	run("12.2", func(t *testing.T) {
		// 激活工程失败就停，避免带着错误继续验
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj3.ID); err != nil {
			// 激活工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 当前激活必须指向这份工程，指错即失败
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			// 不该切换激活却换了，焊接中应被拒
			t.Fatal("switched")
		}
	})
	// 验收缺成员的闭包不能激活，激活成功即失败
	run("12.3", func(t *testing.T) {
		// 拷一份袋再拆成员，用来验缺成员不能激活
		bad := bag
		// 拷出缓存再拆成员，缺成员时激活应被拒
		bad.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		// 缓存份数必须符合上限，多或少即失败
		if len(bad.Closures) == 0 {
			// 空值没有被拒或结果是空就失败
			t.Fatal("empty")
		}
		// 只留根成员，缺依赖时激活应当被拒
		bad.Closures[0].Members = bad.Closures[0].Members[:1]
		// 数量必须符合期望，多或少即失败
		if len(bag.Closures[0].Members) > 1 {
			// 期望激活工程被拒为这几种原因之一，放行即失败
			if err := e.fac.ActivateProject(e.ctx, &bad, e.clocks, bad.Closures[0].AssetID); !errors.Is(err, domain.ErrClosureIncomplete) && !errors.Is(err, domain.ErrIntegrity) && !errors.Is(err, domain.ErrClosureMismatch) {
				// 没拒成这几种原因之一就失败，并打出实际错误
				t.Fatalf("got %v", err)
			}
		}
	})

	// 验收焊接中不能切换或撤当前缓存，做成即失败
	run("13.1", func(t *testing.T) {
		// 标成正在焊接，这时切换或安装应当被拒
		bag.Welding = true
		// 期望激活工程被拒为越权，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			// 切换激活失败，未焊接时应当允许
			t.Fatalf("switch: %v", err)
		}
		// 当前激活必须指向这份工程，指错即失败
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			// 不该变化的状态变了就失败
			t.Fatal("changed")
		}
		// 期望撤掉缓存工程被拒为越权，放行即失败
		if err := e.fac.UncacheProject(e.ctx, &bag, proj3.ID); !errors.Is(err, domain.ErrForbidden) {
			// 撤缓存的结果不对就失败
			t.Fatalf("uncache: %v", err)
		}
		// 标成没在焊接，再验过期或未登录被拒
		bag.Welding = false
	})
	// 验收凭证过期且未焊时不能新激活，激活即失败
	run("13.2", func(t *testing.T) {
		// 拷一份时钟再改到过期，避免弄脏原来的时刻
		expired := e.clocks
		// 把时刻往后推，没成功则本步验收失败
		expired.Server = e.na.Add(time.Hour)
		// 本机时钟跟服务器对齐，只让凭证本身过期
		expired.Local = expired.Server
		// 期望激活工程被拒为越权，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &bag, expired, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收过期但仍在焊时旧激活还在，丢了即失败
	run("13.3", func(t *testing.T) {
		// 标成正在焊接，这时切换或安装应当被拒
		bag.Welding = true
		// 拷一份时钟再改到过期，避免弄脏原来的时刻
		expired := e.clocks
		// 把时刻往后推，没成功则本步验收失败
		expired.Server = e.na.Add(time.Hour)
		// 期望激活工程被拒为越权，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &bag, expired, proj1.ID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 当前激活必须指向这份工程，指错即失败
		if bag.ActiveID == nil || *bag.ActiveID != proj3.ID {
			// 焊接中的激活丢了就失败，应当继续
			t.Fatal("lost weld")
		}
		// 标成没在焊接，再验过期或未登录被拒
		bag.Welding = false
	})

	// 验收个人级工程不能授权给焊机，授成即失败
	run("8.1", func(t *testing.T) {
		// 起草个人工程，失败则本步验收不能继续
		per, err := e.fac.CreatePersonalProject(e.ctx, e.pe.tok, e.direct, "个人工程", []byte("mine"), nil)
		// 起草个人工程失败就停，避免带着错误继续验
		if err != nil {
			// 起草个人工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 发布资产失败就停，避免带着错误继续验
		if _, err := e.fac.PublishAsset(e.ctx, e.pe.tok, per.ID, per.Revision); err != nil {
			// 发布资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望授权焊机接收工程被拒为越权，放行即失败
		if err := e.fac.GrantClientProject(e.ctx, e.sa, per.ID, cid); !errors.Is(err, domain.ErrForbidden) {
			// 授予角色的结果不对就失败
			t.Fatalf("grant: %v", err)
		}
		// 期望把闭包下发到焊机被拒为越权，放行即失败
		if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, per.ID, cid, &bag, e.clocks); !errors.Is(err, domain.ErrForbidden) {
			// 下发结果不对就失败
			t.Fatalf("dist: %v", err)
		}
	})
	// 绑定焊机并装袋，失败则不能下发或激活
	peCid, peBag := e.bound(t, e.pe.acc.ID)
	// 验收创建人能在本机激活个人级，失败即本例不过
	run("8.2", func(t *testing.T) {
		// 起草个人工程，失败则本步验收不能继续
		per, err := e.fac.CreatePersonalProject(e.ctx, e.pe.tok, e.direct, "我的工程", []byte("mine2"), nil)
		// 起草个人工程失败就停，避免带着错误继续验
		if err != nil {
			// 起草个人工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 发布资产，失败则本步验收不能继续
		per, err = e.fac.PublishAsset(e.ctx, e.pe.tok, per.ID, per.Revision)
		// 发布资产失败就停，避免带着错误继续验
		if err != nil {
			// 发布资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 把个人工程放进本机袋失败就停，避免带着错误继续验
		if err := e.fac.CachePersonalProject(e.ctx, e.pe.tok, per.ID, peCid, &peBag, e.clocks); err != nil {
			// 把个人工程放进本机袋失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 激活工程失败就停，避免带着错误继续验
		if err := e.fac.ActivateProject(e.ctx, &peBag, e.clocks, per.ID); err != nil {
			// 激活工程失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 验收他人不能激活别人的个人级，激活即失败
	run("8.3", func(t *testing.T) {
		// 记下个人工程身份，别人激活它应当被拒
		perID := peBag.Closures[0].AssetID
		// 拷一份操作员的袋，用来试激活别人的个人工程
		opBag := bag
		// 钉上目标焊机，下发和激活都按这台核对
		peBag.Closures[0].TargetClientID = &cid
		// 把别人的个人工程塞进这只袋，激活应被拒
		opBag.Closures = append(opBag.Closures, peBag.Closures[0])
		// 期望激活工程被拒为这几种原因之一，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &opBag, e.clocks, perID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrIntegrity) {
			// 没拒成这几种原因之一就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 验收未授权焊机不能激活别人的闭包，激活即失败
	run("14.1", func(t *testing.T) {
		// 新开一个身份，避免和别的绑定撞号
		cidX := id.New()
		// 生成焊机密钥，失败则绑定无法开始
		pub, priv, err := nodekey.Generate()
		// 生成焊机密钥失败就停，避免带着错误继续验
		if err != nil {
			// 生成焊机密钥失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 装好本机袋，缺密钥则签名验证会失败
		other := bagOf(e.seed.ID, cidX, pub, priv, bag.FactoryPublic)
		// 拷到另一台袋上，未授权激活应当被拒
		other.Closures = append([]factory.ClosureSnapshot{}, bag.Closures...)
		// 缓存份数必须符合上限，多或少即失败
		if len(other.Closures) == 0 {
			// 空值没有被拒或结果是空就失败
			t.Fatal("empty")
		}
		// 期望激活工程被拒为越权，放行即失败
		if err := e.fac.ActivateProject(e.ctx, &other, e.clocks, other.Closures[0].AssetID); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})

	// 验收审计齐全且不含正文和口令，缺了或泄密即失败
	run("17.1", func(t *testing.T) {
		// 读取审计，失败则本步验收不能继续
		rows, err := e.fac.ListAudit(e.ctx)
		// 读取审计失败就停，避免带着错误继续验
		if err != nil {
			// 读取审计失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 审计字段不齐就失败，这条记录不能用
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			// 记录字段缺了就失败，不能当完整记录
			t.Fatalf("incomplete %#v", bad[0])
		}
		// 文本里夹带秘密或禁词就失败，说明泄密了
		if audit.ContainsAny(audit.Dump(rows), string(e.secret), "sa-pass", "pe-pass", e.sa) {
			// 审计里出现秘密就失败，说明正文或口令泄了
			t.Fatalf("secret leaked")
		}
	})
	// 验收拒绝原因能区分缺失和完整性，混了即失败
	run("17.2", func(t *testing.T) {
		// 读取审计，失败则本步验收不能继续
		rows, err := e.fac.ListAudit(e.ctx)
		// 读取审计失败就停，避免带着错误继续验
		if err != nil {
			// 读取审计失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 没有期望的拒绝审计就失败，原因没分开
		if !audit.HasResult(rows, "pack_closure", audit.Deny) && !audit.HasResult(rows, "distribute_closure", audit.Deny) {
			// 缺失/串版/未授权至少有一条拒绝
			hasDeny := false
			// 逐条看记录，缺了拒绝或字段即失败
			for _, r := range rows {
				// 审计必须符合期望，对不上即失败
				if r.Result == audit.Deny {
					// 标上见到拒绝审计，一条都没有即失败
					hasDeny = true
					break
				}
			}
			// 一条拒绝审计都没有就失败，原因没留下
			if !hasDeny {
				// 拒绝没有留下审计就失败，原因分不出来
				t.Fatal("need deny audit")
			}
		}
	})
}
