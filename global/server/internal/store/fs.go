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
	if domain.IsUniqueViolation(err) {
		return domain.ErrDuplicateName
	}
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// checkFSName 文件夹名不能空、不能带路径分隔符。
func checkFSName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > fsNameMax || strings.ContainsAny(name, "/\\") {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// EnsureFSRoot 取出该种类的根；没有则建。
func (s *Store) EnsureFSRoot(ctx context.Context, assetKind string) (FSNode, error) {
	if assetKind != KindProcess && assetKind != KindProject {
		return FSNode{}, domain.ErrNotFound
	}
	var out FSNode
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := ensureFSRootTx(tx, assetKind, FSTreePlatform, nil)
		if err != nil {
			return err
		}
		out = n
		return nil
	})
	return out, err
}

// ensureFSRootTx 事务内取或建该种类的根文件夹。
func ensureFSRootTx(tx *gorm.DB, assetKind, treeLevel string, ownerID *uuid.UUID) (FSNode, error) {
	q := tx.Where("asset_kind = ? AND tree_level = ? AND parent_id IS NULL AND node_kind = ?", assetKind, treeLevel, FSNodeFolder)
	if ownerID == nil {
		q = q.Where("owner_id IS NULL")
	} else {
		q = q.Where("owner_id = ?", *ownerID)
	}
	var row fsNodeRow
	err := q.First(&row).Error
	if err == nil {
		return fsFromRow(row), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FSNode{}, err
	}
	now := time.Now().UTC()
	row = fsNodeRow{
		ID: id.New(), Name: "", NodeKind: FSNodeFolder, AssetKind: assetKind,
		TreeLevel: treeLevel, OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			err = q.First(&row).Error
			if err != nil {
				return FSNode{}, err
			}
			return fsFromRow(row), nil
		}
		return FSNode{}, mapFSErr(err)
	}
	return fsFromRow(row), nil
}

// ListFSNodes 列出该种类全部节点，不含资产正文。
func (s *Store) ListFSNodes(ctx context.Context, assetKind string) ([]FSNode, error) {
	var rows []fsNodeRow
	q := s.db.WithContext(ctx).Order("node_kind DESC, name ASC")
	if assetKind != "" {
		q = q.Where("asset_kind = ?", assetKind)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]FSNode, 0, len(rows))
	for _, r := range rows {
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// FSNodeByID 按身份取节点。
func (s *Store) FSNodeByID(ctx context.Context, nodeID uuid.UUID) (FSNode, error) {
	var row fsNodeRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	return fsFromRow(row), nil
}

// FSFileByAsset 按资产身份取文件节点。
func (s *Store) FSFileByAsset(ctx context.Context, assetID uuid.UUID) (FSNode, error) {
	var row fsNodeRow
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	return fsFromRow(row), nil
}

// InsertFSFolder 在父文件夹下新建文件夹。
func (s *Store) InsertFSFolder(ctx context.Context, parentID uuid.UUID, name string) (FSNode, error) {
	name, err := checkFSName(name)
	if err != nil {
		return FSNode{}, err
	}
	var out FSNode
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		parent, err := fsNodeTx(tx, parentID)
		if err != nil {
			return err
		}
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		now := time.Now().UTC()
		row := fsNodeRow{
			ID: id.New(), Name: name, ParentID: &parent.ID, NodeKind: FSNodeFolder,
			AssetKind: parent.AssetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return mapFSErr(err)
		}
		out = fsFromRow(row)
		return nil
	})
	return out, err
}

// RenameFSNode 改节点显示名；根不能改。
func (s *Store) RenameFSNode(ctx context.Context, nodeID uuid.UUID, name string) (FSNode, error) {
	name, err := checkFSName(name)
	if err != nil {
		return FSNode{}, err
	}
	var out FSNode
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := fsNodeTx(tx, nodeID)
		if err != nil {
			return err
		}
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		now := time.Now().UTC()
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"name": name, "updated_at": now})
		if res.Error != nil {
			return mapFSErr(res.Error)
		}
		n.Name = name
		n.UpdatedAt = now
		out = n
		return nil
	})
	return out, err
}

// MoveFSNode 改父节点；不能跨树、不能成环、不能移根。
func (s *Store) MoveFSNode(ctx context.Context, nodeID, parentID uuid.UUID) (FSNode, error) {
	var out FSNode
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := fsNodeTx(tx, nodeID)
		if err != nil {
			return err
		}
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		parent, err := fsNodeTx(tx, parentID)
		if err != nil {
			return err
		}
		if parent.NodeKind != FSNodeFolder {
			return domain.ErrForbidden
		}
		if parent.AssetKind != n.AssetKind || parent.TreeLevel != n.TreeLevel {
			return domain.ErrForbidden
		}
		if !ownerEq(parent.OwnerID, n.OwnerID) {
			return domain.ErrForbidden
		}
		// 不能挂到自己或后代。
		if parent.ID == n.ID || fsIsAncestorTx(tx, parent.ID, n.ID) {
			return domain.ErrCycle
		}
		now := time.Now().UTC()
		res := tx.Model(&fsNodeRow{}).Where("id = ?", nodeID).Updates(map[string]any{"parent_id": parent.ID, "updated_at": now})
		if res.Error != nil {
			return mapFSErr(res.Error)
		}
		n.ParentID = &parent.ID
		n.UpdatedAt = now
		out = n
		return nil
	})
	return out, err
}

// DeleteFSNode 删空文件夹或文件引用；有孩子则拒绝。
func (s *Store) DeleteFSNode(ctx context.Context, nodeID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := fsNodeTx(tx, nodeID)
		if err != nil {
			return err
		}
		if n.ParentID == nil {
			return domain.ErrForbidden
		}
		var kids int64
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ?", nodeID).Count(&kids).Error; err != nil {
			return err
		}
		if kids > 0 {
			return domain.ErrHasActiveChildren
		}
		res := tx.Where("id = ?", nodeID).Delete(&fsNodeRow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// ListFSChildren 列出直接孩子。
func (s *Store) ListFSChildren(ctx context.Context, parentID uuid.UUID) ([]FSNode, error) {
	var rows []fsNodeRow
	if err := s.db.WithContext(ctx).Where("parent_id = ?", parentID).Order("node_kind DESC, name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]FSNode, 0, len(rows))
	for _, r := range rows {
		out = append(out, fsFromRow(r))
	}
	return out, nil
}

// fsNodeTx 事务内按身份取节点。
func fsNodeTx(tx *gorm.DB, id uuid.UUID) (FSNode, error) {
	var row fsNodeRow
	if err := tx.First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FSNode{}, domain.ErrNotFound
		}
		return FSNode{}, err
	}
	return fsFromRow(row), nil
}

// fsIsAncestorTx 向上走父链，判断会不会成环。
func fsIsAncestorTx(tx *gorm.DB, nodeID, ancestorID uuid.UUID) bool {
	cur := nodeID
	for i := 0; i < 64; i++ {
		var row fsNodeRow
		if err := tx.Select("id", "parent_id").First(&row, "id = ?", cur).Error; err != nil {
			return false
		}
		if row.ParentID == nil {
			return false
		}
		if *row.ParentID == ancestorID {
			return true
		}
		cur = *row.ParentID
	}
	return false
}

// ownerEq 两个可空主人是否同一人。
func ownerEq(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// uniqueChildName 同级不撞名；已占用则带编号，避免 UNIQUE 把事务打挂。
func uniqueChildName(tx *gorm.DB, parentID uuid.UUID, name, extra string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = extra
	}
	cands := []string{name}
	if extra != "" && extra != name {
		cands = append(cands, name+" "+extra)
	}
	cands = append(cands, name+" "+id.New().String()[:8])
	for _, cand := range cands {
		var n int64
		if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ?", parentID, cand).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return cand, nil
		}
	}
	return name + " " + extra, nil
}

// placeAssetFileTx 给资产挂文件节点；同名则带编号，避免挡住新建。
func placeAssetFileTx(tx *gorm.DB, assetKind string, assetID uuid.UUID, name, code string, parentID uuid.UUID) error {
	var n int64
	if err := tx.Model(&fsNodeRow{}).Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	parent, err := fsNodeTx(tx, parentID)
	if err != nil {
		return err
	}
	if parent.NodeKind != FSNodeFolder || parent.AssetKind != assetKind {
		return domain.ErrForbidden
	}
	fileName, err := uniqueChildName(tx, parent.ID, name, code)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := fsNodeRow{
		ID: id.New(), Name: fileName, ParentID: &parent.ID, NodeKind: FSNodeFile,
		AssetKind: assetKind, TreeLevel: parent.TreeLevel, OwnerID: parent.OwnerID,
		AssetID: &assetID, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&row).Error; err != nil {
		return mapFSErr(err)
	}
	return nil
}

// syncFSFileNameTx 资产改名时同步文件节点名；撞名则保持旧名。
func syncFSFileNameTx(tx *gorm.DB, assetID uuid.UUID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var file fsNodeRow
	if err := tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if file.ParentID == nil {
		return nil
	}
	var n int64
	if err := tx.Model(&fsNodeRow{}).Where("parent_id = ? AND name = ? AND id <> ?", *file.ParentID, name, file.ID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return tx.Model(&fsNodeRow{}).Where("id = ?", file.ID).Updates(map[string]any{"name": name, "updated_at": time.Now().UTC()}).Error
}

// FSLineage 给出文件挂点和从靠近根到父文件夹的路径，不含根。
func (s *Store) FSLineage(ctx context.Context, assetID uuid.UUID) (*uuid.UUID, []FSFolderHint, error) {
	file, err := s.FSFileByAsset(ctx, assetID)
	if err != nil {
		return nil, nil, err
	}
	if file.ParentID == nil {
		return nil, nil, nil
	}
	var folders []FSFolderHint
	cur := *file.ParentID
	for i := 0; i < 64; i++ {
		n, err := s.FSNodeByID(ctx, cur)
		if err != nil {
			return nil, nil, err
		}
		if n.ParentID == nil {
			break
		}
		hint := FSFolderHint{ID: n.ID, Name: n.Name}
		if parent, err := s.FSNodeByID(ctx, *n.ParentID); err == nil && parent.ParentID != nil {
			pid := parent.ID
			hint.ParentID = &pid
		}
		folders = append(folders, hint)
		cur = *n.ParentID
	}
	for i, j := 0, len(folders)-1; i < j; i, j = i+1, j-1 {
		folders[i], folders[j] = folders[j], folders[i]
	}
	parent := *file.ParentID
	rootish := false
	if rn, err := s.FSNodeByID(ctx, parent); err == nil && rn.ParentID == nil {
		rootish = true
	}
	if rootish {
		return nil, folders, nil
	}
	return &parent, folders, nil
}

// PlatformFSLayout 收集该种类已有文件的祖先文件夹和挂点，供厂端对齐。
func (s *Store) PlatformFSLayout(ctx context.Context, assetKind string) (PlatformFSLayout, error) {
	nodes, err := s.ListFSNodes(ctx, assetKind)
	if err != nil {
		return PlatformFSLayout{}, err
	}
	byID := make(map[uuid.UUID]FSNode, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	seen := map[uuid.UUID]struct{}{}
	out := PlatformFSLayout{Kind: assetKind}
	for _, n := range nodes {
		if n.NodeKind != FSNodeFile || n.AssetID == nil || n.ParentID == nil {
			continue
		}
		parent := *n.ParentID
		hint := FSFileHint{AssetID: *n.AssetID}
		if p, ok := byID[parent]; ok && p.ParentID != nil {
			hint.ParentID = &parent
		}
		out.Files = append(out.Files, hint)
		cur := parent
		for i := 0; i < 64; i++ {
			p, ok := byID[cur]
			if !ok || p.ParentID == nil {
				break
			}
			if _, dup := seen[p.ID]; !dup {
				seen[p.ID] = struct{}{}
				h := FSFolderHint{ID: p.ID, Name: p.Name}
				if gp, ok := byID[*p.ParentID]; ok && gp.ParentID != nil {
					pid := gp.ID
					h.ParentID = &pid
				}
				out.Folders = append(out.Folders, h)
			}
			cur = *p.ParentID
		}
	}
	out.Folders = orderFSFolders(out.Folders)
	return out, nil
}

// orderFSFolders 先落靠近根的文件夹，避免父节点还不存在。
func orderFSFolders(in []FSFolderHint) []FSFolderHint {
	if len(in) < 2 {
		return in
	}
	remaining := append([]FSFolderHint(nil), in...)
	have := map[uuid.UUID]struct{}{}
	out := make([]FSFolderHint, 0, len(in))
	for len(remaining) > 0 {
		progress := false
		next := remaining[:0]
		for _, f := range remaining {
			if f.ParentID == nil {
				out = append(out, f)
				have[f.ID] = struct{}{}
				progress = true
				continue
			}
			if _, ok := have[*f.ParentID]; ok {
				out = append(out, f)
				have[f.ID] = struct{}{}
				progress = true
				continue
			}
			next = append(next, f)
		}
		remaining = next
		if !progress {
			out = append(out, remaining...)
			break
		}
	}
	return out
}
