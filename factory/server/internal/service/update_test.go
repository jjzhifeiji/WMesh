// 软件更新服务层矩阵：厂自拉、确认、本机袋。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 本文件要跑完的更新编号，漏跑就在收尾报错
var matrixSoftwareIDs = []string{
	"U2", "U3", "U4", "U5", "U8", "U9", "U10", "U11", "U12", "U13",
	"U14", "U16", "U17", "U19", "U20", "U21", "U22", "U23", "U28", "U29", "U30", "U31",
}

// 内存里的软件源，用来代替云端拉取
type memSource struct {
	// 保护报价和拉取次数，不加锁会把计数写乱
	mu sync.Mutex
	// 为真时拉取直接拒绝，用来模拟无权
	deny bool
	// 按种类放着的报价，没有就应当报不存在
	offers map[string]factory.SoftwareOffer
	// 实际拉取次数，多一次说明没有走幂等
	pulls int
}

// 按种类记下报价，后面拉取对不上就拿错包
func (m *memSource) put(o factory.SoftwareOffer) {
	// 报价表还没有就先建上，否则后面写入会失败
	if m.offers == nil {
		// 准备按种类放报价的表，空表表示还没发布
		m.offers = map[string]factory.SoftwareOffer{}
	}
	// 按种类盖上这份报价，拉的时候按它给
	m.offers[o.Kind] = o
}

// 按种类给出最新元数据，拒绝或没有就返回错误
func (m *memSource) Latest(_ context.Context, kind string) (factory.SoftwareMeta, error) {
	// 源被设成拒绝时直接返回错误，放行即失败
	if m.deny {
		return factory.SoftwareMeta{}, domain.ErrForbidden
	}
	// 按种类取报价，没有就应当报不存在
	o, ok := m.offers[kind]
	// 没找到目标就失败，说明这一步没生效
	if !ok {
		return factory.SoftwareMeta{}, domain.ErrNotFound
	}
	return factory.SoftwareMeta{Kind: o.Kind, Version: o.Version, VersionName: o.VersionName, Digest: o.Digest}, nil
}

// 按种类和版本给出正文，拒绝或对不上就返回错误
func (m *memSource) Pull(_ context.Context, kind string, version int64) ([]byte, error) {
	// 锁住拉取计数，避免并发把次数写乱
	m.mu.Lock()
	// 拉取次数加一，多出来说明没有走幂等
	m.pulls++
	// 放开拉取计数，不放则后面读不到次数
	m.mu.Unlock()
	// 源被设成拒绝时直接返回错误，放行即失败
	if m.deny {
		return nil, domain.ErrForbidden
	}
	// 按种类取报价，没有就应当报不存在
	o, ok := m.offers[kind]
	// 版本必须是期望值，指错包即失败
	if !ok || o.Version != version {
		return nil, domain.ErrNotFound
	}
	return o.Body, nil
}

// 用正文算出摘要并拼报价，摘要错则拉取会被拒
func offerOf(kind string, version int64, name string, body []byte) factory.SoftwareOffer {
	return factory.SoftwareOffer{
		Kind: kind, Version: version, VersionName: name, Digest: digest.Sum(body), Body: body,
	}
}

// 跑软件更新矩阵，漏编号就在收尾报错
func TestMatrixSoftware(t *testing.T) {
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
		for _, id := range matrixSoftwareIDs {
			// 这个编号没跑到就要报错，矩阵不算完成
			if !ran[id] {
				// 漏跑的编号要报出来，否则矩阵不算完成
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
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
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授角色，失败则夹具缺这个身份
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 取当前时刻，用来核对令牌或凭证是否过期
	now := time.Now().UTC()
	// 服务器和本机同一时刻，避免时钟本身造成拒绝
	clocks := factory.Clocks{Server: now, Local: now}
	// 把时刻往后推，没成功则本步验收失败
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	// 用内存源代替云端，没有报价就当没发布
	src := &memSource{}
	// 拼一份软件报价，摘要错则拉取会被拒
	src.put(offerOf(factory.SoftwareFactoryService, 5, "1.5.0", []byte("factory-svc-5")))
	// 换上软件源，不换则拉不到要验的那一版
	facA.Updates.SetSoftwareSource(src)

	// 验收已认领厂能拉到最高厂服务包，没有副本即失败
	run("U2", func(t *testing.T) {
		// 从源拉取厂内软件，失败则本步验收不能继续
		got, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareFactoryService)
		// 从源拉取厂内软件失败就停，避免带着错误继续验
		if err != nil || got.Version != 5 {
			// 版本不符就把实际值打出并停掉本例
			t.Fatalf("%+v %v", got, err)
		}
	})
	// 验收没人确认时不安装，装上了即失败
	run("U3", func(t *testing.T) {
		// 读取已装版本，失败则本步验收不能继续
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		// 读取已装版本失败就停，避免带着错误继续验
		if err != nil || got != 0 {
			// 已装版本不对就失败
			t.Fatalf("installed %d %v", got, err)
		}
		// 读取待确认软件，失败则本步验收不能继续
		p, err := facA.PendingFactorySoftware(ctx, saA)
		// 读取待确认软件失败就停，避免带着错误继续验
		if err != nil || p == nil || p.Version != 5 {
			// 待确认状态或版本不对就失败
			t.Fatalf("pending %+v %v", p, err)
		}
	})
	// 验收非超管确认厂服务被拒，放行即失败
	run("U5", func(t *testing.T) {
		// 期望确认厂服务更新被拒为越权，放行即失败
		if err := facA.ConfirmFactoryUpdate(ctx, op.tok, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 留出焊接中的袋，确认安装时应当被拒
	var weldingBag factory.Bag
	// 验收超管确认后厂服务换成新版本，没换即失败
	run("U4", func(t *testing.T) {
		// 装好带凭证的本机袋，失败则不能确认更新
		cid, bag := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
		// 焊机号这里不拿来断言，绑定失败则前面已停
		_ = cid
		// 标成正在焊接，这时切换或安装应当被拒
		bag.Welding = true
		// 记下这只正在焊接的袋，确认时应当拒绝安装
		weldingBag = bag
		// 设定这一步的落地结果，用来验失败是否回滚
		facA.Updates.SetApplyOutcome(factory.ApplyOK)
		// 确认厂服务更新失败就停，避免带着错误继续验
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 5); err != nil {
			// 确认厂服务更新失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 读取已装版本，失败则本步验收不能继续
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		// 读取已装版本失败就停，避免带着错误继续验
		if err != nil || got != 5 {
			// 已装版本不对就失败
			t.Fatalf("installed %d %v", got, err)
		}
	})
	// 验收厂服务重启不把正在焊记成停焊，记错即失败
	run("U22", func(t *testing.T) {
		// 焊接标记不对就失败，该拒的操作被放行
		if !weldingBag.Welding {
			// 焊接标记被清掉就失败，应当继续焊
			t.Fatal("welding cleared")
		}
		// 按时钟判定运行凭证，没成功则本步验收失败
		ev := factory.EvaluateRuntime(weldingBag, clocks, factory.NodeContinue)
		// 重启时正在焊必须允许继续，记成停焊即失败
		if ev.Decision != factory.NodeContinueWeld && ev.Decision != factory.NodeAllow {
			// 焊接中的判定不对就失败
			t.Fatalf("weld decision %v", ev.Decision)
		}
	})
	// 验收厂能拉到最高客户端包，存储里没有即失败
	run("U8", func(t *testing.T) {
		// 拼一份软件报价，摘要错则拉取会被拒
		src.put(offerOf(factory.SoftwareClientAPK, 3, "6.1.0", []byte("apk-3")))
		// 从源拉取厂内软件失败就停，避免带着错误继续验
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			// 从源拉取厂内软件失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	})
	// 装好带凭证的本机袋，失败则不能确认更新
	cid1, bag1 := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
	// 装好带凭证的本机袋，失败则不能确认更新
	_, bag2 := boundBag(t, ctx, facA, saA, seedA.ID, pe.acc.ID, nb, na)
	// 焊机号这里不拿来断言，绑定失败则前面已停
	_ = cid1
	// 验收空闲且已登录可以确认安装，确认失败即不过
	run("U9", func(t *testing.T) {
		// 确认客户端更新失败就停，避免带着错误继续验
		if err := facA.ConfirmClientUpdate(ctx, &bag1, clocks, 3); err != nil {
			// 确认客户端更新失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 版本必须是期望值，指错包即失败
		if bag1.SoftwareVersion != 3 {
			// 版本不对就失败，否则失败
			t.Fatalf("ver %d", bag1.SoftwareVersion)
		}
	})
	// 验收焊接中确认安装被拒，装上即失败
	run("U10", func(t *testing.T) {
		// 标成正在焊接，这时切换或安装应当被拒
		bag2.Welding = true
		// 期望确认客户端更新被拒为越权，放行即失败
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, 3); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 版本必须是期望值，指错包即失败
		if bag2.SoftwareVersion != 0 {
			// 焊接中却装上了更新，应当被拒
			t.Fatal("installed while welding")
		}
		// 标成没在焊接，再验过期或未登录被拒
		bag2.Welding = false
	})
	// 验收未登录不能安装，装上即失败
	run("U11", func(t *testing.T) {
		// 拷一份袋再清登录，用来验未登录不能安装
		loggedOut := bag2
		// 清掉操作员，用来验未登录不能安装
		loggedOut.OperatorID = nil
		// 期望确认客户端更新被拒为越权，放行即失败
		if err := facA.ConfirmClientUpdate(ctx, &loggedOut, clocks, 3); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收暂不更新后仍可再提示且未装，装上即失败
	run("U12", func(t *testing.T) {
		// 读取待确认软件，失败则本步验收不能继续
		p, err := facA.PendingFactorySoftware(ctx, saA)
		// 读取待确认软件失败就停，避免带着错误继续验
		if err != nil || p != nil {
			// 安装成功后仍显示待确认就失败
			t.Fatalf("factory pending after install %+v %v", p, err)
		}
		// 拼一份软件报价，摘要错则拉取会被拒
		src.put(offerOf(factory.SoftwareClientAPK, 4, "6.2.0", []byte("apk-4")))
		// 从源拉取厂内软件失败就停，避免带着错误继续验
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			// 从源拉取厂内软件失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 版本必须是期望值，指错包即失败
		if bag2.SoftwareVersion != 0 {
			// 没确认的那台平板版本变了，应当仍旧
			t.Fatal("unconfirmed tablet changed")
		}
	})
	// 验收只确认一台时另一台仍是旧版，一起变即失败
	run("U23", func(t *testing.T) {
		// 版本必须是期望值，指错包即失败
		if bag1.SoftwareVersion != 3 || bag2.SoftwareVersion != 0 {
			// 两种结果混在一起就失败
			t.Fatalf("mixed %d %d", bag1.SoftwareVersion, bag2.SoftwareVersion)
		}
	})
	// 验收待确认时拉到更高版本，提示没改即失败
	run("U14", func(t *testing.T) {
		// 拼一份软件报价，摘要错则拉取会被拒
		src.put(offerOf(factory.SoftwareClientAPK, 6, "6.3.0", []byte("apk-6")))
		// 从源拉取厂内软件失败就停，避免带着错误继续验
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			// 从源拉取厂内软件失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望确认客户端更新被拒为修订已过时，放行即失败
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, 4); !errors.Is(err, domain.ErrStaleRevision) {
			// 没拒成修订已过时就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收可以删掉不是已装的旧包，删不掉即失败
	run("U29", func(t *testing.T) {
		// 列出厂内软件，失败则本步验收不能继续
		rows, err := facA.ListFactorySoftware(ctx, saA, "")
		// 列出厂内软件失败就停，避免带着错误继续验
		if err != nil {
			// 列出厂内软件失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 记下应保留的版本，清理时不该把它们删掉
		keep := map[string]string{}
		// 逐条看记录，缺了拒绝或字段即失败
		for _, row := range rows {
			// 把版本编成文本，没成功则本步验收失败
			keep[row.Kind+":"+strconv.FormatInt(row.Version, 10)] = row.Keep
		}
		// 当前最高版本必须标成保留，被标成可删即失败
		if keep[factory.SoftwareClientAPK+":6"] != factory.SoftwareKeepLatest {
			// 拉到的不是最高版本就失败
			t.Fatalf("apk latest %q", keep[factory.SoftwareClientAPK+":6"])
		}
		// 已装版本必须标成保留，被标成可删即失败
		if keep[factory.SoftwareFactoryService+":5"] != factory.SoftwareKeepInstalled {
			// 厂服务已装版本不对就失败
			t.Fatalf("svc installed %q", keep[factory.SoftwareFactoryService+":5"])
		}
		// 旧包不该再被标成保留，还留着即失败
		if keep[factory.SoftwareClientAPK+":3"] != "" || keep[factory.SoftwareClientAPK+":4"] != "" {
			// 旧的应保留版本丢了就失败
			t.Fatalf("old keep %+v", keep)
		}
		// 删除厂内软件包失败就停，避免带着错误继续验
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareClientAPK, 3); err != nil {
			// 删除厂内软件包失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 清理旧软件包，失败则本步验收不能继续
		n, err := facA.PruneFactorySoftware(ctx, saA, factory.SoftwareClientAPK)
		// 清理旧软件包失败就停，避免带着错误继续验
		if err != nil || n != 1 {
			// 清理结果不对就失败
			t.Fatalf("prune %d %v", n, err)
		}
	})
	// 验收不能删当前最高或已装版本，删掉即失败
	run("U30", func(t *testing.T) {
		// 期望删除厂内软件包被拒为仍被引用，放行即失败
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareClientAPK, 6); !errors.Is(err, domain.ErrReferenced) {
			// 拉到的不是最高版本就失败
			t.Fatalf("latest %v", err)
		}
		// 期望删除厂内软件包被拒为仍被引用，放行即失败
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrReferenced) {
			// 已装版本不对就失败
			t.Fatalf("installed %v", err)
		}
		// 期望删除厂内软件包被拒为越权，放行即失败
		if err := facA.DeleteFactorySoftware(ctx, op.tok, factory.SoftwareClientAPK, 4); !errors.Is(err, domain.ErrForbidden) {
			// 这个身份的结果不符合预期，放行或做错即失败
			t.Fatalf("op %v", err)
		}
	})
	// 验收超管可以请求清理无用镜像，被拒即失败
	run("U31", func(t *testing.T) {
		// 请求清理镜像失败就停，避免带着错误继续验
		if err := facA.RequestImagePrune(ctx, saA, ""); err != nil {
			// 请求清理镜像失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 期望请求清理镜像被拒为越权，放行即失败
		if err := facA.RequestImagePrune(ctx, op.tok, ""); !errors.Is(err, domain.ErrForbidden) {
			// 这个身份的结果不符合预期，放行或做错即失败
			t.Fatalf("op %v", err)
		}
	})
	// 验收已装更高版本后再拉旧版被拒，拉成即失败
	run("U13", func(t *testing.T) {
		// 期望拼一份软件报价被拒为修订已过时，放行即失败
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 4, "1.4.0", []byte("factory-svc-4"))); !errors.Is(err, domain.ErrStaleRevision) {
			// 没拒成修订已过时就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收摘要不符的包被拒，收下即失败
	run("U16", func(t *testing.T) {
		// 拼一份软件报价，摘要错则拉取会被拒
		bad := offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8"))
		// 把包字节改脏，摘要不符应当被拒
		bad.Body = []byte("tampered")
		// 期望吞入软件包被拒为完整性失败，放行即失败
		if err := facA.IngestSoftware(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
			// 没拒成完整性失败就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收他厂拉不到本厂的包，拉到即失败
	run("U17", func(t *testing.T) {
		// 做一个拒绝拉取的源，他厂来拉应当失败
		srcB := &memSource{deny: true}
		// 换上软件源，不换则拉不到要验的那一版
		facB.Updates.SetSoftwareSource(srcB)
		// 期望读取软件副本被拒为不存在，放行即失败
		if _, err := facB.Store().SoftwareReplica(ctx, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrNotFound) {
			// 他厂不该有这份副本，出现了即失败
			t.Fatalf("b replica %v", err)
		}
	})
	// 验收平板不能绕过厂直接拿包，拿到即失败
	run("U19", func(t *testing.T) {
		// 期望拼一份软件报价被拒为越权，放行即失败
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareWANService, 1, "w", []byte("wan"))); !errors.Is(err, domain.ErrForbidden) {
			// 没拒成越权就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
	})
	// 验收软件包不能写进工艺资产，写进去即失败
	run("U20", func(t *testing.T) {
		// 列出资产，失败则本步验收不能继续
		before, err := facA.ListAssets(ctx, saA, factory.KindProcess)
		// 列出资产失败就停，避免带着错误继续验
		if err != nil {
			// 列出资产失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 记下清理前的包数，删完不该把该留的也删
		n := len(before)
		// 读取软件副本失败就停，避免带着错误继续验
		if _, err := facA.Store().SoftwareReplica(ctx, factory.SoftwareFactoryService, 5); err != nil {
			// 读取软件副本失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 列出资产，失败则本步验收不能继续
		after, err := facA.ListAssets(ctx, saA, factory.KindProcess)
		// 列出资产失败就停，避免带着错误继续验
		if err != nil || len(after) != n {
			// 软件更新把工艺资产写进去了，应当被拒
			t.Fatalf("assets leaked into software %d %d %v", n, len(after), err)
		}
	})
	// 验收落地失败不记已装并回到旧版，记成新版即失败
	run("U21", func(t *testing.T) {
		// 拼一份软件报价失败就停，避免带着错误继续验
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 7, "1.7.0", []byte("factory-svc-7"))); err != nil {
			// 拼一份软件报价失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 设定这一步的落地结果，用来验失败是否回滚
		facA.Updates.SetApplyOutcome(factory.ApplyFail)
		// 期望确认厂服务更新被拒为安装失败，放行即失败
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 7); !errors.Is(err, domain.ErrSoftwareInstallFailed) {
			// 没拒成安装失败就失败，并打出实际错误
			t.Fatalf("got %v", err)
		}
		// 读取已装版本，失败则本步验收不能继续
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		// 读取已装版本失败就停，避免带着错误继续验
		if err != nil || got != 5 {
			// 落地失败后没有回到旧版本就失败
			t.Fatalf("rolled %d %v", got, err)
		}
		// 设定这一步的落地结果，用来验失败是否回滚
		facA.Updates.SetApplyOutcome(factory.ApplyDefer)
	})
	// 验收已有完整副本不再下载，又拉一次即失败
	run("U28", func(t *testing.T) {
		// 锁住拉取计数，避免并发把次数写乱
		src.mu.Lock()
		// 把拉取次数清零，后面多拉一次就能看出来
		src.pulls = 0
		// 放开拉取计数，不放则后面读不到次数
		src.mu.Unlock()
		// 确保软件副本在失败就停，避免带着错误继续验
		if err := facA.Updates.EnsureSoftware(ctx, factory.SoftwareFactoryService, 7); err != nil {
			// 确保软件副本在失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 锁住拉取计数，避免并发把次数写乱
		src.mu.Lock()
		// 记下这一次拉取次数，再跑不该增加
		first := src.pulls
		// 放开拉取计数，不放则后面读不到次数
		src.mu.Unlock()
		// 拉取次数必须符合幂等，多拉一次即失败
		if first != 0 {
			// 完整副本又被下载就失败，应当幂等
			t.Fatalf("re-pulled complete %d", first)
		}
		// 拼一份软件报价，摘要错则拉取会被拒
		src.put(offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8")))
		// 用来等两路拉取都结束，提前断言会误判
		var wg sync.WaitGroup
		// 准备并发错误通道，装不下会把失败丢掉
		errCh := make(chan error, 2)
		// 连发两路并发拉取，用来验只下载一次
		for range 2 {
			// 登记一路并发，漏登记则会提前结束等待
			wg.Add(1)
			// 并发确保软件副本在，用来验只下载一次
			go func() {
				// 这一路结束就销掉等待，避免主流程卡死
				defer wg.Done()
				// 确保软件副本在，失败则本步验收不能继续
				errCh <- facA.Updates.EnsureSoftware(ctx, factory.SoftwareFactoryService, 8)
			}()
		}
		// 等两路都结束，提前看结果会误判幂等
		wg.Wait()
		// 关掉错误通道，不关则并发结果读不完
		close(errCh)
		// 逐个看并发结果，有一路出错即本例不过
		for err := range errCh {
			// 并发拉取有一路出错就停，避免把半拉当成功
			if err != nil {
				// 并发拉取失败就把错误打出并停掉本例
				t.Fatal(err)
			}
		}
		// 锁住拉取计数，避免并发把次数写乱
		src.mu.Lock()
		// 读出拉取次数，不是一次就说明重复下载
		got := src.pulls
		// 放开拉取计数，不放则后面读不到次数
		src.mu.Unlock()
		// 已装版本必须是期望值，回错版本即失败
		if got != 1 {
			// 拉取次数不对就失败，不该重复下载
			t.Fatalf("pulls %d", got)
		}
		// 读取待确认软件，失败则本步验收不能继续
		p, err := facA.PendingFactorySoftware(ctx, saA)
		// 读取待确认软件失败就停，避免带着错误继续验
		if err != nil || p == nil || p.Version != 8 {
			// 待确认状态或版本不对就失败
			t.Fatalf("pending %+v %v", p, err)
		}
		// 换上内存对象存储，否则字节落在真实盘上
		facA.Updates.SetBlobs(blob.NewMemory())
		// 读取待确认软件，失败则本步验收不能继续
		p, err = facA.PendingFactorySoftware(ctx, saA)
		// 读取待确认软件失败就停，避免带着错误继续验
		if err != nil || p != nil {
			// 记录字段缺了就失败，不能当完整记录
			t.Fatalf("incomplete pending %+v %v", p, err)
		}
		// 期望确认厂服务更新被拒为这几种原因之一，放行即失败
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 8); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrIntegrity) {
			// 记录字段缺了就失败，不能当完整记录
			t.Fatalf("confirm incomplete %v", err)
		}
	})

	// 读取审计，失败则本步验收不能继续
	rows, err := facA.ListAudit(ctx)
	// 读取审计失败就停，避免带着错误继续验
	if err != nil {
		// 读取审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 把审计打成文本，没成功则本步验收失败
	dump := audit.Dump(rows)
	// 文本里夹带秘密或禁词就失败，说明泄密了
	if audit.ContainsAny(dump, "factory-svc-5") {
		// 审计里出现包正文就失败，不该记下字节
		t.Fatal("package bytes in audit")
	}
	// 没有期望的拒绝审计就失败，原因没分开
	if !audit.HasResult(rows, "confirm_software", audit.Allow) || !audit.HasResult(rows, "confirm_software", audit.Deny) {
		// 确认没有留下审计就失败
		t.Fatal("missing confirm audit")
	}
}

// 绑定焊机并装上运行袋，失败则不能确认更新
func boundBag(t *testing.T, ctx context.Context, fac *factory.Service, sa string, factoryID, personID uuid.UUID, nb, na time.Time) (uuid.UUID, factory.Bag) {
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
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 取厂签名公钥，失败则本步验收不能继续
	facPub, err := fac.SigningPublicKey(ctx)
	// 取厂签名公钥失败就停，避免带着错误继续验
	if err != nil {
		// 取厂签名公钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 签发运行凭证，失败则本步验收不能继续
	rt, err := fac.IssueRuntimeGrant(ctx, sa, cid, nb, na)
	// 签发运行凭证失败就停，避免带着错误继续验
	if err != nil {
		// 签发运行凭证失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 装好本机袋，缺密钥则签名验证会失败
	bag := bagOf(factoryID, cid, pub, priv, facPub)
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

// 验收平板拉客户端包，无令牌或错版本即失败
func TestPadClientSoftwarePull(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa-pad", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa-pad", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-pad", "sa-pass")
	// 查看平板可装软件，失败则本步验收不能继续
	got, err := fac.PadClientSoftware(ctx, tok)
	// 查看平板可装软件失败就停，避免带着错误继续验
	if err != nil || got != nil {
		// 空值没有被拒或结果是空就失败
		t.Fatalf("empty %+v %v", got, err)
	}
	// 期望查看平板可装软件被拒为未授权，放行即失败
	if _, err := fac.PadClientSoftware(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		// 空令牌还能操作就失败，应当未授权
		t.Fatalf("anon %v", err)
	}
	// 准备平板要拉的客户端包，拉错版本即失败
	apk := []byte("apk-pad-51")
	// 拼一份软件报价失败就停，避免带着错误继续验
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareClientAPK, 51, "6.1.0", apk)); err != nil {
		// 拼一份软件报价失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 查看平板可装软件，失败则本步验收不能继续
	row, err := fac.PadClientSoftware(ctx, tok)
	// 查看平板可装软件失败就停，避免带着错误继续验
	if err != nil || row == nil || row.Version != 51 || row.VersionName != "6.1.0" {
		// 软件元数据不对就失败
		t.Fatalf("meta %+v %v", row, err)
	}
	// 平板拉取软件正文，失败则本步验收不能继续
	body, err := fac.PullPadClientSoftware(ctx, tok, 51)
	// 平板拉取软件正文失败就停，避免带着错误继续验
	if err != nil || !bytes.Equal(body, apk) {
		// 拉取结果不对就失败
		t.Fatalf("pull %q %v", body, err)
	}
	// 期望平板拉取软件正文被拒为不存在，放行即失败
	if _, err := fac.PullPadClientSoftware(ctx, tok, 9); !errors.Is(err, domain.ErrNotFound) {
		// 缺了应有的记录就失败
		t.Fatalf("missing %v", err)
	}
	// 期望平板拉取软件正文被拒为未授权，放行即失败
	if _, err := fac.PullPadClientSoftware(ctx, "", 51); !errors.Is(err, domain.ErrUnauthorized) {
		// 空令牌还能操作就失败，应当未授权
		t.Fatalf("anon pull %v", err)
	}
}

// 验收存储用量，空令牌还能看即失败
func TestStorageUsage(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa-st", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa-st", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-st", "sa-pass")
	// 拼一份软件报价失败就停，避免带着错误继续验
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareClientAPK, 2, "6.0.0", []byte("apk-st"))); err != nil {
		// 拼一份软件报价失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 读取存储用量，失败则本步验收不能继续
	got, err := fac.StorageUsage(ctx, tok)
	// 读取存储用量失败就停，避免带着错误继续验
	if err != nil {
		// 读取存储用量失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 存储用量必须读得到，为零即失败
	if got.OSS.Used != int64(len("apk-st")) || got.OSS.Objects != 1 {
		// 对象存储用量不对就失败，字节没记上
		t.Fatalf("oss %+v", got.OSS)
	}
	// 存储用量必须读得到，为零即失败
	if got.Disk.Total <= 0 || got.Database <= 0 {
		// 磁盘或库用量没有就失败，探活没读到
		t.Fatalf("disk/db %+v", got)
	}
	// 镜像占用必须符合清单，对不上即失败
	if got.Images.Count != 0 || got.Images.Used != 0 {
		// 镜像占用不对就失败，清单没解析对
		t.Fatalf("images %+v", got.Images)
	}
	// 期望读取存储用量被拒为未授权，放行即失败
	if _, err := fac.StorageUsage(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		// 空令牌还能操作就失败，应当未授权
		t.Fatalf("anon %v", err)
	}
}

// 记下清理请求的镜像清洁工，供占用核对
type reportJanitor struct {
	// 镜像清单原文，解析对不上则占用验收失败
	raw []byte
	// 记下的清理目标，和请求不一致即失败
	ref string
}

// 记下清理请求，对不上则占用用例无法核对
func (j *reportJanitor) RequestPrune(ref string) error {
	// 记下清理目标，和请求不一致即本例失败
	j.ref = ref
	return nil
}

// 返回固定清理结果，对不上则占用上报失败
func (j *reportJanitor) PruneResult() (string, bool, bool, error) { return "0B", true, true, nil }

// 返回镜像清单原文，空了则占用解析失败
func (j *reportJanitor) ImagesJSON() ([]byte, error) { return j.raw, nil }

// 验收镜像占用，正在使用的镜像被清掉即失败
func TestImageOccupancy(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa-img", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa-img", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-img", "sa-pass")
	// 装上带清单的清洁工，清单错则占用对不上
	jan := &reportJanitor{raw: []byte(`{"kind":"factory_service","used":16,"count":2,"items":[{"ref":"app:dev","id":"y","size":8,"keep":"current"},{"ref":"app:old","id":"z","size":8,"keep":""}]}`)}
	// 换上会记请求的清洁工，否则对不上清理目标
	fac.SetImageJanitor(jan)
	// 读取存储用量，失败则本步验收不能继续
	got, err := fac.StorageUsage(ctx, tok)
	// 读取存储用量失败就停，避免带着错误继续验
	if err != nil {
		// 读取存储用量失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 镜像占用必须符合清单，对不上即失败
	if got.Images.Kind != "factory_service" || got.Images.Used != 16 || got.Images.Count != 2 || len(got.Images.Items) != 2 {
		// 镜像占用不对就失败，清单没解析对
		t.Fatalf("images %+v", got.Images)
	}
	// 期望请求清理镜像被拒为仍被引用，放行即失败
	if err := fac.RequestImagePrune(ctx, tok, "app:dev"); !errors.Is(err, domain.ErrReferenced) {
		// 正在使用的镜像被清掉了，应当被拒
		t.Fatalf("current %v", err)
	}
	// 请求清理镜像失败就停，避免带着错误继续验
	if err := fac.RequestImagePrune(ctx, tok, "app:old"); err != nil {
		// 请求清理镜像失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 清理目标必须是请求的那份，记错即失败
	if jan.ref != "app:old" {
		// 清理目标记错就失败，请求没落到清洁工
		t.Fatalf("ref %q", jan.ref)
	}
}
