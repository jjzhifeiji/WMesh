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
	store.FSNode            // 嵌入的目录节点，不含正文
	OwnerName    string     `json:"ownerName,omitempty"` // 个人树主人显示名
	Asset        *AssetView `json:"asset,omitempty"`     // 文件才有；不含正文
}

// ListFS 列出当前人能看见的目录；同事看不到别人的个人树。
func (s *Assets) ListFS(ctx context.Context, token, kind string) ([]FSNodeView, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return nil, err
	}
	// 目录只分工艺和工程，别的种类拒绝。
	if kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 这一步失败则停下，避免带着残缺结果继续。
	if err := s.ensureVisibleRoots(ctx, acc, kind); err != nil {
		return nil, err
	}
	// 读出该种类的全部目录节点。
	nodes, err := s.store.ListFSNodes(ctx, kind)
	// 目录读失败则列表不返回。
	if err != nil {
		return nil, err
	}
	// 超管或整厂管理员能看见别人的个人树。
	admin := s.isComputerAdmin(ctx, acc)
	// 先按节点数准备可见列表，再按人筛。
	visible := make([]FSNode, 0, len(nodes))
	// 同事看不到别人的个人树，看不见的丢掉。
	for _, n := range nodes {
		// 别人的个人树看不见，这一条丢掉。
		if !canSeeFSNode(acc.ID, admin, n) {
			continue
		}
		// 当前人能看见的节点放进结果。
		visible = append(visible, n)
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	// 补主人名和文件元数据，不含正文。
	return s.decorateFS(ctx, visible)
}

// ListFSForChannel 给云端看本厂全部目录结构，不含正文。
func (s *Assets) ListFSForChannel(ctx context.Context, kind string) ([]FSNodeView, error) {
	// 通道只认工艺或工程目录，别的种类拒绝。
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 没指定种类时按工艺目录给云端看。
	if kind == "" {
		// 没指定种类时通道按工艺目录看。
		kind = KindProcess
	}
	// 根建不出来则目录列表失败。
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePlatform, nil); err != nil {
		return nil, err
	}
	// 根建不出来则目录列表失败。
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreeFactory, nil); err != nil {
		return nil, err
	}
	// 读出该种类的全部目录节点。
	nodes, err := s.store.ListFSNodes(ctx, kind)
	// 目录读失败则列表不返回。
	if err != nil {
		return nil, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, nil, nil, "list_fs", kind, audit.Allow); err != nil {
		return nil, err
	}
	// 补主人名和文件元数据，不含正文。
	return s.decorateFS(ctx, nodes)
}

// ApplyPlatformFSLayout 通道对齐云端平台目录；只动已到达的副本。
func (s *Assets) ApplyPlatformFSLayout(ctx context.Context, layout store.PlatformFSLayout) error {
	// 布局对齐失败则记拒绝。
	if err := s.store.ApplyPlatformFSLayout(ctx, layout); err != nil {
		// 目录对齐失败时记拒绝，布局保持原样。
		_ = s.audit(ctx, nil, nil, "apply_fs", layout.Kind, audit.Deny)
		return err
	}
	// 对齐成功要记审计，没记上则不算对齐完。
	return s.audit(ctx, nil, nil, "apply_fs", layout.Kind, audit.Allow)
}

// CreateFSFolder 在可写树里新建文件夹；平台级副本树只读。
func (s *Assets) CreateFSFolder(ctx context.Context, token string, parentID uuid.UUID, name string) (FSNode, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	parent, err := s.store.FSNodeByID(ctx, parentID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 建文件夹被拒时补记审计，写失败仍不建。
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 只能在文件夹下新建，文件下面拒绝。
	if parent.NodeKind != store.FSNodeFolder {
		// 建文件夹被拒时补记审计，写失败仍不建。
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, parent); err != nil {
		// 建文件夹被拒时补记审计，写失败仍不建。
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 在可写的父文件夹下新建文件夹。
	row, err := s.store.InsertFSFolder(ctx, parentID, name)
	// 新建失败则记拒绝。
	if err != nil {
		// 建文件夹被拒时补记审计，写失败仍不建。
		_ = s.audit(ctx, &acc.ID, nil, "create_fs_folder", name, audit.Deny)
		return FSNode{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "create_fs_folder", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// RenameFSNode 改文件夹名；文件改名走资产改名。
func (s *Assets) RenameFSNode(ctx context.Context, token string, nodeID uuid.UUID, name string) (FSNode, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	n, err := s.store.FSNodeByID(ctx, nodeID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 改名被拒时补记审计，写失败仍不改。
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 文件改名走资产，目录这里只改文件夹。
	if n.NodeKind != store.FSNodeFolder {
		// 改名被拒时补记审计，写失败仍不改。
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrForbidden
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		// 改名被拒时补记审计，写失败仍不改。
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 只改文件夹的名字，文件改名走资产。
	row, err := s.store.RenameFSNode(ctx, nodeID, name)
	// 改名失败则记拒绝。
	if err != nil {
		// 改名被拒时补记审计，写失败仍不改。
		_ = s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "rename_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// MoveFSNode 把节点挂到同一棵树的另一个文件夹。
func (s *Assets) MoveFSNode(ctx context.Context, token string, nodeID, parentID uuid.UUID) (FSNode, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	n, err := s.store.FSNodeByID(ctx, nodeID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 移动被拒时补记审计，位置保持不变。
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	parent, err := s.store.FSNodeByID(ctx, parentID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 移动被拒时补记审计，位置保持不变。
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		// 移动被拒时补记审计，位置保持不变。
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, parent); err != nil {
		// 移动被拒时补记审计，位置保持不变。
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 把节点挂到同一棵树的另一个文件夹。
	row, err := s.store.MoveFSNode(ctx, nodeID, parentID)
	// 移动失败则记拒绝，位置不变。
	if err != nil {
		// 移动被拒时补记审计，位置保持不变。
		_ = s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "move_fs", nodeID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// MoveAssetInto 把已有资产的文件节点挂到指定文件夹。
func (s *Assets) MoveAssetInto(ctx context.Context, token string, assetID, parentID uuid.UUID) (FSNode, error) {
	// 按资产找到它的文件节点。
	file, err := s.store.FSFileByAsset(ctx, assetID)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 核对登录仍有效，后面的操作都靠这次会话。
		acc, accErr := s.RequireActive(ctx, token)
		// 会话还在才补记拒绝，未登录就只返回错误。
		if accErr == nil {
			// 移动被拒时补记审计，位置保持不变。
			_ = s.audit(ctx, &acc.ID, nil, "move_fs", assetID.String(), audit.Deny)
		}
		return FSNode{}, err
	}
	// 把节点挂到同一棵树的另一个文件夹。
	return s.MoveFSNode(ctx, token, file.ID, parentID)
}

// CopyFSNode 把文件夹或文件复制到可写目录；原件不动。
func (s *Assets) CopyFSNode(ctx context.Context, token string, nodeID, destID uuid.UUID) (FSNode, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	src, err := s.store.FSNodeByID(ctx, nodeID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 读出目录节点，没有则不能改。
	dest, err := s.store.FSNodeByID(ctx, destID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, dest); err != nil {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 不能粘到这里则记拒绝，原件保持不动。
	if err := canCopyFSInto(src, dest); err != nil {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 目标在源里面会成环，拒绝粘贴。
	if destInside(ctx, s.store, src.ID, dest.ID) {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, domain.ErrCycle
	}
	// 递归复制到目标文件夹，原件不动。
	row, err := s.copyFSInto(ctx, token, src, dest)
	// 复制失败则记拒绝。
	if err != nil {
		// 复制被拒时补记审计，原件保持不动。
		_ = s.audit(ctx, &acc.ID, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "copy_fs", row.ID.String(), audit.Allow); err != nil {
		return FSNode{}, err
	}
	return row, nil
}

// copyFSInto 递归复制到目标文件夹。
func (s *Assets) copyFSInto(ctx context.Context, token string, src, dest FSNode) (FSNode, error) {
	// 目标下不撞名，占用了就加副本字样。
	name, err := s.uniqueFSName(ctx, dest.ID, src.Name)
	// 起名失败则这次复制停下。
	if err != nil {
		return FSNode{}, err
	}
	// 文件夹先建同名目录，再递归复制下级。
	if src.NodeKind == store.FSNodeFolder {
		// 在目标下建同名文件夹，再递归装下级。
		folder, err := s.CreateFSFolder(ctx, token, dest.ID, name)
		// 文件夹建失败则这次复制停下。
		if err != nil {
			return FSNode{}, err
		}
		// 读出下级，准备递归复制或删除。
		kids, err := s.store.ListFSChildren(ctx, src.ID)
		// 下级读失败则停在这里，不继续递归。
		if err != nil {
			return FSNode{}, err
		}
		// 文件夹的下级继续复制，一个失败就停下。
		for _, k := range kids {
			// 复制失败则记拒绝。
			if _, err := s.copyFSInto(ctx, token, k, folder); err != nil {
				return FSNode{}, err
			}
		}
		return folder, nil
	}
	// 文件节点没有资产则无法复制。
	if src.AssetID == nil {
		return FSNode{}, domain.ErrNotFound
	}
	// 文件按可复制工艺另存，再挂到目标目录。
	copied, err := s.CopyProcess(ctx, token, *src.AssetID, name)
	// 工艺复制失败则目录复制停下。
	if err != nil {
		return FSNode{}, err
	}
	// 按资产找到它的文件节点。
	file, err := s.store.FSFileByAsset(ctx, copied.ID)
	// 文件节点找不到则不能摆放或移动。
	if err != nil {
		return FSNode{}, err
	}
	// 新文件已经在目标下就不用再搬。
	if file.ParentID != nil && *file.ParentID == dest.ID {
		return file, nil
	}
	// 新文件若没落在目标下，再挂过去。
	return s.MoveAssetInto(ctx, token, copied.ID, dest.ID)
}

// uniqueFSName 目标文件夹下不撞名；已占用则加「副本」。
func (s *Assets) uniqueFSName(ctx context.Context, parentID uuid.UUID, wanted string) (string, error) {
	// 去掉两端空白，空的改成未命名。
	wanted = strings.TrimSpace(wanted)
	// 空名字不能建节点，先改成未命名。
	if wanted == "" {
		// 空名字不能建节点，改成未命名再避开重名。
		wanted = "未命名"
	}
	// 读出下级，准备递归复制或删除。
	kids, err := s.store.ListFSChildren(ctx, parentID)
	// 下级读失败则停在这里，不继续递归。
	if err != nil {
		return "", err
	}
	// 记下目标文件夹里已有的名字。
	taken := make(map[string]struct{}, len(kids))
	// 记下已占用的名字，后面避开它们。
	for _, k := range kids {
		// 这个名字已被占用，候选要避开。
		taken[k.Name] = struct{}{}
	}
	// 先试原名，再试带副本字样的名字。
	cands := []string{wanted, wanted + " - 副本"}
	// 名字被占就加序号，最多试到九十九。
	for i := 2; i <= 99; i++ {
		// 原名和一次副本都被占时，继续加序号。
		cands = append(cands, fmt.Sprintf("%s - 副本 (%d)", wanted, i))
	}
	// 挑一个不撞名且不超过长度的候选。
	for _, c := range cands {
		// 名字超长的候选丢掉，目录名有上限。
		if utf8.RuneCountInString(c) > 64 {
			continue
		}
		// 这个名字还没被占用，就用它。
		if _, ok := taken[c]; !ok {
			return c, nil
		}
	}
	return "", domain.ErrDuplicateName
}

// canCopyFSInto 同树可粘贴；平台副本可粘进本厂厂级。
func canCopyFSInto(src, dest FSNode) error {
	// 只能粘进文件夹，根节点本身不能当源。
	if dest.NodeKind != store.FSNodeFolder || src.ParentID == nil {
		return domain.ErrForbidden
	}
	// 工艺和工程不能贴到对方的树上。
	if dest.AssetKind != src.AssetKind {
		return domain.ErrForbidden
	}
	// 同一棵树可以直接粘贴。
	if sameFSTree(src, dest) {
		return nil
	}
	// 平台副本可以粘进本厂厂级，其它跨树拒绝。
	if src.TreeLevel == store.FSTreePlatform && dest.TreeLevel == store.FSTreeFactory {
		return nil
	}
	return domain.ErrForbidden
}

// sameFSTree 同一棵树：级别相同、个人树还要同一主人。
func sameFSTree(a, b FSNode) bool {
	// 同一棵个人树还要主人相同。
	return a.TreeLevel == b.TreeLevel && ownerEqPtr(a.OwnerID, b.OwnerID)
}

// ownerEqPtr 比较可选主人身份。
func ownerEqPtr(a, b *uuid.UUID) bool {
	// 两边都没有主人，算同一棵非个人树。
	if a == nil && b == nil {
		return true
	}
	// 只有一边有主人则不是同一棵树。
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// destInside 目标在源文件夹里面，粘贴会成环。
func destInside(ctx context.Context, st *Store, srcID, destID uuid.UUID) bool {
	// 从目标往上爬，看会不会爬进源文件夹。
	cur := destID
	// 记下已经数过或走过的身份，避免重复。
	seen := map[uuid.UUID]struct{}{}
	// 顺着父级往上走，走到源就说明会成环。
	for i := 0; i < 64; i++ {
		// 爬到源文件夹就说明目标在源里面，会成环。
		if cur == srcID {
			return true
		}
		// 父链绕回来了就当不会成环，避免死循环。
		if _, ok := seen[cur]; ok {
			return false
		}
		// 这个节点走过了，防止父链绕圈。
		seen[cur] = struct{}{}
		// 读出目录节点，没有则不能改。
		n, err := st.FSNodeByID(ctx, cur)
		// 节点读不到就当不会成环，避免误判。
		if err != nil || n.ParentID == nil {
			return false
		}
		// 继续看父级，直到根或碰到源。
		cur = *n.ParentID
	}
	return false
}

// DeleteFSNode 删文件夹（可递归）或本厂原件文件；平台级副本不能从目录删。
func (s *Assets) DeleteFSNode(ctx context.Context, token string, nodeID uuid.UUID) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 读出目录节点，没有则不能改。
	n, err := s.store.FSNodeByID(ctx, nodeID)
	// 节点读不到则记拒绝。
	if err != nil {
		// 删除被拒时补记审计，节点先留着。
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 根节点不能从目录里删掉。
	if n.ParentID == nil {
		// 删除被拒时补记审计，节点先留着。
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 平台副本不能从厂端目录删。
	if n.TreeLevel == store.FSTreePlatform {
		// 删除被拒时补记审计，节点先留着。
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 这棵树只读或不是主人则记拒绝。
	if err := s.canWriteFS(ctx, acc, n); err != nil {
		// 删除被拒时补记审计，节点先留着。
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 文件改走资产删除，仍被引用则拒绝。
	if n.NodeKind == store.FSNodeFile {
		// 文件节点没有资产则无法删除。
		if n.AssetID == nil {
			return domain.ErrNotFound
		}
		// 文件节点改走资产删除，仍被引用则拒绝。
		return s.DeleteAsset(ctx, token, *n.AssetID)
	}
	// 读出下级，准备递归复制或删除。
	kids, err := s.store.ListFSChildren(ctx, nodeID)
	// 下级读失败则停在这里，不继续递归。
	if err != nil {
		return err
	}
	// 先递归删下级，一个失败就停。
	for _, k := range kids {
		// 下级删失败则停，避免留下半截树。
		if err := s.DeleteFSNode(ctx, token, k.ID); err != nil {
			return err
		}
	}
	// 失败则记拒绝并停下，不继续往下改。
	if err := s.store.DeleteFSNode(ctx, nodeID); err != nil {
		// 删除被拒时补记审计，节点先留着。
		_ = s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Deny)
		return err
	}
	// 删掉之后记成功；审计失败则调用方当没删成。
	return s.audit(ctx, &acc.ID, nil, "delete_fs", nodeID.String(), audit.Allow)
}

// isComputerAdmin 工厂超管或整厂组织管理员，可看见别人的个人树。
func (s *kernel) isComputerAdmin(ctx context.Context, acc Account) bool {
	// 按本厂权限判断当前人能不能做。
	return s.can(ctx, acc, permManageOrg, nil) == nil
}

// canSeeFSNode 平台/厂级人人可见；个人树仅主人或本厂管理员。
func canSeeFSNode(personID uuid.UUID, admin bool, n FSNode) bool {
	// 平台和厂级目录人人可见。
	if n.TreeLevel != store.FSTreePersonal {
		return true
	}
	// 个人树只给主人看，管理员在下面另判。
	if n.OwnerID != nil && *n.OwnerID == personID {
		return true
	}
	return admin
}

// canWriteFS 平台副本树只读；厂级本厂可写；个人树主人或本厂管理员可写。
func (s *kernel) canWriteFS(ctx context.Context, acc Account, n FSNode) error {
	// 平台树只读，厂级本厂可写，个人树看主人或管理员。
	switch n.TreeLevel {
	// 平台副本树只读，厂端不能改。
	case store.FSTreePlatform:
		return domain.ErrForbidden
	// 厂级目录本厂有效账号可以改。
	case store.FSTreeFactory:
		// 确认当前人能在该处制作厂级。
		return s.canAuthorFactory(ctx, acc, nil)
	// 个人树只给主人或本厂管理员改。
	case store.FSTreePersonal:
		// 主人可以改自己的个人树。
		if n.OwnerID != nil && *n.OwnerID == acc.ID {
			return nil
		}
		// 按本厂权限判断当前人能不能做。
		return s.can(ctx, acc, permManageOrg, nil)
	// 不认识的树一律拒绝写入。
	default:
		return domain.ErrForbidden
	}
}

// ensureVisibleRoots 保证平台、厂级和当前人的个人根存在。
func (s *Assets) ensureVisibleRoots(ctx context.Context, acc Account, kind string) error {
	// 根建不出来则目录列表失败。
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePlatform, nil); err != nil {
		return err
	}
	// 根建不出来则目录列表失败。
	if _, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreeFactory, nil); err != nil {
		return err
	}
	// 个人根按当前人建，别人看不见。
	oid := acc.ID
	// 保证这棵根存在，没有就补上。
	_, err := s.store.EnsureFSRoot(ctx, kind, store.FSTreePersonal, &oid)
	return err
}

// decorateFS 补个人树主人名和文件资产元数据，不含正文。
func (s *Assets) decorateFS(ctx context.Context, nodes []FSNode) ([]FSNodeView, error) {
	// 先收集个人树主人，再一次把名字补上。
	ownerIDs := make([]uuid.UUID, 0, len(nodes))
	// 逐个节点补主人名和文件元数据。
	for _, n := range nodes {
		// 只有个人树带主人，有则去补名字。
		if n.OwnerID != nil {
			// 有主人的节点才去补名字。
			ownerIDs = append(ownerIDs, *n.OwnerID)
		}
	}
	// 补创建人或主人的名字，失败则列表不作数。
	people, err := s.store.PeopleByIDs(ctx, ownerIDs)
	// 名字补不上则整份列表失败。
	if err != nil {
		return nil, err
	}
	// 按节点数准备带名字的目录行。
	out := make([]FSNodeView, 0, len(nodes))
	// 逐个节点补主人名和文件元数据。
	for _, n := range nodes {
		// 先套上目录节点，名字和资产有则再补。
		view := FSNodeView{FSNode: n}
		// 只有个人树带主人，有则去补名字。
		if n.OwnerID != nil {
			// 查到主人就补显示名，没有就留空。
			if p, ok := people[*n.OwnerID]; ok {
				// 个人树显示主人的名字。
				view.OwnerName = p.DisplayName
				// 没有显示名就改用登录名，避免空白。
				if view.OwnerName == "" {
					// 没有显示名就用登录名，避免空白。
					view.OwnerName = p.LoginName
				}
			}
		}
		// 文件节点才去补资产元数据，文件夹不用。
		if n.AssetID != nil {
			// 元数据读得到才挂上，读不到就只留目录节点。
			if a, err := s.loadAnyMeta(ctx, *n.AssetID); err == nil {
				// 去掉正文再返回，避免工艺参数外泄。
				stripped := stripContent(a)
				// 给每行补上创建人名字，仍不含正文。
				views, err := s.decorateAssets(ctx, []Asset{stripped})
				// 只在恰好补到一行名字时挂上，失败就留空。
				if err == nil && len(views) == 1 {
					// 抄出一份再挂上，避免和列表共用底层。
					cp := views[0]
					// 文件节点带上资产元数据，不含正文。
					view.Asset = &cp
				}
			}
		}
		// 名字补完的一行放进列表。
		out = append(out, view)
	}
	return out, nil
}

// placeCopyBeside 把副本文件放到源文件同一文件夹；失败不影响已建成的资产。
func (s *Assets) placeCopyBeside(ctx context.Context, srcID, newID uuid.UUID) {
	// 按资产找到它的文件节点。
	src, err := s.store.FSFileByAsset(ctx, srcID)
	// 旁边摆不上就停，不影响已经建成的资产。
	if err != nil || src.ParentID == nil {
		return
	}
	// 按资产找到它的文件节点。
	file, err := s.store.FSFileByAsset(ctx, newID)
	// 旁边摆不上就停，不影响已经建成的资产。
	if err != nil {
		return
	}
	// 不在同一棵树就不动位置，避免把个人文件搬到厂级。
	if src.TreeLevel != file.TreeLevel {
		return
	}
	// 把节点挂到同一棵树的另一个文件夹。
	_, _ = s.store.MoveFSNode(ctx, file.ID, *src.ParentID)
}
