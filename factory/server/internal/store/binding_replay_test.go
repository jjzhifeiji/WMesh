// WAN 回连重放同修订绑定：不冲掉本厂改名和换臂，不复活已作废的行，迟到的旧作废不作废新绑定。
package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 设备名只跟 WAN：重放和修订升高都照 WAN 名改；本厂改的是昵称，WAN 怎么重放都留住，空名重放不动设备名。
func TestAcceptBindingReplayKeepsLocalName(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 现发一台身份，验新行带着 WAN 名落库。
	cid := id.New()
	// 首次接受绑定失败就停测，避免后面没有前提。
	if _, err := s.AcceptBinding(ctx, cid, "焊机A", nil, 1); err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept: %v", err)
	}
	// 本厂改昵称，设备名不动。
	ren, err := s.RenameClient(ctx, cid, "一线焊机")
	// 改昵称失败或设备名被改都算错。
	if err != nil || ren.Nickname != "一线焊机" || ren.Name != "焊机A" {
		// 改昵称失败就停测。
		t.Fatalf("rename %+v %v", ren, err)
	}
	// WAN 回连以同修订、同名重放，昵称必须留住。
	got, err := s.AcceptBinding(ctx, cid, "焊机A", nil, 1)
	// 重放报错、设备名不是 WAN 名或昵称被冲都算错。
	if err != nil || got.Name != "焊机A" || got.Nickname != "一线焊机" || got.Status != store.ClientStatusBound {
		// 打出重放结果，便于看是哪一项不对。
		t.Fatalf("replay same name %+v %v", got, err)
	}
	// WAN 改了名仍以同修订送来，设备名要跟着更新，昵称不动。
	got, err = s.AcceptBinding(ctx, cid, "焊机A-新", nil, 1)
	// 报错、没改成 WAN 新名或昵称被冲都算错。
	if err != nil || got.Name != "焊机A-新" || got.WANName == nil || *got.WANName != "焊机A-新" || got.Nickname != "一线焊机" {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("wan rename %+v %v", got, err)
	}
	// 修订升高照 WAN 名，昵称仍留住。
	got, err = s.AcceptBinding(ctx, cid, "焊机A-新", nil, 2)
	// 报错、修订没升或昵称被冲都算错。
	if err != nil || got.Name != "焊机A-新" || got.BindingRevision != 2 || got.Nickname != "一线焊机" {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("higher revision %+v %v", got, err)
	}
	// 空名重放不当 WAN 改名，设备名和 WAN 名都不动。
	got, err = s.AcceptBinding(ctx, cid, "", nil, 2)
	// 报错、名字被改或 WAN 名被清都算错。
	if err != nil || got.Name != "焊机A-新" || got.WANName == nil || *got.WANName != "焊机A-新" {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("empty name replay %+v %v", got, err)
	}
	// 昵称改成空串就清掉。
	if ren, err := s.RenameClient(ctx, cid, ""); err != nil || ren.Nickname != "" || ren.Name != "焊机A-新" {
		// 清昵称不对就停测。
		t.Fatalf("clear nickname %+v %v", ren, err)
	}
	// 不存在的设备改昵称必须报找不到。
	if _, err := s.RenameClient(ctx, id.New(), "无"); !errors.Is(err, domain.ErrNotFound) {
		// 没报找不到就停测。
		t.Fatalf("rename missing: %v", err)
	}
}

// 已作废的行收到同修订不复活，收到更高修订才恢复绑定。
func TestAcceptBindingSameRevisionKeepsVoid(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 生成这台的公钥，绑定要用。
	pub, _ := mustEd25519(t)
	// 现发一台身份，不沿用空值。
	cid := id.New()
	// 接受初始绑定失败就停测，避免后面没有前提。
	if _, err := s.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept: %v", err)
	}
	// 本厂超管作废这台，失败就停测。
	if err := s.VoidBinding(ctx, cid); err != nil {
		// 作废失败就停测。
		t.Fatalf("void: %v", err)
	}
	// WAN 以同修订重推，应原样交回、不报错。
	got, err := s.AcceptBinding(ctx, cid, "焊机", pub, 1)
	// 报错、被复活或作废时间被清都算错。
	if err != nil || got.Status != store.ClientStatusVoid || got.VoidedAt == nil || got.BindingRevision != 1 {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("same revision revived %+v %v", got, err)
	}
	// 读回库里这台，确认确实没被改成有效。
	row, err := s.ClientByID(ctx, cid)
	// 读不到或已复活都算错。
	if err != nil || row.Status != store.ClientStatusVoid {
		// 打出读回结果。
		t.Fatalf("stored void %+v %v", row, err)
	}
	// WAN 改分带来更高修订，才重新绑定。
	got, err = s.AcceptBinding(ctx, cid, "焊机", pub, 2)
	// 报错、没恢复或修订没升都算错。
	if err != nil || got.Status != store.ClientStatusBound || got.VoidedAt != nil || got.BindingRevision != 2 {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("higher revision rebind %+v %v", got, err)
	}
}

// 作废指令带修订：本地已在更高或同修订有效时当迟到帧，修订更高才作废。
func TestVoidBindingByRevision(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 现发一台身份，不沿用空值。
	cid := id.New()
	// 先以修订三落下有效绑定，模拟已落索引里的新绑定。
	if _, err := s.AcceptBinding(ctx, cid, "焊机", nil, 3); err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept: %v", err)
	}
	// 逐个送更旧和同修订的作废，都应当被当迟到帧。
	for _, rev := range []int64{2, 3} {
		// 迟到帧必须交回修订过旧，不能把新绑定作废。
		if err := s.VoidBindingByRevision(ctx, cid, rev); !errors.Is(err, domain.ErrStaleRevision) {
			// 迟到的作废被执行或报了别的错就停测。
			t.Fatalf("void rev%d: %v", rev, err)
		}
		// 读回这台，确认仍是有效绑定。
		row, err := s.ClientByID(ctx, cid)
		// 读不到或被作废都算错。
		if err != nil || row.Status != store.ClientStatusBound || row.VoidedAt != nil {
			// 打出读回结果。
			t.Fatalf("after void rev%d %+v %v", rev, row, err)
		}
	}
	// 修订四的作废高于本地，应当作废成功。
	if err := s.VoidBindingByRevision(ctx, cid, 4); err != nil {
		// 作废失败就停测。
		t.Fatalf("void rev4: %v", err)
	}
	// 读回这台，确认已作废。
	row, err := s.ClientByID(ctx, cid)
	// 读不到或没作废都算错。
	if err != nil || row.Status != store.ClientStatusVoid || row.VoidedAt == nil {
		// 打出读回结果。
		t.Fatalf("after void rev4 %+v %v", row, err)
	}
	// 已作废再收一次作废不算错。
	if err := s.VoidBindingByRevision(ctx, cid, 4); err != nil {
		// 重复作废报错就停测。
		t.Fatalf("void again: %v", err)
	}
	// 没有这台的作废交回不存在，由调用方当已完成。
	if err := s.VoidBindingByRevision(ctx, id.New(), 9); !errors.Is(err, domain.ErrNotFound) {
		// 不存在却没交回不存在就停测。
		t.Fatalf("void missing: %v", err)
	}
}

// 补号：本机没号就补上，本机已有别的号保留本机的，号被别台占用交回业务冲突，作废行不补。
func TestFillDeviceSerial(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// A 没号要补，B 已有号要保留，C 撞号，V 已作废。
	cidA, cidB, cidC, cidV := id.New(), id.New(), id.New(), id.New()
	// 逐台接下初始绑定，任何一台失败就停测。
	for _, cid := range []struct {
		id   uuid.UUID // 设备身份
		name string    // 给人看的名字
	}{{cidA, "焊机A"}, {cidB, "焊机B"}, {cidC, "焊机C"}, {cidV, "焊机V"}} {
		// 接受绑定失败就停测，避免后面没有前提。
		if _, err := s.AcceptBinding(ctx, cid.id, cid.name, nil, 1); err != nil {
			// 接受绑定失败就停测。
			t.Fatalf("accept %s: %v", cid.name, err)
		}
	}
	// B 先在本厂钉上 ARM-2，模拟现场换臂重新注册。
	if _, err := s.PinDeviceSerial(ctx, cidB, "ARM-2", false); err != nil {
		// 钉号失败就停测。
		t.Fatalf("pin b: %v", err)
	}
	// V 先作废，验作废行不被补号。
	if err := s.VoidBinding(ctx, cidV); err != nil {
		// 作废失败就停测。
		t.Fatalf("void v: %v", err)
	}

	// A 本机没号，补上 WAN 的号。
	a, err := s.FillDeviceSerial(ctx, cidA, " ARM-1 ")
	// 报错或没补上都算错。
	if err != nil || a.DeviceSerial != "ARM-1" {
		// 打出结果，便于看是哪一项不对。
		t.Fatalf("fill a %+v %v", a, err)
	}
	// 同号再补一次，保持原样不报错。
	a, err = s.FillDeviceSerial(ctx, cidA, "ARM-1")
	// 报错或号变了都算错。
	if err != nil || a.DeviceSerial != "ARM-1" {
		// 打出结果。
		t.Fatalf("fill a again %+v %v", a, err)
	}
	// B 本机已有 ARM-2，WAN 送旧号 ARM-9 也保留本机的。
	b, err := s.FillDeviceSerial(ctx, cidB, "ARM-9")
	// 报错或本机号被冲掉都算错。
	if err != nil || b.DeviceSerial != "ARM-2" {
		// 打出结果。
		t.Fatalf("fill b %+v %v", b, err)
	}
	// C 拿 A 已占的号来补，必须交回臂号被占。
	if _, err := s.FillDeviceSerial(ctx, cidC, "ARM-1"); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		// 撞号没被收成业务冲突就停测。
		t.Fatalf("fill c taken: %v", err)
	}
	// 读回 C，撞号的号不能落库。
	c, err := s.ClientByID(ctx, cidC)
	// 读不到或号被钉上都算错。
	if err != nil || c.DeviceSerial != "" {
		// 打出读回结果。
		t.Fatalf("c after taken %+v %v", c, err)
	}
	// V 已作废，不补号，原样交回。
	v, err := s.FillDeviceSerial(ctx, cidV, "ARM-7")
	// 报错或作废行被补号都算错。
	if err != nil || v.DeviceSerial != "" || v.Status != store.ClientStatusVoid {
		// 打出结果。
		t.Fatalf("fill void %+v %v", v, err)
	}
	// 纯空白的号没有可补的，交回必须有号。
	if _, err := s.FillDeviceSerial(ctx, cidC, "  "); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		// 空号没被拒就停测。
		t.Fatalf("fill blank: %v", err)
	}
}
