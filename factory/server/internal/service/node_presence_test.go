// 回连现场：设备认会话里鉴权过的本机，载荷自报号不算数。
package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 验收在场上报记的是会话那台，载荷填别的号即失败
func TestPresenceUsesSessionClient(t *testing.T) {
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
	// 会话里鉴权过的那台焊机
	cid := id.New()
	// 载荷里冒报的另一台焊机
	other := id.New()
	// 两台各绑一份，冒报号也能在名录里对上
	for _, dev := range []struct {
		id     uuid.UUID // 焊机身份，绑定和钉号都用它
		name   string    // 焊机名，现场里显示的就是它
		serial string    // 机械臂号，钉上后名录才补得上
	}{{cid, "焊机A", "ARM-1"}, {other, "焊机B", "ARM-2"}} {
		// 生成焊机密钥，失败则绑定无法开始
		pub, _, err := nodekey.Generate()
		// 生成焊机密钥失败就停，避免带着错误继续验
		if err != nil {
			// 生成焊机密钥失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 接受焊机绑定失败就停，避免带着错误继续验
		if _, err := fac.AcceptBinding(ctx, dev.id, dev.name, pub, 1); err != nil {
			// 接受焊机绑定失败就把原因打出并停掉本例
			t.Fatal(err)
		}
		// 登记机械臂号失败就停，避免带着错误继续验
		if _, err := fac.RegisterDevice(ctx, saTok, dev.id, dev.serial); err != nil {
			// 登记机械臂号失败就把原因打出并停掉本例
			t.Fatal(err)
		}
	}

	// 载荷自报成另一台，不带臂号，只能靠会话认设备
	raw, _ := json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"networkName": "Factory-WiFi", "clientId": other.String(),
	})
	// 从会话那台送上在场上报
	fac.HandleAppUp(ctx, op.acc.ID, cid, raw)
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err := fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 没记成一条通道登录就失败
	if err != nil || len(rows) != 1 || rows[0].Kind != factory.LoginKindMQTT {
		// 通道登录没记下，打出登录记录
		t.Fatalf("presence %+v %v", rows, err)
	}
	// 设备必须是会话那台，记成冒报那台即失败
	if rows[0].ClientID == nil || *rows[0].ClientID != cid || rows[0].ClientName != "焊机A" || rows[0].DeviceSerial != "ARM-1" {
		// 现场设备被载荷盖掉，打出这条记录
		t.Fatalf("session device %+v", rows[0])
	}

	// 换个版本让现场有变化，会话不带本机
	raw, _ = json.Marshal(map[string]any{
		"typ": "presence", "version": 53, "versionName": "6.1.2",
		"networkName": "Factory-WiFi", "clientId": other.String(),
	})
	// 会话没有本机身份时送上在场上报
	fac.HandleAppUp(ctx, op.acc.ID, uuid.Nil, raw)
	// 列出人员登录记录，失败则本步验收不能继续
	rows, err = fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	// 版本变了应当多记一条通道登录，没记即失败
	if err != nil || len(rows) != 2 || rows[0].Kind != factory.LoginKindMQTT || rows[0].AppVersion != 53 {
		// 新现场没记下，打出登录记录
		t.Fatalf("presence without session %+v %v", rows, err)
	}
	// 载荷自报号不能当设备，记上即失败
	if rows[0].ClientID != nil || rows[0].ClientName != "" {
		// 载荷自报号被采信，打出这条记录
		t.Fatalf("payload device trusted %+v", rows[0])
	}
}
