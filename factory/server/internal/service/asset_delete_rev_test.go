// 网页删除也带期望修订：持旧页面删别人刚改过的件回修订冲突，原件不变（D-44）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// 建件后改名到修订 2，拿修订 1 或不带修订删都拒，拿修订 2 删成功。
func TestDeleteAssetAtChecksRevision(t *testing.T) {
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
	tok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一份厂级工艺，修订 1。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{}, "待删", []byte(`{"current":100}`))
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 别人改名，修订到 2。
	renamed, err := fac.RenameAsset(ctx, tok, row.ID, row.Revision, "待删改")
	// 改不了就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 持旧页面拿修订 1 删，应回修订冲突。
	if err := fac.DeleteAssetAt(ctx, tok, row.ID, row.Revision); !errors.Is(err, domain.ErrRevisionConflict) {
		// 放行就停住。
		t.Fatalf("stale delete: %v", err)
	}
	// 不带修订删，同样回修订冲突。
	if err := fac.DeleteAssetAt(ctx, tok, row.ID, 0); !errors.Is(err, domain.ErrRevisionConflict) {
		// 放行就停住。
		t.Fatalf("no revision delete: %v", err)
	}
	// 原件还在，修订仍是 2。
	got, err := fac.GetAsset(ctx, tok, row.ID)
	// 读不到或修订变了就停住。
	if err != nil || got.Revision != renamed.Revision {
		// 把结果打出来并停住。
		t.Fatalf("asset after stale delete: %+v %v", got, err)
	}
	// 文件节点也还在，目录里照常看得到。
	if _, err := fac.Store().FSFileByAsset(ctx, row.ID); err != nil {
		// 节点被删就停住。
		t.Fatalf("fs node after stale delete: %v", err)
	}
	// 从目录树删文件带旧修订，同样回修订冲突。
	node, err := fac.Store().FSFileByAsset(ctx, row.ID)
	// 找不到节点就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 目录树按旧修订删应拒。
	if err := fac.DeleteFSNodeAt(ctx, tok, node.ID, row.Revision); !errors.Is(err, domain.ErrRevisionConflict) {
		// 放行就停住。
		t.Fatalf("stale fs delete: %v", err)
	}
	// 拿当前修订删，成功。
	if err := fac.DeleteAssetAt(ctx, tok, row.ID, renamed.Revision); err != nil {
		// 删不掉就停住。
		t.Fatalf("delete current: %v", err)
	}
	// 删完读不到了。
	if _, err := fac.GetAsset(ctx, tok, row.ID); !errors.Is(err, domain.ErrNotFound) {
		// 还在就停住。
		t.Fatalf("asset after delete: %v", err)
	}
}

// 示教器同步删件不跟网页对版本号：示教器会话不带修订照旧能删，网页会话不带修订仍回冲突。
func TestPadDeleteAssetWithoutRevision(t *testing.T) {
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
	tok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 超管示教器会话。
	pad, err := fac.LoginPad(ctx, "sa", personPass("sa"))
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("sa pad login: %v", err)
	}
	// 建一份厂级工艺，修订 1。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{}, "示教器删", []byte(`{"current":100}`))
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 网页会话不带修订删仍回冲突。
	if err := fac.DeleteAssetAt(ctx, tok, row.ID, 0); !errors.Is(err, domain.ErrRevisionConflict) {
		// 放行就停住。
		t.Fatalf("web delete without revision: %v", err)
	}
	// 示教器会话不带修订照旧删成。
	if err := fac.DeleteAssetAt(ctx, pad.Token, row.ID, 0); err != nil {
		// 删不掉就停住。
		t.Fatalf("pad delete without revision: %v", err)
	}
	// 删完读不到了。
	if _, err := fac.GetAsset(ctx, tok, row.ID); !errors.Is(err, domain.ErrNotFound) {
		// 还在就停住。
		t.Fatalf("asset after pad delete: %v", err)
	}
}
