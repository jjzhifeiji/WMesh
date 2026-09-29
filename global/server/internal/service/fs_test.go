// 平台级目录树：同级重名拒绝、搬家、删文件夹会连带删工艺。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 验同级重名拒绝、搬家，删文件夹连带删工艺。
func TestPlatformFS(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 新建平台工艺「根上的工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "根上的工艺", []byte("body"))
	// 新建平台工艺「根上的工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「根上的工艺」失败。
		t.Fatal(err)
	}
	// 列目录，目录列不出来就找不到根。
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	// 列目录失败就停，目录列不出来就找不到根。
	if err != nil {
		// 不符即停：列目录失败。
		t.Fatal(err)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var root, file global.FSNodeView
	// 逐条查看结果，漏看一条会把结论判错。
	for _, n := range nodes {
		// 条件成立才做这一支，漏进来会算错样本。
		if n.ParentID == nil {
			// 记下这个目录节点，后面搬家和删除都指它。
			root = n
		}
		// 条件成立才做这一支，漏进来会算错样本。
		if n.AssetID != nil && *n.AssetID == proc.ID {
			// 记下这个目录节点，后面搬家和删除都指它。
			file = n
		}
	}
	// 根节点身份没有分开或不该是空的就停，不能当通过。
	if root.ID == [16]byte{} || file.ID == [16]byte{} || file.ParentID == nil || *file.ParentID != root.ID {
		t.Fatalf("root/file %+v %+v", root, file)
	}
	// 建文件夹「标准库」，文件夹建不成目录就断了。
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库")
	// 建文件夹「标准库」失败就停，文件夹建不成目录就断了。
	if err != nil {
		// 不符即停：建文件夹「标准库」失败。
		t.Fatal(err)
	}
	// 建文件夹「标准库」应被拒为同级重名，放行或错类都算没拦住。
	if _, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库"); !errors.Is(err, domain.ErrDuplicateName) {
		// 不符即停：建文件夹「标准库」应被拒为同级重名。
		t.Fatalf("dup: %v", err)
	}
	// 移动目录节点，搬不走路径就不会变。
	moved, err := h.WAN.MoveFSNode(ctx, tok, file.ID, folder.ID)
	// 移动目录节点失败或不该是空的就停，不能当通过。
	if err != nil || moved.ParentID == nil || *moved.ParentID != folder.ID {
		// 不符即停：移动目录节点失败或不该是空的。
		t.Fatalf("move %+v %v", moved, err)
	}
	// 移动目录节点应被拒为越权，放行或错类都算没拦住。
	if _, err := h.WAN.MoveFSNode(ctx, tok, folder.ID, file.ID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrCycle) {
		// 不符即停：移动目录节点应被拒为越权。
		t.Fatalf("into file: %v", err)
	}
	// 重命名节点「工艺库」，改名失败重名规则验不成。
	renamed, err := h.WAN.RenameFSNode(ctx, tok, folder.ID, "工艺库")
	// 重命名节点「工艺库」失败或名称不是工艺库就停，不能当通过。
	if err != nil || renamed.Name != "工艺库" {
		// 不符即停：重命名节点「工艺库」失败或名称不是工艺库。
		t.Fatalf("rename %+v %v", renamed, err)
	}
	// 重命名节点「不该改」应被拒为越权，放行或错类都算没拦住。
	if _, err := h.WAN.RenameFSNode(ctx, tok, file.ID, "不该改"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：重命名节点「不该改」应被拒为越权。
		t.Fatalf("rename file: %v", err)
	}
	// 删除目录节点失败就停，删不掉就验不了连带删除。
	if err := h.WAN.DeleteFSNode(ctx, tok, folder.ID); err != nil {
		// 不符即停：删除目录节点失败。
		t.Fatal(err)
	}
	// 读取资产应被拒为找不到，放行或错类都算没拦住。
	if _, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：读取资产应被拒为找不到。
		t.Fatalf("file should be gone: %v", err)
	}
	// 点名文件夹种类，确认这个枚举仍可引用。
	_ = store.FSNodeFolder
}

// 验复制不改原件，复制到自身必须成环拒绝。
func TestPlatformCopyFS(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 新建平台工艺「根上的工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "根上的工艺", []byte("body"))
	// 新建平台工艺「根上的工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「根上的工艺」失败。
		t.Fatal(err)
	}
	// 列目录，目录列不出来就找不到根。
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	// 列目录失败就停，目录列不出来就找不到根。
	if err != nil {
		// 不符即停：列目录失败。
		t.Fatal(err)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var root global.FSNodeView
	// 逐条查看结果，漏看一条会把结论判错。
	for _, n := range nodes {
		// 条件成立才做这一支，漏进来会算错样本。
		if n.ParentID == nil {
			// 记下这个目录节点，后面搬家和删除都指它。
			root = n
		}
	}
	// 根节点身份没有分开就停，说明没有分成新的一份。
	if root.ID == [16]byte{} {
		t.Fatal("missing root")
	}
	// 建文件夹「甲」，文件夹建不成目录就断了。
	alpha, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "甲")
	// 建文件夹「甲」失败就停，文件夹建不成目录就断了。
	if err != nil {
		// 不符即停：建文件夹「甲」失败。
		t.Fatal(err)
	}
	// 建文件夹「乙」，文件夹建不成目录就断了。
	beta, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "乙")
	// 建文件夹「乙」失败就停，文件夹建不成目录就断了。
	if err != nil {
		// 不符即停：建文件夹「乙」失败。
		t.Fatal(err)
	}
	// 按资产找目录文件，找不到工艺文件就搬不了家。
	file, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	// 按资产找目录文件失败就停，找不到工艺文件就搬不了家。
	if err != nil {
		// 不符即停：按资产找目录文件失败。
		t.Fatal(err)
	}
	// 复制目录节点应被拒为越权，放行或错类都算没拦住。
	if _, err := h.WAN.CopyFSNode(ctx, tok, root.ID, beta.ID); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：复制目录节点应被拒为越权。
		t.Fatalf("copy root: %v", err)
	}
	// 复制目录节点，复制失败就没有新节点。
	dup, err := h.WAN.CopyFSNode(ctx, tok, file.ID, alpha.ID)
	// 复制目录节点失败或不该是空的就停，不能当通过。
	if err != nil || dup.ParentID == nil || *dup.ParentID != alpha.ID || dup.AssetID == nil || *dup.AssetID == proc.ID {
		// 不符即停：复制目录节点失败或不该是空的。
		t.Fatalf("copy file %+v %v", dup, err)
	}
	// 按资产找目录文件，找不到工艺文件就搬不了家。
	still, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	// 按资产找目录文件失败或身份变了就停，不能当通过。
	if err != nil || still.ID != file.ID {
		// 不符即停：按资产找目录文件失败或身份变了。
		t.Fatalf("src moved %+v %v", still, err)
	}
	// 移动目录节点失败就停，搬不走路径就不会变。
	if _, err := h.WAN.MoveFSNode(ctx, tok, file.ID, alpha.ID); err != nil {
		// 不符即停：移动目录节点失败。
		t.Fatal(err)
	}
	// 复制目录节点，复制失败就没有新节点。
	folderCopy, err := h.WAN.CopyFSNode(ctx, tok, alpha.ID, beta.ID)
	// 复制目录节点失败或不该是空的就停，不能当通过。
	if err != nil || folderCopy.ParentID == nil || *folderCopy.ParentID != beta.ID {
		// 不符即停：复制目录节点失败或不该是空的。
		t.Fatalf("copy folder %+v %v", folderCopy, err)
	}
	// 列子节点，子节点列不出来复制对不上。
	kids, err := h.WAN.Store().ListFSChildren(ctx, folderCopy.ID)
	// 列子节点失败或条数不是2就停，不能当通过。
	if err != nil || len(kids) != 2 {
		// 不符即停：列子节点失败或条数不是2。
		t.Fatalf("folder copy kids %d %v", len(kids), err)
	}
	// 复制目录节点应被拒为成环，放行或错类都算没拦住。
	if _, err := h.WAN.CopyFSNode(ctx, tok, alpha.ID, alpha.ID); !errors.Is(err, domain.ErrCycle) {
		// 不符即停：复制目录节点应被拒为成环。
		t.Fatalf("into self: %v", err)
	}
}

// 验组包带上目录路径，搬家后路径跟着变。
func TestPlatformFSPathInClosure(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
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
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 新建平台工艺「路径工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "路径工艺", []byte("body"))
	// 新建平台工艺「路径工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「路径工艺」失败。
		t.Fatal(err)
	}
	// 发布资产失败就停，发不出去后面不能当可用。
	if proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision); err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 列目录，目录列不出来就找不到根。
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	// 列目录失败就停，目录列不出来就找不到根。
	if err != nil {
		// 不符即停：列目录失败。
		t.Fatal(err)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var root global.FSNodeView
	// 逐条查看结果，漏看一条会把结论判错。
	for _, n := range nodes {
		// 条件成立才做这一支，漏进来会算错样本。
		if n.ParentID == nil {
			// 记下这个目录节点，后面搬家和删除都指它。
			root = n
		}
	}
	// 建文件夹「标准库」，文件夹建不成目录就断了。
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库")
	// 建文件夹「标准库」失败就停，文件夹建不成目录就断了。
	if err != nil {
		// 不符即停：建文件夹「标准库」失败。
		t.Fatal(err)
	}
	// 按资产找目录文件，找不到工艺文件就搬不了家。
	file, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	// 按资产找目录文件失败就停，找不到工艺文件就搬不了家。
	if err != nil {
		// 不符即停：按资产找目录文件失败。
		t.Fatal(err)
	}
	// 移动目录节点失败就停，搬不走路径就不会变。
	if _, err := h.WAN.MoveFSNode(ctx, tok, file.ID, folder.ID); err != nil {
		// 不符即停：移动目录节点失败。
		t.Fatal(err)
	}
	// 按这份资产组包，组不出包下发就没有内容。
	snap, err := h.WAN.PackAssetForFactory(ctx, proc.ID, created.Factory.ID)
	// 按这份资产组包失败或条数不是1就停，不能当通过。
	if err != nil || len(snap.Members) != 1 {
		// 不符即停：按这份资产组包失败或条数不是1。
		t.Fatalf("pack %+v %v", snap, err)
	}
	// 取出组包里的那一条，路径不对就停。
	m := snap.Members[0]
	// 组包成员不该是空的或目录路径不对就停，不能当通过。
	if m.FSParentID == nil || *m.FSParentID != folder.ID {
		// 不符即停：组包成员不该是空的或目录路径不对。
		t.Fatalf("fs parent %+v", m.FSParentID)
	}
	// 组包成员多项不符预期就停，说明没有按规则落下。
	if len(m.FSPath) != 1 || m.FSPath[0].ID != folder.ID || m.FSPath[0].Name != "标准库" {
		// 不符即停：组包成员多项不符预期。
		t.Fatalf("fs path %+v", m.FSPath)
	}
	// 读目录布局，布局读不到路径就对不上。
	layout, err := h.WAN.Store().PlatformFSLayout(ctx, global.KindProcess)
	// 读目录布局失败或结果是空的就停，不能当通过。
	if err != nil || len(layout.Files) == 0 {
		// 不符即停：读目录布局失败或结果是空的。
		t.Fatalf("layout %+v %v", layout, err)
	}
}
