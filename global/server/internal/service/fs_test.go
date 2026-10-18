// 平台级目录树：同级重名拒绝、搬家、删文件夹会连带删工艺。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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

// 验删文件夹先整体预检：后面的工艺仍被引用，前面的也不能先删先收回。
func TestPlatformFSDeleteFolderAllOrNothing(t *testing.T) {
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
	// 名字排前面的工艺，不被任何工程引用。
	free, err := h.WAN.CreatePlatformProcess(ctx, tok, "a-未引用", []byte("body"))
	// 新建未引用工艺失败就停，没有它验不了先删。
	if err != nil {
		// 不符即停：新建未引用工艺失败。
		t.Fatal(err)
	}
	// 名字排后面的工艺，稍后被工程钉住。
	used, err := h.WAN.CreatePlatformProcess(ctx, tok, "b-被引用", []byte("body"))
	// 新建被引用工艺失败就停，没有它造不出阻塞。
	if err != nil {
		// 不符即停：新建被引用工艺失败。
		t.Fatal(err)
	}
	// 发布后才能被工程依赖。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, used.ID, used.Revision)
	// 发布失败就停，发不出去工程钉不住它。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 建一条钉住后一个工艺的工程，造出引用。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "钉工程", []byte(`[]`), []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}})
	// 建工程失败就停，没有引用验不了拒绝。
	if err != nil {
		// 不符即停：新建平台工程失败。
		t.Fatal(err)
	}
	// 取工艺目录根，文件夹要建在它下面。
	root, err := h.WAN.Store().EnsureFSRoot(ctx, global.KindProcess)
	// 取根失败就停，没有根建不了文件夹。
	if err != nil {
		// 不符即停：取工艺目录根失败。
		t.Fatal(err)
	}
	// 建要删的文件夹，两个工艺都放进去。
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "待删夹")
	// 建文件夹失败就停，文件夹建不成验不了整删。
	if err != nil {
		// 不符即停：建文件夹失败。
		t.Fatal(err)
	}
	// 逐个把两个工艺搬进文件夹。
	for _, a := range []global.Asset{free, used} {
		// 按资产找目录文件，找不到就搬不了家。
		file, err := h.WAN.Store().FSFileByAsset(ctx, a.ID)
		// 找目录文件失败就停，搬不进去就验不了。
		if err != nil {
			// 不符即停：按资产找目录文件失败。
			t.Fatal(err)
		}
		// 搬进文件夹失败就停，两个工艺须同在一夹。
		if _, err := h.WAN.MoveFSNode(ctx, tok, file.ID, folder.ID); err != nil {
			// 不符即停：移动目录节点失败。
			t.Fatal(err)
		}
	}
	// 删文件夹应整体被拒为仍被引用，放行或错类都算没拦住。
	if err := h.WAN.DeleteFSNode(ctx, tok, folder.ID); !errors.Is(err, domain.ErrReferenced) {
		// 不符即停：删文件夹没被整体拒绝。
		t.Fatalf("delete folder: %v", err)
	}
	// 逐个确认两个工艺都还在，并且仍挂在文件夹下。
	for _, a := range []global.Asset{free, used} {
		// 读取资产失败就说明已被删掉。
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, a.ID); err != nil {
			// 不符即停：工艺不该被删。
			t.Fatalf("asset %s gone: %v", a.Name, err)
		}
		// 按资产找目录文件，应仍在原文件夹。
		file, err := h.WAN.Store().FSFileByAsset(ctx, a.ID)
		// 找不到或不在文件夹下就停，目录被动过。
		if err != nil || file.ParentID == nil || *file.ParentID != folder.ID {
			// 不符即停：目录文件不在原文件夹。
			t.Fatalf("file %s %+v %v", a.Name, file, err)
		}
	}
	// 文件夹节点本身也应还在。
	if _, err := h.WAN.Store().FSNodeByID(ctx, folder.ID); err != nil {
		// 不符即停：文件夹节点不该被删。
		t.Fatalf("folder gone: %v", err)
	}
	// 列出收回记录，前一个工艺不该在里面。
	retracted, err := h.WAN.Store().ListRetractions(ctx)
	// 列收回记录失败就停，验不了有没有收回。
	if err != nil {
		// 不符即停：列收回记录失败。
		t.Fatal(err)
	}
	// 逐条看收回记录，有任一工艺就说明已从厂收回。
	for _, id := range retracted {
		// 任一工艺出现在收回里都算部分删除。
		if id == free.ID || id == used.ID {
			// 不符即停：工艺不该被收回。
			t.Fatalf("retracted %s", id)
		}
	}
	// 删掉钉住它的工程，解除阻塞。
	if err := h.WAN.DeletePlatformAsset(ctx, tok, proj.ID); err != nil {
		// 不符即停：删工程失败。
		t.Fatal(err)
	}
	// 阻塞解除后再删文件夹应成功。
	if err := h.WAN.DeleteFSNode(ctx, tok, folder.ID); err != nil {
		// 不符即停：解除引用后删文件夹失败。
		t.Fatalf("delete after unref: %v", err)
	}
	// 逐个确认两个工艺都已连带删掉。
	for _, a := range []global.Asset{free, used} {
		// 读取资产应被拒为找不到，还在就是没连带删。
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, a.ID); !errors.Is(err, domain.ErrNotFound) {
			// 不符即停：工艺应已删掉。
			t.Fatalf("asset %s should be gone: %v", a.Name, err)
		}
	}
}

// 验复制文件夹先整体预检：子树里有停用件就整夹拒绝，不留半截文件夹或副本。
func TestPlatformCopyFSAllOrNothing(t *testing.T) {
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
	// 取工艺目录根，文件夹要建在它下面。
	root, err := h.WAN.Store().EnsureFSRoot(ctx, global.KindProcess)
	// 取根失败就停，没有根建不了文件夹。
	if err != nil {
		// 不符即停：取工艺目录根失败。
		t.Fatal(err)
	}
	// 建复制源文件夹，装一好一坏两份工艺。
	gamma, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "丙")
	// 建文件夹失败就停，没有源夹验不了整夹复制。
	if err != nil {
		// 不符即停：建源文件夹失败。
		t.Fatal(err)
	}
	// 源夹下再建子夹，停用件藏在里面考递归预检。
	gammaSub, err := h.WAN.CreateFSFolder(ctx, tok, gamma.ID, "丙子")
	// 建子夹失败就停，没有子夹验不了递归。
	if err != nil {
		// 不符即停：建子文件夹失败。
		t.Fatal(err)
	}
	// 建粘贴目标文件夹，被拒后里面应仍为空。
	dest, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "目标")
	// 建目标夹失败就停，没有落脚处验不了复制。
	if err != nil {
		// 不符即停：建目标文件夹失败。
		t.Fatal(err)
	}
	// 建一份正常工艺，旧逻辑会先把它复制出去。
	good, err := h.WAN.CreatePlatformProcess(ctx, tok, "好工艺", []byte("good"))
	// 建正常工艺失败就停，没有它验不了先复制。
	if err != nil {
		// 不符即停：新建正常工艺失败。
		t.Fatal(err)
	}
	// 建一份稍后停用的工艺，整夹复制应被它挡住。
	off, err := h.WAN.CreatePlatformProcess(ctx, tok, "停用工艺", []byte("off"))
	// 建待停用工艺失败就停，没有它造不出阻塞。
	if err != nil {
		// 不符即停：新建待停用工艺失败。
		t.Fatal(err)
	}
	// 先发布，只有可用才能停用。
	off, err = h.WAN.PublishPlatformAsset(ctx, tok, off.ID, off.Revision)
	// 发布失败就停，停用不了。
	if err != nil {
		// 不符即停：发布待停用工艺失败。
		t.Fatal(err)
	}
	// 停用这份工艺，另存时应被拒为不可用。
	off, err = h.WAN.DisablePlatformAsset(ctx, tok, off.ID, off.Revision)
	// 停用失败就停，造不出阻塞。
	if err != nil {
		// 不符即停：停用工艺失败。
		t.Fatal(err)
	}
	// 正常工艺挂进源夹，失败则前提断了。
	if _, err := h.WAN.MoveAssetInto(ctx, tok, good.ID, gamma.ID); err != nil {
		// 不符即停：正常工艺挂进源夹失败。
		t.Fatal(err)
	}
	// 停用工艺挂进子夹，失败则前提断了。
	if _, err := h.WAN.MoveAssetInto(ctx, tok, off.ID, gammaSub.ID); err != nil {
		// 不符即停：停用工艺挂进子夹失败。
		t.Fatal(err)
	}
	// 记下复制前的节点数和资产数，被拒后要原样不变。
	nodesBefore, assetsBefore := platformFSTotals(t, ctx, h)
	// 夹里有停用件，整夹复制必须被拒为不可用。
	if _, err := h.WAN.CopyFSNode(ctx, tok, gamma.ID, dest.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 不符即停：带停用件的整夹复制没被拒。
		t.Fatalf("copy folder with disabled: %v", err)
	}
	// 被拒之后再数一遍节点和资产。
	nodesAfter, assetsAfter := platformFSTotals(t, ctx, h)
	// 节点数或资产数变了，说明被拒前已经建了半截。
	if nodesAfter != nodesBefore || assetsAfter != assetsBefore {
		// 不符即停：被拒的复制留下了文件夹或副本。
		t.Fatalf("disabled copy left nodes %d->%d assets %d->%d", nodesBefore, nodesAfter, assetsBefore, assetsAfter)
	}
	// 目标夹下应仍为空。
	destKids, err := h.WAN.Store().ListFSChildren(ctx, dest.ID)
	// 读不出或不空都说明复制留了痕迹。
	if err != nil || len(destKids) != 0 {
		// 不符即停：目标夹里多出了东西。
		t.Fatalf("dest kids %d %v", len(destKids), err)
	}
	// 重新启用停用件，同一整夹复制应能放行。
	if _, err := h.WAN.ReenablePlatformAsset(ctx, tok, off.ID, off.Revision); err != nil {
		// 不符即停：重新启用工艺失败。
		t.Fatal(err)
	}
	// 再复制一次，这回子树里都能另存。
	copied, err := h.WAN.CopyFSNode(ctx, tok, gamma.ID, dest.ID)
	// 复制失败或没落在目标夹下就停，预检拦得太宽了。
	if err != nil || copied.ParentID == nil || *copied.ParentID != dest.ID {
		// 不符即停：全部可另存时整夹复制失败。
		t.Fatalf("copy folder after reenable %+v %v", copied, err)
	}
	// 复制成功后再数一遍节点和资产。
	nodesOK, assetsOK := platformFSTotals(t, ctx, h)
	// 应多出两层文件夹加两个文件，资产多出两份副本。
	if nodesOK != nodesBefore+4 || assetsOK != assetsBefore+2 {
		// 不符即停：整夹复制的数量不对。
		t.Fatalf("copy totals nodes %d->%d assets %d->%d", nodesBefore, nodesOK, assetsBefore, assetsOK)
	}
}

// 验 64 字的文件夹和工艺复制回原夹：截短原名再接副本尾巴，不报同名。
func TestPlatformCopyFSLongName(t *testing.T) {
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
	// 取工艺目录根，文件夹要建在它下面。
	root, err := h.WAN.Store().EnsureFSRoot(ctx, global.KindProcess)
	// 取根失败就停，没有根建不了文件夹。
	if err != nil {
		// 不符即停：取工艺目录根失败。
		t.Fatal(err)
	}
	// 建原文件夹，长名夹和长名工艺都放这里，再复制回这里。
	home, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "长名")
	// 建原文件夹失败就停，没有落脚处验不了复制。
	if err != nil {
		// 不符即停：建原文件夹失败。
		t.Fatal(err)
	}
	// 夹名正好 64 字，第 59 字是空格，考截口处去空白。
	folderName := strings.Repeat("甲", 58) + " " + strings.Repeat("乙", 5)
	// 建长名文件夹，上限以内的名字应能建成。
	long, err := h.WAN.CreateFSFolder(ctx, tok, home.ID, folderName)
	// 建长名文件夹失败就停，前提断了。
	if err != nil {
		// 不符即停：建 64 字文件夹失败。
		t.Fatal(err)
	}
	// 建 64 字的工艺，上限以内的名字应能建成。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, strings.Repeat("工", 64), []byte("long"))
	// 建长名工艺失败就停，前提断了。
	if err != nil {
		// 不符即停：建 64 字工艺失败。
		t.Fatal(err)
	}
	// 长名工艺挂进原文件夹，和长名夹同级。
	file, err := h.WAN.MoveAssetInto(ctx, tok, proc.ID, home.ID)
	// 挂不进去就停，前提断了。
	if err != nil {
		// 不符即停：长名工艺挂进原文件夹失败。
		t.Fatal(err)
	}
	// 两个源各复制两回，名字应依次是副本和副本 (2)。
	cases := []struct {
		src  store.FSNode // 复制源节点
		want []string     // 两回复制依次应得的名字
	}{
		{long, []string{strings.Repeat("甲", 58) + " - 副本", strings.Repeat("甲", 55) + " - 副本 (2)"}},
		{file, []string{strings.Repeat("工", 59) + " - 副本", strings.Repeat("工", 55) + " - 副本 (2)"}},
	}
	// 逐个源核对，每回复制都要截短原名后接尾巴。
	for _, c := range cases {
		// 依次复制，名字按副本、副本 (2) 往下排。
		for _, want := range c.want {
			// 复制回原文件夹，同名已在，应截短原名后接尾巴。
			dup, err := h.WAN.CopyFSNode(ctx, tok, c.src.ID, home.ID)
			// 复制失败或没落在原文件夹就停，长名仍被当成同名。
			if err != nil || dup.ParentID == nil || *dup.ParentID != home.ID {
				// 不符即停：长名复制回原文件夹失败。
				t.Fatalf("copy long %q: %+v %v", c.src.Name, dup, err)
			}
			// 新名不对或超过上限就停，截短规则没生效。
			if dup.Name != want || utf8.RuneCountInString(dup.Name) > store.FSNameMax {
				// 不符即停：复制出的名字不对。
				t.Fatalf("copy name %q want %q", dup.Name, want)
			}
		}
	}
}

// 数出工艺目录的节点数和平台资产数，核对被拒的复制什么都没多。
func platformFSTotals(t *testing.T, ctx context.Context, h *Harness) (int, int) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 读出工艺目录全部节点，读不到就没法比。
	nodes, err := h.WAN.Store().ListFSNodes(ctx, global.KindProcess)
	// 目录读失败就没法比数量。
	if err != nil {
		// 不符即停：读工艺目录失败。
		t.Fatal(err)
	}
	// 读出全部平台资产，读不到就没法比。
	assets, err := h.WAN.Store().ListAssets(ctx)
	// 资产读失败就没法比数量。
	if err != nil {
		// 不符即停：读平台资产失败。
		t.Fatal(err)
	}
	// 交回两个数量，由调用方比对复制前后。
	return len(nodes), len(assets)
}

// 验网页新建挂进文件夹：同名自动带编号；目录已删、挂到文件上或种类不对就整笔不建。
func TestPlatformCreateIntoFolder(t *testing.T) {
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
	// 取工艺目录根，文件夹要建在它下面。
	root, err := h.WAN.Store().EnsureFSRoot(ctx, global.KindProcess)
	// 取根失败就停，没有根建不了文件夹。
	if err != nil {
		// 不符即停：取工艺目录根失败。
		t.Fatal(err)
	}
	// 建标准库夹，两条同名工艺都建到这里。
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库")
	// 建夹失败就停，没有目标夹验不了挂目录。
	if err != nil {
		// 不符即停：建标准库夹失败。
		t.Fatal(err)
	}
	// 第一条建进标准库，名字应原样占位。
	first, err := h.WAN.CreatePlatformProcessIn(ctx, tok, "同名工艺", []byte("a"), false, folder.ID)
	// 第一条建不成就停，挂目录的前提断了。
	if err != nil {
		// 不符即停：建第一条失败。
		t.Fatal(err)
	}
	// 第二条同名再建进去，应成功并自动带编号。
	second, err := h.WAN.CreatePlatformProcessIn(ctx, tok, "同名工艺", []byte("b"), false, folder.ID)
	// 同名被拒说明还按 409 挡新建。
	if err != nil {
		// 不符即停：同名建进文件夹失败。
		t.Fatalf("same name into folder: %v", err)
	}
	// 两条依次应得的文件名：原名，原名加编号。
	want := []struct {
		row  global.Asset // 新建出的资产
		name string       // 目录里应得的文件名
	}{{first, "同名工艺"}, {second, "同名工艺 " + second.Code}}
	// 逐条核对文件节点落在标准库且名字对得上。
	for _, w := range want {
		// 按资产找文件节点，看它挂在哪里。
		file, err := h.WAN.Store().FSFileByAsset(ctx, w.row.ID)
		// 没挂进标准库或名字不对，说明没在同一笔挂好。
		if err != nil || file.ParentID == nil || *file.ParentID != folder.ID || file.Name != w.name {
			// 不符即停：文件挂点或名字不对。
			t.Fatalf("file %+v want %q in %s: %v", file, w.name, folder.ID, err)
		}
	}
	// 第一条的文件节点，拿来当挂不上的目标。
	firstFile, err := h.WAN.Store().FSFileByAsset(ctx, first.ID)
	// 找不到文件节点就停，前提断了。
	if err != nil {
		// 不符即停：读第一条的文件节点失败。
		t.Fatal(err)
	}
	// 建一个夹随即删掉，模拟弹窗拿着旧目录。
	gone, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "旧夹")
	// 建夹失败就停，前提断了。
	if err != nil {
		// 不符即停：建旧夹失败。
		t.Fatal(err)
	}
	// 删掉旧夹失败就停，验不了目录已删。
	if err := h.WAN.DeleteFSNode(ctx, tok, gone.ID); err != nil {
		// 不符即停：删旧夹失败。
		t.Fatal(err)
	}
	// 记下被拒之前的节点数和资产数。
	nodes0, assets0 := platformFSTotals(t, ctx, h)
	// 三种挂不上的目标，各自按原因拒绝。
	cases := []struct {
		label  string                       // 情形说明
		create func() (global.Asset, error) // 这次新建
		want   error                        // 应得的拒绝
	}{
		// 目录已删，应按找不到拒绝。
		{"gone", func() (global.Asset, error) {
			// 建工艺到已删的夹。
			return h.WAN.CreatePlatformProcessIn(ctx, tok, "挂不上", []byte("c"), false, gone.ID)
		}, domain.ErrNotFound},
		// 目标是文件不是文件夹，应拒绝。
		{"file", func() (global.Asset, error) {
			// 建工艺到一个文件节点下。
			return h.WAN.CreatePlatformProcessIn(ctx, tok, "挂不上", []byte("c"), false, firstFile.ID)
		}, domain.ErrForbidden},
		// 工程挂到工艺夹，种类不对应拒绝。
		{"kind", func() (global.Asset, error) {
			// 建工程到工艺目录的夹里。
			return h.WAN.CreatePlatformProjectIn(ctx, tok, "挂不上", []byte(`[]`), nil, folder.ID)
		}, domain.ErrForbidden},
	}
	// 逐个情形试新建，都应整笔不建。
	for _, c := range cases {
		// 放行或错误不对，说明挂目录没和建资产同一笔把关。
		if _, err := c.create(); !errors.Is(err, c.want) {
			// 不符即停：拒绝原因不对。
			t.Fatalf("%s: %v want %v", c.label, err, c.want)
		}
		// 再数一遍节点和资产，被拒的不应留下任何东西。
		nodes, assets := platformFSTotals(t, ctx, h)
		// 多出资产或节点，说明挂不上时没整笔回滚。
		if nodes != nodes0 || assets != assets0 {
			// 不符即停：被拒的新建留下了半截。
			t.Fatalf("%s left rows: nodes %d->%d assets %d->%d", c.label, nodes0, nodes, assets0, assets)
		}
	}
}

// 验厂目录回包按白名单重编：旧厂端多带的主人、创建人和组织字段都到不了浏览器。
func TestDecodeFactoryFSWhitelist(t *testing.T) {
	// 个人树主人身份，回包里不该再出现。
	const owner = "6f1c1d2e-1111-4a5b-8c9d-000000000001"
	// 组织节点身份，回包里不该再出现。
	const unit = "6f1c1d2e-2222-4a5b-8c9d-000000000002"
	// 个人树根身份，文件挂在它下面。
	const root = "6f1c1d2e-3333-4a5b-8c9d-000000000003"
	// 文件节点和资产的身份。
	const asset = "6f1c1d2e-4444-4a5b-8c9d-000000000004"
	// 拼一份旧格式回包：根带主人，文件资产带创建人和组织路径。
	raw := []byte(`[
		{"id":"` + root + `","name":"","parentId":null,"nodeKind":"folder","assetKind":"process","treeLevel":"personal",
		 "ownerId":"` + owner + `","ownerName":"刘焊工","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},
		{"id":"` + asset + `","name":"我的工艺","parentId":"` + root + `","nodeKind":"file","assetKind":"process","treeLevel":"personal",
		 "ownerId":"` + owner + `","assetId":"` + asset + `",
		 "asset":{"id":"` + asset + `","kind":"process","level":"personal","name":"我的工艺","code":"GY-1","revision":3,"digest":"AQID",
		  "status":"draft","copyable":true,"weldKind":"single","creatorId":"` + owner + `","creatorLogin":"liu-welder",
		  "creatorDisplay":"刘焊工","orgUnitId":"` + unit + `","orgPath":"一车间/焊接组","content":"c2VjcmV0"}}
	]`)
	// 按白名单解开旧格式回包。
	rows, err := global.DecodeFactoryFS(raw)
	// 解不开就停，旧厂端回包云端认不了。
	if err != nil || len(rows) != 2 {
		// 不符即停：旧格式回包没解成两行。
		t.Fatalf("decode %d rows: %v", len(rows), err)
	}
	// 文件行的结构和元数据要原样留下。
	file := rows[1]
	// 父级、资产身份、编号、修订和摘要缺一不可。
	if file.ParentID == nil || file.ParentID.String() != root || file.AssetID == nil || file.Asset == nil ||
		file.Asset.Code != "GY-1" || file.Asset.Revision != 3 || !bytes.Equal(file.Asset.Digest, []byte{1, 2, 3}) {
		// 不符即停：白名单字段在重编时丢了。
		t.Fatalf("file row lost fields: %+v", file)
	}
	// 重编成浏览器收到的样子再查。
	out, err := json.Marshal(rows)
	// 编码失败就停，后面无从查字段。
	if err != nil {
		// 不符即停：重编失败。
		t.Fatal(err)
	}
	// 按键名解开，逐个核对只剩白名单字段。
	var back []map[string]json.RawMessage
	// 解不开就停，重编结果不是对象数组。
	if err := json.Unmarshal(out, &back); err != nil {
		// 不符即停：重编结果解不开。
		t.Fatal(err)
	}
	// 目录行允许出现的键。
	nodeKeys := map[string]bool{"id": true, "name": true, "parentId": true, "nodeKind": true, "assetKind": true, "treeLevel": true, "assetId": true, "asset": true}
	// 文件元数据允许出现的键。
	assetKeys := map[string]bool{"id": true, "kind": true, "level": true, "name": true, "code": true, "revision": true, "digest": true, "status": true, "copyable": true, "weldKind": true}
	// 逐行核对键名，白名单外的一个都不许有。
	for _, row := range back {
		// 逐个键看是不是白名单里的。
		for k := range row {
			// 白名单外的键出现就停，人员字段漏到了浏览器。
			if !nodeKeys[k] {
				// 不符即停：目录行多出字段。
				t.Fatalf("node key %q leaked: %s", k, out)
			}
		}
		// 没有资产的文件夹行不用再往下查。
		if row["asset"] == nil {
			continue
		}
		// 把资产元数据按键名解开。
		var a map[string]json.RawMessage
		// 解不开就停，资产元数据不是对象。
		if err := json.Unmarshal(row["asset"], &a); err != nil {
			// 不符即停：资产元数据解不开。
			t.Fatal(err)
		}
		// 逐个键看是不是资产白名单里的。
		for k := range a {
			// 白名单外的键出现就停，创建人或正文漏到了浏览器。
			if !assetKeys[k] {
				// 不符即停：资产元数据多出字段。
				t.Fatalf("asset key %q leaked: %s", k, out)
			}
		}
	}
	// 主人和组织的身份、人名、路径在值里也不许出现。
	for _, bad := range []string{owner, unit, "刘焊工", "liu-welder", "一车间"} {
		// 值里还留着人员或组织信息就停。
		if strings.Contains(string(out), bad) {
			// 不符即停：重编结果带出了人员信息。
			t.Fatalf("value %q leaked: %s", bad, out)
		}
	}
	// 空回包和 null 都要变成空数组，浏览器按列表处理。
	for _, in := range []string{"", "null", "[]"} {
		// 解开这一种空回包。
		got, err := global.DecodeFactoryFS([]byte(in))
		// 解失败或不是空数组就停，浏览器会拿到 null。
		if err != nil || got == nil || len(got) != 0 {
			// 不符即停：空回包没变成空数组。
			t.Fatalf("empty %q: %v %v", in, got, err)
		}
	}
	// 坏回包要报错，由接口层当成厂不在线。
	if _, err := global.DecodeFactoryFS([]byte(`{"oops":1}`)); err == nil {
		// 不符即停：坏回包被当成了目录。
		t.Fatal("bad payload should fail")
	}
}
