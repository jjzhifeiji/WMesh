package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

const (
	FSNodeFolder = "folder" // 容器，不引用正文
	FSNodeFile   = "file"   // 引用一条资产

	FSTreePlatform = "platform" // WAN 只有这一棵
)

const fsNameMax = 64 // 与显示名错误文案对齐

// FSNode 是目录树上的一条：文件夹或文件引用。
type FSNode struct {
	ID        uuid.UUID  `json:"id"`                // 节点身份
	Name      string     `json:"name"`              // 显示名；根为空
	ParentID  *uuid.UUID `json:"parentId"`          // 空表示根
	NodeKind  string     `json:"nodeKind"`          // folder / file
	AssetKind string     `json:"assetKind"`         // process / project
	TreeLevel string     `json:"treeLevel"`         // 固定 platform
	OwnerID   *uuid.UUID `json:"ownerId,omitempty"` // 平台树为空
	AssetID   *uuid.UUID `json:"assetId,omitempty"` // 文件才有
	CreatedAt time.Time  `json:"createdAt"`         // 创建时间
	UpdatedAt time.Time  `json:"updatedAt"`         // 最近改名或搬家
}

// FSFolderHint 是平台文件夹的一层，给厂端对齐路径；不进闭包摘要。
type FSFolderHint struct {
	ID       uuid.UUID  `json:"id"`                 // 与 WAN 文件夹同一身份
	Name     string     `json:"name"`               // 显示名
	ParentID *uuid.UUID `json:"parentId,omitempty"` // 空表示挂在厂端平台根下
}

// FSFileHint 是已下发文件应挂到哪个文件夹。
type FSFileHint struct {
	AssetID  uuid.UUID  `json:"assetId"`            // 平台级资产身份
	ParentID *uuid.UUID `json:"parentId,omitempty"` // 空表示挂在平台根
}

// PlatformFSLayout 是某一种类平台树当前应对齐的目录。
type PlatformFSLayout struct {
	Kind    string         `json:"kind"`    // process / project
	Folders []FSFolderHint `json:"folders"` // 已有文件的祖先文件夹，不含根
	Files   []FSFileHint   `json:"files"`   // 文件挂点
}

// 目录树上的文件夹或文件引用，不含资产正文。
type fsNodeRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 节点身份
	Name      string     `gorm:"not null"`             // 显示名；根为空
	ParentID  *uuid.UUID `gorm:"type:uuid"`            // 空表示根
	NodeKind  string     `gorm:"not null"`             // folder / file
	AssetKind string     `gorm:"not null"`             // process / project
	TreeLevel string     `gorm:"not null"`             // 固定 platform
	OwnerID   *uuid.UUID `gorm:"type:uuid"`            // 平台树为空
	AssetID   *uuid.UUID `gorm:"type:uuid"`            // 文件才有
	CreatedAt time.Time  `gorm:"not null"`             // 创建时间
	UpdatedAt time.Time  `gorm:"not null"`             // 最近改名或搬家
}

// 目录节点落这张表，不含资产正文。
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
	// 同级已经有这个名字，按重名拒绝。
	if domain.IsUniqueViolation(err) {
		return domain.ErrDuplicateName
	}
	// 引用的厂或资产不在名录，按不存在拒绝。
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// checkFSName 文件夹名不能空、不能带路径分隔符。
func checkFSName(name string) (string, error) {
	// 先去掉首尾空白，再看是不是合法文件夹名。
	name = strings.TrimSpace(name)
	// 空名、超长或带分隔符都不能当文件夹名。
	if name == "" || utf8.RuneCountInString(name) > fsNameMax || strings.ContainsAny(name, "/\\") {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// EnsureFSRoot 取出该种类的根；没有则建。
func (s *Store) EnsureFSRoot(ctx context.Context, assetKind string) (FSNode, error) {
	// 不是工艺也不是工程，就没有这棵目录树。
	if assetKind != KindProcess && assetKind != KindProject {
		return FSNode{}, domain.ErrNotFound
	}
	// 准备接住事务里写好的结果，失败则不用它。
	var out FSNode
	// 取根或建根放在同一事务，避免并发插出两棵。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 取出或建好这类的根，事务外再交回。
		n, err := ensureFSRootTx(tx, assetKind, FSTreePlatform, nil)
		// 根取不到也建不成就停，资产不能没有挂点。
		if err != nil {
			return err
		}
		// 节点已经取到，事务外再交回。
		out = n
		return nil
	})
	return out, err
}

// ensureFSRootTx 事务内取或建该种类的根文件夹。
func ensureFSRootTx(tx *gorm.DB, assetKind, treeLevel string, ownerID *uuid.UUID) (FSNode, error) {
	// 先按种类、树和根去找，主人条件后面再补。
	q := tx.Where("asset_kind = ? AND tree_level = ? AND parent_id IS NULL AND node_kind = ?", assetKind, treeLevel, FSNodeFolder)
	// 没有主人就是平台树，根按空主人去对。
	if ownerID == nil {
		// 平台树没有主人，根必须对上空主人。
		q = q.Where("owner_id IS NULL")
		// 指定了主人就只找这个主人的根。
	} else {
		// 只找这个主人的根，不串到别的树。
		q = q.Where("owner_id = ?", *ownerID)
	}
	// 准备接住库里的那一行，没有再另作处理。
	var row fsNodeRow
	// 按上面的条件取根，没有再决定要不要建。
	err := q.First(&row).Error
	// 根已经在，直接用，不再建第二棵。
	if err == nil {
		// 库行收成目录节点再交回。
		return fsFromRow(row), nil
	}
	// 不是没有这行，是读取失败，不能当成该新建。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FSNode{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 组一条空名的根，身份在这里现场发。
	row = fsNodeRow{
		ID: id.New(), Name: "", NodeKind: FSNodeFolder, AssetKind: assetKind,
		TreeLevel: treeLevel, OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := tx.Create(&row).Error; err != nil {
		// 别人刚建好根，再读那一条来用。
		if domain.IsUniqueViolation(err) {
			// 并发下别人刚建好，再读那一条。
			err = q.First(&row).Error
			// 再读根仍然失败就停，不能交回空节点。
			if err != nil {
				return FSNode{}, err
			}
			// 库行收成目录节点再交回。
			return fsFromRow(row), nil
		}
		// 建根失败收成业务错误再交回。
		return FSNode{}, mapFSErr(err)
	}
	// 库行收成目录节点再交回。
	return fsFromRow(row), nil
}

// ListFSNodes 列出该种类全部节点，不含资产正文。
func (s *Store) ListFSNodes(ctx context.Context, assetKind string) ([]FSNode, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []fsNodeRow
	// 文件夹排在文件前，再按名字排，种类后面收窄。
	q := s.db.WithContext(ctx).Order("node_kind DESC, name ASC")
	// 指定了种类就只列这一棵树。
	if assetKind != "" {
		// 指定了种类就只留这一棵树。
		q = q.Where("asset_kind = ?", assetKind)
	}
	// 列表没读出来就停，故障不能当成空表。
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]FSNode, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, r := range rows {
		// 这一行收成目录节点，放进结果。
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// FSNodeByID 按身份取节点。
func (s *Store) FSNodeByID(ctx context.Context, nodeID uuid.UUID) (FSNode, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row fsNodeRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", nodeID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成目录节点再交回。
	return fsFromRow(row), nil
}

// FSFileByAsset 按资产身份取文件节点。
func (s *Store) FSFileByAsset(ctx context.Context, assetID uuid.UUID) (FSNode, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row fsNodeRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成目录节点再交回。
	return fsFromRow(row), nil
}

// InsertFSFolder 在父文件夹下新建文件夹。
func (s *Store) InsertFSFolder(ctx context.Context, parentID uuid.UUID, name string) (FSNode, error) {
	// 文件夹名先校验，空名和分隔符不能入库。
	name, err := checkFSName(name)
	// 名字不合格就停，非法名不能进目录。
	if err != nil {
		return FSNode{}, err
	}
	// 准备接住事务里写好的结果，失败则不用它。
	var out FSNode
	// 确认父节点是文件夹后再插入，重名则回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先取出父节点，确认它能往下挂。
		parent, err := fsNodeTx(tx, parentID)
		// 节点没取到就停，后面的改动没有对象。
		if err != nil {
			return err
		}
		// 父节点不是文件夹，不能往下面挂。
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		// 记下当前时刻，这一笔里的时间都用它。
		now := time.Now().UTC()
		// 在父节点下组一条文件夹，身份现场发。
		row := fsNodeRow{
			ID: id.New(), Name: name, ParentID: &parent.ID, NodeKind: FSNodeFolder,
			AssetKind: parent.AssetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
			CreatedAt: now, UpdatedAt: now,
		}
		// 这一行没写进去就停，不能当成已经落库。
		if err := tx.Create(&row).Error; err != nil {
			// 重名或引用缺失收成业务错误再交回。
			return mapFSErr(err)
		}
		// 新文件夹收成节点，成功才交回。
		out = fsFromRow(row)
		return nil
	})
	return out, err
}

// RenameFSNode 改节点显示名；根不能改。
func (s *Store) RenameFSNode(ctx context.Context, nodeID uuid.UUID, name string) (FSNode, error) {
	// 文件夹名先校验，空名和分隔符不能入库。
	name, err := checkFSName(name)
	// 名字不合格就停，非法名不能进目录。
	if err != nil {
		return FSNode{}, err
	}
	// 准备接住事务里写好的结果，失败则不用它。
	var out FSNode
	// 根不能改名；改名和更新时间一起提交。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先取出要动的节点，根和跨树在这里挡。
		n, err := fsNodeTx(tx, nodeID)
		// 节点没取到就停，后面的改动没有对象。
		if err != nil {
			return err
		}
		// 根没有可改的显示名，直接拒绝。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 记下当前时刻，这一笔里的时间都用它。
		now := time.Now().UTC()
		// 改显示名并记下这次变更的时刻。
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"name": name, "updated_at": now})
		// 这一步报错就收成重名之类的业务错误。
		if res.Error != nil {
			// 重名或引用缺失收成业务错误再交回。
			return mapFSErr(res.Error)
		}
		// 内存里的显示名改掉，和库保持一致。
		n.Name = name
		// 记下这次变更的时刻。
		n.UpdatedAt = now
		// 节点已经取到，事务外再交回。
		out = n
		return nil
	})
	return out, err
}

// MoveFSNode 改父节点；不能跨树、不能成环、不能移根。
func (s *Store) MoveFSNode(ctx context.Context, nodeID, parentID uuid.UUID) (FSNode, error) {
	// 准备接住事务里写好的结果，失败则不用它。
	var out FSNode
	// 先挡住成环和跨树，再改父节点。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先取出要动的节点，根和跨树在这里挡。
		n, err := fsNodeTx(tx, nodeID)
		// 节点没取到就停，后面的改动没有对象。
		if err != nil {
			return err
		}
		// 根不能搬走，否则这一类没有入口。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 先取出父节点，确认它能往下挂。
		parent, err := fsNodeTx(tx, parentID)
		// 节点没取到就停，后面的改动没有对象。
		if err != nil {
			return err
		}
		// 父节点不是文件夹，不能往下面挂。
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		// 不能跨种类或跨树搬家，两边必须同一棵。
		if parent.AssetKind != n.AssetKind || parent.TreeLevel != n.TreeLevel {
			return domain.ErrForbidden
		}
		// 主人不同不能搬，避免挂到别人的树上。
		if !ownerEq(parent.OwnerID, n.OwnerID) {
			return domain.ErrForbidden
		}
		// 不能挂到自己或后代。
		if parent.ID == n.ID || fsIsAncestorTx(tx, parent.ID, n.ID) {
			return domain.ErrCycle
		}
		// 记下当前时刻，这一笔里的时间都用它。
		now := time.Now().UTC()
		// 把父节点改到新文件夹，并记下时刻。
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"parent_id": parent.ID, "updated_at": now})
		// 这一步报错就收成重名之类的业务错误。
		if res.Error != nil {
			// 重名或引用缺失收成业务错误再交回。
			return mapFSErr(res.Error)
		}
		// 内存里的父节点改到新挂点。
		n.ParentID = &parent.ID
		// 记下这次变更的时刻。
		n.UpdatedAt = now
		// 节点已经取到，事务外再交回。
		out = n
		return nil
	})
	return out, err
}

// DeleteFSNode 删空文件夹或文件引用；有孩子则拒绝。
func (s *Store) DeleteFSNode(ctx context.Context, nodeID uuid.UUID) error {
	// 有孩子或是根就不动，检查和删除在同一事务。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先取出要动的节点，根和跨树在这里挡。
		n, err := fsNodeTx(tx, nodeID)
		// 节点没取到就停，后面的改动没有对象。
		if err != nil {
			return err
		}
		// 根不能删，否则这一类没有入口。
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		// 准备接住子节点个数，有孩子就不能删。
		var kids int64
		// 数量没数出来就停，不能把零当成没有。
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ?", nodeID).Count(&kids).Error; err != nil {
			return err
		}
		// 下面还有节点就拒绝，避免子树变成孤儿。
		if kids > 0 {
			return domain.ErrHasActiveChildren
		}
		// 删掉这个空节点，孩子已经提前挡住。
		res := tx.Where("id = ?", nodeID).Delete(&fsNodeRow{})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			return res.Error
		}
		// 一行都没碰到，按不存在拒绝。
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// ListFSChildren 列出直接孩子。
func (s *Store) ListFSChildren(ctx context.Context, parentID uuid.UUID) ([]FSNode, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []fsNodeRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Where("parent_id = ?", parentID).Order("node_kind DESC, name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]FSNode, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, r := range rows {
		// 这一行收成目录节点，放进结果。
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// fsNodeTx 事务内按身份取节点。
func fsNodeTx(tx *gorm.DB, id uuid.UUID) (FSNode, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row fsNodeRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := tx.First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	// 库行收成目录节点再交回。
	return fsFromRow(row), nil
}

// fsIsAncestorTx 向上走父链，判断会不会成环。
func fsIsAncestorTx(tx *gorm.DB, nodeID, ancestorID uuid.UUID) bool {
	// 从当前节点出发，沿父链看会不会成环。
	cur := nodeID
	// 沿父链最多走六十四层，防止坏数据死循环。
	for i := 0; i < 64; i++ {
		// 准备接住库里的那一行，没有再另作处理。
		var row fsNodeRow
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.Select("id", "parent_id").First(&row, "id = ?", cur).Error; err != nil {
			return false
		}
		// 这一层没有父节点，说明还没走到要避开的那个。
		if row.ParentID == nil {
			return false
		}
		// 父节点就是要避开的那个，再挂就会成环。
		if *row.ParentID == ancestorID {
			return true
		}
		// 继续往父节点走，直到根或发现成环。
		cur = *row.ParentID
	}
	return false
}

// ownerEq 两个可空主人是否同一人。
func ownerEq(a, b *uuid.UUID) bool {
	// 两边都没有主人，算同一棵平台树。
	if a == nil && b == nil {
		return true
	}
	// 只有一边有主人，不能当成同一棵树。
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// uniqueChildName 同级不撞名；已占用则带编号，避免 UNIQUE 把事务打挂。
func uniqueChildName(tx *gorm.DB, parentID uuid.UUID, name, extra string) (string, error) {
	// 去掉首尾空白，空了再用编号顶上。
	name = strings.TrimSpace(name)
	// 显示名为空时用编号顶上，避免空名去撞根。
	if name == "" {
		// 显示名是空的，改用编号来占位。
		name = extra
	}
	// 先试原来的名字，撞了再试带编号的。
	cands := []string{name}
	// 原名之外再试一次带编号的，躲开同级重名。
	if extra != "" && extra != name {
		// 原名之外加上编号再试一次。
		cands = append(cands, name+" "+extra)
	}
	// 再加一段短身份，几乎不会和同级撞名。
	cands = append(cands, name+" "+id.New().String()[:8])
	// 按候选名逐个试，用第一个还没被占用的。
	for _, cand := range cands {
		// 准备接住行数，不能事先把零当成没有。
		var n int64
		// 数量没数出来就停，不能把零当成没有。
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ?", parentID, cand).Count(&n).Error; err != nil {
			return "", err
		}
		// 这个名字还没人用，可以挂上去。
		if n == 0 {
			return cand, nil
		}
	}
	return name + " " + extra, nil
}

// placeAssetFileTx 给资产挂文件节点；同名则带编号，避免挡住新建。
func placeAssetFileTx(tx *gorm.DB, assetKind string, assetID uuid.UUID, name, code string, parentID uuid.UUID) error {
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 数量没数出来就停，不能把零当成没有。
	if err := tx.Model(&fsNodeRow{}).Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Count(&n).Error; err != nil {
		return err
	}
	// 已经挂过文件就不再插第二份。
	if n > 0 {
		return nil
	}
	// 先取出父节点，确认它能往下挂。
	parent, err := fsNodeTx(tx, parentID)
	// 节点没取到就停，后面的改动没有对象。
	if err != nil {
		return err
	}
	// 挂点必须是同一种类的文件夹。
	if parent.NodeKind != FSNodeFolder || parent.AssetKind != assetKind {
		return domain.ErrForbidden
	}
	// 同级撞名就改用带编号的名字。
	fileName, err := uniqueChildName(tx, parent.ID, name, code)
	// 起名失败就停，不挂一个空名字的文件。
	if err != nil {
		return err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 组一条文件引用，名字用刚才躲开重名的那个。
	row := fsNodeRow{
		ID: id.New(), Name: fileName, ParentID: &parent.ID, NodeKind: FSNodeFile,
		AssetKind: assetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
		AssetID: &assetID, CreatedAt: now, UpdatedAt: now,
	}
	// 这一行没写进去就停，不能当成已经落库。
	if err := tx.Create(&row).Error; err != nil {
		// 重名或引用缺失收成业务错误再交回。
		return mapFSErr(err)
	}
	return nil
}

// syncFSFileNameTx 资产改名时同步文件节点名；撞名则保持旧名。
func syncFSFileNameTx(tx *gorm.DB, assetID uuid.UUID, name string) error {
	// 去掉空白，空名字就不同步文件名。
	name = strings.TrimSpace(name)
	// 显示名为空时用编号顶上，避免空名去撞根。
	if name == "" {
		return nil
	}
	// 准备接住资产对应的文件节点。
	var file fsNodeRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).First(&file).Error; err != nil {
		// 还没有文件节点就跳过改名，不算失败。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// 没有父文件夹就不改名，避免写到根上。
	if file.ParentID == nil {
		return nil
	}
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 数量没数出来就停，不能把零当成没有。
	if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ? AND id <> ?", *file.ParentID, name, file.ID).Count(&n).Error; err != nil {
		return err
	}
	// 同级已有这个名字就保持旧名，免得把事务打挂。
	if n > 0 {
		return nil
	}
	// 没有撞名就把文件名改成资产现在的名字。
	return tx.Model(&fsNodeRow{}).Where("id = ?", file.ID).Updates(map[string]any{"name": name, "updated_at": time.Now().UTC()}).Error
}

// FSLineage 给出文件挂点和从靠近根到父文件夹的路径，不含根。
func (s *Store) FSLineage(ctx context.Context, assetID uuid.UUID) (*uuid.UUID, []FSFolderHint, error) {
	// 先找到文件挂在哪，没有文件就没有路径。
	file, err := s.FSFileByAsset(ctx, assetID)
	// 文件节点没找到就停，没有挂点就没有路径。
	if err != nil {
		return nil, nil, err
	}
	// 文件直接挂在根上，没有文件夹路径可交。
	if file.ParentID == nil {
		return nil, nil, nil
	}
	// 路径先留空，从叶往根往里堆。
	var folders []FSFolderHint
	// 从文件的父节点往根走。
	cur := *file.ParentID
	// 从挂点往根走，最多六十四层。
	for i := 0; i < 64; i++ {
		// 取出这一层文件夹，失败则路径不完整。
		n, err := s.FSNodeByID(ctx, cur)
		// 这一层没读到就停，路径会缺一截。
		if err != nil {
			return nil, nil, err
		}
		// 走到根就停，根本身不放进路径。
		if n.ParentID == nil {
			break
		}
		// 这一层先记下身份和名字。
		hint := FSFolderHint{ID: n.ID, Name: n.Name}
		// 父的父还不是根，才把父身份放进提示。
		if parent, err := s.FSNodeByID(ctx, *n.ParentID); err == nil && parent.ParentID != nil {
			// 拷一份父身份，避免指到循环里的临时值。
			pid := parent.ID
			// 父还不是根时才把父身份放进提示。
			hint.ParentID = &pid
		}
		// 先按从叶到根堆上，最后再翻过来。
		folders = append(folders, hint)
		// 继续往父节点走，直到根或发现成环。
		cur = *n.ParentID
	}
	// 路径是从叶走到根的，这里翻成从根到叶。
	for i, j := 0, len(folders)-1; i < j; i, j = i+1, j-1 {
		// 对调两端，把路径改成从根走向叶。
		folders[i], folders[j] = folders[j], folders[i]
	}
	// 先记下挂点，若父是根后面会收成空。
	parent := *file.ParentID
	// 先假设父不是根，查到再改。
	rootish := false
	// 父节点就是根时，挂点不交给厂端。
	if rn, err := s.FSNodeByID(ctx, parent); err == nil && rn.ParentID == nil {
		// 确认文件就挂在根上。
		rootish = true
	}
	// 挂在根上时不把根的身份交出去。
	if rootish {
		return nil, folders, nil
	}
	return &parent, folders, nil
}

// PlatformFSLayout 收集该种类已有文件的祖先文件夹和挂点，供厂端对齐。
func (s *Store) PlatformFSLayout(ctx context.Context, assetKind string) (PlatformFSLayout, error) {
	// 先取出整棵树，再从文件往上收祖先。
	nodes, err := s.ListFSNodes(ctx, assetKind)
	// 整棵树没读出来就停，目录对不齐。
	if err != nil {
		return PlatformFSLayout{}, err
	}
	// 按身份索引整棵树，找父节点不用再扫一遍。
	byID := make(map[uuid.UUID]FSNode, len(nodes))
	// 先按身份放进索引，后面找父节点不用再扫。
	for _, n := range nodes {
		// 放进索引，后面按身份取父节点。
		byID[n.ID] = n
	}
	// 记下已经收过的文件夹，避免同一层重复。
	seen := map[uuid.UUID]struct{}{}
	// 结果先带上种类，文件夹和文件后面填。
	out := PlatformFSLayout{Kind: assetKind}
	// 只处理已经挂上资产的文件。
	for _, n := range nodes {
		// 不是已经挂上资产的文件就跳过。
		if n.NodeKind != FSNodeFile || n.AssetID == nil || n.ParentID == nil {
			continue
		}
		// 先记下文件当前挂在哪一层。
		parent := *n.ParentID
		// 先只记资产身份，根上的挂点留空。
		hint := FSFileHint{AssetID: *n.AssetID}
		// 父不是根时才把挂点交给厂端对齐。
		if p, ok := byID[parent]; ok && p.ParentID != nil {
			// 父还不是根时才把父身份放进提示。
			hint.ParentID = &parent
		}
		// 记下这个文件该挂在哪一层。
		out.Files = append(out.Files, hint)
		// 从挂点往根收集文件夹。
		cur := parent
		// 沿父链收集文件夹，走到根为止。
		for i := 0; i < 64; i++ {
			// 取出当前这一层，没有或到根就停。
			p, ok := byID[cur]
			// 没有这一层或已经到根，祖先收到这里为止。
			if !ok || p.ParentID == nil {
				break
			}
			// 这一层还没收过，才放进对齐列表。
			if _, dup := seen[p.ID]; !dup {
				// 这个文件夹已收下，不再重复进列表。
				seen[p.ID] = struct{}{}
				// 记下这一层的身份和名字。
				h := FSFolderHint{ID: p.ID, Name: p.Name}
				// 祖父还不是根，才带上父文件夹的身份。
				if gp, ok := byID[*p.ParentID]; ok && gp.ParentID != nil {
					// 拷一份父身份，避免指到循环里的临时值。
					pid := gp.ID
					// 祖父还不是根时才带上父文件夹。
					h.ParentID = &pid
				}
				// 把这一层祖先收进对齐列表。
				out.Folders = append(out.Folders, h)
			}
			// 继续往父节点走，直到根或发现成环。
			cur = *p.ParentID
		}
	}
	// 先排靠近根的，厂端才建得起父文件夹。
	out.Folders = orderFSFolders(out.Folders)
	return out, nil
}

// orderFSFolders 先落靠近根的文件夹，避免父节点还不存在。
func orderFSFolders(in []FSFolderHint) []FSFolderHint {
	// 不到两个就不用重排，顺序已经安全。
	if len(in) < 2 {
		return in
	}
	// 复制一份待排列表，排序时不改入参。
	remaining := append([]FSFolderHint(nil), in...)
	// 记下已经排好的，子层才知道父在不在。
	have := map[uuid.UUID]struct{}{}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]FSFolderHint, 0, len(in))
	// 每一轮把父已经落下的文件夹排到前面。
	for len(remaining) > 0 {
		// 这一轮先假设没有进展，落下一个才算往前。
		progress := false
		// 这一轮还排不上的先留在这里。
		next := remaining[:0]
		// 父还没落下的先留着，下一轮再排。
		for _, f := range remaining {
			// 靠近根的先落下，子文件夹才有地方挂。
			if f.ParentID == nil {
				// 父已经就绪，这一层可以落下。
				out = append(out, f)
				// 这一层已经落下，下一轮子节点可以挂。
				have[f.ID] = struct{}{}
				// 这一轮至少落下一个，还能继续往下排。
				progress = true
				continue
			}
			// 父已经排好，这一层现在可以落下。
			if _, ok := have[*f.ParentID]; ok {
				// 父已经就绪，这一层可以落下。
				out = append(out, f)
				// 这一层已经落下，下一轮子节点可以挂。
				have[f.ID] = struct{}{}
				// 这一轮至少落下一个，还能继续往下排。
				progress = true
				continue
			}
			// 父还没落下，留到下一轮再排。
			next = append(next, f)
		}
		// 只保留还没排上的，准备下一轮。
		remaining = next
		// 这一轮一个都没落下，剩下的原样接上以免死循环。
		if !progress {
			// 剩下的互相等父，原样接上以免丢掉。
			out = append(out, remaining...)
			break
		}
	}
	return out
}
