package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID         uuid.UUID      `json:"id"`                   // 稳定身份
	Kind       string         `json:"kind"`                 // process / project
	Level      string         `json:"level"`                // platform / factory / personal
	Name       string         `json:"name"`                 // 显示名
	Code       string         `json:"code,omitempty"`       // 只读编号，跟身份走
	Status     string         `json:"status"`               // 送达时状态
	Copyable   bool           `json:"copyable"`             // 与源相同
	WeldKind   string         `json:"weldKind"`             // 作业类型：与源相同
	Revision   int64          `json:"revision"`             // 钉死修订
	Content    []byte         `json:"content"`              // 正文
	Digest     []byte         `json:"digest"`               // 内容 SHA-256
	Deps       []AssetDep     `json:"deps"`                 // 工艺必须空
	CreatorID  *uuid.UUID     `json:"creatorId"`            // 个人级创建人；其余可空
	FSParentID *uuid.UUID     `json:"fsParentId,omitempty"` // 文件所在文件夹；空表示平台根
	FSPath     []FSFolderHint `json:"fsPath,omitempty"`     // 从靠近根到父文件夹，不含根；不进摘要
}

// ClosureSnapshot 是一份工程或单条工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 与源相同
	Copyable        bool            `json:"copyable"`        // 与源相同
	Status          string          `json:"status"`          // 与源相同
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // WAN→厂时必填
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // 厂→Client 或个人级装袋时必填
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// AssetReplica 是已送达本厂的一条平台级（身份, 修订）只读副本。
type AssetReplica struct {
	ID         uuid.UUID  `json:"id"`         // 平台级稳定身份
	Revision   int64      `json:"revision"`   // 送达修订
	Kind       string     `json:"kind"`       // process / project
	Level      string     `json:"level"`      // 固定 platform
	Name       string     `json:"name"`       // 显示名
	Code       string     `json:"code"`       // 只读编号，跟身份走
	Status     string     `json:"status"`     // 送达时状态
	Copyable   bool       `json:"copyable"`   // 与源相同
	WeldKind   string     `json:"weldKind"`   // 作业类型：与源相同
	Corner     bool       `json:"corner"`     // 包角工艺：按送达正文判；工程恒为否
	Content    []byte     `json:"content"`    // 正文
	Digest     []byte     `json:"digest"`     // SHA-256
	Deps       []AssetDep `json:"deps"`       // 工艺必须空
	ReceivedAt time.Time  `json:"receivedAt"` // 本厂收到时间
	Retracted  bool       `json:"retracted"`  // 云端已删；列表不再展示
}

// 已收平台级副本的落库行。
type replicaRow struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`      // 平台级身份
	Revision   int64     `gorm:"primaryKey"`                // 送达修订
	Kind       string    `gorm:"not null"`                  // process / project
	Level      string    `gorm:"not null"`                  // platform
	Name       string    `gorm:"not null"`                  // 显示名
	Code       *string   `gorm:"column:code"`               // 与云端原件相同；旧副本可空
	Status     string    `gorm:"not null"`                  // 送达时状态
	Copyable   bool      `gorm:"not null"`                  // 与源相同
	WeldKind   string    `gorm:"column:weld_kind;not null"` // 作业类型
	Corner     *bool     `gorm:"column:corner"`             // 包角工艺；空表示还没按正文判过
	Content    []byte    `gorm:"type:bytea;not null"`       // 正文
	Digest     []byte    `gorm:"type:bytea;not null"`       // SHA-256
	Deps       []byte    `gorm:"type:jsonb;not null"`       // 依赖 JSON
	ReceivedAt time.Time `gorm:"not null"`                  // 收到时间
	Retracted  bool      `gorm:"not null"`                  // 云端已删；列表不再展示
}

// 指定落库表名，避免查询时按类型名去猜。
func (replicaRow) TableName() string { return "asset_replicas" }

// InsertReplica 写入一条平台级副本；同身份修订且摘要相同则原样返回，不重封。
func (s *Store) InsertReplica(ctx context.Context, in AssetReplica) (AssetReplica, error) {
	// 摘要必须是三十二字节，否则拒绝写入。
	if err := assertAssetDigest(in.Digest); err != nil {
		return AssetReplica{}, err
	}
	// 先收成依赖，结果留给紧跟着的判断。
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	// 收成依赖失败就停，避免带着错误继续。
	if err != nil {
		return AssetReplica{}, err
	}
	// 先规范作业类型，结果留给紧跟着的判断。
	weldKind, err := NormalizeWeldKind(in.WeldKind)
	// 规范作业类型失败就停，避免带着错误继续。
	if err != nil {
		return AssetReplica{}, err
	}
	// 先按修订读副本元数据，结果留给紧跟着的判断。
	got, err := s.ReplicaMetaByIDRev(ctx, in.ID, in.Revision)
	// 已经有记录就核对摘要，不一致不能覆盖。
	if err == nil {
		// 比对原文这一支不成立就换路。
		if !bytes.Equal(got.Digest, in.Digest) {
			return AssetReplica{}, domain.ErrIntegrity
		}
		// 出错就停，避免把失败当成已经完成。
		if err := s.ensureReplicaFS(ctx, got); err != nil {
			return AssetReplica{}, err
		}
		return got, nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return AssetReplica{}, err
	}
	// 有租约用 MK 重封；无租约拒绝新修订，厂库不落明文。
	env, err := s.persistBody(in.ID, in.Revision, tableReplicas, nonempty(in.Content))
	// 封装正文失败就停，避免带着错误继续。
	if err != nil {
		return AssetReplica{}, err
	}
	// 组装要落库的行，编号和摘要已经核对。
	row := replicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind, Level: AssetLevelPlatform,
		Name: in.Name, Status: in.Status, Copyable: in.Copyable, WeldKind: weldKind, Corner: cornerOf(in.Kind, in.Content), Content: env,
		Digest: in.Digest, Deps: deps, ReceivedAt: time.Now().UTC(),
	}
	// 带来了编号才钉上，空着就让库保持空。
	if in.Code != "" {
		// 把这一列放进内存行，随后随保存写入。
		row.Code = &in.Code
	}
	// 放进同一事务，中途失败就整单回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 带来了编号才钉上，空着就让库保持空。
		if in.Code != "" {
			// 钉住编号失败就停，避免带着错误继续。
			if err := bindAssetCode(tx, in.ID, in.Code); err != nil {
				return err
			}
		}
		// 写入一行失败就停，避免带着错误继续。
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		// 挂上文件节点失败就停，避免带着错误继续。
		if err := placeAssetFileTx(tx, row.Kind, FSTreePlatform, nil, row.ID, row.Name, in.Code, uuid.Nil); err != nil {
			return err
		}
		// 先读这份副本已收到的最高修订，结果留给紧跟着的判断。
		top, err := maxRevision(tx, &replicaRow{}, "id = ?", row.ID)
		// 读最高修订失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 晚到的旧钉住修订不改名，免得把文件名改回去。
		if row.Revision < top {
			return nil
		}
		// 新修订是最高的，文件节点名跟上云端的新名字。
		return syncFSFileNameTx(tx, row.ID, row.Name)
	})
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 先按修订读副本元数据，结果留给紧跟着的判断。
			got, getErr := s.ReplicaMetaByIDRev(ctx, in.ID, in.Revision)
			// 按修订读副本元数据失败就停，避免带着错误继续。
			if getErr != nil {
				return AssetReplica{}, getErr
			}
			// 比对原文这一支不成立就换路。
			if !bytes.Equal(got.Digest, in.Digest) {
				return AssetReplica{}, domain.ErrIntegrity
			}
			// 出错就停，避免把失败当成已经完成。
			if err := s.ensureReplicaFS(ctx, got); err != nil {
				return AssetReplica{}, err
			}
			return got, nil
		}
		// 先把库错误译成业务错误再交回。
		return AssetReplica{}, mapAssetWriteErr(err)
	}
	// 库行收成对外结果再交回。
	return s.decodeReplica(row)
}

// ensureReplicaFS 副本已在库则补文件节点，避免列表里有资产没有目录项。
func (s *Store) ensureReplicaFS(ctx context.Context, r AssetReplica) error {
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 做完挂上文件节点后把结果交回。
		return placeAssetFileTx(tx, r.Kind, FSTreePlatform, nil, r.ID, r.Name, r.Code, uuid.Nil)
	})
}

// ReplicaByIDRev 按身份和修订读平台级副本，含正文。
func (s *Store) ReplicaByIDRev(ctx context.Context, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	// 准备承接查到的平台副本。
	var row replicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	// 库行收成对外结果再交回。
	return s.decodeReplica(row)
}

// ReplicaMetaByIDRev 按身份和修订读副本元数据，不解包。
func (s *Store) ReplicaMetaByIDRev(ctx context.Context, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	// 准备承接查到的平台副本。
	var row replicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Omit("Content").First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	// 库行收成对外结果再交回。
	return replicaFromRow(row), nil
}

// LatestReplica 读该身份未撤回的最高修订副本。
func (s *Store) LatestReplica(ctx context.Context, assetID uuid.UUID) (AssetReplica, error) {
	// 准备承接查到的平台副本。
	var row replicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("id = ? AND retracted = ?", assetID, false).Order("revision DESC").First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	// 库行收成对外结果再交回。
	return s.decodeReplica(row)
}

// LatestReplicaMeta 读未撤回最高修订副本的元数据，不解包。
func (s *Store) LatestReplicaMeta(ctx context.Context, assetID uuid.UUID) (AssetReplica, error) {
	// 准备承接查到的平台副本。
	var row replicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("id = ? AND retracted = ?", assetID, false).Omit("Content").Order("revision DESC").First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	// 库行收成对外结果再交回。
	return replicaFromRow(row), nil
}

// ListReplicas 列出本厂未撤回的平台级副本，不含正文。
func (s *Store) ListReplicas(ctx context.Context) ([]AssetReplica, error) {
	// 准备承接查到的多条平台副本。
	var rows []replicaRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("retracted = ?", false).Omit("Content").Order("received_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]AssetReplica, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, replicaFromRow(row))
	}
	return out, nil
}

// RetractReplicas 把该身份还没撤回的副本标成撤回，交回有没有改到；文件节点照常删，没有副本也算成功。
func (s *Store) RetractReplicas(ctx context.Context, assetID uuid.UUID) (bool, error) {
	// 先当没改到，事务里改到了再改过来。
	changed := false
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 只改还没撤回的行，重复送达改不到就不算新撤回。
		res := tx.Model(&replicaRow{}).Where("id = ? AND retracted = ?", assetID, false).Update("retracted", true)
		// 更新失败就停，避免带着错误继续。
		if res.Error != nil {
			return res.Error
		}
		// 记下这次有没有真改到行。
		changed = res.RowsAffected > 0
		// 删掉匹配行，库错误原样交回。
		return tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Delete(&fsNodeRow{}).Error
	})
	// 事务失败就交回库错误，不算撤回过。
	if err != nil {
		return false, err
	}
	return changed, nil
}

// TamperReplicaContent 只改正文不改摘要，供完整性夹具使用。
func (s *Store) TamperReplicaContent(ctx context.Context, assetID uuid.UUID, revision int64, content []byte) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&replicaRow{}).Where("id = ? AND revision = ?", assetID, revision).Update("content", nonempty(content))
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	// 夹具可能写进明文，下次领约要重新整扫补封。
	s.setPlainSealed(false)
	return nil
}

// 库行收成平台级副本视图。
func replicaFromRow(row replicaRow) AssetReplica {
	// 准备要交回的结果，后面逐条填。
	out := AssetReplica{
		ID: row.ID, Revision: row.Revision, Kind: row.Kind, Level: row.Level, Name: row.Name,
		Status: row.Status, Copyable: row.Copyable, WeldKind: row.WeldKind, Corner: row.Corner != nil && *row.Corner,
		Content: row.Content, Digest: row.Digest, Deps: unmarshalAssetDeps(row.Deps), ReceivedAt: row.ReceivedAt, Retracted: row.Retracted,
	}
	// 副本上有编号才抄出来，空的不填。
	if row.Code != nil {
		// 把这一列放进内存行，随后随保存写入。
		out.Code = *row.Code
	}
	return out
}
