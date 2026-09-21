package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// FSNodeView 是目录节点加可选资产元数据，不含正文。
type FSNodeView struct {
	store.FSNode
	OwnerName string `json:"ownerName,omitempty"` // 厂端个人树主人；平台树为空
	Asset     *Asset `json:"asset,omitempty"`     // 文件才有；不含正文
}

// ListFS 列出该种类平台目录；没有根则建。
func (s *Assets) ListFS(ctx context.Context, token, kind string) ([]FSNodeView, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return nil, err
	}
	if kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	if _, err := s.store.EnsureFSRoot(ctx, kind); err != nil {
		return nil, err
	}
	nodes, err := s.store.ListFSNodes(ctx, kind)
	if err != nil {
		return nil, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	return s.decorateFS(ctx, nodes)
}

// CreateFSFolder 在父文件夹下新建文件夹。
func (s *Assets) CreateFSFolder(ctx context.Context, token string, parentID uuid.UUID, name string) (FSNode, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	parent, err := s.store.FSNodeByID(ctx, parentID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	if parent.NodeKind != store.FSNodeFolder {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	row, err := s.store.InsertFSFolder(ctx, parentID, name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// RenameFSNode 改文件夹名；文件改名走资产改名。
func (s *Assets) RenameFSNode(ctx context.Context, token string, nodeID uuid.UUID, name string) (FSNode, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	n, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if n.NodeKind != store.FSNodeFolder {
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	row, err := s.store.RenameFSNode(ctx, nodeID, name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	s.notifyPlatformFS(ctx, row.AssetKind)
	return row, nil
}

// MoveFSNode 把节点挂到另一个文件夹。
func (s *Assets) MoveFSNode(ctx context.Context, token string, nodeID, parentID uuid.UUID) (FSNode, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	row, err := s.store.MoveFSNode(ctx, nodeID, parentID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "move_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	s.notifyPlatformFS(ctx, row.AssetKind)
	return row, nil
}

// MoveAssetInto 把已有资产的文件节点挂到指定文件夹。
func (s *Assets) MoveAssetInto(ctx context.Context, token string, assetID, parentID uuid.UUID) (FSNode, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return FSNode{}, err
	}
	file, err := s.store.FSFileByAsset(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "move_fs", assetID.String(), audit.Deny)
		return FSNode{}, err
	}
	return s.MoveFSNode(ctx, token, file.ID, parentID)
}

// CopyFSNode 把文件夹或文件复制到目录；原件不动。
func (s *Assets) CopyFSNode(ctx context.Context, token string, nodeID, destID uuid.UUID) (FSNode, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	src, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	dest, err := s.store.FSNodeByID(ctx, destID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if dest.NodeKind != store.FSNodeFolder || src.ParentID == nil || dest.AssetKind != src.AssetKind {
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	if destInside(ctx, s.store, src.ID, dest.ID) {
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrCycle
	}
	row, err := s.copyFSInto(ctx, token, src, dest)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "copy_fs", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	s.notifyPlatformFS(ctx, row.AssetKind)
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
	copied, err := s.CopyPlatformProcess(ctx, token, *src.AssetID, name)
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

// DeleteFSNode 删文件夹（可递归）或文件（走删资产）。
func (s *Assets) DeleteFSNode(ctx context.Context, token string, nodeID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	n, err := s.store.FSNodeByID(ctx, nodeID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	if n.ParentID == nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	if n.NodeKind == store.FSNodeFile {
		if n.AssetID == nil {
			return domain.ErrNotFound
		}
		return s.DeletePlatformAsset(ctx, token, *n.AssetID)
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
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Allow); err != nil {
		return err
	}
	s.notifyPlatformFS(ctx, n.AssetKind)
	return nil
}

// decorateFS 给文件节点补资产元数据，仍不含正文。
func (s *Assets) decorateFS(ctx context.Context, nodes []FSNode) ([]FSNodeView, error) {
	assets, err := s.store.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]Asset, len(assets))
	for _, a := range assets {
		byID[a.ID] = stripContent(a)
	}
	out := make([]FSNodeView, 0, len(nodes))
	for _, n := range nodes {
		view := FSNodeView{FSNode: n}
		if n.AssetID != nil {
			if a, ok := byID[*n.AssetID]; ok {
				cp := a
				view.Asset = &cp
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
	_, _ = s.store.MoveFSNode(ctx, file.ID, *src.ParentID)
}
