// 示教器上只看得到、改得了自己的个人树；超管看别人个人件只在网页（口径 K2，D-53）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 超管在示教器上不列别人的个人树，也不能在别人的个人树里建、改、删文件夹；网页上照旧能。
func TestPadFSHidesOthersPersonalTree(t *testing.T) {
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
	// 超管网页会话。
	saWeb := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	op := mustCreateRole(t, ctx, fac, saWeb, "op", "op-pass-1", factory.RoleOperator)
	// 操作员列一次目录，建出自己的个人根。
	opNodes, err := fac.ListFS(ctx, op.tok, factory.KindProcess)
	// 列不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 找出操作员的个人根。
	opRoot := personalRootOf(opNodes, op.acc.ID)
	// 找不到就停住。
	if opRoot == uuid.Nil {
		// 前提不成立就停住。
		t.Fatal("op personal root missing")
	}
	// 操作员在自己的个人树里建一个文件夹。
	mine, err := fac.CreateFSFolder(ctx, op.tok, opRoot, "我的夹")
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 超管在示教器上登录。
	pad, err := fac.LoginPad(ctx, "sa", personPass("sa"))
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("sa pad login: %v", err)
	}
	// 超管示教器列目录。
	padNodes, err := fac.ListFS(ctx, pad.Token, factory.KindProcess)
	// 列不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 示教器上不该出现操作员的任何个人树节点。
	for _, n := range padNodes {
		// 别人的个人树出现就停住。
		if n.TreeLevel == store.FSTreePersonal && n.OwnerID != nil && *n.OwnerID == op.acc.ID {
			// 把节点打出来并停住。
			t.Fatalf("pad sees others personal node %+v", n.FSNode)
		}
	}
	// 自己的个人根照常在。
	if personalRootOf(padNodes, pad.Account.ID) == uuid.Nil {
		// 自己的树不见了就停住。
		t.Fatal("pad lost own personal root")
	}
	// 示教器上在别人的个人树里建文件夹应拒。
	if _, err := fac.CreateFSFolder(ctx, pad.Token, opRoot, "超管夹"); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("pad mkdir in others tree: %v", err)
	}
	// 示教器上改别人的文件夹名应拒。
	if _, err := fac.RenameFSNode(ctx, pad.Token, mine.ID, "改名"); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("pad rename others folder: %v", err)
	}
	// 示教器上删别人的文件夹应拒。
	if err := fac.DeleteFSNode(ctx, pad.Token, mine.ID); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("pad delete others folder: %v", err)
	}
	// 网页上超管照旧看得到别人的个人树。
	webNodes, err := fac.ListFS(ctx, saWeb, factory.KindProcess)
	// 列不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 网页上看不到就停住。
	if personalRootOf(webNodes, op.acc.ID) == uuid.Nil {
		// 网页口径被误改就停住。
		t.Fatal("web sa cannot see others personal root")
	}
	// 网页上超管照旧能改别人的文件夹名。
	if _, err := fac.RenameFSNode(ctx, saWeb, mine.ID, "超管改名"); err != nil {
		// 网页口径被误改就停住。
		t.Fatalf("web rename others folder: %v", err)
	}
}

// personalRootOf 找某人的个人根节点，没有交回空身份。
func personalRootOf(nodes []factory.FSNodeView, owner uuid.UUID) uuid.UUID {
	// 逐个看节点。
	for _, n := range nodes {
		// 个人树、没有父级、主人对上才算。
		if n.TreeLevel == store.FSTreePersonal && n.ParentID == nil && n.OwnerID != nil && *n.OwnerID == owner {
			return n.ID
		}
	}
	return uuid.Nil
}
