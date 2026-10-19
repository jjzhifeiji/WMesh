package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// FactoryFSNode 是经通道拉到的厂目录一行，只认结构和文件元数据，不认人员字段。
type FactoryFSNode struct {
	ID        uuid.UUID       `json:"id"`                // 厂端目录节点身份
	Name      string          `json:"name"`              // 显示名；根节点为空
	ParentID  *uuid.UUID      `json:"parentId"`          // 父文件夹；空表示根
	NodeKind  string          `json:"nodeKind"`          // folder 或 file
	AssetKind string          `json:"assetKind"`         // process 或 project
	TreeLevel string          `json:"treeLevel"`         // platform / factory / personal
	AssetID   *uuid.UUID      `json:"assetId,omitempty"` // 文件才有的资产身份
	Asset     *FactoryFSAsset `json:"asset,omitempty"`   // 文件才有；不含正文和创建人
}

// FactoryFSAsset 是厂目录文件挂的资产元数据，和升档清单同一份字段。
type FactoryFSAsset struct {
	ID       uuid.UUID `json:"id"`       // 厂端资产稳定身份
	Kind     string    `json:"kind"`     // process 或 project
	Level    string    `json:"level"`    // factory / personal / platform
	Name     string    `json:"name"`     // 资产显示名
	Code     string    `json:"code"`     // 只读编号，创建后不改
	Revision int64     `json:"revision"` // 厂端当前修订号
	Digest   []byte    `json:"digest"`   // 内容摘要，用来比对是否最新
	Status   string    `json:"status"`   // draft / available / disabled
	Copyable bool      `json:"copyable"` // 工艺可否复制；工程恒为是
	WeldKind string    `json:"weldKind"` // 作业类型
}

// DecodeFactoryFS 按白名单解开厂端目录回包，旧厂端多带的人员字段一律丢掉。
func DecodeFactoryFS(raw []byte) ([]FactoryFSNode, error) {
	// 先备空数组，厂没回目录时照样回空列表。
	rows := []FactoryFSNode{}
	// 厂没回目录就给空列表，不当成故障。
	if len(raw) == 0 {
		return rows, nil
	}
	// 只解白名单字段，主人和创建人等多余字段自然落空。
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	// 厂回了 null 时也给空数组，免得浏览器拿到 null。
	if rows == nil {
		// 厂回 null 改成空数组，前端按列表处理。
		rows = []FactoryFSNode{}
	}
	return rows, nil
}

// ListFS 列出该种类平台目录；没有根则建。
func (s *Assets) ListFS(ctx context.Context, token, kind string) ([]FSNodeView, error) {
	// 核对当前管理员会话，只读轮询用不到管理员身份。
	_, err := s.RequireAdmin(ctx, token)
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
	// 补上给人看的名字或元数据；目录页轮询读，成功不记允许。
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
	// 先只读走一遍整棵子树，有一件不能另存就整体拒绝，什么都不建。
	if err := s.checkFSCopyable(ctx, src); err != nil {
		// 复制目录被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "copy_fs", nodeID.String(), audit.Deny)
		return FSNode{}, err
	}
	// 复制一份，原件保持不动。
	row, err := s.copyFSInto(ctx, token, src, dest)
	// 复制失败就回收本次新建的顶层文件夹，再记拒绝。
	if err != nil {
		// 已建出顶层文件夹才回收，尽量不留半截。
		if row.ID != uuid.Nil {
			// 回收失败也返回原来的错误，不盖住真因。
			_ = s.deleteFSTree(ctx, token, admin.ID, row)
		}
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

// checkFSCopyable 只读递归查子树里每个文件：缺资产、摘要不符、种类不对或已停用都算不过。
func (s *Assets) checkFSCopyable(ctx context.Context, n FSNode) error {
	// 文件夹本身不用查，逐个看下级。
	if n.NodeKind == store.FSNodeFolder {
		// 列出直接孩子，逐个往下查。
		kids, err := s.store.ListFSChildren(ctx, n.ID)
		// 列出失败就拒绝，查不全不能放行复制。
		if err != nil {
			return err
		}
		// 逐个子项预检，一个不过就整体拒绝。
		for _, k := range kids {
			// 这一支有不能另存的就停，什么都不建。
			if err := s.checkFSCopyable(ctx, k); err != nil {
				return err
			}
		}
		return nil
	}
	// 文件节点缺资产就算不过，和单件复制同一口径。
	if n.AssetID == nil {
		return domain.ErrNotFound
	}
	// 装入并核对摘要，对不上不能另存。
	a, err := s.loadChecked(ctx, *n.AssetID)
	// 读不到或摘要不符就整体拒绝。
	if err != nil {
		return err
	}
	// 和单件另存同一口径，免得两边松紧不一。
	return canCopyPlatform(a)
}

// copyFSInto 递归复制到目标文件夹；出错时交回已建的文件夹，由调用方回收。
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
		// 列出失败就交回已建的文件夹，好让调用方删掉。
		if err != nil {
			return folder, err
		}
		// 逐个子项往下复制，一个失败就停。
		for _, k := range kids {
			// 下级失败则交回这层文件夹，连同已复制的一起回收。
			if _, err := s.copyFSInto(ctx, token, k, folder); err != nil {
				return folder, err
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
	// 文件节点找不到就删掉刚建的副本，再停下。
	if err != nil {
		// 副本可能不在目标下，这里自己回收，失败不盖住真因。
		_ = s.DeletePlatformAsset(ctx, token, copied.ID)
		return FSNode{}, err
	}
	// 没有父节点或已经在目标下，就不用再挪。
	if file.ParentID != nil && *file.ParentID == dest.ID {
		return file, nil
	}
	// 挪到新位置，失败就不能继续。
	moved, err := s.MoveAssetInto(ctx, token, copied.ID, dest.ID)
	// 挂不过去就删掉留在原件旁的副本，免得重试一次多一份。
	if err != nil {
		// 回收失败也返回原来的错误，不盖住真因。
		_ = s.DeletePlatformAsset(ctx, token, copied.ID)
		return FSNode{}, err
	}
	return moved, nil
}

// uniqueFSName 目标文件夹下不撞名；已占用则加「副本」，原名过长先截短再接。
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
	// 先试原名和副本，再试带序号的副本尾巴。
	tails := []string{"", " - 副本"}
	// 准备带序号的副本尾巴，避免文件夹重名。
	for i := 2; i <= 99; i++ {
		// 把这一项接进结果。
		tails = append(tails, fmt.Sprintf(" - 副本 (%d)", i))
	}
	// 按候选名依次试，撞名就换下一个。
	for _, tail := range tails {
		// 原名过长就先截短再接尾巴，不再整条跳过。
		c := clipAssetName(wanted, tail)
		// 这个名字还没占用，可以拿来当文件夹名。
		if _, ok := taken[c]; !ok {
			return c, nil
		}
	}
	// 一百个候选真的都被占了，才按同名拒绝。
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

// DeleteFSNode 删文件夹（先整体预检再递归）或文件（走删资产）。
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
	// 删文件夹先整体预检，有一个删不了就一个都不删。
	if n.NodeKind != store.FSNodeFile {
		// 预检不过就拒绝，免得前面的工艺已删已收回。
		if err := s.checkFSDeletable(ctx, nodeID); err != nil {
			// 删目录被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "delete_fs", nodeID.String(), audit.Deny)
			return err
		}
	}
	// 预检已过，按原顺序递归删掉这一支。
	return s.deleteFSTree(ctx, token, admin.ID, n)
}

// checkFSDeletable 递归查文件夹下每个文件：缺资产、摘要不符或仍被引用都算不过。
func (s *Assets) checkFSDeletable(ctx context.Context, folderID uuid.UUID) error {
	// 列出直接孩子，逐个往下查。
	kids, err := s.store.ListFSChildren(ctx, folderID)
	// 列出失败就拒绝，查不全不能放行删除。
	if err != nil {
		return err
	}
	// 逐个子项预检，一个不过就整体拒绝。
	for _, k := range kids {
		// 子文件夹往下递归，里面的文件同样要查。
		if k.NodeKind != store.FSNodeFile {
			// 下层有一个不过就停，整体都不删。
			if err := s.checkFSDeletable(ctx, k.ID); err != nil {
				return err
			}
			// 文件夹本身没有资产，查完下层就看下一个。
			continue
		}
		// 文件节点缺资产就算不过，和单删文件同一口径。
		if k.AssetID == nil {
			return domain.ErrNotFound
		}
		// 装入并核对摘要，对不上不能删。
		if _, err := s.loadChecked(ctx, *k.AssetID); err != nil {
			return err
		}
		// 查是否仍被工程依赖。
		used, err := s.store.AssetIsReferenced(ctx, *k.AssetID)
		// 查不出来就拒绝，不能当没被引用。
		if err != nil {
			return err
		}
		// 仍被工程依赖就整体拒绝。
		if used {
			return domain.ErrReferenced
		}
	}
	return nil
}

// deleteFSTree 递归删节点：文件走删资产，文件夹删空孩子后删自己并记审计。
func (s *Assets) deleteFSTree(ctx context.Context, token string, adminID uuid.UUID, n FSNode) error {
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
	kids, err := s.store.ListFSChildren(ctx, n.ID)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return err
	}
	// 逐个子项往下删，一个失败就停。
	for _, k := range kids {
		// 删除失败就停，避免库里留下残行。
		if err := s.deleteFSTree(ctx, token, adminID, k); err != nil {
			return err
		}
	}
	// 删除失败就停，避免库里留下残行。
	if err := s.store.DeleteFSNode(ctx, n.ID); err != nil {
		// 删目录被拒就留审计。
		_ = s.audit(ctx, &adminID, nil, nil, "delete_fs", n.ID.String(), audit.Deny)
		return err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &adminID, nil, nil, "delete_fs", n.ID.String(), audit.Allow); err != nil {
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
