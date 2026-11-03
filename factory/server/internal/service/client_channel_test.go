// C2：厂网登录拉过站密文；MQTT 小信封无正文；低修订不覆盖。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 假的下行通道，只把推送记下来不真发出去。
type recDown struct {
	// 保护下行记录，避免并发写乱。
	mu sync.Mutex
	// 按到达顺序记下的下行字节。
	msgs [][]byte
}

// 把一封下行记下来，没记下则后面验不到推送。
func (r *recDown) PublishDown(factoryID, clientID uuid.UUID, payload []byte) {
	// 先锁住下行记录，避免并发把推送写乱。
	r.mu.Lock()
	// 复制一份再追加，避免后面改动影响到原件。
	r.msgs = append(r.msgs, append([]byte(nil), payload...))
	// 放开记录锁，不放则会把下一次读堵住。
	r.mu.Unlock()
}

// 从新到旧找验得过的下行，没有则推送对不上。
func (r *recDown) latestFor(pub []byte, factoryID, clientID uuid.UUID) (clientmqtt.Intent, []byte, error) {
	// 先锁住下行记录，避免并发把推送写乱。
	r.mu.Lock()
	// 收尾时放开锁，中途返回也不会把记录堵死。
	defer r.mu.Unlock()
	// 从新到旧逐封查看，一封都对不上说明没推到这台。
	for i := len(r.msgs) - 1; i >= 0; i-- {
		// 核对下行签名，验不过说明信封不是这台的。
		in, err := clientmqtt.Verify(pub, factoryID, clientID, r.msgs[i])
		// 验签通过就采用这一封，失败则继续往更早找。
		if err == nil {
			return in, r.msgs[i], nil
		}
	}
	return clientmqtt.Intent{}, nil, errNoDown
}

// 没有对得上下行时返回的哨兵错误。
var errNoDown = errDown("no matching down intent")

// 找不到对得上下行时用的哨兵错误。
type errDown string

// 把哨兵错误变成文本，方便和预期原因比对。
func (e errDown) Error() string { return string(e) }

// 拉取的是密文信封，低修订不覆盖，策略和组包推送无正文。
func TestClientChannelPullAndPolicyFanout(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
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
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建管理员，厂级工艺工程由它起草发布。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	// 建操作员，示教器登录和拉包都用它。
	mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)

	// 接上只记录的下行通道，用来看有没有真推送。
	down := &recDown{}
	// 挂上只记录的下行通道，后面用来核对推送。
	fac.SetClientDown(down)

	// 生成一把设备密钥，失败则绑定没有公钥可用。
	pub, _, err := nodekey.Generate()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	cid := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	cidB := id.New()
	// 登记绑定失败就停，否则后面没有可靠结果。
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		// 把登记绑定的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 生成一把设备密钥，失败则绑定没有公钥可用。
	pubB, _, _ := nodekey.Generate()
	// 登记绑定失败就停，否则后面没有可靠结果。
	if _, err := fac.AcceptBinding(ctx, cidB, "焊机B", pubB, 1); err != nil {
		// 把登记绑定的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 操作员厂网登录，失败说明账号不对。
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 厂网登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把厂网登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录应带回签名公钥，空的说明密钥没发下来。
	if len(sess.SigningPublicKey) == 0 {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatal("missing signing key")
	}

	// 不指定文件夹，后面新建资产都挂在厂级树根。
	direct := factory.WorkContext{}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"current":180}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把编号打成文本，方便和路径或正文比对。
	body := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 不存在的资产拉包应交回不存在，放行说明没拦住。
	if _, err := fac.PadPullClientClosure(ctx, sess.Token, id.New()); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("missing asset pull: %v", err)
	}

	// 按登录人拉组包，失败说明清单里没有这份。
	got, err := fac.PadPullClientClosure(ctx, sess.Token, proj.ID)
	// 拉组包失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉组包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 传输形态应是信封或明文中的预期那种，反了即失败。
	if !contentcrypt.IsEnvelope(got.Wrap) || len(got.Snapshot.Members) == 0 {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatal("missing wrap")
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	plain := []byte(`{"current":180}`)
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, m := range got.Snapshot.Members {
		// 传输形态应是信封或明文中的预期那种，反了即失败。
		if !contentcrypt.IsEnvelope(m.Content) || bytes.Contains(m.Content, plain) || bytes.Contains(m.Content, []byte("current")) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatal("plaintext in transit member")
		}
	}

	// 打开传输信封，失败说明密钥或摘要不对。
	opened, err := factory.OpenTransit(fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, got)
	// 拆信封失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拆信封的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 在解开的成员里找工程本身，找不到说明组包丢了根。
	var root []byte
	// 逐条比对身份，命中工程就留下它的明文。
	for _, m := range opened.Members {
		// 只认工程这一条。
		if m.ID == proj.ID {
			// 记下工程明文，后面核对它点名的工艺。
			root = m.Content
		}
	}
	// 工程明文应点名刚造的工艺，否则拆出来的不是这一版。
	if !bytes.Contains(root, []byte(proc.ID.String())) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("opened root %s", root)
	}

	// 读取示教器收件箱，失败说明会话失效。
	box, err := fac.PadClientInbox(ctx, sess.Token)
	// 读收件箱失败或条数不对就停，说明没达预期。
	if err != nil || len(box.Closures) == 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("inbox %+v %v", box, err)
	}

	// 改写客户端策略，失败说明越权或字段不合法。
	pol, err := fac.SetClientPolicy(ctx, sa, factory.ClientPolicy{
		MaxCachedProjects: 3, CacheScope: factory.CacheScopeAll, PersistUnwrapKey: true, KeyTTLSeconds: 60, EncryptPouch: true,
	})
	// 改策略失败或修订不对就停，说明没达预期。
	if err != nil || pol.Revision < 1 {
		// 策略和预期不一致，说明下发没有扇出到端上。
		t.Fatalf("policy %v %v", pol, err)
	}
	// 查找对得上这台的下行，没有说明推送串了机器。
	in, raw, err := down.latestFor(sess.SigningPublicKey, fac.Store().FactoryID(), cid)
	// 查有无正文失败或修订不对就停，说明没达预期。
	if err != nil || clientmqtt.HasBody(raw) || in.Typ != clientmqtt.TypPolicy || in.Revision != pol.Revision ||
		in.MaxCachedProjects != 3 || in.CacheScope != factory.CacheScopeAll || !in.PersistUnwrapKey || in.KeyTTLSeconds != 60 || !in.EncryptPouch {
		// 策略和预期不一致，说明下发没有扇出到端上。
		t.Fatalf("policy intent %+v %v %s", in, err, raw)
	}
	// 改缓存上限失败就停，否则后面没有可靠结果。
	if err := fac.SetCacheLimit(ctx, sa, 4); err != nil {
		// 把改缓存上限的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 查找对得上这台的下行，没有说明推送串了机器。
	in, raw, err = down.latestFor(sess.SigningPublicKey, fac.Store().FactoryID(), cid)
	// 查有无正文失败或修订不对就停，说明没达预期。
	if err != nil || clientmqtt.HasBody(raw) || in.Typ != clientmqtt.TypPolicy || in.Revision != pol.Revision+1 ||
		in.MaxCachedProjects != 4 || !in.PersistUnwrapKey || in.KeyTTLSeconds != 60 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("limit fanout %+v %v %s", in, err, raw)
	}
}
