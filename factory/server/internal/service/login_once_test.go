// 同一次示教器登录只记一条登录记录：厂网登录记一条，连臂补报同一台臂不再另起（D-78）。
package service_test

import (
	"context"
	"testing"
	"time"

	factory "wmesh/factory/internal/service"
)

// 登录时已带臂号：补报同一台不加条；换了臂另记；重新登录再记一条。
// 登录时没连臂：补报的臂号并进登录那条。
func TestPadLoginRecordedOncePerSession(t *testing.T) {
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
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass-1", factory.RoleOperator)
	// 数他的登录记录。
	count := func() int {
		// 读出他的登录记录。
		rows, err := fac.Store().ListPersonLogins(ctx, op.acc.ID)
		// 读不出就停住。
		if err != nil {
			// 把错误打出来并停住。
			t.Fatal(err)
		}
		// 交回条数。
		return len(rows)
	}
	// 厂网登录，和 HTTP 层一样随登录记一条带臂号的现场。
	sess := padLoginRecorded(t, ctx, fac, "op", "op-pass-1", "FA:KE:00:00:00:01")
	// 登录后应是一条。
	if n := count(); n != 1 {
		// 条数不对就停住。
		t.Fatalf("after login = %d", n)
	}
	// 连上同一台臂后补报，不该再加一条。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "FA:KE:00:00:00:01"}); err != nil {
		// 补报失败就停住。
		t.Fatal(err)
	}
	// 仍是一条。
	if n := count(); n != 1 {
		// 重复记了就停住。
		t.Fatalf("after same-arm report = %d", n)
	}
	// 换到另一台臂，照常另记。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "FA:KE:00:00:00:02"}); err != nil {
		// 补报失败就停住。
		t.Fatal(err)
	}
	// 换臂后是两条。
	if n := count(); n != 2 {
		// 条数不对就停住。
		t.Fatalf("after arm switch = %d", n)
	}
	// 重新登录是一次新登录，再记一条。
	sess = padLoginRecorded(t, ctx, fac, "op", "op-pass-1", "FA:KE:00:00:00:02")
	// 新登录后是三条。
	if n := count(); n != 3 {
		// 条数不对就停住。
		t.Fatalf("after relogin = %d", n)
	}
	// 新会话里补报同一台臂，不加条。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "fa:ke:00:00:00:02"}); err != nil {
		// 补报失败就停住。
		t.Fatal(err)
	}
	// 仍是三条。
	if n := count(); n != 3 {
		// 重复记了就停住。
		t.Fatalf("after relogin same-arm report = %d", n)
	}
	// 登录时没连臂。
	sess = padLoginRecorded(t, ctx, fac, "op", "op-pass-1", "")
	// 没连臂的登录也记一条。
	if n := count(); n != 4 {
		// 条数不对就停住。
		t.Fatalf("after armless login = %d", n)
	}
	// 连上臂后补报，并进登录那条。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "FA:KE:00:00:00:03", NetworkName: "wmesh-arm"}); err != nil {
		// 补报失败就停住。
		t.Fatal(err)
	}
	// 仍是四条。
	if n := count(); n != 4 {
		// 另起了一条就停住。
		t.Fatalf("after armless report = %d", n)
	}
	// 最近那条应补上臂号和网络名。
	last, err := fac.Store().LatestPersonLogin(ctx, op.acc.ID)
	// 读不出或没补上就停住。
	if err != nil || last.DeviceSerial != "FA:KE:00:00:00:03" || last.NetworkName != "wmesh-arm" {
		// 把记录打出来并停住。
		t.Fatalf("merged login %+v %v", last, err)
	}
	// 再登录一次没连臂，随后补报一条原时刻早于本次会话的现场（更早那次登录的）。
	sess = padLoginRecorded(t, ctx, fac, "op", "op-pass-1", "")
	// 原时刻取一小时前，早于本次会话建立。
	past := time.Now().Add(-time.Hour)
	// 补报更早那次的现场。
	if err := fac.RecordOwnAppLogin(ctx, sess.Token, factory.LoginSnap{DeviceSerial: "FA:KE:00:00:00:04", OccurredAt: &past}); err != nil {
		// 补报失败就停住。
		t.Fatal(err)
	}
	// 不能并进本次登录那条，照常另记，共六条。
	if n := count(); n != 6 {
		// 条数不对就停住。
		t.Fatalf("after backfill of older login = %d", n)
	}
	// 本次登录那条臂号仍空着，没被更早那次的现场填上。
	rows, err := fac.Store().ListPersonLogins(ctx, op.acc.ID)
	// 读不出或本次那条被填了就停住。
	if err != nil || rows[0].DeviceSerial != "" {
		// 把记录打出来并停住。
		t.Fatalf("latest login after older backfill %+v %v", rows, err)
	}
}

// padLoginRecorded 示教器厂网登录，并像 HTTP 层那样随登录记一条现场。
func padLoginRecorded(t *testing.T, ctx context.Context, fac *factory.Service, login, pass, serial string) factory.PadSession {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 厂网登录。
	sess, err := fac.LoginPad(ctx, login, pass)
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 随登录记现场，和 HTTP 层同一入口。
	if err := fac.RecordAppLogin(ctx, sess.Account.ID, factory.LoginSnap{Kind: factory.LoginKindPad, DeviceSerial: serial}); err != nil {
		// 记不下就停住。
		t.Fatalf("record login: %v", err)
	}
	return sess
}
