// 目录粘贴成环检查：目标在源的深层子孙里也要按成环拒绝，不限层数。
package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 建超过六十四层的文件夹链，把顶层粘进最底层必须按成环拒绝，底层下面不多出东西。
func TestCopyFSIntoDeepDescendant(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 取厂级工艺树的根，链从这里往下挂。
	root, err := fac.Store().EnsureFSRoot(ctx, store.KindProcess, store.FSTreeFactory, nil)
	// 取根失败就停测，后面没有挂点。
	if err != nil {
		// 取根失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下链上每一层，顶层和最底层后面要用。
	chain := make([]store.FSNode, 0, 70)
	// 当前挂点从根开始，每建一层就往下走。
	cur := root.ID
	// 建七十层，超过旧检查六十四层的上限。
	for i := 0; i < 70; i++ {
		// 在当前挂点下建一层文件夹。
		f, err := fac.Store().InsertFSFolder(ctx, cur, fmt.Sprintf("层%02d", i))
		// 建文件夹失败就停测，链不完整没法验。
		if err != nil {
			// 建链失败即停，不能把这一步的失败当成通过。
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
	if _, err := fac.CopyFSNode(ctx, pe.tok, chain[0].ID, bottom.ID); !errors.Is(err, domain.ErrCycle) {
		// 深链没挡住就停，复制会无限往下套。
		t.Fatalf("deep paste: want ErrCycle, got %v", err)
	}
	// 列出最底层的下级，被拒的粘贴不该建出任何东西。
	kids, err := fac.Store().ListFSChildren(ctx, bottom.ID)
	// 列不出或多出了下级就停。
	if err != nil || len(kids) != 0 {
		// 被拒后底层下面还多了东西。
		t.Fatalf("bottom kids %d %v", len(kids), err)
	}
	// 把中间一层粘到根下照常可以，确认不是一律拒绝。
	if _, err := fac.CopyFSNode(ctx, pe.tok, chain[60].ID, root.ID); err != nil {
		// 不成环的粘贴被误拦就停。
		t.Fatalf("paste to root: %v", err)
	}
}
