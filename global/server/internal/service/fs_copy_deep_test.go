// 平台目录粘贴成环检查：目标在源的深层子孙里也要按成环拒绝，不限层数。
package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 建超过六十四层的文件夹链，把顶层粘进最底层必须按成环拒绝，底层下面不多出东西。
func TestPlatformCopyFSIntoDeepDescendant(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 取平台工艺树的根，链从这里往下挂。
	root, err := h.WAN.Store().EnsureFSRoot(ctx, global.KindProcess)
	// 取根失败就停测，后面没有挂点。
	if err != nil {
		// 不符即停：取根失败。
		t.Fatal(err)
	}
	// 记下链上每一层，顶层和最底层后面要用。
	chain := make([]store.FSNode, 0, 70)
	// 当前挂点从根开始，每建一层就往下走。
	cur := root.ID
	// 建七十层，超过旧检查六十四层的上限。
	for i := 0; i < 70; i++ {
		// 在当前挂点下建一层文件夹。
		f, err := h.WAN.Store().InsertFSFolder(ctx, cur, fmt.Sprintf("层%02d", i))
		// 建文件夹失败就停测，链不完整没法验。
		if err != nil {
			// 不符即停：建链失败。
			t.Fatalf("folder %d: %v", i, err)
		}
		// 收下这一层，后面取顶层和底层。
		chain = append(chain, f)
		// 下一层挂到刚建的这层下面。
		cur = f.ID
	}
	// 最底层，粘贴的目标。
	bottom := chain[len(chain)-1]
	// 把顶层粘进最底层，必须按成环拒绝。
	if _, err := h.WAN.CopyFSNode(ctx, tok, chain[0].ID, bottom.ID); !errors.Is(err, domain.ErrCycle) {
		// 不符即停：深链没挡住，复制会无限往下套。
		t.Fatalf("deep paste: want ErrCycle, got %v", err)
	}
	// 列出最底层的下级，被拒的粘贴不该建出任何东西。
	kids, err := h.WAN.Store().ListFSChildren(ctx, bottom.ID)
	// 列不出或多出了下级就停。
	if err != nil || len(kids) != 0 {
		// 不符即停：被拒后底层下面还多了东西。
		t.Fatalf("bottom kids %d %v", len(kids), err)
	}
	// 把中间一层粘到根下照常可以，确认不是一律拒绝。
	if _, err := h.WAN.CopyFSNode(ctx, tok, chain[60].ID, root.ID); err != nil {
		// 不符即停：不成环的粘贴被误拦。
		t.Fatalf("paste to root: %v", err)
	}
}
