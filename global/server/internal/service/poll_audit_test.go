// WAN 只读轮询接口成功不记允许，免得审计表无上限增长；匿名读名录仍留拒绝。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 管理员连读两轮名录、设备和目录，审计条数不变；匿名读名录被拒并留一条拒绝。
func TestPollReadsSkipAllowAudit(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端管理员失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端管理员失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 记下轮询前的审计全量，事后核对没有新增。
	before, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，没有基准就比不了。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 像前端轮询那样连读两轮。
	for i := 0; i < 2; i++ {
		// 读名录失败就停，轮询读不该出错。
		if _, err := h.WAN.Directory(ctx, tok); err != nil {
			// 不符即停：带上第几轮。
			t.Fatalf("directory #%d: %v", i+1, err)
		}
		// 读设备列表失败就停，轮询读不该出错。
		if _, err := h.WAN.ListClients(ctx, tok); err != nil {
			// 不符即停：带上第几轮。
			t.Fatalf("clients #%d: %v", i+1, err)
		}
		// 读工艺目录失败就停，轮询读不该出错。
		if _, err := h.WAN.ListFS(ctx, tok, service.KindProcess); err != nil {
			// 不符即停：带上第几轮。
			t.Fatalf("fs #%d: %v", i+1, err)
		}
	}
	// 读完后再取审计全量。
	after, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 条数应不变，多了说明轮询读仍在记允许。
	if len(after) != len(before) {
		// 不符即停：把前后条数打出来。
		t.Fatalf("audit rows %d -> %d after poll reads", len(before), len(after))
	}
	// 没登录读名录应被拒。
	if _, err := h.WAN.Directory(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		// 不符即停：匿名也读到了名录。
		t.Fatalf("anonymous directory: %v", err)
	}
	// 再读审计，核对匿名读名录留了拒绝。
	rows, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 数出读名录拒绝的条数。
	denies := 0
	// 逐行看审计，只数读名录拒绝。
	for _, row := range rows {
		// 读名录且拒绝的才算进来。
		if row.Action == "read_directory" && row.Result == audit.Deny {
			// 再记一条读名录拒绝。
			denies++
		}
	}
	// 只该有匿名那一条拒绝，少了说明把拒绝也一并去掉了。
	if denies != 1 {
		// 不符即停：把实际条数打出来。
		t.Fatalf("read_directory deny rows %d want 1", denies)
	}
}
