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
	store.FSNode        // 目录节点本体，不含正文。
	OwnerName    string `json:"ownerName,omitempty"` // 厂端个人树主人；平台树为空
	Asset        *Asset `json:"asset,omitempty"`     // 文件才有；不含正文
}

// ListFS 列出该种类平台目录；没有根则建。
func (s *Assets) ListFS(ctx context.Context, token, kind string) ([]FSNodeView, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return nil, err
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 建不好就停，避免后面指到空的。
	if _, err := s.store.EnsureFSRoot(ctx, kind); err != nil {
		return nil, err
	}
	// 列出这一批供后面筛选。
	nodes, err := s.store.ListFSNodes(ctx, kind)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	// 补上给人看的名字或元数据。
	return s.decorateFS(ctx, nodes)
}

// CreateFSFolder 在父文件夹下新建文件夹。
func (s *Assets) CreateFSFolder(ctx context.Context, token string, parentID uuid.UUID, name string) (FSNode, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 建文件夹被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 按目录节点处理。
	parent, err := s.store.FSNodeByID(ctx, parentID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 建文件夹被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 不是文件夹就不能当父节点，文件要另走。
	if parent.NodeKind != store.FSNodeFolder {
		// 建文件夹被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertFSFolder(ctx, parentID, name)
	// 写入失败就停，避免留下半截。
	if err != nil {
		// 建文件夹被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_fs_folder", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// RenameFSNode 改文件夹名；文件改名走资产改名。
func (s *Assets) RenameFSNode(ctx context.Context, token string, nodeID uuid.UUID, name string) (FSNode, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改目录名被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 按目录节点处理。
	n, err := s.store.FSNodeByID(ctx, nodeID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 改目录名被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 不是文件夹就不能当父节点，文件要另走。
	if n.NodeKind != store.FSNodeFolder {
		// 改目录名被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	// 改显示名，身份保持不变。
	row, err := s.store.RenameFSNode(ctx, nodeID, name)
	// 改名失败就停，避免名字和身份错位。
	if err != nil {
		// 改目录名被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "rename_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	// 通知厂端对齐平台目录。
	s.notifyPlatformFS(ctx, row.AssetKind)
	return row, nil
}

// MoveFSNode 把节点挂到另一个文件夹。
func (s *Assets) MoveFSNode(ctx context.Context, token string, nodeID, parentID uuid.UUID) (FSNode, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 移动被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 挪到新位置，失败就不能继续。
	row, err := s.store.MoveFSNode(ctx, nodeID, parentID)
	// 会成环或目标不对就拒绝。
	if err != nil {
		// 移动被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "move_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	// 通知厂端对齐平台目录。
	s.notifyPlatformFS(ctx, row.AssetKind)
	return row, nil
}

// MoveAssetInto 把已有资产的文件节点挂到指定文件夹。
func (s *Assets) MoveAssetInto(ctx context.Context, token string, assetID, parentID uuid.UUID) (FSNode, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return FSNode{}, err
	}
	// 按目录节点处理。
	file, err := s.store.FSFileByAsset(ctx, assetID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 移动被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "move_fs", assetID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 挪到新位置，失败就不能继续。
	return s.MoveFSNode(ctx, token, file.ID, parentID)
}

// CopyFSNode 把文件夹或文件复制到目录；原件不动。
func (s *Assets) CopyFSNode(ctx context.Context, token string, nodeID, destID uuid.UUID) (FSNode, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 按目录节点处理。
	src, err := s.store.FSNodeByID(ctx, nodeID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 按目录节点处理。
	dest, err := s.store.FSNodeByID(ctx, destID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 没有父节点或已经在目标下，就不用再挪。
	if dest.NodeKind != store.FSNodeFolder || src.ParentID == nil || dest.AssetKind != src.AssetKind {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	// 目标在源里面会成环，粘贴必须拒绝。
	if destInside(ctx, s.store, src.ID, dest.ID) {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrCycle
	}
	// 复制一份，原件保持不动。
	row, err := s.copyFSInto(ctx, token, src, dest)
	// 复制失败就停，避免两处缠在一起。
	if err != nil {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "copy_fs", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	// 通知厂端对齐平台目录。
	s.notifyPlatformFS(ctx, row.AssetKind)
	return row, nil
}

// copyFSInto 递归复制到目标文件夹。
func (s *Assets) copyFSInto(ctx context.Context, token string, src, dest FSNode) (FSNode, error) {
	// 找一个还不冲突的名字。
	name, err := s.uniqueFSName(ctx, dest.ID, src.Name)
	// 找不到就拒绝，不能盖掉已有的。
	if err != nil {
		return FSNode{}, err
	}
	// 不是文件夹就不能当父节点，文件要另走。
	if src.NodeKind == store.FSNodeFolder {
		// 新建这一条，已有或没资格则不行。
		folder, err := s.CreateFSFolder(ctx, token, dest.ID, name)
		// 新建失败就停，避免留下半截记录。
		if err != nil {
			return FSNode{}, err
		}
		// 列出这一批供后面筛选。
		kids, err := s.store.ListFSChildren(ctx, src.ID)
		// 列出失败就拒绝，避免交出不完整结果。
		if err != nil {
			return FSNode{}, err
		}
		// 记下每个子项的名字，用来避开重名。
		for _, k := range kids {
			// 复制失败就停，避免两处缠在一起。
			if _, err := s.copyFSInto(ctx, token, k, folder); err != nil {
				return FSNode{}, err
			}
		}
		return folder, nil
	}
	// 还没有准备好就停，避免空着往下用。
	if src.AssetID == nil {
		return FSNode{}, domain.ErrNotFound
	}
	// 复制一份，原件保持不动。
	copied, err := s.CopyPlatformProcess(ctx, token, *src.AssetID, name)
	// 复制失败就停，避免留下半份。
	if err != nil {
		return FSNode{}, err
	}
	// 按目录节点处理。
	file, err := s.store.FSFileByAsset(ctx, copied.ID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		return FSNode{}, err
	}
	// 没有父节点或已经在目标下，就不用再挪。
	if file.ParentID != nil && *file.ParentID == dest.ID {
		return file, nil
	}
	// 挪到新位置，失败就不能继续。
	return s.MoveAssetInto(ctx, token, copied.ID, dest.ID)
}

// uniqueFSName 目标文件夹下不撞名；已占用则加「副本」。
func (s *Assets) uniqueFSName(ctx context.Context, parentID uuid.UUID, wanted string) (string, error) {
	// 去掉多余空白或前后缀。
	wanted = strings.TrimSpace(wanted)
	// 空和有值走不同路，避免把空白写进名录。
	if wanted == "" {
		// 没给名字就用占位，避免文件夹没标题。
		wanted = "未命名"
	}
	// 列出这一批供后面筛选。
	kids, err := s.store.ListFSChildren(ctx, parentID)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return "", err
	}
	// 按数量先准备容器。
	taken := make(map[string]struct{}, len(kids))
	// 记下每个子项的名字，用来避开重名。
	for _, k := range kids {
		// 记下已占用的名字，后面避开它们。
		taken[k.Name] = struct{}{}
	}
	// 先试原名和副本，再试带序号的名字。
	cands := []string{wanted, wanted + " - 副本"}
	// 准备带序号的副本名，避免文件夹重名。
	for i := 2; i <= 99; i++ {
		// 把这一项接进结果。
		cands = append(cands, fmt.Sprintf("%s - 副本 (%d)", wanted, i))
	}
	// 按候选名依次试，撞名就换下一个。
	for _, c := range cands {
		// 字数不在允许范围就拒绝，避免空名或超长。
		if utf8.RuneCountInString(c) > 64 {
			continue
		}
		// 这个名字还没占用，可以拿来当文件夹名。
		if _, ok := taken[c]; !ok {
			return c, nil
		}
	}
	return "", domain.ErrDuplicateName
}

// destInside 目标在源文件夹里面，粘贴会成环。
func destInside(ctx context.Context, st *Store, srcID, destID uuid.UUID) bool {
	// 从目标往父级走，看会不会走进源里面。
	cur := destID
	// 用来挡住同一键被写两次。
	seen := map[uuid.UUID]struct{}{}
	// 沿父链向上找，超过层数就当不在里面。
	for i := 0; i < 64; i++ {
		// 条件不满足则拒绝，避免把错状态写进去。
		if cur == srcID {
			return true
		}
		// 同一键已经见过则拒绝，防止写两遍。
		if _, ok := seen[cur]; ok {
			return false
		}
		// 记下走过的节点，父链成环就停。
		seen[cur] = struct{}{}
		// 按目录节点处理。
		n, err := st.FSNodeByID(ctx, cur)
		// 没有父节点或已经在目标下，就不用再挪。
		if err != nil || n.ParentID == nil {
			return false
		}
		// 再往上一层，直到根或发现成环。
		cur = *n.ParentID
	}
	return false
}

// DeleteFSNode 删文件夹（可递归）或文件（走删资产）。
func (s *Assets) DeleteFSNode(ctx context.Context, token string, nodeID uuid.UUID) error {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 删目录被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 按目录节点处理。
	n, err := s.store.FSNodeByID(ctx, nodeID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		// 删目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 没有父节点或已经在目标下，就不用再挪。
	if n.ParentID == nil {
		// 删目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 不是文件夹就不能当父节点，文件要另走。
	if n.NodeKind == store.FSNodeFile {
		// 还没有准备好就停，避免空着往下用。
		if n.AssetID == nil {
			return domain.ErrNotFound
		}
		// 删掉这一条，仍被引用则不行。
		return s.DeletePlatformAsset(ctx, token, *n.AssetID)
	}
	// 列出这一批供后面筛选。
	kids, err := s.store.ListFSChildren(ctx, nodeID)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return err
	}
	// 记下每个子项的名字，用来避开重名。
	for _, k := range kids {
		// 删除失败就停，避免库里留下残行。
		if err := s.DeleteFSNode(ctx, token, k.ID); err != nil {
			return err
		}
	}
	// 删除失败就停，避免库里留下残行。
	if err := s.store.DeleteFSNode(ctx, nodeID); err != nil {
		// 删目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Allow); err != nil {
		return err
	}
	// 通知厂端对齐平台目录。
	s.notifyPlatformFS(ctx, n.AssetKind)
	return nil
}

// decorateFS 给文件节点补资产元数据，仍不含正文。
func (s *Assets) decorateFS(ctx context.Context, nodes []FSNode) ([]FSNodeView, error) {
	// 列出这一批供后面筛选。
	assets, err := s.store.ListAssets(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 按数量先准备容器。
	byID := make(map[uuid.UUID]Asset, len(assets))
	// 逐条处理资产，一条失败就停。
	for _, a := range assets {
		// 去掉正文再返回。
		byID[a.ID] = stripContent(a)
	}
	// 按数量先准备容器。
	out := make([]FSNodeView, 0, len(nodes))
	// 逐个目录节点装饰，文件才带上资产。
	for _, n := range nodes {
		// 先装上节点本身，文件再补资产元数据。
		view := FSNodeView{FSNode: n}
		// 还没有准备好就停，避免空着往下用。
		if n.AssetID != nil {
			// 对不上就跳过或拒绝，避免用错那一条。
			if a, ok := byID[*n.AssetID]; ok {
				// 复制一份资产，去掉正文时不改原件。
				cp := a
				// 文件节点带上元数据，仍然不含正文。
				view.Asset = &cp
			}
		}
		// 把这一项接进结果。
		out = append(out, view)
	}
	return out, nil
}

// placeCopyBeside 把副本文件放到源文件同一文件夹；失败不影响已建成的资产。
func (s *Assets) placeCopyBeside(ctx context.Context, srcID, newID uuid.UUID) {
	// 按目录节点处理。
	src, err := s.store.FSFileByAsset(ctx, srcID)
	// 没有父节点或已经在目标下，就不用再挪。
	if err != nil || src.ParentID == nil {
		return
	}
	// 按目录节点处理。
	file, err := s.store.FSFileByAsset(ctx, newID)
	// 没有这个节点或会成环就拒绝。
	if err != nil {
		return
	}
	// 挪到新位置，失败就不能继续。
	_, _ = s.store.MoveFSNode(ctx, file.ID, *src.ParentID)
}
