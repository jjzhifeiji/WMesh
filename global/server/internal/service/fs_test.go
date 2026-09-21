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

func TestPlatformFS(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "根上的工艺", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	var root, file global.FSNodeView
	for _, n := range nodes {
		if n.ParentID == nil {
			root = n
		}
		if n.AssetID != nil && *n.AssetID == proc.ID {
			file = n
		}
	}
	if root.ID == [16]byte{} || file.ID == [16]byte{} || file.ParentID == nil || *file.ParentID != root.ID {
		t.Fatalf("root/file %+v %+v", root, file)
	}
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库"); !errors.Is(err, domain.ErrDuplicateName) {
		t.Fatalf("dup: %v", err)
	}
	moved, err := h.WAN.MoveFSNode(ctx, tok, file.ID, folder.ID)
	if err != nil || moved.ParentID == nil || *moved.ParentID != folder.ID {
		t.Fatalf("move %+v %v", moved, err)
	}
	if _, err := h.WAN.MoveFSNode(ctx, tok, folder.ID, file.ID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("into file: %v", err)
	}
	renamed, err := h.WAN.RenameFSNode(ctx, tok, folder.ID, "工艺库")
	if err != nil || renamed.Name != "工艺库" {
		t.Fatalf("rename %+v %v", renamed, err)
	}
	if _, err := h.WAN.RenameFSNode(ctx, tok, file.ID, "不该改"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("rename file: %v", err)
	}
	if err := h.WAN.DeleteFSNode(ctx, tok, folder.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("file should be gone: %v", err)
	}
	_ = store.FSNodeFolder
}

func TestPlatformCopyFS(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "根上的工艺", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	var root global.FSNodeView
	for _, n := range nodes {
		if n.ParentID == nil {
			root = n
		}
	}
	if root.ID == [16]byte{} {
		t.Fatal("missing root")
	}
	alpha, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "甲")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "乙")
	if err != nil {
		t.Fatal(err)
	}
	file, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.CopyFSNode(ctx, tok, root.ID, beta.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("copy root: %v", err)
	}
	dup, err := h.WAN.CopyFSNode(ctx, tok, file.ID, alpha.ID)
	if err != nil || dup.ParentID == nil || *dup.ParentID != alpha.ID || dup.AssetID == nil || *dup.AssetID == proc.ID {
		t.Fatalf("copy file %+v %v", dup, err)
	}
	still, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	if err != nil || still.ID != file.ID {
		t.Fatalf("src moved %+v %v", still, err)
	}
	if _, err := h.WAN.MoveFSNode(ctx, tok, file.ID, alpha.ID); err != nil {
		t.Fatal(err)
	}
	folderCopy, err := h.WAN.CopyFSNode(ctx, tok, alpha.ID, beta.ID)
	if err != nil || folderCopy.ParentID == nil || *folderCopy.ParentID != beta.ID {
		t.Fatalf("copy folder %+v %v", folderCopy, err)
	}
	kids, err := h.WAN.Store().ListFSChildren(ctx, folderCopy.ID)
	if err != nil || len(kids) != 2 {
		t.Fatalf("folder copy kids %d %v", len(kids), err)
	}
	if _, err := h.WAN.CopyFSNode(ctx, tok, alpha.ID, alpha.ID); !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("into self: %v", err)
	}
}

func TestPlatformFSPathInClosure(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "路径工艺", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision); err != nil {
		t.Fatal(err)
	}
	nodes, err := h.WAN.ListFS(ctx, tok, global.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	var root global.FSNodeView
	for _, n := range nodes {
		if n.ParentID == nil {
			root = n
		}
	}
	folder, err := h.WAN.CreateFSFolder(ctx, tok, root.ID, "标准库")
	if err != nil {
		t.Fatal(err)
	}
	file, err := h.WAN.Store().FSFileByAsset(ctx, proc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.MoveFSNode(ctx, tok, file.ID, folder.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := h.WAN.PackAssetForFactory(ctx, proc.ID, created.Factory.ID)
	if err != nil || len(snap.Members) != 1 {
		t.Fatalf("pack %+v %v", snap, err)
	}
	m := snap.Members[0]
	if m.FSParentID == nil || *m.FSParentID != folder.ID {
		t.Fatalf("fs parent %+v", m.FSParentID)
	}
	if len(m.FSPath) != 1 || m.FSPath[0].ID != folder.ID || m.FSPath[0].Name != "标准库" {
		t.Fatalf("fs path %+v", m.FSPath)
	}
	layout, err := h.WAN.Store().PlatformFSLayout(ctx, global.KindProcess)
	if err != nil || len(layout.Files) == 0 {
		t.Fatalf("layout %+v %v", layout, err)
	}
}
