// C1：厂网登录领钥、列设备、记在场，第二次示教器登录作废旧令牌。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/secret"
	factory "wmesh/factory/internal/service"
)

// 验收平板登录能列出已登记焊机，缺一台即失败
func TestPadLoginListsDevicesWithoutSerial(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)

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
	if _, err := fac.Store().PinDeviceSerial(ctx, cidA, "ARM-A", false); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登记机械臂号失败就停，避免带着错误继续验
	if _, err := fac.Store().PinDeviceSerial(ctx, cidB, "ARM-B", false); err != nil {
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
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)

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
	fac.HandleAppUp(ctx, op.acc.ID, uuid.Nil, raw)
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
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)
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
	if _, err := fac.Store().PinDeviceSerial(ctx, cid, "ARM-1", false); err != nil {
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
	fac.HandleAppUp(ctx, op.acc.ID, uuid.Nil, raw)
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
	fac.HandleAppUp(ctx, op.acc.ID, uuid.Nil, raw)
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

// 验收网络名和臂号带 WM2 的在场上报照常记进登录记录
func TestPresenceWithWM2RecordsLogin(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)
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
	// 钉一个带 WM2 的臂号，失败则上报对不上设备
	if _, err := fac.Store().PinDeviceSerial(ctx, cid, "ARM-WM2", false); err != nil {
		// 登记机械臂号失败就把原因打出并停掉本例
		t.Fatal(err)
	}

	// 网络名和臂号都带 WM2 的在场上报
	raw, _ := json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"deviceSerial": "ARM-WM2", "networkName": "WM2-Line",
	})
	// 送上应用上报，被当成夹带就记不进登录
	fac.HandleAppUp(ctx, op.acc.ID, uuid.Nil, raw)
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err := fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 没记成一条通道登录或网络名不对就失败
	if err != nil || len(rows) != 1 || rows[0].Kind != factory.LoginKindMQTT || rows[0].NetworkName != "WM2-Line" {
		// 带 WM2 的上报被丢了，打出登录记录
		t.Fatalf("wm2 presence %+v %v", rows, err)
	}
	// 臂号要对上这台焊机，对不上即上报没认出设备
	if rows[0].DeviceSerial != "ARM-WM2" || rows[0].ClientID == nil || *rows[0].ClientID != cid {
		// 设备对不上就失败，打出这条记录
		t.Fatalf("wm2 device %+v", rows[0])
	}
}

// 验收平板登录发解钥且不要求臂号，缺钥即失败
func TestPadLoginIssuesUnwrapKeyWithoutSerial(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	_ = mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)

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

// 验收应用令牌跟着密钥有效期走，提前过期即失败
func TestAppTokenUsesKeyTTL(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，失败则夹具缺这个身份
	mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)
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

// 验收同一人第二次示教器登录后旧令牌作废，网页令牌和别人的不受影响
func TestSecondPadLoginRevokesOldAppToken(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，带回的令牌是网页会话
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)

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

	// 别人先在厂网登录，用来验只删本人的令牌
	other, err := fac.LoginPad(ctx, "sa", "sa-pass")
	// 别人登录失败就停，避免带着错误继续验
	if err != nil {
		// 别人登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 第一台示教器登录拿到旧令牌 A
	first, err := fac.LoginPad(ctx, "op", "op-pass")
	// 第一次登录失败就停，避免带着错误继续验
	if err != nil {
		// 第一次登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 第二次登录前旧令牌应当可用，否则后面的断言没意义
	if _, err := fac.RequireActive(ctx, first.Token); err != nil {
		// 旧令牌一登录就不能用即失败，夹具不对
		t.Fatalf("first token before second login: %v", err)
	}

	// 第二台示教器厂网登录拿到新令牌 B
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 第二次厂网登录失败就停，避免带着错误继续验
	if err != nil {
		// 第二次厂网登录失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 记下新令牌，后面验它仍可用
	second := sess.Token

	// 旧令牌续接会话应被拒为未登录，放行即失败
	if _, err := fac.RequireActive(ctx, first.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 旧机令牌还能拉包上送就失败
		t.Fatalf("old token still active: %v", err)
	}
	// 旧令牌连通道应被拒为未登录，放行即失败
	if _, err := fac.AuthClientMQTT(ctx, cid, first.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 旧机令牌还能连通道就失败
		t.Fatalf("old token mqtt: %v", err)
	}
	// 新令牌续接会话失败即失败，第二台应当能用
	if _, err := fac.RequireActive(ctx, second); err != nil {
		// 新令牌被误删就失败
		t.Fatalf("new token: %v", err)
	}
	// 新令牌连通道失败即失败，第二台应当能连
	if _, err := fac.AuthClientMQTT(ctx, cid, second); err != nil {
		// 新令牌连不上通道就失败
		t.Fatalf("new token mqtt: %v", err)
	}
	// 本人网页令牌应当保留，被一起删掉即失败
	if _, err := fac.RequireActive(ctx, op.tok); err != nil {
		// 网页会话被示教器登录踢掉就失败
		t.Fatalf("web token: %v", err)
	}
	// 别人的示教器令牌应当保留，被一起删掉即失败
	if _, err := fac.RequireActive(ctx, other.Token); err != nil {
		// 删到别人的会话就失败
		t.Fatalf("other person token: %v", err)
	}
}

// 通道落绑定：撞号、公钥不符、修订过旧只跳过这一台，后面照常落库，对账也不作废
func TestApplyWANBindingSkipsConflicts(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，避免带着错误继续验
	if err != nil {
		// 建厂失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 生成六把焊机公钥，第五把专拿来冒充换钥
	keys := make([][]byte, 6)
	// 逐把生成，任何一把失败就停
	for i := range keys {
		// 生成焊机密钥，失败则绑定无法开始
		pub, _, err := nodekey.Generate()
		// 生成焊机密钥失败就停，避免带着错误继续验
		if err != nil {
			// 生成焊机密钥失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 记下这把公钥，后面按序号取用
		keys[i] = pub
	}
	// A 已钉臂号，B 撞 A 的号，K 公钥不符，S 修订过旧，D 是正常的一台
	cidA, cidB, cidK, cidS, cidD := id.New(), id.New(), id.New(), id.New(), id.New()
	// 先把 A 随绑定补上 ARM-1，本机没号时应补上
	if err := fac.ApplyWANBinding(ctx, factory.WANBinding{ClientID: cidA, Name: "焊机A", PublicKey: keys[0], Revision: 1, DeviceSerial: "ARM-1"}); err != nil {
		// A 落不上就把原因打出并停掉本例
		t.Fatalf("bind a: %v", err)
	}
	// K 先以原公钥落下，后面换钥重推应被跳过
	if err := fac.ApplyWANBinding(ctx, factory.WANBinding{ClientID: cidK, Name: "焊机K", PublicKey: keys[2], Revision: 1}); err != nil {
		// K 落不上就把原因打出并停掉本例
		t.Fatalf("bind k: %v", err)
	}
	// S 先落到修订三，后面修订二重推应被跳过
	if err := fac.ApplyWANBinding(ctx, factory.WANBinding{ClientID: cidS, Name: "焊机S", PublicKey: keys[3], Revision: 3}); err != nil {
		// S 落不上就把原因打出并停掉本例
		t.Fatalf("bind s: %v", err)
	}

	// 模拟回连一轮：三台冲突排在前面，正常的 D 排在最后
	round := []factory.WANBinding{
		{ClientID: cidB, Name: "焊机B", PublicKey: keys[1], Revision: 1, ShortCode: "C0011", DeviceSerial: "ARM-1"},
		{ClientID: cidK, Name: "焊机K", PublicKey: keys[4], Revision: 2},
		{ClientID: cidS, Name: "焊机S-旧", PublicKey: keys[3], Revision: 2},
		{ClientID: cidD, Name: "焊机D", PublicKey: keys[5], Revision: 1, ShortCode: "C0012", DeviceSerial: "ARM-4"},
	}
	// 对账名单先放上 A，后面每落一台就加一台
	keep := []uuid.UUID{cidA}
	// 逐台落下，冲突也不能报错拖垮整轮
	for _, in := range round {
		// 冲突的设备应当只被跳过，报错即失败
		if err := fac.ApplyWANBinding(ctx, in); err != nil {
			// 单台冲突中止整轮就失败，通道会进不了常驻循环
			t.Fatalf("apply %s: %v", in.Name, err)
		}
		// 跳过的设备也留在对账名单里
		keep = append(keep, in.ClientID)
	}
	// 按名单对账，名单里的设备都不该被作废
	if err := fac.ReconcileBindings(ctx, keep); err != nil {
		// 对账失败就把原因打出并停掉本例
		t.Fatalf("reconcile: %v", err)
	}

	// 逐台读回，核对状态、修订、臂号和短码
	read := func(cid uuid.UUID) factory.Client {
		// 读回这台设备，读不到就停
		c, err := fac.Store().ClientByID(ctx, cid)
		// 读不到说明落库或对账出了错
		if err != nil {
			// 读设备失败就把原因打出并停掉本例
			t.Fatalf("read %s: %v", cid, err)
		}
		return c
	}
	// A 仍有效且保留 ARM-1
	if a := read(cidA); a.Status != factory.ClientStatusBound || a.DeviceSerial != "ARM-1" {
		// A 的号被抢或被作废就失败
		t.Fatalf("a %+v", a)
	}
	// B 绑定和短码落下，但撞号的臂号不补
	if b := read(cidB); b.Status != factory.ClientStatusBound || b.DeviceSerial != "" || b.ShortCode != "C0011" {
		// B 被作废、抢到号或短码没落就失败
		t.Fatalf("b %+v", b)
	}
	// K 保持原公钥和修订一，没被换钥也没被作废
	if k := read(cidK); k.Status != factory.ClientStatusBound || k.BindingRevision != 1 || string(k.PublicKey) != string(keys[2]) {
		// K 被换钥、升修订或作废就失败
		t.Fatalf("k %+v", k)
	}
	// S 保持修订三和原名，旧帧没覆盖也没被作废
	if s := read(cidS); s.Status != factory.ClientStatusBound || s.BindingRevision != 3 || s.Name != "焊机S" {
		// S 被旧帧覆盖或作废就失败
		t.Fatalf("s %+v", s)
	}
	// 排在冲突后面的 D 照常落下臂号和短码
	if d := read(cidD); d.Status != factory.ClientStatusBound || d.DeviceSerial != "ARM-4" || d.ShortCode != "C0012" {
		// D 没落全说明冲突拖累了后面的设备
		t.Fatalf("d %+v", d)
	}

	// 取一个已取消的上下文，模拟库连不上
	dead, cancel := context.WithCancel(ctx)
	// 立即取消，后面的库操作都会失败
	cancel()
	// 新开一台身份，库错时它不该落下
	cidX := id.New()
	// 库错不是单台冲突，必须交回让通道重连
	if err := fac.ApplyWANBinding(dead, factory.WANBinding{ClientID: cidX, Name: "焊机X", Revision: 1}); err == nil {
		// 库错被吞掉就失败，通道会以为已经落下
		t.Fatal("db error swallowed")
	}
	// 库错的那台不该留在本厂名录里
	if _, err := fac.Store().ClientByID(ctx, cidX); !errors.Is(err, domain.ErrNotFound) {
		// 库错却落下了设备就失败
		t.Fatalf("x landed: %v", err)
	}
}
