// C1：钉机械臂号、本机登录领钥；空号/他机号/未绑定拒绝。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/secret"
	factory "wmesh/factory/internal/service"
)

// 验收钉臂号和本机登录，空号他号未绑定即失败
func TestRegisterDeviceAndLogin(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 生成焊机密钥，失败则绑定无法开始
	pub, _, err := nodekey.Generate()
	// 生成焊机密钥失败就停，避免带着错误继续验
	if err != nil {
		// 生成焊机密钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 新开一个身份，避免和别的绑定撞号
	cid := id.New()
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 期望登记机械臂号被拒为必须有臂号，放行即失败
	if _, err := fac.RegisterDevice(ctx, cid, ""); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		// 空值没有被拒或结果是空就失败
		t.Fatalf("empty: %v", err)
	}
	// 登记机械臂号，失败则本步验收不能继续
	row, err := fac.RegisterDevice(ctx, cid, "ARM-1")
	// 登记机械臂号失败就停，避免带着错误继续验
	if err != nil || row.DeviceSerial != "ARM-1" || len(row.UnwrapKey) != 0 {
		// 臂号没钉上就失败，并打出登记结果
		t.Fatalf("pin %+v %v", row, err)
	}
	// 登记机械臂号，失败则本步验收不能继续
	again, err := fac.RegisterDevice(ctx, cid, "ARM-1")
	// 登记机械臂号失败就停，避免带着错误继续验
	if err != nil || again.DeviceSerial != "ARM-1" {
		// 重复登记没有保持原号就失败
		t.Fatalf("idempotent %+v %v", again, err)
	}

	// 新开一个身份，避免和别的绑定撞号
	cid2 := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pub2, _, err := nodekey.Generate()
	// 生成焊机密钥失败就停，避免带着错误继续验
	if err != nil {
		// 生成焊机密钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cid2, "焊机2", pub2, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 期望登记机械臂号被拒为臂号已被占用，放行即失败
	if _, err := fac.RegisterDevice(ctx, cid2, "ARM-1"); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		// 重复臂号没有被拒就失败
		t.Fatalf("taken: %v", err)
	}

	// 期望在焊机上登录被拒为必须有臂号，放行即失败
	if _, err := fac.LoginOnClient(ctx, cid, "", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		// 空臂号登录没有被拒就失败
		t.Fatalf("login empty: %v", err)
	}
	// 期望在焊机上登录被拒为臂号对不上，放行即失败
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-9", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		// 臂号对不上却被放行，应当被拒
		t.Fatalf("mismatch: %v", err)
	}
	// 期望新开一个身份被拒为不存在，放行即失败
	if _, err := fac.LoginOnClient(ctx, id.New(), "ARM-1", "op", "op-pass"); !errors.Is(err, domain.ErrNotFound) {
		// 未绑定焊机没有被拒就失败
		t.Fatalf("unbound: %v", err)
	}

	// 在焊机上登录，失败则本步验收不能继续
	sess, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass")
	// 在焊机上登录失败就停，避免带着错误继续验
	if err != nil || sess.Token == "" || len(sess.UnwrapKey) != 32 || sess.Account.ID != op.acc.ID {
		// 登录结果不对就失败
		t.Fatalf("login %+v %v", sess, err)
	}
	// 解钥或策略不对就失败，不该持久化或长度错
	if sess.Policy.PersistUnwrapKey || sess.Policy.CacheScope != factory.CacheScopeAll {
		// 下发策略不对就失败，解钥不该持久化
		t.Fatalf("policy %+v", sess.Policy)
	}
	// 写入焊机短码失败就停，避免带着错误继续验
	if err := fac.Store().PutClientShortCode(ctx, cid, "C0008"); err != nil {
		// 写入焊机短码失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 在焊机上登录，失败则本步验收不能继续
	sess, err = fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass")
	// 在焊机上登录失败就停，避免带着错误继续验
	if err != nil || sess.ClientShortCode != "C0008" {
		// 短码不对就失败，否则失败
		t.Fatalf("short %+v %v", sess, err)
	}
	// 先当作名册里没有操作员，找到了再标上
	hasOp := false
	// 逐项核对，漏掉一项即这一步没验完
	for _, r := range sess.Roles {
		// 对上操作员角色才标上，没标上即角色没授成
		if r == factory.RoleOperator {
			// 标上名册里有这名操作员，没有即建人失败
			hasOp = true
		}
	}
	// 名册里没有这名操作员就失败，建人没落上
	if !hasOp {
		// 角色集合不对就失败
		t.Fatalf("roles %v", sess.Roles)
	}
	// 校验会话仍有效失败就停，避免带着错误继续验
	if _, err := fac.RequireActive(ctx, sess.Token); err != nil {
		// 校验会话仍有效失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-2"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 期望在焊机上登录被拒为臂号对不上，放行即失败
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		// 旧臂号还能用就失败，应当已经换掉
		t.Fatalf("old arm: %v", err)
	}
	// 在焊机上登录失败就停，避免带着错误继续验
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-2", "op", "op-pass"); err != nil {
		// 在焊机上登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 作废绑定失败就停，避免带着错误继续验
	if err := fac.VoidBinding(ctx, cid); err != nil {
		// 作废绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 期望登记机械臂号被拒为绑定已作废，放行即失败
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-3"); !errors.Is(err, domain.ErrBindingVoid) {
		// 作废绑定后还能登记臂号就失败
		t.Fatalf("void pin: %v", err)
	}
	// 期望在焊机上登录被拒为绑定已作废，放行即失败
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-2", "op", "op-pass"); !errors.Is(err, domain.ErrBindingVoid) {
		// 作废绑定后还能登录就失败
		t.Fatalf("void login: %v", err)
	}

	// 读取审计，失败则本步验收不能继续
	rows, err := fac.ListAudit(ctx)
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
	// 把审计打成文本，没成功则本步验收失败
	dump := audit.Dump(rows)
	// 文本里夹带秘密或禁词就失败，说明泄密了
	if !audit.ContainsAny(dump, "client_register") || !audit.ContainsAny(dump, "client_login") {
		// 审计里没有这次动作就失败，记录丢了
		t.Fatalf("audit %s", dump)
	}
	// 文本里夹带秘密或禁词就失败，说明泄密了
	if audit.ContainsAny(dump, "op-pass", "sa-pass") {
		// 审计里出现密码就失败，说明口令泄了
		t.Fatal("password leaked")
	}
}

// 验收平板登录能列出已登记焊机，缺一台即失败
func TestPadLoginListsDevicesWithoutSerial(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 新开一个身份，避免和别的绑定撞号
	cidA := id.New()
	// 新开一个身份，避免和别的绑定撞号
	cidB := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pubA, _, _ := nodekey.Generate()
	// 生成焊机密钥，失败则绑定无法开始
	pubB, _, _ := nodekey.Generate()
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cidA, "焊机A", pubA, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cidB, "焊机B", pubB, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cidA, "ARM-A"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cidB, "ARM-B"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 平板登录，失败则本步验收不能继续
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败就停，避免带着错误继续验
	if err != nil || sess.Token == "" || sess.Account.ID != op.acc.ID || len(sess.Devices) != 2 || len(sess.UnwrapKey) != 32 {
		// 平板登录结果不对就失败
		t.Fatalf("pad %+v %v", sess, err)
	}
	// 按臂号收集设备，缺哪台就能看出来
	seen := map[string]factory.PadDevice{}
	// 逐台看平板列出的设备，空号即失败
	for _, d := range sess.Devices {
		// 列出的设备缺臂号就失败，已登记的号必须带上
		if d.DeviceSerial == "" {
			// 设备结果不对就失败，臂号或密钥有误
			t.Fatalf("device %+v", d)
		}
		// 按臂号收进这台设备，空号或重复即失败
		seen[d.DeviceSerial] = d
	}
	// 列表里没有这台焊机就失败，平板没列全
	if _, ok := seen["ARM-A"]; !ok {
		// 列表里没有焊机A就失败，平板没列全
		t.Fatal("missing ARM-A")
	}
	// 列表里没有这台焊机就失败，平板没列全
	if _, ok := seen["ARM-B"]; !ok {
		// 列表里没有焊机B就失败，平板没列全
		t.Fatal("missing ARM-B")
	}

	// 列出焊机，失败则本步验收不能继续
	listed, err := fac.ListClients(ctx, saTok)
	// 列出焊机失败就停，避免带着错误继续验
	if err != nil {
		// 列出焊机失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 逐台看焊机，不该还占着旧操作员
	for _, c := range listed {
		// 焊机上的操作员归属不对就失败
		if c.OperatorID != nil {
			// 登录日志内容不对就失败
			t.Fatalf("pad login occupied operator %+v", c)
		}
	}

	// 期望读焊机收件箱被拒为越权，放行即失败
	if _, err := fac.ClientInbox(ctx, sess.Token, cidA); !errors.Is(err, domain.ErrForbidden) {
		// 平板令牌读焊机信箱没被拒就失败
		t.Fatalf("operator inbox: %v", err)
	}
	// 读平板收件箱失败就停，避免带着错误继续验
	if _, err := fac.PadClientInbox(ctx, sess.Token); err != nil {
		// 平板信箱读失败，登录后应当能看
		t.Fatalf("pad inbox: %v", err)
	}
	// 校验通道身份失败就停，避免带着错误继续验
	if _, err := fac.AuthClientMQTT(ctx, op.acc.ID, sess.Token); err != nil {
		// 人员身份走通道没通过就失败
		t.Fatalf("pad person mqtt: %v", err)
	}
	// 校验通道身份失败就停，避免带着错误继续验
	if _, err := fac.AuthClientMQTT(ctx, cidA, sess.Token); err != nil {
		// 焊机身份走通道没通过就失败
		t.Fatalf("pad bound mqtt: %v", err)
	}

	// 读取审计，失败则本步验收不能继续
	rows, err := fac.ListAudit(ctx)
	// 读取审计失败就停，避免带着错误继续验
	if err != nil {
		// 读取审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 文本里夹带秘密或禁词就失败，说明泄密了
	if !audit.ContainsAny(audit.Dump(rows), "pad_login") {
		// 审计里没有这次动作就失败，记录丢了
		t.Fatalf("audit %s", audit.Dump(rows))
	}
}

// 验收平板在场与通道在线分开记，记混即失败
func TestPadLoginMarksAppOnlineAndVersion(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 读取名册，失败则本步验收不能继续
	cat, err := fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 网页登录不该把应用标成在线，标了即失败
	if cat.Me.AppOnline {
		// 网页登录不该把应用标成在线，标了即失败
		t.Fatal("web login must not mark app online")
	}
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID && (p.AppOnline || p.AppVersion != 0 || p.AppLastSeenAt != nil) {
			// 登录前应用却已在线或有版本，应当还空
			t.Fatalf("idle app %+v", p)
		}
	}

	// 平板登录失败就停，避免带着错误继续验
	if _, err := fac.LoginPad(ctx, "op", "op-pass"); err != nil {
		// 平板登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 记下应用在场失败就停，避免带着错误继续验
	if err := fac.NoteAppPresence(ctx, op.acc.ID, 52, "6.1.1"); err != nil {
		// 记下应用在场失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 读取名册，失败则本步验收不能继续
	cat, err = fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 留出账号位，循环里找到目标再写上
	var row factory.Account
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID {
			// 记下这名人员，后面核对他的在线或版本
			row = p
		}
	}
	// 在场上报后版本和时间必须对上，缺了即失败
	if row.AppOnline || row.AppVersion != 52 || row.AppVersionName != "6.1.1" || row.AppLastSeenAt == nil {
		// 在场上报不该把通道标成在线，标了即失败
		t.Fatalf("pad session must not mark mqtt online %+v", row)
	}

	// 改通道在线标记，没改则名册状态对不上
	fac.NoteAppMQTT(ctx, op.acc.ID, true)
	// 读取名册，失败则本步验收不能继续
	cat, err = fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID {
			// 记下这名人员，后面核对他的在线或版本
			row = p
		}
	}
	// 通道在线标记必须跟着上报变，没变即失败
	if !row.AppOnline || row.AppVersion != 52 {
		// 应当在线却没标上就失败
		t.Fatalf("mqtt online %+v", row)
	}

	// 把在线上报编成报文，编空则状态送不出去
	raw, _ := json.Marshal(map[string]any{"typ": "presence", "version": 53, "versionName": "6.1.2"})
	// 送上应用上报，送不进去则在线状态没记下
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	// 改通道在线标记，没改则名册状态对不上
	fac.NoteAppMQTT(ctx, op.acc.ID, true)
	// 改通道在线标记，没改则名册状态对不上
	fac.NoteAppMQTT(ctx, op.acc.ID, false)
	// 读取名册，失败则本步验收不能继续
	cat, err = fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID && (!p.AppOnline || p.AppVersion != 53 || p.AppVersionName != "6.1.2") {
			// 上报后通道状态没跟上就失败
			t.Fatalf("one mqtt still online %+v", p)
		}
	}
	// 改通道在线标记，没改则名册状态对不上
	fac.NoteAppMQTT(ctx, op.acc.ID, false)
	// 读取名册，失败则本步验收不能继续
	cat, err = fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID && (p.AppOnline || p.AppVersion != 53) {
			// 应当离线却仍在线就失败
			t.Fatalf("mqtt offline %+v", p)
		}
	}
}

// 验收登录日志留下设备和网络，缺字段即失败
func TestPersonLoginLogsKeepDeviceAndWifi(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 新开一个身份，避免和别的绑定撞号
	cid := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pub, _, err := nodekey.Generate()
	// 生成焊机密钥失败就停，避免带着错误继续验
	if err != nil {
		// 生成焊机密钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cid, "焊机A", pub, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-1"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 平板登录失败就停，避免带着错误继续验
	if _, err := fac.LoginPad(ctx, "op", "op-pass"); err != nil {
		// 平板登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 装一份登录现场，用来核设备和网络有没有留下
	snap := factory.LoginSnap{
		Kind: factory.LoginKindPad, AppVersion: 52, AppVersionName: "6.1.1",
		DeviceModel: "TB-X606F", DeviceManufacturer: "Lenovo", AndroidRelease: "10",
		NetworkName: "Factory-WiFi",
	}
	// 记下应用登录失败就停，避免带着错误继续验
	if err := fac.RecordAppLogin(ctx, op.acc.ID, snap); err != nil {
		// 记下应用登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 列出人员登录记录的结果必须符合期望，不符即失败
	if _, err := fac.ListPersonLogins(ctx, op.tok, op.acc.ID); err == nil {
		// 操作员不该读到登录日志，读到即失败
		t.Fatal("operator must not read login logs")
	}
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err := fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 列出人员登录记录失败就停，避免带着错误继续验
	if err != nil || len(rows) != 1 {
		// 登录日志内容不对就失败
		t.Fatalf("logs %+v %v", rows, err)
	}
	// 取最近一条登录记录，字段丢了即失败
	got := rows[0]
	// 在场上报后版本和时间必须对上，缺了即失败
	if got.Kind != factory.LoginKindPad || got.AppVersion != 52 || got.AppVersionName != "6.1.1" ||
		got.DeviceModel != "TB-X606F" || got.NetworkName != "Factory-WiFi" || got.DeviceSerial != "" {
		// 登录日志内容不对就失败
		t.Fatalf("pad log %+v", got)
	}

	// 把在线上报编成报文，编空则状态送不出去
	raw, _ := json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"deviceModel": "TB-X606F", "deviceManufacturer": "Lenovo", "androidRelease": "10",
		"networkName": "Factory-WiFi",
	})
	// 送上应用上报，送不进去则在线状态没记下
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err = fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 列出人员登录记录失败就停，避免带着错误继续验
	if err != nil || len(rows) != 1 {
		// 同一通道不该多记一条登录，多了即失败
		t.Fatalf("same mqtt %+v %v", rows, err)
	}

	// 把在线上报编成报文，编空则状态送不出去
	raw, _ = json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"deviceSerial": "ARM-1", "deviceModel": "TB-X606F", "deviceManufacturer": "Lenovo",
		"androidRelease": "10", "networkName": "Factory-WiFi",
	})
	// 送上应用上报，送不进去则在线状态没记下
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err = fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 列出人员登录记录失败就停，避免带着错误继续验
	if err != nil || len(rows) != 2 || rows[0].Kind != factory.LoginKindMQTT || rows[0].DeviceSerial != "ARM-1" || rows[0].ClientName != "焊机A" {
		// 带臂号的上报没记成通道登录就失败
		t.Fatalf("serial mqtt %+v %v", rows, err)
	}
	// 通道登录必须带上这台焊机，绑错即失败
	if rows[0].ClientID == nil || *rows[0].ClientID != cid {
		// 焊机身份对不上就失败
		t.Fatalf("client id %+v", rows[0].ClientID)
	}
	// 读取名册，失败则本步验收不能继续
	cat, err := fac.Catalog(ctx, saTok)
	// 读取名册失败就停，避免带着错误继续验
	if err != nil {
		// 读取名册失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 留出账号位，名册里找到目标再核对
	var seen factory.Account
	// 逐个核对名册里的人，漏掉这名即失败
	for _, p := range cat.People {
		// 名册里对上这名操作员才记下，对不上则没人可核
		if p.ID == op.acc.ID {
			// 记下名册里的这个人，设备记错即失败
			seen = p
		}
	}
	// 带臂号的上报必须记成通道登录，记错即失败
	if seen.AppClientName != "焊机A" || seen.AppDeviceSerial != "ARM-1" {
		// 名册上的最后设备不是这台焊机就失败
		t.Fatalf("last device %+v", seen)
	}
}

// 验收平板登录发解钥且不要求臂号，缺钥即失败
func TestPadLoginIssuesUnwrapKeyWithoutSerial(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	_ = mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 新开一个身份，避免和别的绑定撞号
	cid := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pub, _, err := nodekey.Generate()
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

	// 平板登录，失败则本步验收不能继续
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败就停，避免带着错误继续验
	if err != nil || len(sess.Devices) != 1 {
		// 平板登录结果不对就失败
		t.Fatalf("pad %+v %v", sess, err)
	}
	// 平板列出的设备不该带臂号，带上即失败
	if sess.Devices[0].ID != cid || sess.Devices[0].DeviceSerial != "" || len(sess.UnwrapKey) != 32 {
		// 设备结果不对就失败，臂号或密钥有误
		t.Fatalf("device %+v key %d", sess.Devices[0], len(sess.UnwrapKey))
	}
}

// 验收本机登录清掉另一台的操作员，没清即失败
func TestLoginOnClientClearsOtherDevice(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 新开一个身份，避免和别的绑定撞号
	cidA := id.New()
	// 新开一个身份，避免和别的绑定撞号
	cidB := id.New()
	// 生成焊机密钥，失败则绑定无法开始
	pubA, _, _ := nodekey.Generate()
	// 生成焊机密钥，失败则绑定无法开始
	pubB, _, _ := nodekey.Generate()
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cidA, "A", pubA, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 接受焊机绑定失败就停，避免带着错误继续验
	if _, err := fac.AcceptBinding(ctx, cidB, "B", pubB, 1); err != nil {
		// 接受焊机绑定失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cidA, "ARM-A"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.RegisterDevice(ctx, cidB, "ARM-B"); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 在焊机上登录失败就停，避免带着错误继续验
	if _, err := fac.LoginOnClient(ctx, cidA, "ARM-A", "op", "op-pass"); err != nil {
		// 在焊机上登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 在焊机上登录失败就停，避免带着错误继续验
	if _, err := fac.LoginOnClient(ctx, cidB, "ARM-B", "op", "op-pass"); err != nil {
		// 在焊机上登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 列出焊机，失败则本步验收不能继续
	listed, err := fac.ListClients(ctx, saTok)
	// 列出焊机失败就停，避免带着错误继续验
	if err != nil {
		// 列出焊机失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 先当作两台都没被清掉，清掉了再标上
	var aOp, bOp bool
	// 逐台看焊机，不该还占着旧操作员
	for _, c := range listed {
		// 焊机上的操作员归属不对就失败
		if c.ID == cidA && c.OperatorID != nil && *c.OperatorID == op.acc.ID {
			// 标上第一台仍绑着该人，用来验有没有清掉
			aOp = true
		}
		// 焊机上的操作员归属不对就失败
		if c.ID == cidB && c.OperatorID != nil && *c.OperatorID == op.acc.ID {
			// 标上第二台绑着该人，没标上即登录没落到新机
			bOp = true
		}
	}
	// 旧焊机上还留着操作员即失败，登录没互斥
	if aOp || !bOp {
		// 旧焊机上还有操作员就失败，登录没互斥
		t.Fatalf("operator still on old client: a=%v b=%v", aOp, bOp)
	}
}

// 验收应用令牌跟着密钥有效期走，提前过期即失败
func TestAppTokenUsesKeyTTL(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 平板登录，失败则本步验收不能继续
	open, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败就停，避免带着错误继续验
	if err != nil {
		// 平板登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 算出令牌哈希，对不上则找不到这条会话
	row, err := fac.Store().SessionByTokenHash(ctx, secret.TokenHash(open.Token))
	// 计算令牌哈希失败就停，避免带着错误继续验
	if err != nil {
		// 计算令牌哈希失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 零有效期应维持到退出，提前过期即失败
	if row.ExpiresAt.Year() < 9999 {
		// 零有效期却提前过期就失败，应维持到退出
		t.Fatalf("ttl 0 should last until logout, expires %s", row.ExpiresAt)
	}
	// 读取焊机策略，失败则本步验收不能继续
	cur, err := fac.GetClientPolicy(ctx, saTok)
	// 读取焊机策略失败就停，避免带着错误继续验
	if err != nil {
		// 读取焊机策略失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 把密钥有效期改成九十秒，用来验令牌跟着过期
	cur.KeyTTLSeconds = 90
	// 改写焊机策略失败就停，避免带着错误继续验
	if _, err := fac.SetClientPolicy(ctx, saTok, cur); err != nil {
		// 改写焊机策略失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 平板登录，失败则本步验收不能继续
	limited, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败就停，避免带着错误继续验
	if err != nil {
		// 平板登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 算出令牌哈希，对不上则找不到这条会话
	row, err = fac.Store().SessionByTokenHash(ctx, secret.TokenHash(limited.Token))
	// 计算令牌哈希失败就停，避免带着错误继续验
	if err != nil {
		// 计算令牌哈希失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 取当前时刻，用来核对令牌或凭证是否过期
	now := time.Now().UTC()
	// 九十秒有效期不在窗口内即失败
	if row.ExpiresAt.Before(now.Add(60*time.Second)) || row.ExpiresAt.After(now.Add(2*time.Minute)) {
		// 九十秒有效期没按密钥期限过期就失败
		t.Fatalf("ttl 90s expires %s", row.ExpiresAt)
	}
}
