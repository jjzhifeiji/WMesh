// 收回设备到未分配：修订升高、原厂收到作废；之后再分配修订接着往上。厂端保活带的最近登录只认本厂设备。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 验收回：原厂收到更高修订的作废，不发绑定；未分配再收回被拒；再分配修订接着升。
func TestUnassignClientVoidsOldFactory(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 登记一家厂，设备先分到这里。
	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管")
	// 登记失败就停，后面没有厂可分。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 登记一台并当场分到厂A，修订从一起。
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机", id.New(), a.Factory.ID, nil, "ARM-UN")
	// 登记失败或没分到厂A就停。
	if err != nil || c.FactoryID == nil || c.BindingRevision != 1 {
		// 不符即停：登记设备失败。
		t.Fatalf("register: %+v %v", c, err)
	}
	// 换上记录总线，登记那一笔不算在内。
	bus := &syncBus{}
	// 挂上总线，后面的作废和绑定都记在这里。
	h.WAN.SetBus(bus)
	// 收回到未分配。
	out, err := h.WAN.UnassignClient(ctx, tok, c.ID)
	// 没成、还挂着厂、修订没升或还有生效时间都不对。
	if err != nil || out.FactoryID != nil || out.BindingRevision != 2 || out.BoundAt != nil {
		// 不符即停：收回结果不对。
		t.Fatalf("unassign: %+v %v", out, err)
	}
	// 取出作废通知，只该发一条给厂A。
	voids := bus.byType(global.CmdClientVoid)
	// 条数、厂或修订不对，原厂就不会作废。
	if len(voids) != 1 || voids[0].factory != a.Factory.ID || voids[0].cmd.BindingRevision != 2 || voids[0].cmd.ClientID != c.ID.String() {
		// 不符即停：作废通知不对。
		t.Fatalf("voids %+v", voids)
	}
	// 收回后不该再给谁发绑定。
	if binds := bus.byType(global.CmdClientBind); len(binds) != 0 {
		// 不符即停：收回发了绑定。
		t.Fatalf("binds %+v", binds)
	}
	// 未分配再收回应按未绑定拒绝。
	if _, err := h.WAN.UnassignClient(ctx, tok, c.ID); !errors.Is(err, domain.ErrUnbound) {
		// 不符即停：未分配的设备又被收回了。
		t.Fatalf("unassign twice: %v", err)
	}
	// 再分回厂A，修订接着升到三。
	back, err := h.WAN.AssignClient(ctx, tok, c.ID, a.Factory.ID)
	// 没成或修订没往上走，厂端会当迟到帧丢掉。
	if err != nil || back.FactoryID == nil || *back.FactoryID != a.Factory.ID || back.BindingRevision != 3 {
		// 不符即停：再分配结果不对。
		t.Fatalf("assign back: %+v %v", back, err)
	}
	// 再分配要给厂A发修订三的绑定。
	binds := bus.byType(global.CmdClientBind)
	// 条数、厂或修订不对，厂A就收不回这台。
	if len(binds) != 1 || binds[0].factory != a.Factory.ID || binds[0].cmd.BindingRevision != 3 {
		// 不符即停：再分配的绑定通知不对。
		t.Fatalf("binds %+v", binds)
	}
}

// 验保活带的最近登录：本厂设备记下最新一份，别厂上报和更旧的时间都不改。
func TestPresenceClientLogins(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 登记两家厂，一家上报本厂设备，一家冒报。
	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管")
	// 登记失败就停。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 再登记厂B，用来冒报厂A的设备。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管")
	// 登记失败就停。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 登记一台分到厂A。
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机", id.New(), a.Factory.ID, nil, "ARM-LOGIN")
	// 登记失败就停。
	if err != nil {
		// 不符即停：登记设备失败。
		t.Fatal(err)
	}
	// 发一条保活，带上这台的最近登录。
	up := func(factory uuid.UUID, at time.Time, ver int64, name string) {
		// 组一条厂端保活，带一台设备的最近登录。
		raw, err := json.Marshal(global.Cmd{Typ: global.CmdPresence, ClientLogins: []global.ClientLoginUp{
			{ClientID: c.ID.String(), At: at.Format(time.RFC3339Nano), AppVersion: ver, AppVersionName: name},
			{ClientID: "bad", At: "bad"},
		}})
		// 编不出就停。
		if err != nil {
			// 不符即停：编保活失败。
			t.Fatal(err)
		}
		// 交给上行处理，和 MQTT 收到的一样。
		h.WAN.HandleFactoryUp(ctx, factory, raw)
	}
	// 读回这台的名录行。
	get := func() global.Client {
		// 按身份读名录行。
		row, err := h.WAN.ClientByID(ctx, c.ID)
		// 读不到就停。
		if err != nil {
			// 不符即停：读设备失败。
			t.Fatal(err)
		}
		return row
	}
	// 取一个整秒时刻，比较时不受精度影响。
	t1 := time.Now().UTC().Truncate(time.Second)
	// 厂A上报本厂设备。
	up(a.Factory.ID, t1, 12, "1.2.0")
	// 读回这台，核对记下的登录。
	row := get()
	// 时间或版本没记下就不对。
	if row.LastLoginAt == nil || !row.LastLoginAt.Equal(t1) || row.AppVersion != 12 || row.AppVersionName != "1.2.0" {
		// 不符即停：最近登录没记下。
		t.Fatalf("login: %+v", row)
	}
	// 厂B冒报厂A的设备，时间更新也不能改。
	up(b.Factory.ID, t1.Add(time.Hour), 99, "evil")
	// 更旧的时间也不能回退。
	up(a.Factory.ID, t1.Add(-time.Hour), 1, "old")
	// 读回这台，核对仍是第一次那份。
	row = get()
	// 被冒报或被旧报覆盖都不对。
	if row.LastLoginAt == nil || !row.LastLoginAt.Equal(t1) || row.AppVersion != 12 {
		// 不符即停：别厂或旧报改了登录。
		t.Fatalf("login overwritten: %+v", row)
	}
}
