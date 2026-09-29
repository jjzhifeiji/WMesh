// 阶段5：弱网入队、幂等汇聚、Intent 收敛与上传记录 1.1～6.1。
package service_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 这些编号都要跑到，收尾发现漏跑就判失败。
var matrix05IDs = []string{
	"1.1", "1.2",
	"2.1", "2.2", "2.3",
	"3.1", "3.2",
	"4.1", "4.2", "4.3", "4.4",
	"5.1", "5.2", "5.3", "5.4",
	"6.1",
}

// 按编号跑完弱网汇聚，漏掉编号就判这圈失败。
func TestMatrix05(t *testing.T) {
	// 记下哪些编号已经跑过，收尾用来查漏。
	ran := map[string]bool{}
	// 按编号启动子测试，并记下这个编号已经跑过。
	run := func(id string, fn func(*testing.T)) {
		// 标成测试辅助，失败行号才落到真正的用例。
		t.Helper()
		// 按这个编号真正跑子测试，好让收尾核对有没有漏。
		t.Run(id, func(t *testing.T) {
			// 标上这个编号已经执行，漏跑才能被发现。
			ran[id] = true
			// 执行这条矩阵子测试的本体，失败由它自己报出。
			fn(t)
		})
	}
	// 收尾检查每个矩阵编号都至少跑过一次。
	t.Cleanup(func() {
		// 逐个矩阵编号核对有没有跑过，漏了就不算完成。
		for _, id := range matrix05IDs {
			// 每个矩阵编号都必须实际跑过，漏了就不算完成。
			if !ran[id] {
				// 有编号漏跑就记失败，这圈矩阵覆盖不齐。
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	// 跑完弱网汇聚的全部编号，漏编号由收尾抓住。
	testSyncMatrix(t, run)
}

// 铺好离线包，逐条验收入队、汇聚、收敛和上传。
func testSyncMatrix(t *testing.T, run func(string, func(*testing.T))) {
	// 标成测试辅助，失败行号才落到真正的用例。
	t.Helper()
	// 搭好两厂人员、时钟和权限，作为汇聚用例的场地。
	e := newEnv04(t)
	// 准备点云正文，汇聚之后必须能原样读回。
	cloud := []byte("cloud-body-secret")

	// 绑定一台已授权的客户端并拿到包，没有包不能测。
	cid, bag := e.bound(t, e.op.acc.ID)
	// 允许这个包装资产，否则入队会先被拦住。
	bag.AssetAllowed = true
	// 先当成断网，事实应当只留在包里。
	bag.Connected = false
	// 用直属上下文入队，不把事实挂到车间。
	direct := factory.WorkContext{Direct: true}

	// 留出断网入队的那条事实，连上后再到库里查。
	var fact factory.PendingFact
	// 断网入队只留在包里，厂库里还查不到这条事实。
	run("1.1", func(t *testing.T) {
		// 另开一个错误变量，避免盖住外层的结果。
		var err error
		// 入队一条事实，供后面步骤使用，失败则前提断了。
		fact, err = e.fac.EnqueueFact(e.ctx, &bag, e.clocks, "op-a", "op-pass", direct)
		// 入队一条事实没成功，后面的断言就没有依据。
		if err != nil {
			// 入队一条事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 断网入队后包里必须只留着这一条待发事实。
		if len(bag.PendingFacts) != 1 {
			// 队列条数不对就停，入队没有只留在包里。
			t.Fatalf("pending %d", len(bag.PendingFacts))
		}
		// 还没连上时，厂库必须找不到这条事实。
		if _, err := e.fac.GetFact(e.ctx, e.sa, fact.ID); !errors.Is(err, domain.ErrNotFound) {
			// 断网事实进了厂库就停，弱网边界被打破了。
			t.Fatalf("leaked to store: %v", err)
		}
	})

	// 审计员和跨厂的包都不能把事实放进队列。
	run("1.2", func(t *testing.T) {
		// 绑定一台已授权的客户端并拿到包，没有包不能测。
		_, audBag := e.bound(t, e.aud.acc.ID)
		// 给审计员包打开资产允许，拒绝应来自角色。
		audBag.AssetAllowed = true
		// 审计员的包也当成断网，看它会不会入队。
		audBag.Connected = false
		// 审计员把事实入队必须因越权被拒绝。
		if _, err := e.fac.EnqueueFact(e.ctx, &audBag, e.clocks, "aud-a", "aud-pass", direct); !errors.Is(err, domain.ErrForbidden) {
			// 审计员能入队就停，只读身份被放过了。
			t.Fatalf("auditor: %v", err)
		}
		// 审计员被拒绝后，包里不该还留着待发事实。
		if len(audBag.PendingFacts) != 0 {
			// 拒绝之后队列还有东西就停，入队没有撤干净。
			t.Fatal("auditor queued")
		}

		// 生成一个新标识，用来当目录、资产或客户端。
		cidB := id.New()
		// 生成节点密钥对，失败则后面没有私钥可装。
		pub, priv, err := nodekey.Generate()
		// 生成节点密钥没成功，后面的断言就没有依据。
		if err != nil {
			// 生成节点密钥失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 登记客户端绑定没成功，后面的断言就没有依据。
		if _, err := e.facB.AcceptBinding(e.ctx, cidB, "焊机-B", pub, 1); err != nil {
			// 登记客户端绑定失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读取厂签名公钥，供后面步骤使用，失败则前提断了。
		facPub, err := e.facB.SigningPublicKey(e.ctx)
		// 读取厂签名公钥没成功，后面的断言就没有依据。
		if err != nil {
			// 读取厂签名公钥失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 签发运行凭证，供后面步骤使用，失败则前提断了。
		rt, err := e.facB.IssueRuntimeGrant(e.ctx, e.saB, cidB, e.nb, e.na)
		// 签发运行凭证没成功，后面的断言就没有依据。
		if err != nil {
			// 签发运行凭证失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bagB := bagOf(e.seedB.ID, cidB, pub, priv, facPub)
		// 乙厂的包保持断网，用来试跨厂入队。
		bagB.Connected = false
		// 乙厂的包允许装资产，拒绝应当来自跨厂。
		bagB.AssetAllowed = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bagB.ApplyRuntime(rt)
		// 取出乙厂操作员的标识，装进包里当当前的人。
		oid := e.peB.acc.ID
		// 把乙厂的人写进包里，这次入队应当被拒绝。
		bagB.OperatorID = &oid
		// 拿乙厂的包向甲厂入队必须因越权被拒绝。
		if _, err := e.fac.EnqueueFact(e.ctx, &bagB, e.clocks, "pe-b", "pe-b-pass", direct); !errors.Is(err, domain.ErrForbidden) {
			// 跨厂包能入队就停，两厂的边界没有守住。
			t.Fatalf("cross factory: %v", err)
		}
	})

	// 连上之后队列汇进厂库，而且不带组织。
	run("2.1", func(t *testing.T) {
		// 改成已连接，重连时才会把队列汇进厂库。
		bag.Connected = true
		// 重连并汇聚队列没成功，后面的断言就没有依据。
		if err := e.fac.Reconnect(e.ctx, &bag); err != nil {
			// 重连并汇聚队列失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 连上并汇聚之后，包里的待发队列必须清空。
		if len(bag.PendingFacts) != 0 {
			// 队列还在就停，汇聚没有把事实收进厂库。
			t.Fatal("queue not empty")
		}
		// 按标识读事实，供后面步骤使用，失败则前提断了。
		got, err := e.fac.GetFact(e.ctx, e.sa, fact.ID)
		// 按标识读事实没成功，后面的断言就没有依据。
		if err != nil {
			// 按标识读事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 入库的事实必须属于这个操作员，且不带组织。
		if got.CreatorID != e.op.acc.ID || got.OrgUnitID != nil {
			// 创建人或组织不对就停，入队时的上下文错了。
			t.Fatalf("got %+v", got)
		}
	})

	// 同一条再汇一次不能变成两行，路径也不能改。
	run("2.2", func(t *testing.T) {
		// 把已有记录再放进待发队列，用来试重复汇聚。
		bag.PendingFacts = append(bag.PendingFacts, fact)
		// 汇聚待发队列没成功，后面的断言就没有依据。
		if err := e.fac.FlushPending(e.ctx, &bag); err != nil {
			// 汇聚待发队列失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按创建人列事实，供后面步骤使用，失败则前提断了。
		listed, err := e.fac.ListFactsByCreator(e.ctx, e.sa, e.op.acc.ID)
		// 按创建人列事实没成功，后面的断言就没有依据。
		if err != nil {
			// 按创建人列事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 准备数这条事实在库里一共出现了几次。
		n := 0
		// 在已入库的事实里数这一条到底出现了几次。
		for _, row := range listed {
			// 数到同一条事实时，接着看组织有没有被改掉。
			if row.ID == fact.ID {
				// 命中同一条就加一，用来查有没有重复行。
				n++
				// 同一条再次出现时，组织必须仍然是空的。
				if row.OrgUnitID != nil {
					// 路径被改掉就停，重复汇聚把快照写坏了。
					t.Fatalf("path changed %+v", row)
				}
			}
		}
		// 同一条事实在厂库里只能留下一行。
		if n != 1 {
			// 出现多行就停，重复汇聚没有做成幂等。
			t.Fatalf("dup %d", n)
		}
	})

	// 改了路径再汇必须被拒，库里的原事实保持原样。
	run("2.3", func(t *testing.T) {
		// 复制一条事实再改路径，用来试篡改之后的汇聚。
		bad := fact
		// 生成一个新标识，用来当目录、资产或客户端。
		nid := id.New()
		// 给副本安上不存在的组织，完整性应当对不上。
		bad.OrgUnitID = &nid
		// 配上一条假路径，汇聚必须因完整性被拒绝。
		bad.OrgPath = []factory.PathNode{{ID: nid, Name: "假"}}
		// 把已有记录再放进待发队列，用来试重复汇聚。
		bag.PendingFacts = append(bag.PendingFacts, bad)
		// 篡改路径之后再汇聚，必须因完整性被拒绝。
		if err := e.fac.FlushPending(e.ctx, &bag); !errors.Is(err, domain.ErrIntegrity) {
			// 脏路径能汇进去就停，完整性没有挡住。
			t.Fatalf("got %v", err)
		}
		// 按标识读事实，供后面步骤使用，失败则前提断了。
		got, err := e.fac.GetFact(e.ctx, e.sa, fact.ID)
		// 按标识读事实没成功，后面的断言就没有依据。
		if err != nil {
			// 按标识读事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 脏汇聚被拒后，库里的原事实不能带上假组织。
		if got.OrgUnitID != nil {
			// 原事实被改写就停，失败的汇聚没有保持原样。
			t.Fatalf("overwritten %+v", got)
		}
		// 清掉待发队列，避免脏数据带进下一条。
		bag.PendingFacts = nil
	})

	// 留出这次失败汇聚的事实，重试之后要能入库。
	var retry factory.PendingFact
	// 汇聚失败时队列还在，厂库里仍然没有这条。
	run("3.1", func(t *testing.T) {
		// 再次当成断网，新的事实应当只进队列。
		bag.Connected = false
		// 另开一个错误变量，避免盖住外层的结果。
		var err error
		// 入队一条事实，供后面步骤使用，失败则前提断了。
		retry, err = e.fac.EnqueueFact(e.ctx, &bag, e.clocks, "op-a", "op-pass", direct)
		// 入队一条事实没成功，后面的断言就没有依据。
		if err != nil {
			// 入队一条事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 改成在线，但这次汇聚会被夹具打成失败。
		bag.Connected = true
		// 让这次汇聚失败，队列必须原样留着。
		bag.FailFlush = true
		// 夹具让汇聚失败时，必须返回可重试而不是把队列丢掉。
		if err := e.fac.FlushPending(e.ctx, &bag); !errors.Is(err, domain.ErrSyncRetry) {
			// 失败的结果不是可重试就停，队列策略错了。
			t.Fatalf("got %v", err)
		}
		// 汇聚失败时，队列里必须还留着那一条事实。
		if len(bag.PendingFacts) != 1 {
			// 失败后队列丢了就停，重试没有材料了。
			t.Fatalf("queue %d", len(bag.PendingFacts))
		}
		// 汇聚失败时厂库必须仍然找不到这条事实。
		if _, err := e.fac.GetFact(e.ctx, e.sa, retry.ID); !errors.Is(err, domain.ErrNotFound) {
			// 失败却已经入库就停，半截事实落了地。
			t.Fatalf("stored on fail: %v", err)
		}
	})

	// 去掉失败标记后再汇，队列清空而且库里能读到。
	run("3.2", func(t *testing.T) {
		// 关掉失败夹具，这次汇聚应当成功。
		bag.FailFlush = false
		// 汇聚待发队列没成功，后面的断言就没有依据。
		if err := e.fac.FlushPending(e.ctx, &bag); err != nil {
			// 汇聚待发队列失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 重试成功之后，待发队列必须被清空。
		if len(bag.PendingFacts) != 0 {
			// 队列还在就停，成功汇聚没有把事实收完。
			t.Fatal("queue")
		}
		// 按标识读事实没成功，后面的断言就没有依据。
		if _, err := e.fac.GetFact(e.ctx, e.sa, retry.ID); err != nil {
			// 按标识读事实失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
	})

	// 发布一条工程，后面把它授权并下发到客户端。
	proj := e.pubProj(t, "同步工程", []byte("sync-proj"), nil)
	// 授权客户端收工程没成功，后面的断言就没有依据。
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
		// 授权客户端收工程失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把工程下发到包里没成功，后面的断言就没有依据。
	if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
		// 把工程下发到包里失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 收回授权后在线不能激活，离线仍可以先记上。
	run("4.4", func(t *testing.T) {
		// 收回工程授权没成功，后面的断言就没有依据。
		if err := e.fac.RevokeClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			// 收回工程授权失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 改回在线，收回授权之后激活必须被拒绝。
		bag.Connected = true
		// 收回授权后，在线激活工程必须因越权被拒绝。
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); !errors.Is(err, domain.ErrForbidden) {
			// 收回后在线还能激活就停，授权还活着。
			t.Fatalf("online after revoke: %v", err)
		}
		// 改成离线，这时激活应当先记在包上。
		bag.Connected = false
		// 激活已下发工程没成功，后面的断言就没有依据。
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); err != nil {
			// 激活已下发工程失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 清掉已激活的工程，避免影响后面的收敛。
		bag.ActiveID = nil
		// 把修订也清掉，包回到还没激活工程的状态。
		bag.ActiveRevision = 0
		// 再标成在线，供停用之后的收敛使用。
		bag.Connected = true
	})

	// 绑定一台已授权的客户端并拿到包，没有包不能测。
	_, bagOff := e.bound(t, e.op.acc.ID)
	// 离线包允许装资产，拒绝应当来自没有连上。
	bagOff.AssetAllowed = true
	// 保持断网，这时做意图收敛必须被拒绝。
	bagOff.Connected = false

	// 断网不能做意图收敛，但原来的凭证仍可登录。
	run("4.2", func(t *testing.T) {
		// 断网时做意图收敛必须因越权被拒绝。
		if err := e.fac.ConvergeIntent(e.ctx, &bagOff); !errors.Is(err, domain.ErrForbidden) {
			// 离线还能收敛就停，没连上却改了节点意图。
			t.Fatalf("offline converge: %v", err)
		}
		// 离线尝试登录，供后面步骤使用，失败则前提断了。
		login, err := e.fac.LoginOffline(e.ctx, bagOff, e.clocks, "op-a", "op-pass")
		// 离线收敛被拒后，原凭证登录必须仍然放行。
		if err != nil || login.Decision != factory.NodeAllow {
			// 登录被拒绝就停，没做成的收敛误伤了凭证。
			t.Fatalf("still active %+v %v", login, err)
		}
	})

	// 停用操作员并收敛之后，离线操作必须被拒绝。
	run("4.1", func(t *testing.T) {
		// 停用这个账号没成功，后面的断言就没有依据。
		if err := e.fac.DisableAccount(e.ctx, e.sa, e.op.acc.ID); err != nil {
			// 停用这个账号失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 收敛节点意图没成功，后面的断言就没有依据。
		if err := e.fac.ConvergeIntent(e.ctx, &bag); err != nil {
			// 收敛节点意图失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 评估离线操作，供后面步骤使用，失败则前提断了。
		ev, err := e.fac.EvaluateOfflineOp(e.ctx, bag, e.clocks, "op-a", "op-pass")
		// 停用并收敛之后，离线操作必须被拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 停用后还能操作就停，账号状态没有收敛到包上。
			t.Fatalf("still allowed %+v %v", ev, err)
		}
	})

	// 停用之后再拿原凭证离线登录，必须被拒绝。
	run("4.3", func(t *testing.T) {
		// 离线尝试登录，供后面步骤使用，失败则前提断了。
		login, err := e.fac.LoginOffline(e.ctx, bag, e.clocks, "op-a", "op-pass")
		// 停用之后再离线登录必须被拒绝。
		if err != nil || login.Decision != factory.NodeDeny {
			// 停用后还能登录就停，停用没有作用到包上。
			t.Fatalf("still disabled %+v %v", login, err)
		}
	})

	// 绑定一台已授权的客户端并拿到包，没有包不能测。
	cidU, bagU := e.bound(t, e.pe.acc.ID)
	// 上传用的包允许装资产，操作员可以入队。
	bagU.AssetAllowed = true
	// 上传的包先断网，正文应当先留在包里。
	bagU.Connected = false
	// 留出这条待上传，连上之后按它到库里找。
	var up factory.PendingUpload
	// 断网入队的上传在连上之后入库，正文还一致。
	run("5.1", func(t *testing.T) {
		// 另开一个错误变量，避免盖住外层的结果。
		var err error
		// 入队一条上传，供后面步骤使用，失败则前提断了。
		up, err = e.fac.EnqueueUpload(e.ctx, &bagU, e.clocks, "pe-a", "pe-pass", factory.UploadPointCloud, cloud)
		// 入队一条上传没成功，后面的断言就没有依据。
		if err != nil {
			// 入队一条上传失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 改成已连接，重连应当把上传汇进厂库。
		bagU.Connected = true
		// 重连并汇聚队列没成功，后面的断言就没有依据。
		if err := e.fac.Reconnect(e.ctx, &bagU); err != nil {
			// 重连并汇聚队列失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按标识读上传，供后面步骤使用，失败则前提断了。
		rec, err := e.fac.GetUpload(e.ctx, e.sa, up.ID)
		// 按标识读上传没成功，后面的断言就没有依据。
		if err != nil {
			// 按标识读上传失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 入库的上传必须是点云，而且来自这台客户端。
		if rec.Kind != factory.UploadPointCloud || rec.ClientID != cidU {
			// 种类或客户端不对就停，这条上传记错了。
			t.Fatalf("%+v", rec)
		}
		// 读出上传正文，供后面步骤使用，失败则前提断了。
		body, err := e.fac.ReadUploadContent(e.ctx, e.sa, up.ID)
		// 读出上传正文没成功，后面的断言就没有依据。
		if err != nil {
			// 读出上传正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读回的上传正文必须和入队时的点云一致。
		if !bytes.Equal(body, cloud) {
			// 正文对不上就停，汇聚把上传内容换掉了。
			t.Fatal("body mismatch")
		}
	})

	// 同一条上传再汇一次，不能另外插入新的一行。
	run("5.2", func(t *testing.T) {
		// 把已有记录再放进待发队列，用来试重复汇聚。
		bagU.PendingUploads = append(bagU.PendingUploads, up)
		// 汇聚待发队列没成功，后面的断言就没有依据。
		if err := e.fac.FlushPending(e.ctx, &bagU); err != nil {
			// 汇聚待发队列失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按标识读上传，供后面步骤使用，失败则前提断了。
		rec, err := e.fac.GetUpload(e.ctx, e.sa, up.ID)
		// 按标识读上传没成功，后面的断言就没有依据。
		if err != nil {
			// 按标识读上传失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 同一条再汇一次，必须还是原来那一行记录。
		if rec.ID != up.ID {
			// 又插入新行就停，上传汇聚不是幂等的。
			t.Fatal("new row")
		}
	})

	// 正文被换掉再汇必须被拒，库里仍是原来的点云。
	run("5.3", func(t *testing.T) {
		// 复制这条上传，准备换成另一份正文再汇一次。
		bad := up
		// 准备一段字节样本，后面的比对要以它为准。
		bad.Content = []byte("other-cloud")
		// 按正文计算摘要，依赖和收包都要拿它对上。
		bad.Digest = digest.Sum(bad.Content)
		// 把已有记录再放进待发队列，用来试重复汇聚。
		bagU.PendingUploads = append(bagU.PendingUploads, bad)
		// 正文被换掉之后再汇，必须因完整性被拒绝。
		if err := e.fac.FlushPending(e.ctx, &bagU); !errors.Is(err, domain.ErrIntegrity) {
			// 换过的正文能汇进去就停，摘要没有对上。
			t.Fatalf("got %v", err)
		}
		// 读出上传正文，供后面步骤使用，失败则前提断了。
		body, err := e.fac.ReadUploadContent(e.ctx, e.sa, up.ID)
		// 读出上传正文没成功，后面的断言就没有依据。
		if err != nil {
			// 读出上传正文失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 脏正文被拒后，库里必须仍是原来的点云。
		if !bytes.Equal(body, cloud) {
			// 原文被换掉就停，失败的汇聚写进了库。
			t.Fatal("original replaced")
		}
		// 清掉待传队列，避免脏正文带到后面。
		bagU.PendingUploads = nil
	})

	// 超管和审计员都不能把上传放进队列。
	run("5.4", func(t *testing.T) {
		// 绑定一台已授权的客户端并拿到包，没有包不能测。
		_, saBag := e.bound(t, e.seed.SuperAdminID)
		// 超管的包也打开资产允许，拒绝应来自角色。
		saBag.AssetAllowed = true
		// 超管把上传入队必须因越权被拒绝。
		if _, err := e.fac.EnqueueUpload(e.ctx, &saBag, e.clocks, "sa-a", "sa-pass", factory.UploadImage, []byte("img")); !errors.Is(err, domain.ErrForbidden) {
			// 超管能入队上传就停，这个角色不该传文件。
			t.Fatalf("sa: %v", err)
		}
		// 绑定一台已授权的客户端并拿到包，没有包不能测。
		_, audBag := e.bound(t, e.aud.acc.ID)
		// 审计员的包打开资产允许，入队上传仍应被拒绝。
		audBag.AssetAllowed = true
		// 审计员把上传入队必须因越权被拒绝。
		if _, err := e.fac.EnqueueUpload(e.ctx, &audBag, e.clocks, "aud-a", "aud-pass", factory.UploadImage, []byte("img")); !errors.Is(err, domain.ErrForbidden) {
			// 审计员能入队上传就停，只读被放开了。
			t.Fatalf("aud: %v", err)
		}
	})

	// 入队汇聚收敛上传都有允许和拒绝审计，且不泄密。
	run("6.1", func(t *testing.T) {
		// 拉取审计记录，供后面步骤使用，失败则前提断了。
		rows, err := e.fac.ListAudit(e.ctx)
		// 拉取审计记录没成功，后面的断言就没有依据。
		if err != nil {
			// 拉取审计记录失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 入队事实必须同时留下允许和拒绝的审计。
		if !hasAction(rows, "enqueue_fact", audit.Allow) || !hasAction(rows, "enqueue_fact", audit.Deny) {
			// 入队审计缺允许或缺拒绝就停，痕迹不齐。
			t.Fatal("enqueue_fact traces")
		}
		// 汇聚必须同时留下允许和拒绝的审计。
		if !hasAction(rows, "sync_flush", audit.Allow) || !hasAction(rows, "sync_flush", audit.Deny) {
			// 汇聚审计不齐就停，成败没有都留下。
			t.Fatal("sync_flush traces")
		}
		// 意图收敛必须同时留下允许和拒绝的审计。
		if !hasAction(rows, "converge_intent", audit.Allow) || !hasAction(rows, "converge_intent", audit.Deny) {
			// 收敛审计不齐就停，被拒绝的那次没有记下。
			t.Fatal("converge traces")
		}
		// 入队上传必须同时留下允许和拒绝的审计。
		if !hasAction(rows, "enqueue_upload", audit.Allow) || !hasAction(rows, "enqueue_upload", audit.Deny) {
			// 上传审计不齐就停，越权的那次没有痕迹。
			t.Fatal("upload traces")
		}
		// 把审计行拼成文本，用来搜修订号或秘密。
		dump := audit.Dump(rows)
		// 审计文本里不能出现口令、正文或上传内容。
		if audit.ContainsAny(dump, "op-pass", "pe-pass", "sa-pass", "aud-pass", string(cloud)) {
			// 审计里出现秘密就停，日志把内容写出去了。
			t.Fatal("secret leaked")
		}
		// 审计里不能出现节点私钥的十六进制文本。
		if bag.PrivateKey != nil && audit.ContainsAny(dump, hex.EncodeToString(bag.PrivateKey)) {
			// 审计出现私钥就停，密钥被写进了日志。
			t.Fatal("key leaked")
		}
	})
}

// 在审计里查找是否有这条动作和结果。
func hasAction(rows []audit.Row, action, result string) bool {
	// 逐行查看审计，确认动作和结果是否齐。
	for _, r := range rows {
		// 动作和结果都对上，才算审计里有这一条。
		if r.Action == action && r.Result == result {
			return true
		}
	}
	return false
}
