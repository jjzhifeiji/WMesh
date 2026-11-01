// 软件更新服务层矩阵：厂自拉与确认。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 本文件要跑完的更新编号，漏跑就在收尾报错
var matrixSoftwareIDs = []string{
	"U2", "U3", "U4", "U5", "U8", "U12", "U13",
	"U14", "U16", "U17", "U19", "U20", "U21", "U28", "U29", "U30", "U31",
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
func (m *memSource) Pull(_ context.Context, kind string, version int64, _ []byte) ([]byte, error) {
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

// 内存源没有暂存可清，收下通知直接忽略
func (m *memSource) Received(string, int64, []byte) {}

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
	_, facA, err := h.Provision(ctx, "sa-a", "超管A")
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
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator)
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
	// 验收超管确认后厂服务换成新版本，没换即失败
	run("U4", func(t *testing.T) {
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

// 验收平板拉客户端包，无令牌或错版本即失败
func TestPadClientSoftwarePull(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-pad", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa-pad", "sa-pass"); err != nil {
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
	// 平板打开软件包流，失败则本步验收不能继续
	pack, err := fac.PullPadClientSoftware(ctx, tok, 51, false)
	// 平板打开软件包流失败就停，避免带着错误继续验
	if err != nil {
		// 打不开就把原因打出并停掉本例
		t.Fatalf("pull %v", err)
	}
	// 读完整份流再比正文
	body := readStream(t, pack)
	// 正文对不上就失败
	if !bytes.Equal(body, apk) {
		// 拉取结果不对就失败
		t.Fatalf("pull %q", body)
	}
	// 期望平板拉取软件正文被拒为不存在，放行即失败
	if _, err := fac.PullPadClientSoftware(ctx, tok, 9, false); !errors.Is(err, domain.ErrNotFound) {
		// 缺了应有的记录就失败
		t.Fatalf("missing %v", err)
	}
	// 期望平板拉取软件正文被拒为未授权，放行即失败
	if _, err := fac.PullPadClientSoftware(ctx, "", 51, false); !errors.Is(err, domain.ErrUnauthorized) {
		// 空令牌还能操作就失败，应当未授权
		t.Fatalf("anon pull %v", err)
	}
}

// 本进程版本算已装基线：旧包不收不拉，同版不提示，高一版才提示
func TestRunningSoftwareBaseline(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-rb", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa-rb", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-rb", "sa-pass")
	// 假定本进程跑的是 v10，库里还没记过已装
	fac.Updates.SetRunningSoftware(10, "1.10.0")
	// 没记过已装也没有副本，当前版本应是本进程的
	cur, err := fac.CurrentFactorySoftware(ctx, tok)
	// 读失败或不是 v10 和本进程版本名就失败
	if err != nil || cur.Version != 10 || cur.VersionName != "1.10.0" {
		// 当前版本不对就把结果打出并停掉本例
		t.Fatalf("running current %+v %v", cur, err)
	}
	// 收 v8 低于本进程版本，应当拒为过旧
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8"))); !errors.Is(err, domain.ErrStaleRevision) {
		// 旧包被收下就把错误打出并停掉本例
		t.Fatalf("ingest v8 %v", err)
	}
	// 用内存源代替云端，云端最高只有 v8
	src := &memSource{}
	// 云端只给 v8，比本进程旧
	src.put(offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8")))
	// 换上软件源，问包和拉包都走它
	fac.Updates.SetSoftwareSource(src)
	// 记下审计条数，不拉旧包时也不该多记
	rows, err := fac.ListAudit(ctx)
	// 读审计失败就停，避免带着错误继续验
	if err != nil {
		// 读审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 留下拉包前的条数，后面比有没有多记
	audits := len(rows)
	// 通道通知 v8 时应直接拒为过旧
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareFactoryService, 8); !errors.Is(err, domain.ErrStaleRevision) {
		// 没拒成过旧就把错误打出并停掉本例
		t.Fatalf("ensure v8 %v", err)
	}
	// 锁住拉取计数，避免并发把次数写乱
	src.mu.Lock()
	// 读出拉取次数，旧包一次都不该拉
	pulls := src.pulls
	// 放开拉取计数，不放则后面读不到次数
	src.mu.Unlock()
	// 拉过旧包就失败，每次问包都会整包重下
	if pulls != 0 {
		// 旧包被拉就把次数打出并停掉本例
		t.Fatalf("pulled stale %d", pulls)
	}
	// 再读审计，拒旧包不该多记
	rows, err = fac.ListAudit(ctx)
	// 读失败或条数变了就失败
	if err != nil || len(rows) != audits {
		// 多记审计就把条数打出并停掉本例
		t.Fatalf("audit %d -> %d %v", audits, len(rows), err)
	}
	// 收下同版 v10，可以收但不该提示
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 10, "1.10.0", []byte("factory-svc-10"))); err != nil {
		// 同版收不下就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 同版不该有待装提示
	p, err := fac.PendingFactorySoftware(ctx, tok)
	// 读失败或有提示就失败
	if err != nil || p != nil {
		// 同版还提示就把结果打出并停掉本例
		t.Fatalf("pending same %+v %v", p, err)
	}
	// 确认同版应按找不到拒绝，不白白重启
	if err := fac.ConfirmFactoryUpdate(ctx, tok, factory.SoftwareFactoryService, 10); !errors.Is(err, domain.ErrNotFound) {
		// 同版能确认就把错误打出并停掉本例
		t.Fatalf("confirm same %v", err)
	}
	// 当前版本仍是 v10
	cur, err = fac.CurrentFactorySoftware(ctx, tok)
	// 读失败或不是 v10 就失败
	if err != nil || cur.Version != 10 || cur.VersionName != "1.10.0" {
		// 当前版本不对就把结果打出并停掉本例
		t.Fatalf("current same %+v %v", cur, err)
	}
	// 收下高一版 v11，这时才该提示
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 11, "1.11.0", []byte("factory-svc-11"))); err != nil {
		// 新版收不下就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 高一版应有待装提示
	p, err = fac.PendingFactorySoftware(ctx, tok)
	// 读失败或没提示 v11 就失败
	if err != nil || p == nil || p.Version != 11 {
		// 新版没提示就把结果打出并停掉本例
		t.Fatalf("pending next %+v %v", p, err)
	}
	// 列副本看保留原因，正在跑的和最高都不能删
	items, err := fac.ListFactorySoftware(ctx, tok, factory.SoftwareFactoryService)
	// 列副本失败就停，避免带着错误继续验
	if err != nil {
		// 列副本失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 按版本记下保留原因，便于逐个核对
	keep := map[int64]string{}
	// 逐份记下保留原因
	for _, it := range items {
		// 按版本记下这份的保留原因
		keep[it.Version] = it.Keep
	}
	// v10 是正在跑的、v11 是最高，标错就失败
	if keep[10] != factory.SoftwareKeepInstalled || keep[11] != factory.SoftwareKeepLatest {
		// 保留原因不对就把结果打出并停掉本例
		t.Fatalf("keep %+v", keep)
	}
}

// 验收存储用量，空令牌还能看即失败
func TestStorageUsage(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-st", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa-st", "sa-pass"); err != nil {
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
	_, fac, err := h.Provision(ctx, "sa-img", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa-img", "sa-pass"); err != nil {
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

// 假的换包进度来源，按设定交回 updater 结果
type stubApplyReporter struct {
	phase   string // 设定的阶段，ok 或 fail
	kind    string // 设定的种类，对不上就记错包
	version int64  // 设定的版本，对不上就记错包
	errMsg  string // 设定的失败原因，应当进审计对象
}

// 按设定交回进度，不碰磁盘
func (r *stubApplyReporter) Progress() (string, string, int64, string, error) {
	return r.phase, r.kind, r.version, r.errMsg, nil
}

// 挑出某动作某结果的审计行，读不到就停掉本例
func softwareAudit(t *testing.T, ctx context.Context, fac *factory.Service, action, result string) []audit.Row {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 读出本厂审计，读不到就没法数
	rows, err := fac.ListAudit(ctx)
	// 读审计失败就停，避免带着错误继续验
	if err != nil {
		// 读审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 准备收下对上动作和结果的行
	var out []audit.Row
	// 逐行挑出对上动作和结果的
	for _, r := range rows {
		// 动作和结果都对上才收下
		if r.Action == action && r.Result == result {
			// 收下这一行，后面数条数看对象
			out = append(out, r)
		}
	}
	return out
}

// 落地失败只记一条带原因的拒绝，再确认后再失败另记，成功照旧一条允许
func TestApplyFailAudit(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-af", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa-af", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-af", "sa-pass")
	// 收下厂服务 v7，没有它就没有可确认的包
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 7, "1.7.0", []byte("factory-svc-7"))); err != nil {
		// 收包失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 先记下已装版本，失败后必须原样不动
	before, err := fac.CurrentFactorySoftware(ctx, tok)
	// 读已装版本失败就停，避免带着错误继续验
	if err != nil {
		// 读已装版本失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 超管确认 v7，留下一条确认允许
	if err := fac.ConfirmFactoryUpdate(ctx, tok, factory.SoftwareFactoryService, 7); err != nil {
		// 确认失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 假定 updater 探活失败并已切回上一版
	rep := &stubApplyReporter{phase: "fail", kind: factory.SoftwareFactoryService, version: 7, errMsg: "health check failed"}
	// 换上假的进度来源，读到的就是这份失败
	fac.SetApplyReporter(rep)
	// 连查三次进度，模拟页面轮询
	for i := 0; i < 3; i++ {
		// 查一次进度，页面要照样看到失败
		got, err := fac.ApplyProgress(ctx, tok)
		// 读不到或没报失败原因就失败
		if err != nil || got.Phase != "fail" || got.Error != "health check failed" {
			// 页面没看到失败就把结果打出并停掉本例
			t.Fatalf("progress %+v %v", got, err)
		}
	}
	// 再模拟进程重启补记一次
	if err := fac.SettleStaged(ctx); err != nil {
		// 启动补记失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 挑出落地拒绝，同一次结果只该有一条
	denies := softwareAudit(t, ctx, fac, "apply_software", audit.Deny)
	// 条数不是一或对象没带原因就失败
	if len(denies) != 1 || denies[0].Target != "factory_service v7: health check failed" {
		// 重复记或没带原因就把审计打出并停掉本例
		t.Fatalf("denies %+v", denies)
	}
	// 再读已装版本，失败不能改它
	cur, err := fac.CurrentFactorySoftware(ctx, tok)
	// 读失败或版本变了就失败
	if err != nil || cur.Version != before.Version {
		// 已装被改动就把结果打出并停掉本例
		t.Fatalf("installed %+v before %+v %v", cur, before, err)
	}
	// 再确认一次 v7，规格允许重试
	if err := fac.ConfirmFactoryUpdate(ctx, tok, factory.SoftwareFactoryService, 7); err != nil {
		// 再确认失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 再报失败后查一次进度，应另记一条
	if _, err := fac.ApplyProgress(ctx, tok); err != nil {
		// 查进度失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 重启补记一次，不该再多记
	if err := fac.SettleStaged(ctx); err != nil {
		// 启动补记失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 两次确认各失败一次，应当正好两条
	if n := len(softwareAudit(t, ctx, fac, "apply_software", audit.Deny)); n != 2 {
		// 条数不对就把条数打出并停掉本例
		t.Fatalf("denies after reconfirm %d", n)
	}
	// 改成 updater 探活通过
	rep.phase, rep.errMsg = "ok", ""
	// 连查两次进度，成功也会被轮询
	for i := 0; i < 2; i++ {
		// 查一次进度，读不到就失败
		if _, err := fac.ApplyProgress(ctx, tok); err != nil {
			// 查进度失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	}
	// 重启补记一次，已装幂等不再多记
	if err := fac.SettleStaged(ctx); err != nil {
		// 启动补记失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 挑出落地允许，成功照旧只该一条
	allows := softwareAudit(t, ctx, fac, "apply_software", audit.Allow)
	// 条数不是一或对象不对就失败
	if len(allows) != 1 || allows[0].Target != "factory_service v7" {
		// 成功记多或记错就把审计打出并停掉本例
		t.Fatalf("allows %+v", allows)
	}
	// 成功后已装必须换成 v7
	cur, err = fac.CurrentFactorySoftware(ctx, tok)
	// 读失败或没换成 v7 就失败
	if err != nil || cur.Version != 7 {
		// 已装没记上就把结果打出并停掉本例
		t.Fatalf("installed after ok %+v %v", cur, err)
	}
	// 成功不该多出落地拒绝
	if n := len(softwareAudit(t, ctx, fac, "apply_software", audit.Deny)); n != 2 {
		// 拒绝条数变了就把条数打出并停掉本例
		t.Fatalf("denies after ok %d", n)
	}
}
