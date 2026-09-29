package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

const (
	FSNodeFolder = "folder" // 容器，不引用正文
	FSNodeFile   = "file"   // 引用一条资产或副本

	FSTreePlatform = "platform" // 已下发平台级副本
	FSTreeFactory  = "factory"  // 本厂公用
	FSTreePersonal = "personal" // 某人的用户目录
)

const fsNameMax = 64 // 与显示名错误文案对齐

// FSNode 是目录树上的一条：文件夹或文件引用。
type FSNode struct {
	ID        uuid.UUID  `json:"id"`                // 节点身份
	Name      string     `json:"name"`              // 显示名；根为空
	ParentID  *uuid.UUID `json:"parentId"`          // 空表示根
	NodeKind  string     `json:"nodeKind"`          // folder / file
	AssetKind string     `json:"assetKind"`         // process / project
	TreeLevel string     `json:"treeLevel"`         // platform / factory / personal
	OwnerID   *uuid.UUID `json:"ownerId,omitempty"` // 个人树主人
	AssetID   *uuid.UUID `json:"assetId,omitempty"` // 文件才有
	CreatedAt time.Time  `json:"createdAt"`         // 创建时间
	UpdatedAt time.Time  `json:"updatedAt"`         // 最近改名或搬家
}

// FSFolderHint 是云端平台文件夹的一层，用来在本厂对齐路径。
type FSFolderHint struct {
	ID       uuid.UUID  `json:"id"`                 // 与 WAN 文件夹同一身份
	Name     string     `json:"name"`               // 显示名
	ParentID *uuid.UUID `json:"parentId,omitempty"` // 空表示挂在本厂平台根下
}

// FSFileHint 是已下发文件应挂到哪个文件夹。
type FSFileHint struct {
	AssetID  uuid.UUID  `json:"assetId"`            // 平台级资产身份
	ParentID *uuid.UUID `json:"parentId,omitempty"` // 空表示挂在平台根
}

// PlatformFSLayout 是云端推来的平台目录应对齐结果。
type PlatformFSLayout struct {
	Kind    string         `json:"kind"`    // process / project
	Folders []FSFolderHint `json:"folders"` // 已有文件的祖先文件夹，不含根
	Files   []FSFileHint   `json:"files"`   // 文件挂点
}

// 资产目录节点的落库行。
type fsNodeRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 节点身份
	Name      string     `gorm:"not null"`             // 显示名；根为空
	ParentID  *uuid.UUID `gorm:"type:uuid"`            // 空表示根
	NodeKind  string     `gorm:"not null"`             // folder / file
	AssetKind string     `gorm:"not null"`             // process / project
	TreeLevel string     `gorm:"not null"`             // platform / factory / personal
	OwnerID   *uuid.UUID `gorm:"type:uuid"`            // 个人树主人
	AssetID   *uuid.UUID `gorm:"type:uuid"`            // 文件才有
	CreatedAt time.Time  `gorm:"not null"`             // 创建时间
	UpdatedAt time.Time  `gorm:"not null"`             // 最近改名或搬家
}

// 指定落库表名，避免查询时按类型名去猜。
func (fsNodeRow) TableName() string { return "asset_fs_nodes" }

// fsFromRow 把库行收成对外节点。
func fsFromRow(r fsNodeRow) FSNode {
	return FSNode{
		ID: r.ID, Name: r.Name, ParentID: r.ParentID, NodeKind: r.NodeKind,
		AssetKind: r.AssetKind, TreeLevel: r.TreeLevel, OwnerID: r.OwnerID,
		AssetID: r.AssetID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// mapFSErr 把唯一冲突收成同名错误。
func mapFSErr(err error) error {
	// 唯一约束撞了就改成业务冲突，不抛库原文。
	if domain.IsUniqueViolation(err) {
		return domain.ErrDuplicateName
	}
	// 外键对不上就当被指的对象不存在。
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// checkFSName 文件夹名不能空、不能带路径分隔符。
func checkFSName(name string) (string, error) {
	// 去掉首尾空白，纯空白不当有效内容。
	name = strings.TrimSpace(name)
	// 超过字数上限就截断，避免超出列宽。
	if name == "" || utf8.RuneCountInString(name) > fsNameMax || strings.ContainsAny(name, "/\\") {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// ownerPtr 个人树记下主人；其余树必须空。
func ownerPtr(treeLevel string, creator uuid.UUID) *uuid.UUID {
	// 厂级和平台树不记主人，避免串进个人目录。
	if treeLevel != FSTreePersonal {
		return nil
	}
	// 先接调用方给的身份，空的再另发。
	id := creator
	return &id
}

// EnsureFSRoot 取出该树的根；没有则建。
func (s *Store) EnsureFSRoot(ctx context.Context, assetKind, treeLevel string, ownerID *uuid.UUID) (FSNode, error) {
	// 只认工艺和工程，别的种类拒绝。
	if assetKind != KindProcess && assetKind != KindProject {
		return FSNode{}, domain.ErrNotFound
	}
	// 厂级和平台树不记主人，避免串进个人目录。
	if treeLevel != FSTreePlatform && treeLevel != FSTreeFactory && treeLevel != FSTreePersonal {
		return FSNode{}, domain.ErrNotFound
	}
	// 个人树必须有主人，否则会和别人混在一起。
	if treeLevel == FSTreePersonal && (ownerID == nil || *ownerID == uuid.Nil) {
		return FSNode{}, domain.ErrNotFound
	}
	// 厂级和平台树不记主人，避免串进个人目录。
	if treeLevel != FSTreePersonal {
		// 这里不记这个值，避免串数据或留下空钥。
		ownerID = nil
	}
	// 准备承接查到的目录节点。
	var out FSNode
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先确保目录根，结果留给紧跟着的判断。
		n, err := ensureFSRootTx(tx, assetKind, treeLevel, ownerID)
		// 确保目录根失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 把查到的结果交给事务外的调用方。
		out = n
		return nil
	})
	return out, err
}

// ensureFSRootTx 事务内取或建该树的根文件夹。
func ensureFSRootTx(tx *gorm.DB, assetKind, treeLevel string, ownerID *uuid.UUID) (FSNode, error) {
	// 补上筛选或限定列，避免动到不该动的字段。
	q := tx.Where("asset_kind = ? AND tree_level = ? AND parent_id IS NULL AND node_kind = ?", assetKind, treeLevel, FSNodeFolder)
	// 没有主人就按空主人匹配根节点。
	if ownerID == nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("owner_id IS NULL")
		// 个人树按主人去对根，避免串到别人的目录。
	} else {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("owner_id = ?", *ownerID)
	}
	// 准备承接查到的目录节点。
	var row fsNodeRow
	// 按条件去读，没有行交给后面的分支。
	err := q.First(&row).Error
	// 已经查到就按现有结果核对，不再插入。
	if err == nil {
		// 库行收成对外结果再交回。
		return fsFromRow(row), nil
	}
	// 不是缺行的库错误要原样交回。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FSNode{}, err
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 组装目录节点，根和文件按种类填。
	row = fsNodeRow{
		ID: id.New(), Name: "", NodeKind: FSNodeFolder, AssetKind: assetKind,
		TreeLevel: treeLevel, OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := tx.Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 按条件去读，没有行交给后面的分支。
			err = q.First(&row).Error
			// 按条件取一行失败就停，避免带着错误继续。
			if err != nil {
				return FSNode{}, err
			}
			// 库行收成对外结果再交回。
			return fsFromRow(row), nil
		}
		// 先把库错误译成业务错误再交回。
		return FSNode{}, mapFSErr(err)
	}
	// 库行收成对外结果再交回。
	return fsFromRow(row), nil
}

// ListFSNodes 列出该种类全部节点，不含正文。
func (s *Store) ListFSNodes(ctx context.Context, assetKind string) ([]FSNode, error) {
	// 准备承接查到的多条目录节点。
	var rows []fsNodeRow
	// 补上筛选或限定列，避免动到不该动的字段。
	q := s.db.WithContext(ctx).Order("tree_level ASC, node_kind DESC, name ASC")
	// 排好序这一支不成立就换路。
	if assetKind != "" {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("asset_kind = ?", assetKind)
	}
	// 按条件取多行失败就停，避免带着错误继续。
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]FSNode, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// FSNodeByID 按身份取节点。
func (s *Store) FSNodeByID(ctx context.Context, nodeID uuid.UUID) (FSNode, error) {
	// 准备承接查到的目录节点。
	var row fsNodeRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", nodeID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成对外结果再交回。
	return fsFromRow(row), nil
}

// FSFileByAsset 按资产或副本身份取文件节点。
func (s *Store) FSFileByAsset(ctx context.Context, assetID uuid.UUID) (FSNode, error) {
	// 准备承接查到的目录节点。
	var row fsNodeRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成对外结果再交回。
	return fsFromRow(row), nil
}

// FSLineage 给出文件挂点和从靠近根到父文件夹的路径，不含根。
func (s *Store) FSLineage(ctx context.Context, assetID uuid.UUID) (*uuid.UUID, []FSFolderHint, error) {
	// 先按资产找文件节点，结果留给紧跟着的判断。
	file, err := s.FSFileByAsset(ctx, assetID)
	// 按资产找文件节点失败就停，避免带着错误继续。
	if err != nil {
		return nil, nil, err
	}
	// 已经是根就停，根不能改父级或删除。
	if file.ParentID == nil {
		return nil, nil, nil
	}
	// 准备承接这次要交回的结果。
	var folders []FSFolderHint
	// 从当前节点往上收集，直到厂根。
	cur := *file.ParentID
	// 沿父级最多走六十四层，防止坏链死循环。
	for i := 0; i < 64; i++ {
		// 先按身份读目录节点，结果留给紧跟着的判断。
		n, err := s.FSNodeByID(ctx, cur)
		// 按身份读目录节点失败就停，避免带着错误继续。
		if err != nil {
			return nil, nil, err
		}
		// 已经是根就停，根不能改父级或删除。
		if n.ParentID == nil {
			break
		}
		// 记下这一层文件夹，用来还原从根到父的路径。
		hint := FSFolderHint{ID: n.ID, Name: n.Name}
		// 已经查到就按现有结果核对，不再插入。
		if parent, err := s.FSNodeByID(ctx, *n.ParentID); err == nil && parent.ParentID != nil {
			// 记下这一层文件夹，用来还原从根到父的路径。
			pid := parent.ID
			// 带上父级或收进路径，供云端对齐。
			hint.ParentID = &pid
		}
		// 收进结果，保持原来的先后顺序。
		folders = append(folders, hint)
		// 继续走向父级，直到根或发现环。
		cur = *n.ParentID
	}
	// 把自叶到根的顺序倒成自根到叶。
	for i, j := 0, len(folders)-1; i < j; i, j = i+1, j-1 {
		// 交换两截，完成从叶到根的倒序。
		folders[i], folders[j] = folders[j], folders[i]
	}
	// 先记下父级，再往上收集文件夹。
	parent := *file.ParentID
	// 已经是根就停，根不能改父级或删除。
	if rn, err := s.FSNodeByID(ctx, parent); err == nil && rn.ParentID == nil {
		return nil, folders, nil
	}
	return &parent, folders, nil
}

// InsertFSFolder 在父文件夹下新建文件夹。
func (s *Store) InsertFSFolder(ctx context.Context, parentID uuid.UUID, name string) (FSNode, error) {
	// 先检查目录名，结果留给紧跟着的判断。
	name, err := checkFSName(name)
	// 检查目录名失败就停，避免带着错误继续。
	if err != nil {
		return FSNode{}, err
	}
	// 准备承接查到的目录节点。
	var out FSNode
	// 放进同一事务，中途失败就整单回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先读取目录节点，结果留给紧跟着的判断。
		parent, err := fsNodeTx(tx, parentID)
		// 读取目录节点失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 父级必须是文件夹，文件下面不能再挂。
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		// 取当前时刻，时间列和租约用同一个时钟。
		now := time.Now().UTC()
		// 组装目录节点，根和文件按种类填。
		row := fsNodeRow{
			ID: id.New(), Name: name, ParentID: &parent.ID, NodeKind: FSNodeFolder,
			AssetKind: parent.AssetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
			CreatedAt: now, UpdatedAt: now,
		}
		// 写入一行失败就停，避免带着错误继续。
		if err := tx.Create(&row).Error; err != nil {
			// 先把库错误译成业务错误再交回。
			return mapFSErr(err)
		}
		// 先收成目录视图，结果留给紧跟着的判断。
		out = fsFromRow(row)
		return nil
	})
	return out, err
}

// RenameFSNode 改节点显示名；根不能改。
func (s *Store) RenameFSNode(ctx context.Context, nodeID uuid.UUID, name string) (FSNode, error) {
	// 先检查目录名，结果留给紧跟着的判断。
	name, err := checkFSName(name)
	// 检查目录名失败就停，避免带着错误继续。
	if err != nil {
		return FSNode{}, err
	}
	// 准备承接查到的目录节点。
	var out FSNode
	// 放进同一事务，中途失败就整单回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先读取目录节点，结果留给紧跟着的判断。
		n, err := fsNodeTx(tx, nodeID)
		// 读取目录节点失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 已经是根就停，根不能改父级或删除。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 取当前时刻，时间列和租约用同一个时钟。
		now := time.Now().UTC()
		// 只改点名的列，避免把别的字段写成零值。
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"name": name, "updated_at": now})
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			// 先把库错误译成业务错误再交回。
			return mapFSErr(res.Error)
		}
		// 把改好的值放进内存行，随后随保存写入。
		n.Name = name
		// 把改好的值放进内存行，随后随保存写入。
		n.UpdatedAt = now
		// 把查到的结果交给事务外的调用方。
		out = n
		return nil
	})
	return out, err
}

// MoveFSNode 改父节点；不能跨树、不能成环、不能移根。
func (s *Store) MoveFSNode(ctx context.Context, nodeID, parentID uuid.UUID) (FSNode, error) {
	// 准备承接查到的目录节点。
	var out FSNode
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先读取目录节点，结果留给紧跟着的判断。
		n, err := fsNodeTx(tx, nodeID)
		// 读取目录节点失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 已经是根就停，根不能改父级或删除。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 先读取目录节点，结果留给紧跟着的判断。
		parent, err := fsNodeTx(tx, parentID)
		// 读取目录节点失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 父级必须是文件夹，文件下面不能再挂。
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		// 不能跨种类或跨树层级搬家。
		if parent.AssetKind != n.AssetKind || parent.TreeLevel != n.TreeLevel {
			return domain.ErrForbidden
		}
		// 主人不同就不能搬，避免串到别人的目录。
		if !ownerEq(parent.OwnerID, n.OwnerID) {
			return domain.ErrForbidden
		}
		// 不能搬到自己或子孙下面，否则成环。
		if parent.ID == n.ID || fsIsAncestorTx(tx, parent.ID, n.ID) {
			return domain.ErrCycle
		}
		// 取当前时刻，时间列和租约用同一个时钟。
		now := time.Now().UTC()
		// 只改点名的列，避免把别的字段写成零值。
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"parent_id": parent.ID, "updated_at": now})
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			// 先把库错误译成业务错误再交回。
			return mapFSErr(res.Error)
		}
		// 内存里的节点先改好，再交给调用方。
		n.ParentID = &parent.ID
		// 把改好的值放进内存行，随后随保存写入。
		n.UpdatedAt = now
		// 把查到的结果交给事务外的调用方。
		out = n
		return nil
	})
	return out, err
}

// DeleteFSNode 删空文件夹或文件引用；有孩子则拒绝。
func (s *Store) DeleteFSNode(ctx context.Context, nodeID uuid.UUID) error {
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先读取目录节点，结果留给紧跟着的判断。
		n, err := fsNodeTx(tx, nodeID)
		// 读取目录节点失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 已经是根就停，根不能改父级或删除。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 准备承接计数，用来判断有没有匹配行。
		var kids int64
		// 计数失败就停，避免带着错误继续。
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ?", nodeID).Count(&kids).Error; err != nil {
			return err
		}
		// 下面还有子节点就不能删，先清下级。
		if kids > 0 {
			return domain.ErrHasActiveChildren
		}
		// 删掉匹配行，没有行交给后面判断。
		res := tx.Where("id = ?", nodeID).Delete(&fsNodeRow{})
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			return res.Error
		}
		// 没有改到行就当目标不存在。
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// DeleteFSFileByAsset 按资产或副本身份去掉文件节点；没有也算成功。
func (s *Store) DeleteFSFileByAsset(ctx context.Context, assetID uuid.UUID) error {
	// 删掉匹配行，库错误原样交回。
	return s.db.WithContext(ctx).Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Delete(&fsNodeRow{}).Error
}

// ListFSChildren 列出直接孩子。
func (s *Store) ListFSChildren(ctx context.Context, parentID uuid.UUID) ([]FSNode, error) {
	// 准备承接查到的多条目录节点。
	var rows []fsNodeRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("parent_id = ?", parentID).Order("node_kind DESC, name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]FSNode, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// fsNodeTx 事务内按身份取节点。
func fsNodeTx(tx *gorm.DB, id uuid.UUID) (FSNode, error) {
	// 准备承接查到的目录节点。
	var row fsNodeRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := tx.First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成对外结果再交回。
	return fsFromRow(row), nil
}

// fsIsAncestorTx 向上走父链，判断会不会成环。
func fsIsAncestorTx(tx *gorm.DB, nodeID, ancestorID uuid.UUID) bool {
	// 从当前节点往上收集，直到厂根。
	cur := nodeID
	// 沿父级最多走六十四层，防止坏链死循环。
	for i := 0; i < 64; i++ {
		// 准备承接查到的目录节点。
		var row fsNodeRow
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.Select("id", "parent_id").First(&row, "id = ?", cur).Error; err != nil {
			return false
		}
		// 已经是根就停，根不能改父级或删除。
		if row.ParentID == nil {
			return false
		}
		// 走到目标祖先就说明已在其子树里。
		if *row.ParentID == ancestorID {
			return true
		}
		// 继续走向父级，直到根或发现环。
		cur = *row.ParentID
	}
	return false
}

// ownerEq 两个可空主人是否同一人。
func ownerEq(a, b *uuid.UUID) bool {
	// 两边都没有就视为相同。
	if a == nil && b == nil {
		return true
	}
	// 只有一边有就视为已经变化。
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// uniqueChildName 同级不撞名；已占用则带编号，避免 UNIQUE 把事务打挂。
func uniqueChildName(tx *gorm.DB, parentID uuid.UUID, name, extra string) (string, error) {
	// 去掉首尾空白，纯空白不当有效内容。
	name = strings.TrimSpace(name)
	// 空名字不落库，避免出现空白项。
	if name == "" {
		// 名字空了就退回备用名，避免空白项。
		name = extra
	}
	// 准备候选名字，重名就换下一个。
	cands := []string{name}
	// 备用名和原名不同才加入候选。
	if extra != "" && extra != name {
		// 收进结果，保持原来的先后顺序。
		cands = append(cands, name+" "+extra)
	}
	// 现发一个新身份，不沿用空值。
	cands = append(cands, name+" "+id.New().String()[:8])
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, cand := range cands {
		// 准备承接计数，用来判断有没有匹配行。
		var n int64
		// 计数失败就停，避免带着错误继续。
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ?", parentID, cand).Count(&n).Error; err != nil {
			return "", err
		}
		// 这个名字还没被占用，可以拿来用。
		if n == 0 {
			return cand, nil
		}
	}
	return name + " " + extra, nil
}

// placeAssetFileTx 给资产或副本挂文件节点；同名则带编号，避免挡住新建。
func placeAssetFileTx(tx *gorm.DB, assetKind, treeLevel string, ownerID *uuid.UUID, assetID uuid.UUID, name, code string, parentID uuid.UUID) error {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 计数失败就停，避免带着错误继续。
	if err := tx.Model(&fsNodeRow{}).Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Count(&n).Error; err != nil {
		return err
	}
	// 文件节点已经在，就不再建第二份。
	if n > 0 {
		return nil
	}
	// 准备承接查到的目录节点。
	var parent FSNode
	// 先留出错误位，后面的分支再填。
	var err error
	// 没指定父级就挂到这棵树的根上。
	if parentID == uuid.Nil {
		// 先确保目录根，结果留给紧跟着的判断。
		parent, err = ensureFSRootTx(tx, assetKind, treeLevel, ownerID)
		// 指定了父级就读那个节点，不再新建根。
	} else {
		// 先读取目录节点，结果留给紧跟着的判断。
		parent, err = fsNodeTx(tx, parentID)
	}
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return err
	}
	// 父级必须是文件夹，文件下面不能再挂。
	if parent.NodeKind != FSNodeFolder || parent.AssetKind != assetKind || parent.TreeLevel != treeLevel {
		return domain.ErrForbidden
	}
	// 先挑选不重名，结果留给紧跟着的判断。
	fileName, err := uniqueChildName(tx, parent.ID, name, code)
	// 挑选不重名失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 组装目录节点，根和文件按种类填。
	row := fsNodeRow{
		ID: id.New(), Name: fileName, ParentID: &parent.ID, NodeKind: FSNodeFile,
		AssetKind: assetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
		AssetID: &assetID, CreatedAt: now, UpdatedAt: now,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := tx.Create(&row).Error; err != nil {
		// 先把库错误译成业务错误再交回。
		return mapFSErr(err)
	}
	return nil
}

// syncFSFileNameTx 资产改名时同步文件节点名；撞名则保持旧名。
func syncFSFileNameTx(tx *gorm.DB, assetID uuid.UUID, name string) error {
	// 去掉首尾空白，纯空白不当有效内容。
	name = strings.TrimSpace(name)
	// 空名字不落库，避免出现空白项。
	if name == "" {
		return nil
	}
	// 准备承接查到的目录节点。
	var file fsNodeRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).First(&file).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// 已经是根就停，根不能改父级或删除。
	if file.ParentID == nil {
		return nil
	}
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 计数失败就停，避免带着错误继续。
	if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ? AND id <> ?", *file.ParentID, name, file.ID).Count(&n).Error; err != nil {
		return err
	}
	// 同级已经有这个名字，就拒绝改名。
	if n > 0 {
		return nil
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	return tx.Model(&fsNodeRow{}).Where("id = ?", file.ID).Updates(map[string]any{"name": name, "updated_at": time.Now().UTC()}).Error
}

// ApplyPlatformFS 按云端路径把副本文件挂到对应文件夹；没有提示则不动。
func (s *Store) ApplyPlatformFS(ctx context.Context, assetKind string, assetID uuid.UUID, parentID *uuid.UUID, folders []FSFolderHint) error {
	// 没有路径信息就不必改目录。
	if parentID == nil && len(folders) == 0 {
		return nil
	}
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先确保目录根，结果留给紧跟着的判断。
		root, err := ensureFSRootTx(tx, assetKind, FSTreePlatform, nil)
		// 确保目录根失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 准备去重或保留集合，避免重复和误删。
		keep := map[uuid.UUID]struct{}{root.ID: {}}
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, f := range orderFSFolders(folders) {
			// 对齐平台文件夹失败就停，避免带着错误继续。
			if err := upsertPlatformFolderTx(tx, assetKind, f, root.ID); err != nil {
				return err
			}
			// 记下已经放过或必须留下的身份。
			keep[f.ID] = struct{}{}
		}
		// 默认挂在根上，有父级再改到父级。
		dest := root.ID
		// 指定了父级就挂过去，否则留在根上。
		if parentID != nil {
			// 改挂到指定父级，不再留在根上。
			dest = *parentID
		}
		// 挂上或搬走文件失败就停，避免带着错误继续。
		if err := placeOrMovePlatformFileTx(tx, assetKind, assetID, dest); err != nil {
			return err
		}
		// 做完剪掉空目录后把结果交回。
		return pruneEmptyPlatformFoldersTx(tx, assetKind, keep)
	})
}

// ApplyPlatformFSLayout 按云端整棵平台树对齐本厂已到达的副本。
func (s *Store) ApplyPlatformFSLayout(ctx context.Context, layout PlatformFSLayout) error {
	// 只认工艺和工程，别的种类拒绝。
	if layout.Kind != KindProcess && layout.Kind != KindProject {
		return domain.ErrNotFound
	}
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先确保目录根，结果留给紧跟着的判断。
		root, err := ensureFSRootTx(tx, layout.Kind, FSTreePlatform, nil)
		// 确保目录根失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 准备去重或保留集合，避免重复和误删。
		keep := map[uuid.UUID]struct{}{root.ID: {}}
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, f := range orderFSFolders(layout.Folders) {
			// 对齐平台文件夹失败就停，避免带着错误继续。
			if err := upsertPlatformFolderTx(tx, layout.Kind, f, root.ID); err != nil {
				return err
			}
			// 记下已经放过或必须留下的身份。
			keep[f.ID] = struct{}{}
		}
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, f := range layout.Files {
			// 默认挂在根上，有父级再改到父级。
			dest := root.ID
			// 指定了父级就挂过去，否则留在根上。
			if f.ParentID != nil {
				// 改挂到指定父级，不再留在根上。
				dest = *f.ParentID
			}
			// 挂上或搬走文件失败就停，避免带着错误继续。
			if err := placeOrMovePlatformFileTx(tx, layout.Kind, f.AssetID, dest); err != nil {
				return err
			}
		}
		// 做完剪掉空目录后把结果交回。
		return pruneEmptyPlatformFoldersTx(tx, layout.Kind, keep)
	})
}

// upsertPlatformFolderTx 用 WAN 文件夹身份在本厂平台树落或改一层。
func upsertPlatformFolderTx(tx *gorm.DB, assetKind string, hint FSFolderHint, rootID uuid.UUID) error {
	// 空身份或就是根就跳过，避免把根再插一次。
	if hint.ID == uuid.Nil || hint.ID == rootID {
		return domain.ErrForbidden
	}
	// 先检查目录名，结果留给紧跟着的判断。
	name, err := checkFSName(hint.Name)
	// 检查目录名失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 先挂在根或指定父级下面。
	parentID := rootID
	// 指定了父级就挂过去，否则留在根上。
	if hint.ParentID != nil {
		// 先挂在根或指定父级下面。
		parentID = *hint.ParentID
	}
	// 准备承接查到的目录节点。
	var row fsNodeRow
	// 按条件去读，没有行交给后面的分支。
	err = tx.First(&row, "id = ?", hint.ID).Error
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 已经查到就按现有结果核对，不再插入。
	if err == nil {
		// 已经是根就停，根不能改父级或删除。
		if row.NodeKind != FSNodeFolder || row.TreeLevel != FSTreePlatform || row.AssetKind != assetKind || row.ParentID == nil {
			return domain.ErrForbidden
		}
		// 先把库错误译成业务错误再交回。
		return mapFSErr(tx.Model(&fsNodeRow{}).Where("id = ?", hint.ID).Updates(map[string]any{
			"name": name, "parent_id": parentID, "updated_at": now,
		}).Error)
	}
	// 不是缺行的库错误要原样交回。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// 组装目录节点，根和文件按种类填。
	row = fsNodeRow{
		ID: hint.ID, Name: name, ParentID: &parentID, NodeKind: FSNodeFolder,
		AssetKind: assetKind, TreeLevel: FSTreePlatform, CreatedAt: now, UpdatedAt: now,
	}
	// 先把库错误译成业务错误再交回。
	return mapFSErr(tx.Create(&row).Error)
}

// placeOrMovePlatformFileTx 已有文件则搬家；还没有且副本已在则先挂上。
func placeOrMovePlatformFileTx(tx *gorm.DB, assetKind string, assetID, parentID uuid.UUID) error {
	// 准备承接查到的目录节点。
	var file fsNodeRow
	// 按条件去读，没有行交给后面的分支。
	err := tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).First(&file).Error
	// 没有这一行就当成不存在。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 准备承接查到的平台副本。
		var meta replicaRow
		// 按条件取一行失败就停，避免带着错误继续。
		if qerr := tx.Select("id", "kind", "name", "code").Where("id = ?", assetID).Order("revision DESC").First(&meta).Error; qerr != nil {
			// 没有这一行就当成不存在。
			if errors.Is(qerr, gorm.ErrRecordNotFound) {
				return nil
			}
			return qerr
		}
		// 编号先当空，查到元数据再填上。
		code := ""
		if meta.Code != nil {
			// 把查到的编号填上，没有就留空。
			code = *meta.Code
		}
		// 做完挂上文件节点后把结果交回。
		return placeAssetFileTx(tx, meta.Kind, FSTreePlatform, nil, assetID, meta.Name, code, parentID)
	}
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return err
	}
	// 不能跨种类或跨树层级搬家。
	if file.TreeLevel != FSTreePlatform || file.AssetKind != assetKind {
		return domain.ErrForbidden
	}
	// 已经挂在目标下面就不用再搬。
	if file.ParentID != nil && *file.ParentID == parentID {
		return nil
	}
	// 先读取目录节点，结果留给紧跟着的判断。
	parent, err := fsNodeTx(tx, parentID)
	// 读取目录节点失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 父级必须是文件夹，文件下面不能再挂。
	if parent.NodeKind != FSNodeFolder || parent.TreeLevel != FSTreePlatform || parent.AssetKind != assetKind {
		return domain.ErrForbidden
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 先把库错误译成业务错误再交回。
	return mapFSErr(tx.Model(&fsNodeRow{}).Where("id = ?", file.ID).Updates(map[string]any{"parent_id": parent.ID, "updated_at": now}).Error)
}

// pruneEmptyPlatformFoldersTx 清掉不在保留集、也没有孩子的平台文件夹。
func pruneEmptyPlatformFoldersTx(tx *gorm.DB, assetKind string, keep map[uuid.UUID]struct{}) error {
	for i := 0; i < 64; i++ {
		// 准备承接查到的多条目录节点。
		var folders []fsNodeRow
		// 按条件取多行失败就停，避免带着错误继续。
		if err := tx.Where("asset_kind = ? AND tree_level = ? AND node_kind = ? AND parent_id IS NOT NULL", assetKind, FSTreePlatform, FSNodeFolder).Find(&folders).Error; err != nil {
			return err
		}
		// 这轮还没删过空目录。
		deleted := 0
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, f := range folders {
			// 这层还要留，不能当成空目录删掉。
			if _, ok := keep[f.ID]; ok {
				continue
			}
			// 准备承接计数，用来判断有没有匹配行。
			var n int64
			// 计数失败就停，避免带着错误继续。
			if err := tx.Model(&fsNodeRow{}).Where("parent_id = ?", f.ID).Count(&n).Error; err != nil {
				return err
			}
			// 已经有匹配行就停，避免重复或误删。
			if n > 0 {
				continue
			}
			// 删除匹配行失败就停，避免带着错误继续。
			if err := tx.Delete(&fsNodeRow{}, "id = ?", f.ID).Error; err != nil {
				return err
			}
			// 记下这轮删过空目录，没有删就可以停。
			deleted++
		}
		// 这一轮没删掉空目录，就可以停了。
		if deleted == 0 {
			return nil
		}
	}
	return nil
}

// orderFSFolders 先落靠近根的文件夹，避免父节点还不存在。
func orderFSFolders(in []FSFolderHint) []FSFolderHint {
	// 不到两个文件夹就不必再排父子顺序。
	if len(in) < 2 {
		return in
	}
	// 收进结果，保持原来的先后顺序。
	remaining := append([]FSFolderHint(nil), in...)
	// 准备去重或保留集合，避免重复和误删。
	have := map[uuid.UUID]struct{}{}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]FSFolderHint, 0, len(in))
	// 还有没排进去的文件夹就再扫一轮。
	for len(remaining) > 0 {
		// 先当这一轮没有进展。
		progress := false
		// 准备下一轮还没排进去的文件夹。
		next := remaining[:0]
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, f := range remaining {
			// 已经是根就停，根不能改父级或删除。
			if f.ParentID == nil {
				// 收进结果，保持原来的先后顺序。
				out = append(out, f)
				// 记下已经放过或必须留下的身份。
				have[f.ID] = struct{}{}
				// 这一轮有进展，才值得再扫剩下的。
				progress = true
				continue
			}
			// 父级已经入列才放自己，保证父在子前。
			if _, ok := have[*f.ParentID]; ok {
				// 收进结果，保持原来的先后顺序。
				out = append(out, f)
				// 记下已经放过或必须留下的身份。
				have[f.ID] = struct{}{}
				// 这一轮有进展，才值得再扫剩下的。
				progress = true
				continue
			}
			// 收进结果，保持原来的先后顺序。
			next = append(next, f)
		}
		// 换成还没入列的那些，再扫一轮。
		remaining = next
		// 一轮没有任何进展就停止，剩下的直接附上。
		if !progress {
			// 收进结果，保持原来的先后顺序。
			out = append(out, remaining...)
			break
		}
	}
	return out
}
