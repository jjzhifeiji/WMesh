package wanchannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/testtls"
)

// 照 WAN 的写法签一条下行：原文进 Signed，外层字段保留给老厂。
func signDown(t *testing.T, priv []byte, fid uuid.UUID, c Cmd) Cmd {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 先清掉旧签名，原文里不能套签名。
	c.Signed, c.Sig = nil, nil
	// 编出指令原文，签名就覆盖这些字节。
	raw, err := json.Marshal(c)
	// 编不出就停测。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 填回原文。
	c.Signed = raw
	// 绑定目标厂签名。
	c.Sig = nodekey.Sign(priv, nodekey.ChannelCmdPayload(fid, raw))
	return c
}

// 生成一对 WAN 指令签名钥，生不出就停测。
func wanKeyPair(t *testing.T) ([]byte, []byte) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 用系统随机数生成一对钥。
	pub, priv, err := nodekey.Generate()
	// 生不出钥就停测。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	return pub, priv
}

// 验签矩阵：已钉只认签名里的原文；篡改、转投、缺签名都拒收；不强制照旧用外层。
func TestOpenCmdMatrix(t *testing.T) {
	// 本厂和别厂两个身份，验防转投。
	fid, other := uuid.New(), uuid.New()
	// WAN 签名钥和攻击者的钥。
	pub, priv := wanKeyPair(t)
	// 攻击者自己的一把钥。
	_, evil := wanKeyPair(t)
	// 签名里是停用，外层被改成启用。
	good := signDown(t, priv, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 3})
	// 外层改成启用，验签后不该生效。
	good.Status = "active"
	// 验过就用签名里的原文。
	got, err := openCmd(pub, fid, good, true)
	// 验不过或取了外层字段都停测。
	if err != nil || got.Status != "disabled" || got.Revision != 3 || got.Signed != nil || got.Sig != nil {
		// 带上结果停测。
		t.Fatalf("valid %+v %v", got, err)
	}
	// 原文改一个字节就该验不过。
	tampered := good
	// 拷一份原文再改，不动上面那条。
	tampered.Signed = bytes.Replace(append([]byte(nil), good.Signed...), []byte("disabled"), []byte("retired!"), 1)
	// 签给别厂的指令转投过来。
	forOther := signDown(t, priv, other, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 3})
	// 攻击者用自己的钥签。
	forged := signDown(t, evil, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 3})
	// 签名对、原文却不是 JSON。
	notJSON := Cmd{Typ: CmdFactoryState, Signed: []byte("not-json")}
	// 给非 JSON 原文补上真签名。
	notJSON.Sig = nodekey.Sign(priv, nodekey.ChannelCmdPayload(fid, notJSON.Signed))
	// 逐种坏指令列出来：篡改、转投、伪造、非 JSON、缺签名、只有签名。
	bad := map[string]Cmd{"tampered": tampered, "otherFactory": forOther, "forged": forged, "notJSON": notJSON, "unsigned": {Typ: CmdFactoryState, Status: "disabled"}, "sigOnly": {Typ: CmdFactoryState, Sig: good.Sig}}
	// 每种坏指令都试一遍。
	for name, c := range bad {
		// 已钉时都该拒收。
		if _, err := openCmd(pub, fid, c, true); !errors.Is(err, domain.ErrIntegrity) {
			// 坏指令被接受就停测。
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	// 已钉但钥是空的，也不能放过。
	if _, err := openCmd(nil, fid, good, true); !errors.Is(err, domain.ErrIntegrity) {
		// 空钥验过就停测。
		t.Fatalf("nil key accepted: %v", err)
	}
	// 不强制时没签名也照旧用外层。
	got, err = openCmd(nil, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 1}, false)
	// 旧格式过不了就停测。
	if err != nil || got.Status != "active" || got.Revision != 1 {
		// 带上结果停测。
		t.Fatalf("legacy %+v %v", got, err)
	}
	// 不强制时带签名也只看外层。
	got, err = openCmd(pub, fid, good, false)
	// 取了签名原文就停测。
	if err != nil || got.Status != "active" {
		// 带上结果停测。
		t.Fatalf("legacy signed %+v %v", got, err)
	}
}

// 钉钥状态：http 不钉不强制；https 首次钉一次，同钥幂等，换钥拒收；落库失败照用；冲突一律拒收。
func TestCmdGuardAdopt(t *testing.T) {
	// 本厂身份。
	fid := uuid.New()
	// 两把不同的 WAN 公钥。
	pub, priv := wanKeyPair(t)
	// 另一把，用作换钥。
	pub2, _ := wanKeyPair(t)
	// 数落库回调被叫了几次。
	var pins atomic.Int32
	// 落库只记次数，不报错。
	okPin := func([]byte) error {
		// 多记一次落库。
		pins.Add(1)
		return nil
	}
	// http 下带旧钉钥也不强制，也不钉回包里的钥。
	g := newCmdGuard(fid, "http://wan:52080", Trust{Key: pub, Pin: okPin})
	// http 下回包钥不该被钉。
	if err := g.adopt(pub2); err != nil || pins.Load() != 0 {
		// 钉了或报错都停测。
		t.Fatalf("http adopt %v pins %d", err, pins.Load())
	}
	// http 下不该强制验签。
	if _, enforce := g.current(); enforce {
		// 强制了就停测。
		t.Fatal("http enforced")
	}
	// https 下还没钉。
	g = newCmdGuard(fid, "https://wan:52443", Trust{Pin: okPin})
	// 还没钉时不强制。
	if _, enforce := g.current(); enforce {
		// 没钉就强制了，停测。
		t.Fatal("enforced before pin")
	}
	// 首次拿到钥就钉住。
	if err := g.adopt(pub); err != nil || pins.Load() != 1 {
		// 没钉上就停测。
		t.Fatalf("first adopt %v pins %d", err, pins.Load())
	}
	// 同一把再来不再落库。
	if err := g.adopt(pub); err != nil || pins.Load() != 1 {
		// 重复落库或报错就停测。
		t.Fatalf("same adopt %v pins %d", err, pins.Load())
	}
	// 换一把就该拒收。
	if err := g.adopt(pub2); !errors.Is(err, errKeyChanged) {
		// 换钥被接受就停测。
		t.Fatalf("changed adopt %v", err)
	}
	// 钉住后强制验签，用的是首次那把。
	if key, enforce := g.current(); !enforce || !bytes.Equal(key, pub) {
		// 钥或强制不对就停测。
		t.Fatalf("current %x %v", key, enforce)
	}
	// 长度不对的钥不钉。
	g = newCmdGuard(fid, "https://wan:52443", Trust{Pin: okPin})
	// 坏长度按坏钥拒收。
	if err := g.adopt([]byte("short")); !errors.Is(err, domain.ErrInvalidKey) || pins.Load() != 1 {
		// 坏钥被钉就停测。
		t.Fatalf("short adopt %v pins %d", err, pins.Load())
	}
	// 落库失败时这次连接照样用这把钥。
	g = newCmdGuard(fid, "https://wan:52443", Trust{Pin: func([]byte) error { return errors.New("db down") }})
	// 落库失败不算拒收。
	if err := g.adopt(pub); err != nil {
		// 报错就停测。
		t.Fatalf("pin fail adopt %v", err)
	}
	// 内存里已换上这把钥并强制。
	if key, enforce := g.current(); !enforce || !bytes.Equal(key, pub) {
		// 没换上就停测。
		t.Fatalf("pin fail current %x %v", key, enforce)
	}
	// 库里已是另一把：标冲突，之后一律拒收。
	g = newCmdGuard(fid, "https://wan:52443", Trust{Pin: func([]byte) error { return domain.ErrIntegrity }})
	// 冲突要交回，本次连接退出重拨。
	if err := g.adopt(pub); !errors.Is(err, errPinConflict) {
		// 没报冲突就停测。
		t.Fatalf("conflict adopt %v", err)
	}
	// 冲突后即便签名对也验不过。
	if _, err := g.open(signDown(t, priv, fid, Cmd{Typ: CmdFactoryState, Status: "active"})); !errors.Is(err, domain.ErrIntegrity) {
		// 冲突后还收指令就停测。
		t.Fatalf("conflict open %v", err)
	}
	// 冲突后整份索引也交回冲突。
	if _, _, err := g.openIndex(indexResp{WANPublicKey: pub}); !errors.Is(err, errPinConflict) {
		// 没报冲突就停测。
		t.Fatalf("conflict index %v", err)
	}
}

// 整份索引：全部验过才交回原文；有一条不过或回包钥变了，整批不用但不报错。
func TestCmdGuardOpenIndex(t *testing.T) {
	// 本厂身份。
	fid := uuid.New()
	// WAN 签名钥。
	pub, priv := wanKeyPair(t)
	// 另一把，用作换钥。
	pub2, priv2 := wanKeyPair(t)
	// 已钉住的 https 连接。
	g := newCmdGuard(fid, "https://wan:52443", Trust{Key: pub})
	// 两条都签对的索引。
	good := []Cmd{signDown(t, priv, fid, Cmd{Typ: CmdFactoryState, Status: "active"}), signDown(t, priv, fid, Cmd{Typ: CmdClientBind, ClientID: uuid.NewString()})}
	// 全部验过就交回原文。
	cmds, ok, err := g.openIndex(indexResp{Cmds: good, WANPublicKey: pub})
	// 没交回两条就停测。
	if err != nil || !ok || len(cmds) != 2 || cmds[0].Signed != nil {
		// 带上结果停测。
		t.Fatalf("good index %+v %v %v", cmds, ok, err)
	}
	// 夹一条没签名的。
	bad := append(append([]Cmd(nil), good...), Cmd{Typ: CmdClientBind, ClientID: uuid.NewString()})
	// 有一条不过就整批不用。
	if cmds, ok, err := g.openIndex(indexResp{Cmds: bad, WANPublicKey: pub}); err != nil || ok || cmds != nil {
		// 交回了半批就停测。
		t.Fatalf("bad index %+v %v %v", cmds, ok, err)
	}
	// 回包换了公钥，新钥签的也不认。
	changed := []Cmd{signDown(t, priv2, fid, Cmd{Typ: CmdFactoryState, Status: "active"})}
	// 公钥变了整批不用，也不报错。
	if cmds, ok, err := g.openIndex(indexResp{Cmds: changed, WANPublicKey: pub2}); err != nil || ok || cmds != nil {
		// 新钥被接受就停测。
		t.Fatalf("changed key index %+v %v %v", cmds, ok, err)
	}
}

// 带签名的替身平台：回租约和索引，索引内容和回包公钥由用例随时换。
type signStub struct {
	mu   sync.Mutex   // 用例和替身协程都会读写清单
	cmds []Cmd        // 索引里要回的指令
	key  []byte       // 索引回包里的 WAN 公钥
	hits atomic.Int32 // 索引被拉了几次
}

// 换上要回的索引和公钥。
func (s *signStub) set(cmds []Cmd, key []byte) {
	// 占住再改，避免和替身协程交错。
	s.mu.Lock()
	// 改完就放开。
	defer s.mu.Unlock()
	// 记下要回的指令和公钥。
	s.cmds, s.key = cmds, key
}

// ServeHTTP 按路径回租约、索引或找不到。
func (s *signStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 按路径分发。
	switch {
	// 租约路径回一把解包钥。
	case strings.HasSuffix(r.URL.Path, "/lease"):
		// 回解包钥，通道才能继续拉索引。
		writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("https-lease"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	// 索引路径回当前清单和公钥。
	case strings.HasSuffix(r.URL.Path, "/index"):
		// 占住再读，避免和用例改清单交错。
		s.mu.Lock()
		// 拷出要回的内容。
		cmds, key := s.cmds, s.key
		// 读完就放开。
		s.mu.Unlock()
		// 没有清单时回空列表。
		if cmds == nil {
			// 改成空列表，厂端不当失败。
			cmds = []Cmd{}
		}
		// 回清单和公钥。
		writeJSON(w, map[string]any{"cmds": cmds, "wanPublicKey": key})
		// 记一次拉索引。
		s.hits.Add(1)
	// 其余路径回找不到。
	default:
		// 未知路径拒绝。
		http.NotFound(w, r)
	}
}

// 记下 Hold 各回调收到了什么，用例逐项核对。
type holdRec struct {
	states  chan State         // 落地的治理状态
	clients chan ClientIntent  // 落地的设备意图
	syncs   chan []uuid.UUID   // 对账名单
	reqs    chan string        // 被问到的问询类型
	leases  chan []byte        // 落地的租约钥
	pins    chan []byte        // 落库钉住的公钥
	pinErr  func([]byte) error // 落库回调的结果，空则成功
}

// 建一份空记录，缓冲留足，回调不会堵住通道。
func newHoldRec() *holdRec {
	// 各项都留足缓冲。
	return &holdRec{
		states: make(chan State, 32), clients: make(chan ClientIntent, 32), syncs: make(chan []uuid.UUID, 32),
		reqs: make(chan string, 32), leases: make(chan []byte, 32), pins: make(chan []byte, 32),
	}
}

// 落库钉钥：记下公钥，再按用例给的结果交回。
func (r *holdRec) pin(pub []byte) error {
	// 记下被钉的公钥。
	r.pins <- append([]byte(nil), pub...)
	// 用例没给结果就当成功。
	if r.pinErr == nil {
		return nil
	}
	// 交回用例给的结果。
	return r.pinErr(pub)
}

// 后台跑 Hold，回调都记进记录；交回退出原因的通道。
func (r *holdRec) run(ctx context.Context, mqttURL, wanHTTP string, fid uuid.UUID, priv []byte, key []byte) <-chan error {
	// 接住通道退出原因。
	errCh := make(chan error, 1)
	// 后台跑通道，用例才能投下行和断言。
	go func() {
		// 回调都只记录，问询答空清单。
		errCh <- Hold(ctx, mqttURL, wanHTTP, fid, priv, Trust{Key: key, Pin: r.pin}, func(s State) error {
			// 记下治理状态。
			r.states <- s
			return nil
		}, func(in ClientIntent) error { // 记下设备意图。
			// 记下设备意图。
			r.clients <- in
			return nil
		}, func(keep []uuid.UUID) error { // 记下对账名单。
			// 记下对账名单。
			r.syncs <- keep
			return nil
		}, nil, nil, nil, nil, nil, func(l Lease) error { // 记下租约钥。
			// 记下租约钥。
			r.leases <- append([]byte(nil), l.Key...)
			return nil
		}, func(typ, _, _, _ string) (json.RawMessage, json.RawMessage, error) { // 记下问询类型并答空。
			// 记下问询类型。
			r.reqs <- typ
			// 答空清单和空快照，回执照常发出。
			return json.RawMessage(`[]`), json.RawMessage(`{}`), nil
		}, nil, nil)
	}()
	return errCh
}

// 通道里此刻不该再有东西，有就停测。
func expectEmpty[T any](t *testing.T, name string, ch <-chan T) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 不等待，只看有没有多出来的。
	select {
	// 多出来一条就停测。
	case v := <-ch:
		// 带上多出来的内容停测。
		t.Fatalf("unexpected %s: %+v", name, v)
	// 没有就对了。
	default:
	}
}

// 在超时前取一条，通道先退出或超时就停测。
func recv[T any](t *testing.T, ctx context.Context, errCh <-chan error, name string, ch <-chan T) T {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 取到、通道退出或超时谁先到走谁。
	select {
	// 取到就交回。
	case v := <-ch:
		return v
	// 通道先退出就停测。
	case err := <-errCh:
		// 带上原因停测。
		t.Fatalf("%s hold: %v", name, err)
	// 到时还没有就停测。
	case <-ctx.Done():
		// 带上这一步的名字停测。
		t.Fatalf("no %s", name)
	}
	// 走不到这里，给编译器一个零值。
	var zero T
	return zero
}

// 起 HTTPS 替身并换上它的 CA，交回平台地址。
func startSignStubTLS(t *testing.T, stub *signStub) string {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 现生成一套证书。
	pki := testtls.New(t)
	// 用测例证书起 HTTPS 替身。
	srv := startTLSServer(t, pki, stub)
	// 换上测例 CA，厂端才校验得过。
	useRootCAs(t, pki)
	return srv.URL
}

// https 首轮钉住回包公钥并验过整份索引；之后 MQTT 只认本厂的有效签名，租约只认 HTTPS。
func TestHoldPinsAndVerifiesOverHTTPS(t *testing.T) {
	// 本厂和别厂身份。
	fid, other := uuid.New(), uuid.New()
	// 本厂签发钥，连接签名用。
	_, priv := wanKeyPair(t)
	// WAN 指令签名钥。
	wpub, wpriv := wanKeyPair(t)
	// 攻击者的钥。
	_, evil := wanKeyPair(t)
	// 放行代理，留句柄投下行。
	broker, mqttAddr := startAllowBroker(t)
	// 索引里的一台设备。
	cid := uuid.New()
	// 替身回签好的治理状态和绑定。
	stub := &signStub{}
	// 换上签好的索引和公钥。
	stub.set([]Cmd{
		signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 1}),
		signDown(t, wpriv, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), BindingRevision: 1}),
	}, wpub)
	// 起 HTTPS 替身。
	wanURL := startSignStubTLS(t, stub)
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 后台跑通道，还没钉钥。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, wanURL, fid, priv, nil)
	// 首轮应钉住回包里的公钥。
	if got := recv(t, ctx, errCh, "pin", rec.pins); !bytes.Equal(got, wpub) {
		// 钉错钥就停测。
		t.Fatalf("pinned %x", got)
	}
	// 索引里的治理状态要落地。
	if st := recv(t, ctx, errCh, "index state", rec.states); st.Status != "active" || st.Revision != 1 {
		// 状态不对就停测。
		t.Fatalf("index state %+v", st)
	}
	// 索引里的绑定要落地。
	if in := recv(t, ctx, errCh, "index bind", rec.clients); in.ClientID != cid {
		// 设备不对就停测。
		t.Fatalf("index bind %+v", in)
	}
	// 对账名单取验过的原文。
	if keep := recv(t, ctx, errCh, "sync", rec.syncs); len(keep) != 1 || keep[0] != cid {
		// 名单不对就停测。
		t.Fatalf("keep %v", keep)
	}
	// 没签名的停用，伪造的整厂停用不能落。
	publishDown(t, broker, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 9})
	// 攻击者自己签的。
	publishDown(t, broker, fid, signDown(t, evil, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 8}))
	// 签给别厂的转投过来。
	publishDown(t, broker, fid, signDown(t, wpriv, other, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 7}))
	// 没签名的绑定，攻击者的设备不能绑进来。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: uuid.NewString(), BindingRevision: 9})
	// 签好的租约也不认，租约只走 HTTPS。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdLease, Lease: []byte("mqtt-lease"), NotAfter: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}))
	// 签好的绑定照常落地。
	cid2 := uuid.New()
	// 投下签好的绑定。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdClientBind, ClientID: cid2.String(), BindingRevision: 2}))
	// 签名里是启用，外层被改成注销，只认签名原文。
	tampered := signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 5})
	// 外层改成注销。
	tampered.Status = "retired"
	// 投下外层被改过的那条。
	publishDown(t, broker, fid, tampered)
	// 最后投一条签好的哨兵。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 6}))
	// 外层被改的那条按签名原文落地。
	if st := recv(t, ctx, errCh, "tampered state", rec.states); st.Status != "active" || st.Revision != 5 {
		// 落了伪造或外层字段就停测。
		t.Fatalf("state after forged %+v", st)
	}
	// 哨兵按序落地，前面的伪造都已处理过。
	if st := recv(t, ctx, errCh, "sentinel", rec.states); st.Revision != 6 {
		// 顺序不对就停测。
		t.Fatalf("sentinel %+v", st)
	}
	// 只有签好的那台绑定落地。
	if in := recv(t, ctx, errCh, "signed bind", rec.clients); in.ClientID != cid2 {
		// 落了伪造的绑定就停测。
		t.Fatalf("bind %+v", in)
	}
	// 伪造的绑定不该落地。
	expectEmpty(t, "client", rec.clients)
	// 只有首轮那把 HTTPS 租约。
	if k := recv(t, ctx, errCh, "lease", rec.leases); string(k) != "https-lease" {
		// 首把不是 HTTPS 的就停测。
		t.Fatalf("lease %q", k)
	}
	// 通道上的租约不该落地。
	expectEmpty(t, "lease", rec.leases)
	// 只钉一次。
	expectEmpty(t, "pin", rec.pins)
	// 丢弃伪造不能拆连接。
	assertHolding(t, errCh)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}

// 已钉时伪造或不带签名的升档问询不答；签好的照常回执。
func TestHoldIgnoresForgedRequests(t *testing.T) {
	// 本厂和别厂身份。
	fid, other := uuid.New(), uuid.New()
	// 本厂签发钥。
	_, priv := wanKeyPair(t)
	// WAN 指令签名钥。
	wpub, wpriv := wanKeyPair(t)
	// 放行代理。
	broker, mqttAddr := startAllowBroker(t)
	// 替身回空清单和同一把公钥。
	stub := &signStub{}
	// 换上空清单和公钥。
	stub.set(nil, wpub)
	// 起 HTTPS 替身。
	wanURL := startSignStubTLS(t, stub)
	// 收上行回执的问询号。
	replies := make(chan string, 16)
	// 另开一个观察端收上行。
	watch := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + mqttAddr).SetClientID("watch-up"))
	// 观察端连不上就停测。
	if tok := watch.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 带上原因停测。
		t.Fatalf("watch connect: %v", tok.Error())
	}
	// 测完断开观察端。
	t.Cleanup(func() { watch.Disconnect(250) })
	// 订上行，只收带问询号的回执。
	if tok := watch.Subscribe("wan/"+fid.String()+"/up", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 预备装这条上行。
		var c Cmd
		// 解不开或不是回执就丢掉。
		if json.Unmarshal(m.Payload(), &c) != nil || c.ReqID == "" {
			return
		}
		// 记下回执的问询号。
		replies <- c.ReqID
	}); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 订不上就停测。
		t.Fatal(tok.Error())
	}
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 库里已钉这把钥，开局就强制验签。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, wanURL, fid, priv, wpub)
	// 等首轮拉过索引，订阅已就绪。
	waitUntil(t, ctx, errCh, "index", func() bool { return stub.hits.Load() >= 1 })
	// 不带签名的三种问询。
	publishDown(t, broker, fid, Cmd{Typ: CmdAssetList, ReqID: "r1", Kind: "process"})
	// 不带签名的快照问询。
	publishDown(t, broker, fid, Cmd{Typ: CmdAssetSnapshot, ReqID: "r2", AssetID: uuid.NewString()})
	// 不带签名的目录问询。
	publishDown(t, broker, fid, Cmd{Typ: CmdFSList, ReqID: "r3", Kind: "process"})
	// 签给别厂的问询转投过来。
	publishDown(t, broker, fid, signDown(t, wpriv, other, Cmd{Typ: CmdAssetSnapshot, ReqID: "r4", AssetID: uuid.NewString()}))
	// 签好的问询照常答。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdAssetList, ReqID: "r5", Kind: "process"}))
	// 只有签好的那条被问到。
	if typ := recv(t, ctx, errCh, "request", rec.reqs); typ != CmdAssetList {
		// 问询类型不对就停测。
		t.Fatalf("request %s", typ)
	}
	// 回执只该是签好的那条。
	if id := recv(t, ctx, errCh, "reply", replies); id != "r5" {
		// 回了伪造问询就停测。
		t.Fatalf("reply %s", id)
	}
	// 伪造的问询不该被问到。
	expectEmpty(t, "request", rec.reqs)
	// 已钉同一把钥不再落库。
	expectEmpty(t, "pin", rec.pins)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}

// 已钉时索引里有一条坏签名：整批不落、不对账，连接照留，后面签好的 MQTT 照常落地。
func TestHoldSkipsIndexWithBadSignature(t *testing.T) {
	// 换上告警替身，核对整批跳过有记录。
	warns := captureWarn(t)
	// 本厂身份。
	fid := uuid.New()
	// 本厂签发钥。
	_, priv := wanKeyPair(t)
	// WAN 指令签名钥。
	wpub, wpriv := wanKeyPair(t)
	// 放行代理。
	broker, mqttAddr := startAllowBroker(t)
	// 一条签好的绑定，原文被改过。
	bad := signDown(t, wpriv, fid, Cmd{Typ: CmdClientBind, ClientID: uuid.NewString(), BindingRevision: 1})
	// 把原文里的修订改掉，签名随之验不过。
	bad.Signed = bytes.Replace(bad.Signed, []byte(`"bindingRevision":1`), []byte(`"bindingRevision":2`), 1)
	// 替身回一条好的和一条坏的。
	stub := &signStub{}
	// 换上掺了坏签名的索引。
	stub.set([]Cmd{signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 1}), bad}, wpub)
	// 起 HTTPS 替身。
	wanURL := startSignStubTLS(t, stub)
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 库里已钉这把钥。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, wanURL, fid, priv, wpub)
	// 等首轮拉过索引。
	waitUntil(t, ctx, errCh, "index", func() bool { return stub.hits.Load() >= 1 })
	// 投一条签好的状态，证明连接还在。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 2}))
	// 第一条落地的只能是 MQTT 那条。
	if st := recv(t, ctx, errCh, "mqtt state", rec.states); st.Revision != 2 {
		// 落了坏批里的状态就停测。
		t.Fatalf("state %+v", st)
	}
	// 坏批里的绑定不该落。
	expectEmpty(t, "client", rec.clients)
	// 坏批不该对账。
	expectEmpty(t, "sync", rec.syncs)
	// 整批跳过要留告警。
	if warns.count("signature rejected, skip batch") < 1 {
		// 没记告警就停测。
		t.Fatal("no skip warn")
	}
	// 连接照留。
	assertHolding(t, errCh)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}

// 回包公钥和已钉的不一致：整批拒收、不改钉钥，新钥签的 MQTT 也不认，连接照留。
func TestHoldRejectsChangedWANKey(t *testing.T) {
	// 换上告警替身，核对拒收有记录。
	warns := captureWarn(t)
	// 本厂身份。
	fid := uuid.New()
	// 本厂签发钥。
	_, priv := wanKeyPair(t)
	// 已钉的旧钥和 WAN 换上的新钥。
	oldPub, oldPriv := wanKeyPair(t)
	// WAN 换上的新钥。
	newPub, newPriv := wanKeyPair(t)
	// 放行代理。
	broker, mqttAddr := startAllowBroker(t)
	// 替身用新钥签，回包带新公钥。
	stub := &signStub{}
	// 换上新钥签的索引。
	stub.set([]Cmd{signDown(t, newPriv, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 3})}, newPub)
	// 起 HTTPS 替身。
	wanURL := startSignStubTLS(t, stub)
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 库里钉的是旧钥。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, wanURL, fid, priv, oldPub)
	// 等首轮拉过索引。
	waitUntil(t, ctx, errCh, "index", func() bool { return stub.hits.Load() >= 1 })
	// 新钥签的 MQTT 也不认。
	publishDown(t, broker, fid, signDown(t, newPriv, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 4}))
	// 旧钥签的哨兵照常落地，证明钉钥没被换掉。
	publishDown(t, broker, fid, signDown(t, oldPriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 5}))
	// 第一条落地的只能是旧钥签的哨兵。
	if st := recv(t, ctx, errCh, "sentinel", rec.states); st.Revision != 5 {
		// 新钥签的落地了就停测。
		t.Fatalf("state %+v", st)
	}
	// 换钥不该触发落库。
	expectEmpty(t, "pin", rec.pins)
	// 拒收要留告警。
	if warns.count("key rejected") < 1 {
		// 没记告警就停测。
		t.Fatal("no key change warn")
	}
	// 连接照留。
	assertHolding(t, errCh)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}

// http 下库里有旧钉钥也不强制：不带签名的旧格式照常落地，也不钉回包里的钥。
func TestHoldHTTPKeepsLegacyFormat(t *testing.T) {
	// 本厂身份。
	fid := uuid.New()
	// 本厂签发钥。
	_, priv := wanKeyPair(t)
	// 库里的旧钉钥和回包里的另一把。
	oldPub, _ := wanKeyPair(t)
	// 回包里的另一把钥。
	otherPub, _ := wanKeyPair(t)
	// 放行代理。
	broker, mqttAddr := startAllowBroker(t)
	// 设备身份。
	cid := uuid.New()
	// 替身回不带签名的旧格式。
	stub := &signStub{}
	// 换上旧格式索引和另一把钥。
	stub.set([]Cmd{{Typ: CmdFactoryState, Status: "active", Revision: 1}, {Typ: CmdClientBind, ClientID: cid.String(), BindingRevision: 1}}, otherPub)
	// 明文替身。
	srv := httptest.NewServer(stub)
	// 测完关掉。
	t.Cleanup(srv.Close)
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 库里有旧钉钥，但走 http。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, srv.URL, fid, priv, oldPub)
	// 旧格式状态照常落地。
	if st := recv(t, ctx, errCh, "index state", rec.states); st.Revision != 1 {
		// 状态不对就停测。
		t.Fatalf("state %+v", st)
	}
	// 旧格式绑定照常落地。
	if in := recv(t, ctx, errCh, "index bind", rec.clients); in.ClientID != cid {
		// 设备不对就停测。
		t.Fatalf("bind %+v", in)
	}
	// MQTT 旧格式也照常落地。
	publishDown(t, broker, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 2})
	// 不带签名的 MQTT 状态要落地。
	if st := recv(t, ctx, errCh, "mqtt state", rec.states); st.Revision != 2 {
		// 状态不对就停测。
		t.Fatalf("mqtt state %+v", st)
	}
	// http 下不钉钥。
	expectEmpty(t, "pin", rec.pins)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}

// 钉钥落库失败：本次连接照样用这把钥验签，不带签名的 MQTT 仍丢弃。
func TestHoldPinFailureStillEnforces(t *testing.T) {
	// 换上告警替身，核对落库失败有记录。
	warns := captureWarn(t)
	// 本厂身份。
	fid := uuid.New()
	// 本厂签发钥。
	_, priv := wanKeyPair(t)
	// WAN 指令签名钥。
	wpub, wpriv := wanKeyPair(t)
	// 放行代理。
	broker, mqttAddr := startAllowBroker(t)
	// 替身回一条签好的状态。
	stub := &signStub{}
	// 换上签好的索引。
	stub.set([]Cmd{signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 1})}, wpub)
	// 起 HTTPS 替身。
	wanURL := startSignStubTLS(t, stub)
	// 给这一轮限时。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 离开时取消。
	defer cancel()
	// 记下各回调。
	rec := newHoldRec()
	// 落库一律失败。
	rec.pinErr = func([]byte) error { return errors.New("db down") }
	// 还没钉钥。
	errCh := rec.run(ctx, "tcp://"+mqttAddr, wanURL, fid, priv, nil)
	// 索引照常落地。
	if st := recv(t, ctx, errCh, "index state", rec.states); st.Revision != 1 {
		// 状态不对就停测。
		t.Fatalf("state %+v", st)
	}
	// 不带签名的停用要丢弃。
	publishDown(t, broker, fid, Cmd{Typ: CmdFactoryState, Status: "disabled", Revision: 9})
	// 签好的哨兵照常落地。
	publishDown(t, broker, fid, signDown(t, wpriv, fid, Cmd{Typ: CmdFactoryState, Status: "active", Revision: 2}))
	// 第一条落地的只能是哨兵。
	if st := recv(t, ctx, errCh, "sentinel", rec.states); st.Revision != 2 {
		// 不带签名的落地了就停测。
		t.Fatalf("state %+v", st)
	}
	// 落库失败要留告警。
	if warns.count("pin wan key failed") != 1 {
		// 告警条数不对就停测。
		t.Fatalf("pin warns %d", warns.count("pin wan key failed"))
	}
	// 连接照留。
	assertHolding(t, errCh)
	// 断言完就取消。
	cancel()
	// 等通道退出。
	<-errCh
}
