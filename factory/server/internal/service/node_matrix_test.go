// 阶段2第4圈：厂内侧节点凭证 1.1～10.1。
package service_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 铺好两厂绑定，逐条验收凭证、吊销和时钟。
func testNodeMatrix(t *testing.T, run func(string, func(*testing.T))) {
	// 标成测试辅助，失败行号才落到真正的用例。
	t.Helper()
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()

	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 口令登录，供后面步骤使用，失败则前提断了。
	saTok, err := facA.Login(ctx, "sa-a", "sa-pass")
	// 口令登录没成功，后面的断言就没有依据。
	if err != nil {
		// 口令登录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sb-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 取当前时刻，作为节点钟面和凭证窗口的基准。
	now := time.Now().UTC()
	// 服务器和本机都用现在，表示还在有效窗口里。
	valid := factory.Clocks{Server: now, Local: now}
	// 划出一小时前到一天后，作为凭证有效窗口。
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	// 生成一个新标识，用来当目录、资产或客户端。
	cidA := id.New()
	// 生成节点密钥对，失败则后面没有私钥可装。
	pubA, privA, err := nodekey.Generate()
	// 生成节点密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 生成节点密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登记客户端绑定没成功，后面的断言就没有依据。
	if _, err := facA.AcceptBinding(ctx, cidA, "Client-A1", pubA, 1); err != nil {
		// 登记客户端绑定失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读取厂签名公钥，供后面步骤使用，失败则前提断了。
	facPubA, err := facA.SigningPublicKey(ctx)
	// 读取厂签名公钥没成功，后面的断言就没有依据。
	if err != nil {
		// 读取厂签名公钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 留出第一张凭证，后面用来试旧证不能回滚。
	var cred1 factory.RuntimeCred
	// 已绑定节点拿到凭证后，开放评估应当放行。
	run("1.1", func(t *testing.T) {
		// 另开一个错误变量，避免盖住外层的结果。
		var err error
		// 签发运行凭证，供后面步骤使用，失败则前提断了。
		cred1, err = facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		// 签发运行凭证没成功，后面的断言就没有依据。
		if err != nil {
			// 签发运行凭证失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 持有当前有效凭证时，开放评估必须放行。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 有效凭证被拒绝就停，签发没有生效。
			t.Fatalf("eval %+v %v", ev, err)
		}
	})
	// 旧凭证不能覆盖更新的修订，评估仍然放行。
	run("1.2", func(t *testing.T) {
		// 签发运行凭证，供后面步骤使用，失败则前提断了。
		cred2, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		// 签发运行凭证没成功，后面的断言就没有依据。
		if err != nil {
			// 签发运行凭证失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred2)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 后装上去的旧凭证不能把已接受的修订盖掉。
		if bag.AcceptedRevision != cred2.Revision || bag.Runtime.Revision != cred2.Revision {
			// 旧修订盖掉新的就停，包接受了一次回滚。
			t.Fatalf("stale overwrite %d", bag.AcceptedRevision)
		}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 持有当前有效凭证时，开放评估必须放行。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 有效凭证被拒绝就停，签发没有生效。
			t.Fatalf("eval %+v %v", ev, err)
		}
	})

	// 没有绑定过的标识不能签发运行凭证。
	run("2.1", func(t *testing.T) {
		// 给没有绑定过的标识签发必须因找不到被拒绝。
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, id.New(), nb, na); !errors.Is(err, domain.ErrNotFound) {
			// 未绑定也能签发就停，节点可以凭空出现。
			t.Fatalf("unbound: %v", err)
		}
	})
	// 生成一个新标识，用来当目录、资产或客户端。
	cidB := id.New()
	// 生成节点密钥对，失败则后面没有私钥可装。
	pubB, privB, err := nodekey.Generate()
	// 生成节点密钥没成功，后面的断言就没有依据。
	if err != nil {
		// 生成节点密钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登记客户端绑定没成功，后面的断言就没有依据。
	if _, err := facB.AcceptBinding(ctx, cidB, "Client-B1", pubB, 1); err != nil {
		// 登记客户端绑定失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 不能用本厂会话给另一家厂的节点签发。
	run("2.2", func(t *testing.T) {
		// 用甲厂会话给乙厂节点签发必须因找不到被拒绝。
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, cidB, nb, na); !errors.Is(err, domain.ErrNotFound) {
			// 能签给别厂节点就停，凭证发串了厂。
			t.Fatalf("other factory: %v", err)
		}
	})
	// 作废后不能签发，重新绑定之后可以再签发。
	run("2.3", func(t *testing.T) {
		// 作废客户端绑定没成功，后面的断言就没有依据。
		if err := facA.VoidBinding(ctx, cidA); err != nil {
			// 作废客户端绑定失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 绑定作废之后再签发必须因绑定已作废被拒绝。
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na); !errors.Is(err, domain.ErrBindingVoid) {
			// 作废后还能签发就停，废掉的绑定还活着。
			t.Fatalf("voided: %v", err)
		}
		// 登记客户端绑定没成功，后面的断言就没有依据。
		if _, err := facA.AcceptBinding(ctx, cidA, "Client-A1", pubA, 2); err != nil {
			// 登记客户端绑定失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 签发运行凭证，供后面步骤使用，失败则前提断了。
		cred1, err = facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		// 签发运行凭证没成功，后面的断言就没有依据。
		if err != nil {
			// 签发运行凭证失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
	})

	// 读取厂签名公钥，供后面步骤使用，失败则前提断了。
	facPubB, err := facB.SigningPublicKey(ctx)
	// 读取厂签名公钥没成功，后面的断言就没有依据。
	if err != nil {
		// 读取厂签名公钥失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把甲厂的凭证拿到乙厂去评估必须拒绝。
	run("3.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facB.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 把甲厂凭证拿到乙厂评估，决定必须是拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 跨厂凭证被放行就停，评估没有验厂界。
			t.Fatalf("cross factory %+v %v", ev, err)
		}
	})
	// 就算标了资产允许，跨厂凭证仍然要拒绝。
	run("3.2", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		// 只多标一个资产允许，跨厂评估仍必须拒绝。
		bag.AssetAllowed = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facB.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 就算标了资产允许，跨厂凭证仍然必须拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 只拷了一个标记就被放行就停，身份没有验。
			t.Fatalf("copied data %+v %v", ev, err)
		}
	})

	// 包里没有私钥时，评估必须拒绝。
	run("4.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, nil, facPubA)
		// 只塞进凭证、不给私钥，这次评估必须拒绝。
		bag.Runtime = &cred1
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 没有私钥的包必须被拒绝，不能只靠那张凭证。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 没私钥也被放行就停，没有证明包还在本人手里。
			t.Fatalf("no private key %+v %v", ev, err)
		}
	})
	// 只有资产允许、没有节点身份时必须拒绝。
	run("4.2", func(t *testing.T) {
		// 只给资产允许、不带节点身份，评估必须拒绝。
		bag := factory.Bag{AssetAllowed: true}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 只有资产允许、没有节点身份时必须拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 空包被放行就停，评估没有看是哪一台。
			t.Fatalf("asset only %+v %v", ev, err)
		}
	})

	// 凭证过了窗口再开新作业必须拒绝。
	run("5.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 把钟拨到凭证失效之后，用来验过期后的决定。
		expired := factory.Clocks{Server: na.Add(time.Hour), Local: na.Add(time.Hour)}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeOpen)
		// 凭证过了窗口之后，新开作业必须拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 过期还能新开就停，时间窗口没有生效。
			t.Fatalf("expired open %+v %v", ev, err)
		}
	})
	// 过期之后焊接中可以继续，新开作业仍要拒绝。
	run("5.2", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 标成正在焊接，过期之后应允许把这道做完。
		bag.Welding = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 把钟拨到凭证失效之后，用来验过期后的决定。
		expired := factory.Clocks{Server: na.Add(time.Hour), Local: na.Add(time.Hour)}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		cont, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeContinue)
		// 过期之后，正在焊接的这道作业必须允许做完。
		if err != nil || cont.Decision != factory.NodeContinueWeld {
			// 焊接中被掐断就停，在制的作业被误伤了。
			t.Fatalf("weld %+v %v", cont, err)
		}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		open, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeOpen)
		// 同一张过期凭证上再开新作业必须拒绝。
		if err != nil || open.Decision != factory.NodeDeny {
			// 过期还能新开就停，焊接标记被拿去开了新活。
			t.Fatalf("expired new %+v %v", open, err)
		}
	})

	// 吊销运行凭证，供后面步骤使用，失败则前提断了。
	revoked, err := facA.RevokeRuntimeGrant(ctx, saTok, cidA, nb, na)
	// 吊销运行凭证没成功，后面的断言就没有依据。
	if err != nil {
		// 吊销运行凭证失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 在线收到吊销后，旧的放行凭证必须失效。
	run("6.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 标成在线，后到的吊销必须立刻盖住旧凭证。
		bag.Connected = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(revoked)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 在线时后到的吊销必须盖过旧的放行凭证。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 吊销之后还能开工就停，新的修订没有盖住旧证。
			t.Fatalf("online revoke %+v %v", ev, err)
		}
	})
	// 离线并且还没装吊销时，原凭证仍应放行。
	run("7.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 标成离线，还没装吊销时原凭证仍应放行。
		bag.Connected = false
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 离线并且还没装上吊销时，原凭证必须放行。
		if err != nil || ev.Decision != factory.NodeAllow {
			// 离线被拒绝就停，还没收到吊销就把人停了。
			t.Fatalf("offline no revoke %+v %v", ev, err)
		}
	})
	// 重新连上并装上吊销之后必须拒绝。
	run("7.2", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 重新标成在线，装上吊销之后必须拒绝。
		bag.Connected = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(revoked)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 重新连上并装上吊销之后，开工必须被拒绝。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 连上后还能开工就停，吊销没有在重连时生效。
			t.Fatalf("reconnect %+v %v", ev, err)
		}
	})
	// 旧的放行凭证不能把已经吊销的修订盖回去。
	run("8.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(revoked)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(cred1)
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		// 旧的放行凭证不能把已经吊销的修订盖回去。
		if err != nil || ev.Decision != factory.NodeDeny {
			// 旧证能盖回吊销就停，修订被允许回滚了。
			t.Fatalf("no rollback %+v %v", ev, err)
		}
	})

	// 签发运行凭证，供后面步骤使用，失败则前提断了。
	fresh, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
	// 签发运行凭证没成功，后面的断言就没有依据。
	if err != nil {
		// 签发运行凭证失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 在线的时候以服务器时钟为准，偏差仍放行。
	run("9.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 标成在线，时钟应当采信服务器而不是本机。
		bag.Connected = true
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(fresh)
		// 把本机钟拨过失效点，用来看采信哪一侧。
		skew := factory.Clocks{Server: now, Local: na.Add(2 * time.Hour)}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, skew, factory.NodeOpen)
		// 在线时必须放行，并且采信的是服务器时钟。
		if err != nil || ev.Decision != factory.NodeAllow || ev.TimeSource != audit.Server {
			// 没放行或没采信服务器钟就停，在线对时错了。
			t.Fatalf("server clock %+v %v", ev, err)
		}
	})
	// 离线的时候以本机时钟为准，超期必须拒绝。
	run("9.2", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 标成离线，时钟应当采信本机，从而判成过期。
		bag.Connected = false
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(fresh)
		// 把本机钟拨过失效点，用来看采信哪一侧。
		skew := factory.Clocks{Server: now, Local: na.Add(2 * time.Hour)}
		// 评估节点放行，供后面步骤使用，失败则前提断了。
		ev, err := facA.EvaluateNode(ctx, bag, skew, factory.NodeOpen)
		// 离线且本机已经超期时必须拒绝，并采信本机钟。
		if err != nil || ev.Decision != factory.NodeDeny || ev.TimeSource != audit.Local {
			// 没拒绝或没采信本机钟就停，离线对时错了。
			t.Fatalf("local clock %+v %v", ev, err)
		}
	})
	// 记下异常不能清密钥、不能停客户端，审计里无私钥。
	run("10.1", func(t *testing.T) {
		// 按厂标识和密钥拼出节点包，缺了就无法评估。
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		// 把这张凭证装进节点包，后面的评估看的就是它。
		bag.ApplyRuntime(fresh)
		// 记下节点异常没成功，后面的断言就没有依据。
		if err := facA.RecordAnomaly(ctx, cidA, "root"); err != nil {
			// 记下节点异常失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 读出厂签名私钥，供后面步骤使用，失败则前提断了。
		key, err := facA.Store().SigningKey(ctx)
		// 记下异常之后，厂签名私钥必须还在。
		if err != nil || len(key.PrivateKey) == 0 {
			// 私钥被擦掉就停，异常处理误清了厂密钥。
			t.Fatalf("key wiped %v", err)
		}
		// 读客户端登记，供后面步骤使用，失败则前提断了。
		cl, err := facA.Store().ClientByID(ctx, cidA)
		// 记下异常之后，客户端必须仍然是已绑定。
		if err != nil || cl.Status != factory.ClientStatusBound {
			// 客户端被停用就停，记一笔异常误伤了节点。
			t.Fatalf("client disabled %+v %v", cl, err)
		}
		// 包里的节点私钥必须还是原来的那一把。
		if !bytes.Equal(bag.PrivateKey, privA) {
			// 包里私钥变了就停，异常把端上的密钥清掉了。
			t.Fatal("bag key cleared")
		}
		// 拉取审计记录，供后面步骤使用，失败则前提断了。
		rows, err := facA.ListAudit(ctx)
		// 拉取审计记录没成功，后面的断言就没有依据。
		if err != nil {
			// 拉取审计记录失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
		// 审计里不能出现节点私钥，也不能出现厂私钥。
		if audit.ContainsAny(audit.Dump(rows), hex.EncodeToString(privA), hex.EncodeToString(key.PrivateKey)) {
			// 审计出现私钥就停，密钥被写进了日志。
			t.Fatal("private key in audit")
		}
	})
}

// 按厂和密钥拼出一个带公钥的节点包。
func bagOf(factoryID, clientID uuid.UUID, pub, priv, facPub []byte) factory.Bag {
	return factory.Bag{
		ClientID:      clientID,
		PublicKey:     pub,
		PrivateKey:    priv,
		FactoryID:     factoryID,
		FactoryPublic: facPub,
	}
}
