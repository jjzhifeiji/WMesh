// 钉机械臂号：不覆盖时拒绝换号，覆盖分支能换号，查重谁都绕不过。
package store_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 不覆盖时同号幂等、换号拒绝；覆盖分支能换号，仍挡撞号、空号和作废。
func TestPinDeviceSerialOverwrite(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)

	// 生成 A 台的公钥，绑定要用。
	pubA, _ := mustEd25519(t)
	// 生成 B 台的公钥，绑定要用。
	pubB, _ := mustEd25519(t)
	// 现发 A 台身份，不沿用空值。
	cidA := id.New()
	// 现发 B 台身份，不沿用空值。
	cidB := id.New()
	// 接受 A 台绑定失败就停测，避免后面没有前提。
	if _, err := s.AcceptBinding(ctx, cidA, "焊机A", pubA, 1); err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept a: %v", err)
	}
	// 接受 B 台绑定失败就停测，避免后面没有前提。
	if _, err := s.AcceptBinding(ctx, cidB, "焊机B", pubB, 1); err != nil {
		// 接受绑定失败就停测。
		t.Fatalf("accept b: %v", err)
	}

	// 首次钉号不覆盖也应成功。
	if row, err := s.PinDeviceSerial(ctx, cidA, "ARM-1", false); err != nil || row.DeviceSerial != "ARM-1" {
		// 首次钉号失败就停测。
		t.Fatalf("first pin %+v %v", row, err)
	}
	// 同一个号再钉一次应当幂等成功。
	if row, err := s.PinDeviceSerial(ctx, cidA, "ARM-1", false); err != nil || row.DeviceSerial != "ARM-1" {
		// 同号重钉失败就停测。
		t.Fatalf("same pin %+v %v", row, err)
	}
	// 不覆盖时换号必须被拒，不许悄悄换掉已钉的号。
	if _, err := s.PinDeviceSerial(ctx, cidA, "ARM-2", false); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		// 换号被放行就停测。
		t.Fatalf("overwrite: %v", err)
	}
	// 读回 A 台，被拒的换号不该落库。
	if row, err := s.ClientByID(ctx, cidA); err != nil || row.DeviceSerial != "ARM-1" {
		// 号被改掉就停测。
		t.Fatalf("after deny %+v %v", row, err)
	}
	// 走覆盖分支换号应当成功，模拟现场换臂。
	if row, err := s.PinDeviceSerial(ctx, cidA, "ARM-2", true); err != nil || row.DeviceSerial != "ARM-2" {
		// 覆盖分支换号失败就停测，后面的查重没有前提。
		t.Fatalf("replace %+v %v", row, err)
	}
	// 覆盖也绕不过查重，B 台不能抢 A 台的号。
	if _, err := s.PinDeviceSerial(ctx, cidB, "ARM-2", true); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		// 撞号被放行就停测。
		t.Fatalf("taken: %v", err)
	}
	// 空白号必须被拒。
	if _, err := s.PinDeviceSerial(ctx, cidB, "  ", false); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		// 空号被放行就停测。
		t.Fatalf("blank: %v", err)
	}
	// 作废 A 台失败就停测，避免后面没有前提。
	if err := s.VoidBinding(ctx, cidA); err != nil {
		// 作废失败就停测。
		t.Fatal(err)
	}
	// 作废后即使覆盖也不能再钉号。
	if _, err := s.PinDeviceSerial(ctx, cidA, "ARM-3", true); !errors.Is(err, domain.ErrBindingVoid) {
		// 作废后还能钉号就停测。
		t.Fatalf("void: %v", err)
	}
}
