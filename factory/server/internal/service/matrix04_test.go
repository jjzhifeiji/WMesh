// 阶段4第4～7圈：厂内侧工程闭包 1.1～17.2（离线下发与激活已撤，只留组包、送达与审计）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 本文件要跑完的闭包编号，漏跑就在收尾报错
var matrix04IDs = []string{
	"1.1", "1.2", "1.3",
	"3.1", "3.2",
	"4.1", "4.2", "4.3",
	"5.1", "5.4",
	"6.1",
	"10.1", "10.2",
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
	// 跑送达段，失败说明串厂或泄密没被拦住
	testClosureDistribute(t, run)
}

// 闭包矩阵共用夹具，装两厂服务和角色
type env04 struct {
	// 本用例的调用上下文，取消会打断夹具
	ctx context.Context
	// 厂A建厂结果，用来核对归属是不是这家
	seed Seeded
	// 厂A服务，闭包和账号操作都打在这里
	fac *factory.Service
	// 厂B服务，他厂不该看见厂A的数据
	facB *factory.Service
	// 厂A超管令牌，授权和下发时拿它证明身份
	sa string
	// 厂A工艺工程师，负责组包和下发
	pe namedAcc
	// 厂B工程师，读厂A工程应当被拒绝
	peB namedAcc
	// 直属厂的工作上下文，不挂任何车间
	direct factory.WorkContext
	// 工程正文，审计里出现它就算泄密失败
	secret []byte
}

// 准备两厂人员和组织，缺一项则夹具失败
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
	if err := seedPass(ctx, facA, "sa-a", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	// 建厂并种初始超管，失败则本步验收不能继续
	_, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, facB, "sa-b", "sa-b-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	// 装上两厂服务和角色，缺一项则闭包夹具不完整
	e := &env04{
		ctx: ctx, seed: seed, fac: facA, facB: facB, sa: saA,
		pe:     withOperate(t, ctx, facA, saA, mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOrgAdmin)),
		peB:    withOperate(t, ctx, facB, saB, mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleOrgAdmin)),
		direct: factory.WorkContext{},
		secret: []byte("closure-body-secret"),
	}
	return e
}

// 给管理员再授操作员，工程师既要制作厂级又要组包下发
func withOperate(t *testing.T, ctx context.Context, fac *factory.Service, saTok string, acc namedAcc) namedAcc {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 补授操作员失败就停，避免带着错误继续验
	if _, err := fac.GrantRole(ctx, saTok, acc.acc.ID, factory.RoleOperator); err != nil {
		// 补授操作员失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	return acc
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

// 验收平台送达、跨厂隔离和依赖，串厂或泄密即失败
func testClosureDistribute(t *testing.T, run func(string, func(*testing.T))) {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备闭包夹具，失败则本段用例无法开始
	e := newEnv04(t)
	// 发布一条厂级工艺，失败则组包缺这条依赖
	p1 := e.pubProc(t, "厂工艺", []byte("fp"))
	// 发布一条厂级工程，失败则没有可组的包
	proj := e.pubProj(t, "厂工程", e.secret, e.depsOf(p1))
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
}
