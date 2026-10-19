// 目录搬家成环检查：深层父链也要挡住，并发反向搬家只成一笔且树仍连到根。
package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 建一条超过六十四层的文件夹链，把顶层搬到最底层必须按成环拒绝。
func TestMoveFSNodeDeepChainCycle(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 取厂级工艺树的根，链从这里往下挂。
	root, err := s.EnsureFSRoot(ctx, store.KindProcess, store.FSTreeFactory, nil)
	// 取根失败就停测，后面没有挂点。
	if err != nil {
		// 取根失败就停测。
		t.Fatalf("root: %v", err)
	}
	// 记下链上每一层，顶层和最底层后面要用。
	chain := make([]store.FSNode, 0, 70)
	// 当前挂点从根开始，每建一层就往下走。
	cur := root.ID
	// 建七十层，超过旧检查六十四层的上限。
	for i := 0; i < 70; i++ {
		// 在当前挂点下建一层文件夹。
		f, err := s.InsertFSFolder(ctx, cur, fmt.Sprintf("层%02d", i))
		// 建文件夹失败就停测，链不完整没法验。
		if err != nil {
			// 建链失败就停测。
			t.Fatalf("folder %d: %v", i, err)
		}
		// 收下这一层，后面取顶层和底层。
		chain = append(chain, f)
		// 下一层挂到刚建的这层下面。
		cur = f.ID
	}
	// 把顶层搬到最底层下面，必须按成环拒绝。
	if _, err := s.MoveFSNode(ctx, chain[0].ID, chain[len(chain)-1].ID); !errors.Is(err, domain.ErrCycle) {
		// 深链没挡住就停测。
		t.Fatalf("deep move: want ErrCycle, got %v", err)
	}
	// 重读顶层，确认被拒后挂点没动。
	top, err := s.FSNodeByID(ctx, chain[0].ID)
	// 重读失败或不再挂在根上就停测。
	if err != nil || top.ParentID == nil || *top.ParentID != root.ID {
		// 被拒的搬家不能改动挂点。
		t.Fatalf("top after reject: %+v err=%v", top, err)
	}
	// 整棵树必须仍然全部连到根。
	assertFSTreeRooted(t, s, store.KindProcess, store.FSTreeFactory, root.ID)
}

// 两人同时把 X 拖进 Y、把 Y 拖进 X，只能成一笔，整棵树仍全部连到根。
func TestMoveFSNodeConcurrentReverse(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 取厂级工艺树的根，两个文件夹都挂在这里。
	root, err := s.EnsureFSRoot(ctx, store.KindProcess, store.FSTreeFactory, nil)
	// 取根失败就停测，后面没有挂点。
	if err != nil {
		// 取根失败就停测。
		t.Fatalf("root: %v", err)
	}
	// 多跑几轮，放大两笔交错进成环检查的机会。
	for round := 0; round < 20; round++ {
		// 在根下建 X，作为第一笔要搬的节点。
		x, err := s.InsertFSFolder(ctx, root.ID, fmt.Sprintf("X%02d", round))
		// 建 X 失败就停测。
		if err != nil {
			// 建 X 失败就停测。
			t.Fatalf("round %d x: %v", round, err)
		}
		// 在根下建 Y，作为第二笔要搬的节点。
		y, err := s.InsertFSFolder(ctx, root.ID, fmt.Sprintf("Y%02d", round))
		// 建 Y 失败就停测。
		if err != nil {
			// 建 Y 失败就停测。
			t.Fatalf("round %d y: %v", round, err)
		}
		// 两笔搬家方向相反：X 进 Y，Y 进 X。
		moves := [2][2]uuid.UUID{{x.ID, y.ID}, {y.ID, x.ID}}
		// 每笔的结果按下标放，事后统一核对。
		errs := make([]error, len(moves))
		// 关闭这个通道时两笔一起起跑。
		start := make(chan struct{})
		// 等两笔都做完再核对。
		var wg sync.WaitGroup
		// 两笔各起一个协程并发搬家。
		for i, m := range moves {
			// 记下又起了一笔搬家。
			wg.Add(1)
			// 各自等发令后搬家，模拟两人同时拖。
			go func(i int, node, parent uuid.UUID) {
				// 做完就报到，主协程才能核对。
				defer wg.Done()
				// 等发令，两笔尽量同时进事务。
				<-start
				// 搬家结果放进自己那一格。
				_, errs[i] = s.MoveFSNode(ctx, node, parent)
			}(i, m[0], m[1])
		}
		// 发令，两笔同时开始。
		close(start)
		// 等两笔都结束再看结果。
		wg.Wait()
		// 数成功了几笔，必须恰好一笔。
		ok := 0
		// 逐笔核对，失败的只能是成环拒绝。
		for i, e := range errs {
			// 这一笔成功就记一次。
			if e == nil {
				// 成功笔数加一。
				ok++
				continue
			}
			// 失败却不是成环拒绝就停测。
			if !errors.Is(e, domain.ErrCycle) {
				// 只该按成环拒绝，别的错误说明出了别的问题。
				t.Fatalf("round %d move %d: want ErrCycle, got %v", round, i, e)
			}
		}
		// 两笔都成或都败都不对。
		if ok != 1 {
			// 恰好一笔成功才说明搬家已串行。
			t.Fatalf("round %d: %d moves succeeded, want 1", round, ok)
		}
	}
	// 跑完所有轮，整棵树必须仍然全部连到根。
	assertFSTreeRooted(t, s, store.KindProcess, store.FSTreeFactory, root.ID)
}

// assertFSTreeRooted 逐个节点沿父链往上走，必须都在有限步内走到这棵树的根。
func assertFSTreeRooted(t *testing.T, s *store.Store, assetKind, treeLevel string, rootID uuid.UUID) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出这一种类的全部节点。
	nodes, err := s.ListFSNodes(context.Background(), assetKind)
	// 列不出来就停测，没法核对。
	if err != nil {
		// 列节点失败就停测。
		t.Fatalf("list fs: %v", err)
	}
	// 按身份索引，往上找父节点不用再查库。
	byID := make(map[uuid.UUID]store.FSNode, len(nodes))
	// 逐个放进索引。
	for _, n := range nodes {
		// 按身份收下这个节点。
		byID[n.ID] = n
	}
	// 逐个节点核对能不能走到根。
	for _, n := range nodes {
		// 别的树不归这次核对。
		if n.TreeLevel != treeLevel {
			continue
		}
		// 从这个节点出发往上走。
		cur := n
		// 走的步数超过节点总数就说明成环。
		for steps := 0; cur.ParentID != nil; steps++ {
			// 步数超过节点总数，父链一定绕回来了。
			if steps > len(nodes) {
				// 成环的节点连不到根。
				t.Fatalf("node %s (%s) is in a cycle", n.ID, n.Name)
			}
			// 取出父节点继续往上。
			p, ok := byID[*cur.ParentID]
			// 父节点不在表里就是断链。
			if !ok {
				// 父链断了，节点连不到根。
				t.Fatalf("node %s (%s) has missing parent %s", n.ID, n.Name, *cur.ParentID)
			}
			// 换到父节点，继续往上走。
			cur = p
		}
		// 走到的根必须是这棵树的根。
		if cur.ID != rootID {
			// 落到别的根上说明挂错了树。
			t.Fatalf("node %s (%s) ends at %s, want root %s", n.ID, n.Name, cur.ID, rootID)
		}
	}
}
