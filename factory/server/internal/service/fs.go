package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// FSNodeView 是目录节点加可选资产元数据，不含正文。
type FSNodeView struct {
	store.FSNode
	OwnerName string     `json:"ownerName,omitempty"` // 个人树主人显示名
	Asset     *AssetView `json:"asset,omitempty"`     // 文件才有；不含正文
}

// ListFS 列出当前人能看见的目录；同事看不到别人的个人树。
func (s *Assets) ListFS(ctx context.Context, token, kind string) ([]FSNodeView, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	if err := s.ensureVisibleRoots(ctx, acc, kind); err != nil {
		return nil, err
	}
	nodes, err := s.store.ListFSNodes(ctx, kind)
	if err != nil {
		return nil, err
	}
	admin := s.isComputerAdmin(ctx, acc)
	visible := make([]FSNode, 0, len(nodes))
	for _, n := range nodes {
		if !canSeeFSNode(acc.ID, admin, n) {
			continue
		}
		visible = append(visible, n)
	}
	if err := s.audit(ctx, &acc.ID, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	return s.decorateFS(ctx, visible)
}

// ListFSForChannel 给云端看本厂全部目录结构，不含正文。
func (s *Assets) ListFSForChannel(ctx context.Context, kind string) ([]FSNodeView, error) {
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	if kind == "" {
		kind = KindProcess
	}
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePlatform, nil); err != nil {
		return nil, err
	}
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreeFactory, nil); err != nil {
		return nil, err
	}
	nodes, err := s.store.ListFSNodes(ctx, kind)
	if err != nil {
		return nil, err
	}
	if err := s.audit(ctx, nil, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	return s.decorateFS(ctx, nodes)
}

// ApplyPlatformFSLayout 通道对齐云端平台目录；只动已到达的副本。
func (s *Assets) ApplyPlatformFSLayout(ctx context.Context, layout store.PlatformFSLayout) error {
	if err := s.store.ApplyPlatformFSLayout(ctx, layout); err != nil {
		_ = s.audit(ctx, nil, nil, "apply_fs", layout.Kind, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "apply_fs", layout.Kind, audit.Allow)
}

// CreateFSFolder 在可写树里新建文件夹；平台级副本树只读。
func (s *Assets) CreateFSFolder(ctx context.Context, token string, parentID uuid.UUID, name string) (FSNode, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FSNode{}, err
	}
	parent, err := s.store.FSNodeByID(ctx, parentID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	if parent.NodeKind != store.FSNodeFolder {
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	if err := s.canWriteFS(ctx, acc, parent); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	row, err := s.store.InsertFSFolder(ctx, parentID, name)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "create_fs_folder", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// RenameFSNode 改文件夹名；文件改名走资产改名。
func (s *Assets) RenameFSNode(ctx context.Context, token string, nodeID uuid.UUID, name string) (FSNode, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FSNode{}, err
	}
	n, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if n.NodeKind != store.FSNodeFolder {
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	row, err := s.store.RenameFSNode(ctx, nodeID, name)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// MoveFSNode 把节点挂到同一棵树的另一个文件夹。
func (s *Assets) MoveFSNode(ctx context.Context, token string, nodeID, parentID uuid.UUID) (FSNode, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FSNode{}, err
	}
	n, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	parent, err := s.store.FSNodeByID(ctx, parentID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.canWriteFS(ctx, acc, parent); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	row, err := s.store.MoveFSNode(ctx, nodeID, parentID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// MoveAssetInto 把已有资产的文件节点挂到指定文件夹。
func (s *Assets) MoveAssetInto(ctx context.Context, token string, assetID, parentID uuid.UUID) (FSNode, error) {
	file, err := s.store.FSFileByAsset(ctx, assetID)
	if err != nil {
		acc, accErr := s.RequireActive(ctx, token)
		if accErr == nil {
			_ = s.audit(ctx, &acc.ID, nil, "move_fs", assetID.String(), audit.Deny)
		}
		return FSNode{}, err
	}
	return s.MoveFSNode(ctx, token, file.ID, parentID)
}

// CopyFSNode 把文件夹或文件复制到可写目录；原件不动。
func (s *Assets) CopyFSNode(ctx context.Context, token string, nodeID, destID uuid.UUID) (FSNode, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FSNode{}, err
	}
	src, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	dest, err := s.store.FSNodeByID(ctx, destID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.canWriteFS(ctx, acc, dest); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := canCopyFSInto(src, dest); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if destInside(ctx, s.store, src.ID, dest.ID) {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrCycle
	}
	row, err := s.copyFSInto(ctx, token, src, dest)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "copy_fs", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// copyFSInto 递归复制到目标文件夹。
func (s *Assets) copyFSInto(ctx context.Context, token string, src, dest FSNode) (FSNode, error) {
	name, err := s.uniqueFSName(ctx, dest.ID, src.Name)
	if err != nil {
		return FSNode{}, err
	}
	if src.NodeKind == store.FSNodeFolder {
		folder, err := s.CreateFSFolder(ctx, token, dest.ID, name)
		if err != nil {
			return FSNode{}, err
		}
		kids, err := s.store.ListFSChildren(ctx, src.ID)
		if err != nil {
			return FSNode{}, err
		}
		for _, k := range kids {
			if _, err := s.copyFSInto(ctx, token, k, folder); err != nil {
				return FSNode{}, err
			}
		}
		return folder, nil
	}
	if src.AssetID == nil {
		return FSNode{}, domain.ErrNotFound
	}
	copied, err := s.CopyProcess(ctx, token, *src.AssetID, name)
	if err != nil {
		return FSNode{}, err
	}
	file, err := s.store.FSFileByAsset(ctx, copied.ID)
	if err != nil {
		return FSNode{}, err
	}
	if file.ParentID != nil && *file.ParentID == dest.ID {
		return file, nil
	}
	return s.MoveAssetInto(ctx, token, copied.ID, dest.ID)
}

// uniqueFSName 目标文件夹下不撞名；已占用则加「副本」。
func (s *Assets) uniqueFSName(ctx context.Context, parentID uuid.UUID, wanted string) (string, error) {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		wanted = "未命名"
	}
	kids, err := s.store.ListFSChildren(ctx, parentID)
	if err != nil {
		return "", err
	}
	taken := make(map[string]struct{}, len(kids))
	for _, k := range kids {
		taken[k.Name] = struct{}{}
	}
	cands := []string{wanted, wanted + " - 副本"}
	for i := 2; i <= 99; i++ {
		cands = append(cands, fmt.Sprintf("%s - 副本 (%d)", wanted, i))
	}
	for _, c := range cands {
		if utf8.RuneCountInString(c) > 64 {
			continue
		}
		if _, ok := taken[c]; !ok {
			return c, nil
		}
	}
	return "", domain.ErrDuplicateName
}

// canCopyFSInto 同树可粘贴；平台副本可粘进本厂厂级。
func canCopyFSInto(src, dest FSNode) error {
	if dest.NodeKind != store.FSNodeFolder || src.ParentID == nil {
		return domain.ErrForbidden
	}
	if dest.AssetKind != src.AssetKind {
		return domain.ErrForbidden
	}
	if sameFSTree(src, dest) {
		return nil
	}
	if src.TreeLevel == store.FSTreePlatform && dest.TreeLevel == store.FSTreeFactory {
		return nil
	}
	return domain.ErrForbidden
}

// sameFSTree 同一棵树：级别相同、个人树还要同一主人。
func sameFSTree(a, b FSNode) bool {
	return a.TreeLevel == b.TreeLevel && ownerEqPtr(a.OwnerID, b.OwnerID)
}

// ownerEqPtr 比较可选主人身份。
func ownerEqPtr(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// destInside 目标在源文件夹里面，粘贴会成环。
func destInside(ctx context.Context, st *Store, srcID, destID uuid.UUID) bool {
	cur := destID
	seen := map[uuid.UUID]struct{}{}
	for i := 0; i < 64; i++ {
		if cur == srcID {
			return true
		}
		if _, ok := seen[cur]; ok {
			return false
		}
		seen[cur] = struct{}{}
		n, err := st.FSNodeByID(ctx, cur)
		if err != nil || n.ParentID == nil {
			return false
		}
		cur = *n.ParentID
	}
	return false
}

// DeleteFSNode 删文件夹（可递归）或本厂原件文件；平台级副本不能从目录删。
func (s *Assets) DeleteFSNode(ctx context.Context, token string, nodeID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	n, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	if n.ParentID == nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	if n.TreeLevel == store.FSTreePlatform {
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	if n.NodeKind == store.FSNodeFile {
		if n.AssetID == nil {
			return domain.ErrNotFound
		}
		return s.DeleteAsset(ctx, token, *n.AssetID)
	}
	kids, err := s.store.ListFSChildren(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := s.DeleteFSNode(ctx, token, k.ID); err != nil {
			return err
		}
	}
	if err := s.store.DeleteFSNode(ctx, nodeID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Allow)
}

// isComputerAdmin 工厂超管或整厂组织管理员，可看见别人的个人树。
func (s *kernel) isComputerAdmin(ctx context.Context, acc Account) bool {
	return s.can(ctx, acc, permManageOrg, nil) == nil
}

// canSeeFSNode 平台/厂级人人可见；个人树仅主人或本厂管理员。
func canSeeFSNode(personID uuid.UUID, admin bool, n FSNode) bool {
	if n.TreeLevel != store.FSTreePersonal {
		return true
	}
	if n.OwnerID != nil && *n.OwnerID == personID {
		return true
	}
	return admin
}

// canWriteFS 平台副本树只读；厂级本厂可写；个人树主人或本厂管理员可写。
func (s *kernel) canWriteFS(ctx context.Context, acc Account, n FSNode) error {
	switch n.TreeLevel {
	case store.FSTreePlatform:
		return domain.ErrForbidden
	case store.FSTreeFactory:
		return s.canAuthorFactory(ctx, acc, nil)
	case store.FSTreePersonal:
		if n.OwnerID != nil && *n.OwnerID == acc.ID {
			return nil
		}
		return s.can(ctx, acc, permManageOrg, nil)
	default:
		return domain.ErrForbidden
	}
}

// ensureVisibleRoots 保证平台、厂级和当前人的个人根存在。
func (s *Assets) ensureVisibleRoots(ctx context.Context, acc Account, kind string) error {
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePlatform, nil); err != nil {
		return err
	}
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreeFactory, nil); err != nil {
		return err
	}
	oid := acc.ID
	_, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePersonal, &oid)
	return err
}

// decorateFS 补个人树主人名和文件资产元数据，不含正文。
func (s *Assets) decorateFS(ctx context.Context, nodes []FSNode) ([]FSNodeView, error) {
	ownerIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n.OwnerID != nil {
			ownerIDs = append(ownerIDs, *n.OwnerID)
		}
	}
	people, err := s.store.PeopleByIDs(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	out := make([]FSNodeView, 0, len(nodes))
	for _, n := range nodes {
		view := FSNodeView{FSNode: n}
		if n.OwnerID != nil {
			if p, ok := people[*n.OwnerID]; ok {
				view.OwnerName = p.DisplayName
				if view.OwnerName == "" {
					view.OwnerName = p.LoginName
				}
			}
		}
		if n.AssetID != nil {
			if a, err := s.loadAnyMeta(ctx, *n.AssetID); err == nil {
				stripped := stripContent(a)
				views, err := s.decorateAssets(ctx, []Asset{stripped})
				if err == nil && len(views) == 1 {
					cp := views[0]
					view.Asset = &cp
				}
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// placeCopyBeside 把副本文件放到源文件同一文件夹；失败不影响已建成的资产。
func (s *Assets) placeCopyBeside(ctx context.Context, srcID, newID uuid.UUID) {
	src, err := s.store.FSFileByAsset(ctx, srcID)
	if err != nil || src.ParentID == nil {
		return
	}
	file, err := s.store.FSFileByAsset(ctx, newID)
	if err != nil {
		return
	}
	if src.TreeLevel != file.TreeLevel {
		return
	}
	_, _ = s.store.MoveFSNode(ctx, file.ID, *src.ParentID)
}
