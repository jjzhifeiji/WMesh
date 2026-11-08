// 令牌与设备绑定（D-59）：示教器会话钉住登录时（或登录后第一次对上臂时）的那台 Client，MQTT 只认这一台。
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 令牌 A 钉设备 X：报 Y 拒、报 X 过；没钉时报已登记设备也拒；同人在 Y 重新登录后 Y 过，X 的旧令牌按作废拒。
func TestAuthClientMQTTRequiresSessionClient(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	op := mustCreateRole(t, ctx, fac, saTok, "opa", "opa-pass-1", factory.RoleOperator)
	// 登记两台设备 X、Y，各钉一个臂号。
	cx := mustBoundClient(t, ctx, fac, "焊机X", "ARM-X")
	// 第二台设备。
	cy := mustBoundClient(t, ctx, fac, "焊机Y", "ARM-Y")

	// 操作员登录时没连臂，令牌还没钉设备。
	first, err := fac.LoginPad(ctx, "opa", "opa-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 没钉设备时报已登记设备也不让连：这是复测里「目标设备无人在线就能冒充」的遗留。
	if _, err := fac.AuthClientMQTT(ctx, cy, first.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("unbound token as Y: %v", err)
	}
	// 没钉设备时用本人身份照常收听。
	if _, err := fac.AuthClientMQTT(ctx, op.acc.ID, first.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("unbound token as self: %v", err)
	}
	// 自报 clientId 是 Y、臂号却是 X，自相矛盾不钉。
	if err := fac.BindPadClient(ctx, first.Token, factory.LoginSnap{DeviceSerial: "ARM-X", ClientID: &cy}); err != nil {
		// 不该报错就停住。
		t.Fatalf("mismatched report: %v", err)
	}
	// 矛盾的自报没钉上，报 X 仍拒。
	if _, err := fac.AuthClientMQTT(ctx, cx, first.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("after mismatched report: %v", err)
	}
	// 更早那次登录的补报（原时刻早于本次会话）不钉本次会话。
	old := time.Now().Add(-time.Hour)
	// 补报 X，原时刻在一小时前。
	if err := fac.RecordOwnAppLogin(ctx, first.Token, factory.LoginSnap{DeviceSerial: "ARM-X", OccurredAt: &old}); err != nil {
		// 不该报错就停住。
		t.Fatalf("old backfill: %v", err)
	}
	// 旧补报没钉上，报 X 仍拒。
	if _, err := fac.AuthClientMQTT(ctx, cx, first.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("after old backfill: %v", err)
	}
	// 登录后第一次对上臂 X，补报现场即钉住 X。
	if err := fac.RecordOwnAppLogin(ctx, first.Token, factory.LoginSnap{DeviceSerial: "arm-x", ClientID: &cx}); err != nil {
		// 钉不上就停住。
		t.Fatalf("bind X: %v", err)
	}
	// 令牌 A 报 X 通过。
	if _, err := fac.AuthClientMQTT(ctx, cx, first.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("A as X: %v", err)
	}
	// 令牌 A 报 Y 拒绝，Y 无人在线也一样。
	if _, err := fac.AuthClientMQTT(ctx, cy, first.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("A as Y: %v", err)
	}
	// 同一次登录里改报 Y 不改绑定，交回冲突，示教器提示重新登录。
	if err := fac.RecordOwnAppLogin(ctx, first.Token, factory.LoginSnap{DeviceSerial: "ARM-Y", ClientID: &cy}); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 改绑成功或报错不对就停住。
		t.Fatalf("rebind Y: %v", err)
	}
	// 冲突之后绑定仍是 X。
	if _, err := fac.AuthClientMQTT(ctx, cy, first.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 被改绑就停住。
		t.Fatalf("A as Y after rebind: %v", err)
	}
	// 钉住设备后本人身份照常可用。
	if _, err := fac.AuthClientMQTT(ctx, op.acc.ID, first.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("bound token as self: %v", err)
	}

	// 同一人到 Y 上重新登录，登录请求带着 Y 的臂号（厂端 HTTP 层登录后随即钉设备）。
	second, err := fac.LoginPad(ctx, "opa", "opa-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("relogin: %v", err)
	}
	// 按登录带的臂号钉 Y。
	if err := fac.BindPadClient(ctx, second.Token, factory.LoginSnap{DeviceSerial: "ARM-Y"}); err != nil {
		// 钉不上就停住。
		t.Fatalf("bind Y: %v", err)
	}
	// 新令牌报 Y 通过。
	if _, err := fac.AuthClientMQTT(ctx, cy, second.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("B as Y: %v", err)
	}
	// 新令牌报 X 拒绝。
	if _, err := fac.AuthClientMQTT(ctx, cx, second.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("B as X: %v", err)
	}
	// X 上的旧令牌随新登录作废，按令牌失效拒（示教器据此登出）。
	if _, err := fac.AuthClientMQTT(ctx, cx, first.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行或拒绝码不对就停住。
		t.Fatalf("old A as X: %v", err)
	}
}

// 臂号对不上本厂设备、没带臂号时都不钉，也不报错；网页会话不钉设备。
func TestBindPadClientIgnoresUnknownSerial(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿网页会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 登记一台设备。
	cx := mustBoundClient(t, ctx, fac, "焊机X", "ARM-X")
	// 超管在示教器上登录。
	sess, err := fac.LoginPad(ctx, "sa", personPass("sa"))
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 没登记的臂号、空臂号都不钉也不报错。
	for _, serial := range []string{"ARM-NONE", "  "} {
		// 补报一次。
		if err := fac.BindPadClient(ctx, sess.Token, factory.LoginSnap{DeviceSerial: serial}); err != nil {
			// 不该报错就停住。
			t.Fatalf("serial %q: %v", serial, err)
		}
	}
	// 都没钉上，报 X 仍拒。
	if _, err := fac.AuthClientMQTT(ctx, cx, sess.Token); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("unbound as X: %v", err)
	}
	// 网页会话带臂号也不钉，不报错。
	if err := fac.BindPadClient(ctx, saTok, factory.LoginSnap{DeviceSerial: "ARM-X"}); err != nil {
		// 不该报错就停住。
		t.Fatalf("web session: %v", err)
	}
	// 网页令牌报 X 仍拒。
	if _, err := fac.AuthClientMQTT(ctx, cx, saTok); !errors.Is(err, domain.ErrSessionClientMismatch) {
		// 放行就停住。
		t.Fatalf("web as X: %v", err)
	}
}

// mustBoundClient 登记一台未作废设备并钉臂号，失败就停住。
func mustBoundClient(t *testing.T, ctx context.Context, fac *factory.Service, name, serial string) uuid.UUID {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 新开一个身份，避免和别的绑定撞号。
	cid := id.New()
	// 生成设备公钥。
	pub, _, err := nodekey.Generate()
	// 生成失败就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 接受绑定，失败就停住。
	if _, err := fac.AcceptBinding(ctx, cid, name, pub, 1); err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 钉臂号，失败就停住。
	if _, err := fac.Store().PinDeviceSerial(ctx, cid, serial, false); err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	return cid
}

// 平板钟比厂端慢几分钟时，本次登录后臂网上攒的补报原时刻略早于会话建立，仍按本次登录钉设备（P3-R1）。
func TestBindPadClientToleratesSlowPadClock(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	mustCreateRole(t, ctx, fac, saTok, "opb", "opb-pass-1", factory.RoleOperator)
	// 登记一台设备 X。
	cx := mustBoundClient(t, ctx, fac, "焊机X", "ARM-X")
	// 操作员登录时没连臂，令牌还没钉设备。
	sess, err := fac.LoginPad(ctx, "opb", "opb-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 平板钟慢三分钟：补报原时刻比会话建立早三分钟。
	slow := time.Now().Add(-3 * time.Minute)
	// 补报 X，带平板本机原时刻。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "ARM-X", ClientID: &cx, OccurredAt: &slow}); err != nil {
		// 不该报错就停住。
		t.Fatalf("slow backfill: %v", err)
	}
	// 钟差容让之内照样钉上 X，报 X 通过。
	if _, err := fac.AuthClientMQTT(ctx, cx, sess.Token); err != nil {
		// 被误拒说明钟慢就钉不上，停住。
		t.Fatalf("slow clock as X: %v", err)
	}
}
