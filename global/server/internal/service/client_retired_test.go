// 目标厂已注销时登记带厂、建档分厂、分配、改分都拒绝，且不留孤行；停用的厂照常可分。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// retiredClientLogin 立云端超管并登录，交回会话票。
func retiredClientLogin(t *testing.T, h *Harness) string {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 立云端超管，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	return tok
}

// retiredClientCount 直接数设备表行数，核对被拒的登记没落行。
func retiredClientCount(t *testing.T, h *Harness) int64 {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 准备接住行数，零值先不当成没有。
	var n int64
	// 直接数库里的设备行，不经服务免得被过滤。
	if err := h.DB.Table("clients").Count(&n).Error; err != nil {
		// 不符即停：数设备行失败。
		t.Fatal(err)
	}
	return n
}

// 验目标厂已注销时登记、建档分厂、分配、改分都被拒，不留新设备行，从注销厂改走仍可以。
func TestClientRetiredFactoryRejected(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 登记厂R，稍后注销。
	r, err := h.WAN.CreateFactory(ctx, tok, "厂R", "sa-r", "超管R")
	// 登记厂R失败就停，没有可注销的厂。
	if err != nil {
		// 不符即停：登记厂R失败。
		t.Fatal(err)
	}
	// 登记厂B，一直有效，作对照。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记厂B失败就停，没有对照厂。
	if err != nil {
		// 不符即停：登记厂B失败。
		t.Fatal(err)
	}
	// 先在厂R登记一台，名录有引用，删厂就改成注销。
	old, err := h.WAN.RegisterClient(ctx, tok, "旧焊机", id.New(), r.Factory.ID, nil, "ARM-OLD")
	// 登记失败就停，删厂会直接拿掉而不是注销。
	if err != nil {
		// 不符即停：在厂R登记设备失败。
		t.Fatal(err)
	}
	// 删厂R，已有设备引用只能注销。
	retired, err := h.WAN.DeleteFactory(ctx, tok, r.Factory.ID)
	// 没注销成或状态不对就停，后面没有已注销的厂。
	if err != nil || retired == nil || retired.Status != global.FactoryRetired {
		// 不符即停：厂R没变成已注销。
		t.Fatalf("retire: %+v %v", retired, err)
	}
	// 在厂B登记一台，后面拿来改分。
	onB, err := h.WAN.RegisterClient(ctx, tok, "B焊机", id.New(), b.Factory.ID, nil, "ARM-B")
	// 登记失败就停，没有可改分的设备。
	if err != nil {
		// 不符即停：在厂B登记设备失败。
		t.Fatal(err)
	}
	// 登记一台不带厂的，后面拿来分配。
	free, err := h.WAN.RegisterClient(ctx, tok, "空焊机", id.New(), uuid.Nil, nil, "ARM-FREE")
	// 登记失败就停，没有可分配的设备。
	if err != nil {
		// 不符即停：登记未分配设备失败。
		t.Fatal(err)
	}
	// 记下当前设备行数，后面被拒的登记不能让它变多。
	before := retiredClientCount(t, h)

	// 新设备的识别号，登记带已注销厂。
	cid := id.New()
	// 登记时带已注销厂，必须按已注销拒绝。
	if _, err := h.WAN.RegisterClient(ctx, tok, "新焊机", cid, r.Factory.ID, nil, "ARM-NEW"); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：登记带已注销厂没被拒。
		t.Fatalf("register to retired: %v", err)
	}
	// 被拒的登记不能留下设备行。
	if _, err := h.WAN.ClientByID(ctx, cid); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：被拒的登记留下了孤行。
		t.Fatalf("register to retired left row: %v", err)
	}
	// 再造一个识别号，登记带不存在的厂。
	ghost := id.New()
	// 登记时带不存在的厂，必须按不存在拒绝。
	if _, err := h.WAN.RegisterClient(ctx, tok, "幽灵", ghost, id.New(), nil, "ARM-GHOST"); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：登记带不存在厂没被拒。
		t.Fatalf("register to missing: %v", err)
	}
	// 不存在的厂被拒也不能留下设备行。
	if _, err := h.WAN.ClientByID(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：不存在的厂登记留下了孤行。
		t.Fatalf("register to missing left row: %v", err)
	}
	// 再造一个识别号，走建档分厂。
	bid := id.New()
	// 建档并分到已注销厂，必须按已注销拒绝。
	if _, err := h.WAN.BindClient(ctx, tok, bid, r.Factory.ID, "建档焊机", nil); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：建档分到已注销厂没被拒。
		t.Fatalf("bind to retired: %v", err)
	}
	// 被拒的建档分厂不能留下设备行。
	if _, err := h.WAN.ClientByID(ctx, bid); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：被拒的建档留下了孤行。
		t.Fatalf("bind to retired left row: %v", err)
	}
	// 未分配设备分到已注销厂，必须按已注销拒绝。
	if _, err := h.WAN.AssignClient(ctx, tok, free.ID, r.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：分配到已注销厂没被拒。
		t.Fatalf("assign to retired: %v", err)
	}
	// 读回这台未分配设备，核对归属没动。
	cur, err := h.WAN.ClientByID(ctx, free.ID)
	// 读失败或已被分走都不对。
	if err != nil || cur.FactoryID != nil || cur.BindingRevision != 0 {
		// 不符即停：被拒的分配改动了归属。
		t.Fatalf("assign to retired changed row: %+v %v", cur, err)
	}
	// 厂B的设备改到已注销厂，必须按已注销拒绝。
	if _, err := h.WAN.RebindClient(ctx, tok, onB.ID, r.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：改分到已注销厂没被拒。
		t.Fatalf("rebind to retired: %v", err)
	}
	// 读回厂B那台设备，核对归属和修订没动。
	cur, err = h.WAN.ClientByID(ctx, onB.ID)
	// 读失败、归属变了或修订升了都不对。
	if err != nil || cur.FactoryID == nil || *cur.FactoryID != b.Factory.ID || cur.BindingRevision != 1 {
		// 不符即停：被拒的改分改动了归属。
		t.Fatalf("rebind to retired changed row: %+v %v", cur, err)
	}
	// 被拒的几笔之后设备行数必须不变。
	if after := retiredClientCount(t, h); after != before {
		// 不符即停：被拒的操作多留了设备行。
		t.Fatalf("client rows %d -> %d", before, after)
	}

	// 同一识别号和机械臂号改登记到厂B，证明没被孤行占住。
	again, err := h.WAN.RegisterClient(ctx, tok, "新焊机", cid, b.Factory.ID, nil, "ARM-NEW")
	// 登记失败或归属不对都说明号被占住了。
	if err != nil || again.FactoryID == nil || *again.FactoryID != b.Factory.ID {
		// 不符即停：同号改登记到有效厂失败。
		t.Fatalf("register again: %+v %v", again, err)
	}
	// 已注销厂里的设备改到有效厂仍可以。
	moved, err := h.WAN.RebindClient(ctx, tok, old.ID, b.Factory.ID)
	// 改走失败、归属不对或修订没升都不对。
	if err != nil || moved.FactoryID == nil || *moved.FactoryID != b.Factory.ID || moved.BindingRevision != 2 {
		// 不符即停：从已注销厂改走失败。
		t.Fatalf("rebind off retired: %+v %v", moved, err)
	}
}

// 验停用的厂照常能登记带厂、建档分厂、分配和改分，只拒绝已注销。
func TestClientDisabledFactoryAllowed(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 登记厂D，稍后停用。
	d, err := h.WAN.CreateFactory(ctx, tok, "厂D", "sa-d", "超管D")
	// 登记厂D失败就停，没有可停用的厂。
	if err != nil {
		// 不符即停：登记厂D失败。
		t.Fatal(err)
	}
	// 登记厂B，一直有效，作改分起点。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记厂B失败就停，没有改分起点。
	if err != nil {
		// 不符即停：登记厂B失败。
		t.Fatal(err)
	}
	// 停用厂D，按口径停用的照常可分。
	disabled, err := h.WAN.DisableFactory(ctx, tok, d.Factory.ID)
	// 没停用成或状态不对就停。
	if err != nil || disabled.Status != global.FactoryDisabled {
		// 不符即停：厂D没变成停用。
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	// 登记时带停用厂，应照常登记并分过去。
	reg, err := h.WAN.RegisterClient(ctx, tok, "D焊机", id.New(), d.Factory.ID, nil, "ARM-D")
	// 登记失败或归属不对都不对。
	if err != nil || reg.FactoryID == nil || *reg.FactoryID != d.Factory.ID {
		// 不符即停：登记带停用厂被拒。
		t.Fatalf("register to disabled: %+v %v", reg, err)
	}
	// 建档并分到停用厂，应照常成功。
	bound, err := h.WAN.BindClient(ctx, tok, id.New(), d.Factory.ID, "建档焊机", nil)
	// 建档分厂失败或归属不对都不对。
	if err != nil || bound.FactoryID == nil || *bound.FactoryID != d.Factory.ID {
		// 不符即停：建档分到停用厂被拒。
		t.Fatalf("bind to disabled: %+v %v", bound, err)
	}
	// 登记一台不带厂的，拿来分配。
	free, err := h.WAN.RegisterClient(ctx, tok, "空焊机", id.New(), uuid.Nil, nil, "ARM-FREE")
	// 登记失败就停，没有可分配的设备。
	if err != nil {
		// 不符即停：登记未分配设备失败。
		t.Fatal(err)
	}
	// 未分配设备分到停用厂，应照常成功。
	assigned, err := h.WAN.AssignClient(ctx, tok, free.ID, d.Factory.ID)
	// 分配失败或归属不对都不对。
	if err != nil || assigned.FactoryID == nil || *assigned.FactoryID != d.Factory.ID {
		// 不符即停：分配到停用厂被拒。
		t.Fatalf("assign to disabled: %+v %v", assigned, err)
	}
	// 在厂B登记一台，拿来改分。
	onB, err := h.WAN.RegisterClient(ctx, tok, "B焊机", id.New(), b.Factory.ID, nil, "ARM-B")
	// 登记失败就停，没有可改分的设备。
	if err != nil {
		// 不符即停：在厂B登记设备失败。
		t.Fatal(err)
	}
	// 厂B的设备改到停用厂，应照常成功且修订升高。
	moved, err := h.WAN.RebindClient(ctx, tok, onB.ID, d.Factory.ID)
	// 改分失败、归属不对或修订没升都不对。
	if err != nil || moved.FactoryID == nil || *moved.FactoryID != d.Factory.ID || moved.BindingRevision != 2 {
		// 不符即停：改分到停用厂被拒。
		t.Fatalf("rebind to disabled: %+v %v", moved, err)
	}
}
