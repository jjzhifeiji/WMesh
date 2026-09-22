// 本厂目录树：同事看不见别人的个人树，超管能看见；平台副本树只读。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestFactoryFS(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	other := mustCreateRole(t, ctx, fac, sa, "ot", "ot-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}

	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "我的工艺", []byte("p"))
	if err != nil {
		t.Fatal(err)
	}
	facRow, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂级工艺", []byte("f"))
	if err != nil {
		t.Fatal(err)
	}

	peNodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	otNodes, err := fac.ListFS(ctx, other.tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	saNodes, err := fac.ListFS(ctx, sa, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	if !fsHasAsset(peNodes, mine.ID) || !fsHasAsset(saNodes, mine.ID) || fsHasAsset(otNodes, mine.ID) {
		t.Fatalf("personal visibility pe=%v sa=%v ot=%v", fsHasAsset(peNodes, mine.ID), fsHasAsset(saNodes, mine.ID), fsHasAsset(otNodes, mine.ID))
	}
	if !fsHasAsset(otNodes, facRow.ID) {
		t.Fatal("factory tree should be public")
	}

	var factoryRoot factory.FSNodeView
	for _, n := range peNodes {
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			factoryRoot = n
		}
	}
	if factoryRoot.ID == [16]byte{} {
		t.Fatal("missing factory root")
	}
	folder, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "公用夹")
	if err != nil {
		t.Fatal(err)
	}
	var platRoot factory.FSNodeView
	for _, n := range peNodes {
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreePlatform && n.ParentID == nil {
			platRoot = n
		}
	}
	if _, err := fac.CreateFSFolder(ctx, pe.tok, platRoot.ID, "不能写"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("platform mkdir: %v", err)
	}

	file, err := fac.Store().FSFileByAsset(ctx, facRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.MoveFSNode(ctx, pe.tok, file.ID, folder.ID); err != nil {
		t.Fatal(err)
	}

	chanNodes, err := fac.ListFSForChannel(ctx, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	if !fsHasAsset(chanNodes, mine.ID) {
		t.Fatal("cloud should see personal structure")
	}
}

func TestPlatformFSPathFromWAN(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	fid := seed.ID
	body := []byte("plat-fs")
	folderID := id.New()
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: body, Digest: digest.Sum(body),
		FSParentID: &folderID, FSPath: []factory.FSFolderHint{{ID: folderID, Name: "标准库"}},
	}
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	nodes, err := fac.ListFS(ctx, tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fac.Store().FSFileByAsset(ctx, m.ID)
	if err != nil || file.ParentID == nil || *file.ParentID != folderID {
		t.Fatalf("file parent %+v %v", file, err)
	}
	if !fsHasFolder(nodes, folderID, "标准库") {
		t.Fatal("missing synced folder")
	}

	next := id.New()
	m.FSParentID = &next
	m.FSPath = []factory.FSFolderHint{{ID: next, Name: "焊接"}}
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	file, err = fac.Store().FSFileByAsset(ctx, m.ID)
	if err != nil || file.ParentID == nil || *file.ParentID != next {
		t.Fatalf("moved %+v %v", file, err)
	}
	nodes, err = fac.ListFS(ctx, tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	if fsHasFolder(nodes, folderID, "标准库") {
		t.Fatal("empty old folder should be gone")
	}
	if !fsHasFolder(nodes, next, "焊接") {
		t.Fatal("missing new folder")
	}
}

func TestPadNestedFS(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	body := []byte(`{"name":"mine","current":170}`)
	got, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板工艺", body, id.New(), "GY-C0008-000001", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := fac.ListFS(ctx, op.tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	var personalRoot factory.FSNodeView
	for _, n := range nodes {
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreePersonal && n.ParentID == nil {
			personalRoot = n
		}
	}
	if personalRoot.ID == [16]byte{} {
		t.Fatal("missing personal root")
	}
	folder, err := fac.CreateFSFolder(ctx, op.tok, personalRoot.ID, "现场夹")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.MoveAssetInto(ctx, op.tok, got.ID, folder.ID); err != nil {
		t.Fatal(err)
	}
	packed, err := fac.PadPullClientClosure(ctx, op.tok, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packed.Snapshot.Members) != 1 {
		t.Fatalf("members %d", len(packed.Snapshot.Members))
	}
	m := packed.Snapshot.Members[0]
	if m.FSParentID == nil || *m.FSParentID != folder.ID {
		t.Fatalf("parent %+v", m.FSParentID)
	}
	if len(m.FSPath) != 1 || m.FSPath[0].Name != "现场夹" {
		t.Fatalf("path %+v", m.FSPath)
	}
}

func fsHasAsset(nodes []factory.FSNodeView, id [16]byte) bool {
	for _, n := range nodes {
		if n.AssetID != nil && *n.AssetID == id {
			return true
		}
	}
	return false
}

func fsHasFolder(nodes []factory.FSNodeView, id [16]byte, name string) bool {
	for _, n := range nodes {
		if n.ID == id && n.NodeKind == store.FSNodeFolder && n.Name == name {
			return true
		}
	}
	return false
}

func TestCopyFS(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	src, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂级源", []byte("copy-fs"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	var factoryRoot factory.FSNodeView
	for _, n := range nodes {
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			factoryRoot = n
		}
	}
	if factoryRoot.ID == [16]byte{} {
		t.Fatal("missing factory root")
	}
	alpha, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "甲")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "乙")
	if err != nil {
		t.Fatal(err)
	}
	file, err := fac.Store().FSFileByAsset(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CopyFSNode(ctx, pe.tok, factoryRoot.ID, beta.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("copy root: %v", err)
	}
	dup, err := fac.CopyFSNode(ctx, pe.tok, file.ID, alpha.ID)
	if err != nil || dup.ParentID == nil || *dup.ParentID != alpha.ID || dup.AssetID == nil || *dup.AssetID == src.ID {
		t.Fatalf("copy file %+v %v", dup, err)
	}
	still, err := fac.Store().FSFileByAsset(ctx, src.ID)
	if err != nil || still.ID != file.ID {
		t.Fatalf("src moved %+v %v", still, err)
	}
	if _, err := fac.MoveFSNode(ctx, pe.tok, file.ID, alpha.ID); err != nil {
		t.Fatal(err)
	}
	folderCopy, err := fac.CopyFSNode(ctx, pe.tok, alpha.ID, beta.ID)
	if err != nil || folderCopy.ParentID == nil || *folderCopy.ParentID != beta.ID || folderCopy.NodeKind != store.FSNodeFolder {
		t.Fatalf("copy folder %+v %v", folderCopy, err)
	}
	kids, err := fac.Store().ListFSChildren(ctx, folderCopy.ID)
	if err != nil || len(kids) != 2 {
		t.Fatalf("folder copy kids %d %v", len(kids), err)
	}
	origKids, err := fac.Store().ListFSChildren(ctx, alpha.ID)
	if err != nil || len(origKids) != 2 {
		t.Fatalf("src folder kids %d %v", len(origKids), err)
	}
	if _, err := fac.CopyFSNode(ctx, pe.tok, alpha.ID, alpha.ID); !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("into self: %v", err)
	}
	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人源", []byte("p"))
	if err != nil {
		t.Fatal(err)
	}
	mineFile, err := fac.Store().FSFileByAsset(ctx, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CopyFSNode(ctx, pe.tok, mineFile.ID, factoryRoot.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("personal into factory: %v", err)
	}
	platBody := []byte("plat-copy-fs")
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台可复制",
		Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: platBody, Digest: digest.Sum(platBody),
	}
	fid := seed.ID
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	platFile, err := fac.Store().FSFileByAsset(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	fromPlat, err := fac.CopyFSNode(ctx, pe.tok, platFile.ID, beta.ID)
	if err != nil || fromPlat.ParentID == nil || *fromPlat.ParentID != beta.ID || fromPlat.AssetID == nil {
		t.Fatalf("platform copy %+v %v", fromPlat, err)
	}
	copied, err := fac.GetAsset(ctx, pe.tok, *fromPlat.AssetID)
	if err != nil || copied.Level != factory.AssetLevelFactory || copied.ID == m.ID {
		t.Fatalf("platform copy asset %+v %v", copied, err)
	}
}
